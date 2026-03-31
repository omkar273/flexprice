package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	domainInvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	temporalModels "github.com/flexprice/flexprice/internal/temporal/models"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	temporalWorker "github.com/flexprice/flexprice/internal/temporal/worker"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type setupDraftInvoicesParams struct {
	TenantID      string
	EnvironmentID string
	FilePath      string
	BatchSize     int
	DryRun        bool
}

// SetupDraftInvoices reads subscription IDs from a JSON file, creates one empty draft invoice
// per subscription for the current billing window (subscription CurrentPeriodStart → CurrentPeriodEnd),
// bulk-inserts them, and fires ComputeInvoiceWorkflow for each created invoice.
func SetupDraftInvoices() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	filePath := os.Getenv("FILE_PATH")
	batchSizeStr := os.Getenv("BATCH_SIZE")
	dryRunStr := os.Getenv("DRY_RUN")

	if tenantID == "" || environmentID == "" || filePath == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID, and FILE_PATH are required")
	}

	batchSize := 500
	if batchSizeStr != "" {
		if n, err := strconv.Atoi(batchSizeStr); err == nil && n > 0 {
			batchSize = n
		}
	}

	dryRun := false
	if strings.EqualFold(dryRunStr, "true") {
		dryRun = true
	}

	params := setupDraftInvoicesParams{
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		FilePath:      filePath,
		BatchSize:     batchSize,
		DryRun:        dryRun,
	}

	return setupDraftInvoices(params)
}

