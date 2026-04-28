package paddle

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"

	invoicedomain "github.com/flexprice/flexprice/internal/domain/invoice"
	"go.temporal.io/sdk/temporal"
)

// InvoiceSyncActivities handles Paddle invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	invoiceRepo        invoicedomain.Repository
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new Paddle invoice sync activities handler
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	invoiceRepo invoicedomain.Repository,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		invoiceRepo:        invoiceRepo,
		logger:             logger,
	}
}

// SyncInvoiceToPaddle syncs an invoice to Paddle
func (a *InvoiceSyncActivities) SyncInvoiceToPaddle(
	ctx context.Context,
	input models.PaddleInvoiceSyncWorkflowInput,
) error {
	a.logger.Infow("syncing invoice to Paddle",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context values for tenant and environment
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get Paddle integration with runtime context
	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("Paddle connection not configured",
				"invoice_id", input.InvoiceID,
				"customer_id", input.CustomerID)
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Paddle integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Perform the sync using the existing service (customerService for EnsureCustomerSyncedToPaddle)
	syncReq := paddle.PaddleInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}

	_, err = paddleIntegration.InvoiceSyncSvc.SyncInvoiceToPaddle(ctx, syncReq, a.customerService)
	if err != nil {
		a.logger.Errorw("failed to sync invoice to Paddle",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Paddle",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}

// EnsureCustomerSyncedForInvoice checks if the customer for the given invoice is already
// synced to Paddle and syncs them if not. This is a no-op when the customer is already synced.
func (a *InvoiceSyncActivities) EnsureCustomerSyncedForInvoice(
	ctx context.Context,
	input models.PaddleInvoiceSyncWorkflowInput,
) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil { 
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		return err
	}

	flexInvoice, err := a.invoiceRepo.Get(ctx, input.InvoiceID)
	if err != nil {
		return err
	}

	a.logger.Infow("ensuring customer is synced to Paddle before invoice sync",
		"invoice_id", input.InvoiceID,
		"customer_id", flexInvoice.CustomerID)

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, flexInvoice.CustomerID, a.customerService)
	return err
}
