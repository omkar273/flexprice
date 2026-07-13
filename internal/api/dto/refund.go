package dto

import (
	"time"

	refunddomain "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateRefundRequest is the request body for POST /payments/{id}/refunds.
// payment_id comes from the URL path, not the body.
// IdempotencyKey is required per the refund spec.
type CreateRefundRequest struct {
	// amount is the monetary value to refund; must be > 0 and <= payment amount minus previously refunded amount
	Amount decimal.Decimal `json:"amount" validate:"required" swaggertype:"string"`

	// reason is why the refund is being issued
	Reason types.RefundReason `json:"reason" validate:"required"`

	// metadata contains optional caller-supplied key-value pairs
	Metadata types.Metadata `json:"metadata,omitempty"`

	// idempotency_key prevents duplicate refund submissions
	IdempotencyKey string `json:"idempotency_key" validate:"required"`
}

// Validate checks the request for semantic correctness beyond struct-tag validation.
func (r *CreateRefundRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Amount.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("amount must be greater than zero").
			WithHint("Provide a positive refund amount").
			WithReportableDetails(map[string]any{"amount": r.Amount}).
			Mark(ierr.ErrValidation)
	}

	if r.IdempotencyKey == "" {
		return ierr.NewError("idempotency_key is required").
			WithHint("Provide a non-empty idempotency_key to prevent duplicate refunds").
			Mark(ierr.ErrValidation)
	}

	if err := r.Reason.Validate(); err != nil {
		return err
	}

	return nil
}

// RefundResponse is the API response for a single refund.
type RefundResponse struct {
	ID                string             `json:"id"`
	PaymentID         string             `json:"payment_id"`
	PaymentGateway    string             `json:"payment_gateway"`
	GatewayRefundID   *string            `json:"gateway_refund_id,omitempty"`
	GatewayTrackingID *string            `json:"gateway_tracking_id,omitempty"`
	Amount            decimal.Decimal    `json:"amount" swaggertype:"string"`
	Currency          string             `json:"currency"`
	RefundStatus      types.RefundStatus `json:"refund_status"`
	RefundReason      types.RefundReason `json:"refund_reason"`
	IdempotencyKey    string             `json:"idempotency_key"`
	FailureReason     *string            `json:"failure_reason,omitempty"`
	Metadata          types.Metadata     `json:"metadata,omitempty"`
	InitiatedAt       *time.Time         `json:"initiated_at,omitempty"`
	SucceededAt       *time.Time         `json:"succeeded_at,omitempty"`
	FailedAt          *time.Time         `json:"failed_at,omitempty"`
	CancelledAt       *time.Time         `json:"cancelled_at,omitempty"`
	EnvironmentID     string             `json:"environment_id"`
	TenantID          string             `json:"tenant_id"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	CreatedBy         string             `json:"created_by"`
	UpdatedBy         string             `json:"updated_by"`
}

// ListRefundsResponse is the paginated list response for refunds.
type ListRefundsResponse struct {
	Items      []*RefundResponse        `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// NewRefundResponse converts a domain Refund to a RefundResponse DTO.
func NewRefundResponse(r *refunddomain.Refund) *RefundResponse {
	if r == nil {
		return nil
	}

	return &RefundResponse{
		ID:                r.ID,
		PaymentID:         r.PaymentID,
		PaymentGateway:    r.PaymentGateway,
		GatewayRefundID:   r.GatewayRefundID,
		GatewayTrackingID: r.GatewayTrackingID,
		Amount:            r.Amount,
		Currency:          r.Currency,
		RefundStatus:      r.RefundStatus,
		RefundReason:      r.RefundReason,
		IdempotencyKey:    r.IdempotencyKey,
		FailureReason:     r.FailureReason,
		Metadata:          r.Metadata,
		InitiatedAt:       r.InitiatedAt,
		SucceededAt:       r.SucceededAt,
		FailedAt:          r.FailedAt,
		CancelledAt:       r.CancelledAt,
		EnvironmentID:     r.EnvironmentID,
		TenantID:          r.TenantID,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		CreatedBy:         r.CreatedBy,
		UpdatedBy:         r.UpdatedBy,
	}
}

// NewListRefundsResponse builds a paginated list response from domain refunds.
func NewListRefundsResponse(refunds []*refunddomain.Refund, total int) *ListRefundsResponse {
	items := make([]*RefundResponse, 0, len(refunds))
	for _, r := range refunds {
		items = append(items, NewRefundResponse(r))
	}

	return &ListRefundsResponse{
		Items: items,
		Pagination: types.PaginationResponse{
			Total: total,
		},
	}
}