func setupDraftInvoices(params setupDraftInvoicesParams) error {
	log.Printf("Starting draft invoice setup: tenant=%s, env=%s, file=%s, batch_size=%d, dry_run=%v\n",
		params.TenantID, params.EnvironmentID, params.FilePath, params.BatchSize, params.DryRun)

	// 1. Read subscription IDs from JSON file
	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", params.FilePath, err)
	}

	var subscriptionIDs []string
	if err := json.Unmarshal(data, &subscriptionIDs); err != nil {
		return fmt.Errorf("failed to parse JSON file (expected string array): %w", err)
	}

	if len(subscriptionIDs) == 0 {
		return fmt.Errorf("no subscription IDs found in file")
	}

	log.Printf("Loaded %d subscription IDs from file\n", len(subscriptionIDs))

	// 2. Initialize infrastructure
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	appLogger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	sentryService := sentry.NewSentryService(cfg, appLogger)

	entClient, err := postgres.NewEntClients(cfg, appLogger)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	client := postgres.NewClient(entClient, appLogger, sentry.NewSentryService(cfg, appLogger))
	cacheClient := cache.NewInMemoryCache()

	subscriptionRepo := entRepo.NewSubscriptionRepository(client, appLogger, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, appLogger, cacheClient)

	// 3. Initialize Temporal client (for firing workflows)
	if !params.DryRun {
		temporalOpts := &temporalModels.ClientOptions{
			Address:   cfg.Temporal.Address,
			Namespace: cfg.Temporal.Namespace,
			APIKey:    cfg.Temporal.APIKey,
			TLS:       cfg.Temporal.TLS,
		}
		tc, err := temporalClient.NewTemporalClient(temporalOpts, appLogger)
		if err != nil {
			return fmt.Errorf("failed to create temporal client: %w", err)
		}

		ctx := context.Background()
		if err := tc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start temporal client: %w", err)
		}
		defer tc.Stop(ctx)

		wm := temporalWorker.NewTemporalWorkerManager(tc, appLogger)
		temporalService.InitializeGlobalTemporalService(tc, wm, appLogger, sentryService)
	}

	// 4. Set up context with tenant/environment
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, params.TenantID)
	ctx = types.SetEnvironmentID(ctx, params.EnvironmentID)
	ctx = types.SetUserID(ctx, "system")

	// 5. Process subscriptions: one draft per subscription for the current period
	idempGen := idempotency.NewGenerator()

	var allInvoices []*domainInvoice.Invoice
	totalSubsProcessed := 0
	totalSubsSkipped := 0
	totalSubsErrored := 0

	for i, subID := range subscriptionIDs {
		log.Printf("[%d/%d] Processing subscription %s\n", i+1, len(subscriptionIDs), subID)

		sub, _, err := subscriptionRepo.GetWithLineItems(ctx, subID)
		if err != nil {
			log.Printf("  ERROR: failed to get subscription %s: %v\n", subID, err)
			totalSubsErrored++
			continue
		}

		periodStart := sub.CurrentPeriodStart
		periodEnd := sub.CurrentPeriodEnd
		if periodStart.IsZero() || periodEnd.IsZero() {
			log.Printf("  SKIP: missing current period bounds for %s\n", subID)
			totalSubsSkipped++
			continue
		}
		if periodEnd.Before(periodStart) {
			log.Printf("  SKIP: invalid current period for %s (end before start)\n", subID)
			totalSubsSkipped++
			continue
		}

		invoicingCustomerID := sub.GetInvoicingCustomerID()
		billingPeriodStr := string(sub.BillingPeriod)

		idempStart := periodStart.Truncate(time.Minute)
		idempEnd := periodEnd.Truncate(time.Minute)

		idempKey := idempGen.GenerateKey(idempotency.ScopeSubscriptionInvoice, map[string]interface{}{
			"tenant_id":       params.TenantID,
			"environment_id":  params.EnvironmentID,
			"customer_id":     invoicingCustomerID,
			"subscription_id": sub.ID,
			"period_start":    &idempStart,
			"period_end":      &idempEnd,
		})

		inv := &domainInvoice.Invoice{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			CustomerID:      invoicingCustomerID,
			SubscriptionID:  lo.ToPtr(sub.ID),
			InvoiceType:     types.InvoiceTypeSubscription,
			InvoiceStatus:   types.InvoiceStatusDraft,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        strings.ToLower(sub.Currency),
			AmountDue:       decimal.Zero,
			AmountPaid:      decimal.Zero,
			AmountRemaining: decimal.Zero,
			Total:           decimal.Zero,
			Subtotal:        decimal.Zero,
			TotalDiscount:   decimal.Zero,
			TotalTax:        decimal.Zero,
			BillingPeriod:   &billingPeriodStr,
			PeriodStart:     &periodStart,
			PeriodEnd:       &periodEnd,
			BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
			IdempotencyKey:  &idempKey,
			BaseModel:       types.GetDefaultBaseModel(ctx),
			EnvironmentID:   params.EnvironmentID,
		}

		allInvoices = append(allInvoices, inv)
		totalSubsProcessed++
		log.Printf("  OK: %s → current-period draft prepared (%s – %s)\n", subID, periodStart, periodEnd)
	}

	log.Printf("\n=== Subscription Summary ===\n")
	log.Printf("Total in file:  %d\n", len(subscriptionIDs))
	log.Printf("Processed:      %d\n", totalSubsProcessed)
	log.Printf("Skipped:        %d\n", totalSubsSkipped)
	log.Printf("Errored:        %d\n", totalSubsErrored)
	log.Printf("Invoices built: %d\n", len(allInvoices))

	if params.DryRun {
		log.Println("\n[DRY RUN] No invoices created and no workflows triggered.")
		return nil
	}

	if len(allInvoices) == 0 {
		log.Println("No invoices to create.")
		return nil
	}

	// 6. Bulk insert invoices into DB in batches
	log.Printf("\nBulk inserting %d invoices (batch size %d)...\n", len(allInvoices), params.BatchSize)

	for i := 0; i < len(allInvoices); i += params.BatchSize {
		end := i + params.BatchSize
		if end > len(allInvoices) {
			end = len(allInvoices)
		}
		batch := allInvoices[i:end]

		if err := invoiceRepo.CreateBulk(ctx, batch); err != nil {
			log.Printf("  ERROR: bulk insert batch [%d:%d] failed: %v\n", i, end, err)
			log.Printf("  Falling back to one-by-one insert for this batch...\n")
			for _, inv := range batch {
				if err := invoiceRepo.Create(ctx, inv); err != nil {
					log.Printf("    SKIP (duplicate or error) invoice %s: %v\n", inv.ID, err)
				}
			}
		} else {
			log.Printf("  Inserted batch [%d:%d] (%d invoices)\n", i, end, len(batch))
		}
	}

	// 7. Collect invoice IDs and fire ProcessInvoiceWorkflow
	invoiceIDs := make([]string, len(allInvoices))
	for i, inv := range allInvoices {
		invoiceIDs[i] = inv.ID
	}

	log.Printf("\nTriggering ProcessInvoiceWorkflow for %d invoices...\n", len(invoiceIDs))

	temporalSvc := temporalService.GetGlobalTemporalService()
	if temporalSvc == nil {
		return fmt.Errorf("temporal service is not initialized")
	}

	totalTriggered := 0
	totalFailed := 0

	for i := 0; i < len(invoiceIDs); i += params.BatchSize {
		end := i + params.BatchSize
		if end > len(invoiceIDs) {
			end = len(invoiceIDs)
		}

		for _, invoiceID := range invoiceIDs[i:end] {
			_, err := temporalSvc.ExecuteWorkflow(
				ctx,
				types.TemporalComputeInvoiceWorkflow,
				invoiceModels.ComputeInvoiceWorkflowInput{
					InvoiceID:     invoiceID,
					TenantID:      params.TenantID,
					EnvironmentID: params.EnvironmentID,
				},
			)
			if err != nil {
				log.Printf("  WARN: failed to trigger workflow for invoice %s: %v\n", invoiceID, err)
				totalFailed++
			} else {
				totalTriggered++
			}
		}

		// Small sleep between batches for rate limiting
		if end < len(invoiceIDs) {
			time.Sleep(100 * time.Millisecond)
		}

		log.Printf("  Triggered batch [%d:%d] — running total: %d triggered, %d failed\n",
			i, end, totalTriggered, totalFailed)
	}

	log.Printf("\n=== Final Summary ===\n")
	log.Printf("Invoices created:       %d\n", len(allInvoices))
	log.Printf("Workflows triggered:    %d\n", totalTriggered)
	log.Printf("Workflows failed:       %d\n", totalFailed)

	return nil
}
