# Backdated Credit Grant Catch-Up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** For backdated subscriptions, apply all past-due recurring credit grant periods immediately rather than waiting N × 15-minute cron cycles.

**Architecture:** Add `processCatchUpApplications` that wraps `processScheduledApplication` in a bounded loop (max 100 iterations). Thread the newly-created next-period CGA back through a signature chain (`createNextPeriodApplication` → `skipCreditGrantApplication` / `applyCreditGrantToWallet` → `processScheduledApplication`) so the loop has zero extra DB queries. Update all three public call sites to use the new method. Mirror changes in the EE service.

**Tech Stack:** Go 1.23, Ent ORM, PostgreSQL, `github.com/samber/lo`, `github.com/shopspring/decimal`, `testify/suite`

---

## File Map

| File | Change |
|---|---|
| `internal/service/creditgrant.go` | Signature chain refactor + new `processCatchUpApplications` + update 3 callers |
| `internal/service/creditgrant_test.go` | New catch-up test cases |
| `internal/ee/service/credit_grants.go` | Mirror same signature chain + `processCatchUpApplications` + update 1 caller |

---

### Task 1: Signature chain refactor in `internal/service/creditgrant.go`

All four internal methods change return type in the same task so the file compiles after a single step.

**Files:**
- Modify: `internal/service/creditgrant.go:884-938` (`createNextPeriodApplication`)
- Modify: `internal/service/creditgrant.go:1005-1040` (`skipCreditGrantApplication`)
- Modify: `internal/service/creditgrant.go:469-707` (`applyCreditGrantToWallet`)
- Modify: `internal/service/creditgrant.go:791-882` (`processScheduledApplication`)
- Modify: `internal/service/creditgrant.go:283-288` (caller in `InitializeCreditGrantWorkflow`)
- Modify: `internal/service/creditgrant.go:455-461` (caller `ProcessCreditGrantApplication`)
- Modify: `internal/service/creditgrant.go:764-784` (caller `ProcessScheduledCreditGrantApplications`)

- [ ] **Step 1.1: Update `createNextPeriodApplication` — signature and return values**

Replace the function signature and all return statements (lines 884–938):

```go
// BEFORE signature:
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) error {

// AFTER signature:
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

Return value changes inside the function body:
```go
// BEFORE (error path after CalculateNextCreditGrantPeriod):
return err

// AFTER:
return nil, err

// BEFORE (grant end-date guard — early return nil):
return nil

// AFTER:
return nil, nil

// BEFORE (Validate error):
if err := nextPeriodCGAAReq.Validate(); err != nil {
    return err
}

// AFTER:
if err := nextPeriodCGAAReq.Validate(); err != nil {
    return nil, err
}

// BEFORE (Create error):
err = s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)
if err != nil {
    s.Logger.ErrorwCtx(ctx, "Failed to create next period CGA",
        "next_period_start", nextPeriodStart,
        "next_period_end", nextPeriodEnd,
        "error", err)
    return err
}

// AFTER:
err = s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)
if err != nil {
    s.Logger.ErrorwCtx(ctx, "Failed to create next period CGA",
        "next_period_start", nextPeriodStart,
        "next_period_end", nextPeriodEnd,
        "error", err)
    return nil, err
}

// BEFORE (success return at end of function):
return nil

