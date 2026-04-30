# Webhook Retry Job Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `WebhookRetryJob` config struct to `internal/config/config.go` that makes the Temporal stale-webhook retry cron job tenant-aware, rate-limited, and killswitchable.

**Architecture:** A new `WebhookRetryJob` struct is added inline in `config.go` and surfaced as a top-level field on `Configuration`. The repository's `ListStaleUndeliveredWebhooks` receives `maxAttempts` instead of a hardcoded `4`. `WebhookService.RetryStalePendingWebhooks` reads the new config to apply the kill switch, pass `maxAttempts` to the DB query, filter rows in-memory by `ExcludedTenants` and `AllowedEventTypes`, and throttle delivery with a token-bucket rate limiter. The Temporal activity is unchanged — it already delegates entirely to the service.

**Tech Stack:** Go 1.23, Viper (mapstructure), Ent ORM, `golang.org/x/time/rate` (already in go.mod at v0.8.0)

---

## File Map

| File | Change |
|---|---|
| `internal/config/config.go` | Add `WebhookRetryJob` struct + `WebhookRetryJob` field on `Configuration` |
| `internal/config/config.yaml` | Add `webhook_retry_job:` YAML block with defaults |
| `internal/config/webhook_retry_job_config_test.go` | New — verify struct defaults unmarshal correctly |
| `internal/repository/ent/systemevent.go` | Accept `maxAttempts int` in `ListStaleUndeliveredWebhooks` |
| `internal/webhook/service.go` | Read new config: kill switch, maxAttempts, in-memory filters, rate limiter |
| `internal/webhook/service_test.go` | Add tests for kill switch, excluded tenants, allowed event types |

---

## Task 1: Add `WebhookRetryJob` config struct

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add the struct and field**

In `internal/config/config.go`, add this struct **before** `NewConfig()` (after the existing `OnboardingEventsConfig` block around line 358):

```go
// WebhookRetryJob configures the Temporal stale-webhook retry cron job.
// All filtering is applied by the activity after the DB query.
type WebhookRetryJob struct {
	// Enabled is a kill switch — false exits the activity immediately with zero counts.
	Enabled bool `mapstructure:"enabled" default:"true"`
	// MaxAttempts is the maximum number of delivery failures before a system_event is
	// abandoned by the retry job. Replaces the hardcoded FailureCountLT(4) in the query.
	MaxAttempts int `mapstructure:"max_attempts" default:"5"`
	// RateLimit is the maximum number of webhook deliveries per second within a single
	// cron job run (token-bucket, golang.org/x/time/rate).
	RateLimit int `mapstructure:"rate_limit" default:"5"`
	// ExcludedTenants is a flat list of tenant IDs to skip entirely. Empty = process all.
	ExcludedTenants []string `mapstructure:"excluded_tenants"`
	// AllowedEventTypes is a whitelist of event_name values to retry. Empty = retry all.
	AllowedEventTypes []string `mapstructure:"allowed_event_types"`
}
```

Then add the field to `Configuration` struct (after the `OnboardingEvents` field, around line 62):

```go
WebhookRetryJob WebhookRetryJob `mapstructure:"webhook_retry_job" validate:"omitempty"`
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/config/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add WebhookRetryJob struct to Configuration"
```

---

## Task 2: Add YAML defaults and config test

**Files:**
- Modify: `internal/config/config.yaml`
- Create: `internal/config/webhook_retry_job_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/webhook_retry_job_config_test.go`:

```go
package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestWebhookRetryJob_Defaults(t *testing.T) {
	yaml := `
webhook_retry_job:
  enabled: true
  max_attempts: 5
  rate_limit: 5
  excluded_tenants: []
  allowed_event_types: []
`
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(yaml)))

	var cfg Configuration
	require.NoError(t, v.Unmarshal(&cfg))

	j := cfg.WebhookRetryJob
	require.True(t, j.Enabled)
	require.Equal(t, 5, j.MaxAttempts)
	require.Equal(t, 5, j.RateLimit)
	require.Empty(t, j.ExcludedTenants)
	require.Empty(t, j.AllowedEventTypes)
}

func TestWebhookRetryJob_ExcludedTenants(t *testing.T) {
	yaml := `
webhook_retry_job:
  enabled: true
  max_attempts: 3
  rate_limit: 10
  excluded_tenants:
    - "ten_skip_1"
    - "ten_skip_2"
  allowed_event_types:
    - "invoice.finalized"
