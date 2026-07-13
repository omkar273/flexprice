package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainrefund "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// claimStalenessThreshold is the maximum age of a PENDING Refund's ClaimedAt before
// another caller may CAS-reclaim it (long enough to survive a normal gateway RTT).
const claimStalenessThreshold = 30 * time.Second

// resumeFreshnessWindow is how far from InitiatedAt automatic resume of a stale claim is
// allowed. Past this boundary the caller receives a manual-reconciliation error (decision 13).
const resumeFreshnessWindow = 15 * time.Minute

// RefundService is the domain service for gateway refund creation and lifecycle.
type RefundService interface {
	CreateRefund(ctx context.Context, req *dto.CreateRefundRequest) (*dto.RefundResponse, error)
	GetRefund(ctx context.Context, id string) (*dto.RefundResponse, error)
	ListRefunds(ctx context.Context, filter *types.RefundFilter) (*dto.ListRefundsResponse, error)
	// CancelRefund cancels a PROCESSING refund via the gateway (Stripe only).
	// Calls the gateway first; local state is only updated after gateway confirmation.
	CancelRefund(ctx context.Context, id string) (*dto.RefundResponse, error)
}

type refundService struct {
	ServiceParams
}

// NewRefundService constructs a RefundService.
// ServiceParams must have RefundRepo and RefundWebhookEventRepo set.
func NewRefundService(params ServiceParams) RefundService {
	return &refundService{ServiceParams: params}
}

// deriveGatewayIdempotencyToken produces a deterministic, format-safe token to send to the
// gateway's own idempotency mechanism (Stripe: Idempotency-Key header; Razorpay:
// X-Razorpay-Idempotency-Key header). Deriving rather than forwarding the raw client key
// sidesteps gateway format requirements while keeping crash-retries safe: same inputs →
// same token → gateway treats the repeat as a no-op (decision 11).
func deriveGatewayIdempotencyToken(refundID, idempotencyKey string) string {
	h := sha256.Sum256([]byte(refundID + ":" + idempotencyKey))
	return hex.EncodeToString(h[:])
}

// gatewayRefundResult is an internal normalised outcome from any gateway's RefundPayment call.
type gatewayRefundResult struct {
	GatewayRefundID string
	// gatewayStatus is the raw string the gateway returned (e.g. "pending", "processed",
	// "succeeded", "failed"). Mapped to types.RefundStatus by mapGatewayStatus.
	GatewayStatus   string
	GatewayMetadata map[string]interface{}
}

// mapGatewayStatus translates a gateway-specific status string to a domain RefundStatus.
// Stripe: "pending" → PROCESSING; "succeeded" → SUCCEEDED; "failed"/"canceled" → FAILED/CANCELLED.
// Razorpay: "pending" → PROCESSING; "processed" → SUCCEEDED; "failed" → FAILED.
func mapGatewayStatus(gateway, gatewayStatus string) types.RefundStatus {
	switch types.PaymentGatewayType(gateway) {
	case types.PaymentGatewayTypeStripe:
		switch gatewayStatus {
		case "succeeded":
			return types.RefundStatusSucceeded
		case "failed":
			return types.RefundStatusFailed
		case "canceled":
			return types.RefundStatusCancelled
		default: // "pending" and anything else
			return types.RefundStatusProcessing
		}
	case types.PaymentGatewayTypeRazorpay:
		switch gatewayStatus {
		case "processed":
			return types.RefundStatusSucceeded
		case "failed":
			return types.RefundStatusFailed
		default: // "pending"
			return types.RefundStatusProcessing
		}
	default:
		return types.RefundStatusProcessing
	}
}

// --- CreateRefund -----------------------------------------------------------------

