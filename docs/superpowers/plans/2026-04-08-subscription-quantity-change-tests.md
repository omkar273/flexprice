# Subscription Quantity-Change Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a comprehensive test suite for the quantity-change sub-feature of `SubscriptionModificationService`, covering proration math, advance vs arrear cadence branches, effective-date validation, preview non-persistence, multi-line-item atomicity, and edge-case inputs.

**Architecture:** All tests are added to the existing `SubscriptionModificationServiceSuite` in `internal/service/subscription_modification_test.go`. New fixture helpers (`createFixedPrice`, `createFixedLineItemWithPrice`, `setSubPeriod`) are added to the same file. No new files are created.

**Tech Stack:** Go 1.23+, testify/suite, shopspring/decimal, in-memory stores from `internal/testutil/`

---

## File Map

| File | Change |
|------|--------|
| `internal/service/subscription_modification_test.go` | Add 4 fixture helpers + 8 test functions |

---

## Task 1: Add fixture helpers

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

These three helpers are needed by all subsequent tasks. Add them in the `// Test helpers` section, after the existing `createFixedLineItem` helper.

- [ ] **Step 1: Add `createFixedPrice` helper**

Open `internal/service/subscription_modification_test.go` and add the following after the closing brace of `createFixedLineItem`:

```go
// createFixedPrice inserts a Price record into PriceRepo and returns it.
// Used by proration tests that need GetPrice to succeed.
func (s *SubscriptionModificationServiceSuite) createFixedPrice(
	amount decimal.Decimal,
	cadence types.InvoiceCadence,
) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		Amount:         amount,
		Currency:       "USD",
		Type:           types.PRICE_TYPE_FIXED,
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}
```

- [ ] **Step 2: Add `createFixedLineItemWithPrice` helper**

Add immediately after `createFixedPrice`:

```go
// createFixedLineItemWithPrice creates a SubscriptionLineItem tied to a specific PriceID.
// Use this instead of createFixedLineItem when proration tests require GetPrice to resolve.
func (s *SubscriptionModificationServiceSuite) createFixedLineItemWithPrice(
	subID, customerID string,
	qty decimal.Decimal,
	cadence types.InvoiceCadence,
	priceID string,
) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		StartDate:      now,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}
```

- [ ] **Step 3: Add `setSubPeriod` helper**

Add immediately after `createFixedLineItemWithPrice`:

```go
// setSubPeriod overrides CurrentPeriodStart and CurrentPeriodEnd on the subscription
// stored in SubRepo. Use in math-regression tests that need a deterministic calendar month.
func (s *SubscriptionModificationServiceSuite) setSubPeriod(subID string, start, end time.Time) {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart = start
	sub.CurrentPeriodEnd = end
	sub.BillingAnchor = start
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
}
```

- [ ] **Step 4: Verify the file compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && go build ./internal/service/...
```

Expected: no output, exit code 0.

- [ ] **Step 5: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add fixture helpers for quantity-change test suite"
```

---

## Task 2: TestExecuteQuantityChange_Advance (table-driven)

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Assert correct invoice creation (upgrade) and wallet credit (downgrade) for ADVANCE line items, plus proration-behavior=none and same-qty no-op.

- [ ] **Step 1: Write the test**

Add this section after the existing `// Quantity change tests` block:

