---
derived_from_spec: specs/invoice-draft-editing/spec.md
created_at: 2026-08-05
status: in-progress
---

# Handoff — Invoice Draft Editing

Mid-flight status snapshot for resuming subagent-driven implementation of the invoice-draft-editing feature (line item edit/add/remove, ad-hoc coupon/tax apply/remove on draft invoices). Written because the session hit repeated transient connection errors on background subagents and needed a clean resumption point.

## Where everything lives

- **Spec:** `specs/invoice-draft-editing/spec.md` — requirements, EARS acceptance criteria (CR-01 through CR-13, plus CR-04a/CR-04b/CR-06a), known limitations.
- **Plan:** `specs/invoice-draft-editing/plan.md` — architecture, affected modules, key design decisions, risks (includes an explicit note that the recompute lock guards are the highest-risk change in the whole feature).
- **Tasks:** `specs/invoice-draft-editing/tasks.md` — 23 PR-sized tasks, T-01 through T-23. **Note the reordering:** DTOs were moved from their original position (T-18) to T-07, because 12 downstream tasks reference the request DTOs in their signatures. A note at the top of that file documents this.
- **Verification:** `specs/invoice-draft-editing/verification.md` — every CR mapped to a concrete test.
- **Branch:** `claude/invoice-draft-editing-f7bc04` (this worktree). Working tree is clean as of this handoff.

## Process being followed

