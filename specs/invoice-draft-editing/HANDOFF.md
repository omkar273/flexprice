---
derived_from_spec: specs/invoice-draft-editing/spec.md
created_at: 2026-08-07
status: in-progress
---

# Handoff — Invoice Draft Editing

Status snapshot for resuming work on the invoice-draft-editing feature (line item edit/add/remove, ad-hoc coupon/tax apply/remove on draft invoices). This supersedes the earlier version of this doc — the branch has since been split into four PRs, so **the resume point is no longer this branch**.

## Where everything lives

- **Spec/plan/tasks/verification:** `specs/invoice-draft-editing/{spec,plan,tasks,verification}.md` — full requirements, EARS acceptance criteria (CR-01 through CR-13, plus CR-04a/CR-04b/CR-04c/CR-06a), design decisions, and the 24-task plan (T-01 through T-24). **These docs exist only on this branch (`invoice-draft-editing`)** — they were deliberately removed from all four PR branches so the PRs' diffs are code-only. Keep updating them here as the source of truth; don't try to re-add them to the PR branches.
- **Branch-split plan:** `specs/invoice-draft-editing/BRANCH_SPLIT_PLAN.md` — exact commit-to-PR mapping and verification results for how this branch was split.
- **This worktree's branch (`invoice-draft-editing`)** is now a reference/planning branch only — it has the full original linear history and the planning docs, but **new implementation work should NOT continue here.**

## The branch was split into 4 PRs — work now continues on PR3's branch

