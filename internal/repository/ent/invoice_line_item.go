package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/internal/cache"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

const invoiceLineItemBatchSize = 1000

type invoiceLineItemRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

// NewInvoiceLineItemRepository creates a new invoice line item repository.
func NewInvoiceLineItemRepository(
	client postgres.IClient,
	log *logger.Logger,
	c cache.Cache,
) domaininvoice.LineItemRepository {
	return &invoiceLineItemRepository{client: client, log: log, cache: c}
}

// Cache helpers

func (r *invoiceLineItemRepository) SetCache(ctx context.Context, item *domaininvoice.InvoiceLineItem) {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "set", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, item.ID)
	r.cache.Set(ctx, cacheKey, item, cache.ExpiryDefaultInMemory)
}

func (r *invoiceLineItemRepository) GetCache(ctx context.Context, id string) *domaininvoice.InvoiceLineItem {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "get", map[string]interface{}{
		"line_item_id": id,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, id)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domaininvoice.InvoiceLineItem)
	}
	return nil
}

func (r *invoiceLineItemRepository) DeleteCache(ctx context.Context, id string) {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, id)
	r.cache.Delete(ctx, cacheKey)
}

// Create creates a single invoice line item.
func (r *invoiceLineItemRepository) Create(ctx context.Context, item *domaininvoice.InvoiceLineItem) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "create", map[string]interface{}{
		"line_item_id": item.ID,
		"invoice_id":   item.InvoiceID,
	})
	defer FinishSpan(span)

	r.log.Debugw("creating invoice line item",
		"line_item_id", item.ID,
		"invoice_id", item.InvoiceID,
	)

	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := r.client.Writer(ctx).InvoiceLineItem.Create().
		SetID(item.ID).
		SetTenantID(item.TenantID).
		SetInvoiceID(item.InvoiceID).
		SetCustomerID(item.CustomerID).
		SetNillableSubscriptionID(item.SubscriptionID).
		SetNillableEntityID(item.EntityID).
		SetNillableEntityType(convertStringPtrToInvoiceLineItemEntityTypePtr(item.EntityType)).
		SetNillablePlanDisplayName(item.PlanDisplayName).
		SetNillablePriceType(convertStringPtrToPriceTypePtr(item.PriceType)).
		SetNillablePriceID(item.PriceID).
		SetNillableMeterID(item.MeterID).
		SetNillableMeterDisplayName(item.MeterDisplayName).
		SetNillablePriceUnitID(item.PriceUnitID).
		SetNillablePriceUnit(item.PriceUnit).
		SetNillablePriceUnitAmount(item.PriceUnitAmount).
		SetNillableDisplayName(item.DisplayName).
		SetAmount(item.Amount).
		SetQuantity(item.Quantity).
		SetCurrency(item.Currency).
		SetNillablePeriodStart(item.PeriodStart).
		SetNillablePeriodEnd(item.PeriodEnd).
		SetMetadata(item.Metadata).
		SetEnvironmentID(item.EnvironmentID).
		SetCommitmentInfo(item.CommitmentInfo).
		SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
		SetLineItemDiscount(item.LineItemDiscount).
		SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
		SetStatus(string(item.Status)).
		SetCreatedBy(item.CreatedBy).
		SetUpdatedBy(item.UpdatedBy).
		SetCreatedAt(item.CreatedAt).
		SetUpdatedAt(item.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHintf("invoice line item with ID %s already exists", item.ID).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
					"invoice_id":   item.InvoiceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("invoice line item creation failed").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
				"invoice_id":   item.InvoiceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// CreateBulk creates multiple invoice line items, batching to avoid PostgreSQL's parameter limit.
func (r *invoiceLineItemRepository) CreateBulk(ctx context.Context, items []*domaininvoice.InvoiceLineItem) error {
	if len(items) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "invoice_line_item", "create_bulk", map[string]interface{}{
		"item_count": len(items),
	})
	defer FinishSpan(span)

	r.log.Debugw("creating invoice line items in bulk",
		"item_count", len(items),
		"tenant_id", types.GetTenantID(ctx),
	)

	client := r.client.Writer(ctx)

	bulk := make([]*ent.InvoiceLineItemCreate, len(items))
	for i, item := range items {
		if item.EnvironmentID == "" {
			item.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		bulk[i] = client.InvoiceLineItem.Create().
			SetID(item.ID).
			SetTenantID(item.TenantID).
			SetInvoiceID(item.InvoiceID).
			SetCustomerID(item.CustomerID).
			SetNillableSubscriptionID(item.SubscriptionID).
			SetNillableEntityID(item.EntityID).
			SetNillableEntityType(convertStringPtrToInvoiceLineItemEntityTypePtr(item.EntityType)).
			SetNillablePlanDisplayName(item.PlanDisplayName).
			SetNillablePriceType(convertStringPtrToPriceTypePtr(item.PriceType)).
			SetNillablePriceID(item.PriceID).
			SetNillableMeterID(item.MeterID).
			SetNillableMeterDisplayName(item.MeterDisplayName).
			SetNillablePriceUnitID(item.PriceUnitID).
			SetNillablePriceUnit(item.PriceUnit).
			SetNillablePriceUnitAmount(item.PriceUnitAmount).
			SetNillableDisplayName(item.DisplayName).
			SetAmount(item.Amount).
			SetQuantity(item.Quantity).
			SetCurrency(item.Currency).
			SetNillablePeriodStart(item.PeriodStart).
			SetNillablePeriodEnd(item.PeriodEnd).
			SetMetadata(item.Metadata).
			SetEnvironmentID(item.EnvironmentID).
			SetCommitmentInfo(item.CommitmentInfo).
			SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
			SetLineItemDiscount(item.LineItemDiscount).
			SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
			SetStatus(string(item.Status)).
			SetCreatedBy(item.CreatedBy).
			SetUpdatedBy(item.UpdatedBy).
			SetCreatedAt(item.CreatedAt).
			SetUpdatedAt(item.UpdatedAt)
	}

	for i := 0; i < len(bulk); i += invoiceLineItemBatchSize {
		end := i + invoiceLineItemBatchSize
		if end > len(bulk) {
			end = len(bulk)
		}
		_, err := client.InvoiceLineItem.CreateBulk(bulk[i:end]...).Save(ctx)
		if err != nil {
			SetSpanError(span, err)
			return ierr.WithError(err).
				WithHint("failed to create invoice line items in bulk").
				WithReportableDetails(map[string]interface{}{
					"count":       len(items),
					"batch_start": i,
					"batch_end":   end,
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a single invoice line item by ID (tenant-scoped).
func (r *invoiceLineItemRepository) Get(ctx context.Context, id string) (*domaininvoice.InvoiceLineItem, error) {
	span := StartRepositorySpan(ctx, "invoice_line_item", "get", map[string]interface{}{
		"line_item_id": id,
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if cached := r.GetCache(ctx, id); cached != nil {
		return cached, nil
	}

	r.log.Debugw("getting invoice line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	item, err := r.client.Reader(ctx).InvoiceLineItem.Query().
		Where(
			invoicelineitem.ID(id),
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("invoice line item %s not found", id).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("getting invoice line item failed").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := domaininvoice.LineItemFromEnt(item)
	r.SetCache(ctx, result)
	SetSpanSuccess(span)
	return result, nil
}

// Update updates mutable fields on a line item.
func (r *invoiceLineItemRepository) Update(ctx context.Context, item *domaininvoice.InvoiceLineItem) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "update", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer FinishSpan(span)

	r.log.Debugw("updating invoice line item", "line_item_id", item.ID)

	_, err := r.client.Writer(ctx).InvoiceLineItem.UpdateOneID(item.ID).
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
		SetLineItemDiscount(item.LineItemDiscount).
		SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
		SetMetadata(item.Metadata).
		SetStatus(string(item.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("invoice line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("failed to update invoice line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, item.ID)
	SetSpanSuccess(span)
	return nil
}

// Delete soft-deletes an invoice line item by setting its status to deleted.
func (r *invoiceLineItemRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	r.log.Debugw("deleting invoice line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := r.client.Writer(ctx).InvoiceLineItem.UpdateOneID(id).
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("invoice line item %s not found", id).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("failed to delete invoice line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	SetSpanSuccess(span)
	return nil
}

// ListByInvoiceID retrieves all published line items for the given invoice.
func (r *invoiceLineItemRepository) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*domaininvoice.InvoiceLineItem, error) {
	span := StartRepositorySpan(ctx, "invoice_line_item", "list_by_invoice", map[string]interface{}{
		"invoice_id": invoiceID,
	})
	defer FinishSpan(span)

	r.log.Debugw("listing invoice line items by invoice",
		"invoice_id", invoiceID,
		"tenant_id", types.GetTenantID(ctx),
	)

	items, err := r.client.Reader(ctx).InvoiceLineItem.Query().
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			invoicelineitem.InvoiceID(invoiceID),
			invoicelineitem.Status(string(types.StatusPublished)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("listing invoice line items failed").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": invoiceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := make([]*domaininvoice.InvoiceLineItem, len(items))
	for i, item := range items {
		result[i] = domaininvoice.LineItemFromEnt(item)
	}

	SetSpanSuccess(span)
	return result, nil
}
