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
// published_at / webhook_message_id / payload are filled in later by OnPublished.
func (r *SystemEventRepository) OnConsumed(ctx context.Context, event *types.WebhookEvent) error {
	if event == nil || event.ID == "" {
		return nil
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

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

// OnPublished fills in delivery metadata once the webhook has actually been sent.
// webhookMessageID is the Svix msg_… id; nil for native HTTP delivery.
// Saves the internal Kafka envelope payload (event.Payload), not the outbound body.
func (r *SystemEventRepository) OnPublished(
	ctx context.Context,
	event *types.WebhookEvent,
	webhookMessageID *string,
) error {
	if event == nil || event.ID == "" {
		return nil
	}

	// Ensure the row exists — delegates to OnConsumed to avoid duplicate create logic.
	// OnConsumed is idempotent so calling it here is safe even when the row already exists.
	if err := r.OnConsumed(ctx, event); err != nil {
		return err
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

	payloadMap, err := toPayloadMap(event.Payload)
	if err != nil {
		return err
	}

	u := client.SystemEvent.UpdateOneID(event.ID).
		SetUpdatedAt(now).
		SetNillableWebhookMessageID(webhookMessageID).
		SetPublishedAt(now).
		SetPayload(payloadMap)
	if event.UserID != "" {
		u = u.SetUpdatedBy(event.UserID)
	}
	return u.Exec(ctx)
}

func toPayloadMap(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
