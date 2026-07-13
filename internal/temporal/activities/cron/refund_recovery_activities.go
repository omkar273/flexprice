package cron

import (
	"context"
	"time"

	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	domainRefund "github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

const maxRefundWebhookAttempts = 5

// RefundRecoveryActivities handles the cron recovery sweep for stale refunds and
// unprocessed gateway webhook events.
type RefundRecoveryActivities struct {
	refundRepo             domainRefund.Repository
	refundWebhookEventRepo domainRefund.WebhookEventRepository
	paymentRepo            domainPayment.Repository
	logger                 *logger.Logger
}

// NewRefundRecoveryActivities constructs RefundRecoveryActivities.
func NewRefundRecoveryActivities(
	refundRepo domainRefund.Repository,
	refundWebhookEventRepo domainRefund.WebhookEventRepository,
	paymentRepo domainPayment.Repository,
	log *logger.Logger,
) *RefundRecoveryActivities {
	return &RefundRecoveryActivities{
		refundRepo:             refundRepo,
		refundWebhookEventRepo: refundWebhookEventRepo,
		paymentRepo:            paymentRepo,
		logger:                 log,
	}
}

// SurfaceStaleRefundsActivity queries all PENDING refunds across every
// tenant/environment whose claimed_at is older than 30 seconds and logs a
// warning for each. It does NOT mutate RefundStatus — alerting only.
func (a *RefundRecoveryActivities) SurfaceStaleRefundsActivity(ctx context.Context) (*cronModels.SurfaceStaleRefundsResult, error) {
	a.logger.Info(ctx, "starting SurfaceStaleRefundsActivity")

	result := &cronModels.SurfaceStaleRefundsResult{}

	staleRefunds, err := a.refundRepo.ListStalePendingAll(ctx, 30*time.Second)
	if err != nil {
		a.logger.Error(ctx, "failed to list stale PENDING refunds", "error", err)
		return nil, err
	}

	result.Total = len(staleRefunds)
	a.logger.Info(ctx, "fetched stale PENDING refunds", "count", result.Total)

	for _, ref := range staleRefunds {
		a.logger.Warn(ctx, "stale PENDING refund detected — gateway call may have been lost",
			"refund_id", ref.ID,
			"payment_id", ref.PaymentID,
			"payment_gateway", ref.PaymentGateway,
			"initiated_at", ref.InitiatedAt,
			"claimed_at", ref.ClaimedAt,
			"tenant_id", ref.TenantID,
			"environment_id", ref.EnvironmentID,
		)
		result.Alerted++
	}

	a.logger.Info(ctx, "completed SurfaceStaleRefundsActivity",
		"total", result.Total,
		"alerted", result.Alerted,
		"errors", result.Errors,
	)
	return result, nil
}

// RetryUnprocessedRefundWebhookEventsActivity queries RefundWebhookEvent rows that
// are unprocessed and older than 10 seconds (grace period), increments their
// attempt counter, attempts to match them to a known refund, and marks them
// processed when a match is found. Events that exceed maxRefundWebhookAttempts
// are logged as errors for manual investigation.
func (a *RefundRecoveryActivities) RetryUnprocessedRefundWebhookEventsActivity(ctx context.Context) (*cronModels.RetryUnprocessedWebhookEventsResult, error) {
	a.logger.Info(ctx, "starting RetryUnprocessedRefundWebhookEventsActivity")

	result := &cronModels.RetryUnprocessedWebhookEventsResult{}

	events, err := a.refundWebhookEventRepo.ListUnprocessedAll(ctx, 10*time.Second)
	if err != nil {
		a.logger.Error(ctx, "failed to list unprocessed refund webhook events", "error", err)
		return nil, err
	}

	result.Total = len(events)
	a.logger.Info(ctx, "fetched unprocessed refund webhook events", "count", result.Total)

	for _, e := range events {
		scopedCtx := types.SetTenantID(ctx, e.TenantID)
		scopedCtx = types.SetEnvironmentID(scopedCtx, e.EnvironmentID)

		nextAttempts := e.Attempts + 1

		if err := a.refundWebhookEventRepo.IncrementAttempts(scopedCtx, e.ID); err != nil {
			a.logger.Error(scopedCtx, "failed to increment attempts for refund webhook event",
				"event_id", e.ID,
				"error", err,
			)
			result.Errors++
			continue
		}

		if nextAttempts > maxRefundWebhookAttempts {
			a.logger.Error(scopedCtx, "refund webhook event exceeded max attempts — needs manual investigation",
				"event_id", e.ID,
				"gateway", e.Gateway,
				"gateway_event_id", e.GatewayEventID,
				"attempts", nextAttempts,
				"tenant_id", e.TenantID,
				"environment_id", e.EnvironmentID,
			)
			result.Errors++
			continue
		}

		// Attempt to match the event to a known refund using GatewayEventID as the
		// gateway refund identifier (gateway-specific; simplified for this sweep).
		ref, err := a.refundRepo.GetByGatewayRefundID(scopedCtx, e.GatewayEventID)
		if err != nil {
			a.logger.Warn(scopedCtx, "no matching refund found for webhook event — will retry on next sweep",
				"event_id", e.ID,
				"gateway", e.Gateway,
				"gateway_event_id", e.GatewayEventID,
				"attempts", nextAttempts,
			)
			continue
		}

		if markErr := a.refundWebhookEventRepo.MarkProcessed(scopedCtx, e.ID, &ref.ID); markErr != nil {
			a.logger.Error(scopedCtx, "failed to mark refund webhook event as processed",
				"event_id", e.ID,
				"refund_id", ref.ID,
				"error", markErr,
			)
			result.Errors++
			continue
		}

		a.logger.Info(scopedCtx, "successfully matched and processed refund webhook event",
			"event_id", e.ID,
			"refund_id", ref.ID,
			"gateway", e.Gateway,
		)
		result.Processed++
	}

	a.logger.Info(ctx, "completed RetryUnprocessedRefundWebhookEventsActivity",
		"total", result.Total,
		"processed", result.Processed,
		"errors", result.Errors,
	)
	return result, nil
}
