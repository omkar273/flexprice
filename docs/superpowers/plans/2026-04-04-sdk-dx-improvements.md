# SDK DX Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clean up the Flexprice Go SDK v2.1.0 so type names, timestamps, and error handling are idiomatic — delivered in two phases: overlay-first (no backend changes), then backend root-cause cleanup.

**Architecture:** Phase 1 uses a generated Speakeasy overlay file and config edits to fix all SDK output without touching the backend. Phase 2 cleans up root causes in backend swaggo annotations so the overlay shrinks to a minimal permanent set.

**Tech Stack:** Python 3 (overlay generation script), Go 1.23, swaggo/swag v1.16.4+, Speakeasy CLI, OpenAPI Overlay spec v1.0

---

## File Map

### Phase 1 — New / Modified Files

| File | Action | Purpose |
|---|---|---|
| `scripts/generate_overlay.py` | Create | Reads spec + Go DTOs, writes the overlay file |
| `.speakeasy/overlays/flexprice-sdk.yaml` | Overwrite | All name overrides, timestamp patches, error fixes |
| `.speakeasy/gen/go.yaml` | Modify | Delete suffix keys, enable `respectRequiredFields` |
| `api/go/errorutils/errors.go` | Create (manual) | SDK error helper functions, never overwritten by Speakeasy |
| `api/go/**` | Regenerate | Full SDK output from `make go-sdk` |

### Phase 2 — New / Modified Files

| File | Action | Purpose |
|---|---|---|
| `go.mod` / `go.sum` | Modify | swaggo version bump |
| `internal/api/v1/*.go` | Modify | Strip `dto.` prefix from all swaggo annotations |
| `internal/api/dto/*.go` | Modify | Add `// @name` aliases to DTO structs |
| `docs/swagger/swagger-3-0.json` | Regenerate | Via `make swagger` |
| `.speakeasy/overlays/flexprice-sdk.yaml` | Trim | Remove entries superseded by backend fixes |
| `api/go/**` | Regenerate | Via `make go-sdk` |

---

## Phase 1 — Clean SDK via Overlay & Config

---

### Task 1: Write the overlay generation script

**Files:**
- Create: `scripts/generate_overlay.py`

The script reads `docs/swagger/swagger-3-0.json` and `internal/api/dto/*.go`, then writes `.speakeasy/overlays/flexprice-sdk.yaml`. It is committed and re-runnable whenever the spec changes.

- [ ] **Step 1.1: Create the script**