```go
// ─────────────────────────────────────────────
// Advance proration tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_Advance verifies invoice creation for upgrades,
// wallet credit for downgrades, proration-behavior=none, and same-quantity no-ops
// on ADVANCE (in-advance) line items.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_Advance() {
	type tc struct {
		name              string
		oldQty            decimal.Decimal
		newQty            decimal.Decimal
		effectiveDayOffset int // days after periodStart; negative = before start; 0 = at start
		prorationBehavior types.ProrationBehavior
		wantLineItems     int    // expected len(ChangedResources.LineItems)
		wantInvoiceAction string // "created", "wallet_credit", or "" (no invoice)
		wantNoOp          bool   // old line item EndDate must remain zero
	}
	cases := []tc{
		{
			name:              "upgrade_midperiod",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			effectiveDayOffset: 15,
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantLineItems:     2,
			wantInvoiceAction: "created",
		},
		{
			name:              "downgrade_midperiod",
			oldQty:            decimal.NewFromInt(3),
			newQty:            decimal.NewFromInt(1),
			effectiveDayOffset: 15,
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantLineItems:     2,
			wantInvoiceAction: "wallet_credit",
		},
		{
			name:              "upgrade_at_period_start",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			effectiveDayOffset: 0,
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantLineItems:     2,
			wantInvoiceAction: "created",
		},
		{
			name:              "upgrade_near_period_end",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			effectiveDayOffset: -1, // special sentinel: periodEnd - 1 second
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantLineItems:     2,
			wantInvoiceAction: "created",
		},
		{
			name:              "proration_behavior_none",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			effectiveDayOffset: 15,
			prorationBehavior: types.ProrationBehaviorNone,
			wantLineItems:     2,
			wantInvoiceAction: "",
		},
		{
			name:              "same_quantity_noop",
			oldQty:            decimal.NewFromInt(5),
			newQty:            decimal.NewFromInt(5),
			effectiveDayOffset: 5,
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantLineItems:     0,
			wantInvoiceAction: "",
			wantNoOp:          true,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			periodStart := s.GetNow()
			periodEnd := periodStart.AddDate(0, 1, 0)

			var effectiveDate time.Time
			switch tc.effectiveDayOffset {
			case -1:
				effectiveDate = periodEnd.Add(-time.Second)
			default:
				effectiveDate = periodStart.AddDate(0, 0, tc.effectiveDayOffset)
			}

			cust := s.createCustomer("adv-" + tc.name)
			sub := s.createActiveSub(cust.ID)

			// Patch proration behavior when test requires "none"
			if tc.prorationBehavior == types.ProrationBehaviorNone {
				storedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
				s.Require().NoError(err)
				storedSub.ProrationBehavior = types.ProrationBehaviorNone
				s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, storedSub))
			}

			priceAmount := decimal.NewFromInt(50)
			p := s.createFixedPrice(priceAmount, types.InvoiceCadenceAdvance)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceAdvance, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				LineItems: []dto.LineItemQuantityChange{
					{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)

			if tc.wantNoOp {
				// Old line item must be untouched
				orig, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(err)
				s.True(orig.EndDate.IsZero(), "EndDate must remain zero for no-op")
				s.Empty(resp.ChangedResources.Invoices)
				return
			}

			if tc.wantInvoiceAction == "" {
				s.Empty(resp.ChangedResources.Invoices, "expected no invoices for proration_behavior=none")
				return
			}

			s.Require().Len(resp.ChangedResources.Invoices, 1)
			inv := resp.ChangedResources.Invoices[0]
			s.Equal(tc.wantInvoiceAction, inv.Action)
			s.NotEqual("failed", inv.Status)

			if tc.wantInvoiceAction == "created" {
				// Fetch real invoice and verify amount is positive and approximately correct
				realInv, fetchErr := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
				s.Require().NoError(fetchErr)
				s.True(realInv.AmountDue.GreaterThan(decimal.Zero),
					"invoice amount must be positive for upgrade, got %s", realInv.AmountDue.String())

				// Derive expected amount using same second-based formula as the service
				effectivePeriodEnd := periodEnd.Add(-time.Second)
				totalSec := effectivePeriodEnd.Sub(periodStart).Seconds()
				remainingSec := effectivePeriodEnd.Sub(effectiveDate).Seconds()
				if remainingSec < 0 {
					remainingSec = 0
				}
				coeff := decimal.NewFromFloat(remainingSec / totalSec)
				qtyDelta := tc.newQty.Sub(tc.oldQty)
				expectedAmt := qtyDelta.Mul(priceAmount).Mul(coeff)
				tolerance := decimal.NewFromFloat(0.01)
				diff := realInv.AmountDue.Sub(expectedAmt).Abs()
				s.True(diff.LessThanOrEqual(tolerance),
					"invoice amount %s should be ≈ %s (diff=%s)",
					realInv.AmountDue.String(), expectedAmt.String(), diff.String())
			}

			if tc.wantInvoiceAction == "wallet_credit" {
				s.Equal("issued", inv.Status)
				wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
				s.Require().NoError(err)
				s.Require().NotEmpty(wallets, "a PRE_PAID wallet must exist after downgrade credit")
				var totalBalance decimal.Decimal
				for _, w := range wallets {
					totalBalance = totalBalance.Add(w.Balance)
				}
				s.True(totalBalance.GreaterThan(decimal.Zero),
					"wallet balance must be positive after downgrade credit")
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (no implementation missing — this exercises existing code)**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_Advance 2>&1 | tail -30
```

