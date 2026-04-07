# Subscription Modification Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract subscription inheritance into a new `SubscriptionModificationService`, add seat-based mid-cycle quantity change with Stripe-style proration, fix two double-billing bugs, and wire up two new API endpoints.

**Architecture:** New `subscriptionModificationService` (same `ServiceParams` DI pattern as `subscriptionChangeService`) handles both inheritance and quantity changes. Two orthogonal billing bug fixes (proration date and partial-period guard) are applied independently. The old `ExecuteSubscriptionModify` method and its DTO are deleted — not deprecated.

**Tech Stack:** Go 1.23+, Gin, Uber FX, Ent ORM, testify/suite, shopspring/decimal, samber/lo

---

## File Structure

**New files:**
- `internal/api/dto/subscription_modification.go` — All new DTOs (request + response)
- `internal/service/subscription_modification.go` — Service implementation + interface
- `internal/api/v1/subscription_modification.go` — HTTP handler
- `internal/service/subscription_modification_test.go` — Unit tests

**Modified files:**
- `internal/domain/subscription/line_item.go` — Add `GetPeriodStart`, `GetPeriodEnd`, `GetPeriod` helpers
- `internal/service/proration.go` — Fix 1: ProrationDate bug (line 561)
- `internal/service/billing.go` — Fix 2: partial-period guard in `CalculateFixedCharges` else-branch
- `internal/interfaces/service.go` — Remove `ExecuteSubscriptionModify` from `SubscriptionService`, add `SubscriptionModificationService` interface
- `internal/api/dto/subscription.go` — Delete `ExecuteSubscriptionInheritanceRequest` and its `Validate()` method
- `internal/service/subscription.go` — Delete `ExecuteSubscriptionModify` method
- `internal/service/subscription_test.go` — Update tests that called old method to call new service
- `internal/api/v1/subscription.go` — Delete `ExecuteSubscriptionModify` handler
- `internal/api/router.go` — Rewire `/:id/modify/execute` + add `/:id/modify/preview`
- `cmd/server/main.go` — Add `service.NewSubscriptionModificationService` to FX providers, add param + handler to `provideHandlers`
- `internal/api/router.go` — Add `SubscriptionModification` field to `Handlers` struct

---

## Task 1: Add `GetPeriod*` helpers to `SubscriptionLineItem`

**Files:**
- Modify: `internal/domain/subscription/line_item.go`

- [ ] **Step 1: Write failing test**

Create a temporary test at the bottom of `internal/domain/subscription/line_item.go`'s package or in a `_test.go` file. Actually, add to `internal/domain/subscription/line_item_test.go` (create if absent):

```go
package subscription_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/stretchr/testify/assert"
)

func TestGetPeriodStart(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{StartDate: mid}
	assert.Equal(t, mid, li.GetPeriodStart(base))  // StartDate > default → use StartDate
	assert.Equal(t, base, li.GetPeriodStart(end))   // default > StartDate → use default
}

func TestGetPeriodEnd(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{EndDate: mid}
	assert.Equal(t, mid, li.GetPeriodEnd(end))    // EndDate < default → use EndDate
	assert.Equal(t, end, li.GetPeriodEnd(base))   // default < EndDate → use default

	liNoEnd := &subscription.SubscriptionLineItem{}
	assert.Equal(t, end, liNoEnd.GetPeriodEnd(end)) // zero EndDate → use default
}

func TestGetPeriod(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{StartDate: mid}
	s, e := li.GetPeriod(start, end)
	assert.Equal(t, mid, s)
	assert.Equal(t, end, e)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/domain/subscription/... -run TestGetPeriod -v
```

Expected: FAIL — `GetPeriodStart undefined`

- [ ] **Step 3: Add helpers to `line_item.go`**

Add after the `IsUsage()` method (after line ~77 in `internal/domain/subscription/line_item.go`):

```go
// GetPeriodStart returns the effective start of this line item within a billing period.
// Returns max(item.StartDate, defaultPeriodStart).
func (li *SubscriptionLineItem) GetPeriodStart(defaultPeriodStart time.Time) time.Time {
	if li.StartDate.IsZero() || defaultPeriodStart.After(li.StartDate) {
		return defaultPeriodStart
	}
	return li.StartDate
}

// GetPeriodEnd returns the effective end of this line item within a billing period.
// Returns min(item.EndDate, defaultPeriodEnd). If EndDate is zero, defaultPeriodEnd is returned.
func (li *SubscriptionLineItem) GetPeriodEnd(defaultPeriodEnd time.Time) time.Time {
	if li.EndDate.IsZero() || li.EndDate.After(defaultPeriodEnd) {
		return defaultPeriodEnd
	}
	return li.EndDate
}

// GetPeriod returns the effective (start, end) for this line item clipped to the given period.
func (li *SubscriptionLineItem) GetPeriod(defaultPeriodStart, defaultPeriodEnd time.Time) (time.Time, time.Time) {
	return li.GetPeriodStart(defaultPeriodStart), li.GetPeriodEnd(defaultPeriodEnd)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/domain/subscription/... -run TestGetPeriod -v
```

Expected: PASS

- [ ] **Step 5: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/domain/subscription/line_item.go internal/domain/subscription/line_item_test.go
git commit -m "feat(subscription): add GetPeriodStart/GetPeriodEnd/GetPeriod helpers to SubscriptionLineItem"
```

---

## Task 2: Fix 1 — ProrationDate bug in `CreateProrationParamsForLineItem`

**Files:**
- Modify: `internal/service/proration.go` (line 561)

- [ ] **Step 1: Locate the exact line**

In `internal/service/proration.go`, find the `CreateProrationParamsForLineItem` function (starts around line 508). The return statement (around line 551-570) contains:

```go
ProrationDate: subscription.StartDate,
```

- [ ] **Step 2: Change `ProrationDate`**

Replace:
```go
		ProrationDate:         subscription.StartDate,
