package invoice

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceActivities contains all invoice-related activities
type InvoiceActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

// NewInvoiceActivities creates a new InvoiceActivities instance
func NewInvoiceActivities(
	serviceParams service.ServiceParams,
	logger *logger.Logger,
) *InvoiceActivities {
	return &InvoiceActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// ComputeInvoiceActivity computes an invoice (line items, coupons/taxes, or SKIPPED). Returns Skipped=true if zero-dollar.
// Invoice number is NOT assigned here — it is assigned during FinalizeInvoiceActivity.
func (s *InvoiceActivities) ComputeInvoiceActivity(
	ctx context.Context,
	input invoiceModels.ComputeInvoiceActivityInput,
) (*invoiceModels.ComputeInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)
	invoiceService := service.NewInvoiceService(s.serviceParams)
	// Pass nil for subscription invoices - coupons/taxes come from billing service
	skipped, err := invoiceService.ComputeInvoice(ctx, input.InvoiceID, nil)
	if err != nil {
		s.logger.Errorw("failed to compute invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}
	s.logger.Infow("computed invoice",
		"invoice_id", input.InvoiceID,
		"skipped", skipped)
	return &invoiceModels.ComputeInvoiceActivityOutput{
		Skipped: skipped,
	}, nil
}

// FinalizeInvoiceActivity finalizes an invoice
func (s *InvoiceActivities) FinalizeInvoiceActivity(
	ctx context.Context,
	input invoiceModels.FinalizeInvoiceActivityInput,
) (*invoiceModels.FinalizeInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	// Check if finalization delay has elapsed
	due, err := invoiceService.IsFinalizationDue(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to check finalization delay",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}
	if !due {
		s.logger.Infow("finalization delay not yet elapsed, skipping",
			"invoice_id", input.InvoiceID)
		return &invoiceModels.FinalizeInvoiceActivityOutput{Success: true, Skipped: true}, nil
	}

	if err := invoiceService.FinalizeInvoice(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to finalize invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("finalized invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.FinalizeInvoiceActivityOutput{
		Success: true,
	}, nil
}

// SyncInvoiceToVendorActivity syncs an invoice to external vendors
func (s *InvoiceActivities) SyncInvoiceToVendorActivity(
	ctx context.Context,
	input invoiceModels.SyncInvoiceActivityInput,
) (*invoiceModels.SyncInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.SyncInvoiceToExternalVendors(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to sync invoice to external vendor",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("synced invoice to external vendor successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.SyncInvoiceActivityOutput{
		Success: true,
	}, nil
}

// AttemptInvoicePaymentActivity attempts to collect payment for an invoice
func (s *InvoiceActivities) AttemptInvoicePaymentActivity(
	ctx context.Context,
	input invoiceModels.PaymentActivityInput,
) (*invoiceModels.PaymentActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.AttemptPayment(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to attempt payment for invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("attempted payment for invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.PaymentActivityOutput{
		Success: true,
	}, nil
}

// RecalculateInvoiceActivity recalculates a voided subscription invoice by creating a replacement invoice (same billing period).
func (s *InvoiceActivities) RecalculateInvoiceActivity(
	ctx context.Context,
	input invoiceModels.RecalculateInvoiceActivityInput,
) (*invoiceModels.RecalculateInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invSvc := service.NewInvoiceService(s.serviceParams)

	newInv, err := invSvc.RecalculateInvoice(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to recalculate invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	outID := input.InvoiceID
	if newInv != nil {
		outID = newInv.ID
	}
	s.logger.Infow("recalculated invoice successfully",
		"invoice_id", input.InvoiceID,
		"new_invoice_id", outID)

	return &invoiceModels.RecalculateInvoiceActivityOutput{
		Success:   true,
		InvoiceID: outID,
	}, nil
}

// FinalizeDueDraftsActivity scans all draft invoices across tenants and returns those
// whose finalization delay has elapsed. The workflow then triggers ProcessInvoiceWorkflow
// for each due invoice (reusing existing Finalize → Sync → Payment activities).
func (s *InvoiceActivities) FinalizeDueDraftsActivity(
	ctx context.Context,
	input invoiceModels.FinalizeDueDraftsActivityInput,
) (*invoiceModels.FinalizeDueDraftsActivityOutput, error) {
	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	invoiceService := service.NewInvoiceService(s.serviceParams)

	output := &invoiceModels.FinalizeDueDraftsActivityOutput{
		DueInvoices: make([]invoiceModels.DueInvoice, 0),
	}

	offset := 0
	for {
		drafts, err := invoiceService.ListAllTenantDraftInvoices(ctx, batchSize, offset)
		if err != nil {
			s.logger.Errorw("failed to list draft invoices", "error", err, "offset", offset)
			return nil, err
		}
		if len(drafts) == 0 {
			break
		}

		for _, inv := range drafts {
			output.TotalProcessed++

			invCtx := types.SetTenantID(ctx, inv.TenantID)
			invCtx = types.SetEnvironmentID(invCtx, inv.EnvironmentID)

			due, err := invoiceService.IsFinalizationDue(invCtx, inv.ID)
			if err != nil {
				s.logger.Errorw("failed to check finalization due", "invoice_id", inv.ID, "error", err)
				output.FailedCount++
				continue
			}
			if !due {
				output.SkippedCount++
				continue
			}

			output.DueInvoices = append(output.DueInvoices, invoiceModels.DueInvoice{
				InvoiceID:     inv.ID,
				TenantID:      inv.TenantID,
				EnvironmentID: inv.EnvironmentID,
				UserID:        inv.CreatedBy,
			})
		}

		if len(drafts) < batchSize {
			break
		}
		offset += batchSize
	}

	s.logger.Infow("finalize due drafts scan completed",
		"total", output.TotalProcessed,
		"due", len(output.DueInvoices),
		"skipped", output.SkippedCount,
		"failed", output.FailedCount)

	return output, nil
}