`superpowers:subagent-driven-development` — for each task: dispatch a fresh implementer subagent with the full task text + context (never make it read the plan file itself), then a spec-compliance reviewer subagent (verifies independently, doesn't trust the implementer's report), then a code-quality reviewer subagent (`superpowers:code-reviewer` template). Only mark a task complete once both reviews pass. Never dispatch multiple implementers in parallel. Track progress via the TaskCreate/TaskUpdate tool (23 tasks currently registered, IDs #1–#23 — note the task tracker's internal IDs do **not** match the T-XX numbers 1:1 because of the T-07/T-18 DTO reorder; check each task's `subject` field, not its numeric ID, to know which T-number it represents).

## Progress as of this handoff

| Task | Status | Commit(s) |
|---|---|---|
| T-01 Schema: `is_manually_edited` on Invoice | ✅ done | `a91329596` |
| T-02 Verify auto-migration (no file needed) | ✅ done | `e90efa950` (doc correction) |
| T-03 Schema: `parent_line_item_id` on InvoiceLineItem | ✅ done | `ed05b3c76` |
| T-04 Verify auto-migration (no file needed) | ✅ done | (same doc correction as T-02) |
| T-05 Repository: persist `is_manually_edited` | ✅ done | `72e991576` |
| T-06 Service: `recalculateTotalsFromLineItems` helper | ✅ done | `8da6eae45` (renamed from planned `recalculateInvoiceTotals` — real compile collision with a pre-existing unrelated method) |
| T-07 DTOs (moved earlier, see above) | ✅ done | `b70fa8a78` |
| T-08 Service: `UpdateLineItem` (archive-and-replace) | ✅ done | `ea9e37d31`, `f6e1a48c9` (added defensive guard), `1766a9f9f` (fixed a `copyInvoice` test-store bug found along the way) |
| T-09 Service: `AddLineItem` | ✅ done | `86b6a2770` |
| T-10 Service: `RemoveLineItem` | ✅ done | `3b51794b4` |
| **T-11 Service: recompute lock guard in `ComputeInvoice`** | ⚠️ **implementation done + spec-compliance reviewed ✅, code-quality review NOT yet completed** | `e863acb3a` |
| T-12 Service: recompute lock guard in `RecalculateInvoiceV2` | ⏳ not started | — |
| T-13 Service: additive-aware `applyCouponsToInvoice` | ⏳ not started | — |
| T-14 Service: additive-aware tax application | ⏳ not started | — |
| T-15 Service: `ApplyAdHocCoupon` | ⏳ not started | — |
| T-16 Service: `RemoveAdHocCoupon` | ⏳ not started | — |
| T-17 Service: `ApplyAdHocTax` | ⏳ not started | — |
| T-18 Service: `RemoveAdHocTax` | ⏳ not started | — |
| T-19 API: line item handlers | ⏳ not started | — |
| T-20 API: coupon handlers | ⏳ not started | — |
| T-21 API: tax handlers | ⏳ not started | — |
| T-22 Swagger + SDK regeneration | ⏳ not started | — |
| T-23 Verification: full test pass | ⏳ not started | — |

## Exactly where to resume

**T-11's code-quality review needs to be (re-)dispatched.** It was attempted twice; both times the reviewer subagent hit a transient API connection error mid-response ("Connection closed mid-response") — not a content/quality problem, the agent was still actively reading code and hadn't reached a verdict either time. T-11's implementation itself is solid and already independently spec-verified:

- **Commit:** `e863acb3a` — 11-line surgical addition to `internal/ee/service/invoice.go`'s `ComputeInvoice`, plus a new test file `internal/ee/service/invoice_compute_lock_guard_test.go`.
- **Spec-compliance review already passed**, with rigorous independent verification: confirmed the guard reads the locked (`GetForUpdate`) invoice not the pre-lock read, confirmed it returns before any line-item/coupon/tax/total mutation code can execute, confirmed the `computed = false` reset correctly suppresses the downstream webhook without affecting any real caller (traced every consumer of the `computed` variable), ran the loglint tool against the new log statement (zero errors), ran the full existing test suite for regressions (168 tests pass), and diffed against the parent commit to confirm a pre-existing `TestWalletService` flake is unrelated.
- **What's left:** dispatch a fresh `superpowers:code-reviewer` subagent for BASE_SHA `b66ead516` → HEAD_SHA `e863acb3a`. The prompt template used for the first two (failed) attempts is reconstructable from this doc's "What to check" list below — or just re-read this conversation's earlier T-11 code-quality-review dispatch and resend the same prompt.
- **What to check** (carried over from the original dispatch, so the re-run doesn't lose scope): is `computed = false` the right call vs. leaving it `true` or introducing a new signal (trace `computed`'s only consumer); is placing the guard under the lock genuinely load-bearing for correctness (what race does it close); does test coverage feel proportional given this is explicitly the highest-risk change in the plan (both no-op case and regression case, checking persisted state not just return values); any bad interaction with the SKIPPED→DRAFT transition logic just above the guard in the same function.
- Once that review passes (or comes back with fixable issues — loop until approved, per the skill), mark task tracker item for T-11 complete and move to **T-12** (recompute lock guard in `RecalculateInvoiceV2` — the sibling task, same shape, different function, do NOT touch `ComputeInvoice` again).

## A background-task chip is pending, unrelated to blocking this work

`task_f06c12fd` — "Add environment_id filter to InvoiceRepo.RemoveLineItems" — was spawned as a follow-up during T-10's code-quality review. It found that `RemoveLineItems` (and possibly its sibling `AddLineItems`) checks `tenant_id` but not `environment_id` when scoping queries, a real gap against this codebase's multi-tenancy invariant (AGENTS.md: "every query filters on both tenant_id and environment_id"). **Not exploitable through this feature's call paths** (upstream `GetForUpdate`/`Get` calls are already properly scoped before `RemoveLineItems` is ever reached), so it didn't block T-10. It's sitting as a dismissible/startable chip for the user — no action needed from whoever resumes this plan unless the user asks about it.

## Conventions established so far — keep these consistent for T-12 through T-23

- **Naming:** `is_manually_edited`/`IsManuallyEdited` (not `has_manual_edits` — renamed early to match this codebase's `is_enabled`/`is_default` boolean convention). Helper is `recalculateTotalsFromLineItems` (not `recalculateInvoiceTotals` — that name is taken by an unrelated pre-existing method operating on `*dto.InvoiceResponse`).
- **Transaction shape:** every new service method (`UpdateLineItem`/`AddLineItem`/`RemoveLineItem`, and the T-11 guard) follows the same pattern — `req.Validate()` outside the transaction if there's a request DTO, then `s.DB.WithTx` → `s.InvoiceRepo.GetForUpdate` → draft-status check (`ierr.NewError("invoice is not in draft status")...Mark(ierr.ErrValidation)`) → do the work → `recalculateTotalsFromLineItems` → set `IsManuallyEdited = true` (only for line-item mutations, never for coupon/tax ad-hoc ones, per CR-03) → `s.InvoiceRepo.Update` → return `dto.NewInvoiceResponse(lockedInv)` with `.LineItems` explicitly attached.
- **No builder pattern** for `Invoice`/`InvoiceLineItem` — deliberate, since neither has one anywhere in the codebase; plain struct construction throughout.
- **Test pattern:** `testutil.BaseServiceTestSuite`, in-memory stores, no real Postgres/Docker needed for service-layer tests (this sandbox has no Docker access per a standing user preference — never start a Docker daemon to work around missing infra; repository-layer tests that genuinely need real Postgres use a separate skip-if-unreachable pattern established in `coupon_test.go`/`invoice_test.go` from T-01/T-05, and will legitimately SKIP in this sandbox — that's expected, not a failure).
- **Every new service method gets added to the `InvoiceService` interface** in `internal/ee/service/invoice.go` with a doc comment cross-referencing the relevant CR.
- **Defensive guards beyond the literal task text are OK when justified** — e.g. T-08 added a check rejecting edits on already-archived line items, T-10 added a check rejecting removal of already-deleted/archived items, both because the underlying repository methods silently no-op on non-matching IDs rather than erroring. Verify such additions by actually reading the delegated repository method, don't assume.
- **`ent/migrate/schema.go` drift risk:** running `make generate-ent` in this environment has twice picked up unrelated pre-existing drift for other tables (prices, subscriptions, subscription_line_items) whose schema source had changed in earlier merged commits but whose generated file had never been regenerated. This only came up in T-01/T-03 (schema-changing tasks) — none of the remaining tasks touch `ent/schema/*.go`, so it shouldn't recur, but if any future task does add a schema field, check `git diff` on `ent/migrate/schema.go` and `go.sum` before committing and isolate to only the intended table.
- **No Docker, no live Postgres in this sandbox** — any task requiring `make migrate-ent-dry-run` or similar must defer that verification to CI/a dev machine (already the case for T-02/T-04, documented in `tasks.md`).

## Known highest-risk work still ahead

Per `plan.md`'s Risks section: **T-13/T-14 (additive-aware coupon/tax fix)** are the next highest-risk tasks after the T-11/T-12 lock guards, since they modify `applyCouponsToInvoice`/`applyTaxesToInvoice` — hot, existing, billing-critical code exercised by every subscription invoice compute, not just ad-hoc-coupon/tax invoices. Both need an explicit **zero-ad-hoc-records regression test** proving existing behavior is unchanged when no ad-hoc records exist, in addition to the new additive-sum test. Extra scrutiny warranted on both, same as T-11 got.

## If a subagent hits a connection error again

This session saw it twice on the same review task. It's a transient infra failure on the Agent tool call itself (`API Error: Connection closed mid-response`), not a signal about the code being reviewed. Just re-dispatch the same review with the same prompt — don't try to work around it by skipping the review or fixing things directly as the controller.
