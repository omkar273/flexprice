# ONETIME: Move from billing_cadence to billing_period — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `billing_cadence = ONETIME` with `billing_period = ONETIME` as the single signal for one-time charges, and auto-include ONETIME prices in every new subscription.

**Architecture:** Clean break — normalize existing DB rows in one SQL migration, remove `BILLING_CADENCE_ONETIME` from the type system, update every detection site to check `billing_period == ONETIME`. The `billing_cadence` field stays on the Price model but is always `RECURRING`. The `filterValidPricesForSubscription` function is extended to always pass ONETIME prices through regardless of subscription billing period, which handles auto-inclusion during subscription creation for free.

**Tech Stack:** Go 1.23, Ent ORM, PostgreSQL, Gin HTTP framework.

---

## File Map

| File | Change |
|------|--------|
| `migrations/postgres/V3_onetime_billing_period.up.sql` | **Create** — data normalization SQL |
| `internal/types/price.go` | **Modify** — add `BILLING_PERIOD_ONETIME`, remove `BILLING_CADENCE_ONETIME`, update both validators |
| `internal/api/dto/price.go` | **Modify** — validation & defaulting in `Validate()` and `ToPrice()` |
| `internal/api/dto/subscription_line_item.go` | **Modify** — ONETIME detection in 4 places |
| `internal/service/billing.go` | **Modify** — ONETIME detection in 2 places + comment |
| `internal/service/subscription.go` | **Modify** — `filterValidPricesForSubscription` |
| `internal/service/billing_onetime_test.go` | **Modify** — fixture updates throughout |
| `internal/service/subscription_test.go` | **Modify** — fixture updates |
| `internal/service/creditgrant_test.go` | **Modify** — fixture updates |
| `docs/swagger/*` | **Regenerate** — `make swagger` |

---

## Task 1: DB Migration

**Files:**
- Create: `migrations/postgres/V3_onetime_billing_period.up.sql`

- [ ] **Step 1: Create migration file**

```sql
-- migrations/postgres/V3_onetime_billing_period.up.sql
-- Normalize all one-time prices to use billing_period=ONETIME instead of billing_cadence=ONETIME.
-- After this migration every price row has billing_cadence='RECURRING'.
UPDATE prices
SET billing_cadence = 'RECURRING',
    billing_period  = 'ONETIME'
WHERE billing_cadence = 'ONETIME';
```

- [ ] **Step 2: Apply migration**

```bash
make migrate-postgres
```

Expected: migration applies with no errors. Verify:

```bash
docker compose exec postgres psql -U flexprice -d flexprice \
  -c "SELECT COUNT(*) FROM prices WHERE billing_cadence = 'ONETIME';"
```

Expected output: `count = 0`

- [ ] **Step 3: Commit**

```bash
git add migrations/postgres/V3_onetime_billing_period.up.sql
git commit -m "chore(migration): normalize onetime prices to billing_period=ONETIME"
```

---

## Task 2: Type System

**Files:**
- Modify: `internal/types/price.go`

- [ ] **Step 1: Update constants**

In `internal/types/price.go`, in the `const` block (around line 179):

```go
// For BILLING_CADENCE_RECURRING
BILLING_PERIOD_MONTHLY   BillingPeriod = "MONTHLY"
BILLING_PERIOD_ANNUAL    BillingPeriod = "ANNUAL"
BILLING_PERIOD_WEEKLY    BillingPeriod = "WEEKLY"
BILLING_PERIOD_DAILY     BillingPeriod = "DAILY"
BILLING_PERIOD_QUARTER   BillingPeriod = "QUARTERLY"
BILLING_PERIOD_HALF_YEAR BillingPeriod = "HALF_YEARLY"
BILLING_PERIOD_ONETIME   BillingPeriod = "ONETIME"  // ADD THIS LINE

BILLING_CADENCE_RECURRING BillingCadence = "RECURRING"
// BILLING_CADENCE_ONETIME removed — use BILLING_PERIOD_ONETIME instead
```

Remove the line:
```go
BILLING_CADENCE_ONETIME   BillingCadence = "ONETIME"
```

- [ ] **Step 2: Update `BillingPeriod.Validate()`**

Replace the current `allowed` slice in `BillingPeriod.Validate()` (around line 230):