// AFTER:
return nextPeriodCGA, nil
```

- [ ] **Step 1.2: Update `skipCreditGrantApplication` — signature and thread CGA**

Replace the entire function (lines 1005–1040):

```go
func (s *creditGrantService) skipCreditGrantApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
	grant *creditgrant.CreditGrant,
	subscription *subscription.Subscription,
) (*domainCreditGrantApplication.CreditGrantApplication, error) {
	s.Logger.Infow("Skipping credit grant application",
		"application_id", cga.ID,
		"grant_id", cga.CreditGrantID,
		"subscription_id", cga.SubscriptionID,
		"subscription_status", cga.SubscriptionStatusAtApplication,
		"reason", cga.FailureReason)

	cga.ApplicationStatus = types.ApplicationStatusSkipped
	cga.AppliedAt = nil

	err := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to update CGA status to skipped", "application_id", cga.ID, "error", err)
		return nil, err
	}

	if grant.Cadence == types.CreditGrantCadenceRecurring {
		nextCGA, err := s.createNextPeriodApplication(ctx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
		if err != nil {
			s.Logger.Errorw("Failed to create next period application", "application_id", cga.ID, "error", err)
			return nil, err
		}
		return nextCGA, nil
	}

	return nil, nil
}
```

- [ ] **Step 1.3: Update `applyCreditGrantToWallet` — signature, closure capture, thread CGA**

Change the function signature:
```go
// BEFORE:
func (s *creditGrantService) applyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) error {

// AFTER:
func (s *creditGrantService) applyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

All early-exit `handleCreditGrantFailure` returns before the `WithTx` block (lines ~475, ~534) become:
```go
// BEFORE:
return s.handleCreditGrantFailure(ctx, cga, err, "Failed to get wallet for top up")
// AFTER:
return nil, s.handleCreditGrantFailure(ctx, cga, err, "Failed to get wallet for top up")

// BEFORE:
return s.handleCreditGrantFailure(ctx, cga, err, "Failed to create wallet for top up")
// AFTER:
return nil, s.handleCreditGrantFailure(ctx, cga, err, "Failed to create wallet for top up")
```

The validation error return inside expiry-duration handling (~line 573):
```go
// BEFORE:
return ierr.NewError("invalid expiration duration unit").
    WithHint("Please provide a valid expiration duration unit").
    WithReportableDetails(map[string]interface{}{
        "expiration_duration_unit": grant.ExpirationDurationUnit,
    }).
    Mark(ierr.ErrValidation)

// AFTER:
return nil, ierr.NewError("invalid expiration duration unit").
    WithHint("Please provide a valid expiration duration unit").
    WithReportableDetails(map[string]interface{}{
        "expiration_duration_unit": grant.ExpirationDurationUnit,
    }).
    Mark(ierr.ErrValidation)
```

Add a closure capture variable immediately before `err = s.DB.WithTx(...)` (~line 639):
```go
var nextCGA *domainCreditGrantApplication.CreditGrantApplication

err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
```

Inside the tx closure, replace both `createNextPeriodApplication` calls:

```go
// BEFORE (expiry-skip path, ~line 658):
if grant.Cadence == types.CreditGrantCadenceRecurring {
    if err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd)); err != nil {
        return err
    }
}

// AFTER:
if grant.Cadence == types.CreditGrantCadenceRecurring {
    created, err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
    if err != nil {
        return err
    }
    nextCGA = created
}

// BEFORE (normal apply path, ~line 682):
if grant.Cadence == types.CreditGrantCadenceRecurring {
    if err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd)); err != nil {
        return err
    }
}

// AFTER:
if grant.Cadence == types.CreditGrantCadenceRecurring {
    created, err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
    if err != nil {
        return err
    }
    nextCGA = created
}
```

After the `WithTx` block:
```go
// BEFORE:
if err != nil {
    return s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
}
// ... success log ...
return nil

// AFTER:
if err != nil {
    return nil, s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
}
// ... success log unchanged ...
return nextCGA, nil
```

- [ ] **Step 1.4: Update `processScheduledApplication` — signature and thread returns through switch**

Change the function signature:
```go
// BEFORE:
func (s *creditGrantService) processScheduledApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
) error {

// AFTER:
func (s *creditGrantService) processScheduledApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

Replace the `switch action` block and final return (lines 842–882):

```go
switch action {
case StateActionApply:
	nextCGA, err := s.applyCreditGrantToWallet(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
	if err != nil {
		s.Logger.Errorw("Failed to apply credit grant transaction", "application_id", cga.ID, "error", err)
		return nil, err
	}
	return nextCGA, nil

case StateActionSkip:
	nextCGA, err := s.skipCreditGrantApplication(ctx, cga, creditGrant.CreditGrant, subscription.Subscription)
	if err != nil {
		s.Logger.Errorw("Failed to skip credit grant application", "application_id", cga.ID, "error", err)
		return nil, err
	}
	return nextCGA, nil

case StateActionDefer:
	err := s.deferCreditGrantApplication(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to defer credit grant application", "application_id", cga.ID, "error", err)
		return nil, err
	}
	return nil, nil

case StateActionCancel:
	if err := s.cancelCreditGrantApplication(ctx, cga); err != nil {
		return nil, err
	}
	err := s.cancelFutureGrantApplications(ctx, creditGrant.CreditGrant)
	if err != nil {
		s.Logger.Errorw("Failed to cancel future credit grant applications", "application_id", cga.ID, "error", err)
		return nil, err
	}
	return nil, nil
}

return nil, nil
```

Also update the error check before the switch (line ~838):
```go
// BEFORE:
if err != nil {
    s.Logger.Errorw("Failed to determine action", "application_id", cga.ID, "error", err)
    return err
}

// AFTER:
if err != nil {
    s.Logger.Errorw("Failed to determine action", "application_id", cga.ID, "error", err)
    return nil, err
}
```

- [ ] **Step 1.5: Update three callers to compile (temporarily discard `*CGA`)**

`ProcessCreditGrantApplication` (line 455–461):
```go
// BEFORE:
func (s *creditGrantService) ProcessCreditGrantApplication(ctx context.Context, applicationID string) error {
	cga, err := s.CreditGrantApplicationRepo.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	return s.processScheduledApplication(ctx, cga)
}

// AFTER (temporary — will be replaced in Task 3):
func (s *creditGrantService) ProcessCreditGrantApplication(ctx context.Context, applicationID string) error {
	cga, err := s.CreditGrantApplicationRepo.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	_, err = s.processScheduledApplication(ctx, cga)
	return err
}
```

`ProcessScheduledCreditGrantApplications` cron loop (line ~769):
```go
// BEFORE:
err := s.processScheduledApplication(ctxWithEnv, cga)

// AFTER (temporary):
_, err := s.processScheduledApplication(ctxWithEnv, cga)
```

`InitializeCreditGrantWorkflow` eager section (line ~286):
```go
// BEFORE:
if err := s.processScheduledApplication(ctx, cga); err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
}

// AFTER (temporary):
if _, err := s.processScheduledApplication(ctx, cga); err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
}
```

- [ ] **Step 1.6: Verify compilation**

```bash
make build
```

Expected: successful build with no errors.

- [ ] **Step 1.7: Run existing credit grant tests — expect all pass**

```bash
go test -v -race ./internal/service -run TestCreditGrantService
```

Expected: all existing tests PASS (behaviour unchanged).

- [ ] **Step 1.8: Commit**

```bash
git add internal/service/creditgrant.go
git commit -m "refactor(creditgrant): thread next-period CGA through signature chain"
```

---

### Task 2: Write failing tests for `processCatchUpApplications`

**Files:**
- Modify: `internal/service/creditgrant_test.go`

- [ ] **Step 2.1: Add test helpers and test cases to `creditgrant_test.go`**

Add a helper that creates a backdated subscription and a helper that counts CGA statuses, then add the three test methods. Append after the last existing test in the file:

```go
// createBackdatedSubscription creates a subscription with start date in the past.
func (s *CreditGrantServiceTestSuite) createBackdatedSubscription(id string, startDate time.Time) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		Currency:           "USD",
		StartDate:          startDate,
		CurrentPeriodStart: startDate,
		CurrentPeriodEnd:   startDate.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BillingAnchor:      startDate,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
	return sub
}

