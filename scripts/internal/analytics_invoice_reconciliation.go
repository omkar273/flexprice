package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	dateLayout                   = "2006-01-02"
	defaultReconciliationWorkers = 10
	envReconciliationWorkers     = "RECONCILIATION_WORKERS"
)

// AnalyticsInvoiceDiffRow represents one row in the reconciliation diff CSV
type AnalyticsInvoiceDiffRow struct {
	CustomerID         string
	CustomerName       string
	ExternalCustomerID string
	AnalyticsTotalCost string
	InvoiceSubtotalSum string
	Diff               string
	InvoiceIDs         string
	InvoiceCount       int
	Currency           string
}

type analyticsInvoiceReconciliationScript struct {
	log                         *logger.Logger
	customerRepo                customer.Repository
	invoiceRepo                 invoice.Repository
	featureUsageTrackingService service.FeatureUsageTrackingService
}

// RunAnalyticsInvoiceReconciliation compares analytics total cost to invoice subtotals for a period and writes diffs to CSV.
// Period is hardcoded to 1 Feb – 1 Mar. Requires TENANT_ID and ENVIRONMENT_ID.
// Infrastructure (Postgres, ClickHouse, Kafka) must be running.
//
// Invoice side uses stored subtotals only: values come from invoiceRepo.List() (inv.Subtotal).
// Recalculated totals from GetInvoiceWithBreakdown()/recalculateInvoiceTotals() are not used,
// so the report may show divergence where DB-stored subtotals differ from in-memory recalculated totals.
func RunAnalyticsInvoiceReconciliation() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	startTime := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)

	log.Printf("Reconciling analytics vs invoices for period %s to %s (tenant=%s, env=%s)",
		startTime.Format(dateLayout), endTime.Format(dateLayout), tenantID, environmentID)

	script, err := newAnalyticsInvoiceReconciliationScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// 1) List all customers (published)
	customerFilter := types.NewNoLimitCustomerFilter()
	customerFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	customers, err := script.customerRepo.ListAll(ctx, customerFilter)
	if err != nil {
		return fmt.Errorf("list customers: %w", err)
	}

	log.Printf("Found %d customers", len(customers))

	// Build list of customers to process (same tenant/env, with external_customer_id)
	var toProcess []*customer.Customer
	for _, c := range customers {
		if c.TenantID != tenantID || c.EnvironmentID != environmentID {
			continue
		}
		if c.ExternalID == "" {
			continue
		}
		toProcess = append(toProcess, c)
	}
	workers := getReconciliationWorkers()
	log.Printf("Processing %d customers with %d parallel workers, appending diff rows to CSV as each completes", len(toProcess), workers)

	// 2) List invoices created in period, sum subtotal by customer (needed before we iterate customers)
	invFilter := types.NewNoLimitInvoiceFilter()
	invFilter.CreatedAtGTE = &startTime
	invFilter.CreatedAtLTE = &endTime
	invFilter.SkipLineItems = true

	invoices, err := script.invoiceRepo.List(ctx, invFilter)
	if err != nil {
		return fmt.Errorf("list invoices: %w", err)
	}

	// Sum stored subtotals by customer (DB values only; not recalculated via GetInvoiceWithBreakdown/recalculateInvoiceTotals)
	storedInvoiceSubtotalByCustomer := make(map[string]decimal.Decimal)
	invoiceIDsByCustomer := make(map[string][]string)
	for _, inv := range invoices {
		if inv.TenantID != tenantID || inv.EnvironmentID != environmentID {
			continue
		}
		storedInvoiceSubtotalByCustomer[inv.CustomerID] = storedInvoiceSubtotalByCustomer[inv.CustomerID].Add(inv.Subtotal)
		invoiceIDsByCustomer[inv.CustomerID] = append(invoiceIDsByCustomer[inv.CustomerID], inv.ID)
	}
	log.Printf("Summed stored invoice subtotals for %d invoices (DB-stored values only; not runtime-recalculated)", len(invoices))

	// 3) Open CSV and run N workers: each result appends a row if diff exceeds tolerance
	outputFile := fmt.Sprintf("analytics_invoice_reconciliation_%s_%s.csv", tenantID, time.Now().Format("20060102_150405"))
	csvFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("create CSV: %w", err)
	}
	defer csvFile.Close()
	csvWriter := csv.NewWriter(csvFile)
	header := []string{
		"customer_id", "customer_name", "external_customer_id",
		"analytics_total_cost", "invoice_subtotal_sum", "diff",
		"invoice_ids", "invoice_count", "currency",
	}
	if err := csvWriter.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush CSV header: %w", err)
	}

	rowsWritten, completed := runReconciliationWorkers(
		ctx, script, toProcess, startTime, endTime, workers,
		storedInvoiceSubtotalByCustomer, invoiceIDsByCustomer,
		csvWriter,
	)
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	if rowsWritten == 0 {
		log.Printf("No differences found; wrote CSV with header only to %s (completed %d/%d customers)", outputFile, completed, len(toProcess))
	} else {
		log.Printf("Wrote %d diff rows to %s (completed %d/%d customers)", rowsWritten, outputFile, completed, len(toProcess))
	}
	return nil
}

func getReconciliationWorkers() int {
	s := os.Getenv(envReconciliationWorkers)
	if s == "" {
		return defaultReconciliationWorkers
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultReconciliationWorkers
	}
	return n
}

