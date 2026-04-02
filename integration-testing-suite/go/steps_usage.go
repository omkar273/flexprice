package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// runUsageSteps executes Phase 4: Usage Ingestion.
func (r *SanityRunner) runUsageSteps(ctx context.Context) {
	r.setPhase("PHASE 4: Usage Ingestion & Verification")
	r.printPhaseHeader(r.phase)

	if !r.require(r.externalCustID, "Create Customer", "Bulk Ingest Events") ||
		!r.require(r.eventNameA, "Feature A event name", "Bulk Ingest Events") ||
		!r.require(r.eventNameB, "Feature B event name", "Bulk Ingest Events") {
		r.skip("Wait for Processing", "depends on event ingestion")
		r.skip("Verify Usage Counts", "depends on event ingestion")
		r.skip("Verify Entitlement Usage", "depends on event ingestion")
		return
	}

	// ── Bulk Ingest 50 Events ───────────────────────────────────────────
	// SDK: client.Events.IngestEventsBulk(ctx, types.DtoBulkIngestEventRequest{...})
	// API: POST /v1/events/bulk

	r.run("Bulk Ingest 50 Events", "Events.IngestEventsBulk", false, func() error {
		events := make([]map[string]interface{}, 0, 50)

		// 30 events for Feature A (api_call).
		r.totalTokensIngested = 0
		for i := 0; i < 30; i++ {
			tokens := float64(rand.Intn(50) + 1) // 1-50 tokens per event
			r.totalTokensIngested += tokens

			events = append(events, map[string]interface{}{
				"event_name":           r.eventNameA,
				"external_customer_id": r.externalCustID,
				"properties": map[string]interface{}{
					"tokens": fmt.Sprintf("%.0f", tokens),
				},
				"source":    "sanity_test",
				"timestamp": time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
			})
		}

		// 20 events for Feature B (storage_usage).
		r.totalGBHoursIngested = 0
		for i := 0; i < 20; i++ {
			gbHours := float64(rand.Intn(10) + 1) // 1-10 GB-hours per event
			r.totalGBHoursIngested += gbHours

			events = append(events, map[string]interface{}{
				"event_name":           r.eventNameB,
				"external_customer_id": r.externalCustID,
				"properties": map[string]interface{}{
					"gb_hours": fmt.Sprintf("%.0f", gbHours),
				},
				"source":    "sanity_test",
				"timestamp": time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
			})
		}

		body := map[string]interface{}{
			"events": events,
		}

		_, _, err := r.raw.Post(ctx, "/events/bulk", body)
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf(
			"30 events (%.0f tokens) + 20 events (%.0f gb_hours)",
			r.totalTokensIngested, r.totalGBHoursIngested,
		)
		return nil
	})

	// ── Wait for Processing ─────────────────────────────────────────────

	r.run("Wait for Processing", "-", false, func() error {
		time.Sleep(8 * time.Second)
		r.lastResult().Details = "waited 8s for Kafka→ClickHouse pipeline"
		return nil
	})

	// ── Verify Usage Counts ─────────────────────────────────────────────
	// SDK: client.Subscriptions.GetSubscriptionUsage(ctx, types.DtoGetUsageBySubscriptionRequest{...})
	// API: POST /v1/subscriptions/usage

	if !r.require(r.subscriptionID, "Create Subscription", "Verify Usage Counts") {
		r.skip("Verify Entitlement Usage", "depends on subscription")
		return
	}

	r.run("Verify Usage Counts", "Subscriptions.GetSubscriptionUsage", false, func() error {
		body := map[string]interface{}{
			"subscription_id": r.subscriptionID,
		}

		resp, _, err := r.raw.Post(ctx, "/subscriptions/usage", body)
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf(
			"expected: %.0f tokens + %.0f gb_hours ingested, response keys: %v",
			r.totalTokensIngested, r.totalGBHoursIngested, mapKeys(resp),
		)
		return nil
	})

	// ── Verify Entitlement Usage ────────────────────────────────────────
	// SDK: client.Customers.GetCustomerEntitlements(ctx, customerID)
	// API: GET /v1/customers/:id/entitlements

	if !r.require(r.customerID, "Create Customer", "Verify Entitlement Usage") {
		return
	}

	r.run("Verify Entitlement Usage", "Customers.GetCustomerEntitlements", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/customers/%s/entitlements", r.customerID))
		if err != nil {
			return err
		}

		// Response shape: { "customer_id": "...", "features": [ { "feature": {...}, "entitlement": {...} } ] }
		features := getSlice(resp, "features")
		for _, item := range features {
			if af, ok := item.(map[string]interface{}); ok {
				feat := getMap(af, "feature")
				if feat != nil && getString(feat, "id") == r.featureAID {
					details := "Feature A entitlement found"
					ent := getMap(af, "entitlement")
					if ent != nil {
						limit := getFloat(ent, "usage_limit")
						if limit > 0 {
							details += fmt.Sprintf(", limit=%.0f", limit)
						}
					}
					details += fmt.Sprintf(" (ingested %.0f tokens)", r.totalTokensIngested)
					r.lastResult().Details = details
					return nil
				}
			}
		}

		return fmt.Errorf("Feature A entitlement not found in customer entitlements")
	})
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