// countCGAStatuses returns (applied, pending, other) counts for a grant.
func (s *CreditGrantServiceTestSuite) countCGAStatuses(grantID string) (applied, pending int) {
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs: []string{grantID},
		QueryFilter:    types.NewNoLimitQueryFilter(),
	}
	apps, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	for _, a := range apps {
		switch a.ApplicationStatus {
		case types.ApplicationStatusApplied:
			applied++
		case types.ApplicationStatusPending:
			pending++
		}
	}
	return
}

// TestCatchUp_BackdatedOneCycle verifies a grant backdated by 1 cycle applies immediately.
// backDate = now - 1 day → period 1 start is yesterday → applied; period 2 start = now+29d → pending.
func (s *CreditGrantServiceTestSuite) TestCatchUp_BackdatedOneCycle() {
	ctx := s.GetContext()
	backDate := s.testData.now.AddDate(0, 0, -1)

	sub := s.createBackdatedSubscription("sub_catchup_1cycle", backDate)

	resp, err := s.creditGrantService.CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
		Name:           "Catch-up 1 Cycle",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		StartDate:      &backDate,
		SubscriptionID: &sub.ID,
	})
	s.NoError(err)

	applied, pending := s.countCGAStatuses(resp.CreditGrant.ID)
	s.Equal(1, applied, "expected 1 applied CGA for past cycle")
	s.Equal(1, pending, "expected 1 pending CGA for upcoming cycle")
}

