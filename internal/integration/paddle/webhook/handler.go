package webhook

import (
	"context"
	"encoding/json"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Handler handles Paddle webhook events
type Handler struct {
	paymentSvc                   *paddle.PaymentService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Paddle webhook handler
func NewHandler(
	paymentSvc *paddle.PaymentService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Paddle webhook event.
// This function never returns errors to ensure webhooks always return 200 OK.
// All errors are logged internally to prevent Paddle from retrying.
func (h *Handler) HandleWebhookEvent(ctx context.Context, eventType string, payload []byte, environmentID string, services *ServiceDependencies) error {
	h.logger.Infow("processing Paddle webhook event",
		"event_type", eventType,
		"environment_id", environmentID)

	if eventType != string(EventTransactionCompleted) {
		h.logger.Debugw("ignoring non-transaction.completed event", "type", eventType)
		return nil
	}

	var event paddlesdk.TransactionCompletedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Errorw("failed to parse transaction.completed payload",
			"error", err,
			"event_type", eventType)
		return nil
	}

	txn := &event.Data
	txnID := txn.ID

	// Lookup FlexPrice invoice via entity integration mapping
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{txnID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to find mapping for Paddle transaction",
			"error", err,
			"paddle_transaction_id", txnID)
		return nil
	}

	if len(mappings) == 0 {
		h.logger.Warnw("no FlexPrice invoice found for Paddle transaction, skipping",
			"paddle_transaction_id", txnID)
		return nil
	}

	flexpriceInvoiceID := mappings[0].EntityID

	err = h.paymentSvc.ProcessExternalPaddleTransaction(ctx, txn, flexpriceInvoiceID, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Errorw("failed to process external Paddle transaction",
			"error", err,
			"flexprice_invoice_id", flexpriceInvoiceID,
			"paddle_transaction_id", txnID)
		return nil
	}

	h.logger.Infow("successfully processed transaction.completed",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"paddle_transaction_id", txnID)

	return nil
}
