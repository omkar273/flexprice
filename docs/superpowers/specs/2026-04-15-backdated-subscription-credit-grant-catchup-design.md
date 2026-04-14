# Backdated Subscription Credit Grant Catch-Up

**Date:** 2026-04-15
**Status:** Approved

---

## Problem

When a subscription is created with a backdated start date, recurring credit grants are not applied immediately for all past-due periods. Only the first Credit Grant Application (CGA) is processed eagerly; each subsequent CGA is created only after the previous one is processed by the cron job (which runs every 15 minutes). This means a subscription backdated by N billing cycles takes up to N × 15 minutes to receive all credits it should already have.

---

## Goal

For backdated subscriptions (and any scenario where multiple CGAs are past-due), process all past-due CGAs immediately in a single call. The customer receives all eligible credits up to `now` instantly. Future-dated CGAs continue to be handled by cron as before.

---

## Approach

Add a new internal method `processCatchUpApplications` that wraps the existing `processScheduledApplication` in a bounded loop. All three call sites that previously called `processScheduledApplication` directly are updated to call `processCatchUpApplications` instead. `processScheduledApplication` itself is unchanged in responsibility — it remains a single-CGA processor.

To avoid extra DB queries per loop iteration, the signature chain is updated so the newly created next-period CGA is returned in memory from the functions that already hold it.

---

## Signature Changes

The following internal methods change return type. No public API or interface signatures change.

| Method | Before | After |
|---|---|---|
| `createNextPeriodApplication` | `error` | `(*CGA, error)` |
| `applyCreditGrantToWallet` | `error` | `(*CGA, error)` |
| `skipCreditGrantApplication` | `error` | `(*CGA, error)` |
| `processScheduledApplication` | `error` | `(*CGA, error)` |

### `createNextPeriodApplication`

Returns the created `*CreditGrantApplication`, or `nil` if no next period is created (grant end date reached). Error semantics unchanged.

### `applyCreditGrantToWallet`

Captures the next CGA via a closure variable outside the `WithTx` block, since `createNextPeriodApplication` is called in two places inside the closure:

1. **Expiry-skip path** (Task 0): grant has already expired — CGA is marked `Skipped`, next CGA created for recurring grants.
2. **Normal apply path** (Task 3): credits applied to wallet, CGA marked `Applied`, next CGA created for recurring grants.

```go
var nextCGA *CreditGrantApplication

err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
    // Task 0: expiry check
    if expiryDate != nil && expiryDate.Before(time.Now().UTC()) {
        // mark Skipped ...
        if grant.Cadence == Recurring {
            created, err := s.createNextPeriodApplication(txCtx, ...)
            if err != nil { return err }
            nextCGA = created
        }
        return nil
    }
    // Task 1: top-up wallet
    // Task 2: mark Applied
    // Task 3: next period
    if grant.Cadence == Recurring {
        created, err := s.createNextPeriodApplication(txCtx, ...)
        if err != nil { return err }
        nextCGA = created
    }
    return nil
})

if err != nil {
    return nil, s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed ...")
}
return nextCGA, nil
```

### `skipCreditGrantApplication`

Returns the created next CGA if recurring, `nil` for one-time grants.

### `processScheduledApplication`

Threads the return value through each action branch:

| Action | Returns |
|---|---|
| Apply (success) | `(nextCGA, nil)` — nextCGA may be nil for one-time grants |
| Skip (success) | `(nextCGA, nil)` |
| Defer | `(nil, nil)` — subscription not ready, stop loop |
| Cancel | `(nil, nil)` — grant cancelled, stop loop |
| Any error | `(nil, err)` — CGA already marked `Failed` internally |

---

## New Method: `processCatchUpApplications`