Expected: All sub-tests PASS (the feature is already implemented — these are regression tests).
If any sub-test fails, examine the error: it reveals a bug in the existing implementation or a fixture wiring issue.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add TestExecuteQuantityChange_Advance table-driven tests"
```

---

## Task 3: TestExecuteQuantityChange_Arrear (table-driven)

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Confirm ARREAR line items are versioned but produce no proration invoice or wallet credit.

- [ ] **Step 1: Write the test**

Add the following section after the advance proration tests block:

```go
// ─────────────────────────────────────────────
// Arrear tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_Arrear verifies that ARREAR line items are versioned
// (old item ended, new item created) but no proration invoice or wallet credit is issued.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_Arrear() {
	type tc struct {
		name          string
		oldQty        decimal.Decimal
		newQty        decimal.Decimal
		wantLineItems int
		wantNoOp      bool
	}
	cases := []tc{
		{name: "increase_arrear", oldQty: decimal.NewFromInt(1), newQty: decimal.NewFromInt(5), wantLineItems: 2},
		{name: "decrease_arrear", oldQty: decimal.NewFromInt(5), newQty: decimal.NewFromInt(1), wantLineItems: 2},
		{name: "same_qty_arrear", oldQty: decimal.NewFromInt(3), newQty: decimal.NewFromInt(3), wantLineItems: 0, wantNoOp: true},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			effectiveDate := s.GetNow().AddDate(0, 0, 5)

			cust := s.createCustomer("arr-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			p := s.createFixedPrice(decimal.NewFromInt(30), types.InvoiceCadenceArrear)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceArrear, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				LineItems: []dto.LineItemQuantityChange{
					{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)
			s.Empty(resp.ChangedResources.Invoices, "ARREAR items must never generate a proration invoice")

			if !tc.wantNoOp {
				// Old line item must have EndDate set
				old, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(err)
				s.False(old.EndDate.IsZero(), "old line item EndDate should be set after versioning")

				// Verify the new line item exists with updated quantity
				var newLIID string
				for _, cli := range resp.ChangedResources.LineItems {
					if cli.ChangeAction == "created" {
						newLIID = cli.ID
					}
				}
				s.Require().NotEmpty(newLIID, "a 'created' line item entry must exist")
				newLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, newLIID)
				s.Require().NoError(err)
				s.True(tc.newQty.Equal(newLI.Quantity), "new line item quantity mismatch")
			}

			// No wallet balance should exist (no credit issued)
			wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
			s.Require().NoError(err)
			var totalBalance decimal.Decimal
			for _, w := range wallets {
				totalBalance = totalBalance.Add(w.Balance)
			}
			s.True(totalBalance.IsZero(), "no wallet credit should be issued for ARREAR items")
		})
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_Arrear 2>&1 | tail -20
```

Expected: PASS for all 3 sub-tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add TestExecuteQuantityChange_Arrear table-driven tests"
```

---

## Task 4: TestExecuteQuantityChange_EffectiveDateValidation (table-driven)

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Assert exact boundary conditions on `EffectiveDate` — reject out-of-bounds, accept in-bounds.

- [ ] **Step 1: Write the test**

Add this section after the arrear tests block:

```go
// ─────────────────────────────────────────────
// Effective-date validation tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_EffectiveDateValidation tests all boundary conditions
// for the effective_date parameter.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_EffectiveDateValidation() {
	type tc struct {
		name          string
		buildDate     func(start, end time.Time) time.Time
		wantError     bool
	}
	cases := []tc{
		{
			name:      "before_period_start",
			buildDate: func(start, end time.Time) time.Time { return start.Add(-time.Nanosecond) },
			wantError: true,
		},
		{
			name:      "at_period_start",
			buildDate: func(start, end time.Time) time.Time { return start },
			wantError: false,
		},
		{
			name:      "at_period_end",
			buildDate: func(start, end time.Time) time.Time { return end },
			wantError: true,
		},
		{
			name:      "one_ns_before_end",
			buildDate: func(start, end time.Time) time.Time { return end.Add(-time.Nanosecond) },
			wantError: false,
		},
		{
			name:      "future_within_period",
			buildDate: func(start, end time.Time) time.Time { return start.AddDate(0, 0, 10) },
			wantError: false,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			periodStart := s.GetNow()
			periodEnd := periodStart.AddDate(0, 1, 0)

			effectiveDate := tc.buildDate(periodStart, periodEnd)

			cust := s.createCustomer("effdt-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			// Use a simple ARREAR item — cadence doesn't affect date validation
			li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceArrear)

			newQty := decimal.NewFromInt(4)
			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				LineItems: []dto.LineItemQuantityChange{
					{ID: li.ID, Quantity: newQty, EffectiveDate: &effectiveDate},
				},
			}
			_, err := s.service.Execute(ctx, sub.ID, req)
			if tc.wantError {
				s.Require().Error(err, "expected validation error for %s", tc.name)
			} else {
				s.Require().NoError(err, "expected no error for %s", tc.name)
				// Verify versioning occurred for success cases
				old, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(getErr)
				s.False(old.EndDate.IsZero(), "old line item must be ended after valid quantity change")
			}
		})
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_EffectiveDateValidation 2>&1 | tail -25
```

Expected: PASS for all 5 sub-tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add TestExecuteQuantityChange_EffectiveDateValidation boundary tests"
```

---

## Task 5: TestExecuteQuantityChange_MultiLineItem

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Verify mixed-cadence batches and transactional rollback on partial failure.

- [ ] **Step 1: Write the test**

Add this section after the effective-date validation block:

```go
// ─────────────────────────────────────────────
// Multi-line-item tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_MultiLineItem_MixedCadence verifies that in a single Execute
// call with one ADVANCE and one ARREAR line item, the ADVANCE item generates a proration
// invoice while the ARREAR item does not.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_MultiLineItem_MixedCadence() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 10)

	cust := s.createCustomer("multi-mixed-001")
	sub := s.createActiveSub(cust.ID)

	advPrice := s.createFixedPrice(decimal.NewFromInt(50), types.InvoiceCadenceAdvance)
	arrPrice := s.createFixedPrice(decimal.NewFromInt(30), types.InvoiceCadenceArrear)

	advLI := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, advPrice.ID)
	arrLI := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceArrear, arrPrice.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: advLI.ID, Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
			{ID: arrLI.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
		},
	}
	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// 4 changed line items: 2 ended + 2 created
	s.Len(resp.ChangedResources.LineItems, 4)

	// Exactly 1 invoice (for the ADVANCE item)
	s.Require().Len(resp.ChangedResources.Invoices, 1)
	s.Equal("created", resp.ChangedResources.Invoices[0].Action)
	s.NotEqual("failed", resp.ChangedResources.Invoices[0].Status)
}

// TestExecuteQuantityChange_MultiLineItem_AtomicRollback verifies that if the second
// line item ID in a batch is invalid, the entire transaction rolls back and no changes
// are persisted.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_MultiLineItem_AtomicRollback() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("multi-rollback-001")
	sub := s.createActiveSub(cust.ID)

	p := s.createFixedPrice(decimal.NewFromInt(50), types.InvoiceCadenceAdvance)
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceAdvance, p.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
			{ID: "nonexistent-id-xyz", Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err, "should fail when second line item ID is invalid")

	// First line item must be untouched (transaction rolled back)
	orig, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(getErr)
	s.True(orig.EndDate.IsZero(), "first line item EndDate must remain zero after rollback")

	// No invoices created
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(listErr)
	s.Empty(invoices, "no invoices should exist after rollback")
}
```

- [ ] **Step 2: Run the tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run "TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_MultiLineItem" 2>&1 | tail -25
```