```

With:
```go
		ProrationDate:         item.GetPeriodStart(periodStart),
```

The full return block becomes:
```go
	return proration.ProrationParams{
		SubscriptionID:        subscription.ID,
		LineItemID:            item.ID,
		PlanPayInAdvance:      price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart:    periodStart,
		CurrentPeriodEnd:      subscription.CurrentPeriodEnd.Add(time.Second * -1),
		Action:                action,
		NewPriceID:            item.PriceID,
		NewQuantity:           item.Quantity,
		NewPricePerUnit:       price.Amount,
		ProrationDate:         item.GetPeriodStart(periodStart),
		ProrationBehavior:     behavior,
		CustomerTimezone:      subscription.CustomerTimezone,
		OriginalAmountPaid:    decimal.Zero,
		PreviousCreditsIssued: decimal.Zero,
		ProrationStrategy:     types.StrategySecondBased,
		Currency:              price.Currency,
		PlanDisplayName:       item.PlanDisplayName,
	}, nil
```

- [ ] **Step 3: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 4: Run existing proration tests**

```bash
go test ./internal/service/... -run TestProration -v
```

Expected: all PASS (or no tests match — that's fine too, check with `go test ./internal/...`)

- [ ] **Step 5: Commit**

```bash
git add internal/service/proration.go
git commit -m "fix(proration): use line item period start as ProrationDate instead of subscription start date"
```

---

## Task 3: Fix 2 — Partial-period guard in `CalculateFixedCharges`

**Files:**
- Modify: `internal/service/billing.go` (lines 211-225)

- [ ] **Step 1: Locate the else-branch**

In `internal/service/billing.go`, find `CalculateFixedCharges`. The `else` branch (around line 211-225) currently reads:

```go
		} else {
			// Same or shorter cadence: proration, invoice period as service period
			amount = priceService.CalculateCost(ctx, price.Price, item.Quantity)
			proratedAmount, err := s.applyProrationToLineItem(ctx, sub, item, price.Price, amount, &periodStart, &periodEnd)
			if err != nil {
				s.Logger.Warnw("failed to apply proration to line item, using original amount",
					"error", err,
					"subscription_id", sub.ID,
					"line_item_id", item.ID,
					"price_id", item.PriceID)
				proratedAmount = amount
			}
			amount = proratedAmount
			linePeriodStart, linePeriodEnd = periodStart, periodEnd
		}
```

- [ ] **Step 2: Replace with two-path logic**

Replace the entire else-branch with:

```go
		} else {
			// Same or shorter cadence: proration, invoice period as service period
			amount = priceService.CalculateCost(ctx, price.Price, item.Quantity)
			effectiveStart, effectiveEnd := item.GetPeriod(periodStart, periodEnd)
			if !effectiveEnd.After(effectiveStart) {
				s.Logger.Debugw("skipping line item: not active in invoice period",
					"line_item_id", item.ID,
					"effective_start", effectiveStart,
					"effective_end", effectiveEnd)
				continue
			}
			totalDuration := periodEnd.Sub(periodStart)
			effectiveDuration := effectiveEnd.Sub(effectiveStart)
			if effectiveDuration < totalDuration {
				// Partial-period line item (versioned mid-cycle): scale by time ratio
				ratio := decimal.NewFromFloat(effectiveDuration.Seconds()).
					Div(decimal.NewFromFloat(totalDuration.Seconds()))
				amount = amount.Mul(ratio)
				linePeriodStart, linePeriodEnd = effectiveStart, effectiveEnd
			} else {
				// Full-period line item: apply existing proration logic (first-period, cancellation, etc.)
				proratedAmount, err := s.applyProrationToLineItem(ctx, sub, item, price.Price, amount, &periodStart, &periodEnd)
				if err != nil {
					s.Logger.Warnw("failed to apply proration to line item, using original amount",
						"error", err,
						"subscription_id", sub.ID,
						"line_item_id", item.ID,
						"price_id", item.PriceID)
					proratedAmount = amount
				}
				amount = proratedAmount
				linePeriodStart, linePeriodEnd = periodStart, periodEnd
			}
		}
```

- [ ] **Step 3: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 4: Run existing billing tests**

```bash
go test ./internal/service/... -run TestBilling -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/billing.go
git commit -m "fix(billing): add partial-period guard in CalculateFixedCharges to prevent double-billing versioned line items"
```

---

## Task 4: New DTOs file

**Files:**
- Create: `internal/api/dto/subscription_modification.go`

- [ ] **Step 1: Create the file**

```go
package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// =============================================
// Inheritance (existing behavior, migrated here)
// =============================================

// ExecuteSubscriptionInheritanceRequest is the payload for adding
// inherited child subscriptions to a parent subscription.
// Migrated from dto.ExecuteSubscriptionInheritanceRequest.
type SubModifyInheritanceRequest struct {
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
}

