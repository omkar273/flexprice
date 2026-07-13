package payments

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// GatewayRefundDispatcher calls the appropriate payment-gateway integration to
// initiate a refund. It is defined here (in the payments sub-package) to break
// the import cycle that would arise if RefundLifecycle imported the parent
// integration package, which itself imports this package.
//
// *integration.Factory implements this interface via its DispatchRefund method
// and registers it with RefundLifecycle through SetRefundDispatcher.
type GatewayRefundDispatcher interface {
	DispatchRefund(
		ctx context.Context,
		gateway string,
		gatewayPaymentID string,
		amount decimal.Decimal,
		gatewayIdempotencyToken string,
	) (gatewayRefundID string, gatewayStatus string, gatewayMetadata map[string]interface{}, err error)
}

// RefundLifecycle handles status transitions for gateway refunds.
//
// Every public method is its own untransacted read-then-write (decision 9 — no
// gateway HTTP call ever happens inside an open DB transaction). Every internal
// status write is a compare-and-swap UPDATE (decision 12) — never a plain
// read-then-write — to be safe under concurrent webhooks, synchronous Phase 3
// responses, and the recovery sweep all racing on the same row.
type RefundLifecycle struct {
	refundRepo  refund.Repository
	paymentRepo payment.Repository
	dispatcher  GatewayRefundDispatcher
	db          postgres.IClient
	logger      *logger.Logger
}

// NewRefundLifecycle constructs a RefundLifecycle. dispatcher may be nil if
// InitiateRefund will not be called (e.g. in tests that only exercise webhook
// recording methods); call SetRefundDispatcher before calling InitiateRefund.
func NewRefundLifecycle(
	refundRepo refund.Repository,
	paymentRepo payment.Repository,
	dispatcher GatewayRefundDispatcher,
	db postgres.IClient,
	logger *logger.Logger,
) *RefundLifecycle {
	return &RefundLifecycle{
		refundRepo:  refundRepo,
		paymentRepo: paymentRepo,
		dispatcher:  dispatcher,
		db:          db,
		logger:      logger,
	}
}

// SetRefundDispatcher wires a GatewayRefundDispatcher after construction.
// Used by *integration.Factory.SetServices to break the DI cycle.
func (l *RefundLifecycle) SetRefundDispatcher(d GatewayRefundDispatcher) {
	l.dispatcher = d
}

