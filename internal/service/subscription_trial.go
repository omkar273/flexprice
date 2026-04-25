package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// setCreateSubscriptionTrialWindow fills in trial start/end. Precedence: explicit dates, then
// subscription trial_period_days, then plan prices.
func setCreateSubscriptionTrialWindow(req *dto.CreateSubscriptionRequest, sub *subscription.Subscription, planPrices []*dto.PriceResponse) error {
	if req.TrialStart != nil && req.TrialEnd != nil {
		sub.TrialStart = req.TrialStart
		sub.TrialEnd = req.TrialEnd
		return nil
	}

	// check if the request has a trial_period_days else check if any price has a trial_period_days
	effectiveTrialDays := 0
	if req.TrialPeriodDays != nil {
		effectiveTrialDays = lo.FromPtr(req.TrialPeriodDays)
	} else {
		trialDaysPerRecurringFixedPrice := lo.Map(planPrices, func(p *dto.PriceResponse, _ int) int {
			return p.TrialPeriodDays
		})
		if len(trialDaysPerRecurringFixedPrice) == 0 {
			return nil
		}
		if len(trialDaysPerRecurringFixedPrice) > 1 {
			return ierr.NewError("all recurring fixed plan prices must have the same trial_period_days").
				WithHint("Align trial_period_days on plan prices or override with subscription trial_period_days").
				Mark(ierr.ErrValidation)
		}
		effectiveTrialDays = trialDaysPerRecurringFixedPrice[0]
	}

	if effectiveTrialDays <= 0 {
		sub.TrialStart, sub.TrialEnd = nil, nil
		return nil
	}

	// Window starts on subscription start, N full days (AddDate(0,0,N)).
	sub.TrialStart = lo.ToPtr(sub.StartDate)
	sub.TrialEnd = lo.ToPtr(sub.StartDate.AddDate(0, 0, effectiveTrialDays))
	return nil
}

// ProcessTrialEndDue picks up trialing subs past trial end, moves them to the first real period, and creates
// the trial-end invoice. Safe to run repeatedly for the same subscription.
func (s *subscriptionService) ProcessTrialEndDue(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error) {
	const batchSize = 100
	now := time.Now().UTC()

	s.Logger.InfowCtx(ctx, "starting trial end processing", "current_time", now)

	response := &dto.SubscriptionUpdatePeriodResponse{
		Items:        make([]*dto.SubscriptionUpdatePeriodResponseItem, 0),
		TotalFailed:  0,
		TotalSuccess: 0,
		StartAt:      now,
	}

	invoiceService := NewInvoiceService(s.ServiceParams)
	offset := 0
	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
				Offset: lo.ToPtr(offset),
				Status: lo.ToPtr(types.StatusPublished),
			},
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusTrialing},
			TrialEndDueLTE:     &now,
		}

		subs, err := s.SubRepo.GetSubscriptionsForBillingPeriodUpdate(ctx, filter)
		if err != nil {
			return response, err
		}

		if len(subs) == 0 {
			break
		}

		s.Logger.InfowCtx(ctx, "processing trial end batch",
			"batch_size", len(subs),
			"offset", offset)

		// These rows can be from different envs/tenants — set ctx so the store doesn't cross wires.
		for _, trialingSubscription := range subs {
			ctx = context.WithValue(ctx, types.CtxTenantID, trialingSubscription.TenantID)
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, trialingSubscription.EnvironmentID)
			ctx = context.WithValue(ctx, types.CtxUserID, trialingSubscription.CreatedBy)

			responseItem := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: trialingSubscription.ID,
				PeriodStart:    trialingSubscription.CurrentPeriodStart,
				PeriodEnd:      trialingSubscription.CurrentPeriodEnd,
			}

			err := s.processSubscriptionTrialEnd(ctx, trialingSubscription, invoiceService, now)
			if err != nil {
				s.Logger.ErrorwCtx(ctx, "failed to process trial end for subscription",
					"subscription_id", trialingSubscription.ID,
					"error", err)
				response.TotalFailed++
				responseItem.Error = err.Error()
			} else {
				response.TotalSuccess++
				responseItem.Success = true
			}
			response.Items = append(response.Items, responseItem)
		}

		offset += len(subs)
		if len(subs) < batchSize {
			break
		}
	}

	return response, nil
}