func (r *SubModifyInheritanceRequest) Validate() error {
	if len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
		return ierr.NewError("at least one external customer ID is required").
			WithHint("Provide external_customer_ids_to_inherit_subscription with at least one non-empty value").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// =============================================
// Quantity change
// =============================================

// LineItemQuantityChange describes a quantity change for a single line item.
type LineItemQuantityChange struct {
	ID       string          `json:"id" binding:"required"`
	Quantity decimal.Decimal `json:"quantity" swaggertype:"string" binding:"required"`
}

// SubModifyQuantityChangeRequest is the payload for mid-cycle seat/quantity changes.
type SubModifyQuantityChangeRequest struct {
	LineItems []LineItemQuantityChange `json:"line_items" binding:"required,min=1"`
}

func (r *SubModifyQuantityChangeRequest) Validate() error {
	if len(r.LineItems) == 0 {
		return ierr.NewError("at least one line item is required").
			WithHint("Provide line_items with at least one entry").
			Mark(ierr.ErrValidation)
	}
	for _, li := range r.LineItems {
		if li.ID == "" {
			return ierr.NewError("line item ID is required").
				WithHint("Each line_item entry must have a non-empty id").
				Mark(ierr.ErrValidation)
		}
		if li.Quantity.LessThanOrEqual(decimal.Zero) {
			return ierr.NewError("quantity must be positive").
				WithHint("Each line_item quantity must be greater than zero").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// =============================================
// Unified execute / preview request
// =============================================

// SubscriptionModifyType identifies the kind of modification.
type SubscriptionModifyType string

const (
	SubscriptionModifyTypeInheritance     SubscriptionModifyType = "inheritance"
	SubscriptionModifyTypeQuantityChange  SubscriptionModifyType = "quantity_change"
)

// ExecuteSubscriptionModifyRequest is the unified body for
// POST /subscriptions/:id/modify/execute and /modify/preview.
// Exactly one of InheritanceParams or QuantityChangeParams must be set.
type ExecuteSubscriptionModifyRequest struct {
	Type                SubscriptionModifyType          `json:"type" binding:"required"`
	InheritanceParams   *SubModifyInheritanceRequest    `json:"inheritance_params,omitempty"`
	QuantityChangeParams *SubModifyQuantityChangeRequest `json:"quantity_change_params,omitempty"`
}

func (r *ExecuteSubscriptionModifyRequest) Validate() error {
	switch r.Type {
	case SubscriptionModifyTypeInheritance:
		if r.InheritanceParams == nil {
			return ierr.NewError("inheritance_params is required for type 'inheritance'").
				Mark(ierr.ErrValidation)
		}
		return r.InheritanceParams.Validate()
	case SubscriptionModifyTypeQuantityChange:
		if r.QuantityChangeParams == nil {
			return ierr.NewError("quantity_change_params is required for type 'quantity_change'").
				Mark(ierr.ErrValidation)
		}
		return r.QuantityChangeParams.Validate()
	default:
		return ierr.NewError("unknown modification type: " + string(r.Type)).
			WithHint("Valid values: inheritance, quantity_change").
			Mark(ierr.ErrValidation)
	}
}

// =============================================
// Response DTOs (Orb-inspired changed_resources)
// =============================================

// ChangedLineItem describes a subscription line item that was created, updated, or ended.
type ChangedLineItem struct {
	ID           string          `json:"id"`
	PriceID      string          `json:"price_id"`
	Quantity     decimal.Decimal `json:"quantity" swaggertype:"string"`
	StartDate    string          `json:"start_date,omitempty"`
	EndDate      string          `json:"end_date,omitempty"`
	ChangeAction string          `json:"change_action"` // "created" | "updated" | "ended"
}

// ChangedSubscription describes a subscription that was created or updated.
type ChangedSubscription struct {
	ID     string                `json:"id"`
	Action string                `json:"action"` // "created" | "updated"
	Status types.SubscriptionStatus `json:"status"`
}

// ChangedInvoice describes a proration invoice that was created.
type ChangedInvoice struct {
	ID     string `json:"id"`
	Action string `json:"action"` // "created"
	Status string `json:"status"`
}

// ChangedResources is the Orb-inspired envelope for all mutation side-effects.
type ChangedResources struct {
	LineItems     []ChangedLineItem     `json:"line_items,omitempty"`
	Subscriptions []ChangedSubscription `json:"subscriptions,omitempty"`
	Invoices      []ChangedInvoice      `json:"invoices,omitempty"`
}

// SubscriptionModifyResponse is the response from execute and preview endpoints.
type SubscriptionModifyResponse struct {
	// The subscription after the modification.
	Subscription *SubscriptionResponse `json:"subscription"`
	// All resources created or mutated as a result of this modification.
	ChangedResources ChangedResources `json:"changed_resources"`
}
```

- [ ] **Step 2: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/api/dto/subscription_modification.go
git commit -m "feat(dto): add SubscriptionModificationService DTOs with unified request and Orb-style changed_resources response"
```

---

## Task 5: Delete dead code — old DTO, service method, interface entry, and handler

**Files:**
- Modify: `internal/api/dto/subscription.go` (delete `ExecuteSubscriptionInheritanceRequest`)
- Modify: `internal/service/subscription.go` (delete `ExecuteSubscriptionModify`)
- Modify: `internal/interfaces/service.go` (remove `ExecuteSubscriptionModify` from `SubscriptionService`)
- Modify: `internal/api/v1/subscription.go` (delete `ExecuteSubscriptionModify` handler)
- Modify: `internal/service/subscription_test.go` (remove tests that call old method)

- [ ] **Step 1: Delete DTO from `internal/api/dto/subscription.go`**

Find and delete these lines (around 284-297):

```go
// ExecuteSubscriptionInheritanceRequest is the payload for
/ POST /subscriptions:id/modify/execute.
type ExecuteSubscriptionInheritanceRequest struct {
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
}

func (r *ExecuteSubscriptionInheritanceRequest) Validate() error {
	if len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
		return ierr.NewError("at least one external customer ID is required").
			WithHint("Provide external_customer_ids_to_inherit_subscription with at least one non-empty value").
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 2: Delete `ExecuteSubscriptionModify` from `internal/service/subscription.go`**

Delete lines 1644-1714 (the entire `ExecuteSubscriptionModify` method and its comment):

```go
// ExecuteSubscriptionModify adds inherited child subscriptions for external customer IDs on an active standalone or parent subscription.
func (s *subscriptionService) ExecuteSubscriptionModify(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionInheritanceRequest) (*dto.SubscriptionResponse, error) {
    // ... entire method body
}
```

- [ ] **Step 3: Remove from interface in `internal/interfaces/service.go`**

Remove line 84:
```go
	ExecuteSubscriptionModify(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionInheritanceRequest) (*dto.SubscriptionResponse, error)
```

- [ ] **Step 4: Delete handler in `internal/api/v1/subscription.go`**

Find `ExecuteSubscriptionModify` handler (starting around line 229 with the swagger comment block) and delete the entire function including all `@` comment lines above it down through the closing `}`.

The block to delete starts at:
```go
// @Summary Execute subscription modification
```
and ends with the closing `}` of `func (h *SubscriptionHandler) ExecuteSubscriptionModify(c *gin.Context)`.

- [ ] **Step 5: Delete old tests from `internal/service/subscription_test.go`**

Find the test methods that call `s.service.ExecuteSubscriptionModify` (search for `ExecuteSubscriptionModify` in the file). These tests will be replaced by tests in `subscription_modification_test.go`. Delete the following test methods entirely:
- `TestExecuteSubscriptionModify_Success` (or however it's named)
- `TestExecuteSubscriptionModify_DuplicateChild`
- `TestExecuteSubscriptionModify_InheritedSubscriptionCannotAddChildren`

(Run `grep -n ExecuteSubscriptionModify internal/service/subscription_test.go` to find exact line numbers first.)

- [ ] **Step 6: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/api/dto/subscription.go internal/service/subscription.go internal/interfaces/service.go internal/api/v1/subscription.go internal/service/subscription_test.go
git commit -m "refactor(subscription): remove dead ExecuteSubscriptionModify code, DTO, interface entry, and handler"
```

---

## Task 6: Add `SubscriptionModificationService` interface

**Files:**
- Modify: `internal/interfaces/service.go`

- [ ] **Step 1: Add interface**

After the closing `}` of `SubscriptionService` in `internal/interfaces/service.go` (around line 120+), add:

```go
// SubscriptionModificationService handles mid-cycle subscription modifications:
// seat/quantity changes with proration, and subscription inheritance management.
type SubscriptionModificationService interface {
	// Execute performs the modification immediately.
	Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)

	// Preview returns what would happen without committing any changes.
	Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)
}
```

- [ ] **Step 2: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/interfaces/service.go
git commit -m "feat(interfaces): add SubscriptionModificationService interface"
```

---

## Task 7: Implement `SubscriptionModificationService`

**Files:**
- Create: `internal/service/subscription_modification.go`

- [ ] **Step 1: Write the failing test (stub)**

Create `internal/service/subscription_modification_test.go` with a minimal failing test:

```go
package service_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type SubscriptionModificationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionModificationService
}

func TestSubscriptionModificationServiceSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionModificationServiceSuite))
}

