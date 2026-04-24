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

func uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(prices []*dto.PriceResponse) (int, error) {
	var first *int
	for _, pr := range prices {
		p := pr.Price
		if p.BillingCadence != types.BILLING_CADENCE_RECURRING || p.Type != types.PRICE_TYPE_FIXED {
			continue
		}
		d := p.TrialPeriodDays
		if first == nil {
			v := d
			first = &v
			continue
		}
		if *first != d {
			return 0, ierr.NewError("all recurring fixed plan prices must have the same trial_period_days").
				WithHint("Align trial_period_days on plan prices or override with subscription trial_period_days").
				WithReportableDetails(map[string]any{
					"expected": *first,
					"got":      d,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	if first == nil {
		return 0, nil
	}
	return *first, nil
}

func resolveEffectiveTrialPeriodDays(req *dto.CreateSubscriptionRequest, planPrices []*dto.PriceResponse) (int, error) {
	if req.TrialPeriodDays != nil {
		return *req.TrialPeriodDays, nil
	}
	return uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(planPrices)
}

// trialDaysFromBounds returns whole days between trial start and end (floor of duration / 24h).
func trialDaysFromBounds(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	return int(end.Sub(start) / (24 * time.Hour))
}

// applyTrialWindowToSubscription sets TrialStart/TrialEnd from internal bounds (e.g. Stripe) or from
// resolved trial_period_days vs subscription StartDate. Returns effective trial days for recurring fixed line items.
func applyTrialWindowToSubscription(req *dto.CreateSubscriptionRequest, sub *subscription.Subscription, planPrices []*dto.PriceResponse) (effectiveTrialDays int, err error) {
	if req.TrialStart != nil && req.TrialEnd != nil {
		sub.TrialStart = req.TrialStart
		sub.TrialEnd = req.TrialEnd
		return trialDaysFromBounds(*req.TrialStart, *req.TrialEnd), nil
	}

	planHasRecurringFixedTrial := false
	for _, pr := range planPrices {
		if pr == nil || pr.Price == nil {
			continue
		}
		p := pr.Price
		if p.BillingCadence == types.BILLING_CADENCE_RECURRING && p.Type == types.PRICE_TYPE_FIXED && p.TrialPeriodDays > 0 {
			planHasRecurringFixedTrial = true
			break
		}
	}
	if req.TrialPeriodDays == nil && !planHasRecurringFixedTrial {
		return 0, nil
	}

	days, err := resolveEffectiveTrialPeriodDays(req, planPrices)
	if err != nil {
		return 0, err
	}
	if days <= 0 {
		sub.TrialStart, sub.TrialEnd = nil, nil
		return 0, nil
	}

	ts := sub.StartDate
	te := ts.AddDate(0, 0, days)
	sub.TrialStart = &ts
	sub.TrialEnd = &te
	return days, nil
}

// applyTrialingStateAndPeriods sets subscription to trialing and aligns the current billable period to the
// trial window when trial bounds exist. Skipped for draft. If the client set an explicit subscription_status
// other than trialing, status and periods are left unchanged (admin / gateway override).
func applyTrialingStateAndPeriods(req *dto.CreateSubscriptionRequest, sub *subscription.Subscription) {
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		return
	}
	if req.SubscriptionStatus == types.SubscriptionStatusDraft {
		return
	}
	if req.SubscriptionStatus == types.SubscriptionStatusTrialing {
		sub.SubscriptionStatus = types.SubscriptionStatusTrialing
		sub.CurrentPeriodStart = *sub.TrialStart
		sub.CurrentPeriodEnd = *sub.TrialEnd
		return
	}
	if req.SubscriptionStatus != "" {
		return
	}
	sub.SubscriptionStatus = types.SubscriptionStatusTrialing
	sub.CurrentPeriodStart = *sub.TrialStart
	sub.CurrentPeriodEnd = *sub.TrialEnd
}

// ProcessTrialEndDue finds trialing subscriptions whose trial has ended and creates the converting invoice
// (billing_reason SUBSCRIPTION_TRIAL_END), then runs the same payment path as renewal. Idempotent per trial period.
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

		for _, sub := range subs {
			ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
			ctx = context.WithValue(ctx, types.CtxUserID, sub.CreatedBy)

			item := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: sub.ID,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
			}

			err := s.processSubscriptionTrialEnd(ctx, sub, invoiceService, now)
			if err != nil {
				s.Logger.ErrorwCtx(ctx, "failed to process trial end for subscription",
					"subscription_id", sub.ID,
					"error", err)
				response.TotalFailed++
				item.Error = err.Error()
			} else {
				response.TotalSuccess++
				item.Success = true
			}
			response.Items = append(response.Items, item)
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

	existing, err := s.InvoiceRepo.GetForPeriod(
		ctx,
		sub.ID,
		lo.FromPtr(sub.TrialStart),
		lo.FromPtr(sub.TrialEnd),
		string(types.InvoiceBillingReasonSubscriptionTrialEnd),
	)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if existing != nil &&
		existing.InvoiceStatus != types.InvoiceStatusDraft &&
		existing.InvoiceStatus != types.InvoiceStatusSkipped {
		s.Logger.InfowCtx(ctx, "trial end invoice already exists for period, skipping",
			"subscription_id", sub.ID,
			"invoice_id", existing.ID)
		return nil
	}

	subWithItems, _, err := s.SubRepo.GetWithLineItems(ctx, sub.ID)
	if err != nil {
		return err
	}
	sub = subWithItems

	paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID)
	paymentParams = paymentParams.NormalizePaymentParameters()

	inv, _, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    lo.FromPtr(sub.TrialStart),
		PeriodEnd:      lo.FromPtr(sub.TrialEnd),
		ReferencePoint: types.ReferencePointPeriodStart,
		BillingReason:  types.InvoiceBillingReasonSubscriptionTrialEnd,
	}, paymentParams, types.InvoiceFlowRenewal, false)
	if err != nil {
		return err
	}
	if inv == nil {
		s.Logger.InfowCtx(ctx, "no invoice created for trial end (skipped zero amount or inherited)",
			"subscription_id", sub.ID)
	}
	return nil
}
