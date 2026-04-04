# SDK DX Improvements Design

**Date:** 2026-04-04
**Status:** Approved
**Scope:** Flexprice Go SDK v2.1.0 — developer experience overhaul
**Breaking changes:** Yes — shipping as minor version bump 2.0.x → 2.1.0

---

## Problem Statement

The Flexprice Go SDK has several DX issues that make it feel "generated" rather than "hand-crafted":

1. All types are prefixed with `Dto` (e.g., `DtoSubscriptionResponse`) because swaggo emits Go package-qualified schema names (`dto.SubscriptionResponse`) which Speakeasy converts verbatim
2. 177 timestamp fields are typed as `string` instead of `time.Time` due to missing `format: date-time` in the OpenAPI spec
3. Error types are named `ErrorsErrorResponse` with a field called `Error_` (Go keyword escape for `error`)
4. No SDK-level error helpers (`IsNotFound`, `IsValidation`, etc.)
5. 5 schemas have full Go import paths as names (unusable)
6. Required fields are all pointers regardless of whether they're required, due to missing `required` arrays on 27 request schemas and `respectRequiredFields: false` in Speakeasy config

---

## Approach: Overlay-First, Backend Later

**Phase 1** delivers a clean SDK immediately via Speakeasy overlay and config changes — zero backend modifications.
**Phase 2** cleans up root causes in the backend so the spec is healthy and the overlay shrinks to a minimal permanent set.

This sequencing was chosen because:
- Phase 1 ships a clean v3 SDK without waiting for backend annotation cleanup
- The Speakeasy overlay is a first-class mechanism, not a hack
- Phase 2 is lower urgency and can be done incrementally

---

## Phase 1 — Clean SDK via Overlay & Config

### Task 1: Generate Speakeasy Overlay

**File:** `.speakeasy/overlays/flexprice-sdk.yaml`
**Script:** `scripts/generate_overlay.py` (committed, repeatable)

The script reads `docs/swagger/swagger-3-0.json` and generates overlay actions for:

**1a. Strip `dto.` prefix from all 218 `dto.*` schemas**
```yaml
- target: "$.components.schemas['dto.SubscriptionResponse']"
  update:
    x-speakeasy-name-override: SubscriptionResponse
# ... repeated for all 218 dto.* schemas
```

**1b. Rename top-level entity response types (selective)**

Strip `Response` suffix only from clear top-level domain entities:

| Schema | Override |
|---|---|
| `dto.CustomerResponse` | `Customer` |
| `dto.SubscriptionResponse` | `Subscription` |
| `dto.InvoiceResponse` | `Invoice` |
| `dto.PlanResponse` | `Plan` |
| `dto.PriceResponse` | `Price` |
| `dto.AddonResponse` | `Addon` |
| `dto.WalletResponse` | `Wallet` |
| `dto.PaymentResponse` | `Payment` |
| `dto.CouponResponse` | `Coupon` |
| `dto.FeatureResponse` | `Feature` |

Supporting types (`BillingCycleInfo`, `AlertLogResponse`, `CostsheetResponse`, etc.) keep their names as-is.

**1c. Fix error schema names**
```yaml
- target: "$.components.schemas['errors.ErrorResponse']"
  update:
    x-speakeasy-name-override: ErrorResponse

- target: "$.components.schemas['errors.ErrorDetail']"
  update:
    x-speakeasy-name-override: ErrorDetail
```

**1d. Fix `Error_` field (Go reserved word escape)**
```yaml
- target: "$.components.schemas['errors.ErrorResponse'].properties.error"
  update:
    x-speakeasy-name-override: detail
```
This renames the `error` JSON property to `detail` in the SDK, eliminating the `Error_` Go field name.

**1e. Patch 177 timestamp fields with `format: date-time`**

The script also generates overlay patches for all timestamp fields missing `format: date-time` (identified by property names: `created_at`, `updated_at`, `start_date`, `end_date`, `period_start`, `period_end`, `cancel_at`, `cancelled_at`, `current_period_start`, `current_period_end`, `billing_anchor`, etc.).
```yaml
- target: "$.components.schemas['dto.SubscriptionResponse'].properties.created_at"
  update:
    format: date-time
# ... repeated for all 177 fields
```

