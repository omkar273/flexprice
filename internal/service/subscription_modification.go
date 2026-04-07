package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionModificationService handles mid-cycle subscription modifications.
type SubscriptionModificationService interface {
	// Execute performs the modification and persists all changes.
	Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)

	// Preview returns what would happen without committing any changes.
	Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)
}

type subscriptionModificationService struct {
	serviceParams ServiceParams
}

// NewSubscriptionModificationService creates a new SubscriptionModificationService.
func NewSubscriptionModificationService(serviceParams ServiceParams) SubscriptionModificationService {
	return &subscriptionModificationService{
		serviceParams: serviceParams,
	}
}

// Execute performs the modification and persists all changes.
func (s *subscriptionModificationService) Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.executeInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.executeQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change").
			Mark(ierr.ErrValidation)
	}
}

// Preview returns what would happen without committing any changes.
func (s *subscriptionModificationService) Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.previewInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.previewQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change").
			Mark(ierr.ErrValidation)
	}
}

// ─────────────────────────────────────────────
// Sub-feature 1: Inheritance
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// 1. Get subscription
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// 2. Validate: not inherited, is active
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("cannot modify inherited subscription").
			WithHint("Inheritance can only be applied to standalone or parent subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can be modified for inheritance").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	// 3. Resolve external customers for inheritance
	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	// 4. Check for duplicate inherited subscriptions
	existingInherited, err := s.getInheritedSubscriptions(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	existingChildIDs := make(map[string]bool, len(existingInherited))
	for _, inh := range existingInherited {
		existingChildIDs[inh.CustomerID] = true
	}
	for _, childID := range childCustomerIDs {
		if existingChildIDs[childID] {
			return nil, ierr.NewError("duplicate inherited subscription").
				WithHint("A child customer already has an inherited subscription for this parent").
				WithReportableDetails(map[string]interface{}{"child_customer_id": childID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}
	}

	// 5. Transaction: update parent type and create inherited subscriptions
	changedSubs := make([]dto.ChangedSubscription, 0)
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedSubs = nil // reset for safety in case of retry
		// If standalone, promote to parent
		if sub.SubscriptionType == types.SubscriptionTypeStandalone {
			sub.SubscriptionType = types.SubscriptionTypeParent
			if err := sp.SubRepo.Update(txCtx, sub); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update subscription type to parent").
					Mark(ierr.ErrDatabase)
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:     sub.ID,
				Action: "updated",
				Status: sub.SubscriptionStatus,
			})
		}

		// Create inherited subscriptions for each child customer
		for _, childCustomerID := range childCustomerIDs {
			inheritedSub, err := s.createInheritedSubscription(txCtx, sub, childCustomerID)
			if err != nil {
				return err
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:     inheritedSub.ID,
				Action: "created",
				Status: inheritedSub.SubscriptionStatus,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. Publish webhook event
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	// 7. Return response with updated subscription
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

func (s *subscriptionModificationService) previewInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Get subscription (read-only)
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("cannot modify inherited subscription").
			WithHint("Inheritance can only be applied to standalone or parent subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can be modified for inheritance").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	// Resolve external customers
	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	// Check for duplicates
	existingInherited, err := s.getInheritedSubscriptions(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	existingChildIDs := make(map[string]bool, len(existingInherited))
	for _, inh := range existingInherited {
		existingChildIDs[inh.CustomerID] = true
	}
	for _, childID := range childCustomerIDs {
		if existingChildIDs[childID] {
			return nil, ierr.NewError("duplicate inherited subscription").
				WithHint("A child customer already has an inherited subscription for this parent").
				WithReportableDetails(map[string]interface{}{"child_customer_id": childID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}
	}

	// Build preview response (no DB mutations)
	changedSubs := make([]dto.ChangedSubscription, 0)
	if sub.SubscriptionType == types.SubscriptionTypeStandalone {
		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:     sub.ID,
			Action: "updated",
			Status: sub.SubscriptionStatus,
		})
	}
	for range childCustomerIDs {
		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:     "(preview-created)",
			Action: "created",
			Status: types.SubscriptionStatusActive,
		})
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Sub-feature 2: Quantity Change
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Get subscription with line items
	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have quantity changes applied").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	changedLineItems := make([]dto.ChangedLineItem, 0)
	changedInvoices := make([]dto.ChangedInvoice, 0)

	// itemsForProration accumulates pairs of old/new line items that need proration treatment.
	// It is populated inside the transaction and consumed after it commits.
	type prorationPair struct {
		old  *subscription.SubscriptionLineItem
		new_ *subscription.SubscriptionLineItem
	}
	var itemsForProration []prorationPair

	// Single transaction: end all old line items and create all new ones atomically.
	// Proration (invoice creation / wallet credits) happens after the transaction commits
	// because those operations have their own side effects.
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedLineItems = nil    // reset for safety
		itemsForProration = nil   // reset for safety

		for _, change := range params.LineItems {
			// Fetch line item
			lineItem, err := sp.SubscriptionLineItemRepo.Get(txCtx, change.ID)
			if err != nil {
				return err
			}

			// Validate it belongs to the subscription
			if lineItem.SubscriptionID != subscriptionID {
				return ierr.NewError("line item does not belong to subscription").
					WithHint("The specified line item ID must belong to the given subscription").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "subscription_id": subscriptionID}).
					Mark(ierr.ErrValidation)
			}

			// Validate it is published (active)
			if lineItem.Status != types.StatusPublished {
				return ierr.NewError("line item is not active").
					WithHint("Only published line items can have their quantity changed").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID}).
					Mark(ierr.ErrValidation)
			}

			// Validate it is a fixed-price item
			if lineItem.PriceType != types.PRICE_TYPE_FIXED {
				return ierr.NewError("line item is not a fixed-price item").
					WithHint("Quantity changes are only supported for fixed-price line items").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "price_type": lineItem.PriceType}).
					Mark(ierr.ErrValidation)
			}

			// End the old line item
			lineItem.EndDate = now
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, lineItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to end existing line item").
					Mark(ierr.ErrDatabase)
			}

			// Create new line item (copy with new quantity)
			newItem := &subscription.SubscriptionLineItem{
				ID:                      types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID:          lineItem.SubscriptionID,
				CustomerID:              lineItem.CustomerID,
				EntityID:                lineItem.EntityID,
				EntityType:              lineItem.EntityType,
				PlanDisplayName:         lineItem.PlanDisplayName,
				PriceID:                 lineItem.PriceID,
				PriceType:               lineItem.PriceType,
				MeterID:                 lineItem.MeterID,
				MeterDisplayName:        lineItem.MeterDisplayName,
				PriceUnitID:             lineItem.PriceUnitID,
				PriceUnit:               lineItem.PriceUnit,
				DisplayName:             lineItem.DisplayName,
				Quantity:                change.Quantity,
				Currency:                lineItem.Currency,
				BillingPeriod:           lineItem.BillingPeriod,
				BillingPeriodCount:      lineItem.BillingPeriodCount,
				InvoiceCadence:          lineItem.InvoiceCadence,
				TrialPeriod:             lineItem.TrialPeriod,
				StartDate:               now,
				CommitmentAmount:        lineItem.CommitmentAmount,
				CommitmentQuantity:      lineItem.CommitmentQuantity,
				CommitmentType:          lineItem.CommitmentType,
				CommitmentOverageFactor: lineItem.CommitmentOverageFactor,
				CommitmentTrueUpEnabled: lineItem.CommitmentTrueUpEnabled,
				CommitmentWindowed:      lineItem.CommitmentWindowed,
				CommitmentDuration:      lineItem.CommitmentDuration,
				EnvironmentID:           lineItem.EnvironmentID,
				BaseModel:               types.GetDefaultBaseModel(txCtx),
			}
			if err := sp.SubscriptionLineItemRepo.Create(txCtx, newItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create new line item with updated quantity").
					Mark(ierr.ErrDatabase)
			}

			changedLineItems = append(changedLineItems,
				dto.ChangedLineItem{
					ID:           lineItem.ID,
					PriceID:      lineItem.PriceID,
					Quantity:     lineItem.Quantity,
					EndDate:      now.Format(time.RFC3339),
					ChangeAction: "ended",
				},
				dto.ChangedLineItem{
					ID:           newItem.ID,
					PriceID:      newItem.PriceID,
					Quantity:     newItem.Quantity,
					StartDate:    now.Format(time.RFC3339),
					ChangeAction: "created",
				},
			)

			// Collect pairs that need proration (handled after the transaction commits)
			if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
				itemsForProration = append(itemsForProration, prorationPair{old: lineItem, new_: newItem})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Post-transaction: handle proration outside the DB transaction because invoice creation
	// and wallet top-ups carry their own side effects (payment attempts, credit grants).
	for _, pair := range itemsForProration {
		inv, err := s.handleQuantityChangeProration(ctx, sub, pair.old, pair.new_, now)
		if err != nil {
			sp.Logger.Errorw("failed to handle proration for quantity change", "error", err, "line_item_id", pair.old.ID)
			changedInvoices = append(changedInvoices, dto.ChangedInvoice{ID: "(failed)", Action: "failed", Status: "failed"})
			continue
		}
		if inv != nil {
			changedInvoices = append(changedInvoices, *inv)
		}
	}

	// Publish webhook event
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	// Build response
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

