package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// SubscriptionInheritanceService centralizes subscription hierarchy lookups and validation.
type SubscriptionInheritanceService interface {
	// ResolveChildCustomerIDs returns external customer IDs for active INHERITED subscriptions
	// under the given parent subscription.
	ResolveChildCustomerIDs(ctx context.Context, parentSubscriptionID string) ([]string, error)

	// ResolveParentSubscriptionIDs returns parent subscription IDs for a child customer
	// via their INHERITED subscriptions.
	ResolveParentSubscriptionIDs(ctx context.Context, childCustomerID string) ([]string, error)

	// GetAggregatedCustomerIDs returns external customer IDs to aggregate usage for a subscription:
	// PARENT includes the subscription customer plus all hierarchy children; other types only that customer.
	GetAggregatedCustomerIDs(ctx context.Context, sub *subscription.Subscription) ([]string, error)

	// ValidateCustomerHierarchyConflicts ensures customers do not mix STANDALONE with PARENT/INHERITED
	// in ways that violate the hierarchy workflow. parentCustomerID is the billing parent; customerIDs
	// must include the parent and any usage child IDs (same set used for listing existing subscriptions).
	ValidateCustomerHierarchyConflicts(ctx context.Context, parentCustomerID string, customerIDs []string, newSubType types.SubscriptionType) error
}

type subscriptionInheritanceService struct {
	ServiceParams
}

func NewSubscriptionInheritanceService(params ServiceParams) SubscriptionInheritanceService {
	return &subscriptionInheritanceService{
		ServiceParams: params,
	}
}

func (s *subscriptionInheritanceService) ResolveChildCustomerIDs(
	ctx context.Context,
	parentSubscriptionID string,
) ([]string, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubscriptionID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	inheritedSubs, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	childCustomerIDs := make([]string, 0, len(inheritedSubs))
	for _, sub := range inheritedSubs {
		childCustomer, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
		if err != nil {
			s.Logger.Warnw("failed to get child customer",
				"customer_id", sub.CustomerID, "error", err)
			continue
		}
		childCustomerIDs = append(childCustomerIDs, childCustomer.ExternalID)
	}

	return childCustomerIDs, nil
}

func (s *subscriptionInheritanceService) ResolveParentSubscriptionIDs(
	ctx context.Context,
	childCustomerID string,
) ([]string, error) {
	inheritedFilter := types.NewNoLimitSubscriptionFilter()
	inheritedFilter.CustomerID = childCustomerID
	inheritedFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	inheritedFilter.Status = lo.ToPtr(types.StatusPublished)
	inheritedFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	inheritedSubs, err := s.SubRepo.List(ctx, inheritedFilter)
	if err != nil {
		return nil, err
	}
	if len(inheritedSubs) == 0 {
		return nil, nil
	}

	parentIDs := make([]string, 0, len(inheritedSubs))
	for _, inherited := range inheritedSubs {
		if inherited.ParentSubscriptionID != nil && lo.FromPtr(inherited.ParentSubscriptionID) != "" {
			parentIDs = append(parentIDs, lo.FromPtr(inherited.ParentSubscriptionID))
		}
	}
	return lo.Uniq(parentIDs), nil
}

func (s *subscriptionInheritanceService) GetAggregatedCustomerIDs(
	ctx context.Context,
	sub *subscription.Subscription,
) ([]string, error) {
	if sub == nil {
		return nil, ierr.NewError("subscription is required").Mark(ierr.ErrValidation)
	}

	owner, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
	if err != nil {
		return nil, err
	}

	externalIDs := []string{owner.ExternalID}

	subType := sub.SubscriptionType
	if subType == "" {
		subType = types.SubscriptionTypeStandalone
	}

	if subType == types.SubscriptionTypeParent {
		childIDs, err := s.ResolveChildCustomerIDs(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		externalIDs = append(externalIDs, childIDs...)
	}

	return lo.Uniq(externalIDs), nil
}

func (s *subscriptionInheritanceService) ValidateCustomerHierarchyConflicts(
	ctx context.Context,
	parentCustomerID string,
	customerIDs []string,
	newSubType types.SubscriptionType,
) error {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerIDs = customerIDs
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
	}

	existingSubs, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	for _, existing := range existingSubs {
		existingType := existing.SubscriptionType
		if existingType == "" {
			existingType = types.SubscriptionTypeStandalone
		}
		if err := validateHierarchyWorkflowConflict(parentCustomerID, existing.CustomerID, newSubType, existingType); err != nil {
			return err
		}
	}
	return nil
}

// validateHierarchyWorkflowConflict encodes the same rules as the former
// validateCustomerSubscriptionWorkflow loop (parent vs usage customer).
func validateHierarchyWorkflowConflict(
	parentCustomerID, subscriptionCustomerID string,
	newSubType, existingType types.SubscriptionType,
) error {
	cid := subscriptionCustomerID

	if cid == parentCustomerID {
		if newSubType == types.SubscriptionTypeParent && existingType == types.SubscriptionTypeStandalone {
			return ierr.NewError("customer already has standalone subscriptions").
				WithHint("A customer cannot have both standalone and hierarchy-based subscriptions; cancel existing subscriptions first").
				WithReportableDetails(map[string]any{"customer_id": cid, "existing_type": existingType}).
				Mark(ierr.ErrInvalidOperation)
		}
		if newSubType == types.SubscriptionTypeStandalone && (existingType == types.SubscriptionTypeParent || existingType == types.SubscriptionTypeInherited) {
			return ierr.NewError("customer already participates in a subscription hierarchy").
				WithHint("A customer cannot have both standalone and hierarchy-based subscriptions; cancel existing subscriptions first").
				WithReportableDetails(map[string]any{"customer_id": cid, "existing_type": existingType}).
				Mark(ierr.ErrInvalidOperation)
		}
		return nil
	}

	if existingType == types.SubscriptionTypeStandalone {
		return ierr.NewError("child customer already has standalone subscriptions").
			WithHint("A customer cannot have both standalone and inherited subscriptions; cancel existing subscriptions first").
			WithReportableDetails(map[string]any{"customer_id": cid, "existing_type": existingType}).
			Mark(ierr.ErrInvalidOperation)
	}
	if existingType == types.SubscriptionTypeParent {
		return ierr.NewError("child customer is already a parent in another hierarchy").
			WithHint("A customer cannot be both a parent and an inherited child").
			WithReportableDetails(map[string]any{"customer_id": cid, "existing_type": existingType}).
			Mark(ierr.ErrInvalidOperation)
	}
	return nil
}
