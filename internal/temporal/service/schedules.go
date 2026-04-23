package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	cronWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/cron"
	"github.com/flexprice/flexprice/internal/types"
	enumspb "go.temporal.io/api/enums/v1"
	sdkclient "go.temporal.io/sdk/client"
)

// AllTemporalScheduleConfigs returns the configuration for every Temporal server schedule
// (not HTTP-only cron entrypoints; see types.AllScheduleRegistrations).
func AllTemporalScheduleConfigs() []types.ScheduleConfig {
	return []types.ScheduleConfig{
		{
			ID:        types.ScheduleIDCreditGrantProcessing,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.CreditGrantProcessingWorkflow,
			Input:     models.CreditGrantProcessingWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionAutoCancellation,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionAutoCancellationWorkflow,
			Input:     models.SubscriptionAutoCancellationWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDWalletCreditExpiry,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.WalletCreditExpiryWorkflow,
			Input:     models.WalletCreditExpiryWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionBillingPeriods,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionBillingPeriodsWorkflow,
			Input:     models.SubscriptionBillingPeriodsWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionRenewalAlerts,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionRenewalDueAlertsWorkflow,
			Input:     models.SubscriptionRenewalDueAlertsWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDEventsKafkaLagMonitoring,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.EventsKafkaLagMonitoringWorkflow,
			Input:     models.EventsKafkaLagMonitoringWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
	}
}

// EnsureSchedule idempotently creates or updates a single Temporal server schedule.
func EnsureSchedule(ctx context.Context, tc client.TemporalClient, cfg types.ScheduleConfig) types.ScheduleResult {
	id := string(cfg.ID)
	handle := tc.GetScheduleHandle(ctx, id)

	spec := sdkclient.ScheduleSpec{
		Intervals: []sdkclient.ScheduleIntervalSpec{
			{Every: cfg.Interval},
		},
	}

	_, err := handle.Describe(ctx)
	if err == nil {
		updateErr := handle.Update(ctx, sdkclient.ScheduleUpdateOptions{
			DoUpdate: func(in sdkclient.ScheduleUpdateInput) (*sdkclient.ScheduleUpdate, error) {
				in.Description.Schedule.Spec = &spec
				return &sdkclient.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
			},
		})
		if updateErr != nil {
			return types.ScheduleResult{ScheduleID: cfg.ID, Status: types.TemporalServerSetupStatusError, Error: updateErr.Error()}
		}
		return types.ScheduleResult{ScheduleID: cfg.ID, Status: types.TemporalServerSetupStatusUpdated}
	}

	_, createErr := tc.CreateSchedule(ctx, models.CreateScheduleOptions{
		ID:      id,
		Spec:    spec,
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		Action: &sdkclient.ScheduleWorkflowAction{
			Workflow:  cfg.Workflow,
			TaskQueue: cfg.TaskQueue.String(),
			Args:      []interface{}{cfg.Input},
		},
	})
	if createErr != nil {
		return types.ScheduleResult{ScheduleID: cfg.ID, Status: types.TemporalServerSetupStatusError, Error: createErr.Error()}
	}
	return types.ScheduleResult{ScheduleID: cfg.ID, Status: types.TemporalServerSetupStatusCreated}
}

// EnsureSchedules idempotently creates or updates all or selected Temporal server schedules.
// When schedule_ids is non-empty, each id is validated; unknown ids return a validation error.
func EnsureSchedules(ctx context.Context, tc client.TemporalClient, log *logger.Logger, req dto.SetupSchedulesRequest) (dto.SetupSchedulesResponse, error) {
	if len(req.ScheduleIDs) > 0 {
		for _, id := range req.ScheduleIDs {
			if err := id.Validate(); err != nil {
				return dto.SetupSchedulesResponse{}, err
			}
		}
	}

	configs := AllTemporalScheduleConfigs()
	if len(req.ScheduleIDs) > 0 {
		idSet := make(map[types.ScheduleID]struct{}, len(req.ScheduleIDs))
		for _, id := range req.ScheduleIDs {
			idSet[id] = struct{}{}
		}
		filtered := configs[:0]
		for _, cfg := range configs {
			if _, ok := idSet[cfg.ID]; ok {
				filtered = append(filtered, cfg)
			}
		}
		configs = filtered
	}

	results := make([]types.ScheduleResult, 0, len(configs))
	for _, cfg := range configs {
		r := EnsureSchedule(ctx, tc, cfg)
		log.Infow("schedule setup", "id", cfg.ID, "status", r.Status, "error", r.Error)
		results = append(results, r)
	}
	return dto.SetupSchedulesResponse{Schedules: results}, nil
}

// PauseSchedule validates the schedule id and pauses the Temporal server schedule.
func PauseSchedule(ctx context.Context, tc client.TemporalClient, id types.ScheduleID) (dto.PauseScheduleResponse, error) {
	if err := id.Validate(); err != nil {
		return dto.PauseScheduleResponse{}, err
	}
	if err := tc.GetScheduleHandle(ctx, string(id)).Pause(ctx, sdkclient.SchedulePauseOptions{}); err != nil {
		return dto.PauseScheduleResponse{}, err
	}
	return dto.PauseScheduleResponse{Status: "paused", ScheduleID: id}, nil
}

// UnpauseSchedule validates the schedule id and unpauses the Temporal server schedule.
func UnpauseSchedule(ctx context.Context, tc client.TemporalClient, id types.ScheduleID) (dto.UnpauseScheduleResponse, error) {
	if err := id.Validate(); err != nil {
		return dto.UnpauseScheduleResponse{}, err
	}
	if err := tc.GetScheduleHandle(ctx, string(id)).Unpause(ctx, sdkclient.ScheduleUnpauseOptions{}); err != nil {
		return dto.UnpauseScheduleResponse{}, err
	}
	return dto.UnpauseScheduleResponse{Status: "unpaused", ScheduleID: id}, nil
}

// DeleteSchedule validates the schedule id and deletes the Temporal server schedule.
func DeleteSchedule(ctx context.Context, tc client.TemporalClient, id types.ScheduleID) (dto.DeleteScheduleResponse, error) {
	if err := id.Validate(); err != nil {
		return dto.DeleteScheduleResponse{}, err
	}
	if err := tc.GetScheduleHandle(ctx, string(id)).Delete(ctx); err != nil {
		return dto.DeleteScheduleResponse{}, err
	}
	return dto.DeleteScheduleResponse{Status: "deleted", ScheduleID: id}, nil
}

// ListSchedules returns registered schedule metadata and best-effort Temporal describe state.
func ListSchedules(ctx context.Context, tc client.TemporalClient) dto.ListSchedulesResponse {
	out := make([]dto.ScheduleListItem, 0, len(types.AllScheduleRegistrations()))
	for _, reg := range types.AllScheduleRegistrations() {
		item := dto.ScheduleListItem{ScheduleID: reg.ID, Description: reg.Description}
		desc, err := tc.GetScheduleHandle(ctx, string(reg.ID)).Describe(ctx)
		if err != nil {
			item.TemporalError = err.Error()
		} else {
			paused := desc.Schedule.State.Paused
			item.Paused = &paused
		}
		out = append(out, item)
	}
	return dto.ListSchedulesResponse{Schedules: out}
}
