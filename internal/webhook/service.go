package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	repoent "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
)

// WebhookService orchestrates webhook operations
type WebhookService struct {
	config          *config.Configuration
	publisher       publisher.WebhookPublisher
	handler         handler.Handler
	factory         payload.PayloadBuilderFactory
	client          httpclient.Client
	logger          *logger.Logger
	systemEventRepo *repoent.SystemEventRepository
}

// NewWebhookService creates a new webhook service
func NewWebhookService(
	cfg *config.Configuration,
	publisher publisher.WebhookPublisher,
	h handler.Handler,
	f payload.PayloadBuilderFactory,
	c httpclient.Client,
	l *logger.Logger,
	systemEventRepo *repoent.SystemEventRepository,
) *WebhookService {
	return &WebhookService{
		config:          cfg,
		publisher:       publisher,
		handler:         h,
		factory:         f,
		client:          c,
		logger:          l,
		systemEventRepo: systemEventRepo,
	}
}

// RegisterHandler registers the webhook handler with the router
func (s *WebhookService) RegisterHandler(router *pubsubRouter.Router) {
	s.handler.RegisterHandler(router)
}

// RetriggerSystemEvent loads a persisted system_events row by id (scoped to tenant and environment)
// and delivers its webhook synchronously using the same builder and transport as the Kafka consumer.
func (s *WebhookService) RetriggerSystemEvent(ctx context.Context, tenantID, environmentID, systemEventID string) error {
	if systemEventID == "" {
		return ierr.NewError("system event id is required").
			Mark(ierr.ErrValidation)
	}

	se, err := s.systemEventRepo.GetByID(ctx, tenantID, environmentID, systemEventID)
	if err != nil {
		if flexent.IsNotFound(err) {
			return ierr.NewError("system event not found").
				WithHint("Verify the id and that it belongs to the current tenant and environment.").
				Mark(ierr.ErrNotFound)
		}
		return err
	}

	ev, err := SystemEventToWebhookEvent(se)
	if err != nil {
		return err
	}

	return s.handler.DeliverWebhook(ctx, ev)
}

// Start starts the webhook service
func (s *WebhookService) Start(ctx context.Context) error {
	if !s.config.Webhook.Enabled {
		s.logger.Info("webhook service disabled")
		return nil
	}

	s.logger.Debug("starting webhook service")

	s.logger.Info("webhook service started successfully")
	return nil
}

// Stop stops the webhook service.
func (s *WebhookService) Stop() error {
	s.logger.Debug("stopping webhook service")

	// Close publisher only when using in-memory pubsub (Kafka producer is shared and closed)
	if err := s.publisher.Close(); err != nil {
		s.logger.Errorw("failed to close webhook publisher", "error", err)
		return fmt.Errorf("failed to close webhook publisher: %w", err)
	}

	s.logger.Info("webhook service stopped successfully")
	return nil
}

// SystemEventToWebhookEvent maps a persisted system_events row to the payload used by webhook delivery.
func SystemEventToWebhookEvent(se *flexent.SystemEvent) (*types.WebhookEvent, error) {
	if se == nil {
		return nil, ierr.NewError("system event is nil").
			Mark(ierr.ErrValidation)
	}

	var payload json.RawMessage
	if se.Payload != nil {
		b, err := json.Marshal(se.Payload)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Stored system event payload could not be serialized").
				Mark(ierr.ErrInternal)
		}
		payload = b
	}

	return &types.WebhookEvent{
		ID:            se.ID,
		EventName:     se.EventName,
		TenantID:      se.TenantID,
		EnvironmentID: se.EnvironmentID,
		UserID:        se.CreatedBy,
		Timestamp:     se.CreatedAt.UTC(),
		Payload:       payload,
		EntityType:    types.SystemEntityType(se.EntityType),
		EntityID:      se.EntityID,
	}, nil
}
