package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionHandler handles subscription related cron jobs.
//
// Deprecated: use Temporal schedules and /v1/temporal for recurring subscription cron work.
type SubscriptionHandler struct {
	subscriptionService service.SubscriptionService
	logger              *logger.Logger
}

// NewSubscriptionHandler creates a new subscription handler.
//
// Deprecated: use Temporal and /v1/temporal instead of HTTP cron for automation.
func NewSubscriptionHandler(
	subscriptionService service.SubscriptionService,
	logger *logger.Logger,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
		logger:              logger,
	}
}

// UpdateBillingPeriods is bound to POST /v1/cron/subscriptions/update-periods.
//
// Deprecated: the same work is run by the Temporal server schedule; prefer /v1/temporal.
func (h *SubscriptionHandler) UpdateBillingPeriods(c *gin.Context) {
	ctx := c.Request.Context()
	response, err := h.subscriptionService.UpdateBillingPeriods(ctx)
	if err != nil {
		h.logger.Errorw("failed to update billing periods",
			"error", err)

		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// ProcessAutoCancellationSubscriptions processes subscriptions that are eligible for auto-cancellation
// We need to get all unpaid invoices and check if the grace period has expired.
//
// Deprecated: use the Temporal server schedule; prefer /v1/temporal over POST /v1/cron/.../process-auto-cancellation.
func (h *SubscriptionHandler) ProcessAutoCancellationSubscriptions(c *gin.Context) {
	h.logger.Infow("starting auto-cancellation processing cron job")

	if err := h.subscriptionService.ProcessAutoCancellationSubscriptions(c.Request.Context()); err != nil {
		h.logger.Errorw("failed to process auto-cancellation subscriptions",
			"error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("completed auto-cancellation processing cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}

// ProcessSubscriptionRenewalDueAlerts processes subscriptions that are due for renewal in 24 hours
// and sends webhook notifications.
//
// Deprecated: use the Temporal server schedule; prefer /v1/temporal over POST /v1/cron/.../renewal-due-alerts.
func (h *SubscriptionHandler) ProcessSubscriptionRenewalDueAlerts(c *gin.Context) {
	h.logger.Infow("starting subscription renewal due alerts cron job")

	if err := h.subscriptionService.ProcessSubscriptionRenewalDueAlert(c.Request.Context()); err != nil {
		h.logger.Errorw("failed to process subscription renewal due alerts",
			"error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("completed subscription renewal due alerts cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