| PR | Branch | Tasks | Status |
|---|---|---|---|
| [#2481](https://github.com/flexprice/flexprice/pull/2481) | `invoice-draft-editing-foundation` | T-01–T-06 | ✅ Open, ready for review |
| [#2482](https://github.com/flexprice/flexprice/pull/2482) | `invoice-draft-editing-line-item-methods` | T-07–T-10 | ✅ Open, ready for review |
| [#2483](https://github.com/flexprice/flexprice/pull/2483) | `invoice-draft-editing-recompute-safety` | T-11–T-15 | ⚠️ Open as **draft/WIP** — T-11 done, T-12–T-15 not yet built |
| (not created) | `invoice-draft-editing-api-surface` | T-16–T-24 | ⏳ Not started — branch off PR3's tip once PR3 is complete |

**To resume implementation: `git checkout invoice-draft-editing-recompute-safety`, then continue the subagent-driven-development loop for T-12.** Do not implement further tasks on the `invoice-draft-editing` branch — it won't be merged directly; the four PR branches are what land in `develop`.

All three open PRs base against `develop` directly (not against each other as GitHub PR bases) — each PR's diff currently includes the prior PRs' commits too, and will shrink automatically as #2481 and #2482 merge. This is intentional (documented in each PR's description), not a mistake.

## What's done, verified, and already cleaned up in the 3 open PRs

- T-01–T-14 minus T-12/13/14 (i.e., T-01–T-11) are implemented, individually spec-compliance-reviewed and code-quality-reviewed via the subagent-driven-development process, **except T-11's code-quality review — see below.**
- All three PR branches have had a cleanup pass applied on top of the original implementation commits:
  - Removed domain-model `FromEnt` round-trip tests and DTO `Validate()` tests — only service-layer tests remain (per explicit user preference: service logic gets tests, DTOs/models don't).
  - Removed `specs/invoice-draft-editing/` entirely from all three branches.
  - Capped every comment we added at 1-2 lines; stripped `CR-XX`/`T-XX` references from code comments (they'd dangle now that the spec docs aren't in the PR diffs).
  - Fixed a real bug found along the way: `internal/testutil/inmemory_invoice_store.go`'s `copyInvoice` was silently dropping `IsManuallyEdited`/`ParentLineItemID`/`IssueDate` on every Create/Update/Get — now fixed, load-bearing for every test in PR2/PR3.
- Each branch independently verified: `go build`/`go vet` clean, all its own tests pass, standalone (not just as part of the full stack).

## Exactly where to resume

**On `invoice-draft-editing-recompute-safety`:**

1. **First, finish T-11's code-quality review.** It was attempted twice in the original session and both times hit a transient API connection error mid-response — not a content problem, the reviewer hadn't reached a verdict either time. T-11's spec-compliance review already passed with rigorous independent verification (confirmed the guard reads the locked invoice, returns before any mutation code, the `computed = false` reset is correct and doesn't affect any real caller, loglint passes, 168 existing tests pass with no regression). Re-dispatch a fresh `superpowers:code-reviewer` subagent comparing the commit before T-11 to the T-11 commit on this branch (note: commit hashes on this branch differ from the original `invoice-draft-editing` branch's hashes, since they were cherry-picked — use `git log` on `invoice-draft-editing-recompute-safety` to find the actual parent/T-11 commit pair). What to check: is `computed = false` the right call (trace `computed`'s only consumer); is placing the guard under the lock load-bearing (what race does it close); is test coverage proportional to this being the highest-risk change in the plan; any bad interaction with the SKIPPED→DRAFT transition logic just above the guard.
2. **Then T-12** — recompute lock guard in `RecalculateInvoiceV2`, same shape as T-11's `ComputeInvoice` guard, different function. Re-verify the exact line number in `internal/ee/service/invoice.go` before starting — `develop` may have moved since `tasks.md` was written.
3. **Then T-13** — additive-aware `applyCouponsToInvoice`. Before starting, re-check `develop` for further overlapping work in this area (see "A `develop`-drift risk" below — this already happened once).
4. **Then T-14** — additive-aware tax application (`applyTaxesToInvoice`).
5. **Then T-15** — fix `recalculateDiscountOnInvoice` (CR-04c). Must land after T-13/T-14 since it reuses `sumAdHocCouponDiscounts` and inherits T-14's tax fix automatically. See `plan.md`'s "The third recompute pathway" section and `tasks.md`'s T-15 for full detail — this fixes a real gap found mid-implementation where an independently-built pathway (`PUT /invoices/:id {apply_discount: true}`) doesn't respect the manual-edit lock and unconditionally wipes ad-hoc coupons.
6. Once T-11–T-15 are all done and reviewed, mark PR3 ready for review (undraft it), then branch `invoice-draft-editing-api-surface` off its tip and continue with T-16 onward.

**Apply the same cleanup pass (test trimming, comment capping, no specs dir) to every new task's commits before/when pushing**, consistent with what's already on PR1/PR2/PR3 — don't let it drift back to the original, more verbose style.

## A `develop`-drift risk already happened once — check again before T-13

While this feature was mid-implementation, `develop` picked up `recalculateDiscountOnInvoice`/`apply_discount` (an independently-built invoice-discount-recalculation pathway) in the exact same problem space this feature works in. It had to be retrofitted with CR-04c after the fact. Before starting T-13 (which also touches coupon/discount recalculation), pull latest `develop` and scan for any other new work in `internal/ee/service/invoice*.go` or `internal/ee/service/tax.go` that might overlap or conflict. A repeat of this discovery mid-cluster would be more disruptive than catching it up front.

## A background-task chip is pending, unrelated to blocking this work

`task_f06c12fd` — "Add environment_id filter to InvoiceRepo.RemoveLineItems" — a real but non-exploitable multi-tenancy gap found during T-10's review (upstream calls are already properly scoped, so this specific gap isn't reachable through this feature). Sitting as a dismissible/startable chip; no action needed unless asked about.

## Conventions established so far — keep these consistent for T-12 through T-24

- **Naming:** `is_manually_edited`/`IsManuallyEdited` (matches this codebase's `is_enabled`/`is_default` boolean convention). Helper is `recalculateTotalsFromLineItems` (not `recalculateInvoiceTotals` — that name collides with a pre-existing unrelated method operating on `*dto.InvoiceResponse`).
- **Transaction shape:** every new service method follows `req.Validate()` (if applicable) → `s.DB.WithTx` → `s.InvoiceRepo.GetForUpdate` → draft-status check → do the work → `recalculateTotalsFromLineItems` → set `IsManuallyEdited = true` (line-item mutations only, never coupon/tax ad-hoc ones) → `s.InvoiceRepo.Update` → return `dto.NewInvoiceResponse(lockedInv)` with `.LineItems` attached.
- **No builder pattern** for `Invoice`/`InvoiceLineItem` — deliberate, matches existing plain-struct convention.
- **Test pattern:** `testutil.BaseServiceTestSuite`, in-memory stores — no real Postgres/Docker needed for service-layer tests. Repository-layer tests needing real Postgres use the skip-if-unreachable pattern from `coupon_test.go`/`invoice_test.go` (legitimately SKIP in this sandbox — expected, not a failure).
- **Every new service method gets added to the `InvoiceService` interface** with a short (1-2 line) doc comment — no spec-ID citations, since the spec docs aren't in the PR diffs.
- **Comments: 1-2 lines max, no dangling `CR-XX`/`T-XX` references** in anything that lands in a PR branch — this was retrofitted once across all three open PRs; keep it that way going forward rather than needing another cleanup pass.
- **Only service-layer logic gets tests** — no DTO `Validate()` tests, no domain-model `FromEnt` round-trip tests. This is a standing preference, not just cleanup-after-the-fact; write new tasks' tests this way from the start.
- **Defensive guards beyond the literal task text are OK when justified** by actually reading the delegated repository method's behavior (e.g. T-08/T-10 both found their delegated repo calls silently no-op on non-matching IDs, and added explicit existence checks) — don't assume, verify.
- **No Docker, no live Postgres in this sandbox** — defer any `make migrate-ent-dry-run`-style verification to CI/a dev machine.

## If a subagent hits a connection error

Seen twice in this feature's history already, always on review dispatches. It's a transient infra failure (`API Error: Connection closed mid-response`), not a signal about the code being reviewed. Re-dispatch the same review with the same prompt.