```python
#!/usr/bin/env python3
"""
generate_overlay.py — Generates .speakeasy/overlays/flexprice-sdk.yaml

Reads docs/swagger/swagger-3-0.json and produces a Speakeasy overlay that:
  - Strips dto. prefix from all 218 dto.* schemas
  - Renames top-level entity response types (e.g. CustomerResponse → Customer)
  - Renames errors.* schemas to clean names
  - Renames the 'error' property on ErrorResponse → 'detail' (avoids Error_ in Go)
  - Patches 177 timestamp fields with format: date-time
"""

import json
import re
import sys
from pathlib import Path

SPEC_PATH = Path("docs/swagger/swagger-3-0.json")
OVERLAY_PATH = Path(".speakeasy/overlays/flexprice-sdk.yaml")

# Top-level entity response types — strip Response suffix for cleaner SDK names
ENTITY_RENAMES = {
    "dto.CustomerResponse":     "Customer",
    "dto.SubscriptionResponse": "Subscription",
    "dto.InvoiceResponse":      "Invoice",
    "dto.PlanResponse":         "Plan",
    "dto.PriceResponse":        "Price",
    "dto.AddonResponse":        "Addon",
    "dto.WalletResponse":       "Wallet",
    "dto.PaymentResponse":      "Payment",
    "dto.CouponResponse":       "Coupon",
    "dto.FeatureResponse":      "Feature",
}

# Timestamp-like field name patterns — any string field matching these gets format: date-time
TIMESTAMP_PATTERNS = re.compile(
    r"(_at|_date|_start|_end|_time|_anchor|_period|expires_at|expiry|"
    r"due_date|close_time|archived_at|applied_at|executed_at|failed_at|"
    r"finalized_at|completed_at|last_used_at|balance_updated_at)$"
)


def quote(s: str) -> str:
    """Wrap a schema name in single quotes for JSONPath bracket notation."""
    return f"['{s}']"


def build_actions(spec: dict) -> list:
    actions = []
    schemas = spec.get("components", {}).get("schemas", {})

    # ── 1. Strip dto. prefix & apply entity renames ─────────────────────────
    for name in schemas:
        if not name.startswith("dto."):
            continue
        if name in ENTITY_RENAMES:
            override = ENTITY_RENAMES[name]
        else:
            override = name[4:]  # strip "dto."
        actions.append({
            "target": f"$.components.schemas{quote(name)}",
            "update": {"x-speakeasy-name-override": override},
        })

    # ── 2. Rename errors.* schemas ──────────────────────────────────────────
    for name in schemas:
        if not name.startswith("errors."):
            continue
        override = name[7:]  # strip "errors."
        actions.append({
            "target": f"$.components.schemas{quote(name)}",
            "update": {"x-speakeasy-name-override": override},
        })

    # ── 3. Rename 'error' property on ErrorResponse → 'detail' ─────────────
    #    The JSON field name 'error' is a Go reserved keyword; Speakeasy
    #    escapes it to Error_ which is confusing. Renaming to 'detail' in
    #    the SDK (via x-speakeasy-name-override on the property) fixes this.
    if "errors.ErrorResponse" in schemas:
        err_props = schemas["errors.ErrorResponse"].get("properties", {})
        if "error" in err_props:
            actions.append({
                "target": "$.components.schemas['errors.ErrorResponse'].properties.error",
                "update": {"x-speakeasy-name-override": "detail"},
            })

    # ── 4. Patch timestamp fields with format: date-time ────────────────────
    for schema_name, schema in schemas.items():
        for prop_name, prop in schema.get("properties", {}).items():
            if (
                prop.get("type") == "string"
                and prop.get("format") != "date-time"
                and TIMESTAMP_PATTERNS.search(prop_name)
            ):
                actions.append({
                    "target": (
                        f"$.components.schemas{quote(schema_name)}"
                        f".properties{quote(prop_name)}"
                    ),
                    "update": {"format": "date-time"},
                })

    return actions


def write_overlay(actions: list) -> None:
    # Write as YAML manually to avoid requiring PyYAML and to control formatting
    lines = [
        "overlay: 1.0.0",
        "info:",
        "  title: Flexprice SDK and MCP customizations",
        "  version: 2.1.0",
        "actions:",
    ]
    for action in actions:
        target = action["target"]
        update = action["update"]
        lines.append(f"  - target: \"{target}\"")
        lines.append("    update:")
        for k, v in update.items():
            # Quote string values; leave booleans/numbers unquoted
            if isinstance(v, str):
                lines.append(f"      {k}: {v}")
            else:
                lines.append(f"      {k}: {json.dumps(v)}")
    OVERLAY_PATH.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> None:
    if not SPEC_PATH.exists():
        print(f"ERROR: {SPEC_PATH} not found. Run from repo root.", file=sys.stderr)
        sys.exit(1)

    spec = json.loads(SPEC_PATH.read_text(encoding="utf-8"))
    actions = build_actions(spec)
    write_overlay(actions)

    print(f"Written {len(actions)} overlay actions to {OVERLAY_PATH}")

    # Print a summary breakdown
    name_overrides = sum(1 for a in actions if "x-speakeasy-name-override" in a["update"])
    ts_patches = sum(1 for a in actions if a["update"].get("format") == "date-time")
    print(f"  Name overrides:   {name_overrides}")
    print(f"  Timestamp patches: {ts_patches}")


if __name__ == "__main__":
    main()
```

- [ ] **Step 1.2: Make the script executable**

```bash
chmod +x scripts/generate_overlay.py
```

- [ ] **Step 1.3: Run the script from repo root and verify output**

```bash
cd /path/to/flexprice  # repo root
python3 scripts/generate_overlay.py
```

