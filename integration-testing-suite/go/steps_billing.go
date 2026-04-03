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
		resp, err := r.client.Entitlements.GetPlanEntitlements(ctx, r.planID)
		if err != nil {
			return err
		}
		plan := resp.DtoPlanResponse
		if plan == nil {
			return fmt.Errorf("get plan entitlements returned no body")
		}

		entitlements := plan.Entitlements
		if len(entitlements) == 0 {
			return fmt.Errorf("expected at least 1 entitlement on plan, got 0")
		}

		// Verify Feature A entitlement is present.
		found := false
		for _, ent := range entitlements {
			if ent.FeatureID != nil && *ent.FeatureID == r.featureAID {
				found = true
				limit := int64(0)
				if ent.UsageLimit != nil {
					limit = *ent.UsageLimit
				}
				r.lastResult().Details = fmt.Sprintf("found Feature A entitlement, limit=%d", limit)
				break
			}
		}
		if !found {
			return fmt.Errorf("entitlement for Feature A (%s) not found on plan", r.featureAID)
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
