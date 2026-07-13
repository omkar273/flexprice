package refund

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Refund represents a gateway refund transaction linked to a Payment.
type Refund struct {
	// Unique identifier for this refund
	ID string `json:"id"`
	// The payment this refund is applied against
	PaymentID string `json:"payment_id"`
	// The gateway used to process the original payment (and this refund)
	PaymentGateway string `json:"payment_gateway"`
	// GatewayRefundID is the refund identifier returned by the gateway (nil until gateway confirms)
	GatewayRefundID *string `json:"gateway_refund_id,omitempty"`
	// GatewayTrackingID is an optional tracking handle from the gateway
	GatewayTrackingID *string `json:"gateway_tracking_id,omitempty"`
	// Amount to refund in the payment's currency
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`
	// Currency is a three-letter ISO code (USD, EUR, etc.)
	Currency string `json:"currency"`
	// RefundStatus is the current lifecycle state of the refund
	RefundStatus types.RefundStatus `json:"refund_status"`
	// RefundReason is why the refund was issued
	RefundReason types.RefundReason `json:"refund_reason"`
	// IdempotencyKey is the caller-supplied deduplication key
	IdempotencyKey string `json:"idempotency_key"`
	// GatewayIdempotencyToken is the token sent to the gateway to deduplicate the refund call
	GatewayIdempotencyToken string `json:"gateway_idempotency_token"`
	// FailureReason contains the gateway error message when the refund fails
	FailureReason *string `json:"failure_reason,omitempty"`
	// Metadata contains caller-supplied key-value pairs
	Metadata types.Metadata `json:"metadata,omitempty"`
	// GatewayMetadata is the raw JSONB response from the gateway
	GatewayMetadata map[string]interface{} `json:"gateway_metadata,omitempty"`
	// InitiatedAt is when the refund request was first received
	InitiatedAt *time.Time `json:"initiated_at,omitempty"`
	// ClaimedAt is when the optimistic lock on payment.refunded_amount was claimed (phase 1)
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	// SucceededAt is when the gateway confirmed the refund succeeded
	SucceededAt *time.Time `json:"succeeded_at,omitempty"`
	// FailedAt is when the gateway confirmed the refund failed
	FailedAt *time.Time `json:"failed_at,omitempty"`
	// CancelledAt is when the refund was cancelled
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	// EnvironmentID identifies which environment this refund belongs to
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// RefundWebhookEvent is an inbox table entry for idempotent gateway webhook processing.
type RefundWebhookEvent struct {
	// Unique identifier for this webhook event record
	ID string `json:"id"`
	// Gateway that sent the webhook (e.g. "razorpay", "stripe")
	Gateway string `json:"gateway"`
	// GatewayEventID is the gateway's own event/notification identifier
	GatewayEventID string `json:"gateway_event_id"`
	// RawPayload is the raw JSON body received from the gateway
	RawPayload map[string]interface{} `json:"raw_payload,omitempty"`
	// Processed indicates whether this event has been successfully handled
	Processed bool `json:"processed"`
	// ProcessedAt is when the event was marked as processed
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	// MatchedRefundID is the refund that was updated as a result of this event
	MatchedRefundID *string `json:"matched_refund_id,omitempty"`
	// Attempts counts how many processing attempts have been made
	Attempts int `json:"attempts"`
	// EnvironmentID identifies which environment this event belongs to
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// TableName returns the table name for the refund.
func (r *Refund) TableName() string {
	return "refunds"
}

// TableName returns the table name for the refund webhook event.
func (rwe *RefundWebhookEvent) TableName() string {
	return "refund_webhook_events"
}

// FromEnt converts an Ent Refund to a domain Refund.
func FromEnt(r *ent.Refund) *Refund {
	if r == nil {
		return nil
	}

	return &Refund{
		ID:                      r.ID,
		PaymentID:               r.PaymentID,
		PaymentGateway:          r.PaymentGateway,
		GatewayRefundID:         r.GatewayRefundID,
		GatewayTrackingID:       r.GatewayTrackingID,
		Amount:                  r.Amount,
		Currency:                r.Currency,
		RefundStatus:            types.RefundStatus(r.RefundStatus),
		RefundReason:            types.RefundReason(r.RefundReason),
		IdempotencyKey:          r.IdempotencyKey,
		GatewayIdempotencyToken: r.GatewayIdempotencyToken,
		FailureReason:           r.FailureReason,
		Metadata:                r.Metadata,
		GatewayMetadata:         r.GatewayMetadata,
		InitiatedAt:             r.InitiatedAt,
		ClaimedAt:               r.ClaimedAt,
		SucceededAt:             r.SucceededAt,
		FailedAt:                r.FailedAt,
		CancelledAt:             r.CancelledAt,
		EnvironmentID:           r.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  r.TenantID,
			Status:    types.Status(r.Status),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			CreatedBy: r.CreatedBy,
			UpdatedBy: r.UpdatedBy,
		},
	}
}

// FromEntList converts a slice of Ent Refunds to domain Refunds.
func FromEntList(refunds []*ent.Refund) []*Refund {
	if refunds == nil {
		return nil
	}

	result := make([]*Refund, len(refunds))
	for i, r := range refunds {
		result[i] = FromEnt(r)
	}
	return result
}

// FromEntWebhookEvent converts an Ent RefundWebhookEvent to a domain RefundWebhookEvent.
func FromEntWebhookEvent(e *ent.RefundWebhookEvent) *RefundWebhookEvent {
	if e == nil {
		return nil
	}

	return &RefundWebhookEvent{
		ID:              e.ID,
		Gateway:         e.Gateway,
		GatewayEventID:  e.GatewayEventID,
		RawPayload:      e.RawPayload,
		Processed:       e.Processed,
		ProcessedAt:     e.ProcessedAt,
		MatchedRefundID: e.MatchedRefundID,
		Attempts:        e.Attempts,
		EnvironmentID:   e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntWebhookEventList converts a slice of Ent RefundWebhookEvents to domain RefundWebhookEvents.
func FromEntWebhookEventList(events []*ent.RefundWebhookEvent) []*RefundWebhookEvent {
	if events == nil {
		return nil
	}

	result := make([]*RefundWebhookEvent, len(events))
	for i, e := range events {
		result[i] = FromEntWebhookEvent(e)
	}
	return result
}