Expected output:
```
Written NNN overlay actions to .speakeasy/overlays/flexprice-sdk.yaml
  Name overrides:   220+
  Timestamp patches: 177+
```

- [ ] **Step 1.4: Spot-check the overlay file**

```bash
# Should contain dto.CustomerResponse → Customer
grep "CustomerResponse" .speakeasy/overlays/flexprice-sdk.yaml | head -3

# Should contain the error field rename
grep "detail" .speakeasy/overlays/flexprice-sdk.yaml

# Should contain timestamp patches
grep "date-time" .speakeasy/overlays/flexprice-sdk.yaml | wc -l
```

Expected:
- `CustomerResponse` lines present, one showing `x-speakeasy-name-override: Customer`
- `detail` line present under `errors.ErrorResponse`.properties.error
- `date-time` count ≥ 150

- [ ] **Step 1.5: Commit**

```bash
git add scripts/generate_overlay.py .speakeasy/overlays/flexprice-sdk.yaml
git commit -m "feat(sdk): generate speakeasy overlay for naming and timestamp fixes"
```

---

### Task 2: Update Speakeasy generator config

**Files:**
- Modify: `.speakeasy/gen/go.yaml`

- [ ] **Step 2.1: Read the current config**

Open `.speakeasy/gen/go.yaml`. Confirm these keys are present under the `go:` block:
```yaml
go:
  inputModelSuffix: Input
  outputModelSuffix: Output
  respectRequiredFields: false
```

- [ ] **Step 2.2: Delete the suffix keys and flip respectRequiredFields**

Remove the `inputModelSuffix` and `outputModelSuffix` lines entirely. Change `respectRequiredFields` to `true`.

The `go:` block should look like this after the edit (showing only the changed lines in context):
```yaml
go:
  version: 2.0.0
  additionalDependencies: {}
  baseErrorName: FlexpriceError
  clientServerStatusCodesAsErrors: true
  defaultErrorName: APIError
  flattenGlobalSecurity: true
  forwardCompatibleEnumsByDefault: true
  forwardCompatibleUnionsByDefault: tagged-and-untagged
  imports:
    option: openapi
    paths:
      callbacks: models/callbacks
      errors: models/errors
      operations: models/dtos
      shared: models/types
      webhooks: models/webhooks
  includeEmptyObjects: true
  inferUnionDiscriminators: true
  maxMethodParams: 4
  methodArguments: require-security-and-request
  modulePath: github.com/flexprice/go-sdk
  multipartArrayFormat: standard
  nullableOptionalWrapper: true
  respectRequiredFields: true
  respectTitlesForPrimitiveUnionMembers: true
  responseFormat: envelope-http
  sdkPackageName: flexprice
  unionStrategy: populated-fields
```

Note: `inputModelSuffix` and `outputModelSuffix` lines are gone. `respectRequiredFields` is `true`.

- [ ] **Step 2.3: Commit**

```bash
git add .speakeasy/gen/go.yaml
git commit -m "feat(sdk): enable respectRequiredFields and remove conflicting suffix config"
```

---

### Task 3: Create the errorutils package

**Files:**
- Create: `api/go/errorutils/errors.go`

This file is hand-written and lives outside Speakeasy's generated file set — it will not be overwritten on SDK regeneration.

- [ ] **Step 3.1: Create the file**

```go
// Package errorutils provides helper functions for inspecting Flexprice SDK errors.
// These functions check the HTTP status code of an *errors.APIError.
//
// Usage:
//
//	_, err := client.Customers.CreateCustomer(ctx, req)
//	if errorutils.IsConflict(err) {
//	    // handle duplicate customer
//	}
package errorutils

import (
	"net/http"

	sderr "github.com/flexprice/flexprice-go/v2/models/errors"
)

// IsNotFound reports whether err is an API error with HTTP 404.
func IsNotFound(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusNotFound
}

// IsValidation reports whether err is an API error with HTTP 400.
func IsValidation(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusBadRequest
}

// IsConflict reports whether err is an API error with HTTP 409.
func IsConflict(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusConflict
}

// IsRateLimit reports whether err is an API error with HTTP 429.
func IsRateLimit(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusTooManyRequests
}

// IsPermissionDenied reports whether err is an API error with HTTP 403.
func IsPermissionDenied(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusForbidden
}

// IsServerError reports whether err is an API error with HTTP 5xx.
func IsServerError(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode >= http.StatusInternalServerError
}
```

