package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUniformTrialPeriodDaysAmongRecurringFixedPlanPrices_Success(t *testing.T) {
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	}
	d, err := uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(prices)
	require.NoError(t, err)
	assert.Equal(t, 14, d)
}

func TestUniformTrialPeriodDaysAmongRecurringFixedPlanPrices_Mismatch(t *testing.T) {
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 7}},
	}
	_, err := uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(prices)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestUniformTrialPeriodDaysAmongRecurringFixedPlanPrices_SkipsNonFixedRecurring(t *testing.T) {
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_USAGE, TrialPeriodDays: 99}},
	}
	d, err := uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(prices)
	require.NoError(t, err)
	assert.Equal(t, 0, d)
}

func TestResolveEffectiveTrialPeriodDays_RequestOverrides(t *testing.T) {
	seven := 7
	req := &dto.CreateSubscriptionRequest{TrialPeriodDays: &seven}
	d, err := resolveEffectiveTrialPeriodDays(req, nil)
	require.NoError(t, err)
	assert.Equal(t, 7, d)

	zero := 0
	req2 := &dto.CreateSubscriptionRequest{TrialPeriodDays: &zero}
	d2, err := resolveEffectiveTrialPeriodDays(req2, []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, d2)
}

func TestApplyTrialWindowToSubscription_FromDays(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{}
	inherit := 5
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: inherit}},
	}

	days, err := applyTrialWindowToSubscription(req, sub, prices)
	require.NoError(t, err)
	assert.Equal(t, 5, days)
	require.NotNil(t, sub.TrialStart)
	require.NotNil(t, sub.TrialEnd)
	assert.True(t, sub.TrialStart.Equal(start))
	assert.Equal(t, start.AddDate(0, 0, 5), *sub.TrialEnd)
}

func TestApplyTrialWindowToSubscription_InternalBounds(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{
		TrialStart: &start,
		TrialEnd:   &trialEnd,
	}

	days, err := applyTrialWindowToSubscription(req, sub, nil)
	require.NoError(t, err)
	assert.Equal(t, 14, days)
	assert.Equal(t, &start, sub.TrialStart)
	assert.Equal(t, &trialEnd, sub.TrialEnd)
}

func TestApplyTrialWindowToSubscription_ZeroClears(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start, TrialStart: &start, TrialEnd: &start}
	z := 0
	req := &dto.CreateSubscriptionRequest{TrialPeriodDays: &z}
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	}

	days, err := applyTrialWindowToSubscription(req, sub, prices)
	require.NoError(t, err)
	assert.Equal(t, 0, days)
	assert.Nil(t, sub.TrialStart)
	assert.Nil(t, sub.TrialEnd)
}