// CreateRefund implements the atomic claim → gateway call → confirm/release flow
// described in spec §5.  No DB transaction is held open across the gateway HTTP call
// (decision 9).
func (s *refundService) CreateRefund(ctx context.Context, req *dto.CreateRefundRequest) (*dto.RefundResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.PaymentID == "" {
		return nil, ierr.NewError("payment_id is required").
			WithHint("Provide the payment ID to refund").
			Mark(ierr.ErrValidation)
	}

	// ── Phase 1 step 1: Idempotency pre-check (outside any transaction) ─────────
	// This is a best-effort fast-path; correctness does not depend on it seeing
	// consistent state — the atomic INSERT below is the authoritative guard (decision 12).
	existing, preCheckErr := s.RefundRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if preCheckErr != nil && !ierr.IsNotFound(preCheckErr) {
		return nil, ierr.WithError(preCheckErr).
			WithHint("Failed to check for an existing refund with this idempotency key").
			Mark(ierr.ErrInternal)
	}
	if existing != nil {
		s.Logger.Info(ctx, "refund pre-check: found existing row, applying crash-recovery",
			"refund_id", existing.ID,
			"status", existing.RefundStatus,
			"idempotency_key", req.IdempotencyKey,
		)
		return s.applyCrashRecovery(ctx, existing, req)
	}

	// ── Phase 1 steps 3-5: transaction, lock payment, atomic INSERT ───────────────
	var (
		claimedRefund *domainrefund.Refund // row this request owns after the tx
		weWon         bool                 // true iff this request's INSERT created the row
	)

	txErr := s.DB.WithTx(ctx, func(tx context.Context) error {
		// 3a. Lock the payment row for the duration of this transaction (decision 8/10).
		pmt, lockErr := s.PaymentRepo.GetForUpdate(tx, req.PaymentID)
		if lockErr != nil {
			return lockErr
		}

		// 3b. Validate tenant/environment match.
		if pmt.TenantID != types.GetTenantID(ctx) || pmt.EnvironmentID != types.GetEnvironmentID(ctx) {
			return ierr.NewError("payment not found").
				WithHint("Payment does not belong to this tenant or environment").
				Mark(ierr.ErrNotFound)
		}

		// 3c. Validate the payment is in a refundable status.
		if pmt.PaymentStatus != types.PaymentStatusSucceeded &&
			pmt.PaymentStatus != types.PaymentStatusPartiallyRefunded {
			return ierr.NewError("payment is not eligible for refund").
				WithHint("Only SUCCEEDED or PARTIALLY_REFUNDED payments can be refunded").
				WithReportableDetails(map[string]any{"payment_status": pmt.PaymentStatus}).
				Mark(ierr.ErrValidation)
		}

		// 3d. Validate the payment has a gateway (required for gateway refunds).
		if pmt.PaymentGateway == nil || *pmt.PaymentGateway == "" {
			return ierr.NewError("payment has no gateway").
				WithHint("Refunds require a payment processed via a supported gateway").
				Mark(ierr.ErrValidation)
		}
		if pmt.GatewayPaymentID == nil || *pmt.GatewayPaymentID == "" {
			return ierr.NewError("payment has no gateway payment ID").
				WithHint("Refunds require a payment with a gateway-assigned payment ID").
				Mark(ierr.ErrValidation)
		}

		// 3e. Enforce the refund cap under the row lock (decisions 8, 10).
		remaining := pmt.Amount.Sub(pmt.RefundedAmount)
		if req.Amount.GreaterThan(remaining) {
			return ierr.NewError("refund amount exceeds remaining refundable balance").
				WithHint("The requested amount exceeds the payment balance available for refund").
				WithReportableDetails(map[string]any{
					"requested": req.Amount,
					"remaining": remaining,
					"payment_amount": pmt.Amount,
					"already_refunded": pmt.RefundedAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		// 3f. Build the refund row.  ID and timestamps are Go-computed so we get a
		// complete struct without round-tripping RETURNING columns (spec §5 note).
		now := time.Now().UTC()
		refundID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REFUND)
		gatewayToken := deriveGatewayIdempotencyToken(refundID, req.IdempotencyKey)

		newRefund := &domainrefund.Refund{
			ID:                      refundID,
			PaymentID:               pmt.ID,
			PaymentGateway:          *pmt.PaymentGateway,
			Amount:                  req.Amount,
			Currency:                pmt.Currency,
			RefundStatus:            types.RefundStatusPending,
			RefundReason:            req.Reason,
			IdempotencyKey:          req.IdempotencyKey,
			GatewayIdempotencyToken: gatewayToken,
			Metadata:                req.Metadata,
			InitiatedAt:             &now,
			ClaimedAt:               &now,
			EnvironmentID:           types.GetEnvironmentID(ctx),
			BaseModel: types.BaseModel{
				TenantID:  types.GetTenantID(ctx),
				Status:    types.StatusPublished,
				CreatedBy: types.GetUserID(ctx),
				UpdatedBy: types.GetUserID(ctx),
			},
		}

		// 3g. Atomic INSERT ... ON CONFLICT (tenant_id, environment_id, idempotency_key) DO NOTHING.
		// The repository must use raw SQL here (Ent's sql/upsert feature is not enabled —
		// decision 12).  created=false means a concurrent request already owns this key.
		_, created, createErr := s.RefundRepo.Create(tx, newRefund)
		if createErr != nil {
			return createErr
		}

		if !created {
			// Step 6: concurrent request won the INSERT race.  Re-SELECT the winning row
			// and apply the crash-recovery table after the transaction.
			s.Logger.Info(ctx, "refund INSERT lost race; re-fetching winning row",
				"idempotency_key", req.IdempotencyKey,
			)
			winner, fetchErr := s.RefundRepo.GetByIdempotencyKey(tx, req.IdempotencyKey)
			if fetchErr != nil {
				return fetchErr
			}
			claimedRefund = winner
			weWon = false
			return nil
		}

		// Step 5: this request won — increment Payment.RefundedAmount under the same lock.
		if incrErr := s.RefundRepo.IncrementPaymentRefundedAmount(tx, pmt.ID, req.Amount); incrErr != nil {
			return incrErr
		}
		claimedRefund = newRefund
		weWon = true
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	// If the INSERT race was lost, apply crash-recovery to the winning row.
	if !weWon {
		s.Logger.Info(ctx, "applying crash-recovery after losing INSERT race",
			"winning_refund_id", claimedRefund.ID,
			"status", claimedRefund.RefundStatus,
		)
		return s.applyCrashRecovery(ctx, claimedRefund, req)
	}

	// ── Phase 2: call the gateway (no open transaction) ─────────────────────────
	s.Logger.Info(ctx, "Phase 2: calling gateway",
		"refund_id", claimedRefund.ID,
		"gateway", claimedRefund.PaymentGateway,
		"amount", claimedRefund.Amount.String(),
	)

	gwResult, gwErr := s.callGateway(ctx, claimedRefund)

	// ── Phase 3: confirm or release ──────────────────────────────────────────────
	if gwErr != nil {
		s.Logger.Error(ctx, "gateway refund call failed; releasing reservation",
			"refund_id", claimedRefund.ID,
			"error", gwErr,
		)
		// Release: CAS PENDING→FAILED + decrement Payment.RefundedAmount.
		s.releaseReservation(ctx, claimedRefund, gwErr.Error())

		return nil, ierr.WithError(gwErr).
			WithHint("Gateway refused or errored on the refund request").
			WithReportableDetails(map[string]any{"refund_id": claimedRefund.ID}).
			Mark(ierr.ErrSystem)
	}

	finalRefund, confirmErr := s.confirmGatewayResult(ctx, claimedRefund, gwResult)
	if confirmErr != nil {
		return nil, confirmErr
	}

	// Step 10: publish webhook event.
	s.publishRefundEvent(ctx, types.WebhookEventRefundCreated, finalRefund.ID)

	return dto.NewRefundResponse(finalRefund), nil
}

// --- crash-recovery table --------------------------------------------------------

// applyCrashRecovery implements the state-machine table from spec §5 for cases where
// an existing Refund row was found before or during Phase 1.
func (s *refundService) applyCrashRecovery(
	ctx context.Context,
	existing *domainrefund.Refund,
	req *dto.CreateRefundRequest,
) (*dto.RefundResponse, error) {

	// Params-differ check (decision 12 / spec table last row).
	if !existing.Amount.Equal(req.Amount) ||
		existing.RefundReason != req.Reason ||
		existing.PaymentID != req.PaymentID {
		return nil, ierr.NewError("idempotency key conflict: request parameters differ from original").
			WithHint("An existing refund with this idempotency_key has different parameters; use a unique key per unique request").
			WithReportableDetails(map[string]any{
				"existing_refund_id": existing.ID,
				"existing_amount":    existing.Amount,
				"existing_reason":    existing.RefundReason,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	switch existing.RefundStatus {
	// Terminal / post-gateway states → plain idempotent replay (200 OK).
	case types.RefundStatusProcessing,
		types.RefundStatusSucceeded,
		types.RefundStatusFailed,
		types.RefundStatusCancelled:
		s.Logger.Info(ctx, "idempotent replay: refund already in terminal/post-gateway state",
			"refund_id", existing.ID,
			"status", existing.RefundStatus,
		)
		return dto.NewRefundResponse(existing), nil

	case types.RefundStatusPending:
		return s.handlePendingCrashRecovery(ctx, existing, req)
	}

	// Unknown status – treat as terminal replay to avoid stuck state.
	s.Logger.Warn(ctx, "crash-recovery: unexpected refund status, returning existing row",
		"refund_id", existing.ID,
		"status", existing.RefundStatus,
	)
	return dto.NewRefundResponse(existing), nil
}

// handlePendingCrashRecovery applies the PENDING rows of the crash-recovery table.
func (s *refundService) handlePendingCrashRecovery(
	ctx context.Context,
	existing *domainrefund.Refund,
	_ *dto.CreateRefundRequest,
) (*dto.RefundResponse, error) {

	now := time.Now().UTC()

	// Check resume-freshness window (decision 13).
	if existing.InitiatedAt == nil || now.Sub(*existing.InitiatedAt) > resumeFreshnessWindow {
		s.Logger.Warn(ctx, "crash-recovery: outside resume-freshness window; directing to manual reconciliation",
			"refund_id", existing.ID,
			"initiated_at", existing.InitiatedAt,
		)
		return nil, ierr.NewError("refund is stale and outside the automatic-resume window").
			WithHint("This refund was initiated more than 15 minutes ago and requires manual reconciliation; contact support with the refund_id").
			WithReportableDetails(map[string]any{
				"refund_id":   existing.ID,
				"initiated_at": existing.InitiatedAt,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Check claim-staleness threshold.
	if existing.ClaimedAt != nil && now.Sub(*existing.ClaimedAt) <= claimStalenessThreshold {
		// Claim is fresh → a gateway call is genuinely in flight.
		s.Logger.Info(ctx, "crash-recovery: claim is fresh, gateway call in flight",
			"refund_id", existing.ID,
			"claimed_at", existing.ClaimedAt,
		)
		return nil, ierr.NewError("refund is already being processed").
			WithHint("A refund with this idempotency_key is already in flight; retry after a short delay").
			WithReportableDetails(map[string]any{"refund_id": existing.ID}).
			Mark(ierr.ErrAlreadyExists)
	}

	// Claim is stale (or ClaimedAt is nil) → attempt CAS-reclaim.
	// NOTE: UpdateStatus(PENDING→PENDING, ClaimedAt=now) reclaims the row.
	// The repository implementation SHOULD add `AND claimed_at < now() - interval '30 seconds'`
	// to the WHERE clause to prevent two concurrent reclaimers both winning.
	s.Logger.Info(ctx, "crash-recovery: claim is stale, attempting CAS-reclaim",
		"refund_id", existing.ID,
		"claimed_at", existing.ClaimedAt,
	)

	newClaimedAt := now
	reclaimed, reclaimErr := s.RefundRepo.UpdateStatus(
		ctx,
		existing.ID,
		types.RefundStatusPending, // expectedStatus
		types.RefundStatusPending, // newStatus (keep PENDING, just refresh ClaimedAt)
		domainrefund.RefundStatusUpdate{ClaimedAt: &newClaimedAt},
	)
	if reclaimErr != nil {
		return nil, ierr.WithError(reclaimErr).
			WithHint("Failed to reclaim stale refund").
			Mark(ierr.ErrInternal)
	}
	if !reclaimed {
		// Another concurrent reclaimer won.
		s.Logger.Info(ctx, "crash-recovery: CAS-reclaim lost to another caller",
			"refund_id", existing.ID,
		)
		return nil, ierr.NewError("refund reclaim race: another caller won the reclaim").
			WithHint("A concurrent request reclaimed this refund; retry after a short delay").
			WithReportableDetails(map[string]any{"refund_id": existing.ID}).
			Mark(ierr.ErrAlreadyExists)
	}

	// We won the reclaim — proceed to Phase 2 using the stored GatewayIdempotencyToken.
	s.Logger.Info(ctx, "crash-recovery: reclaimed stale PENDING refund, retrying gateway call",
		"refund_id", existing.ID,
		"gateway_idempotency_token", existing.GatewayIdempotencyToken,
	)

	gwResult, gwErr := s.callGateway(ctx, existing)
	if gwErr != nil {
		s.Logger.Error(ctx, "crash-recovery Phase 2: gateway call failed; releasing reservation",
			"refund_id", existing.ID,
			"error", gwErr,
		)
		s.releaseReservation(ctx, existing, gwErr.Error())
		return nil, ierr.WithError(gwErr).
			WithHint("Gateway refused the retried refund request").
			WithReportableDetails(map[string]any{"refund_id": existing.ID}).
			Mark(ierr.ErrSystem)
	}

	finalRefund, confirmErr := s.confirmGatewayResult(ctx, existing, gwResult)
	if confirmErr != nil {
		return nil, confirmErr
	}

	s.publishRefundEvent(ctx, types.WebhookEventRefundUpdated, finalRefund.ID)
	return dto.NewRefundResponse(finalRefund), nil
}

// --- Phase 2: gateway call -------------------------------------------------------

// callGateway resolves the correct integration from the factory and calls RefundPayment.
// Must be called with NO open DB transaction (decision 9).
func (s *refundService) callGateway(ctx context.Context, refund *domainrefund.Refund) (*gatewayRefundResult, error) {
	// Re-fetch the payment to get the latest GatewayPaymentID (the locked copy from
	// Phase 1 is no longer in scope here).
	pmt, err := s.PaymentRepo.Get(ctx, refund.PaymentID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Could not retrieve payment to resolve gateway payment ID").
			Mark(ierr.ErrInternal)
	}
	if pmt.GatewayPaymentID == nil || *pmt.GatewayPaymentID == "" {
		return nil, ierr.NewError("payment has no gateway payment ID").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Info(ctx, "calling gateway for refund",
		"refund_id", refund.ID,
		"gateway", refund.PaymentGateway,
		"gateway_payment_id", *pmt.GatewayPaymentID,
		"gateway_idempotency_token", refund.GatewayIdempotencyToken,
	)

	switch types.PaymentGatewayType(refund.PaymentGateway) {
	case types.PaymentGatewayTypeStripe:
		integration, integErr := s.IntegrationFactory.GetStripeIntegration(ctx)
		if integErr != nil {
			return nil, ierr.WithError(integErr).
				WithHint("Failed to initialize Stripe integration").
				Mark(ierr.ErrInternal)
		}
		result, refundErr := integration.PaymentSvc.RefundPayment(
			ctx,
			*pmt.GatewayPaymentID,
			refund.Amount,
			refund.GatewayIdempotencyToken,
		)
		if refundErr != nil {
			return nil, refundErr
		}
		return &gatewayRefundResult{
			GatewayRefundID: result.GatewayRefundID,
			GatewayStatus:   result.Status,
			GatewayMetadata: result.GatewayMetadata,
		}, nil

	case types.PaymentGatewayTypeRazorpay:
		integration, integErr := s.IntegrationFactory.GetRazorpayIntegration(ctx)
		if integErr != nil {
			return nil, ierr.WithError(integErr).
				WithHint("Failed to initialize Razorpay integration").
				Mark(ierr.ErrInternal)
		}
		result, refundErr := integration.PaymentSvc.RefundPayment(
			ctx,
			*pmt.GatewayPaymentID,
			refund.Amount,
			refund.GatewayIdempotencyToken,
		)
		if refundErr != nil {
			return nil, refundErr
		}
		return &gatewayRefundResult{
			GatewayRefundID: result.GatewayRefundID,
			GatewayStatus:   result.Status,
			GatewayMetadata: result.GatewayMetadata,
		}, nil

	default:
		return nil, ierr.NewError("unsupported gateway for refunds").
			WithHint("Only Stripe and Razorpay are supported for gateway refunds").
			WithReportableDetails(map[string]any{"gateway": refund.PaymentGateway}).
			Mark(ierr.ErrValidation)
	}
}

// --- Phase 3: confirm or release -------------------------------------------------

// confirmGatewayResult applies the Phase 3 CAS UPDATE based on the gateway's returned status.
// Returns the post-confirm Refund domain object.
func (s *refundService) confirmGatewayResult(
	ctx context.Context,
	refund *domainrefund.Refund,
	result *gatewayRefundResult,
) (*domainrefund.Refund, error) {

	mappedStatus := mapGatewayStatus(refund.PaymentGateway, result.GatewayStatus)
	now := time.Now().UTC()

	s.Logger.Info(ctx, "Phase 3: confirming gateway result",
		"refund_id", refund.ID,
		"gateway_refund_id", result.GatewayRefundID,
		"gateway_status", result.GatewayStatus,
		"mapped_status", mappedStatus,
	)

	// For terminal states (SUCCEEDED/FAILED/CANCELLED) we need to update Payment too.
	// For PROCESSING (non-terminal), a single UpdateStatus call is sufficient.
	switch mappedStatus {
	case types.RefundStatusProcessing:
		return s.confirmProcessing(ctx, refund, result, now)

	case types.RefundStatusSucceeded:
		return s.confirmSucceeded(ctx, refund, result, now)

	case types.RefundStatusFailed, types.RefundStatusCancelled:
		// Gateway returned a terminal failure inline — release the reservation.
		failureReason := "gateway returned " + result.GatewayStatus + " synchronously"
		s.releaseReservation(ctx, refund, failureReason)
		return nil, ierr.NewError("gateway rejected refund").
			WithHint("The gateway returned a terminal failure status on the refund request").
			WithReportableDetails(map[string]any{
				"refund_id":        refund.ID,
				"gateway_status":   result.GatewayStatus,
				"gateway_refund_id": result.GatewayRefundID,
			}).
			Mark(ierr.ErrSystem)
	}

	// Default: treat as PROCESSING.
	return s.confirmProcessing(ctx, refund, result, now)
}

// confirmProcessing applies a PENDING→PROCESSING CAS transition.
func (s *refundService) confirmProcessing(
	ctx context.Context,
	refund *domainrefund.Refund,
	result *gatewayRefundResult,
	now time.Time,
) (*domainrefund.Refund, error) {

	updated, err := s.RefundRepo.UpdateStatus(
		ctx,
		refund.ID,
		types.RefundStatusPending,
		types.RefundStatusProcessing,
		domainrefund.RefundStatusUpdate{
			GatewayRefundID: &result.GatewayRefundID,
			GatewayMetadata: result.GatewayMetadata,
		},
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to record gateway acceptance").
			Mark(ierr.ErrInternal)
	}
	if !updated {
		// CAS missed — re-read and apply decision 12's carve-out.
		current, readErr := s.RefundRepo.Get(ctx, refund.ID)
		if readErr != nil {
			return nil, readErr
		}
		if current.RefundStatus == types.RefundStatusProcessing {
			// Ordinary duplicate-convergence (concurrent webhook already advanced it).
			s.Logger.Info(ctx, "confirmProcessing: row already in PROCESSING (concurrent write), no-op",
				"refund_id", refund.ID,
			)
			return current, nil
		}
		// Different terminal state: data-integrity anomaly (decision 12 carve-out).
		s.Logger.Error(ctx, "data-integrity anomaly: confirmProcessing CAS missed and row is in unexpected state",
			"refund_id", refund.ID,
			"current_status", current.RefundStatus,
		)
		return current, nil
	}

	_ = now
	refund.RefundStatus = types.RefundStatusProcessing
	refund.GatewayRefundID = &result.GatewayRefundID
	refund.GatewayMetadata = result.GatewayMetadata
	return refund, nil
}

// confirmSucceeded applies a PENDING→SUCCEEDED CAS transition and updates Payment.PaymentStatus.
func (s *refundService) confirmSucceeded(
	ctx context.Context,
	refund *domainrefund.Refund,
	result *gatewayRefundResult,
	now time.Time,
) (*domainrefund.Refund, error) {

	var finalRefund *domainrefund.Refund

	txErr := s.DB.WithTx(ctx, func(tx context.Context) error {
		succeededAt := now
		updated, err := s.RefundRepo.UpdateStatus(
			tx,
			refund.ID,
			types.RefundStatusPending,
			types.RefundStatusSucceeded,
			domainrefund.RefundStatusUpdate{
				GatewayRefundID: &result.GatewayRefundID,
				GatewayMetadata: result.GatewayMetadata,
				SucceededAt:     &succeededAt,
			},
		)
		if err != nil {
			return err
		}
		if !updated {
			// Apply decision 12 carve-out.
			current, readErr := s.RefundRepo.Get(tx, refund.ID)
			if readErr != nil {
				return readErr
			}
			if current.RefundStatus == types.RefundStatusSucceeded {
				s.Logger.Info(ctx, "confirmSucceeded: row already SUCCEEDED (concurrent write), no-op",
					"refund_id", refund.ID,
				)
				finalRefund = current
				return nil
			}
			s.Logger.Error(ctx, "data-integrity anomaly: confirmSucceeded CAS missed, row in unexpected state",
				"refund_id", refund.ID,
				"current_status", current.RefundStatus,
			)
			finalRefund = current
			return nil
		}

		// Update Payment.PaymentStatus to REFUNDED or PARTIALLY_REFUNDED.
		pmt, pmtErr := s.PaymentRepo.GetForUpdate(tx, refund.PaymentID)
		if pmtErr != nil {
			// Log but do not fail the transaction — the refund itself is confirmed.
			s.Logger.Error(ctx, "confirmSucceeded: failed to lock payment for status update",
				"refund_id", refund.ID,
				"payment_id", refund.PaymentID,
				"error", pmtErr,
			)
			refund.RefundStatus = types.RefundStatusSucceeded
			refund.GatewayRefundID = &result.GatewayRefundID
			refund.SucceededAt = &succeededAt
			finalRefund = refund
			return nil
		}

		if pmt.RefundedAmount.GreaterThanOrEqual(pmt.Amount) {
			pmt.PaymentStatus = types.PaymentStatusRefunded
		} else {
			pmt.PaymentStatus = types.PaymentStatusPartiallyRefunded
		}
		pmt.RefundedAt = &succeededAt

		if updateErr := s.PaymentRepo.Update(tx, pmt); updateErr != nil {
			s.Logger.Error(ctx, "confirmSucceeded: failed to update payment status",
				"refund_id", refund.ID,
				"payment_id", refund.PaymentID,
				"error", updateErr,
			)
		}

		refund.RefundStatus = types.RefundStatusSucceeded
		refund.GatewayRefundID = &result.GatewayRefundID
		refund.SucceededAt = &succeededAt
		finalRefund = refund
		return nil
	})
	if txErr != nil {
		return nil, ierr.WithError(txErr).
			WithHint("Failed to record inline refund success").
			Mark(ierr.ErrInternal)
	}
	return finalRefund, nil
}

// releaseReservation performs the Phase 3 / crash-recovery reservation release:
// CAS PENDING→FAILED and decrements Payment.RefundedAmount in one transaction.
// Errors are logged but not returned (caller already has an error to surface).
func (s *refundService) releaseReservation(ctx context.Context, refund *domainrefund.Refund, failureReason string) {
	now := time.Now().UTC()
	txErr := s.DB.WithTx(ctx, func(tx context.Context) error {
		updated, err := s.RefundRepo.UpdateStatus(
			tx,
			refund.ID,
			types.RefundStatusPending,
			types.RefundStatusFailed,
			domainrefund.RefundStatusUpdate{
				FailureReason: &failureReason,
				FailedAt:      &now,
			},
		)
		if err != nil {
			return err
		}
		if !updated {
			// CAS missed.  Re-read to apply decision 12 carve-out.
			current, readErr := s.RefundRepo.Get(tx, refund.ID)
			if readErr != nil {
				return readErr
			}
			if current.RefundStatus == types.RefundStatusFailed {
				// Duplicate release — safe no-op.
				return nil
			}
			// Different terminal state (e.g. SUCCEEDED via concurrent webhook): anomaly.
			s.Logger.Error(ctx, "data-integrity anomaly: releaseReservation CAS missed, row in unexpected state — reservation NOT released",
				"refund_id", refund.ID,
				"current_status", current.RefundStatus,
			)
			return nil
		}

		// Decrement Payment.RefundedAmount to release the reservation.
		if incrErr := s.RefundRepo.IncrementPaymentRefundedAmount(tx, refund.PaymentID, refund.Amount.Neg()); incrErr != nil {
			return incrErr
		}
		return nil
	})
	if txErr != nil {
		s.Logger.Error(ctx, "releaseReservation: failed to release refund reservation",
			"refund_id", refund.ID,
			"payment_id", refund.PaymentID,
			"error", txErr,
		)
	}
}

// --- GetRefund -------------------------------------------------------------------

func (s *refundService) GetRefund(ctx context.Context, id string) (*dto.RefundResponse, error) {
	if id == "" {
		return nil, ierr.NewError("refund id is required").
			Mark(ierr.ErrValidation)
	}

	r, err := s.RefundRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewRefundResponse(r), nil
}

// --- ListRefunds -----------------------------------------------------------------

func (s *refundService) ListRefunds(ctx context.Context, filter *types.RefundFilter) (*dto.ListRefundsResponse, error) {
	if filter == nil {
		filter = &types.RefundFilter{}
	}

	refunds, err := s.RefundRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := s.RefundRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	return dto.NewListRefundsResponse(refunds, total), nil
}

// CancelRefund cancels a PROCESSING refund via the gateway (Stripe only).
// The gateway is called first; local state is only updated after the gateway confirms
// that cancellation actually took effect. Returns an error if the gateway does not
// support cancellation (Razorpay) or if the refund is not in PROCESSING state.
func (s *refundService) CancelRefund(ctx context.Context, id string) (*dto.RefundResponse, error) {
	return nil, ierr.NewError("CancelRefund is not yet implemented").
		WithHint("Refund cancellation is not yet available").
		Mark(ierr.ErrSystem)
}

// --- webhook event helper --------------------------------------------------------

func (s *refundService) publishRefundEvent(ctx context.Context, eventType string, refundID string) {
	payload, err := json.Marshal(webhookDto.InternalRefundEvent{
		RefundID: refundID,
		TenantID: types.GetTenantID(ctx),
	})
	if err != nil {
		s.Logger.Error(ctx, "publishRefundEvent: failed to marshal payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventType,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(payload),
		EntityType:    types.SystemEntityTypeRefund,
		EntityID:      refundID,
	}

	if pubErr := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); pubErr != nil {
		s.Logger.Error(ctx, "publishRefundEvent: failed to publish webhook",
			"error", pubErr,
			"refund_id", refundID,
			"event_type", eventType,
		)
	}
}
