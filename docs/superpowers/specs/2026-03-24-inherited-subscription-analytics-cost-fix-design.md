# Inherited Subscription Analytics Cost Fix Design

**Date:** 2026-03-24
**Status:** Approved

---

## Overview

When `include_children=true` (or when `ExternalCustomerIDs` contains a child customer directly), the analytics API correctly fetches usage events for child customers from ClickHouse. However, **cost calculation silently produces $0** for those children because the pricing information (subscription line items) lives on the parent subscription, not on the child's INHERITED skeleton subscription.

This spec fixes cost calculation for customers with INHERITED subscriptions by ensuring the parent subscription's line items are available in the pricing lookup map before costs are computed.

---

## Background: Subscription Hierarchy

Three subscription types exist:

| Type | Description |
|------|-------------|
| `standalone` | Regular subscription. Owns its own line items. Cost lookup works correctly today. |
| `parent` | Primary subscription in a hierarchy. Owns all line items for itself and its children. |
| `inherited` | Skeleton subscription created for each child customer. Has **no line items**. Pricing lives on the parent subscription (`ParentSubscriptionID` field). |

Usage events ingested for a child customer are tagged with `sub_line_item_id` values that reference the **parent subscription's** line items. When analytics fetches the child's subscriptions (INHERITED only) and builds `SubscriptionLineItems`, those parent line item IDs are absent — so `calculateCosts` finds nothing and returns $0.

---

## Root Cause

In `fetchAnalyticsData`:

```
subscriptions = fetchSubscriptions(ctx, child.ID)
// → returns [InheritedSub{LineItems: []}]

data.SubscriptionLineItems = {}   // built from subscriptions → empty

analytics = fetchAnalytics(ctx, params)
// → ClickHouse returns events with sub_line_item_id = "parent-line-item-id"

calculateCosts(data)
// → looks up "parent-line-item-id" in SubscriptionLineItems → NOT FOUND → cost = $0
```

---

## Fix

After `fetchSubscriptions` returns a list that contains any `SubscriptionTypeInherited` subscription with a non-nil `ParentSubscriptionID`, fetch those parent subscriptions (with line items) and append them to the slice. The existing downstream code in `fetchAnalyticsData` then builds `SubscriptionLineItems` and `SubscriptionsMap` from the full combined slice — parent line items are present and cost lookup succeeds.

### New helper

```go
// fetchParentSubscriptions fetches the parent subscriptions for any inherited
// subscriptions in the provided list (deduplicated by parent subscription ID).
// Returns the parent subscriptions to be merged into the caller's subscription slice.
func (s *featureUsageTrackingService) fetchParentSubscriptions(
    ctx context.Context,
    subscriptions []*subscription.Subscription,
) ([]*subscription.Subscription, error)
```

Called once from `fetchAnalyticsData`, immediately after `fetchSubscriptions`. For INHERITED subs with `ParentSubscriptionID != nil`, deduplicates parent IDs and fetches each. Returns the list of parent subs to append.

### Call site in `fetchAnalyticsData`

```
// 2. Fetch subscriptions
subscriptions, err = fetchSubscriptions(ctx, customer.ID)

// 2a. For inherited subscriptions, also include the parent subscription
//     so its line items are available for cost calculation.
parentSubs, err = fetchParentSubscriptions(ctx, subscriptions)
subscriptions = append(subscriptions, parentSubs...)

// 3. Validate currency consistency (unchanged — reads from combined list)
currency, err = validateCurrency(subscriptions)
```

No other functions change.

---

## What Does Not Change

- `GetDetailedUsageAnalytics` (multi-customer loop) — untouched
- `GetDetailedUsageAnalyticsV2` — untouched
- `buildAnalyticsResponse`, `calculateCosts`, `mergeAnalyticsData` — untouched
- V1 legacy path (`event_post_processing.go`) — untouched
- DTO shape — no API changes
- Standalone customers — `fetchParentSubscriptions` returns an empty slice when no INHERITED subs exist, so no behaviour change

---

## Files Changed

| File | Change |
|------|--------|
| `internal/service/feature_usage_tracking.go` | Add `fetchParentSubscriptions` helper; call it in `fetchAnalyticsData` after `fetchSubscriptions` |
| `internal/service/feature_usage_tracking_analytics_test.go` | Tests for the new code path |

---

## How to Fetch a Parent Subscription

Use `s.SubscriptionRepo.Get(ctx, parentSubID)` which returns a subscription with line items already populated (consistent with the `WithLineItems = true` flag used in `fetchSubscriptions`).

> **Note:** `SubscriptionRepo.Get` returns a subscription with `LineItems` populated via an Ent eager-load. Confirm this is the case by checking the repository implementation before coding. If `Get` does not eager-load line items, use `ListSubscriptions` with an ID filter and `WithLineItems = true` instead.

---

## Currency Handling

`validateCurrency` is called after the parent subs are appended. Parent and child subscriptions in a hierarchy are created with the same currency (enforced at subscription creation time), so no currency conflict is expected. The existing mismatch error in `validateCurrency` provides a safety net if this invariant is ever violated.

---

## Testing

| Scenario | Expected |
|----------|----------|
| Standalone customer (no INHERITED subs) | `fetchParentSubscriptions` returns empty slice; no behaviour change |
| Child customer with one INHERITED sub | Parent subscription's line items added; cost calculated correctly |
| Child customer with INHERITED sub, parent already in subscription list | Deduplication: parent fetched once |
| Two INHERITED subs pointing to the same parent sub | Parent sub fetched once (dedup by parent ID) |
| `fetchParentSubscriptions` fails to fetch parent (DB error) | Error propagated, request fails with a clear error message |
| `include_children=true` with a parent customer: child has INHERITED sub | Child costs calculated correctly; total reflects both parent's and child's usage |
| `include_children=false` (or not set), child customer requested directly via `ExternalCustomerIDs` | Same fix applies — `fetchParentSubscriptions` runs for any customer with INHERITED subs |
