package subscription

import (
	"time"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"github.com/samber/lo"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowCancelOldSandboxSubscriptions = "CancelOldSandboxSubscriptionsWorkflow"
	// Activity name - must match the registered method name
	ActivityCancelOldSandboxSubscriptions = "CancelOldSandboxSubscriptionsActivity"
)

// CancelOldSandboxSubscriptionsWorkflow cancels old sandbox subscriptions
// This workflow is triggered daily by a Temporal schedule at 00:00:00 UTC
func CancelOldSandboxSubscriptionsWorkflow(ctx workflow.Context, input subscriptionModels.CancelOldSandboxSubscriptionsWorkflowInput) (*subscriptionModels.CancelOldSandboxSubscriptionsWorkflowResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)

	// Define activity options with extended timeouts for large batch processing
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 24,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 10,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Execute the cancel old sandbox subscriptions activity
	var result subscriptionModels.CancelOldSandboxSubscriptionsWorkflowResult

	activityInput := subscriptionModels.CancelOldSandboxSubscriptionsWorkflowInput{}
	err := workflow.ExecuteActivity(ctx, ActivityCancelOldSandboxSubscriptions, activityInput).Get(ctx, &result)

	if err != nil {
		logger.Error("Cancel old sandbox subscriptions workflow failed", "error", err)
		return nil, err
	}

	return lo.ToPtr(result), nil
}