// TestCatchUp_BackdatedThreeCycles verifies all 3 past cycles are applied immediately.
// backDate = now - 2 months - 15 days:
//   period 1 start: now-2m-15d < now → applied
//   period 2 start: now-1m-15d < now → applied
//   period 3 start: now-15d    < now → applied
//   period 4 start: now+15d    > now → pending
func (s *CreditGrantServiceTestSuite) TestCatchUp_BackdatedThreeCycles() {
	ctx := s.GetContext()
	backDate := s.testData.now.AddDate(0, -2, -15)

	sub := s.createBackdatedSubscription("sub_catchup_3cycles", backDate)

	resp, err := s.creditGrantService.CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
		Name:           "Catch-up 3 Cycles",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(50),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		StartDate:      &backDate,
		SubscriptionID: &sub.ID,
	})
	s.NoError(err)

	applied, pending := s.countCGAStatuses(resp.CreditGrant.ID)
	s.Equal(3, applied, "expected 3 applied CGAs for 3 past cycles")
	s.Equal(1, pending, "expected 1 pending CGA for upcoming cycle")
}

// TestCatchUp_FutureStartDate verifies a future-dated grant does not trigger catch-up.
func (s *CreditGrantServiceTestSuite) TestCatchUp_FutureStartDate() {
	ctx := s.GetContext()
	futureDate := s.testData.now.AddDate(0, 1, 0)

	// Subscription must start before or at grant start; use now for simplicity.
	sub := s.createBackdatedSubscription("sub_catchup_future", s.testData.now.AddDate(0, 0, -1))

	resp, err := s.creditGrantService.CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
		Name:           "Future Grant",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		StartDate:      &futureDate,
		SubscriptionID: &sub.ID,
	})
	s.NoError(err)

	applied, pending := s.countCGAStatuses(resp.CreditGrant.ID)
	s.Equal(0, applied, "expected no applied CGAs for future grant")
	s.Equal(1, pending, "expected 1 pending CGA waiting for future schedule")
}
```

- [ ] **Step 2.2: Run the new tests — expect FAIL**

```bash
go test -v -race ./internal/service -run "TestCreditGrantService/TestCatchUp"
```

Expected: tests FAIL — `TestCatchUp_BackdatedOneCycle` and `TestCatchUp_BackdatedThreeCycles` will show 1 applied and 1 pending (current eager-apply behaviour), not the expected values. `TestCatchUp_FutureStartDate` should already pass.

---

### Task 3: Implement `processCatchUpApplications` and update callers

**Files:**
- Modify: `internal/service/creditgrant.go`

- [ ] **Step 3.1: Add `processCatchUpApplications` after `processScheduledApplication`**

Insert this new method immediately after `processScheduledApplication` ends (after line 882):

```go
const maxCatchUpIterations = 100

