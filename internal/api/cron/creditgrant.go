package cron

import (
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// CreditGrantCronHandler is the HTTP entrypoint for scheduled credit grant application runs.
//
// Deprecated: use Temporal schedules and /v1/temporal.
type CreditGrantCronHandler struct {
	creditGrantService service.CreditGrantService
	logger             *logger.Logger
}

// NewCreditGrantCronHandler creates a CreditGrantCronHandler.
//
// Deprecated: use Temporal and /v1/temporal.
func NewCreditGrantCronHandler(creditGrantService service.CreditGrantService, log *logger.Logger) *CreditGrantCronHandler {
	return &CreditGrantCronHandler{
		creditGrantService: creditGrantService,
		logger:             log,
	}
}

// ProcessScheduledCreditGrantApplications is bound to POST /v1/cron/creditgrants/process-scheduled-applications.
//
// Deprecated: use the Temporal server schedule; prefer /v1/temporal.
func (h *CreditGrantCronHandler) ProcessScheduledCreditGrantApplications(c *gin.Context) {
	h.logger.Infow("starting credit grant scheduled applications cron job - %s", time.Now().UTC().Format(time.RFC3339))

	resp, err := h.creditGrantService.ProcessScheduledCreditGrantApplications(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
