package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) executeCouponModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	switch params.Action {
	case dto.SubModifyCouponActionAdd:
		return s.executeAddCoupon(ctx, subscriptionID, params, effectiveDate)
	case dto.SubModifyCouponActionRemove:
		return s.executeRemoveCoupon(ctx, subscriptionID, params, effectiveDate)
	default:
		return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) executeAddCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate subscription exists before any mutation.
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Resolve coupon by coupon_code only.
	c, err := sp.CouponRepo.GetByCode(ctx, *params.CouponCode)
	if err != nil {
		return nil, ierr.NewError("coupon not found").
			WithHintf("No published coupon with code '%s'", *params.CouponCode).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Ensure the coupon is in 'published' status").
			Mark(ierr.ErrValidation)
	}
	couponID := c.ID

	// Resolve target: line-item level or subscription level.
	var lineItemID *string
	if params.SubscriptionLineItemID != nil {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, *params.SubscriptionLineItemID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("subscription_line_item_id does not belong to this subscription").
				WithHint("Ensure the line item belongs to this subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_line_item_id": *params.SubscriptionLineItemID,
					"subscription_id":           subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
		lineItemID = params.SubscriptionLineItemID
	} else if params.SubscriptionID != nil && *params.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("subscription_id does not match the subscription being modified").
			WithHint("subscription_id must match the subscription in the URL").
			WithReportableDetails(map[string]interface{}{
				"provided_subscription_id": *params.SubscriptionID,
				"subscription_id":          subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	startDate := effectiveDate
	if params.StartDate != nil {
		startDate = params.StartDate.UTC()
	}

	assoc := &coupon_association.CouponAssociation{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:               couponID,
		SubscriptionID:         subscriptionID,
		SubscriptionLineItemID: lineItemID,
		StartDate:              startDate,
		EndDate:                params.EndDate,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	if err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		return sp.CouponAssociationRepo.Create(txCtx, assoc)
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) executeRemoveCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	associationID := *params.CouponAssociationID

	// Validate subscription exists before any mutation.
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id belonging to this subscription").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("association does not belong to this subscription").
			WithReportableDetails(map[string]interface{}{
				"association_id":  associationID,
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Resolve the target end_date: an explicit value voids/ends at that point (down to
	// start_date, which fully voids the association); omitted defaults to now, preserving
	// today's behavior.
	newEndDate := effectiveDate
	if params.EndDate != nil {
		newEndDate = params.EndDate.UTC()
	}

	if newEndDate.Before(assoc.StartDate) {
		return nil, ierr.NewError("end_date cannot be before start_date").
			WithHint("Provide an end_date on or after the association's start_date; pass end_date equal to start_date to void it entirely").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"start_date":     assoc.StartDate,
				"end_date":       newEndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Only reject extending an already-ended association forward: a plain remove (no
	// explicit end_date) on an already-inactive association still errors here exactly as
	// before, since effectiveDate (now) is after its past end_date. An explicit end_date at
	// or before the current end_date — down to start_date, i.e. voiding — is allowed through.
	if assoc.EndDate != nil && newEndDate.After(*assoc.EndDate) {
		return nil, ierr.NewError("association already inactive").
			WithHint("This coupon association has already ended; provide an end_date at or before its current end_date to void it").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"end_date":       assoc.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	assoc.EndDate = &newEndDate
	if err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		return sp.CouponAssociationRepo.Update(txCtx, assoc)
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) previewCouponModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	switch params.Action {
	case dto.SubModifyCouponActionAdd:
		return s.previewAddCoupon(ctx, subscriptionID, params, effectiveDate)
	case dto.SubModifyCouponActionRemove:
		return s.previewRemoveCoupon(ctx, subscriptionID, params, effectiveDate)
	default:
		return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) previewAddCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Resolve coupon by coupon_code only.
	c, err := sp.CouponRepo.GetByCode(ctx, *params.CouponCode)
	if err != nil {
		return nil, ierr.NewError("coupon not found").
			WithHintf("No published coupon with code '%s'", *params.CouponCode).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Ensure the coupon is in 'published' status").
			Mark(ierr.ErrValidation)
	}

	// Resolve target: line-item level or subscription level.
	if params.SubscriptionLineItemID != nil {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, *params.SubscriptionLineItemID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("subscription_line_item_id does not belong to this subscription").
				WithHint("Ensure the line item belongs to this subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_line_item_id": *params.SubscriptionLineItemID,
					"subscription_id":           subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	} else if params.SubscriptionID != nil && *params.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("subscription_id does not match the subscription being modified").
			WithHint("subscription_id must match the subscription in the URL").
			WithReportableDetails(map[string]interface{}{
				"provided_subscription_id": *params.SubscriptionID,
				"subscription_id":          subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) previewRemoveCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	associationID := *params.CouponAssociationID

	assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("association does not belong to this subscription").
			WithReportableDetails(map[string]interface{}{
				"association_id":  associationID,
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Resolve the target end_date: an explicit value voids/ends at that point (down to
	// start_date, which fully voids the association); omitted defaults to now, preserving
	// today's behavior.
	newEndDate := effectiveDate
	if params.EndDate != nil {
		newEndDate = params.EndDate.UTC()
	}

	if newEndDate.Before(assoc.StartDate) {
		return nil, ierr.NewError("end_date cannot be before start_date").
			WithHint("Provide an end_date on or after the association's start_date; pass end_date equal to start_date to void it entirely").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"start_date":     assoc.StartDate,
				"end_date":       newEndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Only reject extending an already-ended association forward: a plain remove (no
	// explicit end_date) on an already-inactive association still errors here exactly as
	// before, since effectiveDate (now) is after its past end_date. An explicit end_date at
	// or before the current end_date — down to start_date, i.e. voiding — is allowed through.
	if assoc.EndDate != nil && newEndDate.After(*assoc.EndDate) {
		return nil, ierr.NewError("association already inactive").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrValidation)
	}

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}
