package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
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

func TestApplyTrialingStateAndPeriods_AutoFromTrialWindow(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{}

	applyTrialingStateAndPeriods(req, sub)
	assert.Equal(t, types.SubscriptionStatusTrialing, sub.SubscriptionStatus)
	assert.True(t, sub.CurrentPeriodStart.Equal(ts))
	assert.True(t, sub.CurrentPeriodEnd.Equal(te))
}

func TestApplyTrialingStateAndPeriods_ExplicitActiveNoPromote(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{SubscriptionStatus: types.SubscriptionStatusActive}

	applyTrialingStateAndPeriods(req, sub)
	assert.Equal(t, types.SubscriptionStatusActive, sub.SubscriptionStatus)
	assert.True(t, sub.CurrentPeriodStart.Equal(periodStart))
	assert.True(t, sub.CurrentPeriodEnd.Equal(periodEnd))
}

func TestApplyTrialingStateAndPeriods_DraftSkipped(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusDraft,
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{SubscriptionStatus: types.SubscriptionStatusDraft}

	applyTrialingStateAndPeriods(req, sub)
	assert.Equal(t, types.SubscriptionStatusDraft, sub.SubscriptionStatus)
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

type SubscriptionTrialInvoicePaidSuite struct {
	testutil.BaseServiceTestSuite
	svc SubscriptionService
}

func TestSubscriptionTrialInvoicePaid(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialInvoicePaidSuite))
}

func (s *SubscriptionTrialInvoicePaidSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionTrialInvoicePaidSuite) TestTrialEndPaidInvoice_ActivatesAndReanchors() {
	ctx := s.GetContext()
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC) // payment time must not shift period start

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Paid Customer",
		Email:     "trial-paid@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Paid Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusTrialing,
		Currency:           "usd",
		BillingAnchor:      anchor,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          anchor,
		CurrentPeriodStart: anchor,
		CurrentPeriodEnd:   trialEnd,
		TrialStart:         &anchor,
		TrialEnd:           &trialEnd,
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive),
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	wantEnd, err := types.NextBillingDate(trialEnd, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
	s.Require().NoError(err)

	inv := &invoice.Invoice{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		SubscriptionID: &sub.ID,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PaidAt:         &paidAt,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))

	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, updated.SubscriptionStatus)
	s.True(updated.CurrentPeriodStart.Equal(trialEnd), "period start should be previous period end (trial end), not paid_at")
	s.True(updated.CurrentPeriodEnd.Equal(wantEnd))
	s.NotNil(updated.TrialStart)
	s.NotNil(updated.TrialEnd)
}

func (s *SubscriptionTrialInvoicePaidSuite) TestTrialEndPaidInvoice_IdempotentWhenAlreadyActive() {
	ctx := s.GetContext()
	anchor := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Idem Customer",
		Email:     "trial-idem@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Idem Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusTrialing,
		Currency:           "usd",
		BillingAnchor:      anchor,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          anchor,
		CurrentPeriodStart: anchor,
		CurrentPeriodEnd:   trialEnd,
		TrialStart:         &anchor,
		TrialEnd:           &trialEnd,
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive),
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	inv := &invoice.Invoice{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		SubscriptionID: &sub.ID,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PaidAt:         &paidAt,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))
	first, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))
	second, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	s.Equal(first.CurrentPeriodStart, second.CurrentPeriodStart)
	s.Equal(first.CurrentPeriodEnd, second.CurrentPeriodEnd)
	s.Equal(types.SubscriptionStatusActive, second.SubscriptionStatus)
}