// processCatchUpApplications processes a CGA and immediately applies any subsequent
// past-due CGAs for the same grant. Stops when the next period is in the future,
// the action is Defer/Cancel, or maxCatchUpIterations is reached.
// Always returns nil — failures are logged and left for cron retry (backward compat).
func (s *creditGrantService) processCatchUpApplications(
	ctx context.Context,
	initialCGA *domainCreditGrantApplication.CreditGrantApplication,
) error {
	currentCGA := initialCGA
	startTime := currentCGA.ScheduledFor

	for i := 0; i < maxCatchUpIterations; i++ {
		nextCGA, err := s.processScheduledApplication(ctx, currentCGA)
		if err != nil {
			// CGA already marked Failed in DB by handleCreditGrantFailure.
			// Cron will retry on next tick.
			s.Logger.WarnwCtx(ctx, "catch-up loop stopping due to failure",
				"cga_id", currentCGA.ID,
				"grant_id", currentCGA.CreditGrantID,
				"subscription_id", currentCGA.SubscriptionID,
				"iterations_completed", i,
				"error", err)
			return nil
		}

		// nil nextCGA: one-time grant, Defer, Cancel, or grant end date reached
		if nextCGA == nil {
			break
		}

		// Next period is future-dated — normal cron territory
		if nextCGA.ScheduledFor.After(time.Now().UTC()) {
			break
		}

		currentCGA = nextCGA
	}

	s.Logger.InfowCtx(ctx, "credit grant catch-up loop completed",
		"grant_id", initialCGA.CreditGrantID,
		"subscription_id", initialCGA.SubscriptionID,
		"catch_up_from", startTime,
		"catch_up_to", currentCGA.ScheduledFor)

	return nil
}
```

- [ ] **Step 3.2: Update three callers to use `processCatchUpApplications`**

`ProcessCreditGrantApplication` (line 455–461):
```go
func (s *creditGrantService) ProcessCreditGrantApplication(ctx context.Context, applicationID string) error {
	cga, err := s.CreditGrantApplicationRepo.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	return s.processCatchUpApplications(ctx, cga)
}
```

`ProcessScheduledCreditGrantApplications` cron loop (line ~769):
```go
// BEFORE:
_, err := s.processScheduledApplication(ctxWithEnv, cga)

// AFTER:
err := s.processCatchUpApplications(ctxWithEnv, cga)
```

`InitializeCreditGrantWorkflow` eager section (line ~283):
```go
// BEFORE:
if _, err := s.processScheduledApplication(ctx, cga); err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
}

