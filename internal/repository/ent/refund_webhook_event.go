package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entRWE "github.com/flexprice/flexprice/ent/refundwebhookevent"
	domainRefund "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type refundWebhookEventRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewRefundWebhookEventRepository creates a new Ent-backed refund webhook event repository.
func NewRefundWebhookEventRepository(client postgres.IClient, log *logger.Logger) domainRefund.WebhookEventRepository {
	return &refundWebhookEventRepository{
		client: client,
		log:    log,
	}
}

// Create inserts a new webhook event using INSERT … ON CONFLICT (gateway, gateway_event_id) DO NOTHING.
// Silently succeeds if the event was already recorded (idempotent).
func (r *refundWebhookEventRepository) Create(ctx context.Context, event *domainRefund.RefundWebhookEvent) error {
	span := StartRepositorySpan(ctx, "refund_webhook_event", "create", map[string]interface{}{
		"event_id":         event.ID,
		"gateway":          event.Gateway,
		"gateway_event_id": event.GatewayEventID,
	})
	defer FinishSpan(span)

	if event.EnvironmentID == "" {
		event.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if event.TenantID == "" {
		event.TenantID = types.GetTenantID(ctx)
	}

	now := time.Now().UTC()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	if event.UpdatedAt.IsZero() {
		event.UpdatedAt = now
	}
	if event.Status == "" {
		event.Status = types.StatusPublished
	}

	const rawSQL = `
INSERT INTO refund_webhook_events (
  id, tenant_id, environment_id, status, gateway, gateway_event_id,
  raw_payload, processed, attempts, created_at, updated_at, created_by, updated_by
)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (gateway, gateway_event_id) DO NOTHING`

	_, err := r.client.Writer(ctx).ExecContext(ctx, rawSQL,
		event.ID,
		event.TenantID,
		event.EnvironmentID,
		string(event.Status),
		event.Gateway,
		event.GatewayEventID,
		event.RawPayload,
		event.Processed,
		event.Attempts,
		event.CreatedAt,
		event.UpdatedAt,
		event.CreatedBy,
		event.UpdatedBy,
	)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to insert refund webhook event").
			WithReportableDetails(map[string]interface{}{
				"event_id":         event.ID,
				"gateway":          event.Gateway,
				"gateway_event_id": event.GatewayEventID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// GetByGatewayEventID looks up an event by its composite (gateway, gateway_event_id) key.
func (r *refundWebhookEventRepository) GetByGatewayEventID(ctx context.Context, gateway, gatewayEventID string) (*domainRefund.RefundWebhookEvent, error) {
	span := StartRepositorySpan(ctx, "refund_webhook_event", "get_by_gateway_event_id", map[string]interface{}{
		"gateway":          gateway,
		"gateway_event_id": gatewayEventID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	event, err := client.RefundWebhookEvent.Query().
		Where(
			entRWE.Gateway(gateway),
			entRWE.GatewayEventID(gatewayEventID),
			entRWE.TenantID(types.GetTenantID(ctx)),
			entRWE.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund webhook event not found").
				WithReportableDetails(map[string]interface{}{
					"gateway":          gateway,
					"gateway_event_id": gatewayEventID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get refund webhook event").
			WithReportableDetails(map[string]interface{}{
				"gateway":          gateway,
				"gateway_event_id": gatewayEventID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEntWebhookEvent(event), nil
}

// MarkProcessed marks the event as processed and records the matched refund ID.
func (r *refundWebhookEventRepository) MarkProcessed(ctx context.Context, id string, refundID *string) error {
	span := StartRepositorySpan(ctx, "refund_webhook_event", "mark_processed", map[string]interface{}{
		"event_id":  id,
		"refund_id": refundID,
	})
	defer FinishSpan(span)

	now := time.Now().UTC()

	update := r.client.Writer(ctx).RefundWebhookEvent.Update().
		Where(
			entRWE.ID(id),
			entRWE.TenantID(types.GetTenantID(ctx)),
			entRWE.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetProcessed(true).
		SetProcessedAt(now).
		SetUpdatedAt(now)

	if refundID != nil {
		update = update.SetMatchedRefundID(*refundID)
	}

	_, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Refund webhook event not found").
				WithReportableDetails(map[string]interface{}{"event_id": id}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to mark refund webhook event as processed").
			WithReportableDetails(map[string]interface{}{"event_id": id}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// ListUnprocessed returns unprocessed events whose created_at is older than minAge.
func (r *refundWebhookEventRepository) ListUnprocessed(ctx context.Context, minAge time.Duration) ([]*domainRefund.RefundWebhookEvent, error) {
	span := StartRepositorySpan(ctx, "refund_webhook_event", "list_unprocessed", map[string]interface{}{
		"min_age": minAge.String(),
	})
	defer FinishSpan(span)

	threshold := time.Now().UTC().Add(-minAge)
	client := r.client.Reader(ctx)

	events, err := client.RefundWebhookEvent.Query().
		Where(
			entRWE.TenantID(types.GetTenantID(ctx)),
			entRWE.EnvironmentID(types.GetEnvironmentID(ctx)),
			entRWE.Processed(false),
			entRWE.CreatedAtLTE(threshold),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list unprocessed refund webhook events").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEntWebhookEventList(events), nil
}

// IncrementAttempts bumps the attempts counter on the given event.
func (r *refundWebhookEventRepository) IncrementAttempts(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "refund_webhook_event", "increment_attempts", map[string]interface{}{
		"event_id": id,
	})
	defer FinishSpan(span)

	const rawSQL = `
UPDATE refund_webhook_events
SET attempts = attempts + 1, updated_at = now()
WHERE id = $1 AND tenant_id = $2 AND environment_id = $3`

	result, err := r.client.Writer(ctx).ExecContext(ctx, rawSQL,
		id,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
	)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to increment attempts on refund webhook event").
			WithReportableDetails(map[string]interface{}{"event_id": id}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to get rows affected for refund webhook event attempts increment").
			Mark(ierr.ErrDatabase)
	}
	if rowsAffected == 0 {
		return ierr.NewError("refund webhook event not found").
			WithHint("Refund webhook event not found when incrementing attempts").
			WithReportableDetails(map[string]interface{}{"event_id": id}).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	return nil
}