```go
func (b BillingPeriod) Validate() error {
	if b == "" {
		return nil
	}

	allowed := []BillingPeriod{
		BILLING_PERIOD_MONTHLY,
		BILLING_PERIOD_ANNUAL,
		BILLING_PERIOD_WEEKLY,
		BILLING_PERIOD_DAILY,
		BILLING_PERIOD_QUARTER,
		BILLING_PERIOD_HALF_YEAR,
		BILLING_PERIOD_ONETIME,
	}
	if !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing period").
			WithHint("Invalid billing period").
			WithReportableDetails(map[string]interface{}{
				"billing_period": b,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 3: Update `BillingCadence.Validate()`**

Replace the `allowed` slice (around line 209):

```go
func (b BillingCadence) Validate() error {
	allowed := []BillingCadence{
		BILLING_CADENCE_RECURRING,
	}
	if b != "" && !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing cadence").
			WithHint("Invalid billing cadence — only RECURRING is supported").
			WithReportableDetails(map[string]interface{}{
				"billing_cadence": b,
				"allowed":         allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 4: Verify compilation**

```bash
make build
```

Expected: fails with compile errors wherever `BILLING_CADENCE_ONETIME` is still referenced. This is intentional — the compiler now guides us to every remaining reference. Note each file reported.

- [ ] **Step 5: Commit**

```bash
git add internal/types/price.go
git commit -m "feat(types): add BILLING_PERIOD_ONETIME, remove BILLING_CADENCE_ONETIME"
```

---

## Task 3: Price DTO — Validation and Defaulting

**Files:**
- Modify: `internal/api/dto/price.go`

- [ ] **Step 1: Update `Validate()` — cadence switch (around line 358)**

Replace the entire billing cadence switch block:

```go
// Before:
// 9. Validate billing cadence specific requirements
switch r.BillingCadence {
case types.BILLING_CADENCE_RECURRING:
    if r.BillingPeriod == "" {
        return ierr.NewError("billing_period is required when billing_cadence is RECURRING").
            WithHint("Please select a billing period to set up recurring pricing").
            Mark(ierr.ErrValidation)
    }
case types.BILLING_CADENCE_ONETIME:
    if r.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("invoice_cadence must be ADVANCE for ONETIME prices").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
}
```

With:

```go
// 9. Validate billing period requirements
// billing_cadence is always RECURRING; billing_period drives one-time vs recurring
if r.BillingPeriod == "" {
    return ierr.NewError("billing_period is required").
        WithHint("Please select a billing period (e.g. MONTHLY, ANNUAL, ONETIME)").
        Mark(ierr.ErrValidation)
}
if r.BillingPeriod == types.BILLING_PERIOD_ONETIME {
    if r.InvoiceCadence != "" && r.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("invoice_cadence must be ADVANCE for ONETIME prices").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
}
```

- [ ] **Step 2: Update trial period validation (around line 380)**

Replace:

```go
if r.TrialPeriod > 0 &&
    r.BillingCadence != types.BILLING_CADENCE_RECURRING &&
    r.Type != types.PRICE_TYPE_FIXED {
    return ierr.NewError("trial period can only be set for recurring fixed prices").
        WithHint("Trial period can only be set for recurring fixed prices").
        Mark(ierr.ErrValidation)
}
```

With:

```go
if r.TrialPeriod > 0 &&
    (r.BillingPeriod == types.BILLING_PERIOD_ONETIME || r.Type != types.PRICE_TYPE_FIXED) {
    return ierr.NewError("trial period can only be set for recurring fixed prices").
        WithHint("Trial period can only be set for recurring fixed prices").
        Mark(ierr.ErrValidation)
}
```

- [ ] **Step 3: Default `billing_cadence` to `RECURRING` in `ToPrice()` (around line 402)**

In `ToPrice()`, before the `price := &priceDomain.Price{...}` struct literal, add:

```go
// billing_cadence is always RECURRING; default it when omitted by the caller
if r.BillingCadence == "" {
    r.BillingCadence = types.BILLING_CADENCE_RECURRING
}
```

- [ ] **Step 4: Verify**

```bash
make build
```

Expected: fewer compile errors (this file no longer references `BILLING_CADENCE_ONETIME`).

- [ ] **Step 5: Commit**

```bash
git add internal/api/dto/price.go
git commit -m "feat(dto/price): move ONETIME validation to billing_period; default cadence to RECURRING"
```

---

## Task 4: Subscription Line Item DTO

**Files:**
- Modify: `internal/api/dto/subscription_line_item.go`

- [ ] **Step 1: Update comment on line 84**

Replace:
```go
// For prices with billing_cadence ONETIME, request end_date is ignored: the line item end_date is always start_date + 1 calendar day (UTC), clamped to the subscription end when present.
```
With:
```go
// For prices with billing_period ONETIME, request end_date is ignored: the line item end_date is always start_date + 1 calendar day (UTC), clamped to the subscription end when present.
```

- [ ] **Step 2: Update `onetimeIgnoresRequestEndDate` check (around line 185)**

Replace:
```go
onetimeIgnoresRequestEndDate := (price != nil && price.BillingCadence == types.BILLING_CADENCE_ONETIME) ||
    (r.Price != nil && r.Price.BillingCadence == types.BILLING_CADENCE_ONETIME)
```
With:
```go
onetimeIgnoresRequestEndDate := (price != nil && price.BillingPeriod == types.BILLING_PERIOD_ONETIME) ||
    (r.Price != nil && r.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME)
```

- [ ] **Step 3: Update invoice cadence validation check (around line 257)**

Replace:
```go
if price != nil && price.BillingCadence == types.BILLING_CADENCE_ONETIME {
    if price.InvoiceCadence != "" && price.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("ONETIME charges must have invoice_cadence ADVANCE").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
}
```
With:
```go
if price != nil && price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
    if price.InvoiceCadence != "" && price.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("ONETIME charges must have invoice_cadence ADVANCE").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
}
```

- [ ] **Step 4: Update invoice cadence defaulting in `ToSubscriptionLineItem()` (around line 444)**

In `ToSubscriptionLineItem()`, the section that resolves cadences currently reads:

```go
billingCadence := types.BILLING_CADENCE_RECURRING
invoiceCadence := types.InvoiceCadenceAdvance
if params.Price != nil {
    if params.Price.BillingCadence != "" {
        billingCadence = params.Price.BillingCadence
    }
    invoiceCadence = params.Price.InvoiceCadence
    // ONETIME charges default to ADVANCE invoice cadence if not explicitly set
    if billingCadence == types.BILLING_CADENCE_ONETIME && invoiceCadence == "" {
        invoiceCadence = types.InvoiceCadenceAdvance
    }
}
```

Replace with:

```go
invoiceCadence := types.InvoiceCadenceAdvance
if params.Price != nil {
    invoiceCadence = params.Price.InvoiceCadence
    // ONETIME charges default to ADVANCE invoice cadence if not explicitly set
    if params.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME && invoiceCadence == "" {
        invoiceCadence = types.InvoiceCadenceAdvance
    }
}
```

Note: the `billingCadence` local variable is no longer needed — it was only used for the ONETIME invoice cadence default, which now reads from `params.Price.BillingPeriod`. Remove it entirely. If the variable is used elsewhere in the function, keep only those usages (search the file for other `billingCadence` references before deleting).

- [ ] **Step 5: Update end-date auto-calculation (around line 540)**

Replace:
```go
if params.Price != nil && params.Price.BillingCadence == types.BILLING_CADENCE_ONETIME {
```
With:
```go
if params.Price != nil && params.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
```

- [ ] **Step 6: Verify**

```bash
make build
```

- [ ] **Step 7: Commit**

```bash
git add internal/api/dto/subscription_line_item.go
git commit -m "feat(dto/subscription): detect ONETIME via billing_period instead of billing_cadence"
```

---

## Task 5: Billing Service

**Files:**
- Modify: `internal/service/billing.go`

- [ ] **Step 1: Update `CalculateFixedCharges` (around line 186)**

Replace:
```go
if price.BillingCadence == types.BILLING_CADENCE_ONETIME {
```
With:
```go
if price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
```

- [ ] **Step 2: Update `ClassifyLineItems` (around line 2274)**

Replace:
```go
if item.Price.BillingCadence == types.BILLING_CADENCE_ONETIME {
```
With:
```go
if item.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
```

- [ ] **Step 3: Update comment for `attachPricesToLineItems` (around line 1875)**

Replace:
```go
// each price to its line item so that IsOneTime() (and other price-aware methods) can
// use price.BillingCadence without an additional per-item DB call.
```
With:
```go
// each price to its line item so that ONETIME detection (and other price-aware methods) can
// use price.BillingPeriod without an additional per-item DB call.
```

- [ ] **Step 4: Verify**

```bash
make build
```

Expected: clean build (or only test file errors remaining, which is fine at this stage).

- [ ] **Step 5: Commit**

```bash
git add internal/service/billing.go
git commit -m "feat(billing): detect ONETIME charges via billing_period instead of billing_cadence"
```

---

## Task 6: Subscription Service — Auto-Include ONETIME Prices

**Files:**
- Modify: `internal/service/subscription.go`

- [ ] **Step 1: Update `filterValidPricesForSubscription` (around line 3274)**

Replace the current function body:

```go
func filterValidPricesForSubscription(prices []*dto.PriceResponse, subscription *subscription.Subscription) []*dto.PriceResponse {
	var validPrices []*dto.PriceResponse
	for _, p := range prices {
		if !types.IsMatchingCurrency(p.Price.Currency, subscription.Currency) {
			continue
		}
		periodOK := p.Price.BillingPeriod == subscription.BillingPeriod ||
			types.IsBillingPeriodMultiple(p.Price.BillingPeriod, subscription.BillingPeriod)
		if periodOK {
			validPrices = append(validPrices, p)
		}
	}
	return validPrices
}
```

With:

```go
func filterValidPricesForSubscription(prices []*dto.PriceResponse, subscription *subscription.Subscription) []*dto.PriceResponse {
	var validPrices []*dto.PriceResponse
	for _, p := range prices {
		if !types.IsMatchingCurrency(p.Price.Currency, subscription.Currency) {
			continue
		}
		// ONETIME prices always apply — they are not tied to the subscription billing period
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
	return validPrices
}
```

- [ ] **Step 2: Verify**

```bash
make build
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription.go
git commit -m "feat(subscription): auto-include ONETIME prices in filterValidPricesForSubscription"
```

---

## Task 7: Fix Test Fixtures

**Files:**
- Modify: `internal/service/billing_onetime_test.go`
- Modify: `internal/service/subscription_test.go`
- Modify: `internal/service/creditgrant_test.go`

- [ ] **Step 1: Find all remaining `BILLING_CADENCE_ONETIME` references**

```bash
grep -rn "BILLING_CADENCE_ONETIME" internal/
```

This will list every remaining reference with line numbers. All of them are test fixtures.

- [ ] **Step 2: Update `billing_onetime_test.go` fixtures**

For every test fixture that looks like:
```go
BillingCadence: types.BILLING_CADENCE_ONETIME,
BillingPeriod:  "",
```

Replace with:
```go
BillingCadence: types.BILLING_CADENCE_RECURRING,
BillingPeriod:  types.BILLING_PERIOD_ONETIME,
```

There are approximately two base fixtures at the top of the file (around lines 146–172) — the rest of the tests inherit from them. Verify by running grep again after editing.

- [ ] **Step 3: Update `subscription_test.go` fixtures**

Apply the same substitution pattern to every occurrence in this file.

- [ ] **Step 4: Update `creditgrant_test.go` fixtures**

Apply the same substitution pattern to every occurrence in this file.

- [ ] **Step 5: Confirm zero remaining references**

```bash
grep -rn "BILLING_CADENCE_ONETIME" internal/
```

Expected: no output.

- [ ] **Step 6: Run tests**

```bash
go test -v -race ./internal/service/... -run "TestBillingOnetime\|TestSubscription\|TestCreditGrant" 2>&1 | tail -40
```

Expected: all tests pass. If any test fails, read the failure message — it will point to a fixture that was not updated or a behaviour difference.

- [ ] **Step 7: Run all tests**

```bash
make test
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/service/billing_onetime_test.go \
        internal/service/subscription_test.go \
        internal/service/creditgrant_test.go
git commit -m "test: update fixtures to use BILLING_PERIOD_ONETIME"
```

---

## Task 8: Swagger Regeneration + Manual Smoke Test

**Files:**
- Regenerate: `docs/swagger/*`

- [ ] **Step 1: Regenerate swagger**

```bash
make swagger
```

Expected: no errors. The swagger docs will no longer mention `ONETIME` as a valid `billing_cadence` value and will add `ONETIME` to the `billing_period` enum.

- [ ] **Step 2: Start the server**

```bash
make run
```

- [ ] **Step 3: Create a plan with an ONETIME price**

```bash
# 1. Create a plan
curl -s -X POST http://localhost:8080/v1/plans \
  -H "x-api-key: sk_01KNVMKJ2Z937A4M8VJK6J8KMM" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Smoke Test Plan",
    "description": "Plan with ONETIME price"
  }' | jq .

# Note the plan id from the response
PLAN_ID="<paste plan id>"

# 2. Create a recurring price on the plan
curl -s -X POST http://localhost:8080/v1/prices \
  -H "x-api-key: sk_01KNVMKJ2Z937A4M8VJK6J8KMM" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Monthly base\",
    \"amount\": 1000,
    \"currency\": \"USD\",
    \"type\": \"FIXED\",
    \"billing_cadence\": \"RECURRING\",
    \"billing_period\": \"MONTHLY\",
    \"billing_period_count\": 1,
    \"billing_model\": \"FLAT_FEE\",
    \"invoice_cadence\": \"advance\",
    \"entity_type\": \"PLAN\",
    \"entity_id\": \"$PLAN_ID\"
  }" | jq .

# 3. Create an ONETIME price on the same plan
curl -s -X POST http://localhost:8080/v1/prices \
  -H "x-api-key: sk_01KNVMKJ2Z937A4M8VJK6J8KMM" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Setup fee\",
    \"amount\": 5000,
    \"currency\": \"USD\",
    \"type\": \"FIXED\",
    \"billing_cadence\": \"RECURRING\",
    \"billing_period\": \"ONETIME\",
    \"billing_model\": \"FLAT_FEE\",
    \"invoice_cadence\": \"advance\",
    \"entity_type\": \"PLAN\",
    \"entity_id\": \"$PLAN_ID\"
  }" | jq .
```

Expected: both price creations return 200 with the price object. The ONETIME price has `billing_period: "ONETIME"` and `billing_cadence: "RECURRING"`.

- [ ] **Step 4: Create a subscription and verify ONETIME price is auto-included**

```bash
CUSTOMER_ID="<an existing customer id>"

curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KNVMKJ2Z937A4M8VJK6J8KMM" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUSTOMER_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"USD\",
    \"billing_period\": \"MONTHLY\",
    \"billing_period_count\": 1
  }" | jq '.line_items[] | {price_id, billing_period}'
```

Expected: response contains two line items — one with `billing_period: "MONTHLY"` and one with `billing_period: "ONETIME"`. The ONETIME line item has `end_date = start_date + 1 day`.

- [ ] **Step 5: Verify ONETIME is rejected with cadence=ONETIME**

```bash
curl -s -X POST http://localhost:8080/v1/prices \
  -H "x-api-key: sk_01KNVMKJ2Z937A4M8VJK6J8KMM" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Bad price\",
    \"amount\": 1000,
    \"currency\": \"USD\",
    \"type\": \"FIXED\",
    \"billing_cadence\": \"ONETIME\",
    \"billing_period\": \"MONTHLY\",
    \"billing_model\": \"FLAT_FEE\",
    \"invoice_cadence\": \"advance\",
    \"entity_type\": \"PLAN\",
    \"entity_id\": \"$PLAN_ID\"
  }" | jq .
```

Expected: 400 error — `billing_cadence` value `ONETIME` is no longer valid.

- [ ] **Step 6: Commit swagger**

```bash
git add docs/swagger/
git commit -m "docs: regenerate swagger after billing_period=ONETIME changes"
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] Remove `BILLING_CADENCE_ONETIME` → Task 2
- [x] Add `BILLING_PERIOD_ONETIME` → Task 2
- [x] DB migration → Task 1
- [x] Price DTO validation updated → Task 3
- [x] `subscription_line_item.go` 4 spots → Task 4
- [x] `billing.go` 2 spots → Task 5
- [x] `filterValidPricesForSubscription` auto-include → Task 6
- [x] Test fixtures → Task 7
- [x] Swagger regen → Task 8
- [x] Smoke test → Task 8

**No placeholders:** All steps contain actual code or exact commands.

**Type consistency:** `types.BILLING_PERIOD_ONETIME` defined in Task 2 Step 1, used identically in Tasks 3–7.
