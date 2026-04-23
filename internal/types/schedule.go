package types

import (
	"fmt"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ScheduleID is the Temporal server schedule ID for a recurring workflow.
type ScheduleID string

const (
	ScheduleIDCreditGrantProcessing        ScheduleID = "credit-grants-processing"
	ScheduleIDSubscriptionAutoCancellation ScheduleID = "subscription-auto-cancellation"
	ScheduleIDWalletCreditExpiry           ScheduleID = "wallet-credit-expiry"
	ScheduleIDSubscriptionBillingPeriods   ScheduleID = "subscription-billing-periods"
	ScheduleIDSubscriptionRenewalAlerts    ScheduleID = "subscription-renewal-due-alerts"
	ScheduleIDEventsKafkaLagMonitoring     ScheduleID = "events-kafka-lag-monitoring"
)

// String returns the raw schedule id.
func (id ScheduleID) String() string { return string(id) }

// AllTemporalServerScheduleIDs returns every ID registered as a Temporal server schedule
// (same order as AllScheduleRegistrations).
func AllTemporalServerScheduleIDs() []ScheduleID {
	return lo.Map(AllScheduleRegistrations(), func(r ScheduleRegistration, _ int) ScheduleID {
		return r.ID
	})
}

// Validate returns nil if this id is a known Temporal server schedule (pause / describe / setup).
// Use for API bodies where schedule_id must be one of the registered ids.
func (id ScheduleID) Validate() error {
	if id == "" {
		return ierr.NewError("schedule_id is required").
			WithHint("Use an id from GET /v1/temporal/schedules").
			Mark(ierr.ErrValidation)
	}
	allowed := AllTemporalServerScheduleIDs()
	if lo.Contains(allowed, id) {
		return nil
	}
	return ierr.NewError("invalid schedule_id").
		WithHint(fmt.Sprintf("Must be one of: %s", strings.Join(lo.Map(allowed, func(s ScheduleID, _ int) string { return string(s) }), ", "))).
		Mark(ierr.ErrValidation)
}

// ScheduleRegistration is one row in the managed schedule catalog (Temporal Schedules only).
// Register new workflows in internal/temporal/service/schedules.go and list them here.
type ScheduleRegistration struct {
	ID          ScheduleID `json:"schedule_id"`
	Description string     `json:"description"`
}

// AllScheduleRegistrations returns every known Temporal server schedule.
// HTTP /v1/cron/* routes that call the same service logic remain for manual/legacy triggers; void-old-pending is HTTP-only for now.
func AllScheduleRegistrations() []ScheduleRegistration {
	return []ScheduleRegistration{
		{ID: ScheduleIDCreditGrantProcessing, Description: "Credit grant application processing (also POST /v1/cron/creditgrants/process-scheduled-applications)"},
		{ID: ScheduleIDSubscriptionAutoCancellation, Description: "Subscription auto-cancellation (also POST /v1/cron/subscriptions/process-auto-cancellation)"},
		{ID: ScheduleIDWalletCreditExpiry, Description: "Wallet credit expiry (also POST /v1/cron/wallets/expire-credits)"},
		{ID: ScheduleIDSubscriptionBillingPeriods, Description: "Update subscription billing periods (also POST /v1/cron/subscriptions/update-periods)"},
		{ID: ScheduleIDSubscriptionRenewalAlerts, Description: "Subscription renewal-due alerts (also POST /v1/cron/subscriptions/renewal-due-alerts)"},
		{ID: ScheduleIDEventsKafkaLagMonitoring, Description: "Kafka consumer lag monitoring (also POST /v1/cron/events/monitoring)"},
	}
}

// TemporalServerSetupStatus is the per-schedule outcome of EnsureSchedule (Temporal create/update).
type TemporalServerSetupStatus string

const (
	TemporalServerSetupStatusCreated TemporalServerSetupStatus = "created"
	TemporalServerSetupStatusUpdated TemporalServerSetupStatus = "updated"
	TemporalServerSetupStatusError   TemporalServerSetupStatus = "error"
)

// String returns the string form of the setup status.
func (s TemporalServerSetupStatus) String() string { return string(s) }

// Validate returns nil if s is a known Temporal server setup status.
func (s TemporalServerSetupStatus) Validate() error {
	allowed := []TemporalServerSetupStatus{
		TemporalServerSetupStatusCreated,
		TemporalServerSetupStatusUpdated,
		TemporalServerSetupStatusError,
	}
	if lo.Contains(allowed, s) {
		return nil
	}
	return ierr.NewError("invalid schedule setup status").
		WithHint(fmt.Sprintf("Must be one of: %s", strings.Join(lo.Map(allowed, func(x TemporalServerSetupStatus, _ int) string { return string(x) }), ", "))).
		Mark(ierr.ErrValidation)
}

// ScheduleResult is the result of a single Temporal server schedule create/update.
type ScheduleResult struct {
	ScheduleID ScheduleID                `json:"schedule_id"`
	Status     TemporalServerSetupStatus `json:"status"`
	Error      string                    `json:"error,omitempty"`
}

// ScheduleConfig is everything needed to create or update one Temporal server schedule.
type ScheduleConfig struct {
	ID        ScheduleID
	Interval  time.Duration
	Workflow  interface{}
	Input     interface{}
	TaskQueue TemporalTaskQueue
}
