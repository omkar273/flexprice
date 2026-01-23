package internal

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

const (
	BenchTenantID      = "00000000-0000-0000-0000-000000000000"
	BenchEnvironmentID = "env_01KFD6CZM9C2QJYN1QGGJMAV9H"
	BenchUserID        = "dde9a118-f186-45a6-969b-a9dab4590a75"
	DefaultSubCount    = 10000
	PriceCount         = 100
	defaultPlanID      = "plan_01KFKYQ4N295ZBVZVNY57DW4WE"
)

func CreateLoadTestData() error {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize database client
	entClient, err := postgres.NewEntClients(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	// Create postgres client wrapper (some repos might need it, but we can use ent client wrapper for most)
	// We need the wrapper that satisfies the interface expected by New...Repository
	// Using basic ent client wrapper usually provided in internal/postgres or constructing creating simple wrapper
	// Checking onboard_tenents.go, it uses postgres.NewClient(entClient, ...) which returns a *Client
	// but strictly speaking repos take interfaces. Let's stick to what onboard_tenents.go does.
	client := postgres.NewClient(entClient, logger, nil) // nil for sentry service if not needed locally

	cache := cache.GetInMemoryCache()

	// Initialize Repositories
	_ = ent.NewMeterRepository(client, logger, cache)
	_ = ent.NewPlanRepository(client, logger, cache)
	priceRepo := ent.NewPriceRepository(client, logger, cache)
	customerRepo := ent.NewCustomerRepository(client, logger, cache)

	// Context with Tenant and Environment
	ctx = context.WithValue(ctx, types.CtxTenantID, BenchTenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, BenchEnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, BenchUserID)

	// 1. Create Meter
	// meterID, err := createMeter(ctx, meterRepo)
	// if err != nil {
	// 	return err
	// }
	// fmt.Printf("Created Meter: %s\n", meterID)
	meterID := "meter_01KFEXW53K4ZK0Z2YRN455STB0"

	// 2. Create Plan
	// planID, err := createPlan(ctx, planRepo)
	// if err != nil {
	// 	return err
	// }
	// fmt.Printf("Created Plan: %s\n", planID)
	planID := defaultPlanID

	// 3. Create Prices (800 total: 2 fixed, 798 usage)
	prices, err := createPrices(ctx, priceRepo, planID, meterID)
	if err != nil {
		return err
	}
	fmt.Printf("Created %d Prices\n", len(prices))

	// 4. Create Customer
	customerID, err := createCustomer(ctx, customerRepo)
	if err != nil {
		return err
	}
	fmt.Printf("Created Customer: %s\n", customerID)

	// 5. Create Subscriptions
	subCount := DefaultSubCount
	// Optional: Check env for override (though user said hardcode, having a small override for dev testing is safe)
	if countStr := os.Getenv("SUB_COUNT"); countStr != "" {
		fmt.Sscanf(countStr, "%d", &subCount)
	}

	fmt.Printf("Starting creation of %d subscriptions...\n", subCount)
	startTime := time.Now()

	// Get raw SQL connection for bulk inserts
	db, err := sql.Open("postgres", cfg.Postgres.GetDSN())
	if err != nil {
		return fmt.Errorf("failed to open SQL connection: %w", err)
	}
	defer db.Close()

	err = createSubscriptionsBulk(ctx, db, customerID, planID, prices, subCount)
	if err != nil {
		return err
	}

	duration := time.Since(startTime)
	fmt.Printf("Finished creating %d subscriptions in %s\n", subCount, duration)
	fmt.Printf("Rate: %.2f subs/sec\n", float64(subCount)/duration.Seconds())

	return nil
}

func createMeter(ctx context.Context, repo meter.Repository) (string, error) {
	m := meter.NewMeter("Load Test Meter", BenchTenantID, BenchUserID)
	m.EnvironmentID = BenchEnvironmentID
	m.EventName = "load_test_event"
	m.Aggregation = meter.Aggregation{
		Type: types.AggregationCount,
	}
	m.ResetUsage = types.ResetUsageBillingPeriod

	if err := repo.CreateMeter(ctx, m); err != nil {
		return "", fmt.Errorf("create meter: %w", err)
	}
	return m.ID, nil
}

func createPlan(ctx context.Context, repo plan.Repository) (string, error) {
	p := &plan.Plan{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:          "Load Test Plan",
		LookupKey:     "load_test_plan",
		Description:   "Plan for load testing",
		EnvironmentID: BenchEnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  BenchTenantID,
			Status:    types.StatusPublished,
			CreatedBy: BenchUserID,
			UpdatedBy: BenchUserID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	if err := repo.Create(ctx, p); err != nil {
		return "", fmt.Errorf("create plan: %w", err)
	}
	return p.ID, nil
}

func createPrices(ctx context.Context, repo price.Repository, planID, meterID string) ([]*price.Price, error) {
	var prices []*price.Price
	var pricesToCreate []*price.Price
	baseTime := time.Now().UTC()

	// 2 Fixed Prices
	for i := 0; i < 2; i++ {
		lookupKey := fmt.Sprintf("%s_fixed_price1_%d", planID, i+1)
		if p, err := repo.GetByLookupKey(ctx, lookupKey); err == nil {
			prices = append(prices, p)
			continue
		}

		p := &price.Price{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:             decimal.NewFromInt(10),
			Currency:           types.MustCurrency("usd"),
			DisplayAmount:      "$10",
			PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
			TransformQuantity:  price.JSONBTransformQuantity{DivideBy: 1, Round: types.ROUND_UP},
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			DisplayName:        fmt.Sprintf("Fixed Price %d", i+1),
			LookupKey:          lookupKey,
			EnvironmentID:      BenchEnvironmentID,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planID,
			BaseModel: types.BaseModel{
				TenantID:  BenchTenantID,
				Status:    types.StatusPublished,
				CreatedBy: BenchUserID,
				UpdatedBy: BenchUserID,
				CreatedAt: baseTime,
				UpdatedAt: baseTime,
			},
		}
		prices = append(prices, p)
		pricesToCreate = append(pricesToCreate, p)
	}

	// 798 Usage Prices
	for i := 0; i < PriceCount-2; i++ {
		lookupKey := fmt.Sprintf("%s_usage_price1_%d", planID, i+1)
		if p, err := repo.GetByLookupKey(ctx, lookupKey); err == nil {
			prices = append(prices, p)
			continue
		}

		p := &price.Price{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:             decimal.NewFromFloat(0.01),
			Currency:           types.MustCurrency("usd"),
			DisplayAmount:      "$0.01",
			PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
			TransformQuantity:  price.JSONBTransformQuantity{DivideBy: 1, Round: types.ROUND_UP},
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY, // Usage prices also need billing period
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING, // Usage is typically part of recurring billing cycle
			InvoiceCadence:     types.InvoiceCadenceArrear,      // Usage is billed in arrears
			DisplayName:        fmt.Sprintf("Usage Price %d", i+1),
			LookupKey:          lookupKey,
			EnvironmentID:      BenchEnvironmentID,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planID,
			MeterID:            meterID,
			BaseModel: types.BaseModel{
				TenantID:  BenchTenantID,
				Status:    types.StatusPublished,
				CreatedBy: BenchUserID,
				UpdatedBy: BenchUserID,
				CreatedAt: baseTime,
				UpdatedAt: baseTime,
			},
		}
		prices = append(prices, p)
		pricesToCreate = append(pricesToCreate, p)
	}

	if len(pricesToCreate) > 0 {
		fmt.Printf("Creating %d new prices (found %d existing)\n", len(pricesToCreate), len(prices)-len(pricesToCreate))
		// Create in chunks of 100
		chunkSize := 100

		for i := 0; i < len(pricesToCreate); i += chunkSize {
			end := i + chunkSize
			if end > len(pricesToCreate) {
				end = len(pricesToCreate)
			}
			if err := repo.CreateBulk(ctx, pricesToCreate[i:end]); err != nil {
				return nil, fmt.Errorf("create bulk prices: %w", err)
			}
		}
	} else {
		fmt.Printf("All %d prices already exist, skipping creation\n", len(prices))
	}

	return prices, nil
}

func createCustomer(ctx context.Context, repo customer.Repository) (string, error) {
	// Re-use specific ID from previous run if possible for idempotency
	existingID := "cust_01KFD78KSCG0Q3FYDVXS8RWJ5Q"
	if c, err := repo.Get(ctx, existingID); err == nil {
		fmt.Printf("Using existing Customer: %s\n", c.ID)
		return c.ID, nil
	}

	c := &customer.Customer{
		ID:            existingID, // Use fixed ID
		Name:          "Load Test Customer",
		ExternalID:    "load_test_customer_001",
		Email:         "loadtest@example.com",
		EnvironmentID: BenchEnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  BenchTenantID,
			Status:    types.StatusPublished,
			CreatedBy: BenchUserID,
			UpdatedBy: BenchUserID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	if err := repo.Create(ctx, c); err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}
	return c.ID, nil
}

func createSubscriptionsBulk(
	ctx context.Context,
	db *sql.DB,
	customerID, planID string,
	prices []*price.Price,
	count int,
) error {
	now := time.Now().UTC()

	// Generate all subscription data in memory
	fmt.Printf("Generating %d subscriptions with %d line items each...\n", count, len(prices))
	subs := make([]*subscription.Subscription, count)
	allLineItems := make([]*subscription.SubscriptionLineItem, 0, count*len(prices))

	for i := 0; i < count; i++ {
		subID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION)
		subs[i] = &subscription.Subscription{
			ID:                 subID,
			CustomerID:         customerID,
			PlanID:             planID,
			EnvironmentID:      BenchEnvironmentID,
			SubscriptionStatus: types.SubscriptionStatusActive,
			Currency:           types.MustCurrency("usd"),
			BillingCycle:       types.BillingCycleAnniversary,
			BillingAnchor:      now,
			StartDate:          now,
			CurrentPeriodStart: now,
			CurrentPeriodEnd:   now.AddDate(0, 1, 0), // 1 month
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriodCount: 1,
			Version:            1,
			ProrationBehavior:  types.ProrationBehaviorCreateProrations,
			BaseModel: types.BaseModel{
				TenantID:  BenchTenantID,
				Status:    types.StatusPublished,
				CreatedBy: BenchUserID,
				UpdatedBy: BenchUserID,
				CreatedAt: now,
				UpdatedAt: now,
			},
		}

		// Generate line items for this subscription
		for _, p := range prices {
			qty := decimal.NewFromInt(1)
			if p.Type == types.PRICE_TYPE_USAGE {
				qty = decimal.Zero
			}

			allLineItems = append(allLineItems, &subscription.SubscriptionLineItem{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID: subID,
				CustomerID:     customerID,
				EntityID:       planID,
				EntityType:     types.SubscriptionLineItemEntityTypePlan,
				PriceID:        p.ID,
				PriceType:      p.Type,
				MeterID:        p.MeterID,
				Quantity:       qty,
				Currency:       p.Currency,
				BillingPeriod:  subs[i].BillingPeriod,
				InvoiceCadence: p.InvoiceCadence,
				StartDate:      subs[i].StartDate,
				EnvironmentID:  BenchEnvironmentID,
				BaseModel: types.BaseModel{
					TenantID:  BenchTenantID,
					Status:    types.StatusPublished,
					CreatedBy: BenchUserID,
					UpdatedBy: BenchUserID,
					CreatedAt: now,
					UpdatedAt: now,
				},
			})
		}
	}

	fmt.Printf("Generated %d subscriptions and %d line items. Starting bulk insert...\n", len(subs), len(allLineItems))

	// Insert subscriptions in batches
	batchSize := 1000
	for i := 0; i < len(subs); i += batchSize {
		end := i + batchSize
		if end > len(subs) {
			end = len(subs)
		}
		batch := subs[i:end]

		if err := bulkInsertSubscriptions(ctx, db, batch); err != nil {
			return fmt.Errorf("failed to insert subscriptions batch %d-%d: %w", i, end-1, err)
		}

		if (i+batchSize)%5000 == 0 || end == len(subs) {
			fmt.Printf("Inserted %d/%d subscriptions\n", end, len(subs))
		}
	}

	// Insert line items in batches
	// Each line item has 35 fields, so we need to keep batch size under 65,535/35 = ~1,872
	// Using 1,500 to stay well under PostgreSQL's 65,535 parameter limit
	batchSize = 1500
	for i := 0; i < len(allLineItems); i += batchSize {
		end := i + batchSize
		if end > len(allLineItems) {
			end = len(allLineItems)
		}
		batch := allLineItems[i:end]

		if err := bulkInsertSubscriptionLineItems(ctx, db, batch); err != nil {
			return fmt.Errorf("failed to insert line items batch %d-%d: %w", i, end-1, err)
		}

		if (i+batchSize)%15000 == 0 || end == len(allLineItems) {
			fmt.Printf("Inserted %d/%d line items\n", end, len(allLineItems))
		}
	}

	return nil
}

func bulkInsertSubscriptions(ctx context.Context, db *sql.DB, subs []*subscription.Subscription) error {
	if len(subs) == 0 {
		return nil
	}

	// Build VALUES clause
	valueStrings := make([]string, 0, len(subs))
	valueArgs := make([]interface{}, 0, len(subs)*40) // 40 fields total

	argIndex := 1
	for _, sub := range subs {
		// Generate placeholders dynamically
		placeholders := make([]string, 40)
		for i := 0; i < 40; i++ {
			placeholders[i] = fmt.Sprintf("$%d", argIndex+i)
		}
		valueStrings = append(valueStrings, "("+strings.Join(placeholders, ", ")+")")

		// Collect all values (40 fields total)
		valueArgs = append(valueArgs,
			sub.ID,                         // 1: id
			sub.TenantID,                   // 2: tenant_id
			sub.EnvironmentID,              // 3: environment_id
			sub.CustomerID,                 // 4: customer_id
			sub.PlanID,                     // 5: plan_id
			string(sub.SubscriptionStatus), // 6: subscription_status
			sub.Currency,                   // 7: currency
			sub.BillingAnchor,              // 8: billing_anchor
			sub.StartDate,                  // 9: start_date
			nil,                            // 10: end_date
			sub.CurrentPeriodStart,         // 11: current_period_start
			sub.CurrentPeriodEnd,           // 12: current_period_end
			nil,                            // 13: cancelled_at
			nil,                            // 14: cancel_at
			false,                          // 15: cancel_at_period_end
			nil,                            // 16: trial_start
			nil,                            // 17: trial_end
			string(sub.BillingCadence),     // 18: billing_cadence
			string(sub.BillingPeriod),      // 19: billing_period
			sub.BillingPeriodCount,         // 20: billing_period_count
			sub.Version,                    // 21: version
			pq.Array([]string{}),           // 22: metadata (empty jsonb)
			string(types.PauseStatusNone),  // 23: pause_status
			nil,                            // 24: active_pause_id
			string(sub.BillingCycle),       // 25: billing_cycle
			nil,                            // 26: commitment_amount
			nil,                            // 27: overage_factor
			string(types.PaymentBehaviorDefaultActive),        // 28: payment_behavior
			string(types.CollectionMethodChargeAutomatically), // 29: collection_method
			nil,                           // 30: gateway_payment_method_id
			"UTC",                         // 31: customer_timezone
			string(sub.ProrationBehavior), // 32: proration_behavior
			false,                         // 33: enable_true_up
			nil,                           // 34: invoicing_customer_id
			nil,                           // 35: lookup_key
			string(sub.Status),            // 36: status
			sub.CreatedBy,                 // 37: created_by
			sub.UpdatedBy,                 // 38: updated_by
			sub.CreatedAt,                 // 39: created_at
			sub.UpdatedAt,                 // 40: updated_at
		)

		argIndex += 40
	}

	query := fmt.Sprintf(`
		INSERT INTO subscriptions (
			id, tenant_id, environment_id, customer_id, plan_id, subscription_status,
			currency, billing_anchor, start_date, end_date, current_period_start,
			current_period_end, cancelled_at, cancel_at, cancel_at_period_end,
			trial_start, trial_end, billing_cadence, billing_period, billing_period_count,
			version, metadata, pause_status, active_pause_id, billing_cycle,
			commitment_amount, overage_factor, payment_behavior, collection_method,
			gateway_payment_method_id, customer_timezone, proration_behavior,
			enable_true_up, invoicing_customer_id, lookup_key,
			status, created_by, updated_by, created_at, updated_at
		) VALUES %s`,
		strings.Join(valueStrings, ","))

	_, err := db.ExecContext(ctx, query, valueArgs...)
	return err
}

func bulkInsertSubscriptionLineItems(ctx context.Context, db *sql.DB, items []*subscription.SubscriptionLineItem) error {
	if len(items) == 0 {
		return nil
	}

	// Build VALUES clause
	valueStrings := make([]string, 0, len(items))
	valueArgs := make([]interface{}, 0, len(items)*35) // 35 fields total

	argIndex := 1
	for _, item := range items {
		// Generate placeholders dynamically
		placeholders := make([]string, 35)
		for i := 0; i < 35; i++ {
			placeholders[i] = fmt.Sprintf("$%d", argIndex+i)
		}
		valueStrings = append(valueStrings, "("+strings.Join(placeholders, ", ")+")")

		// Collect all values
		valueArgs = append(valueArgs,
			item.ID,             // id
			item.TenantID,       // tenant_id
			item.EnvironmentID,  // environment_id
			item.SubscriptionID, // subscription_id
			item.CustomerID,     // customer_id
			item.EntityID,       // entity_id
			func() interface{} {
				if item.EntityType != "" {
					return string(item.EntityType)
				}
				return nil
			}(), // entity_type
			nil,          // plan_display_name
			item.PriceID, // price_id
			func() interface{} {
				if item.PriceType != "" {
					return string(item.PriceType)
				}
				return nil
			}(), // price_type
			item.MeterID,               // meter_id
			nil,                        // meter_display_name
			nil,                        // price_unit_id
			nil,                        // price_unit
			nil,                        // display_name
			item.Quantity,              // quantity
			item.Currency,              // currency
			string(item.BillingPeriod), // billing_period
			func() interface{} {
				if item.InvoiceCadence != "" {
					return string(item.InvoiceCadence)
				}
				return nil
			}(), // invoice_cadence
			0,                    // trial_period
			item.StartDate,       // start_date
			nil,                  // end_date
			nil,                  // subscription_phase_id
			pq.Array([]string{}), // metadata (empty jsonb)
			nil,                  // commitment_amount
			nil,                  // commitment_quantity
			nil,                  // commitment_type
			nil,                  // commitment_overage_factor
			false,                // commitment_true_up_enabled
			false,                // commitment_windowed
		)

		// BaseModel fields
		valueArgs = append(valueArgs,
			string(item.Status), // status
			item.CreatedBy,      // created_by
			item.UpdatedBy,      // updated_by
			item.CreatedAt,      // created_at
			item.UpdatedAt,      // updated_at
		)

		argIndex += 35
	}

	query := fmt.Sprintf(`
		INSERT INTO subscription_line_items (
			id, tenant_id, environment_id, subscription_id, customer_id, entity_id,
			entity_type, plan_display_name, price_id, price_type, meter_id,
			meter_display_name, price_unit_id, price_unit, display_name,
			quantity, currency, billing_period, invoice_cadence, trial_period,
			start_date, end_date, subscription_phase_id, metadata,
			commitment_amount, commitment_quantity, commitment_type,
			commitment_overage_factor, commitment_true_up_enabled, commitment_windowed,
			status, created_by, updated_by, created_at, updated_at
		) VALUES %s`,
		strings.Join(valueStrings, ","))

	_, err := db.ExecContext(ctx, query, valueArgs...)
	return err
}
