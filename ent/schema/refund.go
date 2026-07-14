package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// Refund holds the schema definition for the Refund entity.
// One row per gateway refund attempt, child of Payment.
type Refund struct {
	ent.Schema
}

// Mixin of the Refund.
func (Refund) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Refund.
func (Refund) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("payment_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("payment_gateway").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("gateway_refund_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable(),
		field.String("gateway_tracking_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable(),
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Immutable(),
		field.String("refund_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("refund_reason").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable(),
		field.String("gateway_idempotency_token").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable(),
		field.String("failure_reason").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.JSON("gateway_metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		// initiated_at anchors the resume-freshness window (decision 13).
		field.Time("initiated_at").
			Optional().
			Nillable(),
		// claimed_at is refreshed on a reclaim CAS; used by the recovery sweep
		// to detect stale PENDING claims.
		field.Time("claimed_at").
			Optional().
			Nillable(),
		field.Time("succeeded_at").
			Optional().
			Nillable(),
		field.Time("failed_at").
			Optional().
			Nillable(),
		field.Time("cancelled_at").
			Optional().
			Nillable(),
	}
}

// Edges of the Refund.
func (Refund) Edges() []ent.Edge {
	return nil
}

// Indexes of the Refund.
func (Refund) Indexes() []ent.Index {
	return []ent.Index{
		// Load-bearing for Phase 1's ON CONFLICT target (decision 12).
		// Must be scoped to (tenant_id, environment_id) — not table-wide.
		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Unique().
			StorageKey("idx_refund_tenant_env_idempotency"),
		index.Fields("tenant_id", "environment_id", "payment_id").
			StorageKey("idx_refund_tenant_payment"),
		index.Fields("tenant_id", "environment_id", "refund_status").
			StorageKey("idx_refund_tenant_status"),
		// For the recovery sweep's stale-PENDING query (§5b).
		index.Fields("refund_status", "claimed_at").
			StorageKey("idx_refund_status_claimed_at"),
		index.Fields("gateway_refund_id").
			StorageKey("idx_refund_gateway_refund_id"),
	}
}