Expected: PASS for both sub-tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add multi-line-item mixed-cadence and rollback tests"
```

---

## Task 6: TestPreviewQuantityChange (table-driven)

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Confirm `Preview` returns correct sentinel IDs and writes nothing to any store.

- [ ] **Step 1: Write the test**

Add after the multi-line-item block:

```go
// ─────────────────────────────────────────────
// Preview tests
// ─────────────────────────────────────────────

// TestPreviewQuantityChange verifies that Preview returns the correct placeholder IDs
// and status values, and that no persistent store is mutated.
func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange() {
	type tc struct {
		name            string
		oldQty          decimal.Decimal
		newQty          decimal.Decimal
		cadence         types.InvoiceCadence
		wantLineItems   int
		wantInvoiceID   string // "" means no invoice expected
		wantInvoiceStatus string
	}
	cases := []tc{
		{
			name:              "upgrade_advance",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			cadence:           types.InvoiceCadenceAdvance,
			wantLineItems:     2,
			wantInvoiceID:     "(preview-invoice)",
			wantInvoiceStatus: "preview",
		},
		{
			name:              "downgrade_advance",
			oldQty:            decimal.NewFromInt(3),
			newQty:            decimal.NewFromInt(1),
			cadence:           types.InvoiceCadenceAdvance,
			wantLineItems:     2,
			wantInvoiceID:     "(preview-wallet-credit)",
			wantInvoiceStatus: "preview",
		},
		{
			name:          "same_qty",
			oldQty:        decimal.NewFromInt(5),
			newQty:        decimal.NewFromInt(5),
			cadence:       types.InvoiceCadenceAdvance,
			wantLineItems: 0,
			wantInvoiceID: "",
		},
		{
			name:          "arrear_increase",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(5),
			cadence:       types.InvoiceCadenceArrear,
			wantLineItems: 2,
			wantInvoiceID: "",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			effectiveDate := s.GetNow().AddDate(0, 0, 10)

			cust := s.createCustomer("prev-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			p := s.createFixedPrice(decimal.NewFromInt(50), tc.cadence)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, tc.cadence, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				LineItems: []dto.LineItemQuantityChange{
					{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
				},
			}
			resp, err := s.service.Preview(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			// Line item shape
			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)

			// The original line item must be untouched in the store
			orig, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
			s.Require().NoError(getErr)
			s.True(orig.EndDate.IsZero(),
				"Preview must not mutate the line item EndDate (tc=%s)", tc.name)

			// No real invoices created in InvoiceRepo
			filter := types.NewNoLimitInvoiceFilter()
			filter.SubscriptionID = sub.ID
			invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, filter)
			s.Require().NoError(listErr)
			s.Empty(invoices, "Preview must not persist any invoice (tc=%s)", tc.name)

			// No wallet balance created
			wallets, _ := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
			var totalBal decimal.Decimal
			for _, w := range wallets {
				totalBal = totalBal.Add(w.Balance)
			}
			s.True(totalBal.IsZero(), "Preview must not create wallet credits (tc=%s)", tc.name)

			// Invoice sentinel IDs
			if tc.wantInvoiceID == "" {
				s.Empty(resp.ChangedResources.Invoices,
					"expected no invoice entry in response (tc=%s)", tc.name)
			} else {
				s.Require().Len(resp.ChangedResources.Invoices, 1)
				s.Equal(tc.wantInvoiceID, resp.ChangedResources.Invoices[0].ID)
				s.Equal(tc.wantInvoiceStatus, resp.ChangedResources.Invoices[0].Status)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite/TestPreviewQuantityChange 2>&1 | tail -25
```

Expected: PASS for all 4 sub-tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add TestPreviewQuantityChange non-persistence tests"
```

---

## Task 7: TestProrationMath_Upgrade (deterministic math regression)

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Pin the proration dollar amounts to known values using a fixed 31-day January period. If the calculation formula ever changes, this test breaks intentionally.

- [ ] **Step 1: Write the test**

Add after the preview tests block:

```go
// ─────────────────────────────────────────────
// Proration math regression tests
// ─────────────────────────────────────────────

// TestProrationMath_Upgrade pins the upgrade proration amount to a deterministic
// value using a fixed 31-day billing period (January 2026).
// Formula: deltaAmount = (newQty - oldQty) × pricePerUnit × (remainingSec / totalSec)
// where totalSec = (periodEnd - 1s) - periodStart  and  remainingSec = (periodEnd - 1s) - effectiveDate
func (s *SubscriptionModificationServiceSuite) TestProrationMath_Upgrade() {
	// Fix period to January 2026 for determinism
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	// effectivePeriodEnd is what ProrationParams receives (sub.CurrentPeriodEnd.Add(-time.Second))
	effectivePeriodEnd := periodEnd.Add(-time.Second)

	type tc struct {
		name          string
		oldQty        decimal.Decimal
		newQty        decimal.Decimal
		pricePerUnit  decimal.Decimal
		effectiveDate time.Time
	}
	cases := []tc{
		{
			name:          "15_days_remaining",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(3),
			pricePerUnit:  decimal.NewFromInt(50),
			effectiveDate: time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "1_day_remaining",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(2),
			pricePerUnit:  decimal.NewFromInt(100),
			effectiveDate: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "full_period",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(3),
			pricePerUnit:  decimal.NewFromInt(50),
			effectiveDate: periodStart,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()

			// Compute expected amount using the same second-based formula
			totalSec := decimal.NewFromFloat(effectivePeriodEnd.Sub(periodStart).Seconds())
			remainingSec := decimal.NewFromFloat(effectivePeriodEnd.Sub(tc.effectiveDate).Seconds())
			if remainingSec.LessThan(decimal.Zero) {
				remainingSec = decimal.Zero
			}
			coeff := remainingSec.Div(totalSec)
			qtyDelta := tc.newQty.Sub(tc.oldQty)
			expectedAmt := qtyDelta.Mul(tc.pricePerUnit).Mul(coeff)

			cust := s.createCustomer("math-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			s.setSubPeriod(sub.ID, periodStart, periodEnd)

			p := s.createFixedPrice(tc.pricePerUnit, types.InvoiceCadenceAdvance)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceAdvance, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				LineItems: []dto.LineItemQuantityChange{
					{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &tc.effectiveDate},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)
			s.Require().Len(resp.ChangedResources.Invoices, 1)

			inv := resp.ChangedResources.Invoices[0]
			s.Equal("created", inv.Action)
			s.NotEqual("failed", inv.Status)

			realInv, fetchErr := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
			s.Require().NoError(fetchErr)

			tolerance := decimal.NewFromFloat(0.01) // 1 cent tolerance
			diff := realInv.AmountDue.Sub(expectedAmt).Abs()
			s.True(diff.LessThanOrEqual(tolerance),
				"invoice amount %s should be ≈ %s (diff=%s, tc=%s)",
				realInv.AmountDue.String(), expectedAmt.String(), diff.String(), tc.name)
		})
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite/TestProrationMath_Upgrade 2>&1 | tail -25
```

Expected: PASS for all 3 sub-tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add TestProrationMath_Upgrade deterministic regression tests"
```

---

## Task 8: TestExecuteQuantityChange_NonFixedPriceRejected and TestExecuteQuantityChange_InactiveLineItemRejected

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

**Purpose:** Validate the two remaining guard conditions: non-fixed price type and non-published line item status.

- [ ] **Step 1: Write the tests**

Add after the proration math block:

```go
// ─────────────────────────────────────────────
// Guard condition tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_NonFixedPriceRejected verifies that attempting to change
// the quantity of a USAGE-type line item returns a validation error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_NonFixedPriceRejected() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("guard-usage-001")
	sub := s.createActiveSub(cust.ID)

	// Create a line item with USAGE price type (not FIXED)
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: sub.ID,
		CustomerID:     cust.ID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_USAGE,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.GetNow(),
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "not a fixed-price item")
}

// TestExecuteQuantityChange_InactiveLineItemRejected verifies that attempting to change
// the quantity of a non-published (archived) line item returns a validation error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_InactiveLineItemRejected() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("guard-inactive-001")
	sub := s.createActiveSub(cust.ID)

	// Create a line item with archived status (simulates an already-ended item)
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: sub.ID,
		CustomerID:     cust.ID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(2),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.GetNow(),
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// Mark it as archived (non-published)
	li.Status = types.StatusArchived
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "not active")
}
```

- [ ] **Step 2: Run the tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run "TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_NonFixed|TestSubscriptionModificationServiceSuite/TestExecuteQuantityChange_Inactive" 2>&1 | tail -20
```

