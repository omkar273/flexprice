package razorpay

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
)

// RefundResult holds the outcome of a Razorpay refund API call.
type RefundResult struct {
	GatewayRefundID string
	Status          string // Razorpay refund status: "pending", "processed", "failed"
	GatewayMetadata map[string]interface{}
}

// RefundPayment initiates a refund via Razorpay's API.
// gatewayIdempotencyToken is sent as the X-Razorpay-Idempotency-Key header.
// Some Razorpay refunds complete instantly (status="processed"); handle synchronous terminal states.
func (s *PaymentService) RefundPayment(ctx context.Context, gatewayPaymentID string, amount decimal.Decimal, gatewayIdempotencyToken string) (*RefundResult, error) {
	s.logger.Info(ctx, "initiating refund via Razorpay",
		"gateway_payment_id", gatewayPaymentID,
		"amount", amount.String(),
	)

	razorpayClient, _, err := s.client.GetRazorpaySDKClient(ctx)
	if err != nil {
		s.logger.Error(ctx, "failed to get Razorpay client for refund", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	// Razorpay expects amounts in the smallest currency unit (paise for INR).
	amountInPaise := amount.Mul(decimal.NewFromInt(100)).IntPart()

	body := map[string]interface{}{
		"amount": amountInPaise,
		"notes": map[string]interface{}{
			"flexprice_idempotency_token": gatewayIdempotencyToken,
		},
	}

	headers := map[string]string{}
	if gatewayIdempotencyToken != "" {
		headers["X-Razorpay-Idempotency-Key"] = gatewayIdempotencyToken
	}

	resp, err := razorpayClient.Payment.Refund(gatewayPaymentID, int(amountInPaise), body, headers)
	if err != nil {
		s.logger.Error(ctx, "failed to create refund in Razorpay",
			"error", err,
			"gateway_payment_id", gatewayPaymentID,
		)
		return nil, ierr.NewError("failed to create refund in Razorpay").
			WithHint("Unable to create refund in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"gateway_payment_id": gatewayPaymentID,
				"error":              err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	refundID, _ := resp["id"].(string)
	status, _ := resp["status"].(string)

	s.logger.Info(ctx, "successfully created refund in Razorpay",
		"gateway_payment_id", gatewayPaymentID,
		"gateway_refund_id", refundID,
		"status", status,
	)

	return &RefundResult{
		GatewayRefundID: refundID,
		Status:          status,
		GatewayMetadata: resp,
	}, nil
}
