package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// MeterUsageHandler handles meter_usage query endpoints
type MeterUsageHandler struct {
	meterUsageRepo events.MeterUsageRepository
	log            *logger.Logger
}

func NewMeterUsageHandler(meterUsageRepo events.MeterUsageRepository, log *logger.Logger) *MeterUsageHandler {
	return &MeterUsageHandler{
		meterUsageRepo: meterUsageRepo,
		log:            log,
	}
}

// QueryUsage queries aggregated usage for a single meter
// @Summary Query meter usage
// @ID queryMeterUsage
// @Description Query aggregated usage from meter_usage table for a single meter with optional time-window bucketing
// @Tags MeterUsage
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.MeterUsageQueryRequest true "Query parameters"
// @Success 200 {object} dto.MeterUsageQueryResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /meter-usage/query [post]
func (h *MeterUsageHandler) QueryUsage(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.MeterUsageQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Check your request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	params := req.ToParams(tenantID, environmentID)

	result, err := h.meterUsageRepo.GetUsage(ctx, params)
	if err != nil {
		h.log.ErrorwCtx(ctx, "failed to query meter usage",
			"error", err,
			"meter_id", req.MeterID,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterUsageQueryResponse(result))
}

// GetAnalytics queries aggregated usage for multiple meters
// @Summary Get meter usage analytics
// @ID getMeterUsageAnalytics
// @Description Query aggregated usage from meter_usage table for multiple meters
// @Tags MeterUsage
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.MeterUsageAnalyticsRequest true "Analytics parameters"
// @Success 200 {object} dto.MeterUsageAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /meter-usage/analytics [post]
func (h *MeterUsageHandler) GetAnalytics(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.MeterUsageAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Check your request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	params := req.ToParams(tenantID, environmentID)

	results, err := h.meterUsageRepo.GetUsageMultiMeter(ctx, params)
	if err != nil {
		h.log.ErrorwCtx(ctx, "failed to query meter usage analytics",
			"error", err,
			"meter_ids", req.MeterIDs,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterUsageAnalyticsResponse(results))
}