**1f. Add `required` arrays to 27 request schemas missing them**

The script reads Go DTO structs from `internal/api/dto/*.go`, extracts fields with `validate:"required"` tags, and generates overlay `required` array patches for the 27 schemas that are missing them.

---

### Task 2: Speakeasy Config Changes

**File:** `.speakeasy/gen/go.yaml`

| Setting | Before | After | Reason |
|---|---|---|---|
| `inputModelSuffix` | `Input` | deleted | Conflicts with explicit `Request` suffix already in schema names |
| `outputModelSuffix` | `Output` | deleted | Conflicts with explicit `Response` suffix / entity name overrides |
| `respectRequiredFields` | `false` | `true` | Required fields should be non-pointer; requires `required` arrays in spec (added via Task 1f) |

---

### Task 3: Manual `errorutils` Package

**File:** `api/go/errorutils/errors.go`

A hand-written, non-generated file. Speakeasy will not overwrite files outside of its generation scope. Module path stays `github.com/flexprice/flexprice-go/v2` — SDK version bumps to `2.1.0` (minor bump, no module path change).

```go
package errorutils

import (
    "net/http"
    sderr "github.com/flexprice/flexprice-go/v2/models/errors"
)

func IsNotFound(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode == http.StatusNotFound
}

func IsValidation(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode == http.StatusBadRequest
}

func IsConflict(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode == http.StatusConflict
}

func IsRateLimit(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode == http.StatusTooManyRequests
}

func IsPermissionDenied(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode == http.StatusForbidden
}

func IsServerError(err error) bool {
    e, ok := err.(*sderr.APIError)
    return ok && e.StatusCode >= http.StatusInternalServerError
}
```

Usage:
```go
import "github.com/flexprice/flexprice-go/v2/errorutils"

_, err := client.Customers.CreateCustomer(ctx, req)
if errorutils.IsConflict(err) {
    // handle duplicate
}
```

---

### Task 4: Regenerate SDK

```bash
make go-sdk
make build
```

---

### Phase 1 Validation Checklist

- [ ] `grep -r "DtoCustomer\|DtoSubscription\|DtoInvoice\|DtoPrice\|DtoPlan" api/go/` → zero results
- [ ] `grep "Error_" api/go/` → zero results
- [ ] `grep "ErrorsError" api/go/` → zero results
- [ ] `grep -r "time.Time" api/go/models/types/` → present (timestamps are now typed)
- [ ] `api/go/errorutils/errors.go` exists and compiles
- [ ] `make build` passes

---

## Phase 2 — Backend Root Cause Cleanup

### Task 5: Check & Upgrade swaggo

```bash
# Check current version
grep "swaggo/swag" go.mod

# Upgrade to latest
go get github.com/swaggo/swag@latest
go mod tidy
make swagger
```

Audit how many of the 177 timestamp fields now have `format: date-time` automatically. Remove corresponding overlay patches for any that are now correct at source.

---

### Task 6: Fix 5 Leaked Go Package Path Schemas

Find and fix the 5 swaggo annotations referencing domain structs directly:

```
github_com_flexprice_flexprice_internal_domain_addon.Addon
github_com_flexprice_flexprice_internal_domain_coupon.Coupon
github_com_flexprice_flexprice_internal_domain_customer.Customer
github_com_flexprice_flexprice_internal_domain_feature.Feature
github_com_flexprice_flexprice_internal_domain_plan.Plan
```

Each requires finding the handler annotation (e.g., `@Success 200 {object} addon.Addon`) and replacing it with the correct DTO (`@Success 200 {object} dto.AddonResponse`).

After fix: remove any overlay entries for these 5 schemas.

---

### Task 7: Fix Backend Swaggo Annotations (Strip `dto.` Prefix)

