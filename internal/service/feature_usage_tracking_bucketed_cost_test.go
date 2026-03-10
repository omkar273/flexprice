package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetPointCostsFromGroupedResults verifies that per-point costs use the same
// per-group tiered pricing as TotalCost, so time-series point costs match the total.
func TestSetPointCostsFromGroupedResults(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.GetLogger()
	serviceParams := ServiceParams{
		PriceRepo:     testutil.NewInMemoryPriceStore(),
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
	}
	priceService := NewPriceService(serviceParams)
	svc := &featureUsageTrackingService{ServiceParams: serviceParams}

	// Tiered slab: 0–10 units $1/unit, 10+ $2/unit. Per-group application.
	upTo10 := uint64(10)
	tieredPrice := &price.Price{
		ID:           "price-tiered",
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(1)},
			{UpTo: nil, UnitAmount: decimal.NewFromInt(2)},
		},
	}

	bucket1 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	bucket2 := time.Date(2024, 3, 1, 1, 0, 0, 0, time.UTC)

	// Bucket1: group A=3, group B=12 → tier(3)=3, tier(12)=10*1+2*2=14 → cost 17
	// Bucket2: group A=5, group B=5 → tier(5)=5, tier(5)=5 → cost 10
	results := []events.UsageResult{
		{WindowSize: bucket1, Value: decimal.NewFromInt(3), GroupKey: "A"},
		{WindowSize: bucket1, Value: decimal.NewFromInt(12), GroupKey: "B"},
		{WindowSize: bucket2, Value: decimal.NewFromInt(5), GroupKey: "A"},
		{WindowSize: bucket2, Value: decimal.NewFromInt(5), GroupKey: "B"},
	}

	points := []events.UsageAnalyticPoint{
		{Timestamp: bucket1, WindowStart: bucket1, Usage: decimal.NewFromInt(15), MaxUsage: decimal.NewFromInt(15)},
		{Timestamp: bucket2, WindowStart: bucket2, Usage: decimal.NewFromInt(10), MaxUsage: decimal.NewFromInt(10)},
	}

	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, points, results)

	// Per-window cost: bucket1 = 17, bucket2 = 10
	expectBucket1 := decimal.NewFromInt(17)
	expectBucket2 := decimal.NewFromInt(10)
	assert.True(t, expectBucket1.Equal(points[0].Cost), "point 0 cost: expected %s, got %s", expectBucket1.String(), points[0].Cost.String())
	assert.True(t, expectBucket2.Equal(points[1].Cost), "point 1 cost: expected %s, got %s", expectBucket2.String(), points[1].Cost.String())

	// Sum of point costs should equal TotalCost from same results
	totalFromResults := priceService.CalculateCostFromUsageResults(ctx, tieredPrice, results)
	sumPointCosts := points[0].Cost.Add(points[1].Cost)
	assert.True(t, totalFromResults.Equal(sumPointCosts),
		"sum of point costs (%s) should equal TotalCost from results (%s)", sumPointCosts.String(), totalFromResults.String())
}

// TestSetPointCostsFromGroupedResults_EmptyInput is a no-op and does not panic.
func TestSetPointCostsFromGroupedResults_EmptyInput(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.GetLogger()
	serviceParams := ServiceParams{
		PriceRepo:     testutil.NewInMemoryPriceStore(),
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
	}
	priceService := NewPriceService(serviceParams)
	svc := &featureUsageTrackingService{ServiceParams: serviceParams}

	upTo10 := uint64(10)
	tieredPrice := &price.Price{
		ID:           "price-tiered",
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(1)},
			{UpTo: nil, UnitAmount: decimal.NewFromInt(2)},
		},
	}

	// Empty results
	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, nil, nil)
	points := []events.UsageAnalyticPoint{{Timestamp: time.Now()}}
	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, points, nil)
	require.True(t, points[0].Cost.Equal(decimal.Zero))

	// Empty points
	results := []events.UsageResult{{WindowSize: time.Now(), Value: decimal.NewFromInt(5)}}
	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, nil, results)
	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, []events.UsageAnalyticPoint{}, results)
}

// TestSetPointCostsFromGroupedResults_PointWithoutMatchingWindow leaves Cost unchanged (e.g. zero).
func TestSetPointCostsFromGroupedResults_PointWithoutMatchingWindow(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.GetLogger()
	serviceParams := ServiceParams{
		PriceRepo:     testutil.NewInMemoryPriceStore(),
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
	}
	priceService := NewPriceService(serviceParams)
	svc := &featureUsageTrackingService{ServiceParams: serviceParams}

	upTo10 := uint64(10)
	tieredPrice := &price.Price{
		ID:           "price-tiered",
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(1)},
			{UpTo: nil, UnitAmount: decimal.NewFromInt(2)},
		},
	}

	bucketWithUsage := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	results := []events.UsageResult{
		{WindowSize: bucketWithUsage, Value: decimal.NewFromInt(5), GroupKey: "A"},
	}
	// Point for a different bucket (no usage) – Cost should remain zero
	otherBucket := time.Date(2024, 3, 1, 2, 0, 0, 0, time.UTC)
	points := []events.UsageAnalyticPoint{
		{Timestamp: otherBucket, Cost: decimal.Zero},
	}

	svc.setPointCostsFromGroupedResults(ctx, priceService, tieredPrice, points, results)

	assert.True(t, points[0].Cost.Equal(decimal.Zero), "point with no matching window should keep zero cost")
}
