# WebhookRetryJob Config Design

**Date:** 2026-04-30  
**Status:** Approved

## Problem

The Temporal cron job `OutboundWebhookStaleRetryWorkflow` picks up **all** stale undelivered system events across all tenants. Several values are hardcoded:

- `FailureCountLT(4)` — max failures before giving up
- `staleWebhookGracePeriod = 15 * time.Minute` (left hardcoded by design)
- `staleWebhookPageSize = 500` (left hardcoded by design)
- No rate limiting in the retry loop
- No way to skip tenants not consuming webhooks
- No way to restrict which event types are retried

## Goal

Add a `WebhookRetryJob` config struct inline in `internal/config/config.go` that makes the retry job tenant-aware and rate-limited, without changing the grace period or page size.

## Config Struct

Added inline to `internal/config/config.go`:

```go
type WebhookRetryJob struct {
    Enabled           bool     `mapstructure:"enabled" default:"true"`
    MaxAttempts       int      `mapstructure:"max_attempts" default:"5"`
    RateLimit         int      `mapstructure:"rate_limit" default:"5"`
    ExcludedTenants   []string `mapstructure:"excluded_tenants"`
    AllowedEventTypes []string `mapstructure:"allowed_event_types"`
}
```

Added as a field on the root `Configuration` struct:

```go
WebhookRetryJob WebhookRetryJob `mapstructure:"webhook_retry_job"`
```

### Field Semantics

| Field | Default | Behaviour when empty/zero |
|---|---|---|
| `enabled` | `true` | `false` = activity exits immediately, no events processed |
| `max_attempts` | `5` | Replaces hardcoded `FailureCountLT(4)` in repository query |
| `rate_limit` | `5` | Token-bucket rate limiter (RPS) around per-event retry loop in activity |
| `excluded_tenants` | `[]` | Empty = process all tenants; non-empty = skip listed tenant IDs |
| `allowed_event_types` | `[]` | Empty = process all event types; non-empty = only retry listed event names |

## YAML Block

Added to `internal/config/config.yaml`:

```yaml
webhook_retry_job:
  enabled: true
  max_attempts: 5
  rate_limit: 5
  excluded_tenants: []
  allowed_event_types: []
```

## Usage in the Activity

File: `internal/temporal/activities/cron/webhook_outbound_retry_activities.go`

Changes to `RetryStalePendingWebhooks` (called by the activity):

1. **Kill switch** — check `cfg.WebhookRetryJob.Enabled` at entry; return early with zero counts if false.
2. **MaxAttempts** — passed to repository `ListStaleUndeliveredWebhooks` to replace the hardcoded `FailureCountLT(4)`.
3. **ExcludedTenants / AllowedEventTypes** — in-memory filters applied on each fetched batch (page size 500); `MaxAttempts` is the only value pushed into the DB query.
4. **RateLimit** — `golang.org/x/time/rate` token bucket (`rate.NewLimiter(rate.Limit(rps), rps)`) wraps the per-event retry call.

## Files Touched

| File | Change |
|---|---|
| `internal/config/config.go` | Add `WebhookRetryJob` struct + field on `Configuration` |
| `internal/config/config.yaml` | Add `webhook_retry_job:` block with defaults |
| `internal/repository/ent/systemevent.go` | Accept `maxAttempts int` param in `ListStaleUndeliveredWebhooks` |
| `internal/webhook/service.go` | Thread `maxAttempts`, `excludedTenants`, `allowedEventTypes` into `RetryStalePendingWebhooks` |
| `internal/temporal/activities/cron/webhook_outbound_retry_activities.go` | Read config, apply kill switch, pass params, apply rate limiter |

## What Is Not Changing

- `staleWebhookGracePeriod` (15 min) — stays hardcoded
- `staleWebhookPageSize` (500) — stays hardcoded
- Temporal schedule cadence (every 15 min) — unchanged
- Existing `Webhook` consumer config (`webhook:` block) — untouched
