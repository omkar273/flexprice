---
derived_from_spec: specs/invoice-draft-editing/spec.md
created_at: 2026-08-05
status: executed (PR1-PR3-partial), PR4 not started
---

# Branch Split Plan — Invoice Draft Editing

Retroactively split the single `invoice-draft-editing` branch (24-task plan, T-01 through T-15 in flight) into four smaller, risk-tiered branches so each can be reviewed and tested independently, rather than as one large PR.

## Why risk-tiered, not domain- or dependency-ordered

Considered three groupings: by domain slice (line-items vs. coupons/taxes), by strict build-order dependency, and by risk tier. Chose risk tier because it directly serves the goal — the riskiest changes (edits to existing, billing-critical code) end up in their own small PR, reviewable with maximum scrutiny, separate from the much larger volume of new, isolated, lower-risk code.

## PR boundaries

| PR | Branch name | Tasks | Base | Status |
|---|---|---|---|---|
| PR1 — Foundation | `invoice-draft-editing-foundation` | T-01–T-06 | `develop` (`8bfaf055d`) | ✅ Cut and verified |
| PR2 — Line-item methods | `invoice-draft-editing-line-item-methods` | T-07–T-10 | PR1 | ✅ Cut and verified |
| PR3 — Recompute safety | `invoice-draft-editing-recompute-safety` | T-11–T-15 | PR2 | ⚠️ Cut and verified for what exists (T-11); T-12–T-15 still need to be built directly on this branch |
| PR4 — Ad-hoc methods + API | `invoice-draft-editing-api-surface` | T-16–T-24 | PR3 | ⏳ Not created — branch off PR3 once it's complete |

Sequential stacking (each based on the tip of the previous), matching how the code was actually built and tested together — not parallelized, since PR2 and PR3 both touch `internal/ee/service/invoice.go`.

**All branches are built on `develop`'s current tip (`8bfaf055d`, confirmed identical to `origin/develop`)** — not the stale base this work was originally started from. This matters because `develop` picked up `recalculateDiscountOnInvoice`/`apply_discount` (the pathway CR-04c fixes) while this feature was mid-implementation; building on the current tip means PR1/PR2 don't silently reintroduce that gap, and PR3 carries the actual fix.

## Exact commit-to-PR mapping (as executed)

### PR1 — `invoice-draft-editing-foundation`
Cherry-picked cleanly, in order, onto `develop` (`8bfaf055d`):
1. `6c997eb61` → `0de33b473` — Add spec for draft invoice editing
2. `0674f1af5` → `64310d9b8` — Narrow the manual-edit lock to line items only
3. `51a49ec76` → `69a37f503` — Add implementation plan/tasks/verification docs
4. `a91329596` → `8490a9066` — T-01: `is_manually_edited` schema + domain
5. `e90efa950` → `12e57c099` — T-02/T-04 doc correction
6. `ed05b3c76` → `2fe172e2d` — T-03: `parent_line_item_id` schema + domain
7. `72e991576` → `26bb05b66` — T-05: repository persistence
8. `8da6eae45` → `34781d0a1` — T-06: totals helper
9. `049870ff8` → `17cb8bb56` — Doc fix: helper rename

(Left column = original hash on `invoice-draft-editing`; right column = new hash after cherry-pick onto this branch.)

### PR2 — `invoice-draft-editing-line-item-methods`
Cherry-picked cleanly, in order, onto PR1's tip:
10. `55203865a` → `92ab2c245` — Doc reorder: DTOs move to T-07
11. `b70fa8a78` → `0b976e9b8` — T-07: request DTOs
12. `ea9e37d31` → `4e6613408` — T-08: `UpdateLineItem`
13. `f6e1a48c9` → `41c17359d` — T-08 fix: reject edits on already-archived line items
14. `1766a9f9f` → `14d9fdf30` — T-08 fix: `copyInvoice` `IssueDate` gap
15. `86b6a2770` → `43ee64aa8` — T-09: `AddLineItem`
16. `3b51794b4` → `27165695e` — T-10: `RemoveLineItem`

### PR3 — `invoice-draft-editing-recompute-safety`
Cherry-picked cleanly, in order, onto PR2's tip, **so far**:
17. `e863acb3a` → `8d1e0c312` — T-11: `ComputeInvoice` lock guard
18. `e2ca70f8f` → `8719e16a6` — Doc: add CR-04c (the `recalculateDiscountOnInvoice` gap + new T-15)

Continue the subagent-driven-development loop directly on this branch for T-12, T-13, T-14, T-15 — do not implement them on `invoice-draft-editing` and cherry-pick after; this branch is now the active working branch for the recompute-safety cluster.

**Before this PR is opened:** T-11's code-quality review must complete (it was interrupted twice by transient connection errors, not content issues — see HANDOFF.md). Do not open PR3 with an unreviewed task inside it.

### PR4 — `invoice-draft-editing-api-surface`
Does not exist yet. Branch from PR3's tip once PR3 is complete (T-11–T-15 all done and reviewed), then build T-16–T-24 directly on it.

## Explicitly excluded from all four PRs

- `8ad3d4fa3` (HANDOFF.md) — a session-resumption working document, not reviewable feature content. Stays only on `invoice-draft-editing`.
- The `merge main`/`pull --rebase` mechanics themselves (not real commits to cherry-pick — branch-management noise from working in this same worktree directly, outside this session).

## Verification results (executed, not just planned)

Every cherry-pick sequence above applied with **zero manual conflict resolution** — only automatic (non-conflicting) merges in a handful of shared files (`ent/migrate/schema.go`, `internal/repository/ent/invoice.go`, `internal/api/dto/invoice.go`, `internal/ee/service/invoice.go`, `internal/testutil/inmemory_invoice_store.go`). For each of PR1, PR2, and PR3-so-far, standalone on that branch:
- `go build ./internal/... ./ent/...` — clean.
- `go vet ./internal/...` — clean.
- All of that PR's own tests pass (repository-layer tests requiring live Postgres skip gracefully, same as on the original branch — no Docker in this environment).

Local branch creation and cherry-picking are safe, reversible operations — no push, no PR creation, no force operations occurred. Branches exist locally, ready to be pushed and opened as PRs — that is a separate, explicit step, not yet taken.

## Note on file loss during execution

An earlier draft of this file was written before any branch existed, then lost when cleaning up stale untracked files while setting up the `invoice-draft-editing-foundation` branch (the draft had never been committed, so `rm -rf` on the containing directory removed it along with the actual stale leftovers it was meant to target). Recreated from conversation context with the verification results folded in. No feature code or commits were affected — this only affected an uncommitted planning document.