`
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(yaml)))

	var cfg Configuration
	require.NoError(t, v.Unmarshal(&cfg))

	j := cfg.WebhookRetryJob
	require.Equal(t, 3, j.MaxAttempts)
	require.Equal(t, 10, j.RateLimit)
	require.Equal(t, []string{"ten_skip_1", "ten_skip_2"}, j.ExcludedTenants)
	require.Equal(t, []string{"invoice.finalized"}, j.AllowedEventTypes)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run TestWebhookRetryJob -v
```

Expected: FAIL — `WebhookRetryJob` field does not exist yet on `Configuration` (Task 1 must be complete first; if Task 1 is done these should PASS already — treat this step as a smoke-run).

- [ ] **Step 3: Add YAML block to config.yaml**

In `internal/config/config.yaml`, add the following block after the `webhook:` section (around line 119, before the `temporal:` block):

```yaml
webhook_retry_job:
  enabled: true
  max_attempts: 5
  rate_limit: 5
  excluded_tenants: []
  allowed_event_types: []
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/config/... -run TestWebhookRetryJob -v
```

Expected: PASS — both `TestWebhookRetryJob_Defaults` and `TestWebhookRetryJob_ExcludedTenants` pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.yaml internal/config/webhook_retry_job_config_test.go
git commit -m "feat(config): add webhook_retry_job YAML defaults and config tests"
```

---

## Task 3: Update `ListStaleUndeliveredWebhooks` to accept `maxAttempts`

**Files:**
- Modify: `internal/repository/ent/systemevent.go` (lines 37–53)

- [ ] **Step 1: Update the method signature and replace hardcoded `4`**

Change the signature of `ListStaleUndeliveredWebhooks` from:

```go
func (r *SystemEventRepository) ListStaleUndeliveredWebhooks(ctx context.Context, olderThan time.Time, limit int) ([]*flexent.SystemEvent, error) {
```

to:

```go
func (r *SystemEventRepository) ListStaleUndeliveredWebhooks(ctx context.Context, olderThan time.Time, limit, maxAttempts int) ([]*flexent.SystemEvent, error) {
```

And replace the hardcoded predicate inside the query:

```go
// Before:
systemevent.FailureCountLT(4),

// After:
systemevent.FailureCountLT(maxAttempts),
```

- [ ] **Step 2: Fix the call site in `service.go`**

In `internal/webhook/service.go`, the existing call (line ~108) is:

```go
rows, err := s.systemEventRepo.ListStaleUndeliveredWebhooks(ctx, cutoff, staleWebhookPageSize)
```

Update it to pass `5` as `maxAttempts` temporarily (Task 4 will replace this with the config value):

```go
rows, err := s.systemEventRepo.ListStaleUndeliveredWebhooks(ctx, cutoff, staleWebhookPageSize, 5)
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/repository/... ./internal/webhook/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/repository/ent/systemevent.go internal/webhook/service.go
git commit -m "feat(repo): accept maxAttempts in ListStaleUndeliveredWebhooks"
```

---

## Task 4: Update `RetryStalePendingWebhooks` to use `WebhookRetryJob` config

**Files:**
- Modify: `internal/webhook/service.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/webhook/service_test.go`:

```go
package webhook

import (
	"context"
	"testing"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// stubSystemEventRepo is a minimal stub for SystemEventRepository used in service tests.
type stubSystemEventRepo struct {
	rows      []*flexent.SystemEvent
	failedIDs []string
}

func (s *stubSystemEventRepo) ListStaleUndeliveredWebhooks(_ context.Context, _ time.Time, _, _ int) ([]*flexent.SystemEvent, error) {
	return s.rows, nil
}

func (s *stubSystemEventRepo) OnFailed(_ context.Context, id, _ string) error {
	s.failedIDs = append(s.failedIDs, id)
	return nil
}

// makeEvent creates a minimal SystemEvent for testing.
func makeEvent(id, tenantID, eventName string) *flexent.SystemEvent {
	return &flexent.SystemEvent{
		ID:            id,
		TenantID:      tenantID,
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventName(eventName),
		CreatedAt:     time.Now().UTC().Add(-30 * time.Minute),
	}
}

func TestRetryStalePendingWebhooks_KillSwitch(t *testing.T) {
	t.Parallel()

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook:        config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJob{Enabled: false, MaxAttempts: 5, RateLimit: 100},
		},
		logger: logger.NewNoopLogger(),
		systemEventRepo: &stubSystemEventRepo{
			rows: []*flexent.SystemEvent{makeEvent("sev_1", "ten_1", "invoice.finalized")},
		},
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Total, "kill switch: no events should be processed")
}

func TestRetryStalePendingWebhooks_ExcludedTenants(t *testing.T) {
	t.Parallel()

	stub := &stubSystemEventRepo{
		rows: []*flexent.SystemEvent{
			makeEvent("sev_skip", "ten_excluded", "invoice.finalized"),
			makeEvent("sev_ok", "ten_included", "invoice.finalized"),
		},
	}

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook: config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJob{
				Enabled:         true,
				MaxAttempts:     5,
				RateLimit:       100,
				ExcludedTenants: []string{"ten_excluded"},
			},
		},
		logger:          logger.NewNoopLogger(),
		systemEventRepo: stub,
		// handler is nil — RetriggerSystemEvent will error, which is fine for this test
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	// Only sev_ok reaches RetriggerSystemEvent; sev_skip is filtered before it
	require.Equal(t, 1, res.Total, "excluded tenant events should not count toward Total")
}