- [ ] **Step 3.2: Verify it compiles**

```bash
cd api/go
go build ./errorutils/...
```

Expected: no output (clean compile).

- [ ] **Step 3.3: Commit**

```bash
git add api/go/errorutils/errors.go
git commit -m "feat(sdk): add errorutils package with IsNotFound, IsValidation etc"
```

---

### Task 4: Regenerate the SDK

- [ ] **Step 4.1: Run the SDK generator**

```bash
make go-sdk
```

This runs `speakeasy run --target flexprice-go` which applies the overlay on top of the spec and regenerates `api/go/`. Expected: no errors in Speakeasy output.

- [ ] **Step 4.2: Confirm errorutils was not deleted**

```bash
ls api/go/errorutils/errors.go
```

Expected: file still present (Speakeasy does not delete files it did not generate).

- [ ] **Step 4.3: Validate — no Dto prefix in generated types**

```bash
grep -r "DtoCustomer\|DtoSubscription\|DtoInvoice\|DtoPrice\|DtoPlan\|DtoCoupon\|DtoAddon\|DtoWallet\|DtoPayment\|DtoFeature" api/go/models/
```

Expected: zero matches.

- [ ] **Step 4.4: Validate — no Error_ field**

```bash
grep -r "Error_" api/go/
```

Expected: zero matches.

- [ ] **Step 4.5: Validate — no ErrorsError prefix**

```bash
grep -r "ErrorsError" api/go/
```

Expected: zero matches.

- [ ] **Step 4.6: Validate — timestamps are time.Time**

```bash
grep -r "time\.Time" api/go/models/types/ | wc -l
```

Expected: ≥ 100 matches (timestamp fields now generate as `time.Time`).

- [ ] **Step 4.7: Build the SDK**

```bash
cd api/go && go build ./...
```

Expected: clean compile, no errors.

- [ ] **Step 4.8: Commit**

```bash
git add api/go/
git commit -m "feat(sdk): regenerate SDK v2.1.0 — clean type names, time.Time timestamps, error helpers"
```

---

### Phase 1 Complete ✓

At this point the SDK is clean. Consumers will see:
- `resp.Subscription` not `resp.DtoSubscriptionResponse`
- `time.Time` on all date fields
- `errorutils.IsNotFound(err)` instead of status code inspection
- `errResp.Detail` not `errResp.Error_`

---

## Phase 2 — Backend Root Cause Cleanup

---

### Task 5: Upgrade swaggo and audit timestamp coverage

**Files:**
- Modify: `go.mod`, `go.sum`
- Regenerate: `docs/swagger/swagger-3-0.json`

- [ ] **Step 5.1: Check current swaggo version**

```bash
grep "swaggo/swag" go.mod
```

Expected: `github.com/swaggo/swag v1.16.4`

- [ ] **Step 5.2: Upgrade swaggo**

```bash
go get github.com/swaggo/swag@latest
go mod tidy
```

- [ ] **Step 5.3: Regenerate the swagger spec**

```bash
make swagger
```

Expected: `docs/swagger/swagger-3-0.json` updated.

- [ ] **Step 5.4: Count how many timestamp fields now have format: date-time natively**

```bash
python3 -c "
import json
with open('docs/swagger/swagger-3-0.json') as f:
    spec = json.load(f)
count = sum(
    1 for s in spec['components']['schemas'].values()
    for p in s.get('properties', {}).values()
    if p.get('format') == 'date-time'
)
print('Fields with format: date-time:', count)
"
```

Note the count. If it is ≥ 150, swaggo upgrade resolved most timestamp fields automatically.

- [ ] **Step 5.5: Remove resolved timestamp patches from the overlay**

Run the following to re-generate the overlay, this time skipping fields that already have `format: date-time` in the spec:

```bash
python3 scripts/generate_overlay.py
```

