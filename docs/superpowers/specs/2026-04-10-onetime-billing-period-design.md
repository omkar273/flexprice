# Design: Move ONETIME from billing_cadence to billing_period

**Date:** 2026-04-10
**Branch:** feat/onetime-charges
**Status:** Approved

---

## Problem

PR #1440 introduced one-time charge support by adding `billing_cadence = ONETIME`. This conflates two orthogonal concepts:

- `billing_cadence` — whether something recurs (RECURRING vs. one-shot)
- `billing_period` — how often it recurs (MONTHLY, ANNUAL, etc.)

The result is an inconsistent model: ONETIME prices carry an empty `billing_period`, the cadence field does double duty, and detection logic is scattered across billing_cadence checks. Additionally, ONETIME prices are not automatically included during subscription creation since the period-match filter skips them.

---

## Decision

- `billing_period = ONETIME` is the single source of truth for one-time charges.
- `billing_cadence` stays on the Price model but is always `RECURRING`. `BILLING_CADENCE_ONETIME` is removed.
- All existing data is normalized via a single SQL migration before code changes land.

---

## Approach: Clean Break (Approach A)

No dual-detection paths. No backward-compat shims. One migration, then clean code everywhere.

---

## Changes by Layer

### 1. Type System — `internal/types/price.go`

**Add:**
```go
BILLING_PERIOD_ONETIME BillingPeriod = "ONETIME"
```

**Remove:**
```go
BILLING_CADENCE_ONETIME BillingCadence = "ONETIME"
```

**Update `BillingPeriod.Validate()`:** add `BILLING_PERIOD_ONETIME` to the allowed list.

**Update `BillingCadence.Validate()`:** remove `BILLING_CADENCE_ONETIME` from the allowed list. Only `BILLING_CADENCE_RECURRING` is valid.

No ent schema field changes needed — `"ONETIME"` is a non-empty string that naturally satisfies the existing `NotEmpty()` constraint on `billing_period`.

---

### 2. DB Migration — `migrations/postgres/<timestamp>_onetime_billing_period.sql`

```sql
UPDATE prices
SET billing_cadence = 'RECURRING',
    billing_period  = 'ONETIME'
WHERE billing_cadence = 'ONETIME';
```

Runs before any application code change. Normalizes all existing ONETIME price records.

---

### 3. API / DTO Layer

**`internal/api/dto/price.go` — `CreatePriceRequest.Validate()`:**

- Remove `BILLING_CADENCE_ONETIME` case from cadence switch.
- Add `BILLING_PERIOD_ONETIME` case to period validation:
  - Enforce `invoice_cadence = ADVANCE` (one-time charges are always billed in advance).
  - `billing_period_count` forced to 0 (no cycles for ONETIME).
- In `ToPrice()`: default `billing_cadence` to `RECURRING` if omitted — since only one valid value exists, callers should not be required to pass it.

**`internal/api/dto/subscription_line_item.go`:**

- `onetimeIgnoresRequestEndDate`: `billing_cadence == ONETIME` → `billing_period == ONETIME`
- Auto end-date calculation: same change (start_date + 1 calendar day, clamped to sub end)
- Invoice cadence defaulting: `billing_cadence == ONETIME` → `billing_period == ONETIME`
- Update comment on line 84 accordingly.

---

### 4. Service Layer

**`internal/service/billing.go` — two spots:**

| Location | Before | After |
|----------|--------|-------|
| `CalculateFixedCharges` (~line 186) | `price.BillingCadence == BILLING_CADENCE_ONETIME` | `price.BillingPeriod == BILLING_PERIOD_ONETIME` |
| `ClassifyLineItems` (~line 2274) | `price.BillingCadence == BILLING_CADENCE_ONETIME` | `price.BillingPeriod == BILLING_PERIOD_ONETIME` |

**`internal/service/subscription.go` — `filterValidPricesForSubscription`:**

ONETIME prices bypass the period-match check and are always included:

```go
for _, p := range prices {
    if !types.IsMatchingCurrency(p.Price.Currency, subscription.Currency) {
        continue
    }
    // ONETIME prices always apply — no period match needed
    if p.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
        validPrices = append(validPrices, p)
        continue
    }
    periodOK := p.Price.BillingPeriod == subscription.BillingPeriod ||
        types.IsBillingPeriodMultiple(p.Price.BillingPeriod, subscription.BillingPeriod)
    if periodOK {
        validPrices = append(validPrices, p)
    }
}
```

This single change also handles **auto-adding ONETIME prices during subscription creation**: `CreateSubscription` already calls `ValidateAndFilterPricesForSubscription` → `filterValidPricesForSubscription`, so all ONETIME prices on a plan are automatically included as line items with no additional creation-flow changes.

---

### 5. Tests

**`internal/service/billing_onetime_test.go`:** Replace all fixtures:
```go
// Before
BillingCadence: types.BILLING_CADENCE_ONETIME, BillingPeriod: ""

// After
BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_ONETIME
```

**`internal/service/subscription_test.go`** and **`internal/service/creditgrant_test.go`:** Same fixture updates wherever `BILLING_CADENCE_ONETIME` appears.

**Swagger docs:** Regenerate via `make swagger` after all code changes.

---

## Files Touched

| File | Change |
|------|--------|
| `internal/types/price.go` | Add `BILLING_PERIOD_ONETIME`, remove `BILLING_CADENCE_ONETIME`, update both validators |
| `migrations/postgres/<ts>_onetime_billing_period.sql` | Data normalization UPDATE |
| `internal/api/dto/price.go` | ONETIME rules move from cadence-switch to period-switch; default cadence to RECURRING |
| `internal/api/dto/subscription_line_item.go` | ONETIME detection via `billing_period` (3 spots + comment) |
| `internal/service/billing.go` | ONETIME detection via `billing_period` (2 spots) |
| `internal/service/subscription.go` | `filterValidPricesForSubscription`: always include ONETIME |
| `internal/service/billing_onetime_test.go` | Fixture updates throughout |
| `internal/service/subscription_test.go` | Fixture updates |
| `internal/service/creditgrant_test.go` | Fixture updates |
| `docs/swagger/*` | Regenerate |

---

## Invariants After This Change

- Every price in the DB has `billing_cadence = 'RECURRING'`.
- A price is one-time if and only if `billing_period = 'ONETIME'`.
- All ONETIME prices on a plan are automatically included in any new subscription for that plan.
- ONETIME line items are billed exactly once (advance), never regenerated on renewal.
- No proration applied to ONETIME charges.
- `billing_cadence` on the Subscription entity is unchanged — it still describes the subscription's own recurring cycle.

---

## Out of Scope

- Removing the `billing_cadence` column from the DB or Ent schema (kept for compatibility).
- Changes to the Subscription entity's `billing_cadence`/`billing_period` fields.
- Temporal workflow changes (no billing cadence references in workflow layer).
