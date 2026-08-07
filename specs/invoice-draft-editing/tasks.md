---
derived_from_spec: specs/invoice-draft-editing/spec.md
status: pending
created_at: 2026-08-03
---

# Tasks — Invoice Draft Editing

Each task is one implementable unit — one agent loop or one PR. Tasks are ordered so each one is testable on its own (TDD: write the test for the task's behavior before/alongside the implementation).

**Reordering note (post-T-06):** the DTOs task was originally numbered last among the service/API tasks, but T-08 through T-19 (service methods) all reference the request DTOs in their method signatures. Moved DTOs to T-07, right after the shared totals helper, so every later task can actually compile against real types. All subsequent tasks renumbered accordingly (old T-07→T-08, ..., old T-17→T-18).

**Reordering note 2 (post-T-11, before starting T-12):** while implementing the recompute-safety cluster, discovered that `internal/ee/service/invoice_recalculate_discount.go`'s `recalculateDiscountOnInvoice` — a third recompute pathway built independently and merged into `develop` mid-implementation — has the exact clobber problem this feature exists to prevent (see spec CR-04c), through a path CR-04/CR-04b didn't originally cover. Inserted a new task, T-15, to fix it. Everything from the old T-15 onward is renumbered by one (old T-15→T-16, ..., old T-23→T-24).

---

## T-01: Schema — `is_manually_edited` on Invoice
**Files:** `ent/schema/invoice.go`, `internal/domain/invoice/model.go`
**What:** Add `field.Bool("is_manually_edited").Optional().Default(false).Comment("True once a user has manually added, edited, or removed a line item on this draft invoice")` to `Invoice.Fields()` (near `version`/`recalculated_invoice_id`, ~line 224). Add `IsManuallyEdited bool` to the `Invoice` domain struct (`internal/domain/invoice/model.go:15-139`) and wire it through `FromEnt`. Run `make generate-ent`.
**Done when:** `make generate-ent` succeeds; `ent.Invoice` and `domain/invoice.Invoice` both expose `IsManuallyEdited`/`is_manually_edited`.
**Covers:** CR-03, CR-04
**Status:** ✅ Done — commit `a91329596`.

---

## T-02: Verify `is_manually_edited` auto-migration (no file to commit)
**Files:** none expected — this task does NOT commit a `migrations/postgres/` file.
**What:** This repo's convention for a simple additive Ent-native column (nullable, with a default) is to let `make migrate-ent` apply it directly against the live DB via Ent's own schema diff — no hand-committed SQL file. Confirmed precedent: commit `0a2fb87e0` ("feat(invoice): add display_name in line items") added a comparable simple field with zero `migrations/postgres/` changes. `migrations/postgres/` is reserved for changes Ent's auto-migration can't express (sequences, extensions, backfills) — not this. Run `make migrate-ent-dry-run` to review the SQL Ent would apply, purely as a sanity check (requires a live Postgres connection — `cmd/migrate/postgres.go` always opens a real DB connection, there is no offline/static-diff mode).
**Done when:** `make migrate-ent-dry-run` output reviewed and confirmed to show only `ALTER TABLE invoices ADD COLUMN is_manually_edited ...` — no unrelated statements for other tables. **If no Postgres is reachable in the execution environment** (true for this sandbox — no Docker, no local Postgres), this verification must be deferred to CI or a developer machine with DB access before this change ships; do not fabricate or skip this check, just document that it's pending.
**Covers:** CR-03, CR-04
**Status:** ✅ Done (doc-only correction) — commit `e90efa950`. Live-DB dry-run still deferred to CI/human-with-DB-access.

---

## T-03: Schema — `parent_line_item_id` on InvoiceLineItem
**Files:** `ent/schema/invoice_line_item.go`, `internal/domain/invoice/line_item.go`
**What:** Add `field.String("parent_line_item_id").Optional().Nillable().Comment("ID of the line item this one replaced, if it was created by editing an existing line item")` to `InvoiceLineItem.Fields()` (plain string ID reference, no ent edge — same style as the existing `subscription_line_item_id` field). Add `ParentLineItemID *string` to the `InvoiceLineItem` domain struct (`internal/domain/invoice/line_item.go:15-57`) and wire it through `FromEnt`. Run `make generate-ent`. Note: `display_name` is NOT touched by this task — it stays `.Immutable()`; editing goes through archive-and-replace (T-08), which creates a new row via `Create` (unaffected by `Immutable()`, which only blocks `Update`), not an in-place rename.
**Done when:** `make generate-ent` succeeds; `ent.InvoiceLineItem` and `domain/invoice.InvoiceLineItem` both expose `ParentLineItemID`/`parent_line_item_id`.
**Covers:** CR-06, CR-06a
**Status:** ✅ Done — commit `ed05b3c76`.

---

## T-04: Verify `parent_line_item_id` auto-migration (no file to commit)
**Files:** none expected — same reasoning as T-02.
**What:** Same as T-02: a nullable string column is Ent-auto-migration-native, no `migrations/postgres/` file needed. `make migrate-ent-dry-run` sanity check only, same live-Postgres caveat.
**Done when:** Same as T-02 — reviewed if a DB is reachable, otherwise explicitly deferred to CI/human-with-DB-access before shipping.
**Covers:** CR-06, CR-06a
**Status:** ✅ Done (doc-only correction, same commit as T-02) — `e90efa950`.

---

## T-05: Repository — persist `is_manually_edited` on invoice update
**Files:** `internal/repository/ent/invoice.go` (invoice-level `Update` method)
**What:** Ensure the existing invoice `Update` repository method includes `SetIsManuallyEdited(inv.IsManuallyEdited)` in its update chain (follow the same pattern as other boolean/simple fields already persisted there).
**Done when:** Unit test: set `IsManuallyEdited = true` on an invoice via service, re-fetch from DB, assert it persisted.
**Covers:** CR-03
**Status:** ✅ Done — commit `72e991576`.

---

## T-06: Service — shared totals recalculation helper
**Files:** `internal/ee/service/invoice_line_item_edit.go` (new file)
**What:** Add a private helper `func (s *invoiceService) recalculateTotalsFromLineItems(inv *invoice.Invoice, lineItems []*invoice.InvoiceLineItem)`. Sets `inv.Subtotal` = sum of `lineItems[i].Amount` (callers must pass only published, non-archived, non-deleted line items — this helper does not filter); sets `inv.Total = max(0, Subtotal - TotalPrepaidCreditsApplied - TotalDiscount + TotalTax)`; sets `inv.AmountDue = inv.Total`; sets `inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)`. Mirrors the arithmetic already used in `applyTaxesToInvoice` (`internal/ee/service/invoice.go:3564-3570`). Does NOT recompute `TotalDiscount`/`TotalTax` — only re-derives from current values.
**Done when:** Unit test: given a set of line items and existing `TotalDiscount`/`TotalTax`, assert `Total`/`AmountDue`/`AmountRemaining` come out correct, including the floor-at-zero case.
**Covers:** CR-02
**Note:** the name in this task's original spec text was `recalculateInvoiceTotals`, but that collided with a pre-existing unrelated method on `*invoiceService` (`internal/ee/service/invoice.go:4129`, operating on `*dto.InvoiceResponse`). Implemented and documented here under the corrected name, `recalculateTotalsFromLineItems`.
**Status:** ✅ Done — commit `8da6eae45`.

---

## T-07: DTOs — request types for all 7 endpoints
**Files:** `internal/api/dto/invoice.go`
**What:** Add, each with a `Validate() error` following `UpdateInvoiceRequest`'s pattern (`internal/api/dto/invoice.go:944-970`):
```go
type AddLineItemRequest struct {
    DisplayName string          `json:"display_name" validate:"required"`
    Amount      decimal.Decimal `json:"amount" validate:"required" swaggertype:"string"`
    Quantity    decimal.Decimal `json:"quantity" validate:"required" swaggertype:"string"`
}
type UpdateLineItemRequest struct {
    DisplayName *string          `json:"display_name,omitempty"`
    Amount      *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
    Quantity    *decimal.Decimal `json:"quantity,omitempty" swaggertype:"string"`
}
type ApplyCouponRequest struct {
    CouponID   string  `json:"coupon_id" validate:"required"`
    LineItemID *string `json:"line_item_id,omitempty"`
}
type ApplyTaxRequest struct {
    TaxRateID string `json:"tax_rate_id" validate:"required"`
}
```
`AddLineItemRequest.Validate()`/`UpdateLineItemRequest.Validate()` reject negative `Amount`/`Quantity` (mirroring `InvoiceLineItem.Validate()`); `UpdateLineItemRequest.Validate()` rejects an entirely-empty request (at least one field must be set).
**Done when:** Unit tests for each `Validate()`: valid request passes; negative amount/quantity rejected; empty `UpdateLineItemRequest` rejected.
**Covers:** CR-05, CR-11
**Status:** ✅ Done — commit `b70fa8a78`.

---

## T-08: Service — `UpdateLineItem` (archive-and-replace)
**Files:** `internal/ee/service/invoice_line_item_edit.go`
**What:** `func (s *invoiceService) UpdateLineItem(ctx context.Context, invoiceID, lineItemID string, req dto.UpdateLineItemRequest) (*dto.InvoiceResponse, error)`. Inside `s.DB.WithTx`: `GetForUpdate` the invoice, reject with `ierr.NewError("invoice is not in draft status").WithHint(...).Mark(ierr.ErrValidation)` if `InvoiceStatus != Draft`. Fetch the existing line item via `LineItemRepo.Get`, verify it belongs to this invoice (tenant/environment-scoped). This is NOT an in-place update:
1. Build `newItem`, a copy of every field on the existing line item (`PriceID`, `MeterID`, `SubscriptionLineItemID`, `Currency`, `PeriodStart`/`PeriodEnd`, etc. all carried forward unchanged — CR-06), with `DisplayName`/`Amount`/`Quantity` overridden by whichever fields are present in `req` (CR-05: independent — setting one never derives another), and `newItem.ParentLineItemID = &existingItem.ID`.
2. Validate `newItem` via existing `InvoiceLineItem.Validate()`.
3. Persist the old row as archived: `existingItem.Status = types.StatusArchived`, `LineItemRepo.Update(ctx, existingItem)`.
4. Persist `newItem` via `LineItemRepo.Create(ctx, newItem)`.
5. Re-fetch all published line items for the invoice (the archived predecessor is excluded), call `recalculateTotalsFromLineItems` (T-06), set `inv.IsManuallyEdited = true`, persist invoice via `InvoiceRepo.Update`.
**Done when:** Service test: edit `display_name` only → amount/quantity unchanged on the new row; edit `quantity` only → amount unchanged (CR-05); the old row's `status` becomes `archived` and it is excluded from `ListByInvoiceID`'s default totals view; the new row's `parent_line_item_id` equals the old row's ID (CR-06); editing a line item that is itself the result of a prior edit sets the new row's `parent_line_item_id` to the immediately-preceding row's ID, not the original (CR-06a); edit on a finalized invoice → rejected (CR-01); `is_manually_edited` becomes `true` (CR-03); totals reflect the change (CR-02).
**Covers:** CR-01, CR-02, CR-03, CR-05, CR-06, CR-06a, CR-11, CR-12, CR-13
**Status:** ✅ Done — commits `ea9e37d31`, `f6e1a48c9` (archived-row guard), `1766a9f9f` (`copyInvoice` `IssueDate` fix).

---

## T-09: Service — `AddLineItem`
**Files:** `internal/ee/service/invoice_line_item_edit.go`
**What:** `func (s *invoiceService) AddLineItem(ctx context.Context, invoiceID string, req dto.AddLineItemRequest) (*dto.InvoiceResponse, error)`. Same draft-only + row-lock gate as T-08. Build a new `*invoice.InvoiceLineItem` from `req` (`DisplayName`, `Amount`, `Quantity`, `Currency` inherited from invoice, `ParentLineItemID = nil` — nothing preceded it), validate, persist via `LineItemRepo.Create`. Re-fetch published line items, recalculate totals, set `IsManuallyEdited = true`, persist invoice.
**Done when:** Service test: add a line item to a draft invoice → appears in `ListByInvoiceID` with `ParentLineItemID = nil`; totals include it; `is_manually_edited = true`; add on a non-draft invoice → rejected.
**Covers:** CR-01, CR-02, CR-03, CR-11, CR-12, CR-13
**Status:** ✅ Done — commit `86b6a2770`.

---

## T-10: Service — `RemoveLineItem`
**Files:** `internal/ee/service/invoice_line_item_edit.go`
**What:** `func (s *invoiceService) RemoveLineItem(ctx context.Context, invoiceID, lineItemID string) (*dto.InvoiceResponse, error)`. Same draft-only + row-lock gate. Call `InvoiceRepo.RemoveLineItems(ctx, invoiceID, []string{lineItemID})` (existing soft-delete, sets `status = deleted` — distinct from the `archived` status T-08 uses, since removal has no replacement row). Re-fetch remaining published line items, recalculate totals, set `IsManuallyEdited = true`, persist invoice.
**Done when:** Service test: remove a line item → its `status` is `deleted` in DB, not physically gone (CR-07); it's excluded from totals and from `ListByInvoiceID`'s default (published-only) filter; remove on non-draft → rejected.
**Covers:** CR-01, CR-02, CR-03, CR-07, CR-12, CR-13
**Status:** ✅ Done — commit `3b51794b4`. Follow-up flagged (not blocking): `InvoiceRepo.RemoveLineItems` is missing an `environment_id` filter (pre-existing gap, not exploitable via this call path since upstream calls are already tenant+env scoped) — tracked as a separate background task, not part of this plan.

---

## T-11: Service — recompute lock guard in `ComputeInvoice`
**Files:** `internal/ee/service/invoice.go` (~line 509-527, inside the `GetForUpdate` transaction block)
**What:** Immediately after the existing re-check-status-under-lock block (and after `computed = true`), add: `if inv.IsManuallyEdited { s.Logger.Info(txCtx, "skipping compute: invoice has manual line-item edits", "invoice_id", inv.ID); computed = false; return nil }`. Reset `computed = false` (not left `true`) so the no-op doesn't fire the downstream "invoice updated" webhook (`if computed && !skipped { s.publishSystemEvent(...) }`) — `computed` has exactly one consumer, this reset is the minimal correct fix. Do NOT touch `skipped` (a distinct signal for zero-amount invoices, unrelated to this guard).
**Done when:** Service test: call `ComputeInvoice` on a draft invoice with `IsManuallyEdited = true` and known line items → line items and totals are unchanged after the call, no error returned, an info log is emitted, no webhook published. Regression test: `IsManuallyEdited = false` still computes normally and still publishes the webhook.
**Covers:** CR-04
**Status:** ✅ Done — commit `e863acb3a` (hash may show as `1be3ae028` in some local views prior to a `develop` merge that occurred outside this implementation session; content is identical). Spec-compliance reviewed and passed. Code-quality review was attempted twice and both times hit a transient connection error mid-review (not a content issue) — **still needs a clean code-quality review pass before this task is considered fully closed.**

---

## T-12: Service — recompute lock guard in `RecalculateInvoiceV2`
**Files:** `internal/ee/service/invoice.go` (~line 3253, right after its existing draft-status guard — re-verify the exact line number before starting, since `develop` has moved since this plan was written)
**What:** Same shape as T-11: `if inv.IsManuallyEdited { s.Logger.Info(ctx, "skipping recalculate-v2: invoice has manual line-item edits", "invoice_id", inv.ID); return <unchanged invoice>, nil }`.
**Done when:** Service test: call `RecalculateInvoiceV2` on a locked draft invoice → no changes, no error, info log emitted.
**Covers:** CR-04
**Status:** ⏳ Not started.

---

## T-13: Service — additive-aware `applyCouponsToInvoice`
**Files:** `internal/ee/service/invoice.go` (`applyCouponsToInvoice`, ~line 4420 — re-verify), new private helper in `internal/ee/service/invoice_coupon_edit.go`
**What:** Add `func (s *invoiceService) sumAdHocCouponDiscounts(ctx context.Context, invoiceID string) (decimal.Decimal, error)`: calls `CouponApplicationRepo.List(ctx, &types.CouponApplicationFilter{InvoiceIDs: []string{invoiceID}})`, filters to records where `CouponAssociationID == ""` (ad-hoc, no standing association), sums `DiscountedAmount`. In `applyCouponsToInvoice`, change `inv.TotalDiscount = couponResult.TotalDiscountAmount` to `inv.TotalDiscount = couponResult.TotalDiscountAmount.Add(adHocTotal)` where `adHocTotal` comes from the new helper. Recompute `inv.Total`/`AmountDue`/`AmountRemaining` from the combined `TotalDiscount` as the function already does.
**Done when:** Regression test: invoice with **zero** ad-hoc coupons behaves identically to before this change (existing coupon tests still pass unmodified). New test: invoice with one ad-hoc `CouponApplication` (`CouponAssociationID = ""`) + subscription-resolved coupons both present → `TotalDiscount` reflects the sum of both.
**Covers:** CR-04b
**Status:** ⏳ Not started. Before starting, re-check `develop` for further overlapping work in this area beyond T-15's `recalculateDiscountOnInvoice` finding (see plan.md Risks).

---

## T-14: Service — additive-aware tax application
**Files:** `internal/ee/service/tax.go` (or `internal/ee/service/invoice.go`'s `applyTaxesToInvoice`/`RecalculateTaxesOnInvoice`), new private helper in `internal/ee/service/invoice_tax_edit.go`
**What:** Add `func (s *invoiceService) sumAdHocTaxAmounts(ctx context.Context, invoiceID string) (decimal.Decimal, error)`: calls `taxService.ListTaxApplied(ctx, &types.TaxAppliedFilter{EntityType: types.TaxRateEntityTypeInvoice, EntityID: invoiceID})`, filters to records where `TaxAssociationID == nil` (ad-hoc), sums `TaxAmount`. In `applyTaxesToInvoice` (`internal/ee/service/invoice.go:3557-3565` — re-verify), add this ad-hoc sum to `taxResult.TotalTaxAmount` before assigning `inv.TotalTax`.
**Done when:** Regression test: invoice with zero ad-hoc taxes behaves identically to before (existing tax tests, including `TestRecalculateTaxesOnInvoice_WithSubscriptionTax`/`_NoTaxUnchanged`/`_FinalizeNoDoubleTax` in `internal/ee/service/invoice_apply_taxes_draft_test.go`, still pass unmodified). New test: invoice with one ad-hoc `TaxApplied` record + subscription-resolved tax rates both present → `TotalTax` reflects the sum.
**Covers:** CR-04b
**Status:** ⏳ Not started.

---

## T-15: Service — fix `recalculateDiscountOnInvoice` (lock guard + ad-hoc preservation)
**Files:** `internal/ee/service/invoice_recalculate_discount.go`
**What:** This function was built independently and merged into `develop` mid-implementation (see spec CR-04c, plan.md's "The third recompute pathway" section). It has the exact clobber problem this feature protects against, through a path CR-04/CR-04b didn't originally cover. Two changes, both inside `recalculateDiscountOnInvoice`:
1. Add a lock guard at the very top of the function: `if inv.IsManuallyEdited { s.Logger.Info(ctx, "skipping discount recalc: invoice has manual line-item edits", "invoice_id", inv.ID); return nil }`. The caller (`UpdateInvoice`) already holds the row lock via `GetForUpdate` before calling this function, so no additional locking needed — just the check.
2. In `wipeCouponApplications` (same file), change the delete loop to skip `CouponApplication` records where `CouponAssociationID == ""` (ad-hoc) — only delete subscription-derived ones. Then, after `couponResult.TotalDiscountAmount` is computed (currently line 48), add the ad-hoc sum before assigning to `inv.TotalDiscount`: reuse `sumAdHocCouponDiscounts` from T-13 (do not duplicate the fetch+filter+sum logic a third time).

No separate tax fix needed — this function's tax-refresh step already calls `s.applyTaxesToInvoice`, which inherits T-14's additive-aware fix automatically.

**Done when:**
- Service test: invoice with `IsManuallyEdited = true` → `recalculateDiscountOnInvoice` (via `UpdateInvoice` with `apply_discount: true`) makes no change to any `CouponApplication`, line item, or total; info log emitted.
- Service test: invoice with one ad-hoc `CouponApplication` + subscription-resolved coupons, `IsManuallyEdited = false` → after recalculation, the ad-hoc `CouponApplication` row still exists (not deleted), and `TotalDiscount` includes both its amount and the subscription-resolved amount.
- Regression test: invoice with zero ad-hoc coupons behaves identically to before this change (existing tests for this function, if any exist — search for them first — must still pass unmodified).

**Covers:** CR-04c
**Depends on:** T-13 (needs `sumAdHocCouponDiscounts` to exist), T-14 (for the tax side to already be additive-aware) — must land after both.
**Status:** ⏳ Not started.

---

## T-16: Service — `ApplyAdHocCoupon`
**Files:** `internal/ee/service/invoice_coupon_edit.go`
**What:** `func (s *invoiceService) ApplyAdHocCoupon(ctx context.Context, invoiceID string, req dto.ApplyCouponRequest) (*dto.InvoiceResponse, error)`. Draft-only + row-lock gate (same shape as T-08, but does NOT set `IsManuallyEdited`). Determine `OriginalPrice`: `inv.Subtotal.Sub(inv.TotalDiscount)` for invoice-level (`req.LineItemID == nil`), or the target line item's `Amount.Sub(LineItemDiscount)` for line-item-level. Call `couponService.ApplyDiscount(ctx, dto.ApplyDiscountRequest{CouponID: req.CouponID, OriginalPrice: ..., Currency: inv.Currency})`. Build and persist a `CouponApplication` via its repository `Create` (`CouponAssociationID: ""`, `InvoiceID: invoiceID`, `InvoiceLineItemID: req.LineItemID`, `OriginalPrice`/`FinalPrice`/`DiscountedAmount` from the discount result). Recompute `TotalDiscount` via T-13's `sumAdHocCouponDiscounts` (plus any subscription-resolved amount already on the invoice) and re-run `recalculateTotalsFromLineItems`.
**Done when:** Service test: apply an invoice-level coupon → `CouponApplication` row created with no association, `TotalDiscount`/`Total` updated, `IsManuallyEdited` remains `false`; apply a line-item-level coupon (`LineItemID` set) → discount computed against that line item's amount; apply on non-draft invoice → rejected.
**Covers:** CR-01, CR-02, CR-08, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-17: Service — `RemoveAdHocCoupon`
**Files:** `internal/ee/service/invoice_coupon_edit.go`
**What:** `func (s *invoiceService) RemoveAdHocCoupon(ctx context.Context, invoiceID, couponApplicationID string) (*dto.InvoiceResponse, error)`. Draft-only + row-lock gate. Verify the `CouponApplication` belongs to this invoice. Delete via its repository `Delete`. Recompute `TotalDiscount` via `sumAdHocCouponDiscounts` + subscription-resolved portion, re-run `recalculateTotalsFromLineItems`.
**Done when:** Service test: remove an ad-hoc coupon application → row deleted, `TotalDiscount`/`Total` recomputed as if it never applied (CR-10); remove on non-draft → rejected.
**Covers:** CR-01, CR-02, CR-10, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-18: Service — `ApplyAdHocTax`
**Files:** `internal/ee/service/invoice_tax_edit.go`
**What:** `func (s *invoiceService) ApplyAdHocTax(ctx context.Context, invoiceID string, req dto.ApplyTaxRequest) (*dto.InvoiceResponse, error)`. Draft-only + row-lock gate (does NOT set `IsManuallyEdited`). Fetch the `TaxRate` (via existing tax repo/service getter), build a `*dto.TaxRateResponse`, compute the amount using the same switch-logic as `calculateTaxAmount` (`internal/ee/service/tax.go:1065` — same package, callable directly) against taxable base `inv.Subtotal.Sub(inv.TotalDiscount)`. Call `taxService.CreateTaxApplied(ctx, dto.CreateTaxAppliedRequest{TaxRateID: req.TaxRateID, EntityType: types.TaxRateEntityTypeInvoice, EntityID: invoiceID, TaxableAmount, TaxAmount, Currency: inv.Currency, TaxAssociationID: nil})`. Recompute `TotalTax` via T-14's `sumAdHocTaxAmounts` + subscription-resolved portion, re-run `recalculateTotalsFromLineItems`.
**Done when:** Service test: apply a tax → `TaxApplied` row created with `TaxAssociationID = nil`, `TotalTax`/`Total` updated, `IsManuallyEdited` remains `false`; apply on non-draft invoice → rejected.
**Covers:** CR-01, CR-02, CR-09, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-19: Service — `RemoveAdHocTax`
**Files:** `internal/ee/service/invoice_tax_edit.go`
**What:** `func (s *invoiceService) RemoveAdHocTax(ctx context.Context, invoiceID, taxAppliedID string) (*dto.InvoiceResponse, error)`. Draft-only + row-lock gate. Verify the `TaxApplied` record belongs to this invoice (`EntityType == invoice && EntityID == invoiceID`). Delete via `taxService.DeleteTaxApplied`. Recompute `TotalTax` via `sumAdHocTaxAmounts` + subscription-resolved portion, re-run `recalculateTotalsFromLineItems`.
**Done when:** Service test: remove an ad-hoc tax → row deleted, `TotalTax`/`Total` recomputed as if never applied (CR-10); remove on non-draft → rejected.
**Covers:** CR-01, CR-02, CR-10, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-20: API — line item handlers
**Files:** `internal/api/v1/invoice.go`, `internal/api/router.go`
**What:** Three handlers following the `VoidInvoice` pattern (`internal/api/v1/invoice.go:278-320`) exactly (swagger block with `@x-scope "write"`, param binding, service call, JSON response):
- `AddLineItem` — `POST /invoices/:id/line-items` → `dto.AddLineItemRequest` → `invoiceService.AddLineItem`
- `UpdateLineItem` — `PUT /invoices/:id/line-items/:line_item_id` → `dto.UpdateLineItemRequest` → `invoiceService.UpdateLineItem`
- `RemoveLineItem` — `DELETE /invoices/:id/line-items/:line_item_id` → `invoiceService.RemoveLineItem`

Register in `router.go`'s existing `invoices := v1Private.Group("/invoices")` block: `invoices.POST("/:id/line-items", write(types.EntityInvoice, types.ActionWrite), handlers.Invoice.AddLineItem)` (and equivalent PUT/DELETE lines).
**Done when:** Handler test (mock service layer): valid request → 200 with updated invoice; service error → propagated via `c.Error`.
**Covers:** CR-01 through CR-07, CR-06a, CR-11, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-21: API — coupon handlers
**Files:** `internal/api/v1/invoice.go`, `internal/api/router.go`
**What:** Two handlers, same pattern:
- `ApplyCoupon` — `POST /invoices/:id/coupons` → `dto.ApplyCouponRequest` → `invoiceService.ApplyAdHocCoupon`
- `RemoveCoupon` — `DELETE /invoices/:id/coupons/:coupon_application_id` → `invoiceService.RemoveAdHocCoupon`

Register both routes in `router.go`.
**Done when:** Handler test: valid apply/remove → 200; invalid coupon ID → 404/400 propagated.
**Covers:** CR-08, CR-10, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-22: API — tax handlers
**Files:** `internal/api/v1/invoice.go`, `internal/api/router.go`
**What:** Two handlers, same pattern:
- `ApplyTax` — `POST /invoices/:id/taxes` → `dto.ApplyTaxRequest` → `invoiceService.ApplyAdHocTax`
- `RemoveTax` — `DELETE /invoices/:id/taxes/:tax_applied_id` → `invoiceService.RemoveAdHocTax`

Register both routes in `router.go`.
**Done when:** Handler test: valid apply/remove → 200; invalid tax rate ID → 404/400 propagated.
**Covers:** CR-09, CR-10, CR-12, CR-13
**Status:** ⏳ Not started.

---

## T-23: Swagger + SDK regeneration
**Files:** auto-generated (`docs/swagger/`, `api/go`, `api/python`, `api/typescript`, `api/mcp`)
**What:** `make swagger && make sdk-all` after T-20/21/22. Verify all 7 new endpoints appear in `docs/swagger/swagger-3-0.json` with correct `@x-scope "write"` tagging.
**Done when:** OpenAPI spec contains all 7 new paths; `make sdk-all` completes without error.
**Covers:** developer experience
**Status:** ⏳ Not started.

---

## T-24: Verification — full test pass per verification.md
**Files:** `internal/ee/service/invoice_line_item_edit_test.go`, `internal/ee/service/invoice_coupon_edit_test.go`, `internal/ee/service/invoice_tax_edit_test.go`, `internal/api/v1/invoice_test.go` (or equivalent)
**What:** Implement all test cases listed in `verification.md`, including the three regression tests (T-13, T-14, T-15) confirming zero-ad-hoc-records behavior is unchanged across all three recompute pathways.
**Done when:** `make test` green; every CR-* in spec.md (including CR-04c) has at least one passing test; `make lint-ci` passes (no LL006 violations — every new `Error(...)` log call includes a literal `"error"` key).
**Covers:** all criteria
**Status:** ⏳ Not started.