The script already skips fields where `prop.get("format") == "date-time"`, so re-running it after the swaggo upgrade automatically removes redundant patches.

- [ ] **Step 5.6: Regenerate SDK and verify timestamps still work**

```bash
make go-sdk
grep -r "time\.Time" api/go/models/types/ | wc -l
```

Expected: same or higher count as Phase 1.

- [ ] **Step 5.7: Commit**

```bash
git add go.mod go.sum docs/swagger/ .speakeasy/overlays/flexprice-sdk.yaml api/go/
git commit -m "chore(sdk): upgrade swaggo and trim timestamp overlay patches"
```

---

### Task 6: Fix 5 leaked Go package path schemas

**Files:**
- Modify: files in `internal/api/v1/` containing the annotations that reference domain structs

The 5 leaked schemas are:
```
github_com_flexprice_flexprice_internal_domain_addon.Addon
github_com_flexprice_flexprice_internal_domain_coupon.Coupon
github_com_flexprice_flexprice_internal_domain_customer.Customer
github_com_flexprice_flexprice_internal_domain_feature.Feature
github_com_flexprice_flexprice_internal_domain_plan.Plan
```

- [ ] **Step 6.1: Find the offending annotations**

```bash
grep -rn "addon\.Addon\|coupon\.Coupon\|customer\.Customer\|feature\.Feature\|plan\.Plan" internal/api/v1/ | grep "@"
```

This will show lines like:
```
internal/api/v1/plan.go:42:// @Success 200 {object} plan.Plan
```

- [ ] **Step 6.2: Replace each annotation with its DTO equivalent**

For each file found, change:
```go
// @Success 200 {object} addon.Addon
```
to:
```go
// @Success 200 {object} dto.AddonResponse
```

Apply for all 5 domain types:
- `addon.Addon` → `dto.AddonResponse`
- `coupon.Coupon` → `dto.CouponResponse`
- `customer.Customer` → `dto.CustomerResponse`
- `feature.Feature` → `dto.FeatureResponse`
- `plan.Plan` → `dto.PlanResponse`

- [ ] **Step 6.3: Regenerate spec and verify leaks are gone**

```bash
make swagger
python3 -c "
import json
with open('docs/swagger/swagger-3-0.json') as f:
    spec = json.load(f)
leaks = [k for k in spec['components']['schemas'] if 'github_com_flexprice' in k]
print('Leaked schemas:', leaks)
"
```

Expected: `Leaked schemas: []`

- [ ] **Step 6.4: Regenerate SDK and build**

```bash
make go-sdk
cd api/go && go build ./...
```

- [ ] **Step 6.5: Commit**

```bash
git add internal/api/v1/ docs/swagger/ api/go/
git commit -m "fix(sdk): replace domain struct annotations with DTO equivalents"
```

---

### Task 7: Strip dto. prefix from all backend swaggo annotations

**Files:**
- Modify: `internal/api/v1/*.go` (~30 files)
- Modify: `internal/api/dto/*.go` (add `// @name` aliases)

This is a systematic find-replace. After this task, Speakeasy will see clean schema names directly from the spec and the 218 `x-speakeasy-name-override` entries in the overlay become redundant.

- [ ] **Step 7.1: Count the annotations to change**

```bash
grep -r "@Success\|@Failure\|@Param" internal/api/v1/ | grep "dto\." | wc -l
```

Note the count (expect ~300–500 annotation lines referencing `dto.*`).

- [ ] **Step 7.2: Add @name aliases to all DTO response structs**

For every response DTO struct in `internal/api/dto/*.go`, add a `// @name` comment so swaggo emits a clean schema name instead of the package-qualified one.

Example — in `internal/api/dto/customer.go`:
```go
// CustomerResponse is the response for customer operations.
//
// @name CustomerResponse
type CustomerResponse struct {
    *customer.Customer
    // ...
}
```

Run this to find all response structs that need aliases:
```bash
grep -rn "type.*Response struct" internal/api/dto/ | grep -v "_test"
```

Add `// @name TypeName` above each struct's doc comment (or as its first comment if none exists).

- [ ] **Step 7.3: Update annotations in handler files**

