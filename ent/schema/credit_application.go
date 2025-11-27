package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// CreditApplication holds the schema definition for the CreditApplication entity.
type CreditApplication struct {
	ent.Schema
}

// Mixin of the CreditApplication.
func (CreditApplication) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CreditApplication.
func (CreditApplication) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Comment("ID of the entity this credit is applied to (invoice, invoice_line_item, etc.)"),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Comment("Type of entity: INVOICE, INVOICE_LINE_ITEM, TRANSACTION, WALLET, etc."),
		field.Other("credits_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Comment("Amount of credits applied"),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Comment("Currency of the credit application"),
		field.String("wallet_transaction_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("Wallet transaction ID that was debited for this credit application"),
	}
}

// Edges of the CreditApplication.
func (CreditApplication) Edges() []ent.Edge {
	return nil
}

// Indexes of the CreditApplication.
func (CreditApplication) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
	}
}
