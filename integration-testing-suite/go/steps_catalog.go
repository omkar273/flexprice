package main

import (
	"context"
	"fmt"
)

// runCatalogSteps executes Phase 1: Product Catalog.
func (r *SanityRunner) runCatalogSteps(ctx context.Context) {
	r.setPhase("PHASE 1: Product Catalog")
	r.printPhaseHeader(r.phase)

	// ── Create Feature Group ────────────────────────────────────────────
	// SDK: client.Groups.CreateGroup(ctx, req)   [missing from SDK]
	// API: POST /v1/groups

	r.run("Create Feature Group", "Groups.CreateGroup", true, func() error {
		body := map[string]interface{}{
			"name":        fmt.Sprintf("sanity-feature-group-%d", ts()),
			"entity_type": "feature",
			"lookup_key":  fmt.Sprintf("sanity_feat_grp_%d", ts()),
		}
		resp, _, err := r.raw.Post(ctx, "/groups", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in response: %v", resp)
		}
		r.featureGroupID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("group_id=%s, entity_type=feature", id)
		return nil
	})

	// ── Create Price Group ──────────────────────────────────────────────
	// SDK: client.Groups.CreateGroup(ctx, req)   [missing from SDK]
	// API: POST /v1/groups

	r.run("Create Price Group", "Groups.CreateGroup", true, func() error {
		body := map[string]interface{}{
			"name":        fmt.Sprintf("sanity-price-group-%d", ts()),
			"entity_type": "price",
			"lookup_key":  fmt.Sprintf("sanity_price_grp_%d", ts()),
		}
		resp, _, err := r.raw.Post(ctx, "/groups", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in response: %v", resp)
		}
		r.priceGroupID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("group_id=%s, entity_type=price", id)
		return nil
	})

	// ── Create Metered Feature A (grouped) ──────────────────────────────
	// SDK: client.Features.CreateFeature(ctx, types.DtoCreateFeatureRequest{...})
	// API: POST /v1/features

	r.run("Create Metered Feature A (grouped)", "Features.CreateFeature", false, func() error {
		r.eventNameA = fmt.Sprintf("api_call_%d", ts())
		body := map[string]interface{}{
			"name":       fmt.Sprintf("API Calls %d", ts()),
			"type":       "metered",
			"lookup_key": fmt.Sprintf("api_calls_%d", ts()),
			"meter": map[string]interface{}{
				"name":       fmt.Sprintf("api_call_meter_%d", ts()),
				"event_name": r.eventNameA,
				"aggregation": map[string]interface{}{
					"type":  "SUM",
					"field": "tokens",
				},
				"reset_usage": "BILLING_PERIOD",
			},
			"group_id": r.featureGroupID,
			"metadata": map[string]string{"source": "sanity_test"},
		}

		resp, _, err := r.raw.Post(ctx, "/features", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in feature response: %v", resp)
		}
		r.featureAID = id
		r.lastResult().EntityID = id

		// Extract meter ID.
		if meterID := getString(resp, "meter_id"); meterID != "" {
			r.meterAID = meterID
		} else if meter := getMap(resp, "meter"); meter != nil {
			r.meterAID = getString(meter, "id")
		}

		r.lastResult().Details = fmt.Sprintf("feat_id=%s, meter_id=%s, event=%s", id, r.meterAID, r.eventNameA)
		return nil
	})

	// ── Create Metered Feature B (ungrouped) ────────────────────────────
	// SDK: client.Features.CreateFeature(ctx, types.DtoCreateFeatureRequest{...})
	// API: POST /v1/features

	r.run("Create Metered Feature B", "Features.CreateFeature", false, func() error {
		r.eventNameB = fmt.Sprintf("storage_usage_%d", ts())
		body := map[string]interface{}{
			"name":       fmt.Sprintf("Storage Usage %d", ts()),
			"type":       "metered",
			"lookup_key": fmt.Sprintf("storage_usage_%d", ts()),
			"meter": map[string]interface{}{
				"name":       fmt.Sprintf("storage_meter_%d", ts()),
				"event_name": r.eventNameB,
				"aggregation": map[string]interface{}{
					"type":  "SUM",
					"field": "gb_hours",
				},
				"reset_usage": "BILLING_PERIOD",
			},
			"metadata": map[string]string{"source": "sanity_test"},
		}

		resp, _, err := r.raw.Post(ctx, "/features", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in feature response: %v", resp)
		}
		r.featureBID = id
		r.lastResult().EntityID = id

		if meterID := getString(resp, "meter_id"); meterID != "" {
			r.meterBID = meterID
		} else if meter := getMap(resp, "meter"); meter != nil {
			r.meterBID = getString(meter, "id")
		}

		r.lastResult().Details = fmt.Sprintf("feat_id=%s, meter_id=%s, event=%s", id, r.meterBID, r.eventNameB)
		return nil
	})

	// ── Create Plan ─────────────────────────────────────────────────────
	// SDK: client.Plans.CreatePlan(ctx, types.DtoCreatePlanRequest{...})
	// API: POST /v1/plans

	r.run("Create Plan", "Plans.CreatePlan", false, func() error {
		body := map[string]interface{}{
			"name":        fmt.Sprintf("Sanity Plan %d", ts()),
			"lookup_key":  fmt.Sprintf("sanity_plan_%d", ts()),
			"description": "Integration test plan with recurring + usage charges",
		}

		resp, _, err := r.raw.Post(ctx, "/plans", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in plan response: %v", resp)
		}
		r.planID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("plan_id=%s, name=%s", id, getString(resp, "name"))
		return nil
	})

	// ── Add Recurring Price 1 (grouped) ─────────────────────────────────
	// SDK: client.Prices.CreatePrice(ctx, types.DtoCreatePriceRequest{...})
	// API: POST /v1/prices

	if !r.require(r.planID, "Create Plan", "Add Recurring Price 1 (grouped)") {
		r.skip("Add Recurring Price 2", "depends on Create Plan which failed")
		r.skip("Add Usage Price (Feature A)", "depends on Create Plan which failed")
		r.skip("Add Usage Price (Feature B)", "depends on Create Plan which failed")
		return
	}

	r.run("Add Recurring Price 1 (grouped)", "Prices.CreatePrice", false, func() error {
		body := map[string]interface{}{
			"entity_id":          r.planID,
			"entity_type":        "PLAN",
			"type":               "FIXED",
			"billing_model":      "FLAT_FEE",
			"billing_cadence":    "RECURRING",
			"billing_period":     "MONTHLY",
			"billing_period_count": 1,
			"invoice_cadence":    "ARREAR",
			"price_unit_type":    "FIAT",
			"amount":             "49.99",
			"currency":           "USD",
			"display_name":       "Platform Fee (Grouped)",
			"group_id":           r.priceGroupID,
		}

		resp, _, err := r.raw.Post(ctx, "/prices", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in price response: %v", resp)
		}
		r.priceRecurr1 = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("price_id=%s, amount=$49.99, grouped, plan=%s", id, r.planID)
		return nil
	})

	// ── Add Recurring Price 2 (ungrouped) ───────────────────────────────

	r.run("Add Recurring Price 2", "Prices.CreatePrice", false, func() error {
		body := map[string]interface{}{
			"entity_id":          r.planID,
			"entity_type":        "PLAN",
			"type":               "FIXED",
			"billing_model":      "FLAT_FEE",
			"billing_cadence":    "RECURRING",
			"billing_period":     "MONTHLY",
			"billing_period_count": 1,
			"invoice_cadence":    "ARREAR",
			"price_unit_type":    "FIAT",
			"amount":             "19.99",
			"currency":           "USD",
			"display_name":       "Base Fee",
		}

		resp, _, err := r.raw.Post(ctx, "/prices", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in price response: %v", resp)
		}
		r.priceRecurr2 = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("price_id=%s, amount=$19.99, ungrouped, plan=%s", id, r.planID)
		return nil
	})

	// ── Add Usage Price for Feature A ────────────────────────────────────

	if !r.require(r.meterAID, "Feature A meter", "Add Usage Price (Feature A)") {
		r.skip("Add Usage Price (Feature B)", "depends on prior steps")
		return
	}

	r.run("Add Usage Price (Feature A)", "Prices.CreatePrice", false, func() error {
		body := map[string]interface{}{
			"entity_id":          r.planID,
			"entity_type":        "PLAN",
			"type":               "USAGE",
			"billing_model":      "FLAT_FEE",
			"billing_cadence":    "RECURRING",
			"billing_period":     "MONTHLY",
			"billing_period_count": 1,
			"invoice_cadence":    "ARREAR",
			"price_unit_type":    "FIAT",
			"amount":             "0.01",
			"currency":           "USD",
			"meter_id":           r.meterAID,
			"display_name":       "API Call Usage",
		}

		resp, _, err := r.raw.Post(ctx, "/prices", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in price response: %v", resp)
		}
		r.priceUsageA = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("price_id=%s, per_unit=$0.01/token, meter=%s", id, r.meterAID)
		return nil
	})

	// ── Add Usage Price for Feature B ────────────────────────────────────

	if !r.require(r.meterBID, "Feature B meter", "Add Usage Price (Feature B)") {
		return
	}

	r.run("Add Usage Price (Feature B)", "Prices.CreatePrice", false, func() error {
		body := map[string]interface{}{
			"entity_id":          r.planID,
			"entity_type":        "PLAN",
			"type":               "USAGE",
			"billing_model":      "FLAT_FEE",
			"billing_cadence":    "RECURRING",
			"billing_period":     "MONTHLY",
			"billing_period_count": 1,
			"invoice_cadence":    "ARREAR",
			"price_unit_type":    "FIAT",
			"amount":             "0.05",
			"currency":           "USD",
			"meter_id":           r.meterBID,
			"display_name":       "Storage Usage",
		}

		resp, _, err := r.raw.Post(ctx, "/prices", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in price response: %v", resp)
		}
		r.priceUsageB = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("price_id=%s, per_unit=$0.05/gb_hour, meter=%s", id, r.meterBID)
		return nil
	})
}
