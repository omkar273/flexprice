package main

import (
	"context"
	"fmt"
)

// runBillingSteps executes Phase 2: Entitlements & Billing Entities.
func (r *SanityRunner) runBillingSteps(ctx context.Context) {
	r.setPhase("PHASE 2: Entitlements & Billing Entities")
	r.printPhaseHeader(r.phase)

	// ── Create Entitlement for Feature A on Plan ────────────────────────
	// SDK: client.Entitlements.CreateEntitlement(ctx, types.DtoCreateEntitlementRequest{...})
	// API: POST /v1/entitlements

	if !r.require(r.planID, "Create Plan", "Create Entitlement (Feature A)") ||
		!r.require(r.featureAID, "Feature A", "Create Entitlement (Feature A)") {
		r.skip("Verify Plan Entitlements", "depends on entitlement creation")
		goto skipTax
	}

	r.run("Create Entitlement (Feature A)", "Entitlements.CreateEntitlement", false, func() error {
		body := map[string]interface{}{
			"plan_id":            r.planID,
			"feature_id":        r.featureAID,
			"feature_type":      "metered",
			"is_enabled":        true,
			"usage_limit":       1000,
			"usage_reset_period": "MONTHLY",
			"is_soft_limit":     true,
			"entity_type":       "PLAN",
			"entity_id":         r.planID,
		}

		resp, _, err := r.raw.Post(ctx, "/entitlements", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in entitlement response: %v", resp)
		}
		r.entitlementID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("ent_id=%s, plan-level, metered, limit=1000 tokens, soft limit", id)
		return nil
	})

	// ── Verify Plan Entitlements ────────────────────────────────────────
	// SDK: client.Plans.GetPlanEntitlements(ctx, planID)
	// API: GET /v1/plans/:id/entitlements

	r.run("Verify Plan Entitlements", "Plans.GetPlanEntitlements", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/plans/%s/entitlements", r.planID))
		if err != nil {
			return err
		}

		items := getSlice(resp, "items")
		if len(items) == 0 {
			return fmt.Errorf("expected at least 1 entitlement on plan, got 0")
		}

		// Verify Feature A entitlement is present.
		found := false
		for _, item := range items {
			if ent, ok := item.(map[string]interface{}); ok {
				if getString(ent, "feature_id") == r.featureAID {
					found = true
					limit := getFloat(ent, "usage_limit")
					r.lastResult().Details = fmt.Sprintf("found Feature A entitlement, limit=%.0f", limit)
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("entitlement for Feature A (%s) not found on plan", r.featureAID)
		}
		return nil
	})

skipTax:
	// ── Create Tax Rate ─────────────────────────────────────────────────
	// SDK: client.TaxRates.CreateTaxRate(ctx, req)   [missing from SDK]
	// API: POST /v1/taxes/rates

	r.run("Create Tax Rate (18% GST)", "TaxRates.CreateTaxRate", true, func() error {
		taxCode := fmt.Sprintf("GST18_%d", ts())
		body := map[string]interface{}{
			"name":             fmt.Sprintf("GST 18%% %d", ts()),
			"code":             taxCode,
			"percentage_value": "18.00",
			"tax_rate_type":    "percentage",
			"description":      "Goods and Services Tax",
			"scope":            "INTERNAL",
		}
		resp, _, err := r.raw.Post(ctx, "/taxes/rates", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in tax rate response: %v", resp)
		}
		r.taxRateID = id
		r.taxRateCode = taxCode
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("tax_id=%s, code=%s, rate=18%%", id, taxCode)
		return nil
	})

	// ── Create Coupon ───────────────────────────────────────────────────
	// SDK: client.Coupons.CreateCoupon(ctx, req)   [missing from SDK]
	// API: POST /v1/coupons

	r.run("Create Coupon (10% off)", "Coupons.CreateCoupon", true, func() error {
		body := map[string]interface{}{
			"name":           fmt.Sprintf("Sanity 10pct Off %d", ts()),
			"type":           "percentage",
			"cadence":        "once",
			"percentage_off": "10",
		}
		resp, _, err := r.raw.Post(ctx, "/coupons", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in coupon response: %v", resp)
		}
		r.couponID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("coupon_id=%s, type=percentage, 10%% off", id)
		return nil
	})
}