func (s *SubscriptionModificationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewSubscriptionModificationService(s.GetServiceParams())
}
```

- [ ] **Step 2: Run failing test**

```bash
go test ./internal/service/... -run TestSubscriptionModificationServiceSuite -v
```

Expected: FAIL — `SubscriptionModificationService undefined`, `NewSubscriptionModificationService undefined`

- [ ] **Step 3: Create the service implementation**

Create `internal/service/subscription_modification.go`:

```go
package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// SubscriptionModificationService handles mid-cycle subscription modifications.
type SubscriptionModificationService interface {
	Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)
	Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)
}

type subscriptionModificationService struct {
	serviceParams ServiceParams
}

// NewSubscriptionModificationService creates a new SubscriptionModificationService.
func NewSubscriptionModificationService(serviceParams ServiceParams) SubscriptionModificationService {
	return &subscriptionModificationService{serviceParams: serviceParams}
}

// Execute performs the modification immediately and returns what changed.
func (s *subscriptionModificationService) Execute(
	ctx context.Context,
	subscriptionID string,
	req dto.ExecuteSubscriptionModifyRequest,
) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.executeInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.executeQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	default:
		return nil, ierr.NewError("unsupported modification type").Mark(ierr.ErrValidation)
	}
}

// Preview returns a dry-run of what Execute would do.
func (s *subscriptionModificationService) Preview(
	ctx context.Context,
	subscriptionID string,
	req dto.ExecuteSubscriptionModifyRequest,
) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.previewInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.previewQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	default:
		return nil, ierr.NewError("unsupported modification type").Mark(ierr.ErrValidation)
	}
}

// ─── Inheritance ──────────────────────────────────────────────────────────────