In each file under `internal/api/v1/`, replace `dto.XxxResponse` with `XxxResponse` in all `@Success` lines:

```bash
# Dry-run to see what will change
grep -rn "dto\." internal/api/v1/ | grep "// @"
```

Perform the replacements. The pattern is: `dto\.(\w+)` → `$1` in swaggo comment lines only.

For each file, the change looks like:
```go
// Before:
// @Success 200 {object} dto.CustomerResponse

// After:
// @Success 200 {object} CustomerResponse
```

- [ ] **Step 7.4: Regenerate spec and verify schema names are clean**

```bash
make swagger
python3 -c "
import json
with open('docs/swagger/swagger-3-0.json') as f:
    spec = json.load(f)
dto_names = [k for k in spec['components']['schemas'] if k.startswith('dto.')]
print(f'dto. prefixed schemas remaining: {len(dto_names)}')
if dto_names:
    print('Still present:', dto_names[:10])
"
```

Expected: `dto. prefixed schemas remaining: 0`

- [ ] **Step 7.5: Remove the 218 x-speakeasy-name-override entries from the overlay**

Re-run the overlay generator. Because the schemas are now named cleanly in the spec, the `dto.*` name overrides are no longer needed. Modify `scripts/generate_overlay.py` to skip the dto-prefix-stripping section (or gate it on whether `dto.` schemas still exist in the spec):

In `scripts/generate_overlay.py`, wrap the dto section:
```python
    # ── 1. Strip dto. prefix & apply entity renames ─────────────────────────
    # Only needed if spec still has dto.-prefixed schema names (pre-Phase-2)
    dto_schemas = [n for n in schemas if n.startswith("dto.")]
    if dto_schemas:
        for name in dto_schemas:
            if name in ENTITY_RENAMES:
                override = ENTITY_RENAMES[name]
            else:
                override = name[4:]
            actions.append({
                "target": f"$.components.schemas{quote(name)}",
                "update": {"x-speakeasy-name-override": override},
            })
    else:
        # Schema names are already clean — add entity renames directly
        for clean_name, override in ENTITY_RENAMES.items():
            schema_name = clean_name[4:]  # strip "dto." to get "CustomerResponse"
            if schema_name in schemas:
                actions.append({
                    "target": f"$.components.schemas{quote(schema_name)}",
                    "update": {"x-speakeasy-name-override": override},
                })
```

Then re-run:
```bash
python3 scripts/generate_overlay.py
```

Verify the overlay now has far fewer entries (≤ 20).

- [ ] **Step 7.6: Regenerate SDK, verify no Dto prefix, build**

```bash
make go-sdk
grep -r "DtoCustomer\|DtoSubscription\|DtoInvoice\|DtoPrice\|DtoPlan" api/go/models/
cd api/go && go build ./...
```

Expected: zero grep matches, clean build.

- [ ] **Step 7.7: Commit**

```bash
git add internal/api/v1/ internal/api/dto/ docs/swagger/ .speakeasy/overlays/flexprice-sdk.yaml scripts/generate_overlay.py api/go/
git commit -m "fix(sdk): strip dto. prefix from all swaggo annotations — overlay trimmed to entity renames only"
```

---

### Task 8: Confirm required-array coverage (verification only)

**Files:** None — this is a verification step, no changes expected.

The design spec called for scripting `required` arrays into backend DTO structs from `validate:"required"` tags. Investigation confirmed that all 27 request schemas missing `required` arrays are PATCH-style update requests with no required fields at all — there is nothing to add. The `respectRequiredFields: true` config (Task 2) benefits the 52 schemas that already have `required` arrays in the spec.

- [ ] **Step 8.1: Verify no required fields were missed**