Expected: PASS for both tests.
If `TestExecuteQuantityChange_InactiveLineItemRejected` fails because the in-memory store doesn't support `Update` for `Status`, check `InMemorySubscriptionLineItemStore.Update` — it must store the full struct including `Status`. If it copies only specific fields, the test approach should instead set `li.Status = types.StatusArchived` at creation time before calling `Create`.

- [ ] **Step 3: Commit**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): add guard-condition tests for non-fixed price and inactive line items"
```

---

## Task 9: Full suite run and final commit

**Files:**
- No file changes — this is a verification + optional cleanup task.

- [ ] **Step 1: Run the entire subscription modification test suite**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
go test -v -race ./internal/service/... -run TestSubscriptionModificationServiceSuite 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|FAIL|ok)" | head -60
```

Expected: all lines are `--- PASS` or `ok`. No `--- FAIL` or `FAIL` lines.

- [ ] **Step 2: Build check**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
make build 2>&1 | tail -5
```

Expected: successful build, no errors.

- [ ] **Step 3: Final commit if any fixups were made**

If the previous steps required any fixups (e.g., import additions, method signature corrections):

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/frosty-newton && \
git add internal/service/subscription_modification_test.go && \
git commit -m "test(subscription): fix compilation issues in quantity-change test suite"
```