// executeInheritance migrates the logic from the old subscriptionService.ExecuteSubscriptionModify.
func (s *subscriptionModificationService) executeInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("inherited subscription cannot add inheritance children").
			WithHint("Use the parent subscription to add customers to inheritance").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("The subscription must be active to add inherited customers").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":     subscriptionID,
				"subscription_status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	existingInherited, err := s.getInheritedSubscriptions(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	existingByCustomer := lo.SliceToMap(existingInherited, func(ch *subscription.Subscription) (string, bool) {
		return ch.CustomerID, true
	})
	for _, cid := range childCustomerIDs {
		if _, dup := existingByCustomer[cid]; dup {
			return nil, ierr.NewError("customer already inherits this subscription").
				WithHint("Each customer may only have one inherited subscription per parent").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": subscriptionID,
					"customer_id":     cid,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	var createdSubIDs []string
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		if sub.SubscriptionType == types.SubscriptionTypeStandalone {
			sub.SubscriptionType = types.SubscriptionTypeParent
			if err := sp.SubRepo.Update(txCtx, sub); err != nil {
				return err
			}
		}
		for _, childID := range childCustomerIDs {
			childSub, err := s.createInheritedSubscription(txCtx, sub, childID)
			if err != nil {
				return err
			}
			createdSubIDs = append(createdSubIDs, childSub.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	changedSubs := []dto.ChangedSubscription{
		{ID: subscriptionID, Action: "updated", Status: sub.SubscriptionStatus},
	}
	for _, id := range createdSubIDs {
		changedSubs = append(changedSubs, dto.ChangedSubscription{ID: id, Action: "created", Status: types.SubscriptionStatusActive})
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

func (s *subscriptionModificationService) previewInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	changedSubs := []dto.ChangedSubscription{
		{ID: subscriptionID, Action: "updated", Status: sub.SubscriptionStatus},
	}
	for _, cid := range childCustomerIDs {
		changedSubs = append(changedSubs, dto.ChangedSubscription{ID: "(preview-" + cid + ")", Action: "created", Status: types.SubscriptionStatusActive})
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

// ─── Quantity Change ──────────────────────────────────────────────────────────

func (s *subscriptionModificationService) executeQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	sub, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions support quantity changes").
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()

	var changedLineItems []dto.ChangedLineItem
	var changedInvoices []dto.ChangedInvoice

	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		for _, change := range params.LineItems {
			// Fetch the specific line item
			lineItem, err := sp.SubLineItemRepo.Get(txCtx, change.ID)
			if err != nil {
				return err
			}
			if lineItem.SubscriptionID != subscriptionID {
				return ierr.NewError("line item does not belong to this subscription").
					WithReportableDetails(map[string]interface{}{
						"line_item_id":     change.ID,
						"subscription_id":  subscriptionID,
					}).
					Mark(ierr.ErrValidation)
			}
			if lineItem.Status != types.StatusPublished {
				return ierr.NewError("line item is not active").
					Mark(ierr.ErrValidation)
			}
			if lineItem.PriceType != types.PRICE_TYPE_FIXED {
				return ierr.NewError("quantity change is only supported for fixed-price line items").
					Mark(ierr.ErrValidation)
			}

			// End-date the old line item at now
			lineItem.EndDate = now
			if err := sp.SubLineItemRepo.Update(txCtx, lineItem); err != nil {
				return err
			}
			changedLineItems = append(changedLineItems, dto.ChangedLineItem{
				ID:           lineItem.ID,
				PriceID:      lineItem.PriceID,
				Quantity:     lineItem.Quantity,
				EndDate:      now.Format(time.RFC3339),
				ChangeAction: "ended",
			})

			// Create new line item with updated quantity starting now
			newItem := *lineItem
			newItem.ID = types.GenerateUUID()
			newItem.Quantity = change.Quantity
			newItem.StartDate = now
			newItem.EndDate = time.Time{} // no end
			if err := sp.SubLineItemRepo.Create(txCtx, &newItem); err != nil {
				return err
			}
			changedLineItems = append(changedLineItems, dto.ChangedLineItem{
				ID:           newItem.ID,
				PriceID:      newItem.PriceID,
				Quantity:     newItem.Quantity,
				StartDate:    now.Format(time.RFC3339),
				ChangeAction: "created",
			})

			// For in-advance items: generate proration invoice
			if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
				inv, err := s.generateProrationInvoice(txCtx, sub, lineItem, &newItem, now)
				if err != nil {
					return err
				}
				if inv != nil {
					changedInvoices = append(changedInvoices, dto.ChangedInvoice{
						ID:     inv.ID,
						Action: "created",
						Status: string(inv.PaymentStatus),
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

func (s *subscriptionModificationService) previewQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	sub, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var changedLineItems []dto.ChangedLineItem
	var changedInvoices []dto.ChangedInvoice

	for _, change := range params.LineItems {
		lineItem, err := sp.SubLineItemRepo.Get(ctx, change.ID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("line item does not belong to this subscription").
				Mark(ierr.ErrValidation)
		}

		changedLineItems = append(changedLineItems, dto.ChangedLineItem{
			ID:           lineItem.ID,
			PriceID:      lineItem.PriceID,
			Quantity:     lineItem.Quantity,
			EndDate:      now.Format(time.RFC3339),
			ChangeAction: "ended",
		})
		changedLineItems = append(changedLineItems, dto.ChangedLineItem{
			ID:           "(preview)",
			PriceID:      lineItem.PriceID,
			Quantity:     change.Quantity,
			StartDate:    now.Format(time.RFC3339),
			ChangeAction: "created",
		})

		if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
			inv, err := s.previewProrationInvoice(ctx, sub, lineItem, change.Quantity, now)
			if err != nil {
				return nil, err
			}
			if inv != nil {
				changedInvoices = append(changedInvoices, *inv)
			}
		}
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

// ─── Proration invoice helpers ────────────────────────────────────────────────

// generateProrationInvoice creates a proration invoice for an in-advance line item quantity change.
// For upgrades: charges the prorated difference for the remainder of the period.
// For downgrades: issues a wallet credit for the prorated unused amount.
func (s *subscriptionModificationService) generateProrationInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newItem *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
) (*dto.InvoiceResponse, error) {
	sp := s.serviceParams
	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
	if err != nil {
		return nil, err
	}

	// Build proration params for the new (higher/lower) quantity item
	params, err := prorationSvc.CreateProrationParamsForLineItem(sub, newItem, price.Price, types.ProrationActionChangeQuantity, types.ProrationBehaviorProrate)
	if err != nil {
		return nil, err
	}

	// Also supply previous quantity info for delta calculation
	params.PreviousQuantity = &oldItem.Quantity

	result, err := prorationSvc.Calculate(params)
	if err != nil {
		return nil, err
	}

	if result.NetAmount.IsZero() {
		return nil, nil
	}

	invoiceSvc := NewInvoiceService(sp)

	if result.NetAmount.GreaterThan(dec.Zero) {
		// Upgrade: charge the difference
		inv, err := invoiceSvc.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     sub.CustomerID,
			SubscriptionID: &sub.ID,
			BillingReason:  types.BillingReasonSubscriptionUpdate,
			Currency:       sub.Currency,
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					PriceID:     lo.ToPtr(oldItem.PriceID),
					Amount:      result.NetAmount,
					Quantity:    newItem.Quantity,
					PeriodStart: lo.ToPtr(effectiveDate),
					PeriodEnd:   lo.ToPtr(sub.CurrentPeriodEnd),
					DisplayName: lo.ToPtr(oldItem.DisplayName + " (quantity change proration)"),
				},
			},
		})
		if err != nil {
			return nil, err
		}
		// Auto-finalize and attempt payment
		if _, err := invoiceSvc.ComputeInvoice(ctx, inv.ID, nil); err != nil {
			sp.Logger.Warnw("failed to compute proration invoice", "invoice_id", inv.ID, "error", err)
		}
		return inv, nil
	}

	// Downgrade: issue wallet credit
	walletSvc := NewWalletService(sp)
	creditAmount := result.NetAmount.Abs()
	if err := walletSvc.TopUpWallet(ctx, sub.CustomerID, sub.Currency, creditAmount, "quantity_change_proration"); err != nil {
		sp.Logger.Warnw("failed to top up wallet for downgrade proration", "customer_id", sub.CustomerID, "amount", creditAmount, "error", err)
	}
	return nil, nil
}

// previewProrationInvoice returns a preview ChangedInvoice for a quantity change without persisting.
func (s *subscriptionModificationService) previewProrationInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newQuantity decimal.Decimal,
	effectiveDate time.Time,
) (*dto.ChangedInvoice, error) {
	sp := s.serviceParams
	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
	if err != nil {
		return nil, err
	}

	newItem := *oldItem
	newItem.Quantity = newQuantity
	newItem.StartDate = effectiveDate

	params, err := prorationSvc.CreateProrationParamsForLineItem(sub, &newItem, price.Price, types.ProrationActionChangeQuantity, types.ProrationBehaviorProrate)
	if err != nil {
		return nil, err
	}
	params.PreviousQuantity = &oldItem.Quantity

	result, err := prorationSvc.Calculate(params)
	if err != nil {
		return nil, err
	}

	action := "created"
	if result.NetAmount.LessThan(dec.Zero) {
		action = "wallet_credit"
	}

	return &dto.ChangedInvoice{
		ID:     "(preview)",
		Action: action,
		Status: "draft",
	}, nil
}

// ─── Inheritance helpers (migrated from subscriptionService) ─────────────────

func (s *subscriptionModificationService) resolveExternalCustomersForInheritance(
	ctx context.Context,
	parentCustomerID string,
	externalIDs []string,
) ([]string, error) {
	sp := s.serviceParams
	customerIDs := make([]string, 0, len(externalIDs))
	for _, extID := range externalIDs {
		cust, err := sp.CustomerRepo.GetByLookupKey(ctx, extID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Could not find customer with external ID: " + extID).
				Mark(ierr.ErrNotFound)
		}
		if cust.ID == parentCustomerID {
			return nil, ierr.NewError("cannot inherit subscription to own customer").
				Mark(ierr.ErrValidation)
		}
		customerIDs = append(customerIDs, cust.ID)
	}
	return customerIDs, nil
}

func (s *subscriptionModificationService) getInheritedSubscriptions(
	ctx context.Context,
	parentSubscriptionID string,
) ([]*subscription.Subscription, error) {
	sp := s.serviceParams
	filter := &types.SubscriptionFilter{
		ParentSubscriptionID: &parentSubscriptionID,
	}
	subs, err := sp.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *subscriptionModificationService) createInheritedSubscription(
	ctx context.Context,
	parent *subscription.Subscription,
	childCustomerID string,
) (*subscription.Subscription, error) {
	sp := s.serviceParams
	subSvc := NewSubscriptionService(sp)
	req := dto.CreateSubscriptionRequest{
		CustomerID:           childCustomerID,
		PlanID:               parent.PlanID,
		Currency:             parent.Currency,
		BillingPeriod:        parent.BillingPeriod,
		BillingPeriodCount:   parent.BillingPeriodCount,
		SubscriptionType:     types.SubscriptionTypeInherited,
		ParentSubscriptionID: &parent.ID,
	}
	resp, err := subSvc.CreateSubscription(ctx, req)
	if err != nil {
		return nil, err
	}
	return sp.SubRepo.Get(ctx, resp.ID)
}

func (s *subscriptionModificationService) publishSystemEvent(ctx context.Context, eventType types.WebhookEventType, subscriptionID string) {
	// Mirror the pattern from subscriptionService.publishSystemEvent
	sp := s.serviceParams
	if sp.WebhookPublisher == nil {
		return
	}
	_ = sp.WebhookPublisher.PublishSystemEvent(ctx, eventType, map[string]interface{}{
		"subscription_id": subscriptionID,
	})
}
```

- [ ] **Step 4: Verify build**

```bash
make build
```

Expected: exit 0 (or compile errors that need fixing — address them now)

- [ ] **Step 5: Run test suite to confirm stub passes**

```bash
go test ./internal/service/... -run TestSubscriptionModificationServiceSuite -v
```

Expected: PASS (suite setup works, no panics)

- [ ] **Step 6: Commit**

```bash
git add internal/service/subscription_modification.go internal/service/subscription_modification_test.go
git commit -m "feat(service): implement SubscriptionModificationService with inheritance and quantity change"
```

---

## Task 8: Write unit tests for `SubscriptionModificationService`

**Files:**
- Modify: `internal/service/subscription_modification_test.go`

- [ ] **Step 1: Write inheritance tests**

Replace the minimal stub in `subscription_modification_test.go` with:

```go
package service_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionModificationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service service.SubscriptionModificationService
}

func TestSubscriptionModificationServiceSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionModificationServiceSuite))
}

