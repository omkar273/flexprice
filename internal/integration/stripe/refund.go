package stripe

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v82"
)

// RefundResult holds the result of a refund creation at the Stripe gateway.
type RefundResult struct {
	GatewayRefundID string
	Status          string // Stripe refund status: "pending", "succeeded", "failed", "canceled"
	GatewayMetadata map[string]interface{}
}

// CancelRefundResult holds the result of a refund cancellation attempt at the Stripe gateway.
type CancelRefundResult struct {
	Cancelled     bool   // true if cancellation actually applied
	CurrentStatus string // Stripe's current refund status after the attempt
	GatewayMetadata map[string]interface{}
}

// RefundPayment initiates a refund for the given gateway payment ID.
// gatewayIdempotencyToken is a derived token (NOT the raw client key) sent as
// Stripe's Idempotency-Key header. This prevents double-charging even on crash-retry.
func (s *PaymentService) RefundPayment(ctx context.Context, gatewayPaymentID string, amount decimal.Decimal, gatewayIdempotencyToken string) (*RefundResult, error) {
	s.logger.Info(ctx, "initiating stripe refund",
		"gateway_payment_id", gatewayPaymentID,
		"amount", amount.String(),
		"idempotency_token", gatewayIdempotencyToken,
	)

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	amountInSmallestUnit := amount.Mul(decimal.NewFromInt(100)).IntPart()

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(gatewayPaymentID),
		Amount:        stripe.Int64(amountInSmallestUnit),
	}
	params.SetIdempotencyKey(gatewayIdempotencyToken)

	refund, err := stripeClient.V1Refunds.Create(ctx, params)
	if err != nil {
		s.logger.Error(ctx, "failed to create stripe refund",
			"error", err,
			"gateway_payment_id", gatewayPaymentID,
			"amount", amount.String(),
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to initiate refund at Stripe gateway").
			WithReportableDetails(map[string]interface{}{
				"gateway_payment_id": gatewayPaymentID,
				"amount":             amount.String(),
				"error":              err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	result := &RefundResult{
		GatewayRefundID: refund.ID,
		Status:          string(refund.Status),
		GatewayMetadata: stripeRefundToMetadata(refund),
	}

	s.logger.Info(ctx, "stripe refund created",
		"gateway_refund_id", refund.ID,
		"status", refund.Status,
		"gateway_payment_id", gatewayPaymentID,
		"amount", amount.String(),
	)

	return result, nil
}

// CancelRefund attempts to cancel an in-flight refund at the gateway.
// Returns (result, error). If Stripe reports the refund already reached a terminal
// state (already succeeded/failed), result.Cancelled=false — the caller must not
// proceed to RecordRefundCancelled.
func (s *PaymentService) CancelRefund(ctx context.Context, gatewayRefundID string) (*CancelRefundResult, error) {
	s.logger.Info(ctx, "cancelling stripe refund",
		"gateway_refund_id", gatewayRefundID,
	)

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	refund, err := stripeClient.V1Refunds.Cancel(ctx, gatewayRefundID, &stripe.RefundCancelParams{})
	if err != nil {
		// Attempt to retrieve the refund to determine its current status. If Stripe
		// rejected the cancel because the refund is already in a terminal state,
		// we return Cancelled=false with the actual current status rather than an error.
		retrieved, retrieveErr := stripeClient.V1Refunds.Retrieve(ctx, gatewayRefundID, nil)
		if retrieveErr == nil {
			currentStatus := string(retrieved.Status)
			switch currentStatus {
			case "succeeded", "failed", "canceled":
				s.logger.Info(ctx, "refund already in terminal state, skipping cancel",
					"gateway_refund_id", gatewayRefundID,
					"current_status", currentStatus,
				)
				return &CancelRefundResult{
					Cancelled:       false,
					CurrentStatus:   currentStatus,
					GatewayMetadata: stripeRefundToMetadata(retrieved),
				}, nil
			}
		}

		s.logger.Error(ctx, "failed to cancel stripe refund",
			"error", err,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to cancel refund at Stripe gateway").
			WithReportableDetails(map[string]interface{}{
				"gateway_refund_id": gatewayRefundID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	cancelled := string(refund.Status) == "canceled"

	s.logger.Info(ctx, "stripe refund cancel attempted",
		"gateway_refund_id", gatewayRefundID,
		"status", refund.Status,
		"cancelled", cancelled,
	)

	return &CancelRefundResult{
		Cancelled:       cancelled,
		CurrentStatus:   string(refund.Status),
		GatewayMetadata: stripeRefundToMetadata(refund),
	}, nil
}

// stripeRefundToMetadata converts a Stripe Refund object into a generic metadata map.
func stripeRefundToMetadata(r *stripe.Refund) map[string]interface{} {
	meta := map[string]interface{}{
		"id":     r.ID,
		"status": string(r.Status),
		"amount": r.Amount,
	}
	if r.Currency != "" {
		meta["currency"] = string(r.Currency)
	}
	if r.PaymentIntent != nil {
		meta["payment_intent_id"] = r.PaymentIntent.ID
	}
	for k, v := range r.Metadata {
		meta[k] = v
	}
	return meta
}