---

## Self-Review

### Spec coverage check

| Spec section | Covered by task |
|---|---|
| 3.1 TestExecuteQuantityChange_Advance (6 cases) | Task 2 |
| 3.2 TestExecuteQuantityChange_Arrear (3 cases) | Task 3 |
| 3.3 TestExecuteQuantityChange_EffectiveDateValidation (5 cases) | Task 4 |
| 3.4 TestExecuteQuantityChange_MultiLineItem (sub-tests A & B) | Task 5 |
| 3.5 TestPreviewQuantityChange (4 cases) | Task 6 |
| 3.6 TestProrationMath_Upgrade (3 cases) | Task 7 |
| 3.7 TestExecuteQuantityChange_EndedLineItemExcluded | Task 8 (InactiveLineItemRejected) |
| 3.8 TestExecuteQuantityChange_NonFixedPriceRejected | Task 8 |
| Fixture helpers (§2.3, §2.4) | Task 1 |

### Placeholder scan

No TBD, TODO, or "similar to" references. All code blocks are complete. All method names are consistent across tasks.

### Type consistency

- `createFixedPrice` returns `*price.Price` — referenced correctly in all tasks
- `createFixedLineItemWithPrice` returns `*subscription.SubscriptionLineItem` — used as `li` in all tasks
- `setSubPeriod(subID string, start, end time.Time)` — called correctly in Task 7
- `types.InvoiceCadenceAdvance` / `types.InvoiceCadenceArrear` — string constants, used consistently
- `types.ProrationBehaviorNone` / `types.ProrationBehaviorCreateProrations` — used in Task 2
- `types.PRICE_TYPE_FIXED` / `types.PRICE_TYPE_USAGE` — used in Task 8
- `types.StatusArchived` — used in Task 8
- `types.NewNoLimitInvoiceFilter()` — confirmed present in types package
- `WalletRepo.GetWalletsByCustomerID` — confirmed in wallet.Repository interface
- `InvoiceRepo.List(ctx, filter)` — confirmed in invoice.Repository interface