func (s *SubscriptionModificationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = service.NewSubscriptionModificationService(s.GetServiceParams())
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) createCustomerWithExternalID(externalID string) *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		BaseModel:  types.GetDefaultBaseModel(ctx),
		TenantID:   types.DefaultTenantID,
		ExternalID: externalID,
		Name:       "Test Customer " + externalID,
		Email:      externalID + "@test.com",
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

func (s *SubscriptionModificationServiceSuite) createActiveSubscription(customerID string) *subscription.Subscription {
	ctx := s.GetContext()
	now := s.GetNow()
	sub := &subscription.Subscription{
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         customerID,
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		StartDate:          now,
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionModificationServiceSuite) createFixedLineItem(subID, customerID, priceID string, qty decimal.Decimal) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      now,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// ─── Inheritance tests ────────────────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_Success() {
	ctx := s.GetContext()
	parent := s.createCustomerWithExternalID("parent-ext")
	child := s.createCustomerWithExternalID("child-ext")
	sub := s.createActiveSubscription(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	}
	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Len(resp.ChangedResources.Subscriptions, 2) // parent updated + child created
}

func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_DuplicateChildRejected() {
	ctx := s.GetContext()
	parent := s.createCustomerWithExternalID("parent2-ext")
	child := s.createCustomerWithExternalID("child2-ext")
	sub := s.createActiveSubscription(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)

	// Second call with same child must fail
	_, err = s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
}

