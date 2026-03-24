package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
