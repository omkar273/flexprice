package ent

import (
	"context"
	"encoding/json"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// SystemEventRepository persists rows into the system_events table.
type SystemEventRepository struct {
	client postgres.IClient
}

func NewSystemEventRepository(client postgres.IClient) *SystemEventRepository {
	return &SystemEventRepository{client: client}
}

// OnConsumed creates a bare system_events row the moment the consumer reads the Kafka message.
// webhook_message_id / published_at / payload are set later in OnPublished, when the webhook is actually delivered.
func (r *SystemEventRepository) OnConsumed(ctx context.Context, event *types.WebhookEvent) error {
	if event == nil || event.ID == "" {
		return nil
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

	// Idempotent — skip if the row already exists.
	if _, err := client.SystemEvent.Get(ctx, event.ID); err == nil {
		return nil
	} else if !flexent.IsNotFound(err) {
		return err
	}

	err := client.SystemEvent.Create().
		SetID(event.ID).
		SetTenantID(event.TenantID).
		SetEnvironmentID(event.EnvironmentID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		SetCreatedBy(event.UserID).
		SetUpdatedBy(event.UserID).
		Exec(ctx)
	if err != nil && flexent.IsConstraintError(err) {
		return nil // duplicate — fine
	}
	return err
}

// OnPublished fills in delivery metadata on the existing row.
// If OnConsumed was never called (race or missed message), the row is created here instead.
// webhookMessageID is the Svix msg_… id (nil for native HTTP delivery).
// outboundPayload is the fully-built webhook body sent to the subscriber.
func (r *SystemEventRepository) OnPublished(
	ctx context.Context,
	event *types.WebhookEvent,
	webhookMessageID *string,
	outboundPayload map[string]interface{},
) error {
	if event == nil || event.ID == "" {
		return nil
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

	if _, err := client.SystemEvent.Get(ctx, event.ID); flexent.IsNotFound(err) {
		// Row missing — create it with delivery info already set.
		return client.SystemEvent.Create().
			SetID(event.ID).
			SetTenantID(event.TenantID).
			SetEnvironmentID(event.EnvironmentID).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			SetCreatedBy(event.UserID).
			SetUpdatedBy(event.UserID).
			SetNillableWebhookMessageID(webhookMessageID).
			SetPublishedAt(now).
			SetPayload(outboundPayload).
			Exec(ctx)
	} else if err != nil {
		return err
	}

	u := client.SystemEvent.UpdateOneID(event.ID).
		SetUpdatedAt(now).
		SetNillableWebhookMessageID(webhookMessageID).
		SetPublishedAt(now).
		SetPayload(outboundPayload)
	if event.UserID != "" {
		u = u.SetUpdatedBy(event.UserID)
	}
	return u.Exec(ctx)
}

func toPayloadMap(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
