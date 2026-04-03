package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
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

	r.run("Bulk Ingest 50 Events", "Events.IngestEventsBulk", false, func() error {
		events := make([]types.DtoIngestEventRequest, 0, 50)

		// 30 events for Feature A (api_call).
		r.totalTokensIngested = 0
		for i := 0; i < 30; i++ {
			tokens := float64(rand.Intn(50) + 1) // 1-50 tokens per event
			r.totalTokensIngested += tokens

			events = append(events, types.DtoIngestEventRequest{
				EventName:          r.eventNameA,
				ExternalCustomerID: r.externalCustID,
				Properties: map[string]string{
					"tokens": fmt.Sprintf("%.0f", tokens),
				},
				Source:    strPtr("sanity_test"),
				Timestamp: strPtr(time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339)),
			})
		}

		// 20 events for Feature B (storage_usage).
		r.totalGBHoursIngested = 0
		for i := 0; i < 20; i++ {
			gbHours := float64(rand.Intn(10) + 1) // 1-10 GB-hours per event
			r.totalGBHoursIngested += gbHours

			events = append(events, types.DtoIngestEventRequest{
				EventName:          r.eventNameB,
				ExternalCustomerID: r.externalCustID,
				Properties: map[string]string{
					"gb_hours": fmt.Sprintf("%.0f", gbHours),
				},
				Source:    strPtr("sanity_test"),
				Timestamp: strPtr(time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339)),
			})
		}

		req := types.DtoBulkIngestEventRequest{
			Events: events,
		}

		_, err := r.client.Events.IngestEventsBulk(ctx, req)
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
	// Using raw HTTP for subscription usage query (POST /v1/subscriptions/usage)

	if !r.require(r.subscriptionID, "Create Subscription", "Verify Usage Counts") {
		r.skip("Verify Entitlement Usage", "depends on subscription")
		return
	}

	r.run("Verify Usage Counts", "Subscriptions.GetSubscriptionUsage", false, func() error {
		req := types.DtoGetUsageBySubscriptionRequest{
			SubscriptionID: r.subscriptionID,
		}

		_, err := r.client.Subscriptions.GetSubscriptionUsage(ctx, req)
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf(
			"expected: %.0f tokens + %.0f gb_hours ingested, usage query succeeded",
			r.totalTokensIngested, r.totalGBHoursIngested,
		)
		return nil
	})

	// ── Verify Entitlement Usage ────────────────────────────────────────
	// SDK: client.Customers.GetCustomerEntitlements(ctx, customerID)

	if !r.require(r.customerID, "Create Customer", "Verify Entitlement Usage") {
		return
	}

	r.run("Verify Entitlement Usage", "Customers.GetCustomerEntitlements", false, func() error {
		resp, err := r.client.Customers.GetCustomerEntitlements(ctx, r.customerID)
		if err != nil {
			return err
		}
		entResp := resp.DtoCustomerEntitlementsResponse
		if entResp == nil {
			return fmt.Errorf("get customer entitlements returned no body")
		}

		for _, af := range entResp.Features {
			if af.Feature != nil && af.Feature.ID != nil && *af.Feature.ID == r.featureAID {
				details := "Feature A entitlement found"
				if af.Entitlement != nil && af.Entitlement.UsageLimit != nil {
					details += fmt.Sprintf(", limit=%d", *af.Entitlement.UsageLimit)
				}
				details += fmt.Sprintf(" (ingested %.0f tokens)", r.totalTokensIngested)
				r.lastResult().Details = details
				return nil
			}
		}

		return fmt.Errorf("Feature A entitlement not found in customer entitlements")
	})
}

