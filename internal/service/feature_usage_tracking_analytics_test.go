package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- resolveEffectiveExternalIDs ---

func TestResolveEffectiveExternalIDs(t *testing.T) {
	tests := []struct {
		name     string
		req      *dto.GetUsageAnalyticsRequest
		expected []string
	}{
		{
			name:     "only ExternalCustomerID",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"},
			expected: []string{"cust_a"},
		},
		{
			name:     "only ExternalCustomerIDs",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b", "cust_c"}},
			expected: []string{"cust_b", "cust_c"},
		},
		{
			name: "both fields, no overlap",
			req: &dto.GetUsageAnalyticsRequest{
				ExternalCustomerID:  "cust_a",
				ExternalCustomerIDs: []string{"cust_b", "cust_c"},
			},
			expected: []string{"cust_a", "cust_b", "cust_c"},
		},
		{
			name: "both fields, with duplicate — each unique ID appears once",
			req: &dto.GetUsageAnalyticsRequest{
				ExternalCustomerID:  "cust_a",
				ExternalCustomerIDs: []string{"cust_a", "cust_b"},
			},
			expected: []string{"cust_a", "cust_b"},
		},
		{
			name:     "both empty returns nil",
			req:      &dto.GetUsageAnalyticsRequest{},
			expected: nil,
		},
		{
			name:     "empty string in ExternalCustomerID is ignored",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "", ExternalCustomerIDs: []string{"cust_x"}},
			expected: []string{"cust_x"},
		},
		{
			name:     "duplicates within ExternalCustomerIDs are deduplicated",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_a", "cust_a", "cust_b"}},
			expected: []string{"cust_a", "cust_b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveEffectiveExternalIDs(tc.req)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- validateAnalyticsRequest ---

func TestValidateAnalyticsRequest(t *testing.T) {
	svc := &featureUsageTrackingService{}

	t.Run("both fields empty returns validation error", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{})
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
	})

	t.Run("only ExternalCustomerID set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"})
		assert.NoError(t, err)
	})

	t.Run("only ExternalCustomerIDs set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b"}})
		assert.NoError(t, err)
	})

	t.Run("both fields set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
			ExternalCustomerID:  "cust_a",
			ExternalCustomerIDs: []string{"cust_b"},
		})
		assert.NoError(t, err)
	})

	t.Run("ExternalCustomerIDs with only empty strings is a validation error", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
			ExternalCustomerIDs: []string{"", ""},
		})
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
	})
}

// Note: scenarios requiring real DB or mocked repos (currency mismatch across customers,
// all-customers-fail → empty response, IncludeChildren expansion) are covered by
// integration tests in the standard test suite (testutil.SetupTestDB). They are not
// duplicated here as unit tests because fetchAnalyticsData, fetchCustomer, and
// fetchChildCustomers all require repo implementations.

// --- fetchParentSubscriptions ---

func TestFetchParentSubscriptions(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	ctx = context.WithValue(ctx, types.CtxRequestID, types.GenerateUUID())

	newSub := func(id string, subType types.SubscriptionType, parentSubID *string) *subscription.Subscription {
		return &subscription.Subscription{
			ID:                   id,
			SubscriptionType:     subType,
			ParentSubscriptionID: parentSubID,
			BaseModel:            types.GetDefaultBaseModel(ctx),
		}
	}

	newLineItem := func(id, subID string) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: subID,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	}

	t.Run("no subscriptions — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		parents, err := svc.fetchParentSubscriptions(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("standalone subscription — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_standalone", types.SubscriptionTypeStandalone, nil),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("parent-type subscription — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_parent", types.SubscriptionTypeParent, nil),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("one inherited subscription — fetches parent with line items", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		// Create the parent subscription with a line item
		parentSub := newSub("sub_parent_1", types.SubscriptionTypeParent, nil)
		lineItem := newLineItem("li_1", "sub_parent_1")
		require.NoError(t, subStore.CreateWithLineItems(ctx, parentSub, []*subscription.SubscriptionLineItem{lineItem}))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inherited_1", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_1")),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, "sub_parent_1", parents[0].ID)
		require.Len(t, parents[0].LineItems, 1)
		assert.Equal(t, "li_1", parents[0].LineItems[0].ID)
	})

	t.Run("two inherited subs pointing to same parent — parent fetched once", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		parentSub := newSub("sub_parent_shared", types.SubscriptionTypeParent, nil)
		lineItem := newLineItem("li_shared", "sub_parent_shared")
		require.NoError(t, subStore.CreateWithLineItems(ctx, parentSub, []*subscription.SubscriptionLineItem{lineItem}))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inh_a", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_shared")),
			newSub("sub_inh_b", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_shared")),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, "sub_parent_shared", parents[0].ID)
		require.Len(t, parents[0].LineItems, 1)
		assert.Equal(t, "li_shared", parents[0].LineItems[0].ID)
	})

	t.Run("parent subscription not found — returns error", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		// Do NOT create the parent sub in the store
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inh_missing", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_missing")),
		}
		_, err := svc.fetchParentSubscriptions(ctx, subs)
		require.Error(t, err)
		assert.True(t, ierr.IsNotFound(err), "expected a not-found error, got: %v", err)
	})
}