Systematically update all `@Success`, `@Failure`, and `@Param` annotations across `internal/api/v1/*.go`:

```go
// Before:
// @Success 200 {object} dto.CustomerResponse

// After:
// @Success 200 {object} CustomerResponse
```

Each DTO struct needs a `// @name` alias:
```go
// CustomerResponse godoc
// @name CustomerResponse
type CustomerResponse struct { ... }
```

After fix: remove the 218 `x-speakeasy-name-override` entries from the overlay.
Run `make swagger && make go-sdk` to verify.

---

### Task 8: Inject `required` Arrays into Backend DTO Structs

Replace overlay `required` patches with source-level fixes. Parse `validate:"required"` tags and ensure swaggo picks them up natively (swaggo reads `binding:"required"` and `validate:"required"` tags).

After fix: remove the 27 `required` overlay patches.
Run `make swagger && make go-sdk` to verify.

---

### Task 9: Trim Overlay to Permanent Entries

After Tasks 5–8, the overlay should contain only entries that can never be expressed in swaggo:

1. `error` property rename → `detail` (Go reserved word — permanent)
2. Top-level entity renames (`CustomerResponse` → `Customer`, etc.) — keep unless the team renames the DTO structs themselves

Target: ≤ 15 overlay actions total.

---

### Phase 2 Validation Checklist

- [ ] `grep "github_com_flexprice" docs/swagger/swagger-3-0.json` → zero results
- [ ] `grep -c '"format": "date-time"' docs/swagger/swagger-3-0.json` → ≥ 150
- [ ] Overlay file has ≤ 15 `actions` entries
- [ ] `make swagger && make go-sdk && make build` runs clean end-to-end

---

## File Change Summary

### Phase 1

| File | Type | Change |
|---|---|---|
| `.speakeasy/overlays/flexprice-sdk.yaml` | Modified | Generated overlay with all name overrides, timestamp patches, required arrays, error fixes |
| `.speakeasy/gen/go.yaml` | Modified | Remove suffix settings, enable `respectRequiredFields` |
| `scripts/generate_overlay.py` | New | Script that produces the overlay — committed for repeatability |
| `api/go/errorutils/errors.go` | New (manual) | Error helper functions |
| `api/go/**` | Regenerated | Full SDK regeneration via `make go-sdk` |

### Phase 2

| File | Type | Change |
|---|---|---|
| `go.mod` / `go.sum` | Modified | swaggo version bump |
| `internal/api/v1/*.go` | Modified | Swaggo annotation cleanup (~30 files) |
| `internal/api/dto/*.go` | Modified | `// @name` aliases, `required` tag additions |
| `.speakeasy/overlays/flexprice-sdk.yaml` | Modified | Progressively trimmed |
| `docs/swagger/swagger-3-0.json` | Regenerated | Via `make swagger` |
| `api/go/**` | Regenerated | Via `make go-sdk` |

---

## Before / After Examples

### Type names
```go
// Before
resp.DtoSubscriptionResponse.GetID()
var req types.DtoCreateCustomerRequest

// After
resp.Subscription.GetID()
var req types.CreateCustomerRequest
```

### Error handling
```go
// Before
if apiErr, ok := err.(*errors.APIError); ok && apiErr.StatusCode == 404 { ... }
fmt.Println(errResp.Error_.Display) // Error_ field

// After
if errorutils.IsNotFound(err) { ... }
fmt.Println(errResp.Detail.Message) // clean field name
```

### Timestamps
```go
// Before
startDate := "2024-01-01T00:00:00Z" // string
t, _ := time.Parse(time.RFC3339, sub.StartDate)

// After (Phase 1 via overlay, Phase 2 cleaned up at source)
t := sub.StartDate // time.Time directly
```

### Required fields (after respectRequiredFields: true)
```go
// Before — required fields still need pointer helpers
req := types.DtoCreateCustomerRequest{
    Name: flexprice.String("Acme"),  // required but still *string
}

// After
req := types.CreateCustomerRequest{
    Name: "Acme",  // required → plain string, no pointer needed
}
```
