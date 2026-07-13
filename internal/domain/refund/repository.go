package refund

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Repository defines the persistence interface for Refund entities.
type Repository interface {
	// Create does the atomic INSERT ... ON CONFLICT (tenant_id, environment_id, idempotency_key) DO NOTHING RETURNING id.
	// Must be called within a transaction (ctx must carry tx). Returns (refund, created bool, error).
	// created=false means a row with the same idempotency key already existed (duplicate request).
	Create(ctx context.Context, refund *Refund) (*Refund, bool, error)

	// Get fetches a refund by its primary key.
	Get(ctx context.Context, id string) (*Refund, error)

	// GetByGatewayRefundID looks up a refund by its gateway refund ID (for webhook matching).
	GetByGatewayRefundID(ctx context.Context, gatewayRefundID string) (*Refund, error)

	// GetByIdempotencyKey looks up a refund by (tenant_id, environment_id, idempotency_key).
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*Refund, error)

	// GetPendingByPaymentID returns in-flight refunds (PENDING or PROCESSING, no GatewayRefundID) for a payment.
	// Used by the Razorpay webhook fallback path to match unlinked events.
	GetPendingByPaymentID(ctx context.Context, paymentID string) ([]*Refund, error)

	// List returns refunds matching the given filter.
	List(ctx context.Context, filter *types.RefundFilter) ([]*Refund, error)

	// Count returns the number of refunds matching the given filter.
	Count(ctx context.Context, filter *types.RefundFilter) (int, error)

	// UpdateStatus is a compare-and-swap UPDATE:
	//   UPDATE refunds SET refund_status = $newStatus, ... WHERE id = $1 AND refund_status = $expectedStatus
	// Returns updated=false when the CAS found a different status (concurrent write detected).
	UpdateStatus(ctx context.Context, id string, expectedStatus types.RefundStatus, newStatus types.RefundStatus, updates RefundStatusUpdate) (bool, error)

	// IncrementPaymentRefundedAmount increments payment.refunded_amount by delta under a SELECT FOR UPDATE lock.
	// Must be called within a transaction. delta can be negative to release a previously claimed amount.
	IncrementPaymentRefundedAmount(ctx context.Context, paymentID string, delta decimal.Decimal) error
}

// RefundStatusUpdate contains optional fields to set alongside a status transition.
type RefundStatusUpdate struct {
	GatewayRefundID         *string
	GatewayIdempotencyToken *string
	FailureReason           *string
	ClaimedAt               *time.Time
	SucceededAt             *time.Time
	FailedAt                *time.Time
	CancelledAt             *time.Time
	GatewayMetadata         map[string]interface{}
}

// WebhookEventRepository defines the persistence interface for RefundWebhookEvent inbox entries.
type WebhookEventRepository interface {
	// Create inserts a new event, ignoring duplicates (ON CONFLICT DO NOTHING on gateway + gateway_event_id).
	Create(ctx context.Context, event *RefundWebhookEvent) error

	// GetByGatewayEventID looks up an event by the composite (gateway, gateway_event_id) key.
	GetByGatewayEventID(ctx context.Context, gateway, gatewayEventID string) (*RefundWebhookEvent, error)

	// MarkProcessed marks the event as processed and records the matched refund ID.
	MarkProcessed(ctx context.Context, id string, refundID *string) error

	// ListUnprocessed returns unprocessed events whose created_at is older than minAge.
	// Used by the crash-recovery sweep to re-process events that were never handled.
	ListUnprocessed(ctx context.Context, minAge time.Duration) ([]*RefundWebhookEvent, error)

	// IncrementAttempts bumps the attempts counter on the given event.
	IncrementAttempts(ctx context.Context, id string) error
}