func (s *subscriptionModificationService) previewQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Get subscription with line items
	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have quantity changes applied").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	changedLineItems := make([]dto.ChangedLineItem, 0)
	changedInvoices := make([]dto.ChangedInvoice, 0)

	for _, change := range params.LineItems {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, change.ID)
		if err != nil {
			return nil, err
		}

		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("line item does not belong to subscription").
				WithHint("The specified line item ID must belong to the given subscription").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.Status != types.StatusPublished {
			return nil, ierr.NewError("line item is not active").
				WithHint("Only published line items can have their quantity changed").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.PriceType != types.PRICE_TYPE_FIXED {
			return nil, ierr.NewError("line item is not a fixed-price item").
				WithHint("Quantity changes are only supported for fixed-price line items").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "price_type": lineItem.PriceType}).
				Mark(ierr.ErrValidation)
		}

		changedLineItems = append(changedLineItems,
			dto.ChangedLineItem{
				ID:           "(preview-ended)",
				PriceID:      lineItem.PriceID,
				Quantity:     lineItem.Quantity,
				EndDate:      now.Format(time.RFC3339),
				ChangeAction: "ended",
			},
			dto.ChangedLineItem{
				ID:           "(preview-created)",
				PriceID:      lineItem.PriceID,
				Quantity:     change.Quantity,
				StartDate:    now.Format(time.RFC3339),
				ChangeAction: "created",
			},
		)

		// Preview proration for in-advance line items
		if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
			// Build a preview new item to compute proration
			previewNewItem := &subscription.SubscriptionLineItem{
				PriceID:  lineItem.PriceID,
				Quantity: change.Quantity,
			}
			inv, err := s.handleQuantityChangeProration(ctx, sub, lineItem, previewNewItem, now)
			if err != nil {
				sp.Logger.Warnw("failed to preview proration for quantity change", "error", err, "line_item_id", lineItem.ID)
			} else if inv != nil {
				changedInvoices = append(changedInvoices, *inv)
			}
		}
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

