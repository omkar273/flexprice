package events

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceVendorSyncInput is the minimal data needed to fan out invoice sync workflows.
type InvoiceVendorSyncInput struct {
	TenantID           string
	EnvironmentID      string
	UserID             string
	InvoiceID          string
	CustomerID         string
	CollectionMethod   string
}

// DispatchInvoiceVendorSync starts Temporal invoice-sync workflows for each enabled provider.
// Used by the integration consumer (on invoice.update.finalized) and by manual SyncInvoiceToExternalVendors.
func DispatchInvoiceVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	if in.InvoiceID == "" || in.CustomerID == "" {
		return nil
	}

	log.Infow("integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"customer_id", in.CustomerID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	triggerStripeIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerRazorpayIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerChargebeeIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerQuickBooksIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerHubSpotIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerMoyasarIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerNomodIfEnabled(ctx, connRepo, temporalSvc, log, in)
	triggerPaddleIfEnabled(ctx, connRepo, temporalSvc, log, in)

	return nil
}

func executeWorkflow(
	ctx context.Context,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	workflowType types.TemporalWorkflowType,
	input interface{},
	provider string,
	invoiceID string,
) {
	workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, workflowType, input)
	if err != nil {
		log.Errorw("integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"invoice_id", invoiceID,
			"error", err,
		)
		return
	}

	log.Infow("integration_events: workflow started",
		"provider", provider,
		"workflow_type", workflowType,
		"invoice_id", invoiceID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID(),
	)
}

func triggerStripeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.StripeInvoiceSyncWorkflowInput{
		InvoiceID:        in.InvoiceID,
		CustomerID:       in.CustomerID,
		CollectionMethod: in.CollectionMethod,
		TenantID:         in.TenantID,
		EnvironmentID:    in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalStripeInvoiceSyncWorkflow, input, "stripe", in.InvoiceID)
}

func triggerRazorpayIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.RazorpayInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalRazorpayInvoiceSyncWorkflow, input, "razorpay", in.InvoiceID)
}

func triggerChargebeeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.ChargebeeInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalChargebeeInvoiceSyncWorkflow, input, "chargebee", in.InvoiceID)
}

func triggerQuickBooksIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.QuickBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalQuickBooksInvoiceSyncWorkflow, input, "quickbooks", in.InvoiceID)
}

func triggerHubSpotIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderHubSpot)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.HubSpotInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalHubSpotInvoiceSyncWorkflow, input, "hubspot", in.InvoiceID)
}

func triggerMoyasarIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderMoyasar)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.MoyasarInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalMoyasarInvoiceSyncWorkflow, input, "moyasar", in.InvoiceID)
}

func triggerNomodIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderNomod)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.NomodInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalNomodInvoiceSyncWorkflow, input, "nomod", in.InvoiceID)
}

func triggerPaddleIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil || conn == nil {
		return
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return
	}

	input := &temporalmodels.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	executeWorkflow(ctx, temporalSvc, log, types.TemporalPaddleInvoiceSyncWorkflow, input, "paddle", in.InvoiceID)
}

var errTemporalUnavailable = fmt.Errorf("integration_events: temporal service not available")
