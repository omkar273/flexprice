package invoice

import (
	"fmt"
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowScheduleDraftFinalization = "ScheduleDraftFinalizationWorkflow"
	ActivityFinalizeDueDrafts         = "FinalizeDueDraftsActivity"
)

// ScheduleDraftFinalizationWorkflow scans all draft invoices system-wide, identifies those
// whose finalization delay has elapsed, and triggers FinalizeDraftInvoiceWorkflow for each.
// FinalizeDraftInvoiceWorkflow skips Compute (already done) and runs: Finalize → Sync → Payment.
func ScheduleDraftFinalizationWorkflow(
	ctx workflow.Context,
	input invoiceModels.ScheduleDraftFinalizationWorkflowInput,
) (*invoiceModels.ScheduleDraftFinalizationWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)

	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// Step 1: Scan for due draft invoices
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var scanOutput invoiceModels.FinalizeDueDraftsActivityOutput
	err := workflow.ExecuteActivity(ctx, ActivityFinalizeDueDrafts, invoiceModels.FinalizeDueDraftsActivityInput{
		BatchSize: batchSize,
	}).Get(ctx, &scanOutput)
	if err != nil {
		logger.Error("FinalizeDueDraftsActivity failed", "error", err)
		return nil, err
	}

	logger.Info("Draft invoice scan completed",
		"total_scanned", scanOutput.TotalProcessed,
		"due_count", len(scanOutput.DueInvoices),
		"skipped", scanOutput.SkippedCount)

	if len(scanOutput.DueInvoices) == 0 {
		return &invoiceModels.ScheduleDraftFinalizationWorkflowResult{
			TotalProcessed: scanOutput.TotalProcessed,
			SkippedCount:   scanOutput.SkippedCount,
		}, nil
	}

	// Step 2: Trigger FinalizeDraftInvoiceWorkflow for each due invoice (fire-and-forget child workflows).
	// Uses FinalizeDraftInvoiceWorkflow (not ProcessInvoiceWorkflow) because the invoice is already
	// computed — we only need Finalize → Sync → Payment, skipping the Compute step to avoid
	// re-calculating usage after the billing period closed.
	var triggeredCount, failedCount int
	for _, inv := range scanOutput.DueInvoices {
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:               fmt.Sprintf("finalize-draft-invoice-%s", inv.InvoiceID),
			TaskQueue:                string("invoice"),
			WorkflowExecutionTimeout: 10 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
			ParentClosePolicy: 1, // ABANDON — child continues even if parent completes
		})

		future := workflow.ExecuteChildWorkflow(childCtx, FinalizeDraftInvoiceWorkflow, invoiceModels.ProcessInvoiceWorkflowInput{
			InvoiceID:     inv.InvoiceID,
			TenantID:      inv.TenantID,
			EnvironmentID: inv.EnvironmentID,
			UserID:        inv.UserID,
		})

		// Fire-and-forget: just check if it started successfully
		if err := future.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
			logger.Error("Failed to trigger FinalizeDraftInvoiceWorkflow",
				"invoice_id", inv.InvoiceID, "error", err)
			failedCount++
			continue
		}
		triggeredCount++
	}

	logger.Info("ScheduleDraftFinalizationWorkflow completed",
		"total_scanned", scanOutput.TotalProcessed,
		"triggered", triggeredCount,
		"skipped", scanOutput.SkippedCount,
		"failed", failedCount)

	return &invoiceModels.ScheduleDraftFinalizationWorkflowResult{
		TotalProcessed: scanOutput.TotalProcessed,
		FinalizedCount: triggeredCount,
		SkippedCount:   scanOutput.SkippedCount,
		FailedCount:    failedCount,
	}, nil
}
