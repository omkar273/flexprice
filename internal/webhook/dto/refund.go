package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalRefundEvent is the minimal payload published to the system-events topic
// when a Refund is created or updated.
type InternalRefundEvent struct {
	RefundID string `json:"refund_id"`
	TenantID string `json:"tenant_id"`
}

// RefundWebhookPayload is the full webhook payload sent to tenant-facing webhook endpoints.
type RefundWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Refund    *dto.RefundResponse    `json:"refund"`
}

// NewRefundWebhookPayload constructs a RefundWebhookPayload.
func NewRefundWebhookPayload(refund *dto.RefundResponse, eventType types.WebhookEventName) *RefundWebhookPayload {
	return &RefundWebhookPayload{EventType: eventType, Refund: refund}
}
