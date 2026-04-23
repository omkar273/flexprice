package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityMonitorKafkaLag = "MonitorKafkaLagActivity"
)

// EventsKafkaLagMonitoringWorkflow monitors Kafka consumer lag; same as POST /v1/cron/events/monitoring.
func EventsKafkaLagMonitoringWorkflow(ctx workflow.Context, _ cronModels.EventsKafkaLagMonitoringWorkflowInput) (*cronModels.EventsKafkaLagMonitoringWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting EventsKafkaLagMonitoringWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.EventsKafkaLagMonitoringWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityMonitorKafkaLag).Get(ctx, &result); err != nil {
		log.Error("EventsKafkaLagMonitoringWorkflow activity failed", "error", err)
		return nil, err
	}
	return &result, nil
}
