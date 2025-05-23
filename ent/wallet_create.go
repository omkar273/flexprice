// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// WalletCreate is the builder for creating a Wallet entity.
type WalletCreate struct {
	config
	mutation *WalletMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (wc *WalletCreate) SetTenantID(s string) *WalletCreate {
	wc.mutation.SetTenantID(s)
	return wc
}

// SetStatus sets the "status" field.
func (wc *WalletCreate) SetStatus(s string) *WalletCreate {
	wc.mutation.SetStatus(s)
	return wc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (wc *WalletCreate) SetNillableStatus(s *string) *WalletCreate {
	if s != nil {
		wc.SetStatus(*s)
	}
	return wc
}

// SetCreatedAt sets the "created_at" field.
func (wc *WalletCreate) SetCreatedAt(t time.Time) *WalletCreate {
	wc.mutation.SetCreatedAt(t)
	return wc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (wc *WalletCreate) SetNillableCreatedAt(t *time.Time) *WalletCreate {
	if t != nil {
		wc.SetCreatedAt(*t)
	}
	return wc
}

// SetUpdatedAt sets the "updated_at" field.
func (wc *WalletCreate) SetUpdatedAt(t time.Time) *WalletCreate {
	wc.mutation.SetUpdatedAt(t)
	return wc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (wc *WalletCreate) SetNillableUpdatedAt(t *time.Time) *WalletCreate {
	if t != nil {
		wc.SetUpdatedAt(*t)
	}
	return wc
}

// SetCreatedBy sets the "created_by" field.
func (wc *WalletCreate) SetCreatedBy(s string) *WalletCreate {
	wc.mutation.SetCreatedBy(s)
	return wc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (wc *WalletCreate) SetNillableCreatedBy(s *string) *WalletCreate {
	if s != nil {
		wc.SetCreatedBy(*s)
	}
	return wc
}

// SetUpdatedBy sets the "updated_by" field.
func (wc *WalletCreate) SetUpdatedBy(s string) *WalletCreate {
	wc.mutation.SetUpdatedBy(s)
	return wc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (wc *WalletCreate) SetNillableUpdatedBy(s *string) *WalletCreate {
	if s != nil {
		wc.SetUpdatedBy(*s)
	}
	return wc
}

// SetEnvironmentID sets the "environment_id" field.
func (wc *WalletCreate) SetEnvironmentID(s string) *WalletCreate {
	wc.mutation.SetEnvironmentID(s)
	return wc
}

// SetNillableEnvironmentID sets the "environment_id" field if the given value is not nil.
func (wc *WalletCreate) SetNillableEnvironmentID(s *string) *WalletCreate {
	if s != nil {
		wc.SetEnvironmentID(*s)
	}
	return wc
}

// SetName sets the "name" field.
func (wc *WalletCreate) SetName(s string) *WalletCreate {
	wc.mutation.SetName(s)
	return wc
}

// SetNillableName sets the "name" field if the given value is not nil.
func (wc *WalletCreate) SetNillableName(s *string) *WalletCreate {
	if s != nil {
		wc.SetName(*s)
	}
	return wc
}

// SetCustomerID sets the "customer_id" field.
func (wc *WalletCreate) SetCustomerID(s string) *WalletCreate {
	wc.mutation.SetCustomerID(s)
	return wc
}

// SetCurrency sets the "currency" field.
func (wc *WalletCreate) SetCurrency(s string) *WalletCreate {
	wc.mutation.SetCurrency(s)
	return wc
}

// SetDescription sets the "description" field.
func (wc *WalletCreate) SetDescription(s string) *WalletCreate {
	wc.mutation.SetDescription(s)
	return wc
}

// SetNillableDescription sets the "description" field if the given value is not nil.
func (wc *WalletCreate) SetNillableDescription(s *string) *WalletCreate {
	if s != nil {
		wc.SetDescription(*s)
	}
	return wc
}

// SetMetadata sets the "metadata" field.
func (wc *WalletCreate) SetMetadata(m map[string]string) *WalletCreate {
	wc.mutation.SetMetadata(m)
	return wc
}

// SetBalance sets the "balance" field.
func (wc *WalletCreate) SetBalance(d decimal.Decimal) *WalletCreate {
	wc.mutation.SetBalance(d)
	return wc
}

// SetNillableBalance sets the "balance" field if the given value is not nil.
func (wc *WalletCreate) SetNillableBalance(d *decimal.Decimal) *WalletCreate {
	if d != nil {
		wc.SetBalance(*d)
	}
	return wc
}

// SetCreditBalance sets the "credit_balance" field.
func (wc *WalletCreate) SetCreditBalance(d decimal.Decimal) *WalletCreate {
	wc.mutation.SetCreditBalance(d)
	return wc
}

// SetWalletStatus sets the "wallet_status" field.
func (wc *WalletCreate) SetWalletStatus(s string) *WalletCreate {
	wc.mutation.SetWalletStatus(s)
	return wc
}

// SetNillableWalletStatus sets the "wallet_status" field if the given value is not nil.
func (wc *WalletCreate) SetNillableWalletStatus(s *string) *WalletCreate {
	if s != nil {
		wc.SetWalletStatus(*s)
	}
	return wc
}

// SetAutoTopupTrigger sets the "auto_topup_trigger" field.
func (wc *WalletCreate) SetAutoTopupTrigger(s string) *WalletCreate {
	wc.mutation.SetAutoTopupTrigger(s)
	return wc
}

// SetNillableAutoTopupTrigger sets the "auto_topup_trigger" field if the given value is not nil.
func (wc *WalletCreate) SetNillableAutoTopupTrigger(s *string) *WalletCreate {
	if s != nil {
		wc.SetAutoTopupTrigger(*s)
	}
	return wc
}

// SetAutoTopupMinBalance sets the "auto_topup_min_balance" field.
func (wc *WalletCreate) SetAutoTopupMinBalance(d decimal.Decimal) *WalletCreate {
	wc.mutation.SetAutoTopupMinBalance(d)
	return wc
}

// SetNillableAutoTopupMinBalance sets the "auto_topup_min_balance" field if the given value is not nil.
func (wc *WalletCreate) SetNillableAutoTopupMinBalance(d *decimal.Decimal) *WalletCreate {
	if d != nil {
		wc.SetAutoTopupMinBalance(*d)
	}
	return wc
}

// SetAutoTopupAmount sets the "auto_topup_amount" field.
func (wc *WalletCreate) SetAutoTopupAmount(d decimal.Decimal) *WalletCreate {
	wc.mutation.SetAutoTopupAmount(d)
	return wc
}

// SetNillableAutoTopupAmount sets the "auto_topup_amount" field if the given value is not nil.
func (wc *WalletCreate) SetNillableAutoTopupAmount(d *decimal.Decimal) *WalletCreate {
	if d != nil {
		wc.SetAutoTopupAmount(*d)
	}
	return wc
}

// SetWalletType sets the "wallet_type" field.
func (wc *WalletCreate) SetWalletType(s string) *WalletCreate {
	wc.mutation.SetWalletType(s)
	return wc
}

// SetNillableWalletType sets the "wallet_type" field if the given value is not nil.
func (wc *WalletCreate) SetNillableWalletType(s *string) *WalletCreate {
	if s != nil {
		wc.SetWalletType(*s)
	}
	return wc
}

// SetConversionRate sets the "conversion_rate" field.
func (wc *WalletCreate) SetConversionRate(d decimal.Decimal) *WalletCreate {
	wc.mutation.SetConversionRate(d)
	return wc
}

// SetConfig sets the "config" field.
func (wc *WalletCreate) SetConfig(tc types.WalletConfig) *WalletCreate {
	wc.mutation.SetConfig(tc)
	return wc
}

// SetNillableConfig sets the "config" field if the given value is not nil.
func (wc *WalletCreate) SetNillableConfig(tc *types.WalletConfig) *WalletCreate {
	if tc != nil {
		wc.SetConfig(*tc)
	}
	return wc
}

// SetID sets the "id" field.
func (wc *WalletCreate) SetID(s string) *WalletCreate {
	wc.mutation.SetID(s)
	return wc
}

// Mutation returns the WalletMutation object of the builder.
func (wc *WalletCreate) Mutation() *WalletMutation {
	return wc.mutation
}

// Save creates the Wallet in the database.
func (wc *WalletCreate) Save(ctx context.Context) (*Wallet, error) {
	wc.defaults()
	return withHooks(ctx, wc.sqlSave, wc.mutation, wc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (wc *WalletCreate) SaveX(ctx context.Context) *Wallet {
	v, err := wc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (wc *WalletCreate) Exec(ctx context.Context) error {
	_, err := wc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (wc *WalletCreate) ExecX(ctx context.Context) {
	if err := wc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (wc *WalletCreate) defaults() {
	if _, ok := wc.mutation.Status(); !ok {
		v := wallet.DefaultStatus
		wc.mutation.SetStatus(v)
	}
	if _, ok := wc.mutation.CreatedAt(); !ok {
		v := wallet.DefaultCreatedAt()
		wc.mutation.SetCreatedAt(v)
	}
	if _, ok := wc.mutation.UpdatedAt(); !ok {
		v := wallet.DefaultUpdatedAt()
		wc.mutation.SetUpdatedAt(v)
	}
	if _, ok := wc.mutation.EnvironmentID(); !ok {
		v := wallet.DefaultEnvironmentID
		wc.mutation.SetEnvironmentID(v)
	}
	if _, ok := wc.mutation.Balance(); !ok {
		v := wallet.DefaultBalance
		wc.mutation.SetBalance(v)
	}
	if _, ok := wc.mutation.WalletStatus(); !ok {
		v := wallet.DefaultWalletStatus
		wc.mutation.SetWalletStatus(v)
	}
	if _, ok := wc.mutation.AutoTopupTrigger(); !ok {
		v := wallet.DefaultAutoTopupTrigger
		wc.mutation.SetAutoTopupTrigger(v)
	}
	if _, ok := wc.mutation.WalletType(); !ok {
		v := wallet.DefaultWalletType
		wc.mutation.SetWalletType(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (wc *WalletCreate) check() error {
	if _, ok := wc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "Wallet.tenant_id"`)}
	}
	if v, ok := wc.mutation.TenantID(); ok {
		if err := wallet.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "Wallet.tenant_id": %w`, err)}
		}
	}
	if _, ok := wc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "Wallet.status"`)}
	}
	if _, ok := wc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "Wallet.created_at"`)}
	}
	if _, ok := wc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "Wallet.updated_at"`)}
	}
	if _, ok := wc.mutation.CustomerID(); !ok {
		return &ValidationError{Name: "customer_id", err: errors.New(`ent: missing required field "Wallet.customer_id"`)}
	}
	if v, ok := wc.mutation.CustomerID(); ok {
		if err := wallet.CustomerIDValidator(v); err != nil {
			return &ValidationError{Name: "customer_id", err: fmt.Errorf(`ent: validator failed for field "Wallet.customer_id": %w`, err)}
		}
	}
	if _, ok := wc.mutation.Currency(); !ok {
		return &ValidationError{Name: "currency", err: errors.New(`ent: missing required field "Wallet.currency"`)}
	}
	if v, ok := wc.mutation.Currency(); ok {
		if err := wallet.CurrencyValidator(v); err != nil {
			return &ValidationError{Name: "currency", err: fmt.Errorf(`ent: validator failed for field "Wallet.currency": %w`, err)}
		}
	}
	if _, ok := wc.mutation.Balance(); !ok {
		return &ValidationError{Name: "balance", err: errors.New(`ent: missing required field "Wallet.balance"`)}
	}
	if _, ok := wc.mutation.CreditBalance(); !ok {
		return &ValidationError{Name: "credit_balance", err: errors.New(`ent: missing required field "Wallet.credit_balance"`)}
	}
	if _, ok := wc.mutation.WalletStatus(); !ok {
		return &ValidationError{Name: "wallet_status", err: errors.New(`ent: missing required field "Wallet.wallet_status"`)}
	}
	if _, ok := wc.mutation.WalletType(); !ok {
		return &ValidationError{Name: "wallet_type", err: errors.New(`ent: missing required field "Wallet.wallet_type"`)}
	}
	if _, ok := wc.mutation.ConversionRate(); !ok {
		return &ValidationError{Name: "conversion_rate", err: errors.New(`ent: missing required field "Wallet.conversion_rate"`)}
	}
	if v, ok := wc.mutation.Config(); ok {
		if err := v.Validate(); err != nil {
			return &ValidationError{Name: "config", err: fmt.Errorf(`ent: validator failed for field "Wallet.config": %w`, err)}
		}
	}
	return nil
}

func (wc *WalletCreate) sqlSave(ctx context.Context) (*Wallet, error) {
	if err := wc.check(); err != nil {
		return nil, err
	}
	_node, _spec := wc.createSpec()
	if err := sqlgraph.CreateNode(ctx, wc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected Wallet.ID type: %T", _spec.ID.Value)
		}
	}
	wc.mutation.id = &_node.ID
	wc.mutation.done = true
	return _node, nil
}

func (wc *WalletCreate) createSpec() (*Wallet, *sqlgraph.CreateSpec) {
	var (
		_node = &Wallet{config: wc.config}
		_spec = sqlgraph.NewCreateSpec(wallet.Table, sqlgraph.NewFieldSpec(wallet.FieldID, field.TypeString))
	)
	if id, ok := wc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := wc.mutation.TenantID(); ok {
		_spec.SetField(wallet.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := wc.mutation.Status(); ok {
		_spec.SetField(wallet.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := wc.mutation.CreatedAt(); ok {
		_spec.SetField(wallet.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := wc.mutation.UpdatedAt(); ok {
		_spec.SetField(wallet.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := wc.mutation.CreatedBy(); ok {
		_spec.SetField(wallet.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := wc.mutation.UpdatedBy(); ok {
		_spec.SetField(wallet.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := wc.mutation.EnvironmentID(); ok {
		_spec.SetField(wallet.FieldEnvironmentID, field.TypeString, value)
		_node.EnvironmentID = value
	}
	if value, ok := wc.mutation.Name(); ok {
		_spec.SetField(wallet.FieldName, field.TypeString, value)
		_node.Name = value
	}
	if value, ok := wc.mutation.CustomerID(); ok {
		_spec.SetField(wallet.FieldCustomerID, field.TypeString, value)
		_node.CustomerID = value
	}
	if value, ok := wc.mutation.Currency(); ok {
		_spec.SetField(wallet.FieldCurrency, field.TypeString, value)
		_node.Currency = value
	}
	if value, ok := wc.mutation.Description(); ok {
		_spec.SetField(wallet.FieldDescription, field.TypeString, value)
		_node.Description = value
	}
	if value, ok := wc.mutation.Metadata(); ok {
		_spec.SetField(wallet.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if value, ok := wc.mutation.Balance(); ok {
		_spec.SetField(wallet.FieldBalance, field.TypeOther, value)
		_node.Balance = value
	}
	if value, ok := wc.mutation.CreditBalance(); ok {
		_spec.SetField(wallet.FieldCreditBalance, field.TypeOther, value)
		_node.CreditBalance = value
	}
	if value, ok := wc.mutation.WalletStatus(); ok {
		_spec.SetField(wallet.FieldWalletStatus, field.TypeString, value)
		_node.WalletStatus = value
	}
	if value, ok := wc.mutation.AutoTopupTrigger(); ok {
		_spec.SetField(wallet.FieldAutoTopupTrigger, field.TypeString, value)
		_node.AutoTopupTrigger = &value
	}
	if value, ok := wc.mutation.AutoTopupMinBalance(); ok {
		_spec.SetField(wallet.FieldAutoTopupMinBalance, field.TypeOther, value)
		_node.AutoTopupMinBalance = &value
	}
	if value, ok := wc.mutation.AutoTopupAmount(); ok {
		_spec.SetField(wallet.FieldAutoTopupAmount, field.TypeOther, value)
		_node.AutoTopupAmount = &value
	}
	if value, ok := wc.mutation.WalletType(); ok {
		_spec.SetField(wallet.FieldWalletType, field.TypeString, value)
		_node.WalletType = value
	}
	if value, ok := wc.mutation.ConversionRate(); ok {
		_spec.SetField(wallet.FieldConversionRate, field.TypeOther, value)
		_node.ConversionRate = value
	}
	if value, ok := wc.mutation.Config(); ok {
		_spec.SetField(wallet.FieldConfig, field.TypeJSON, value)
		_node.Config = value
	}
	return _node, _spec
}

// WalletCreateBulk is the builder for creating many Wallet entities in bulk.
type WalletCreateBulk struct {
	config
	err      error
	builders []*WalletCreate
}

// Save creates the Wallet entities in the database.
func (wcb *WalletCreateBulk) Save(ctx context.Context) ([]*Wallet, error) {
	if wcb.err != nil {
		return nil, wcb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(wcb.builders))
	nodes := make([]*Wallet, len(wcb.builders))
	mutators := make([]Mutator, len(wcb.builders))
	for i := range wcb.builders {
		func(i int, root context.Context) {
			builder := wcb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*WalletMutation)
				if !ok {
					return nil, fmt.Errorf("unexpected mutation type %T", m)
				}
				if err := builder.check(); err != nil {
					return nil, err
				}
				builder.mutation = mutation
				var err error
				nodes[i], specs[i] = builder.createSpec()
				if i < len(mutators)-1 {
					_, err = mutators[i+1].Mutate(root, wcb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, wcb.driver, spec); err != nil {
						if sqlgraph.IsConstraintError(err) {
							err = &ConstraintError{msg: err.Error(), wrap: err}
						}
					}
				}
				if err != nil {
					return nil, err
				}
				mutation.id = &nodes[i].ID
				mutation.done = true
				return nodes[i], nil
			})
			for i := len(builder.hooks) - 1; i >= 0; i-- {
				mut = builder.hooks[i](mut)
			}
			mutators[i] = mut
		}(i, ctx)
	}
	if len(mutators) > 0 {
		if _, err := mutators[0].Mutate(ctx, wcb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (wcb *WalletCreateBulk) SaveX(ctx context.Context) []*Wallet {
	v, err := wcb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (wcb *WalletCreateBulk) Exec(ctx context.Context) error {
	_, err := wcb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (wcb *WalletCreateBulk) ExecX(ctx context.Context) {
	if err := wcb.Exec(ctx); err != nil {
		panic(err)
	}
}
