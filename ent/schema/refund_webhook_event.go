package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// RefundWebhookEvent is the durable inbox for inbound refund-related gateway webhooks.
// Every inbound refund webhook is persisted here immediately on receipt — before any
// matching or reconciliation logic runs — so the recovery sweep (§5b) can retry
// unprocessed events without data loss.
type RefundWebhookEvent struct {
	ent.Schema
}

// Mixin of the RefundWebhookEvent.
func (RefundWebhookEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the RefundWebhookEvent.
func (RefundWebhookEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("gateway").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("gateway_event_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable(),
		field.JSON("raw_payload", map[string]interface{}{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.Bool("processed").
			Default(false),
		field.Time("processed_at").
			Optional().
			Nillable(),
		field.String("matched_refund_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		// Incremented by the recovery sweep on each retry attempt.
		field.Int("attempts").
			Default(0),
	}
}

// Edges of the RefundWebhookEvent.
func (RefundWebhookEvent) Edges() []ent.Edge {
	return nil
}

// Indexes of the RefundWebhookEvent.
func (RefundWebhookEvent) Indexes() []ent.Index {
	return []ent.Index{
		// Dedup incoming webhooks by gateway + event ID (ON CONFLICT DO NOTHING target).
		index.Fields("gateway", "gateway_event_id").
			Unique().
			StorageKey("idx_refund_webhook_event_gateway_event"),
		// For the recovery sweep to find unprocessed events efficiently.
		index.Fields("processed", "created_at").
			StorageKey("idx_refund_webhook_event_processed"),
	}
}