func TestRetryStalePendingWebhooks_AllowedEventTypes(t *testing.T) {
	t.Parallel()

	stub := &stubSystemEventRepo{
		rows: []*flexent.SystemEvent{
			makeEvent("sev_allowed", "ten_1", "invoice.finalized"),
			makeEvent("sev_blocked", "ten_1", "subscription.created"),
		},
	}

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook: config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJob{
				Enabled:           true,
				MaxAttempts:       5,
				RateLimit:         100,
				AllowedEventTypes: []string{"invoice.finalized"},
			},
		},
		logger:          logger.NewNoopLogger(),
		systemEventRepo: stub,
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	// Only sev_allowed reaches RetriggerSystemEvent
	require.Equal(t, 1, res.Total, "events not in AllowedEventTypes should be skipped")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/webhook/... -run "TestRetryStalePendingWebhooks" -v
```

Expected: FAIL — `stubSystemEventRepo` doesn't satisfy the current repo interface, and `WebhookRetryJob` logic doesn't exist yet in the service. Confirm you see compilation errors, not panics.

- [ ] **Step 3: Extract a minimal repo interface for testability**

The tests use `stubSystemEventRepo`. To allow this, `WebhookService` needs to hold an interface rather than the concrete `*repoent.SystemEventRepository`. Add this interface at the top of `internal/webhook/service.go` (before the `const` block):

```go
// systemEventRepo is the subset of SystemEventRepository methods used by WebhookService.
type systemEventRepo interface {
	ListStaleUndeliveredWebhooks(ctx context.Context, olderThan time.Time, limit, maxAttempts int) ([]*flexent.SystemEvent, error)
	GetByID(ctx context.Context, tenantID, environmentID, id string) (*flexent.SystemEvent, error)
	OnConsumed(ctx context.Context, event *types.WebhookEvent) error
	OnDelivered(ctx context.Context, eventID, webhookMessageID string) error
	OnFailed(ctx context.Context, eventID, reason string) error
}
```

Change the field on `WebhookService` from:

```go
systemEventRepo *repoent.SystemEventRepository
```

to:

```go
systemEventRepo systemEventRepo
```

The `NewWebhookService` constructor parameter type changes from `*repoent.SystemEventRepository` to `systemEventRepo` — since `*repoent.SystemEventRepository` implements all those methods, all existing callers keep working without modification.

- [ ] **Step 4: Implement the config-driven logic in `RetryStalePendingWebhooks`**

Replace the entire `RetryStalePendingWebhooks` method in `internal/webhook/service.go` with:

```go
// RetryStalePendingWebhooks loads undelivered system_events older than the grace period and
// delivers each via RetriggerSystemEvent. Behaviour is controlled by cfg.WebhookRetryJob.
func (s *WebhookService) RetryStalePendingWebhooks(ctx context.Context) (RetryStalePendingWebhooksResult, error) {
	var out RetryStalePendingWebhooksResult

	if !s.config.Webhook.Enabled {
		return out, nil
	}

	jobCfg := s.config.WebhookRetryJob
	if !jobCfg.Enabled {
		return out, nil
	}

	// Build fast-lookup sets for in-memory filtering.
	excludedTenants := make(map[string]struct{}, len(jobCfg.ExcludedTenants))
	for _, id := range jobCfg.ExcludedTenants {
		excludedTenants[id] = struct{}{}
	}
	allowedEvents := make(map[string]struct{}, len(jobCfg.AllowedEventTypes))
	for _, name := range jobCfg.AllowedEventTypes {
		allowedEvents[name] = struct{}{}
	}

	// Token-bucket rate limiter: RateLimit deliveries per second.
	rps := jobCfg.RateLimit
	if rps <= 0 {
		rps = 5
	}
	limiter := rate.NewLimiter(rate.Limit(rps), rps)

	cutoff := time.Now().UTC().Add(-staleWebhookGracePeriod)

	for {
		rows, err := s.systemEventRepo.ListStaleUndeliveredWebhooks(ctx, cutoff, staleWebhookPageSize, jobCfg.MaxAttempts)
		if err != nil {
			return out, err
		}

		for _, se := range rows {
			// Skip excluded tenants.
			if _, skip := excludedTenants[se.TenantID]; skip {
				continue
			}

			// Skip event types not in the allowlist (when allowlist is non-empty).
			if len(allowedEvents) > 0 {
				if _, ok := allowedEvents[string(se.EventName)]; !ok {
					continue
				}
			}

			out.Total++

			// Throttle delivery rate.
			if err := limiter.Wait(ctx); err != nil {
				return out, err
			}

			if err := s.RetriggerSystemEvent(ctx, se.TenantID, se.EnvironmentID, se.ID); err != nil {
				out.Failed++
				s.logger.Errorw("stale webhook retry failed",
					"error", err,
					"system_event_id", se.ID,
					"tenant_id", se.TenantID,
					"environment_id", se.EnvironmentID,
				)
				if dbErr := s.systemEventRepo.OnFailed(ctx, se.ID, err.Error()); dbErr != nil {
					s.logger.Warnw("failed to persist webhook failure_reason on stale retry",
						"error", dbErr,
						"system_event_id", se.ID,
					)
				}
				continue
			}
			out.Succeeded++
		}

		if len(rows) < staleWebhookPageSize {
			break
		}
	}

	return out, nil
}
```

Add `"golang.org/x/time/rate"` to the import block in `service.go`.

- [ ] **Step 5: Add `NewNoopLogger` helper if it doesn't exist**

Check whether `logger.NewNoopLogger()` exists:

```bash
grep -r "NewNoopLogger" /path/to/internal/logger/
```

If it does not exist, add to `internal/logger/logger.go` (or whichever file defines `Logger`):

```go
// NewNoopLogger returns a logger that discards all output. For use in tests only.
func NewNoopLogger() *Logger {
	return &Logger{SugaredLogger: zap.NewNop().Sugar()}
}
```

- [ ] **Step 6: Run the new tests**

```bash
go test ./internal/webhook/... -run "TestRetryStalePendingWebhooks" -v
```

Expected: PASS — all three new tests pass.

- [ ] **Step 7: Run the full test suite**

```bash
go test ./internal/config/... ./internal/webhook/... ./internal/repository/... -v
```

Expected: All pass (including existing `TestSystemEventToWebhookEvent*` tests).

- [ ] **Step 8: Vet**

```bash
go vet ./...
```

Expected: no errors.

- [ ] **Step 9: Commit**

```bash
git add internal/webhook/service.go internal/webhook/service_test.go
git commit -m "feat(webhook): apply WebhookRetryJob config — kill switch, filters, rate limiter"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** Kill switch ✓, MaxAttempts replaces hardcoded 4 ✓, RateLimit token bucket ✓, ExcludedTenants flat list ✓, AllowedEventTypes whitelist ✓, empty = process all ✓, defaults (5/5) ✓
- [x] **Placeholder scan:** No TBDs. All code blocks are complete.
- [x] **Type consistency:** `WebhookRetryJob` struct name used consistently across tasks. `ListStaleUndeliveredWebhooks(ctx, olderThan, limit, maxAttempts int)` signature defined in Task 3 and consumed in Task 4.
- [x] **Interface:** `systemEventRepo` interface defined in Task 4 Step 3; `stubSystemEventRepo` in tests implements all methods used in the real code path.
