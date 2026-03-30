package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// getConnectionIfExists returns the connection for a provider, or nil if none is configured.
// A "not found" result is not an error — it simply means the tenant hasn't set up that provider.
// Real DB errors are still propagated.
func getConnectionIfExists(ctx context.Context, connRepo connection.Repository, provider types.SecretProvider) (*connection.Connection, error) {
	conn, err := connRepo.GetByProvider(ctx, provider)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, nil // provider not configured for this tenant — skip silently
		}
		return nil, fmt.Errorf("provider %s lookup failed: %w", provider, err)
	}
	return conn, nil
}

// invoiceVendorSyncInput holds the resolved data used by the internal provider triggers.
type invoiceVendorSyncInput struct {
	TenantID         string
	EnvironmentID    string
	UserID           string
	InvoiceID        string
	CustomerID       string
	CollectionMethod string
}

// DispatchInvoiceVendorSync resolves the invoice from the event payload, loads its
// subscription (for collection method), then starts Temporal sync workflows for each
// enabled provider.  It is the single entry point for both the integration consumer
// (invoice.update.finalized) and the manual SyncInvoiceToExternalVendors path.
func DispatchInvoiceVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	invoiceRepo invoice.Repository,
	subRepo subscription.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	// Parse invoice ID from the event payload.
	var pl struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.InvoiceID == "" {
		log.Errorw("integration_events: invalid invoice payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	// Load the invoice.
	inv, err := invoiceRepo.Get(ctx, pl.InvoiceID)
	if err != nil {
		log.Errorw("integration_events: failed to load invoice for sync dispatch",
			"invoice_id", pl.InvoiceID,
			"error", err,
		)
		return err
	}

	// Load the subscription to get the collection method (best-effort).
	collectionMethod := ""
	if inv.SubscriptionID != nil && subRepo != nil {
		sub, err := subRepo.Get(ctx, *inv.SubscriptionID)
		if err != nil {
			log.Warnw("integration_events: failed to get subscription for collection method",
				"invoice_id", inv.ID,
				"subscription_id", *inv.SubscriptionID,
				"error", err)
		} else if sub != nil {
			collectionMethod = sub.CollectionMethod
		}
	}

	in := invoiceVendorSyncInput{
		TenantID:         event.TenantID,
		EnvironmentID:    event.EnvironmentID,
		UserID:           event.UserID,
		InvoiceID:        inv.ID,
		CustomerID:       inv.CustomerID,
		CollectionMethod: collectionMethod,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	log.Infow("integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"customer_id", in.CustomerID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	var dispatchErrs []error
	for _, trigger := range []func() error{
		func() error { return triggerStripeIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerRazorpayIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerChargebeeIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerQuickBooksIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerHubSpotIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerMoyasarIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerNomodIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerPaddleIfEnabled(ctx, connRepo, temporalSvc, log, in) },
	} {
		if err := trigger(); err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	if len(dispatchErrs) > 0 {
		return fmt.Errorf("integration_events: one or more provider dispatches failed for invoice %s: %w", in.InvoiceID, errors.Join(dispatchErrs...))
	}

	return nil
}

func executeWorkflow(
	ctx context.Context,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	workflowType types.TemporalWorkflowType,
	input interface{},
	provider types.SecretProvider,
	invoiceID string,
) error {
	workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, workflowType, input)
	if err != nil {
		log.Errorw("integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"invoice_id", invoiceID,
			"error", err,
		)
		return fmt.Errorf("provider %s workflow start failed: %w", provider, err)
	}

	log.Infow("integration_events: workflow started",
		"provider", provider,
		"workflow_type", workflowType,
		"invoice_id", invoiceID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID(),
	)
	return nil
}

func triggerStripeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderStripe)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.StripeInvoiceSyncWorkflowInput{
		InvoiceID:        in.InvoiceID,
		CustomerID:       in.CustomerID,
		CollectionMethod: in.CollectionMethod,
		TenantID:         in.TenantID,
		EnvironmentID:    in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalStripeInvoiceSyncWorkflow, input, types.SecretProviderStripe, in.InvoiceID)
}

func triggerRazorpayIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderRazorpay)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.RazorpayInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalRazorpayInvoiceSyncWorkflow, input, types.SecretProviderRazorpay, in.InvoiceID)
}

func triggerChargebeeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderChargebee)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.ChargebeeInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalChargebeeInvoiceSyncWorkflow, input, types.SecretProviderChargebee, in.InvoiceID)
}

func triggerQuickBooksIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderQuickBooks)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.QuickBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalQuickBooksInvoiceSyncWorkflow, input, types.SecretProviderQuickBooks, in.InvoiceID)
}

func triggerHubSpotIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderHubSpot)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.HubSpotInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalHubSpotInvoiceSyncWorkflow, input, types.SecretProviderHubSpot, in.InvoiceID)
}

func triggerMoyasarIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderMoyasar)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.MoyasarInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalMoyasarInvoiceSyncWorkflow, input, types.SecretProviderMoyasar, in.InvoiceID)
}

func triggerNomodIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderNomod)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.NomodInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalNomodInvoiceSyncWorkflow, input, types.SecretProviderNomod, in.InvoiceID)
}

func triggerPaddleIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderPaddle)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalPaddleInvoiceSyncWorkflow, input, types.SecretProviderPaddle, in.InvoiceID)
}

var errTemporalUnavailable = fmt.Errorf("integration_events: temporal service not available")
