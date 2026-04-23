package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TemporalHandler manages Temporal infrastructure endpoints.
type TemporalHandler struct {
	temporalClient temporalclient.TemporalClient
	logger         *logger.Logger
}

func NewTemporalHandler(temporalClient temporalclient.TemporalClient, log *logger.Logger) *TemporalHandler {
	return &TemporalHandler{
		temporalClient: temporalClient,
		logger:         log,
	}
}

// SetupSchedules creates or updates Temporal server schedules. Optional JSON body with schedule_ids limits which are processed; omit to run all.
func (h *TemporalHandler) SetupSchedules(c *gin.Context) {
	var req dto.SetupSchedulesRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.Error(ierr.WithError(err).
				WithHint("Invalid request body").
				Mark(ierr.ErrValidation))
			return
		}
	}

	resp, err := temporalservice.EnsureSchedules(c.Request.Context(), h.temporalClient, h.logger, req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// PauseSchedule pauses a Temporal server schedule (POST /v1/temporal/schedules/:schedule_id/pause, no body).
func (h *TemporalHandler) PauseSchedule(c *gin.Context) {
	id := types.ScheduleID(c.Param("schedule_id"))
	resp, err := temporalservice.PauseSchedule(c.Request.Context(), h.temporalClient, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.logger.Infow("paused schedule", "schedule_id", id)
	c.JSON(http.StatusOK, resp)
}

// UnpauseSchedule resumes a paused Temporal server schedule (POST /v1/temporal/schedules/:schedule_id/unpause, no body).
func (h *TemporalHandler) UnpauseSchedule(c *gin.Context) {
	id := types.ScheduleID(c.Param("schedule_id"))
	resp, err := temporalservice.UnpauseSchedule(c.Request.Context(), h.temporalClient, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.logger.Infow("unpaused schedule", "schedule_id", id)
	c.JSON(http.StatusOK, resp)
}

// DeleteSchedule removes a Temporal server schedule (DELETE /v1/temporal/schedules/:schedule_id).
func (h *TemporalHandler) DeleteSchedule(c *gin.Context) {
	id := types.ScheduleID(c.Param("schedule_id"))
	resp, err := temporalservice.DeleteSchedule(c.Request.Context(), h.temporalClient, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.logger.Infow("deleted schedule", "schedule_id", id)
	c.JSON(http.StatusOK, resp)
}

// ListSchedules returns registered schedules and best-effort Temporal pause / describe state.
func (h *TemporalHandler) ListSchedules(c *gin.Context) {
	resp := temporalservice.ListSchedules(c.Request.Context(), h.temporalClient)
	c.JSON(http.StatusOK, resp)
}