type analyticsResult struct {
	customer  *customer.Customer
	totalCost decimal.Decimal
	err       error
}

// runReconciliationWorkers runs GetDetailedUsageAnalytics with a worker pool; as each result
// arrives, if analytics total and invoice subtotal don't match, a row is appended to the CSV.
// Returns rowsWritten and completed count.
func runReconciliationWorkers(
	ctx context.Context,
	script *analyticsInvoiceReconciliationScript,
	customers []*customer.Customer,
	startTime, endTime time.Time,
	workers int,
	storedInvoiceSubtotalByCustomer map[string]decimal.Decimal,
	invoiceIDsByCustomer map[string][]string,
	csvWriter *csv.Writer,
) (rowsWritten, completed int) {
	if len(customers) == 0 {
		return 0, 0
	}
	if workers > len(customers) {
		workers = len(customers)
	}

	jobCh := make(chan *customer.Customer, len(customers))
	resultCh := make(chan analyticsResult, workers*2)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobCh {
				req := &dto.GetUsageAnalyticsRequest{
					ExternalCustomerID: c.ExternalID,
					StartTime:          startTime,
					EndTime:            endTime,
				}
				resp, err := script.featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, req)
				if err != nil {
					resultCh <- analyticsResult{customer: c, err: err}
					continue
				}
				resultCh <- analyticsResult{customer: c, totalCost: resp.TotalCost}
			}
		}()
	}

	go func() {
		for _, c := range customers {
			jobCh <- c
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		completed++
		if r.err != nil {
			log.Printf("Warning: analytics for customer %s: %v", r.customer.ID, r.err)
			if completed%100 == 0 || completed == len(customers) {
				log.Printf("Progress: %d/%d customers (%d diff rows so far)", completed, len(customers), rowsWritten)
			}
			continue
		}
		c := r.customer
		analyticsCost := r.totalCost
		storedSubtotal := storedInvoiceSubtotalByCustomer[c.ID]
		if analyticsCost.Equal(storedSubtotal) {
			if completed%100 == 0 || completed == len(customers) {
				log.Printf("Progress: %d/%d customers (%d diff rows so far)", completed, len(customers), rowsWritten)
			}
			continue
		}
		diff := analyticsCost.Sub(storedSubtotal)
		customerName := c.Name
		if customerName == "" {
			customerName = c.ExternalID
		}
		if customerName == "" {
			customerName = c.ID
		}
		ids := invoiceIDsByCustomer[c.ID]
		idList := ""
		for _, id := range ids {
			if idList != "" {
				idList += ";"
			}
			idList += id
		}
		record := []string{
			c.ID, customerName, c.ExternalID,
			analyticsCost.StringFixed(4), storedSubtotal.StringFixed(4), diff.StringFixed(4),
			idList, fmt.Sprintf("%d", len(ids)), "USD",
		}
		if err := csvWriter.Write(record); err != nil {
			log.Printf("Error writing CSV row for customer %s: %v", c.ID, err)
			continue
		}
		rowsWritten++
		if rowsWritten%50 == 0 {
			csvWriter.Flush()
		}
		if completed%100 == 0 || completed == len(customers) {
			log.Printf("Progress: %d/%d customers (%d diff rows so far)", completed, len(customers), rowsWritten)
		}
	}
	return rowsWritten, completed
}

func newAnalyticsInvoiceReconciliationScript() (*analyticsInvoiceReconciliationScript, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}
	sentrySvc := sentry.NewSentryService(cfg, logger)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentrySvc)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	entClient, err := postgres.NewEntClients(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	pgClient := postgres.NewClient(entClient, logger, sentrySvc)
	cacheClient := cache.NewInMemoryCache()

	customerRepo := entRepo.NewCustomerRepository(pgClient, logger, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(pgClient, logger, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(pgClient, logger, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, logger, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, logger, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, logger, cacheClient)
	subRepo := entRepo.NewSubscriptionRepository(pgClient, logger, cacheClient)
	subLineItemRepo := entRepo.NewSubscriptionLineItemRepository(pgClient, logger, cacheClient)
	addonRepo := entRepo.NewAddonRepository(pgClient, logger, cacheClient)
	settingsRepo := entRepo.NewSettingsRepository(pgClient, logger, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, logger)
	featureUsageRepo := chRepo.NewFeatureUsageRepository(chStore, logger)

	serviceParams := service.ServiceParams{
		Logger:                   logger,
		Config:                   cfg,
		DB:                       pgClient,
		CustomerRepo:             customerRepo,
		InvoiceRepo:              invoiceRepo,
		FeatureRepo:              featureRepo,
		MeterRepo:                meterRepo,
		PriceRepo:                priceRepo,
		PlanRepo:                 planRepo,
		SubRepo:                  subRepo,
		SubscriptionLineItemRepo: subLineItemRepo,
		AddonRepo:                addonRepo,
		SettingsRepo:             settingsRepo,
		EventRepo:                eventRepo,
		FeatureUsageRepo:         featureUsageRepo,
	}
	featureUsageTrackingService := service.NewFeatureUsageTrackingService(serviceParams, eventRepo, featureUsageRepo)

	return &analyticsInvoiceReconciliationScript{
		log:                         logger,
		customerRepo:                customerRepo,
		invoiceRepo:                 invoiceRepo,
		featureUsageTrackingService: featureUsageTrackingService,
	}, nil
}
