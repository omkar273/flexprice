package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type EnvironmentHandler struct {
	service         service.EnvironmentService
	temporalService temporalservice.TemporalService
	log             *logger.Logger
}

func NewEnvironmentHandler(service service.EnvironmentService, temporalService temporalservice.TemporalService, log *logger.Logger) *EnvironmentHandler {
	return &EnvironmentHandler{service: service, temporalService: temporalService, log: log}
}

func (h *EnvironmentHandler) CreateEnvironment(c *gin.Context) {
	var req dto.CreateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateEnvironment(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *EnvironmentHandler) GetEnvironment(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetEnvironment(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EnvironmentHandler) GetEnvironments(c *gin.Context) {
	var filter types.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetEnvironments(c.Request.Context(), filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EnvironmentHandler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateEnvironment(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EnvironmentHandler) CloneEnvironment(c *gin.Context) {
	sourceEnvID := c.Param("id")
	if sourceEnvID == "" {
		c.Error(ierr.NewError("source environment ID is required").
			WithHint("Please provide a valid environment ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CloneEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Create the new target environment first
	newEnv, err := h.service.CreateEnvironment(c.Request.Context(), dto.CreateEnvironmentRequest{
		Name: req.Name,
		Type: string(req.Type),
	})
	if err != nil {
		h.log.Error("failed to create target environment for clone", "error", err)
		c.Error(err)
		return
	}

	workflowRun, err := h.temporalService.ExecuteWorkflow(
		c.Request.Context(),
		types.TemporalEnvironmentCloneWorkflow,
		temporalmodels.EnvironmentCloneWorkflowInput{
			SourceEnvironmentID: sourceEnvID,
			TargetEnvironmentID: newEnv.ID,
		},
	)
	if err != nil {
		h.log.Error("failed to start environment clone workflow", "error", err, "source_env", sourceEnvID, "target_env", newEnv.ID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, &dto.CloneEnvironmentResponse{
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
		Message:    "Environment clone workflow started successfully",
	})
}
