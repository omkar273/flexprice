package payments

import (
	"context"
	"time"

	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	domainRefund "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// RefundLifecycle manages CAS status transitions for gateway-backed refunds.
// It mirrors the shape of PaymentLifecycle: every method is its own read-then-CAS-write,
// and every status transition inside it is a CAS UPDATE (never a plain read-then-write).
type RefundLifecycle struct {
	refundRepo  domainRefund.Repository
	paymentRepo domainPayment.Repository
	db          postgres.IClient
	logger      *logger.Logger
}

// NewRefundLifecycle returns a RefundLifecycle wired with the given repositories.
func NewRefundLifecycle(
	refundRepo domainRefund.Repository,
	paymentRepo domainPayment.Repository,
	db postgres.IClient,
	log *logger.Logger,
) *RefundLifecycle {
	return &RefundLifecycle{
		refundRepo:  refundRepo,
		paymentRepo: paymentRepo,
		db:          db,
		logger:      log,
	}
}

// RecordRefundSucceeded transitions a refund from any non-terminal status to SUCCEEDED via a CAS UPDATE.
// Accepts PENDING or PROCESSING as source states — the Razorpay fallback-reconciliation path (§7) can
// legitimately observe a PENDING refund that Phase 3 hasn't yet committed to PROCESSING.
// Idempotent: returns nil if the refund is already SUCCEEDED.
func (l *RefundLifecycle) RecordRefundSucceeded(ctx context.Context, gatewayRefundID string) error {
	if gatewayRefundID == "" {
		return ierr.NewError("gateway_refund_id is required").Mark(ierr.ErrValidation)
	}

	ref, err := l.refundRepo.GetByGatewayRefundID(ctx, gatewayRefundID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Refund not found by gateway refund ID").
			WithReportableDetails(map[string]any{"gateway_refund_id": gatewayRefundID}).
			Mark(ierr.ErrNotFound)
	}

	if ref.RefundStatus == types.RefundStatusSucceeded {
		l.logger.Info(ctx, "refund already succeeded, skipping",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	}

	if ref.RefundStatus.IsTerminal() {
		// Per decision 12: a different terminal state than what this call is attempting
		// to write is a genuine disagreement — log as a data-integrity anomaly, not a
		// silent no-op.
		l.logger.Warn(ctx, "refund is in a different terminal state — data-integrity anomaly",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
			"current_status", ref.RefundStatus,
			"attempted_status", types.RefundStatusSucceeded,
		)
		return ierr.NewError("refund is in a terminal state").
			WithHint("Cannot transition to SUCCEEDED from current terminal state; data-integrity anomaly").
			WithReportableDetails(map[string]any{
				"refund_id":        ref.ID,
				"current_status":   ref.RefundStatus,
				"attempted_status": types.RefundStatusSucceeded,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	now := time.Now().UTC()

	// Wrap the CAS update and Payment.PaymentStatus update in a single transaction so
	// they are atomic — the same guarantee confirmSucceeded provides on the inline path.
	return l.db.WithTx(ctx, func(tx context.Context) error {
		updated, err := l.refundRepo.UpdateStatus(
			tx,
			ref.ID,
			ref.RefundStatus,
			types.RefundStatusSucceeded,
			domainRefund.RefundStatusUpdate{SucceededAt: &now},
		)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update refund status to SUCCEEDED").
				WithReportableDetails(map[string]any{"refund_id": ref.ID}).
				Mark(ierr.ErrDatabase)
		}
		if !updated {
			l.logger.Warn(ctx, "CAS update missed for SUCCEEDED (concurrent write advanced the status)",
				"refund_id", ref.ID,
				"gateway_refund_id", gatewayRefundID,
			)
			return nil
		}

		// Update Payment.PaymentStatus to REFUNDED or PARTIALLY_REFUNDED.
		// Lock the payment row within this transaction to serialize with concurrent refunds.
		pmt, pmtErr := l.paymentRepo.GetForUpdate(tx, ref.PaymentID)
		if pmtErr != nil {
			l.logger.Error(ctx, "RecordRefundSucceeded: failed to lock payment for status update — payment status may lag",
				"refund_id", ref.ID,
				"payment_id", ref.PaymentID,
				"error", pmtErr,
			)
			// Don't fail the transaction — the refund itself is confirmed; the payment
			// status inconsistency is recoverable by the recovery sweep.
			return nil
		}

		if pmt.RefundedAmount.GreaterThanOrEqual(pmt.Amount) {
			pmt.PaymentStatus = types.PaymentStatusRefunded
		} else {
			pmt.PaymentStatus = types.PaymentStatusPartiallyRefunded
		}
		pmt.RefundedAt = &now

		if updateErr := l.paymentRepo.Update(tx, pmt); updateErr != nil {
			l.logger.Error(ctx, "RecordRefundSucceeded: failed to update payment status",
				"refund_id", ref.ID,
				"payment_id", ref.PaymentID,
				"error", updateErr,
			)
			// Same rationale: don't roll back the refund CAS for a payment-status lag.
		}

		l.logger.Info(ctx, "refund marked as SUCCEEDED",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	})
}

// RecordRefundFailed transitions a refund from any non-terminal status to FAILED via a CAS UPDATE.
// Releases the Payment.RefundedAmount reservation (decrements by the refund amount) as a best-effort
// operation — the Temporal recovery sweep handles any cases where this decrement fails.
// Idempotent: returns nil if the refund is already FAILED.
func (l *RefundLifecycle) RecordRefundFailed(ctx context.Context, gatewayRefundID string, reason string) error {
	if gatewayRefundID == "" {
		return ierr.NewError("gateway_refund_id is required").Mark(ierr.ErrValidation)
	}

	ref, err := l.refundRepo.GetByGatewayRefundID(ctx, gatewayRefundID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Refund not found by gateway refund ID").
			WithReportableDetails(map[string]any{"gateway_refund_id": gatewayRefundID}).
			Mark(ierr.ErrNotFound)
	}

	if ref.RefundStatus == types.RefundStatusFailed {
		l.logger.Info(ctx, "refund already failed, skipping",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	}

	if ref.RefundStatus.IsTerminal() {
		// Per decision 12: a different terminal state than FAILED is a data-integrity anomaly.
		l.logger.Warn(ctx, "refund is in a different terminal state — data-integrity anomaly",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
			"current_status", ref.RefundStatus,
			"attempted_status", types.RefundStatusFailed,
		)
		return ierr.NewError("refund is in a terminal state").
			WithHint("Cannot transition to FAILED from current terminal state; data-integrity anomaly").
			WithReportableDetails(map[string]any{
				"refund_id":        ref.ID,
				"current_status":   ref.RefundStatus,
				"attempted_status": types.RefundStatusFailed,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	now := time.Now().UTC()
	updated, err := l.refundRepo.UpdateStatus(
		ctx,
		ref.ID,
		ref.RefundStatus,
		types.RefundStatusFailed,
		domainRefund.RefundStatusUpdate{FailureReason: &reason, FailedAt: &now},
	)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update refund status to FAILED").
			WithReportableDetails(map[string]any{"refund_id": ref.ID}).
			Mark(ierr.ErrDatabase)
	}
	if !updated {
		l.logger.Warn(ctx, "CAS update missed for FAILED (concurrent write advanced the status)",
			"refund_id", ref.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	}

	// Release the Payment.RefundedAmount reservation by decrementing by the refund amount.
	// Best-effort: if this fails, the Temporal recovery sweep will reconcile the stale reservation.
	if releaseErr := l.refundRepo.IncrementPaymentRefundedAmount(ctx, ref.PaymentID, ref.Amount.Neg()); releaseErr != nil {
		l.logger.Error(ctx, "failed to release payment refunded amount after refund failure — recovery sweep will reconcile",
			"refund_id", ref.ID,
			"payment_id", ref.PaymentID,
			"amount", ref.Amount.String(),
			"error", releaseErr,
		)
	}

	l.logger.Info(ctx, "refund marked as FAILED",
		"refund_id", ref.ID,
		"gateway_refund_id", gatewayRefundID,
		"reason", reason,
	)
	return nil
}