// AFTER:
if err := s.processCatchUpApplications(ctx, cga); err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
}
```

- [ ] **Step 3.3: Run new catch-up tests — expect PASS**

```bash
go test -v -race ./internal/service -run "TestCreditGrantService/TestCatchUp"
```

Expected: all 3 catch-up tests PASS.

- [ ] **Step 3.4: Run full credit grant test suite — expect no regressions**

```bash
go test -v -race ./internal/service -run TestCreditGrantService
```

Expected: all tests PASS.

- [ ] **Step 3.5: Commit**

```bash
git add internal/service/creditgrant.go internal/service/creditgrant_test.go
git commit -m "feat(creditgrant): add catch-up loop for backdated subscription credit grants"
```

---

### Task 4: Mirror changes in `internal/ee/service/credit_grants.go`

The EE service has its own parallel implementations of `createNextPeriodApplication`, `skipCreditGrantApplication`, `applyCreditGrantToWallet`, and `processScheduledApplication`. Apply the same signature chain and add `processCatchUpApplications`.

**Files:**
- Modify: `internal/ee/service/credit_grants.go`

- [ ] **Step 4.1: Update `createNextPeriodApplication` in EE service (line 440–464)**

```go
// BEFORE signature:
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) error {

// AFTER signature:
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

Return value changes:
```go
// error path after CalculateNextCreditGrantPeriod:
// BEFORE: return err
// AFTER: return nil, err

// grant end-date guard:
// BEFORE: return nil
// AFTER: return nil, nil

// Validate error:
// BEFORE: return err
// AFTER: return nil, err

// BEFORE (final line):
return s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)

// AFTER:
nextPeriodCGA := nextPeriodCGAReq.ToCreditGrantApplication(ctx)
if err := s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA); err != nil {
    return nil, err
}
return nextPeriodCGA, nil
```

- [ ] **Step 4.2: Update `skipCreditGrantApplication` in EE service (line 478–488)**

```go
// BEFORE:
func (s *creditGrantService) skipCreditGrantApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
	cga.ApplicationStatus = types.ApplicationStatusSkipped
	cga.AppliedAt = nil
	if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
		return err
	}
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		return s.createNextPeriodApplication(ctx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
	}
	return nil
}

// AFTER:
func (s *creditGrantService) skipCreditGrantApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) (*domainCreditGrantApplication.CreditGrantApplication, error) {
	cga.ApplicationStatus = types.ApplicationStatusSkipped
	cga.AppliedAt = nil
	if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
		return nil, err
	}
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		nextCGA, err := s.createNextPeriodApplication(ctx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
		if err != nil {
			return nil, err
		}
		return nextCGA, nil
	}
	return nil, nil
}
```

- [ ] **Step 4.3: Update `applyCreditGrantToWallet` in EE service (line 244–378)**

Change signature:
```go
// BEFORE:
func (s *creditGrantService) applyCreditGrantToWallet(...) error {

// AFTER:
func (s *creditGrantService) applyCreditGrantToWallet(...) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

Early-exit failures before `WithTx` (lines ~249, ~284):
```go
// BEFORE: return s.handleCreditGrantFailure(ctx, cga, err, "...")
// AFTER:  return nil, s.handleCreditGrantFailure(ctx, cga, err, "...")
```

Validation error for invalid expiry duration unit (~line 307):
```go
// BEFORE: return ierr.NewError("invalid expiration duration unit").Mark(ierr.ErrValidation)
// AFTER:  return nil, ierr.NewError("invalid expiration duration unit").Mark(ierr.ErrValidation)
```

Also `return err` for `CalculateBillingPeriods` failure (~line 323):
```go
// BEFORE: return err
// AFTER:  return nil, err
```

Add closure capture variable before `err = s.DB.WithTx(...)` (~line 347):
```go
var nextCGA *domainCreditGrantApplication.CreditGrantApplication

err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
```

Replace both `createNextPeriodApplication` calls inside the tx closure:
```go
// BEFORE (expiry-skip path, ~line 355):
if grant.Cadence == types.CreditGrantCadenceRecurring {
    return s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
}

// AFTER:
if grant.Cadence == types.CreditGrantCadenceRecurring {
    created, err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
    if err != nil {
        return err
    }
    nextCGA = created
}

// BEFORE (normal apply path, ~line 369):
if grant.Cadence == types.CreditGrantCadenceRecurring {
    return s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
}

// AFTER:
if grant.Cadence == types.CreditGrantCadenceRecurring {
    created, err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
    if err != nil {
        return err
    }
    nextCGA = created
}
```

After the `WithTx` block:
```go
// BEFORE:
if err != nil {
    return s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
}
return nil

// AFTER:
if err != nil {
    return nil, s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
}
return nextCGA, nil
```

- [ ] **Step 4.4: Update `processScheduledApplication` in EE service (line 393–438)**

Change signature and switch block:
```go
// BEFORE:
func (s *creditGrantService) processScheduledApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication) error {

// AFTER:
func (s *creditGrantService) processScheduledApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication) (*domainCreditGrantApplication.CreditGrantApplication, error) {
```

Error returns before the switch:
```go
// BEFORE: return err  (for subscription/grant lookup failures)
// AFTER:  return nil, err
```

The switch block:
```go
switch action {
case service.StateActionApply:
	nextCGA, err := s.applyCreditGrantToWallet(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
	if err != nil {
		return nil, err
	}
	return nextCGA, nil
case service.StateActionSkip:
	nextCGA, err := s.skipCreditGrantApplication(ctx, cga, creditGrant.CreditGrant, subscription.Subscription)
	if err != nil {
		return nil, err
	}
	return nextCGA, nil
case service.StateActionDefer:
	if err := s.deferCreditGrantApplication(ctx, cga); err != nil {
		return nil, err
	}
	return nil, nil
case service.StateActionCancel:
	cga.ApplicationStatus = types.ApplicationStatusCancelled
	cga.AppliedAt = nil
	if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
		return nil, err
	}
	if err := s.cancelFutureGrantApplications(ctx, creditGrant.CreditGrant); err != nil {
		return nil, err
	}
	return nil, nil
}
return nil, nil
```

- [ ] **Step 4.5: Add `processCatchUpApplications` to EE service**

Insert after `processScheduledApplication` in the EE service:

```go
const maxCatchUpIterations = 100

func (s *creditGrantService) processCatchUpApplications(
	ctx context.Context,
	initialCGA *domainCreditGrantApplication.CreditGrantApplication,
) error {
	currentCGA := initialCGA
	startTime := currentCGA.ScheduledFor

	for i := 0; i < maxCatchUpIterations; i++ {
		nextCGA, err := s.processScheduledApplication(ctx, currentCGA)
		if err != nil {
			s.Logger.Errorw("catch-up loop stopping due to failure",
				"cga_id", currentCGA.ID,
				"grant_id", currentCGA.CreditGrantID,
				"subscription_id", currentCGA.SubscriptionID,
				"iterations_completed", i,
				"error", err)
			return nil
		}
		if nextCGA == nil {
			break
		}
		if nextCGA.ScheduledFor.After(time.Now().UTC()) {
			break
		}
		currentCGA = nextCGA
	}

	s.Logger.Infow("EE credit grant catch-up loop completed",
		"grant_id", initialCGA.CreditGrantID,
		"subscription_id", initialCGA.SubscriptionID,
		"catch_up_from", startTime,
		"catch_up_to", currentCGA.ScheduledFor)

	return nil
}
```

Note: `maxCatchUpIterations` is already declared in `internal/service/creditgrant.go`. In the EE package, declare it as a local constant with the same value to avoid cross-package dependency on an unexported constant:
```go
const maxCatchUpIterations = 100
```

- [ ] **Step 4.6: Update EE `initializeCreditGrantWorkflow` caller (line 200–203)**

```go
// BEFORE:
if cg.CreditGrantAnchor.Before(time.Now()) || cg.CreditGrantAnchor.Equal(time.Now()) {
    if err := s.processScheduledApplication(ctx, cga); err != nil {
        s.Logger.Errorw("failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
    }
}

// AFTER:
if cg.CreditGrantAnchor.Before(time.Now()) || cg.CreditGrantAnchor.Equal(time.Now()) {
    if err := s.processCatchUpApplications(ctx, cga); err != nil {
        s.Logger.Errorw("failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
    }
}
```

- [ ] **Step 4.7: Verify full build**

```bash
make build
```

Expected: clean build.

- [ ] **Step 4.8: Run all tests**

```bash
go test -v -race ./internal/service -run TestCreditGrantService
go test -v -race ./internal/...
```

Expected: all tests PASS.

- [ ] **Step 4.9: Commit**

```bash
git add internal/ee/service/credit_grants.go
git commit -m "feat(ee/creditgrant): mirror catch-up loop for backdated subscription credit grants"
```
