package main

import (
	"context"
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runBillingSteps executes Phase 2: Entitlements & Billing Entities.
func (r *SanityRunner) runBillingSteps(ctx context.Context) {
	r.setPhase("PHASE 2: Entitlements & Billing Entities")
	r.printPhaseHeader(r.phase)

	// ── Create Entitlement for Feature A on Plan ────────────────────────
	// SDK: client.Entitlements.CreateEntitlement(ctx, types.DtoCreateEntitlementRequest{...})

	if !r.require(r.planID, "Create Plan", "Create Entitlement (Feature A)") ||
		!r.require(r.featureAID, "Feature A", "Create Entitlement (Feature A)") {
		r.skip("Verify Plan Entitlements", "depends on entitlement creation")
		goto skipTax
	}

	r.run("Create Entitlement (Feature A)", "Entitlements.CreateEntitlement", false, func() error {
		usageReset := types.EntitlementUsageResetPeriodMonthly
		req := types.DtoCreateEntitlementRequest{
			FeatureID:   r.featureAID,
			FeatureType: types.FeatureTypeMetered,
			PlanID:      strPtr(r.planID),
			IsEnabled:   boolPtr(true),
			UsageLimit:  int64Ptr(1000),
			UsageResetPeriod: &usageReset,
			IsSoftLimit: boolPtr(true),
			EntityType:  types.EntitlementEntityTypePlan.ToPointer(),
			EntityID:    strPtr(r.planID),
		}

		resp, err := r.client.Entitlements.CreateEntitlement(ctx, req)
		if err != nil {
			return err
		}
		ent := resp.DtoEntitlementResponse
		if ent == nil || ent.ID == nil {
			return fmt.Errorf("create entitlement returned no body")
		}
		r.entitlementID = *ent.ID
		r.lastResult().EntityID = *ent.ID
		r.lastResult().Details = fmt.Sprintf("ent_id=%s, plan-level, metered, limit=1000 tokens, soft limit", *ent.ID)
		return nil
	})

	// ── Verify Plan Entitlements ────────────────────────────────────────
	// SDK: client.Entitlements via raw HTTP (SDK GetPlanEntitlements may differ)
	// Fallback to raw HTTP for plan entitlements query

	r.run("Verify Plan Entitlements", "Entitlements.GetPlanEntitlements", false, func() error {
		// SDK GetPlanEntitlements has a known bug: Swagger says PlanResponse but backend
		// returns ListEntitlementsResponse, so the SDK deserialises into the wrong shape
		// and always returns 0 entitlements.
		//
		// Strategy:
		//   1. Try SDK GetPlanEntitlements (the "correct" method).
		//   2. If it returns 0 entitlements, try SDK QueryEntitlement with entity filter
		//      (uses the right response type).
		//   3. If that also fails, fall back to raw HTTP.

		// ── Attempt 1: SDK GetPlanEntitlements ───────────────────────────
		sdkErr := func() error {
			resp, err := r.client.Entitlements.GetPlanEntitlements(ctx, r.planID)
			if err != nil {
				return fmt.Errorf("SDK GetPlanEntitlements call failed: %w", err)
			}
			plan := resp.DtoPlanResponse
			if plan == nil {
				return fmt.Errorf("SDK GetPlanEntitlements returned nil body")
			}
			if len(plan.Entitlements) == 0 {
				return fmt.Errorf("SDK GetPlanEntitlements returned 0 entitlements (Swagger annotation mismatch: backend returns ListEntitlementsResponse but SDK expects PlanResponse)")
			}
			for _, ent := range plan.Entitlements {
				if ent.FeatureID != nil && *ent.FeatureID == r.featureAID {
					limit := int64(0)
					if ent.UsageLimit != nil {
						limit = *ent.UsageLimit
					}
					r.lastResult().Details = fmt.Sprintf("found Feature A entitlement via GetPlanEntitlements, limit=%d", limit)
					return nil
				}
			}
			return fmt.Errorf("Feature A not found in %d entitlements", len(plan.Entitlements))
		}()
		if sdkErr == nil {
			return nil
		}

		// ── Attempt 2: SDK QueryEntitlement with entity filter ───────────
		r.markSDKFallback("Entitlements.GetPlanEntitlements", sdkErr)

		queryErr := func() error {
			entityType := types.EntitlementEntityTypePlan
			status := types.StatusPublished
			filter := types.EntitlementFilter{
				EntityIds:  []string{r.planID},
				EntityType: &entityType,
				Status:     &status,
			}
			resp, err := r.client.Entitlements.QueryEntitlement(ctx, filter)
			if err != nil {
				return fmt.Errorf("SDK QueryEntitlement failed: %w", err)
			}
			list := resp.DtoListEntitlementsResponse
			if list == nil || len(list.Items) == 0 {
				return fmt.Errorf("SDK QueryEntitlement returned 0 entitlements for plan %s", r.planID)
			}
			for _, ent := range list.Items {
				if ent.FeatureID != nil && *ent.FeatureID == r.featureAID {
					limit := int64(0)
					if ent.UsageLimit != nil {
						limit = *ent.UsageLimit
					}
					r.lastResult().Details += fmt.Sprintf("\n        → found Feature A entitlement via QueryEntitlement (fallback), limit=%d", limit)
					return nil
				}
			}
			return fmt.Errorf("Feature A not found in %d entitlements via QueryEntitlement", len(list.Items))
		}()
		if queryErr == nil {
			return nil
		}

		// ── Attempt 3: raw HTTP ──────────────────────────────────────────
		rawResp, _, err := r.raw.Get(ctx, fmt.Sprintf("/plans/%s/entitlements", r.planID))
		if err != nil {
			return fmt.Errorf("all 3 attempts failed — SDK GetPlanEntitlements: %v | SDK QueryEntitlement: %v | raw HTTP: %w", sdkErr, queryErr, err)
		}

		items := getSlice(rawResp, "items")
		if len(items) == 0 {
			return fmt.Errorf("all 3 attempts returned 0 entitlements for plan %s — SDK GetPlanEntitlements: %v | SDK QueryEntitlement: %v", r.planID, sdkErr, queryErr)
		}

		found := false
		for _, item := range items {
			if ent, ok := item.(map[string]interface{}); ok {
				if getString(ent, "feature_id") == r.featureAID {
					found = true
					limit := getFloat(ent, "usage_limit")
					r.lastResult().Details += fmt.Sprintf("\n        → found Feature A entitlement via raw HTTP (2nd fallback), limit=%.0f", limit)
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("entitlement for Feature A (%s) not found on plan via any method", r.featureAID)
		}
		return nil
	})

skipTax:
	// ── Create Tax Rate ─────────────────────────────────────────────────
	// SDK: client.TaxRates.CreateTaxRate(ctx, types.DtoCreateTaxRateRequest{...})

	r.run("Create Tax Rate (18% GST)", "TaxRates.CreateTaxRate", false, func() error {
		taxCode := fmt.Sprintf("GST18_%d", ts())
		taxRateType := types.TaxRateTypePercentage
		scope := types.TaxRateScopeInternal

		req := types.DtoCreateTaxRateRequest{
			Name:            fmt.Sprintf("GST 18%% %d", ts()),
			Code:            taxCode,
			PercentageValue: strPtr("18.00"),
			TaxRateType:     &taxRateType,
			Description:     strPtr("Goods and Services Tax"),
			Scope:           &scope,
		}

		resp, err := r.client.TaxRates.CreateTaxRate(ctx, req)
		if err != nil {
			return err
		}
		taxRate := resp.DtoTaxRateResponse
		if taxRate == nil || taxRate.ID == nil {
			return fmt.Errorf("create tax rate returned no body")
		}
		r.taxRateID = *taxRate.ID
		r.taxRateCode = taxCode
		r.lastResult().EntityID = *taxRate.ID
		r.lastResult().Details = fmt.Sprintf("tax_id=%s, code=%s, rate=18%%", *taxRate.ID, taxCode)
		return nil
	})

	// ── Create Coupon ───────────────────────────────────────────────────
	// SDK: client.Coupons.CreateCoupon(ctx, types.DtoCreateCouponRequest{...})

	r.run("Create Coupon (10% off)", "Coupons.CreateCoupon", false, func() error {
		req := types.DtoCreateCouponRequest{
			Name:          fmt.Sprintf("Sanity 10pct Off %d", ts()),
			Type:          types.CouponTypePercentage,
			Cadence:       types.CouponCadenceOnce,
			PercentageOff: strPtr("10"),
		}

		resp, err := r.client.Coupons.CreateCoupon(ctx, req)
		if err != nil {
			return err
		}
		coupon := resp.DtoCouponResponse
		if coupon == nil || coupon.ID == nil {
			return fmt.Errorf("create coupon returned no body")
		}
		r.couponID = *coupon.ID
		r.lastResult().EntityID = *coupon.ID
		r.lastResult().Details = fmt.Sprintf("coupon_id=%s, type=percentage, 10%% off", *coupon.ID)
		return nil
	})
}