```bash
python3 -c "
import re, glob, json

files = glob.glob('internal/api/dto/*.go')
structs_with_required = {}

for f in files:
    with open(f) as fh:
        content = fh.read()
    struct_blocks = re.findall(r'type\s+(\w+)\s+struct\s*\{([^}]+)\}', content, re.DOTALL)
    for name, body in struct_blocks:
        required_fields = []
        for line in body.split('\n'):
            json_match = re.search(r'json:\"([^,\"]+)', line)
            validate_match = re.search(r'validate:\"([^\"]+)\"', line)
            if json_match and validate_match:
                if 'required' in validate_match.group(1).split(','):
                    required_fields.append(json_match.group(1))
        if required_fields:
            structs_with_required[name] = required_fields

with open('docs/swagger/swagger-3-0.json') as f:
    spec = json.load(f)

schemas = spec['components']['schemas']
missing = [k for k, v in schemas.items() if k.startswith('dto.') and k.endswith('Request') and not v.get('required')]
missed = [(m, structs_with_required.get(m[4:], [])) for m in missing if structs_with_required.get(m[4:])]
if missed:
    print('SCHEMAS MISSING REQUIRED ARRAYS THAT HAVE REQUIRED FIELDS:')
    for name, fields in missed:
        print(f'  {name}: {fields}')
else:
    print('PASS: all request schemas missing required arrays have no validate:required fields')
"
```

Expected output: `PASS: all request schemas missing required arrays have no validate:required fields`

---

### Task 9: Final overlay trim and end-to-end validation

- [ ] **Step 9.1: Check overlay action count**

```bash
grep -c "target:" .speakeasy/overlays/flexprice-sdk.yaml
```

Expected: ≤ 15 (only permanent entries: `error`→`detail` rename + entity renames like `CustomerResponse`→`Customer`).

- [ ] **Step 9.2: Run full end-to-end pipeline**

```bash
make swagger && make go-sdk
```

Expected: both commands complete without errors.

- [ ] **Step 9.3: Run all Phase 2 validation checks**

```bash
# No leaked package paths
python3 -c "
import json
with open('docs/swagger/swagger-3-0.json') as f:
    s = json.load(f)
leaks = [k for k in s['components']['schemas'] if 'github_com_flexprice' in k]
dto = [k for k in s['components']['schemas'] if k.startswith('dto.')]
print('Leaked paths:', leaks)
print('dto. schemas:', dto)
"

# Timestamp coverage
python3 -c "
import json
with open('docs/swagger/swagger-3-0.json') as f:
    s = json.load(f)
count = sum(1 for sc in s['components']['schemas'].values() for p in sc.get('properties',{}).values() if p.get('format')=='date-time')
print('date-time fields:', count, '(expect ≥ 150)')
"

# SDK cleanliness
grep -r "DtoCustomer\|DtoSubscription\|DtoInvoice" api/go/ && echo "FAIL: Dto prefix still present" || echo "PASS: no Dto prefix"
grep -r "Error_" api/go/ && echo "FAIL: Error_ still present" || echo "PASS: no Error_"

# Build
cd api/go && go build ./... && echo "PASS: SDK builds clean"
```

- [ ] **Step 9.4: Commit**

```bash
git add .
git commit -m "chore(sdk): Phase 2 complete — spec clean, overlay at permanent minimum"
```

---

## Quick Reference: Validation Commands

```bash
# Phase 1 complete check
grep -r "DtoCustomer\|DtoSubscription\|DtoInvoice\|DtoPrice\|DtoPlan" api/go/models/ | wc -l  # expect 0
grep -r "Error_" api/go/ | wc -l                                                                # expect 0
grep -r "ErrorsError" api/go/ | wc -l                                                           # expect 0
grep -r "time\.Time" api/go/models/types/ | wc -l                                               # expect ≥ 100
ls api/go/errorutils/errors.go                                                                  # expect file present
cd api/go && go build ./...                                                                     # expect clean

# Phase 2 complete check
grep "github_com_flexprice" docs/swagger/swagger-3-0.json | wc -l    # expect 0
python3 -c "import json; s=json.load(open('docs/swagger/swagger-3-0.json')); print(sum(1 for sc in s['components']['schemas'].values() for p in sc.get('properties',{}).values() if p.get('format')=='date-time'))"  # expect ≥ 150
grep -c "target:" .speakeasy/overlays/flexprice-sdk.yaml              # expect ≤ 15
make swagger && make go-sdk && cd api/go && go build ./...            # expect all clean
```