// handleQuantityChangeProration handles the proration logic for quantity changes on in-advance line items.
func (s *subscriptionModificationService) handleQuantityChangeProration(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newItem *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
) (*dto.ChangedInvoice, error) {
	sp := s.serviceParams
	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
	if err != nil {
		return nil, err
	}

	customerTimezone := sub.CustomerTimezone
	if customerTimezone == "" {
		customerTimezone = "UTC"
	}

	prorationParams := proration.ProrationParams{
		SubscriptionID:     sub.ID,
		LineItemID:         oldItem.ID,
		PlanPayInAdvance:   price.Price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd.Add(-time.Second),
		Action:             types.ProrationActionQuantityChange,
		NewPriceID:         newItem.PriceID,
		OldQuantity:        oldItem.Quantity,
		NewQuantity:        newItem.Quantity,
		NewPricePerUnit:    price.Price.Amount,
		OldPricePerUnit:    price.Price.Amount,
		ProrationDate:      effectiveDate,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		ProrationStrategy:  types.StrategySecondBased,
		Currency:           sub.Currency,
		PlanDisplayName:    oldItem.PlanDisplayName,
		CustomerTimezone:   customerTimezone,
	}

	result, err := prorationSvc.CalculateProration(ctx, prorationParams)
	if err != nil {
		return nil, err
	}

	if result.NetAmount.IsZero() {
		return nil, nil
	}

	if result.NetAmount.GreaterThan(decimal.Zero) {
		// Upgrade: create a delta-only invoice for exactly the prorated amount (Stripe-style).
		// We do NOT re-bill the full remaining period — only the incremental charge.
		invoiceSvc := NewInvoiceService(sp)
		periodEnd := sub.CurrentPeriodEnd
		billingPeriod := string(sub.BillingPeriod)

		// Build a descriptive line item for the delta charge.
		qtyDelta := newItem.Quantity.Sub(oldItem.Quantity)
		displayName := oldItem.PlanDisplayName + " — Quantity Change Proration"
		priceID := oldItem.PriceID
		priceType := string(price.Price.Type)
		planDisplayName := oldItem.PlanDisplayName
		lineItemDescription := fmt.Sprintf("Proration for quantity change: %s → %s units × $%s/unit for remaining period",
			oldItem.Quantity.String(), newItem.Quantity.String(), price.Price.Amount.String())
		lineItems := []dto.CreateInvoiceLineItemRequest{
			{
				PriceID:         &priceID,
				PriceType:       &priceType,
				PlanDisplayName: &planDisplayName,
				DisplayName:     &displayName,
				Amount:          result.NetAmount,
				Quantity:        qtyDelta,
				PeriodStart:     &effectiveDate,
				PeriodEnd:       &periodEnd,
				Metadata:        types.Metadata{"description": lineItemDescription},
			},
		}

		// Use InvoiceTypeOneOff so ComputeInvoice uses the explicit delta amount rather
		// than recomputing from the subscription's (now-updated) line items.
		// SubscriptionID is set for reference/traceability only.
		inv, err := invoiceSvc.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     sub.CustomerID,
			SubscriptionID: &sub.ID,
			InvoiceType:    types.InvoiceTypeOneOff,
			Currency:       sub.Currency,
			BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
			AmountDue:      result.NetAmount,
			Total:          result.NetAmount,
			Subtotal:       result.NetAmount,
			PeriodStart:    &effectiveDate,
			PeriodEnd:      &periodEnd,
			BillingPeriod:  &billingPeriod,
			LineItems:      lineItems,
		})
		if err != nil {
			sp.Logger.Errorw("failed to create delta proration invoice for quantity change", "error", err)
			return &dto.ChangedInvoice{ID: "(failed)", Action: "failed", Status: "failed"}, err
		}
		// CreateInvoice with InvoiceTypeOneOff already finalizes the invoice internally.
		// Attempt payment (credits + payment method charge).
		if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
			sp.Logger.Warnw("failed to attempt payment for delta proration invoice", "error", err, "invoice_id", inv.ID)
		}
		// Re-fetch to get latest payment status after finalize+payment attempt.
		latest, fetchErr := invoiceSvc.GetInvoice(ctx, inv.ID)
		if fetchErr != nil {
			latest = inv
		}
		return &dto.ChangedInvoice{
			ID:     latest.ID,
			Action: "created",
			Status: string(latest.PaymentStatus),
		}, nil
	}

	// Downgrade: wallet credit
	walletSvc := NewWalletService(sp)
	creditAmount := result.NetAmount.Abs()
	if err := walletSvc.TopUpWalletForProratedCharge(ctx, sub.CustomerID, creditAmount, sub.Currency); err != nil {
		sp.Logger.Errorw("failed to top up wallet for downgrade proration", "error", err)
	}
	return &dto.ChangedInvoice{
		ID:     "(wallet_credit)",
		Action: "wallet_credit",
		Status: "issued",
	}, nil
}

