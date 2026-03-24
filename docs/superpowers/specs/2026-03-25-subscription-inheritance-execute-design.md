# Subscription Inheritance Execute API — Design Spec

## Goal

Two changes to the subscription system:
1. Replace flat inheritance fields on `CreateSubscriptionRequest` with a single nested `inheritance` object.
2. Add a new `POST /subscriptions/:id/inheritance/execute` endpoint that adds new child customers to an already-existing parent subscription.

---

## Background

When creating a parent subscription today, inheritance is declared via flat top-level fields:
- `external_customer_ids_to_inherit_subscription []string`
- `customer_ids_to_inherit_subscription []string`
- `invoicing_customer_id *string`
- `invoicing_customer_external_id *string`
- `parent_subscription_id *string`

These are removed and replaced with a single `inheritance` object. The API is internal, so no backwards-compatibility shim is needed.

After a parent subscription is created, there is currently no way to add new children without recreating the subscription. The new execute endpoint fills this gap.

---

## Change 1: Restructure `CreateSubscriptionRequest`

### New DTO

**File:** `internal/api/dto/subscription.go`

Add a new struct:

```go
// SubscriptionInheritanceConfig groups all inheritance-related fields for
// subscription creation and the inheritance execute endpoint.
type SubscriptionInheritanceConfig struct {
    CustomerIDsToInheritSubscription         []string `json:"customer_ids_to_inherit_subscription,omitempty"`
    ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
    ParentSubscriptionID                     string   `json:"parent_subscription_id,omitempty"`
    InvoicingCustomerID                      string   `json:"invoicing_customer_id,omitempty"`
    InvoicingCustomerExternalID              string   `json:"invoicing_customer_external_id,omitempty"`
}
```

Replace the five flat fields on `CreateSubscriptionRequest` with:

```go
Inheritance *SubscriptionInheritanceConfig `json:"inheritance,omitempty"`
```

### Service changes

**File:** `internal/service/subscription.go`

`resolveUsageCustomerIDs(ctx, req)` reads from `req.Inheritance` instead of flat fields:

```go
if req.Inheritance == nil {
    return nil, nil // STANDALONE
}
if len(req.Inheritance.CustomerIDsToInheritSubscription) > 0 {
    // use internal IDs directly (existing logic)
}
if len(req.Inheritance.ExternalCustomerIDsToInheritSubscription) > 0 {
    // resolve external → internal (existing logic)
}
return nil, nil
```

All other logic in `CreateSubscription` (type resolution, validation, `createInheritedSubscriptions` call) is unchanged — only the field access path changes.

`InvoicingCustomerID` and `InvoicingCustomerExternalID` are also read from `req.Inheritance` wherever referenced in `CreateSubscription`.

`ParentSubscriptionID` on the request is read from `req.Inheritance.ParentSubscriptionID`.

---

## Change 2: `POST /subscriptions/:id/inheritance/execute`

### Purpose

Adds new child customers to an already-existing **parent** subscription. Idempotent: customers who already have an inherited subscription under this parent are silently skipped.

### Request

```go
// ExecuteSubscriptionInheritanceRequest is the payload for POST /subscriptions/:id/inheritance/execute.
// Exactly one of the two arrays must be non-empty.
type ExecuteSubscriptionInheritanceRequest struct {
    CustomerIDsToInheritSubscription         []string `json:"customer_ids_to_inherit_subscription,omitempty"`
    ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
}
```

**Validation (returns 400):**
- Both arrays provided and non-empty → `"provide either customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription, not both"`
- Both arrays empty or absent → `"at least one customer ID is required"`

### Response

```json
{
  "id": "subs_...",
  "subscription_type": "parent",
  "...": "full subscription object"
}
```

Returns the (unchanged) parent subscription. The created inherited subscriptions are accessible via `GET /subscriptions?parent_subscription_id=...`.

### Execution logic

1. Fetch parent subscription by `:id`. Return 404 if not found.
2. Validate `subscription_type == "parent"`. Return 400 if STANDALONE or INHERITED.
3. Resolve customer IDs:
   - If `ExternalCustomerIDsToInheritSubscription` provided: resolve external → internal via `CustomerRepo`. Return error if any not found.
   - If `CustomerIDsToInheritSubscription` provided: validate all exist. Return error if any not found.
4. Fetch existing inherited subscriptions for this parent via `getInheritedSubscriptions(ctx, parentSubID)`.
5. Build a set of already-inherited customer IDs. Filter the resolved list — remove any customer already present in the set.
6. If the filtered list is empty → return parent subscription immediately (nothing to do).
7. Run `validateCustomerSubscriptionWorkflow(ctx, parentSub.CustomerID, filteredCustomerIDs)` on the filtered list to catch STANDALONE conflicts.
8. Call `createInheritedSubscriptions(ctx, parentSub, filteredCustomerIDs)`.
9. Fetch and return the updated parent subscription.

### Handler

Added to the existing `SubscriptionHandler` (not a new handler struct) to avoid DI changes.

**File:** `internal/api/v1/subscription.go`

```go
func (h *SubscriptionHandler) ExecuteSubscriptionInheritance(c *gin.Context) {
    id := c.Param("id")
    var req dto.ExecuteSubscriptionInheritanceRequest
    if err := c.ShouldBindJSON(&req); err != nil { ... }
    resp, err := h.service.ExecuteSubscriptionInheritance(c.Request.Context(), id, &req)
    if err != nil { c.Error(err); return }
    c.JSON(http.StatusOK, resp)
}
```

### Service method

**File:** `internal/service/subscription.go`

Added to `subscriptionService` (implements `SubscriptionService` interface).

```go
func (s *subscriptionService) ExecuteSubscriptionInheritance(
    ctx context.Context,
    parentSubID string,
    req *dto.ExecuteSubscriptionInheritanceRequest,
) (*subscription.Subscription, error)
```

Interface updated in `internal/service/subscription.go` (the `SubscriptionService` interface).

### Route registration

**File:** `internal/api/router.go`

```go
subscription.POST("/:id/inheritance/execute", handlers.Subscription.ExecuteSubscriptionInheritance)
```

---

## Files Changed

| File | Change |
|------|--------|
| `internal/api/dto/subscription.go` | Add `SubscriptionInheritanceConfig`, `ExecuteSubscriptionInheritanceRequest`; replace flat fields on `CreateSubscriptionRequest` with `Inheritance *SubscriptionInheritanceConfig` |
| `internal/service/subscription.go` | Update `resolveUsageCustomerIDs` to read from `req.Inheritance`; update all other flat-field reads; add `ExecuteSubscriptionInheritance` method; update `SubscriptionService` interface |
| `internal/api/v1/subscription.go` | Add `ExecuteSubscriptionInheritance` handler |
| `internal/api/router.go` | Register new route |

---

## Error Cases

| Scenario | HTTP | Message |
|----------|------|---------|
| Both arrays provided | 400 | provide either … not both |
| Both arrays empty | 400 | at least one customer ID is required |
| Parent sub not found | 404 | subscription not found |
| Sub is not PARENT type | 400 | subscription is not a parent subscription |
| Customer ID not found | 400 | customer not found: `<id>` |
| STANDALONE conflict | 400 | existing hierarchy conflict error (from validateCustomerSubscriptionWorkflow) |
| All customers already inherited | 200 | returns parent sub, no-op |

---

## What Is NOT Changed

- `createInheritedSubscriptions` — reused as-is
- `validateCustomerSubscriptionWorkflow` — reused as-is
- `getInheritedSubscriptions` — reused as-is
- Cascade cancel/pause/resume — unaffected
- No new DB migrations needed