func (s *subscriptionService) processSubscriptionTrialEnd(ctx context.Context, sub *subscription.Subscription, invoiceService InvoiceService, now time.Time) error {
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil
	}
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused {
		return nil
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		s.Logger.WarnwCtx(ctx, "trialing subscription missing trial bounds, skipping",
			"subscription_id", sub.ID)
		return nil
	}
	if sub.TrialEnd.After(now) {
		return nil
	}

	subWithItems, _, err := s.SubRepo.GetWithLineItems(ctx, sub.ID)
	if err != nil {
		return err
	}
	sub = subWithItems

	// Billing really starts at trial end. Anchor there so the first paid period isn't short-changed
	// (same idea as trial end becomes the new cycle anchor).
	firstPeriodStart := lo.FromPtr(sub.TrialEnd)
	sub.BillingAnchor = firstPeriodStart
	firstPeriodEnd, err := types.NextBillingDate(firstPeriodStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
	if err != nil {
		return err
	}

	// Out of trialing, first real period on the books. If this job double-fires, we bail earlier because
	// we aren't "trialing" anymore.
	sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	sub.CurrentPeriodStart = firstPeriodStart
	sub.CurrentPeriodEnd = firstPeriodEnd
	if err := s.SubRepo.Update(ctx, sub); err != nil {
		return err
	}

	if err := s.cascadeTrialEndToInherited(ctx, sub); err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "subscription period advanced and moved to incomplete after trial end",
		"subscription_id", sub.ID,
		"first_period_start", firstPeriodStart,
		"first_period_end", firstPeriodEnd)

	// Already have a real invoice for this period? Don't mint another.
	existing, err := s.InvoiceRepo.GetForPeriod(
		ctx,
		sub.ID,
		firstPeriodStart,
		firstPeriodEnd,
		string(types.InvoiceBillingReasonSubscriptionTrialEnd),
	)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if existing != nil &&
		existing.InvoiceStatus != types.InvoiceStatusDraft &&
		existing.InvoiceStatus != types.InvoiceStatusSkipped {
		s.Logger.InfowCtx(ctx, "trial end invoice already exists for period, skipping invoice creation",
			"subscription_id", sub.ID,
			"invoice_id", existing.ID)
		return nil
	}

	paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID)
	paymentParams = paymentParams.NormalizePaymentParameters()

	// Generate the first bill after the trial. Duplicate guard is just above.
	trialEndInvoice, _, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    firstPeriodStart,
		PeriodEnd:      firstPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodStart,
		BillingReason:  types.InvoiceBillingReasonSubscriptionTrialEnd,
	}, paymentParams, types.InvoiceFlowRenewal, false)
	if err != nil {
		return err
	}

	if trialEndInvoice == nil {
		// Nothing to collect — we can go straight to active.
		if err := s.completeTrialConversionToActive(ctx, sub); err != nil {
			return err
		}
		s.Logger.InfowCtx(ctx, "subscription activated after zero-amount trial end",
			"subscription_id", sub.ID)
	}
	return nil
}

// cascadeTrialEndToInherited propagates the trial-end state (incomplete + advanced period) to all
// inherited subscriptions under a parent. Mirrors cascadePauseToInherited.
func (s *subscriptionService) cascadeTrialEndToInherited(ctx context.Context, parentSub *subscription.Subscription) error {
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil
	}
	children, err := s.getInheritedSubscriptions(ctx, parentSub.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.SubscriptionStatus = types.SubscriptionStatusIncomplete
		child.BillingAnchor = parentSub.BillingAnchor
		child.CurrentPeriodStart = parentSub.CurrentPeriodStart
		child.CurrentPeriodEnd = parentSub.CurrentPeriodEnd
		if err := s.SubRepo.Update(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// cascadeTrialActivationToInherited propagates active status to all inherited subscriptions
// once the parent's trial-end invoice is paid (or was zero-amount). Mirrors cascadeResumeToInherited.
func (s *subscriptionService) cascadeTrialActivationToInherited(ctx context.Context, parentSub *subscription.Subscription) error {
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil
	}
	children, err := s.getInheritedSubscriptions(ctx, parentSub.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.SubscriptionStatus = types.SubscriptionStatusActive
		if err := s.SubRepo.Update(ctx, child); err != nil {
			return err
		}
	}
	return nil
}
