package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowRefundRecovery                 = "RefundRecoveryWorkflow"
	ActivitySurfaceStaleRefunds            = "SurfaceStaleRefundsActivity"
	ActivityRetryUnprocessedRefundWebhooks = "RetryUnprocessedRefundWebhookEventsActivity"
)

// RefundRecoveryWorkflow is a cron workflow that:
//  1. Surfaces stale PENDING refunds (Activity A) — alerting only, no mutations
//  2. Retries unprocessed gateway webhook events for refunds (Activity B)
func RefundRecoveryWorkflow(ctx workflow.Context, _ struct{}) (*cronModels.RefundRecoveryWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting RefundRecoveryWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	result := &cronModels.RefundRecoveryWorkflowResult{}

	// Activity A: surface stale PENDING refunds (log/alert only)
	var surfaceResult cronModels.SurfaceStaleRefundsResult
	if err := workflow.ExecuteActivity(ctx, ActivitySurfaceStaleRefunds).Get(ctx, &surfaceResult); err != nil {
		log.Error("SurfaceStaleRefundsActivity failed", "error", err)
		return nil, err
	}
	result.SurfaceStaleResult = &surfaceResult

	// Activity B: retry unprocessed refund webhook events
	var retryResult cronModels.RetryUnprocessedWebhookEventsResult
	if err := workflow.ExecuteActivity(ctx, ActivityRetryUnprocessedRefundWebhooks).Get(ctx, &retryResult); err != nil {
		log.Error("RetryUnprocessedRefundWebhookEventsActivity failed", "error", err)
		return nil, err
	}
	result.RetryWebhooksResult = &retryResult

	log.Info("RefundRecoveryWorkflow completed successfully")
	return result, nil
}