// ─────────────────────────────────────────────
// Helper methods
// ─────────────────────────────────────────────

// resolveExternalCustomersForInheritance resolves published customers by external ID and validates
// they may receive an inherited subscription.
func (s *subscriptionModificationService) resolveExternalCustomersForInheritance(ctx context.Context, subscriberCustomerID string, externalIDs []string) ([]string, error) {
	childFilter := types.NewNoLimitCustomerFilter()
	childFilter.ExternalIDs = externalIDs
	childFilter.Status = lo.ToPtr(types.StatusPublished)
	customers, err := s.serviceParams.CustomerRepo.ListAll(ctx, childFilter)
	if err != nil {
		return nil, err
	}

	byExternalID := make(map[string]*customer.Customer, len(customers))
	for _, cust := range customers {
		byExternalID[cust.ExternalID] = cust
	}

	childCustomerIDs := make([]string, 0, len(externalIDs))
	for _, extID := range externalIDs {
		cust, ok := byExternalID[extID]
		if !ok {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer exists for the given external id in this environment").
				WithReportableDetails(map[string]interface{}{"external_id": extID}).
				Mark(ierr.ErrNotFound)
		}
		if cust.ID == subscriberCustomerID {
			return nil, ierr.NewError("cannot inherit onto itself").
				WithHint("The subscriber cannot appear in external_customer_ids_to_inherit_subscription").
				WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
				Mark(ierr.ErrValidation)
		}
		if cust.Status != types.StatusPublished {
			return nil, ierr.NewError("customer is not active").
				WithHint("Only active/published customers can receive inherited subscriptions").
				WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
				Mark(ierr.ErrValidation)
		}
		childCustomerIDs = append(childCustomerIDs, cust.ID)
	}
	return childCustomerIDs, nil
}