```go
const maxCatchUpIterations = 100

func (s *creditGrantService) processCatchUpApplications(
    ctx context.Context,
    initialCGA *CreditGrantApplication,
) error {
    currentCGA := initialCGA
    startTime := currentCGA.ScheduledFor

    for i := 0; i < maxCatchUpIterations; i++ {
        nextCGA, err := s.processScheduledApplication(ctx, currentCGA)
        if err != nil {
            // CGA already marked Failed in DB by handleCreditGrantFailure.
            // No point retrying immediately — cron will pick it up on next run.
            s.Logger.WarnwCtx(ctx, "catch-up loop stopping due to failure",
                "cga_id", currentCGA.ID,
                "grant_id", currentCGA.CreditGrantID,
                "subscription_id", currentCGA.SubscriptionID,
                "iterations_completed", i,
                "error", err)
            return nil // backward compat: caller error handling unchanged
        }

        // nil nextCGA: one-time grant, Defer, Cancel, or grant end date reached
        if nextCGA == nil {
            break
        }

        // Next period is future-dated — normal cron territory
        if nextCGA.ScheduledFor.After(time.Now().UTC()) {
            break
        }

        currentCGA = nextCGA
    }

    s.Logger.InfowCtx(ctx, "catch-up loop completed",
        "grant_id", initialCGA.CreditGrantID,
        "subscription_id", initialCGA.SubscriptionID,
        "time_range_start", startTime,
        "time_range_end", currentCGA.ScheduledFor)

    return nil
}
```

**Loop termination conditions:**
- `nextCGA == nil`: no next period (one-time grant, Defer/Cancel action, grant end date hit)
- `nextCGA.ScheduledFor > now`: next period is in the future
- `err != nil`: failure mid-loop — break, log, return nil
- `i >= maxCatchUpIterations (100)`: safety cap

---

## Caller Updates

All three direct call sites of `processScheduledApplication` are replaced with `processCatchUpApplications`. No other files change.

| Caller | File | Change |
|---|---|---|
| `InitializeCreditGrantWorkflow` | `creditgrant.go` | `processScheduledApplication` → `processCatchUpApplications` |
| `ProcessScheduledCreditGrantApplications` (cron) | `creditgrant.go` | same |
| `ProcessCreditGrantApplication` (manual trigger) | `creditgrant.go` | same |

`processPendingCreditGrantsForSubscription` in `subscription.go` calls `ProcessCreditGrantApplication`, so catch-up is inherited there for free with no changes needed.

---

## Error Contract

| Scenario | DB state | `processCatchUpApplications` return |
|---|---|---|
| All CGAs applied | All `Applied`, one future CGA created | `nil` |
| Mid-loop CGA fails | Failed CGA marked `Failed`, cron retries | `nil` (backward compat) |
| Initial CGA fails | Marked `Failed` | `nil` (logged) |
| Defer/Cancel mid-loop | CGA deferred/cancelled | `nil`, loop stops cleanly |
| Max iterations (100) hit | Last processed CGA `Applied`/`Skipped` | `nil`, warning logged |

---

## Idempotency

No new idempotency concerns introduced. `createNextPeriodApplication` uses a unique `IdempotencyKey` constraint on `(grant_id, period_start, period_end)`. If a crash leaves a dangling `Pending` CGA and the catch-up loop reruns, `processScheduledApplication` picks up the same row — no duplicate credits.

---

## Test Scenarios

Tests live in `internal/service/creditgrant_test.go`.

| Scenario | Assertion |
|---|---|
| Backdated by 1 cycle | 1 CGA applied immediately, 1 future CGA created in `Pending` |
| Backdated by N cycles (e.g. 6) | All 6 applied in single call, 1 future CGA left `Pending` |
| Start date = future | No catch-up; 1 CGA created `Pending`, cron handles it |
| Mid-loop failure (cycle 3 of 6) | Cycles 1–2 applied, cycle 3 `Failed`, loop stops, cron retries cycle 3 |
| Defer action mid-loop | Loop stops, deferred CGA rescheduled, no further processing |
| One-time grant (non-recurring) | Single CGA applied, `nextCGA = nil`, no loop |
| Max iterations guard (>100 cycles backdated) | Exactly 100 CGAs processed, loop exits, warning logged |

---

## Trade-offs

| | Loop catch-up (this design) | Cron-only (current) |
|---|---|---|
| UX | Credits available immediately | Delayed by N × 15 min |
| DB load | Bounded spike at subscription creation | Gradual, spread over time |
| Correctness | All past credits applied atomically per CGA | Same, just delayed |
| Risk | Mid-loop failure leaves partial state (cron recovers) | No partial state |

The bounded loop (max 100 iterations) and per-CGA atomicity keep the risk manageable.
