package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// RefundHandler handles HTTP requests for refund operations.
type RefundHandler struct {
	service service.RefundService
	log     *logger.Logger
}

// NewRefundHandler creates a new RefundHandler.
func NewRefundHandler(svc service.RefundService, log *logger.Logger) *RefundHandler {
	return &RefundHandler{service: svc, log: log}
}

// @Summary Create refund
// @ID createRefund
// @Description Creates a gateway refund for the specified payment. Calls the payment gateway (Stripe or Razorpay) to return funds to the customer's original payment method. IdempotencyKey is required to prevent duplicate refunds.
// @Tags Refunds
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Payment ID"
// @Param refund body dto.CreateRefundRequest true "Refund request"
// @Success 201 {object} dto.RefundResponse "Created refund"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or validation error"
// @Failure 404 {object} ierr.ErrorResponse "Payment not found"
// @Failure 409 {object} ierr.ErrorResponse "Idempotency key conflict"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "write"
// @Router /payments/{id}/refunds [post]
func (h *RefundHandler) CreateRefund(c *gin.Context) {
	paymentID := c.Param("id")
	if paymentID == "" {
		c.Error(ierr.NewError("payment id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CreateRefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// payment_id comes from the path, not the body
	req.PaymentID = paymentID

	if err := req.Validate(); err != nil {
		h.log.Error(c.Request.Context(), "Request validation failed", "error", err)
		c.Error(err)
		return
	}

	resp, err := h.service.CreateRefund(c.Request.Context(), &req)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to create refund", "payment_id", paymentID, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get refund
// @ID getRefund
// @Description Use when you need to load a single refund by ID (e.g. to check status or display details).
// @Tags Refunds
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Refund ID"
// @Success 200 {object} dto.RefundResponse "Refund details"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Refund not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /refunds/{id} [get]
func (h *RefundHandler) GetRefund(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Refund ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetRefund(c.Request.Context(), id)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to get refund", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List refunds
// @ID listRefunds
// @Description Use when listing or searching refunds (e.g. for a payment history or reconciliation view). Returns a paginated list; supports filtering by payment_id, status, gateway, and date range.
// @Tags Refunds
// @Produce json
// @Security ApiKeyAuth
// @Param payment_id query string false "Filter by payment ID"
// @Param status query string false "Filter by refund status (PENDING, PROCESSING, SUCCEEDED, FAILED, CANCELLED)"
// @Param gateway query string false "Filter by payment gateway"
// @Param limit query int false "Page size (default 50)"
// @Param offset query int false "Page offset"
// @Param start_time query string false "Filter by created_at >= start_time (RFC3339)"
// @Param end_time query string false "Filter by created_at <= end_time (RFC3339)"
// @Success 200 {object} dto.ListRefundsResponse "Paginated refunds"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /refunds [get]
func (h *RefundHandler) ListRefunds(c *gin.Context) {
	filter := &types.RefundFilter{
		QueryFilter:     &types.QueryFilter{},
		TimeRangeFilter: &types.TimeRangeFilter{},
	}

	if err := c.ShouldBindQuery(filter); err != nil {
		h.log.Error(c.Request.Context(), "Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.QueryFilter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	if paymentID := c.Query("payment_id"); paymentID != "" {
		filter.PaymentID = &paymentID
	}
	if status := c.Query("status"); status != "" {
		s := types.RefundStatus(status)
		filter.Status = &s
	}
	if gateway := c.Query("gateway"); gateway != "" {
		filter.Gateway = &gateway
	}

	resp, err := h.service.ListRefunds(c.Request.Context(), filter)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to list refunds", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Cancel refund
// @ID cancelRefund
// @Description Cancels a PROCESSING refund where the gateway supports it (Stripe only). Razorpay does not support refund cancellation. Only PROCESSING refunds can be cancelled — PENDING refunds are rejected (retry CreateRefund instead). The gateway is called first; local state is only updated after the gateway confirms cancellation.
// @Tags Refunds
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Refund ID"
// @Success 200 {object} dto.RefundResponse "Cancelled refund"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or refund is in PENDING state"
// @Failure 404 {object} ierr.ErrorResponse "Refund not found"
// @Failure 409 {object} ierr.ErrorResponse "Refund cannot be cancelled in its current state"
// @Failure 422 {object} ierr.ErrorResponse "Gateway does not support refund cancellation"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "delete"
// @Router /refunds/{id}/cancel [post]
func (h *RefundHandler) CancelRefund(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Refund ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CancelRefund(c.Request.Context(), id)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to cancel refund", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