func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_InheritedSubCannotAddChildren() {
	ctx := s.GetContext()
	parent := s.createCustomerWithExternalID("parent3-ext")
	child := s.createCustomerWithExternalID("child3-ext")
	parentSub := s.createActiveSubscription(parent.ID)

	// Create an inherited sub manually
	inherited := &subscription.Subscription{
		BaseModel:            types.GetDefaultBaseModel(ctx),
		CustomerID:           child.ID,
		Currency:             "USD",
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeInherited,
		ParentSubscriptionID: &parentSub.ID,
		CurrentPeriodStart:   s.GetNow(),
		CurrentPeriodEnd:     s.GetNow().AddDate(0, 1, 0),
		StartDate:            s.GetNow(),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, inherited))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{"some-ext"},
		},
	}
	_, err := s.service.Execute(ctx, inherited.ID, req)
	s.Require().Error(err)
}

// ─── Quantity change tests ────────────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_VersionsLineItem() {
	ctx := s.GetContext()
	cust := s.createCustomerWithExternalID("qty-cust-ext")
	sub := s.createActiveSubscription(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, "price-1", decimal.NewFromInt(10))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(20)},
			},
		},
	}
	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	// Two line item changes: one "ended", one "created"
	s.Len(resp.ChangedResources.LineItems, 2)
	s.Equal("ended", resp.ChangedResources.LineItems[0].ChangeAction)
	s.Equal("created", resp.ChangedResources.LineItems[1].ChangeAction)
	s.Equal(decimal.NewFromInt(20), resp.ChangedResources.LineItems[1].Quantity)
}

func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_OldLineItemIsEndDated() {
	ctx := s.GetContext()
	cust := s.createCustomerWithExternalID("qty-cust2-ext")
	sub := s.createActiveSubscription(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, "price-2", decimal.NewFromInt(5))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(15)},
			},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)

	// Old line item should now have a non-zero EndDate
	updated, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.False(updated.EndDate.IsZero(), "old line item should have been end-dated")
}

func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_WrongSubscriptionRejected() {
	ctx := s.GetContext()
	cust := s.createCustomerWithExternalID("qty-cust3-ext")
	sub1 := s.createActiveSubscription(cust.ID)
	sub2 := s.createActiveSubscription(cust.ID)
	li := s.createFixedLineItem(sub1.ID, cust.ID, "price-3", decimal.NewFromInt(5))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(15)},
			},
		},
	}
	_, err := s.service.Execute(ctx, sub2.ID, req) // wrong subscription
	s.Require().Error(err)
}

func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange_DoesNotPersist() {
	ctx := s.GetContext()
	cust := s.createCustomerWithExternalID("qty-preview-ext")
	sub := s.createActiveSubscription(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, "price-4", decimal.NewFromInt(5))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(15)},
			},
		},
	}
	resp, err := s.service.Preview(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Original line item should be unchanged
	original, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(original.EndDate.IsZero(), "preview must not modify the line item")
	_ = resp
}