// getInheritedSubscriptions retrieves all INHERITED child subscriptions for a parent subscription.
func (s *subscriptionModificationService) getInheritedSubscriptions(ctx context.Context, parentSubID string) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusPaused,
	}
	return s.serviceParams.SubRepo.List(ctx, filter)
}

// createInheritedSubscription creates a child inherited subscription from a parent.
func (s *subscriptionModificationService) createInheritedSubscription(ctx context.Context, parent *subscription.Subscription, childCustomerID string) (*subscription.Subscription, error) {
	inheritedSub := &subscription.Subscription{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:             childCustomerID,
		PlanID:                 parent.PlanID,
		Currency:               parent.Currency,
		LookupKey:              "",
		SubscriptionStatus:     parent.SubscriptionStatus,
		BillingAnchor:          parent.BillingAnchor,
		BillingCycle:           parent.BillingCycle,
		StartDate:              parent.StartDate,
		EndDate:                parent.EndDate,
		CurrentPeriodStart:     parent.CurrentPeriodStart,
		CurrentPeriodEnd:       parent.CurrentPeriodEnd,
		BillingCadence:         parent.BillingCadence,
		BillingPeriod:          parent.BillingPeriod,
		BillingPeriodCount:     parent.BillingPeriodCount,
		Version:                1,
		EnvironmentID:          parent.EnvironmentID,
		PauseStatus:            parent.PauseStatus,
		PaymentBehavior:        parent.PaymentBehavior,
		CollectionMethod:       parent.CollectionMethod,
		GatewayPaymentMethodID: parent.GatewayPaymentMethodID,
		CustomerTimezone:       parent.CustomerTimezone,
		ProrationBehavior:      parent.ProrationBehavior,
		ParentSubscriptionID:   &parent.ID,
		SubscriptionType:       types.SubscriptionTypeInherited,
		PaymentTerms:           parent.PaymentTerms,
		EnableTrueUp:           parent.EnableTrueUp,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	if err := s.serviceParams.SubRepo.Create(ctx, inheritedSub); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create inherited subscription for child customer").
			WithReportableDetails(map[string]interface{}{
				"parent_subscription_id": parent.ID,
				"child_customer_id":      childCustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return inheritedSub, nil
}

// publishSystemEvent publishes a webhook event for a subscription change.
func (s *subscriptionModificationService) publishSystemEvent(ctx context.Context, eventName types.WebhookEventName, subscriptionID string) {
	eventPayload := webhookDto.InternalSubscriptionEvent{
		SubscriptionID: subscriptionID,
		TenantID:       types.GetTenantID(ctx),
	}

	webhookPayload, err := json.Marshal(eventPayload)
	if err != nil {
		s.serviceParams.Logger.ErrorwCtx(ctx, "failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeSubscription,
		EntityID:      subscriptionID,
	}
	if err := s.serviceParams.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.serviceParams.Logger.ErrorfCtx(ctx, "failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}
