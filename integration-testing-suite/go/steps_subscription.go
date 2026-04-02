package main

import (
	"context"
	"fmt"
	"time"
)

// runSubscriptionSteps executes Phase 3: Customer & Subscription.
func (r *SanityRunner) runSubscriptionSteps(ctx context.Context) {
	r.setPhase("PHASE 3: Customer & Subscription")
	r.printPhaseHeader(r.phase)

	// ── Create Customer ─────────────────────────────────────────────────
	// SDK: client.Customers.CreateCustomer(ctx, types.DtoCreateCustomerRequest{...})
	// API: POST /v1/customers

	r.run("Create Customer", "Customers.CreateCustomer", false, func() error {
		r.externalCustID = fmt.Sprintf("sanity-cust-%d", ts())

		body := map[string]interface{}{
			"external_id": r.externalCustID,
			"name":        fmt.Sprintf("Sanity Test Customer %d", ts()),
			"email":       fmt.Sprintf("sanity-%d@test.flexprice.io", ts()),
			"metadata":    map[string]string{"source": "sanity_test"},
		}

		resp, _, err := r.raw.Post(ctx, "/customers", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in customer response: %v", resp)
		}
		r.customerID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("cust_id=%s, external_id=%s", id, r.externalCustID)
		return nil
	})

	// ── Create Subscription ─────────────────────────────────────────────
	// SDK: client.Subscriptions.CreateSubscription(ctx, types.DtoCreateSubscriptionRequest{...})
	// API: POST /v1/subscriptions

	if !r.require(r.customerID, "Create Customer", "Create Subscription") ||
		!r.require(r.planID, "Create Plan", "Create Subscription") {
		r.skip("Verify Subscription Active", "depends on Create Subscription")
		r.skip("Verify Subscription Entitlements", "depends on Create Subscription")
		r.skip("Verify Customer Entitlements", "depends on Create Subscription")
		return
	}

	r.run("Create Subscription", "Subscriptions.CreateSubscription", false, func() error {
		startDate := time.Now().Format(time.RFC3339)

		body := map[string]interface{}{
			"customer_id":        r.customerID,
			"plan_id":            r.planID,
			"currency":           "usd",
			"billing_cadence":    "RECURRING",
			"billing_period":     "MONTHLY",
			"billing_period_count": 1,
			"billing_cycle":      "anniversary",
			"start_date":         startDate,
			"metadata":           map[string]string{"source": "sanity_test"},
		}

		// Attach coupon if available.
		if r.couponID != "" {
			body["coupons"] = []string{r.couponID}
		}

		// Attach tax rate override if available (uses code + currency, both required).
		if r.taxRateCode != "" {
			body["tax_rate_overrides"] = []map[string]interface{}{
				{
					"tax_rate_code": r.taxRateCode,
					"currency":      "usd",
					"auto_apply":    true,
					"priority":      1,
				},
			}
		}

		resp, _, err := r.raw.Post(ctx, "/subscriptions", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in subscription response: %v", resp)
		}
		r.subscriptionID = id
		r.lastResult().EntityID = id

		details := fmt.Sprintf("sub_id=%s, plan=%s, customer=%s", id, r.planID, r.customerID)
		if r.couponID != "" {
			details += ", coupon=" + r.couponID
		}
		if r.taxRateID != "" {
			details += ", tax=" + r.taxRateCode
		}
		r.lastResult().Details = details
		return nil
	})

	// ── Verify Subscription Active ──────────────────────────────────────
	// SDK: client.Subscriptions.GetSubscription(ctx, subID)
	// SDK: client.Subscriptions.ActivateSubscription(ctx, subID, req) if DRAFT
	// API: GET /v1/subscriptions/:id, POST /v1/subscriptions/:id/activate

	if !r.require(r.subscriptionID, "Create Subscription", "Verify Subscription Active") {
		r.skip("Verify Subscription Entitlements", "depends on subscription")
		r.skip("Verify Customer Entitlements", "depends on subscription")
		return
	}

	r.run("Verify Subscription Active", "Subscriptions.GetSubscription", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/subscriptions/%s", r.subscriptionID))
		if err != nil {
			return err
		}

		status := getString(resp, "subscription_status")
		if status == "" {
			status = getString(resp, "status")
		}

		// If DRAFT, activate it.
		if status == "draft" || status == "DRAFT" {
			_, _, err := r.raw.Post(ctx, fmt.Sprintf("/subscriptions/%s/activate", r.subscriptionID), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("activate subscription: %w", err)
			}
			// Re-fetch to confirm.
			resp, _, err = r.raw.Get(ctx, fmt.Sprintf("/subscriptions/%s", r.subscriptionID))
			if err != nil {
				return fmt.Errorf("re-fetch after activate: %w", err)
			}
			status = getString(resp, "subscription_status")
			if status == "" {
				status = getString(resp, "status")
			}
		}

		if status != "active" && status != "ACTIVE" {
			return fmt.Errorf("expected subscription status ACTIVE, got %s", status)
		}

		r.lastResult().Details = "status=ACTIVE"
		return nil
	})

	// ── Verify Subscription Entitlements ─────────────────────────────────
	// SDK: client.Subscriptions.GetSubscriptionEntitlements(ctx, subID)
	// API: GET /v1/subscriptions/:id/entitlements

	r.run("Verify Subscription Entitlements", "Subscriptions.GetSubscriptionEntitlements", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/subscriptions/%s/entitlements", r.subscriptionID))
		if err != nil {
			return err
		}

		// Response shape: { "features": [ { "feature": {...}, "entitlement": {...}, "sources": [...] } ] }
		features := getSlice(resp, "features")
		if len(features) == 0 {
			return fmt.Errorf("expected at least 1 entitlement on subscription, got 0")
		}

		found := false
		for _, item := range features {
			if af, ok := item.(map[string]interface{}); ok {
				feat := getMap(af, "feature")
				if feat != nil && getString(feat, "id") == r.featureAID {
					found = true
					ent := getMap(af, "entitlement")
					if ent != nil {
						limit := getFloat(ent, "usage_limit")
						r.lastResult().Details = fmt.Sprintf("Feature A entitlement found, limit=%.0f", limit)
					} else {
						r.lastResult().Details = "Feature A found but entitlement is nil"
					}
					break
				}
			}
		}
		if !found {
			r.lastResult().Details = fmt.Sprintf("features count=%d, looking for feature_id=%s", len(features), r.featureAID)
			return fmt.Errorf("Feature A entitlement not inherited to subscription")
		}
		return nil
	})

	// ── Verify Customer Entitlements ────────────────────────────────────
	// SDK: client.Customers.GetCustomerEntitlements(ctx, customerID)
	// API: GET /v1/customers/:id/entitlements

	r.run("Verify Customer Entitlements", "Customers.GetCustomerEntitlements", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/customers/%s/entitlements", r.customerID))
		if err != nil {
			return err
		}

		// Response shape: { "customer_id": "...", "features": [ { "feature": {...}, "entitlement": {...} } ] }
		features := getSlice(resp, "features")
		if len(features) == 0 {
			return fmt.Errorf("expected at least 1 customer entitlement, got 0")
		}

		found := false
		for _, item := range features {
			if af, ok := item.(map[string]interface{}); ok {
				feat := getMap(af, "feature")
				if feat != nil && getString(feat, "id") == r.featureAID {
					found = true
					r.lastResult().Details = "Feature A entitlement visible on customer"
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("Feature A entitlement not visible on customer")
		}
		return nil
	})
}
