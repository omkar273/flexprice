package paddle

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type CustomerSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

func NewCustomerSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	logger *logger.Logger,
) *CustomerSyncActivities {
	return &CustomerSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		logger:             logger,
	}
}

// SyncCustomerToPaddle is called from PaddleCustomerSyncWorkflow (triggered on customer creation).
// Errors are not wrapped as NonRetryableApplicationError; Temporal will retry transient failures.
func (a *CustomerSyncActivities) SyncCustomerToPaddle(ctx context.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		// Let Temporal retry transient integration lookup failures.
		return err
	}

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, input.CustomerID, a.customerService)
	return err
}

// EnsureCustomerSyncedToPaddle is called from PaddleInvoiceSyncWorkflow as an explicit pre-check
// step before invoice sync. It ensures the customer exists in Paddle (creating them if needed).
//
// Validation errors (e.g. missing email, missing address country) are returned as
// NonRetryableApplicationError so the workflow fails immediately with a clear message rather than
// burning through retry attempts on a problem that retrying cannot fix.
func (a *CustomerSyncActivities) EnsureCustomerSyncedToPaddle(ctx context.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	a.logger.Infow("ensuring customer synced to Paddle before invoice sync",
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Warnw("Paddle connection not configured, skipping customer pre-check",
				"customer_id", input.CustomerID)
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Paddle integration for customer pre-check",
			"error", err,
			"customer_id", input.CustomerID)
		return err
	}

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, input.CustomerID, a.customerService)
	if err != nil {
		if ierr.IsValidation(err) {
			a.logger.Warnw("customer cannot be synced to Paddle: validation error (non-retryable)",
				"customer_id", input.CustomerID,
				"error", err)
			return temporal.NewNonRetryableApplicationError(
				err.Error(),
				"CustomerValidationError",
				err,
			)
		}
		a.logger.Errorw("failed to ensure customer synced to Paddle",
			"error", err,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Infow("customer successfully synced to Paddle",
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)
	return nil
}
