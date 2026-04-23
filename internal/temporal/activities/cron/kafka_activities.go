package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

// KafkaLagMonitoringActivities runs the same work as POST /v1/cron/events/monitoring.
type KafkaLagMonitoringActivities struct {
	eventService service.EventService
	logger       *logger.Logger
}

// NewKafkaLagMonitoringActivities creates activities for Kafka lag monitoring.
func NewKafkaLagMonitoringActivities(eventService service.EventService, log *logger.Logger) *KafkaLagMonitoringActivities {
	return &KafkaLagMonitoringActivities{eventService: eventService, logger: log}
}

// MonitorKafkaLagActivity monitors consumer lag (cron activity).
func (a *KafkaLagMonitoringActivities) MonitorKafkaLagActivity(ctx context.Context) (*cronModels.EventsKafkaLagMonitoringWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Kafka lag monitoring (cron activity)")
	if err := a.eventService.MonitorKafkaLag(ctx); err != nil {
		return nil, err
	}
	return &cronModels.EventsKafkaLagMonitoringWorkflowResult{}, nil
}