// InitiateRefund is Phase 2/3 of the refund creation flow.
//
// The refund row must already be in PENDING state (created atomically in Phase 1
// by RefundService.CreateRefund). This method:
//  1. Fetches the parent payment to get GatewayPaymentID.
//  2. Dispatches the refund call to the appropriate gateway integration.
//  3. On synchronous acceptance: CAS PENDING → PROCESSING (or straight to SUCCEEDED
//     for instant Razorpay refunds), leaving the Payment.RefundedAmount reservation intact.
//  4. On synchronous FAILED response or network error: calls releaseReservation to CAS
//     PENDING → FAILED and decrement Payment.RefundedAmount in one transaction.
//
// No DB transaction is open during the gateway HTTP call (decision 9).
func (l *RefundLifecycle) InitiateRefund(ctx context.Context, r *refund.Refund) error {
	l.logger.Info(ctx, "initiating refund gateway call",
		"refund_id", r.ID,
		"payment_id", r.PaymentID,
		"gateway", r.PaymentGateway,
		"amount", r.Amount.String(),
	)

	if r.ID == "" {
		return ierr.NewError("refund_id is required").Mark(ierr.ErrValidation)
	}
	if r.PaymentID == "" {
		return ierr.NewError("payment_id is required").Mark(ierr.ErrValidation)
	}
	if r.PaymentGateway == "" {
		return ierr.NewError("payment_gateway is required").Mark(ierr.ErrValidation)
	}
	if r.GatewayIdempotencyToken == "" {
		return ierr.NewError("gateway_idempotency_token is required").Mark(ierr.ErrValidation)
	}
	if l.dispatcher == nil {
		return ierr.NewError("gateway dispatcher not configured").
			WithHint("RefundLifecycle.SetRefundDispatcher must be called before InitiateRefund").
			Mark(ierr.ErrSystem)
	}

	// Fetch the parent payment to retrieve GatewayPaymentID.
	pmt, err := l.paymentRepo.Get(ctx, r.PaymentID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch parent payment for refund").
			WithReportableDetails(map[string]any{
				"refund_id":  r.ID,
				"payment_id": r.PaymentID,
			}).
			Mark(ierr.ErrSystem)
	}
	if pmt.GatewayPaymentID == nil || *pmt.GatewayPaymentID == "" {
		return ierr.NewError("payment has no gateway_payment_id").
			WithHint("Cannot issue a gateway refund against a payment with no gateway_payment_id").
			WithReportableDetails(map[string]any{
				"refund_id":  r.ID,
				"payment_id": r.PaymentID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
	gatewayPaymentID := *pmt.GatewayPaymentID

	// Phase 2 — call the gateway (no open transaction).
	gwRefundID, gwStatus, gwMeta, gatewayErr := l.dispatcher.DispatchRefund(
		ctx, r.PaymentGateway, gatewayPaymentID, r.Amount, r.GatewayIdempotencyToken,
	)

	if gatewayErr != nil {
		// Phase 3 — gateway rejected; release the reservation atomically.
		l.logger.Error(ctx, "gateway refund call failed, releasing reservation",
			"refund_id", r.ID,
			"gateway", r.PaymentGateway,
			"error", gatewayErr,
		)

		failureReason := gatewayErr.Error()
		now := time.Now().UTC()
		releaseErr := l.releaseReservation(ctx, r.ID, types.RefundStatusPending, types.RefundStatusFailed,
			refund.RefundStatusUpdate{
				FailureReason:   &failureReason,
				FailedAt:        &now,
				GatewayMetadata: gwMeta,
			},
			r.Amount, r.PaymentID,
		)
		if releaseErr != nil {
			l.logger.Error(ctx, "failed to release refund reservation after gateway error",
				"refund_id", r.ID,
				"release_error", releaseErr,
				"original_gateway_error", gatewayErr,
			)
			// Return the original gateway error; the recovery sweep will surface any stuck reservation.
		}
		return gatewayErr
	}

	// Phase 3 — gateway accepted. Map gateway status to our RefundStatus.
	newStatus, isTerminalSuccess := mapGatewayStatusToRefundStatus(r.PaymentGateway, gwStatus)

	l.logger.Info(ctx, "gateway refund call returned",
		"refund_id", r.ID,
		"gateway_refund_id", gwRefundID,
		"gateway_status", gwStatus,
		"local_status", newStatus,
	)

	// Synchronous terminal failure (e.g. Stripe "failed", Razorpay "failed"): release reservation.
	if newStatus == types.RefundStatusFailed {
		failureReason := gwStatus
		now := time.Now().UTC()
		releaseErr := l.releaseReservation(ctx, r.ID, types.RefundStatusPending, types.RefundStatusFailed,
			refund.RefundStatusUpdate{
				GatewayRefundID: &gwRefundID,
				FailureReason:   &failureReason,
				FailedAt:        &now,
				GatewayMetadata: gwMeta,
			},
			r.Amount, r.PaymentID,
		)
		if releaseErr != nil {
			l.logger.Error(ctx, "failed to release refund reservation after synchronous gateway failure",
				"refund_id", r.ID,
				"gateway_refund_id", gwRefundID,
				"error", releaseErr,
			)
		}
		return ierr.NewError("gateway refund was synchronously rejected").
			WithHint("The gateway returned a terminal failure status for the refund").
			WithReportableDetails(map[string]any{
				"refund_id":        r.ID,
				"gateway_refund_id": gwRefundID,
				"gateway_status":   gwStatus,
			}).
			Mark(ierr.ErrSystem)
	}

	// CAS PENDING → newStatus (PROCESSING or SUCCEEDED).
	now := time.Now().UTC()
	update := refund.RefundStatusUpdate{
		GatewayRefundID: &gwRefundID,
		GatewayMetadata: gwMeta,
	}
	if isTerminalSuccess {
		update.SucceededAt = &now
	}

	updated, casErr := l.refundRepo.UpdateStatus(ctx, r.ID, types.RefundStatusPending, newStatus, update)
	if casErr != nil {
		l.logger.Error(ctx, "failed to CAS refund status after gateway acceptance",
			"refund_id", r.ID,
			"gateway_refund_id", gwRefundID,
			"error", casErr,
		)
		return ierr.WithError(casErr).
			WithHint("Gateway refund was accepted but the local status update failed").
			WithReportableDetails(map[string]any{
				"refund_id":         r.ID,
				"gateway_refund_id": gwRefundID,
				"attempted_status":  string(newStatus),
			}).
			Mark(ierr.ErrSystem)
	}

	if !updated {
		// 0 rows affected — a concurrent writer already advanced this row.
		// Decision 12: check whether it converged to the same state (ordinary idempotence)
		// or a different terminal state (data-integrity anomaly).
		current, readErr := l.refundRepo.Get(ctx, r.ID)
		if readErr != nil {
			l.logger.Error(ctx, "CAS returned 0 rows; could not re-read refund for anomaly check",
				"refund_id", r.ID,
				"attempted_status", string(newStatus),
				"error", readErr,
			)
			return nil
		}
		if current.RefundStatus == newStatus {
			l.logger.Info(ctx, "refund status already at target (concurrent writer), CAS no-op",
				"refund_id", r.ID,
				"status", current.RefundStatus,
			)
		} else {
			l.logger.Error(ctx, "refund status anomaly: CAS found different terminal state than expected (decision 12)",
				"refund_id", r.ID,
				"attempted_status", string(newStatus),
				"found_status", current.RefundStatus,
				"gateway_refund_id", gwRefundID,
			)
		}
		return nil
	}

	// CAS won. For instant terminal success, update Payment.PaymentStatus.
	if isTerminalSuccess {
		if err := l.updatePaymentAfterSucceeded(ctx, r.PaymentID, &now); err != nil {
			l.logger.Error(ctx, "refund succeeded but payment status update failed (non-fatal)",
				"refund_id", r.ID,
				"payment_id", r.PaymentID,
				"error", err,
			)
			// Non-fatal: refund row is correctly SUCCEEDED; payment display status will be
			// reconciled by the next webhook or recovery sweep.
		}
	}

	l.logger.Info(ctx, "refund phase 3 complete",
		"refund_id", r.ID,
		"gateway_refund_id", gwRefundID,
		"status", newStatus,
	)

	return nil
}

// RecordRefundSucceeded transitions the refund from PENDING or PROCESSING to SUCCEEDED.
//
// Accepts both PENDING and PROCESSING as source states — the Razorpay fallback
// reconciliation path can legitimately observe a PENDING row that has not yet
// reached PROCESSING (Phase 3 has not committed). Does NOT re-increment
// Payment.RefundedAmount (already reserved at Phase 1). Only flips
// Payment.PaymentStatus and sets Payment.RefundedAt.
//
// Decision 12 carve-out: if 0 rows and the current status is already SUCCEEDED,
// this is harmless idempotent convergence. If the current status is a different
// terminal state, that is a genuine gateway/system disagreement — logged as anomaly.
func (l *RefundLifecycle) RecordRefundSucceeded(ctx context.Context, gatewayRefundID string) error {
	l.logger.Info(ctx, "recording refund succeeded",
		"gateway_refund_id", gatewayRefundID,
	)

	if gatewayRefundID == "" {
		return ierr.NewError("gateway_refund_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.refundRepo.GetByGatewayRefundID(ctx, gatewayRefundID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to find refund by gateway_refund_id").
			WithReportableDetails(map[string]any{
				"gateway_refund_id": gatewayRefundID,
			}).
			Mark(ierr.ErrSystem)
	}

	// Already SUCCEEDED — idempotent no-op.
	if existing.RefundStatus == types.RefundStatusSucceeded {
		l.logger.Info(ctx, "refund already succeeded, skipping",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	}

	// Different terminal state — data-integrity anomaly (decision 12).
	if existing.RefundStatus.IsTerminal() {
		l.logger.Error(ctx, "refund status anomaly: RecordRefundSucceeded found a different terminal state",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
			"current_status", existing.RefundStatus,
		)
		return ierr.NewError("refund is in an unexpected terminal state").
			WithHint("Gateway reported refund succeeded but local record is in a different terminal state").
			WithReportableDetails(map[string]any{
				"refund_id":         existing.ID,
				"gateway_refund_id": gatewayRefundID,
				"current_status":    string(existing.RefundStatus),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// CAS from current non-terminal status → SUCCEEDED.
	now := time.Now().UTC()
	updated, err := l.refundRepo.UpdateStatus(ctx, existing.ID, existing.RefundStatus, types.RefundStatusSucceeded,
		refund.RefundStatusUpdate{
			SucceededAt: &now,
		},
	)
	if err != nil {
		l.logger.Error(ctx, "failed to CAS refund to SUCCEEDED",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record refund as succeeded").
			WithReportableDetails(map[string]any{
				"refund_id":         existing.ID,
				"gateway_refund_id": gatewayRefundID,
			}).
			Mark(ierr.ErrSystem)
	}

	if !updated {
		// 0 rows — apply decision 12 carve-out.
		current, readErr := l.refundRepo.Get(ctx, existing.ID)
		if readErr != nil {
			l.logger.Error(ctx, "CAS to SUCCEEDED returned 0 rows; cannot verify current state",
				"refund_id", existing.ID,
				"error", readErr,
			)
			return nil
		}
		if current.RefundStatus == types.RefundStatusSucceeded {
			l.logger.Info(ctx, "refund already SUCCEEDED by concurrent writer, treating as no-op",
				"refund_id", existing.ID,
				"gateway_refund_id", gatewayRefundID,
			)
			return nil
		}
		l.logger.Error(ctx, "refund status anomaly: CAS to SUCCEEDED found different state (decision 12)",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
			"found_status", current.RefundStatus,
		)
		return ierr.NewError("refund status anomaly after concurrent write").
			WithHint("CAS to SUCCEEDED found a different terminal state in the database").
			WithReportableDetails(map[string]any{
				"refund_id":         existing.ID,
				"gateway_refund_id": gatewayRefundID,
				"found_status":      string(current.RefundStatus),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Update Payment.PaymentStatus and RefundedAt — no re-increment (reservation intact from Phase 1).
	if err := l.updatePaymentAfterSucceeded(ctx, existing.PaymentID, &now); err != nil {
		l.logger.Error(ctx, "refund marked SUCCEEDED but payment status update failed (non-fatal)",
			"refund_id", existing.ID,
			"payment_id", existing.PaymentID,
			"error", err,
		)
		// Non-fatal: refund is correctly SUCCEEDED; payment display status is eventually consistent.
	}

	l.logger.Info(ctx, "refund marked as SUCCEEDED",
		"refund_id", existing.ID,
		"gateway_refund_id", gatewayRefundID,
		"payment_id", existing.PaymentID,
	)

	return nil
}

// RecordRefundFailed transitions the refund from any non-terminal status to FAILED and
// releases the Payment.RefundedAmount reservation (decision 12 — same shared
// releaseReservation helper as RecordRefundCancelled).
//
// Idempotent: if the refund is already FAILED, logs and returns nil.
// Data-integrity anomaly: if the refund is in a different terminal state, logs an error
// and returns an error (not silently swallowed — decision 12).
func (l *RefundLifecycle) RecordRefundFailed(ctx context.Context, gatewayRefundID, reason string) error {
	l.logger.Info(ctx, "recording refund failed",
		"gateway_refund_id", gatewayRefundID,
		"reason", reason,
	)

	if gatewayRefundID == "" {
		return ierr.NewError("gateway_refund_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.refundRepo.GetByGatewayRefundID(ctx, gatewayRefundID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to find refund by gateway_refund_id").
			WithReportableDetails(map[string]any{
				"gateway_refund_id": gatewayRefundID,
			}).
			Mark(ierr.ErrSystem)
	}

	// Already FAILED — idempotent no-op.
	if existing.RefundStatus == types.RefundStatusFailed {
		l.logger.Info(ctx, "refund already FAILED, skipping",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
		)
		return nil
	}

	// Different terminal state — data-integrity anomaly (decision 12).
	if existing.RefundStatus.IsTerminal() {
		l.logger.Error(ctx, "refund status anomaly: RecordRefundFailed found a different terminal state",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
			"current_status", existing.RefundStatus,
		)
		return ierr.NewError("refund is in an unexpected terminal state").
			WithHint("Gateway reported refund failed but local record is in a different terminal state").
			WithReportableDetails(map[string]any{
				"refund_id":         existing.ID,
				"gateway_refund_id": gatewayRefundID,
				"current_status":    string(existing.RefundStatus),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	now := time.Now().UTC()
	if err := l.releaseReservation(ctx,
		existing.ID, existing.RefundStatus, types.RefundStatusFailed,
		refund.RefundStatusUpdate{
			FailureReason: &reason,
			FailedAt:      &now,
		},
		existing.Amount, existing.PaymentID,
	); err != nil {
		l.logger.Error(ctx, "failed to release refund reservation on failure",
			"refund_id", existing.ID,
			"gateway_refund_id", gatewayRefundID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record refund failure and release reservation").
			WithReportableDetails(map[string]any{
				"refund_id":         existing.ID,
				"gateway_refund_id": gatewayRefundID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "refund marked as FAILED, reservation released",
		"refund_id", existing.ID,
		"gateway_refund_id", gatewayRefundID,
		"payment_id", existing.PaymentID,
		"amount", existing.Amount.String(),
		"reason", reason,
	)

	return nil
}

// RecordRefundCancelled transitions the refund from PROCESSING (only) to CANCELLED and
// releases the Payment.RefundedAmount reservation via the same shared helper as
// RecordRefundFailed.
//
// PENDING is deliberately excluded — a PENDING refund has no GatewayRefundID yet,
// so there is nothing for the gateway to confirm cancellation against. The /cancel API
// handler rejects PENDING rows outright and directs callers to retry CreateRefund instead
// (which safely resolves the ambiguity via the crash-recovery table, §5).
//
// Must only be called after the gateway has confirmed that cancellation actually applied
// (see §6 — the gateway is the authoritative arbiter, not local CAS alone).
func (l *RefundLifecycle) RecordRefundCancelled(ctx context.Context, refundID string) error {
	l.logger.Info(ctx, "recording refund cancelled",
		"refund_id", refundID,
	)

	if refundID == "" {
		return ierr.NewError("refund_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.refundRepo.Get(ctx, refundID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to find refund for cancellation").
			WithReportableDetails(map[string]any{
				"refund_id": refundID,
			}).
			Mark(ierr.ErrSystem)
	}

	// Already CANCELLED — idempotent no-op.
	if existing.RefundStatus == types.RefundStatusCancelled {
		l.logger.Info(ctx, "refund already CANCELLED, skipping",
			"refund_id", refundID,
		)
		return nil
	}

	// Only PROCESSING is a valid source state for cancellation (spec §5).
	// PENDING has no GatewayRefundID — cancellation against it would be a purely-local
	// mutation with no gateway confirmation, which is exactly the double-payout risk
	// this design exists to prevent.
	if existing.RefundStatus != types.RefundStatusProcessing {
		l.logger.Error(ctx, "refund cancellation rejected: not in PROCESSING state",
			"refund_id", refundID,
			"current_status", existing.RefundStatus,
		)
		return ierr.NewError("refund is not in PROCESSING state").
			WithHint("Cancellation requires a PROCESSING refund (one with a GatewayRefundID). For PENDING refunds, retry CreateRefund instead to safely resolve via the crash-recovery path.").
			WithReportableDetails(map[string]any{
				"refund_id":      refundID,
				"current_status": string(existing.RefundStatus),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	now := time.Now().UTC()
	if err := l.releaseReservation(ctx,
		existing.ID, types.RefundStatusProcessing, types.RefundStatusCancelled,
		refund.RefundStatusUpdate{
			CancelledAt: &now,
		},
		existing.Amount, existing.PaymentID,
	); err != nil {
		l.logger.Error(ctx, "failed to release refund reservation on cancellation",
			"refund_id", refundID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record refund cancellation and release reservation").
			WithReportableDetails(map[string]any{
				"refund_id": refundID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "refund marked as CANCELLED, reservation released",
		"refund_id", refundID,
		"payment_id", existing.PaymentID,
		"amount", existing.Amount.String(),
	)

	return nil
}

// releaseReservation atomically:
//  1. CAS the refund row from expectedStatus to newStatus (with the provided field updates).
//  2. If the CAS wins, decrements Payment.RefundedAmount by amount under a SELECT FOR
//     UPDATE lock — both writes run in the same short transaction (decision 12).
//
// If the CAS returns 0 rows (a concurrent writer already moved the row), the decrement
// is skipped. The concurrent writer is responsible for its own release.
func (l *RefundLifecycle) releaseReservation(
	ctx context.Context,
	refundID string,
	expectedStatus types.RefundStatus,
	newStatus types.RefundStatus,
	update refund.RefundStatusUpdate,
	amount decimal.Decimal,
	paymentID string,
) error {
	return l.db.WithTx(ctx, func(txCtx context.Context) error {
		updated, err := l.refundRepo.UpdateStatus(txCtx, refundID, expectedStatus, newStatus, update)
		if err != nil {
			return err
		}
		if !updated {
			// CAS found a different status — log if it's not the target (anomaly check).
			current, readErr := l.refundRepo.Get(txCtx, refundID)
			if readErr == nil && current.RefundStatus != newStatus {
				l.logger.Error(txCtx, "releaseReservation CAS found unexpected state",
					"refund_id", refundID,
					"expected", string(expectedStatus),
					"attempted", string(newStatus),
					"found", string(current.RefundStatus),
				)
			}
			return nil
		}

		// CAS won — decrement Payment.RefundedAmount (negative delta releases the reservation).
		return l.refundRepo.IncrementPaymentRefundedAmount(txCtx, paymentID, amount.Neg())
	})
}

// updatePaymentAfterSucceeded updates Payment.PaymentStatus to REFUNDED (if
// RefundedAmount >= Amount) or PARTIALLY_REFUNDED (otherwise) and sets RefundedAt.
// Called after a refund row transitions to SUCCEEDED — does NOT modify RefundedAmount
// (the reservation was taken at Phase 1 and stands). Non-transactional; any update
// failure is non-fatal since payment display status is eventually consistent.
func (l *RefundLifecycle) updatePaymentAfterSucceeded(ctx context.Context, paymentID string, succeededAt *time.Time) error {
	pmt, err := l.paymentRepo.Get(ctx, paymentID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch payment for post-refund status update").
			Mark(ierr.ErrSystem)
	}

	if pmt.RefundedAmount.GreaterThanOrEqual(pmt.Amount) {
		pmt.PaymentStatus = types.PaymentStatusRefunded
	} else {
		pmt.PaymentStatus = types.PaymentStatusPartiallyRefunded
	}

	if pmt.RefundedAt == nil && succeededAt != nil {
		pmt.RefundedAt = succeededAt
	}

	if err := l.paymentRepo.Update(ctx, pmt); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update payment status after refund succeeded").
			WithReportableDetails(map[string]any{
				"payment_id":      paymentID,
				"payment_status":  string(pmt.PaymentStatus),
				"refunded_amount": pmt.RefundedAmount.String(),
				"amount":          pmt.Amount.String(),
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "payment status updated after refund succeeded",
		"payment_id", paymentID,
		"payment_status", pmt.PaymentStatus,
		"refunded_amount", pmt.RefundedAmount.String(),
		"amount", pmt.Amount.String(),
	)

	return nil
}

// mapGatewayStatusToRefundStatus maps a raw gateway status string to the corresponding
// RefundStatus and whether it represents an instant terminal success.
//
// Stripe statuses: "pending" → PROCESSING, "succeeded" → SUCCEEDED,
// "failed"/"canceled" → FAILED.
// Razorpay statuses: "pending" → PROCESSING, "processed" → SUCCEEDED, "failed" → FAILED.
// Unrecognised statuses default to PROCESSING (conservative — reconciled by webhook).
func mapGatewayStatusToRefundStatus(gateway, gatewayStatus string) (types.RefundStatus, bool) {
	switch gateway {
	case string(types.PaymentGatewayTypeStripe):
		switch gatewayStatus {
		case "succeeded":
			return types.RefundStatusSucceeded, true
		case "failed", "canceled":
			return types.RefundStatusFailed, false
		default: // "pending" and any unknown
			return types.RefundStatusProcessing, false
		}

	case string(types.PaymentGatewayTypeRazorpay):
		switch gatewayStatus {
		case "processed":
			return types.RefundStatusSucceeded, true
		case "failed":
			return types.RefundStatusFailed, false
		default: // "pending" and any unknown
			return types.RefundStatusProcessing, false
		}

	default:
		return types.RefundStatusProcessing, false
	}
}