func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_InvalidRequestRejected() {
	ctx := s.GetContext()
	cust := s.createCustomerWithExternalID("qty-invalid-ext")
	sub := s.createActiveSubscription(cust.ID)

	// Empty line items
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)

	// Zero quantity
	li := s.createFixedLineItem(sub.ID, cust.ID, "price-5", decimal.NewFromInt(5))
	req2 := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.Zero},
			},
		},
	}
	_, err = s.service.Execute(ctx, sub.ID, req2)
	s.Require().Error(err)
}
```

- [ ] **Step 2: Run all new tests**

```bash
go test ./internal/service/... -run TestSubscriptionModificationServiceSuite -v
```

Expected: all PASS (some may fail due to helpers not existing in ServiceParams — fix those compilation issues before proceeding)

- [ ] **Step 3: Run full service test suite to ensure no regressions**

```bash
go test ./internal/service/... -v 2>&1 | tail -30
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/subscription_modification_test.go
git commit -m "test(service): add unit tests for SubscriptionModificationService"
```

---

## Task 9: HTTP handler

**Files:**
- Create: `internal/api/v1/subscription_modification.go`

- [ ] **Step 1: Create the handler**

```go
package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SubscriptionModificationHandler handles API requests for mid-cycle subscription modifications.
type SubscriptionModificationHandler struct {
	modificationService service.SubscriptionModificationService
	log                 *logger.Logger
}

// NewSubscriptionModificationHandler creates a new SubscriptionModificationHandler.
func NewSubscriptionModificationHandler(
	modificationService service.SubscriptionModificationService,
	log *logger.Logger,
) *SubscriptionModificationHandler {
	return &SubscriptionModificationHandler{
		modificationService: modificationService,
		log:                 log,
	}
}

// @Summary Execute subscription modification
// @ID executeSubscriptionModify
// @Description Execute a mid-cycle subscription modification (inheritance or quantity change).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Subscription ID"
// @Param request body dto.ExecuteSubscriptionModifyRequest true "Modification request"
// @Success 200 {object} dto.SubscriptionModifyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/modify/execute [post]
func (h *SubscriptionModificationHandler) Execute(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ExecuteSubscriptionModifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.modificationService.Execute(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Preview subscription modification
// @ID previewSubscriptionModify
// @Description Preview the impact of a mid-cycle subscription modification without committing changes.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Subscription ID"
// @Param request body dto.ExecuteSubscriptionModifyRequest true "Modification preview request"
// @Success 200 {object} dto.SubscriptionModifyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/modify/preview [post]
func (h *SubscriptionModificationHandler) Preview(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ExecuteSubscriptionModifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.modificationService.Preview(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 2: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/api/v1/subscription_modification.go
git commit -m "feat(api): add SubscriptionModificationHandler with Execute and Preview endpoints"
```

---

## Task 10: Wire up FX and router

**Files:**
- Modify: `internal/api/router.go` — add `SubscriptionModification` field to `Handlers` struct, wire routes
- Modify: `cmd/server/main.go` — add to `fx.Provide` and `provideHandlers`

- [ ] **Step 1: Add `SubscriptionModification` to `Handlers` struct**

In `internal/api/router.go`, find the `Handlers` struct (around line 20-50) and add after the `SubscriptionChange` field:

```go
	SubscriptionModification *v1.SubscriptionModificationHandler
```

- [ ] **Step 2: Rewire and add routes**

In `internal/api/router.go`, find the subscription route group (around line 289-293):

Replace:
```go
			subscription.POST("/:id/modify/execute", handlers.Subscription.ExecuteSubscriptionModify)
```

With:
```go
			subscription.POST("/:id/modify/execute", handlers.SubscriptionModification.Execute)
			subscription.POST("/:id/modify/preview", handlers.SubscriptionModification.Preview)
```

- [ ] **Step 3: Add service to FX providers in `cmd/server/main.go`**

In the `fx.Provide(...)` service list (around line 252), add after `service.NewSubscriptionChangeService`:

```go
			service.NewSubscriptionModificationService,
```

- [ ] **Step 4: Add param and handler to `provideHandlers`**

In `provideHandlers` function signature (around line 337), add after `subscriptionChangeService`:

```go
	subscriptionModificationService service.SubscriptionModificationService,
```

In the `api.Handlers{...}` return value (around line 367), add after `SubscriptionChange`:

```go
		SubscriptionModification: v1.NewSubscriptionModificationHandler(subscriptionModificationService, logger),
```

- [ ] **Step 5: Verify build**

```bash
make build
```

Expected: exit 0

- [ ] **Step 6: Run full test suite**

```bash
go test ./internal/... 2>&1 | tail -30
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/router.go cmd/server/main.go
git commit -m "feat(router): wire SubscriptionModificationService and add /modify/preview endpoint"
```

---

## Self-Review Checklist

After all tasks are complete, verify:

1. **Spec coverage:**
   - [x] Fix 1: `ProrationDate` → `item.GetPeriodStart(periodStart)` — Task 2
   - [x] Fix 2: Partial-period guard in `CalculateFixedCharges` — Task 3
   - [x] New DTOs with unified request + Orb-style response — Task 4
   - [x] Dead code removal: DTO, service method, interface, handler, old tests — Task 5
   - [x] `SubscriptionModificationService` interface — Task 6
   - [x] Service implementation: inheritance + quantity change + preview — Task 7
   - [x] Unit tests: inheritance success/duplicate/invalid, quantity change success/end-date/wrong-sub/preview/invalid — Task 8
   - [x] HTTP handler with swagger annotations — Task 9
   - [x] FX wiring + router changes — Task 10

2. **Type consistency:** All references to `dto.ExecuteSubscriptionModifyRequest`, `dto.SubscriptionModifyResponse`, `dto.ChangedResources`, `dto.ChangedLineItem`, `dto.ChangedSubscription`, `dto.ChangedInvoice`, `service.SubscriptionModificationService` are consistent across tasks.

3. **Dead code fully removed:** `ExecuteSubscriptionInheritanceRequest` appears in `subscription.go` (DTO), `subscription.go` (service), `service.go` (interface), `subscription.go` (handler), `subscription_test.go` (tests), and `docs/swagger/docs.go` (regenerated by `make swagger`) — all cleaned up in Task 5 (swagger is regenerated separately).
