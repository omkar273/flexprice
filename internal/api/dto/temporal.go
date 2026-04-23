package dto

import "github.com/flexprice/flexprice/internal/types"

// SetupSchedulesRequest optionally filters which schedules to set up.
// If ScheduleIDs is empty, all entries in types.AllScheduleRegistrations() are processed (idempotent).
type SetupSchedulesRequest struct {
	ScheduleIDs []types.ScheduleID `json:"schedule_ids"`
}

// SetupSchedulesResponse is the response for POST /v1/temporal/setup.
type SetupSchedulesResponse struct {
	Schedules []types.ScheduleResult `json:"schedules"`
}

// PauseScheduleResponse is the success body for POST /v1/temporal/schedules/:schedule_id/pause.
type PauseScheduleResponse struct {
	Status     string           `json:"status"`
	ScheduleID types.ScheduleID `json:"schedule_id"`
}

// UnpauseScheduleResponse is the success body for POST /v1/temporal/schedules/:schedule_id/unpause.
type UnpauseScheduleResponse struct {
	Status     string           `json:"status"`
	ScheduleID types.ScheduleID `json:"schedule_id"`
}

// DeleteScheduleResponse is the success body for DELETE /v1/temporal/schedules/:schedule_id.
type DeleteScheduleResponse struct {
	Status     string           `json:"status"`
	ScheduleID types.ScheduleID `json:"schedule_id"`
}

// ScheduleListItem is one row in GET /v1/temporal/schedules.
type ScheduleListItem struct {
	ScheduleID    types.ScheduleID `json:"schedule_id"`
	Description   string           `json:"description"`
	Paused        *bool            `json:"paused,omitempty"`
	TemporalError string           `json:"temporal_error,omitempty"`
}

// ListSchedulesResponse is the response for GET /v1/temporal/schedules.
type ListSchedulesResponse struct {
	Schedules []ScheduleListItem `json:"schedules"`
}
