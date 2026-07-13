package ent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	entRefund "github.com/flexprice/flexprice/ent/refund"
	domainRefund "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type refundRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts RefundQueryOptions
}

// NewRefundRepository creates a new Ent-backed refund repository.
func NewRefundRepository(client postgres.IClient, log *logger.Logger) domainRefund.Repository {
	return &refundRepository{
		client:    client,
		log:       log,
		queryOpts: RefundQueryOptions{},
	}
}

// Create inserts a new refund using INSERT … ON CONFLICT DO NOTHING RETURNING id.
// Returns (refund, true, nil) when inserted, or (nil, false, nil) when the idempotency key
// already existed.  Must be called within a transaction.
func (r *refundRepository) Create(ctx context.Context, ref *domainRefund.Refund) (*domainRefund.Refund, bool, error) {
	span := StartRepositorySpan(ctx, "refund", "create", map[string]interface{}{
		"refund_id":       ref.ID,
		"payment_id":      ref.PaymentID,
		"idempotency_key": ref.IdempotencyKey,
	})
	defer FinishSpan(span)

	if ref.EnvironmentID == "" {
		ref.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	now := time.Now().UTC()
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = now
	}
	if ref.UpdatedAt.IsZero() {
		ref.UpdatedAt = now
	}
	if ref.Status == "" {
		ref.Status = types.StatusPublished
	}
	if ref.TenantID == "" {
		ref.TenantID = types.GetTenantID(ctx)
	}

	// Marshal JSON fields
	metadataBytes, err := json.Marshal(ref.Metadata)
	if err != nil {
		metadataBytes = []byte("{}")
	}
	gwMetadataBytes, err := json.Marshal(ref.GatewayMetadata)
	if err != nil {
		gwMetadataBytes = []byte("{}")
	}

	const rawSQL = `
INSERT INTO refunds (
  id, tenant_id, environment_id, status, refund_status, payment_id, payment_gateway,
  amount, currency, refund_reason, idempotency_key, gateway_idempotency_token,
  claimed_at, initiated_at, created_at, updated_at, created_by, updated_by,
  metadata, gateway_metadata
)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT (tenant_id, environment_id, idempotency_key) DO NOTHING
RETURNING id`

	rows, err := r.client.Writer(ctx).QueryContext(ctx, rawSQL,
		ref.ID,
		ref.TenantID,
		ref.EnvironmentID,
		string(ref.Status),
		string(ref.RefundStatus),
		ref.PaymentID,
		ref.PaymentGateway,
		ref.Amount,
		ref.Currency,
		string(ref.RefundReason),
		ref.IdempotencyKey,
		ref.GatewayIdempotencyToken,
		ref.ClaimedAt,
		ref.InitiatedAt,
		ref.CreatedAt,
		ref.UpdatedAt,
		ref.CreatedBy,
		ref.UpdatedBy,
		metadataBytes,
		gwMetadataBytes,
	)
	if err != nil {
		SetSpanError(span, err)
		return nil, false, ierr.WithError(err).
			WithHint("Failed to insert refund").
			WithReportableDetails(map[string]interface{}{
				"refund_id":       ref.ID,
				"idempotency_key": ref.IdempotencyKey,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			SetSpanError(span, err)
			return nil, false, ierr.WithError(err).
				WithHint("Failed to read refund insert result").
				Mark(ierr.ErrDatabase)
		}
		// ON CONFLICT DO NOTHING — row already exists
		return nil, false, nil
	}

	var returnedID string
	if err := rows.Scan(&returnedID); err != nil {
		SetSpanError(span, err)
		return nil, false, ierr.WithError(err).
			WithHint("Failed to scan refund id after insert").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return ref, true, nil
}

// Get fetches a refund by its primary key, scoped to the current tenant+environment.
func (r *refundRepository) Get(ctx context.Context, id string) (*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get", map[string]interface{}{
		"refund_id": id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	ref, err := client.Refund.Query().
		Where(
			entRefund.ID(id),
			entRefund.TenantID(types.GetTenantID(ctx)),
			entRefund.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{"refund_id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get refund").
			WithReportableDetails(map[string]interface{}{"refund_id": id}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEnt(ref), nil
}

// GetByGatewayRefundID looks up a refund by its gateway refund identifier.
func (r *refundRepository) GetByGatewayRefundID(ctx context.Context, gatewayRefundID string) (*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get_by_gateway_refund_id", map[string]interface{}{
		"gateway_refund_id": gatewayRefundID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	ref, err := client.Refund.Query().
		Where(
			entRefund.GatewayRefundID(gatewayRefundID),
			entRefund.TenantID(types.GetTenantID(ctx)),
			entRefund.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{"gateway_refund_id": gatewayRefundID}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get refund by gateway refund ID").
			WithReportableDetails(map[string]interface{}{"gateway_refund_id": gatewayRefundID}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEnt(ref), nil
}

// GetByIdempotencyKey looks up a refund by (tenant_id, environment_id, idempotency_key).
func (r *refundRepository) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": idempotencyKey,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	ref, err := client.Refund.Query().
		Where(
			entRefund.IdempotencyKey(idempotencyKey),
			entRefund.TenantID(types.GetTenantID(ctx)),
			entRefund.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{"idempotency_key": idempotencyKey}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get refund by idempotency key").
			WithReportableDetails(map[string]interface{}{"idempotency_key": idempotencyKey}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEnt(ref), nil
}

// GetPendingByPaymentID returns in-flight refunds (PENDING or PROCESSING, no GatewayRefundID) for a payment.
func (r *refundRepository) GetPendingByPaymentID(ctx context.Context, paymentID string) ([]*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get_pending_by_payment_id", map[string]interface{}{
		"payment_id": paymentID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	refs, err := client.Refund.Query().
		Where(
			entRefund.PaymentID(paymentID),
			entRefund.TenantID(types.GetTenantID(ctx)),
			entRefund.EnvironmentID(types.GetEnvironmentID(ctx)),
			entRefund.RefundStatusIn(
				string(types.RefundStatusPending),
				string(types.RefundStatusProcessing),
			),
			entRefund.GatewayRefundIDIsNil(),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get pending refunds by payment ID").
			WithReportableDetails(map[string]interface{}{"payment_id": paymentID}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEntList(refs), nil
}

// List returns refunds matching the given filter.
func (r *refundRepository) List(ctx context.Context, filter *types.RefundFilter) ([]*domainRefund.Refund, error) {
	if filter == nil {
		filter = &types.RefundFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	span := StartRepositorySpan(ctx, "refund", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.Refund.Query()
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	refs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list refunds").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainRefund.FromEntList(refs), nil
}

// Count returns the number of refunds matching the given filter.
func (r *refundRepository) Count(ctx context.Context, filter *types.RefundFilter) (int, error) {
	if filter == nil {
		filter = &types.RefundFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	span := StartRepositorySpan(ctx, "refund", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.Refund.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count refunds").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// UpdateStatus performs a compare-and-swap UPDATE on refund_status.
// Returns updated=false when no row was affected (concurrent write detected).
func (r *refundRepository) UpdateStatus(
	ctx context.Context,
	id string,
	expectedStatus types.RefundStatus,
	newStatus types.RefundStatus,
	updates domainRefund.RefundStatusUpdate,
) (bool, error) {
	span := StartRepositorySpan(ctx, "refund", "update_status", map[string]interface{}{
		"refund_id":       id,
		"expected_status": expectedStatus,
		"new_status":      newStatus,
	})
	defer FinishSpan(span)

	// Build the SET clause dynamically based on what is provided
	setClauses := []string{"refund_status = $1", "updated_at = $2"}
	args := []interface{}{string(newStatus), time.Now().UTC()}
	argIdx := 3

	if updates.GatewayRefundID != nil {
		setClauses = append(setClauses, fmt.Sprintf("gateway_refund_id = $%d", argIdx))
		args = append(args, *updates.GatewayRefundID)
		argIdx++
	}
	if updates.GatewayIdempotencyToken != nil {
		setClauses = append(setClauses, fmt.Sprintf("gateway_idempotency_token = $%d", argIdx))
		args = append(args, *updates.GatewayIdempotencyToken)
		argIdx++
	}
	if updates.FailureReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("failure_reason = $%d", argIdx))
		args = append(args, *updates.FailureReason)
		argIdx++
	}
	if updates.ClaimedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("claimed_at = $%d", argIdx))
		args = append(args, *updates.ClaimedAt)
		argIdx++
	}
	if updates.SucceededAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("succeeded_at = $%d", argIdx))
		args = append(args, *updates.SucceededAt)
		argIdx++
	}
	if updates.FailedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("failed_at = $%d", argIdx))
		args = append(args, *updates.FailedAt)
		argIdx++
	}
	if updates.CancelledAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("cancelled_at = $%d", argIdx))
		args = append(args, *updates.CancelledAt)
		argIdx++
	}
	if updates.GatewayMetadata != nil {
		gwBytes, err := json.Marshal(updates.GatewayMetadata)
		if err != nil {
			gwBytes = []byte("{}")
		}
		setClauses = append(setClauses, fmt.Sprintf("gateway_metadata = $%d", argIdx))
		args = append(args, gwBytes)
		argIdx++
	}

	// WHERE conditions
	args = append(args, id, string(expectedStatus), types.GetTenantID(ctx), types.GetEnvironmentID(ctx))
	wherePlaceholders := fmt.Sprintf(
		"id = $%d AND refund_status = $%d AND tenant_id = $%d AND environment_id = $%d",
		argIdx, argIdx+1, argIdx+2, argIdx+3,
	)

	query := fmt.Sprintf("UPDATE refunds SET %s WHERE %s",
		strings.Join(setClauses, ", "),
		wherePlaceholders,
	)

	result, err := r.client.Writer(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return false, ierr.WithError(err).
			WithHint("Failed to update refund status").
			WithReportableDetails(map[string]interface{}{
				"refund_id":       id,
				"expected_status": expectedStatus,
				"new_status":      newStatus,
			}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		SetSpanError(span, err)
		return false, ierr.WithError(err).
			WithHint("Failed to get rows affected for refund status update").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return rowsAffected > 0, nil
}

// IncrementPaymentRefundedAmount atomically increments payment.refunded_amount by delta.
// Must be called within a transaction. delta may be negative to release a claim.
func (r *refundRepository) IncrementPaymentRefundedAmount(ctx context.Context, paymentID string, delta decimal.Decimal) error {
	span := StartRepositorySpan(ctx, "refund", "increment_payment_refunded_amount", map[string]interface{}{
		"payment_id": paymentID,
		"delta":      delta.String(),
	})
	defer FinishSpan(span)

	const rawSQL = `
UPDATE payments
SET refunded_amount = refunded_amount + $1, updated_at = now()
WHERE id = $2 AND tenant_id = $3 AND environment_id = $4`

	result, err := r.client.Writer(ctx).ExecContext(ctx, rawSQL,
		delta,
		paymentID,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
	)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to increment payment refunded amount").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
				"delta":      delta.String(),
			}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to get rows affected for payment refunded amount increment").
			Mark(ierr.ErrDatabase)
	}
	if rowsAffected == 0 {
		return ierr.NewError("payment not found").
			WithHint("Payment not found when incrementing refunded amount").
			WithReportableDetails(map[string]interface{}{"payment_id": paymentID}).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	return nil
}

// RefundQuery is a type alias for better readability
type RefundQuery = *ent.RefundQuery

// RefundQueryOptions implements BaseQueryOptions for refund queries
type RefundQueryOptions struct{}

func (o RefundQueryOptions) ApplyTenantFilter(ctx context.Context, query RefundQuery) RefundQuery {
	return query.Where(entRefund.TenantID(types.GetTenantID(ctx)))
}

func (o RefundQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query RefundQuery) RefundQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entRefund.EnvironmentID(environmentID))
	}
	return query
}

func (o RefundQueryOptions) ApplyStatusFilter(query RefundQuery, status string) RefundQuery {
	if status == "" {
		return query.Where(entRefund.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entRefund.Status(status))
}

func (o RefundQueryOptions) ApplySortFilter(query RefundQuery, field string, order string) RefundQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o RefundQueryOptions) ApplyPaginationFilter(query RefundQuery, limit int, offset int) RefundQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o RefundQueryOptions) GetFieldName(field string) string {
	if entRefund.ValidColumn(field) {
		return field
	}
	return entRefund.FieldCreatedAt
}

func (o RefundQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.RefundFilter, query RefundQuery) RefundQuery {
	if f == nil {
		return query
	}
	if f.PaymentID != nil {
		query = query.Where(entRefund.PaymentID(*f.PaymentID))
	}
	if f.Status != nil {
		query = query.Where(entRefund.RefundStatus(string(*f.Status)))
	}
	if f.Gateway != nil {
		query = query.Where(entRefund.PaymentGateway(*f.Gateway))
	}
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(entRefund.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(entRefund.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}
	return query
}
