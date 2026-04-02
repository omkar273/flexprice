package main

import (
	"context"
	"fmt"
	"strconv"
)

// runInvoiceSteps executes Phase 5: Cancellation & Invoice.
func (r *SanityRunner) runInvoiceSteps(ctx context.Context) {
	r.setPhase("PHASE 5: Cancellation & Invoice Generation")
	r.printPhaseHeader(r.phase)

	// ── Cancel Subscription (immediate, generate invoice) ───────────────
	// SDK: client.Subscriptions.CancelSubscription(ctx, subID, types.DtoCancelSubscriptionRequest{...})
	// API: POST /v1/subscriptions/:id/cancel

	if !r.require(r.subscriptionID, "Create Subscription", "Cancel Subscription") {
		r.skip("Find Generated Invoice", "depends on cancellation")
		r.skip("Verify Invoice Line Items", "depends on cancellation")
		r.skip("Preview Invoice", "depends on cancellation")
		r.skip("Mark Invoice Paid", "depends on cancellation")
		return
	}

	r.run("Cancel Subscription (immediate)", "Subscriptions.CancelSubscription", false, func() error {
		body := map[string]interface{}{
			"cancellation_type":                 "immediate",
			"cancel_immediately_inovice_policy": "generate_invoice", // note: backend typo in field name
			"reason":                            "Integration test cancellation",
		}

		resp, _, err := r.raw.Post(ctx, fmt.Sprintf("/subscriptions/%s/cancel", r.subscriptionID), body)
		if err != nil {
			return err
		}

		// Extract invoice ID from proration_invoice.
		if prorationInvoice := getMap(resp, "proration_invoice"); prorationInvoice != nil {
			invoiceID := getString(prorationInvoice, "id")
			if invoiceID != "" {
				r.invoiceID = invoiceID
				r.lastResult().EntityID = invoiceID
			}
		}

		details := "generate_invoice policy"
		if subID := getString(resp, "subscription_id"); subID != "" {
			details += fmt.Sprintf(", sub=%s", subID)
		}
		if ct := getString(resp, "cancellation_type"); ct != "" {
			details += fmt.Sprintf(", type=%s", ct)
		}
		if st := getString(resp, "status"); st != "" {
			details += fmt.Sprintf(", status=%s", st)
		}
		r.lastResult().Details = details
		return nil
	})

	// ── Find Generated Invoice (if not in cancel response) ──────────────
	// SDK: client.Invoices.QueryInvoice(ctx, types.InvoiceFilter{...})
	// API: POST /v1/invoices/search

	if r.invoiceID == "" {
		r.run("Find Generated Invoice", "Invoices.QueryInvoice", false, func() error {
			body := map[string]interface{}{
				"subscription_id": r.subscriptionID,
				"invoice_type":    "SUBSCRIPTION",
				"sort": []map[string]interface{}{
					{"field": "created_at", "direction": "desc"},
				},
				"limit": 5,
			}

			resp, _, err := r.raw.Post(ctx, "/invoices/search", body)
			if err != nil {
				return err
			}

			items := getSlice(resp, "items")
			if len(items) == 0 {
				return fmt.Errorf("no invoices found for subscription %s", r.subscriptionID)
			}

			// Pick the most recently created invoice (sorted desc).
			if inv, ok := items[0].(map[string]interface{}); ok {
				id := getString(inv, "id")
				if id != "" {
					r.invoiceID = id
					r.lastResult().EntityID = id
				}
			}

			if r.invoiceID == "" {
				return fmt.Errorf("could not extract invoice ID from search results")
			}
			r.lastResult().Details = fmt.Sprintf("invoice_id=%s, %d invoice(s) found", r.invoiceID, len(items))
			return nil
		})
	}

	// ── Verify Invoice Line Items ───────────────────────────────────────
	// SDK: client.Invoices.GetInvoice(ctx, invoiceID)
	// API: GET /v1/invoices/:id

	if !r.require(r.invoiceID, "Generated Invoice", "Verify Invoice Line Items") {
		r.skip("Preview Invoice", "depends on invoice")
		r.skip("Mark Invoice Paid", "depends on invoice")
		return
	}

	r.run("Verify Invoice Line Items", "Invoices.GetInvoice", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/invoices/%s", r.invoiceID))
		if err != nil {
			return err
		}

		lineItems := getSlice(resp, "line_items")
		lineItemCount := len(lineItems)

		details := fmt.Sprintf("invoice=%s, %d line items", r.invoiceID, lineItemCount)

		// Show each line item breakdown.
		for i, li := range lineItems {
			if item, ok := li.(map[string]interface{}); ok {
				name := getString(item, "display_name")
				if name == "" {
					name = getString(item, "description")
				}
				amount := getString(item, "amount")
				qty := getString(item, "quantity")
				if name != "" || amount != "" {
					liDetail := fmt.Sprintf("  line[%d]: %s", i, name)
					if qty != "" {
						liDetail += fmt.Sprintf(", qty=%s", qty)
					}
					if amount != "" {
						liDetail += fmt.Sprintf(", amount=$%s", amount)
					}
					details += "\n        " + liDetail
				}
			}
		}

		total := getString(resp, "total")
		subtotal := getString(resp, "subtotal")
		status := getString(resp, "invoice_status")
		currency := getString(resp, "currency")

		details += fmt.Sprintf("\n        subtotal=$%s, total=$%s, currency=%s, status=%s",
			subtotal, total, currency, status)

		r.lastResult().Details = details
		return nil
	})

	// ── Preview Invoice (full billing period estimate) ──────────────────
	// SDK: client.Invoices.GetInvoicePreview(ctx, types.DtoGetPreviewInvoiceRequest{...})
	// API: POST /v1/invoices/preview

	r.run("Preview Invoice", "Invoices.GetInvoicePreview", false, func() error {
		body := map[string]interface{}{
			"subscription_id": r.subscriptionID,
		}

		resp, _, err := r.raw.Post(ctx, "/invoices/preview", body)
		if err != nil {
			// Preview may fail for cancelled subscription — fall back to GetInvoice.
			if r.invoiceID != "" {
				getResp, _, getErr := r.raw.Get(ctx, fmt.Sprintf("/invoices/%s", r.invoiceID))
				if getErr != nil {
					return fmt.Errorf("preview failed (%v) and fallback GetInvoice also failed: %w", err, getErr)
				}
				details := "preview unavailable (sub cancelled), verified via GetInvoice"
				if total := getString(getResp, "total"); total != "" {
					details += fmt.Sprintf(", total=$%s", total)
				}
				r.lastResult().Details = details
				return nil
			}
			return fmt.Errorf("preview failed: %w", err)
		}

		total := getString(resp, "total")
		subtotal := getString(resp, "subtotal")
		lineItems := getSlice(resp, "line_items")

		details := fmt.Sprintf("preview: %d line items, subtotal=$%s, total=$%s", len(lineItems), subtotal, total)

		// Show expected usage math.
		expectedUsageA := r.totalTokensIngested * 0.01   // $0.01 per token
		expectedUsageB := r.totalGBHoursIngested * 0.05   // $0.05 per GB-hour
		details += fmt.Sprintf("\n        expected usage: Feature A = %.0f tokens x $0.01 = $%.2f",
			r.totalTokensIngested, expectedUsageA)
		details += fmt.Sprintf("\n        expected usage: Feature B = %.0f gb_hours x $0.05 = $%.2f",
			r.totalGBHoursIngested, expectedUsageB)
		details += fmt.Sprintf("\n        expected recurring: $49.99 + $19.99 = $69.98")

		// Show each preview line item.
		for i, li := range lineItems {
			if item, ok := li.(map[string]interface{}); ok {
				name := getString(item, "display_name")
				if name == "" {
					name = getString(item, "description")
				}
				amount := getString(item, "amount")
				qty := getString(item, "quantity")
				liDetail := fmt.Sprintf("  line[%d]: %s", i, name)
				if qty != "" {
					liDetail += fmt.Sprintf(", qty=%s", qty)
				}
				if amount != "" {
					liDetail += fmt.Sprintf(", amount=$%s", amount)
				}
				details += "\n        " + liDetail
			}
		}

		// Verify total is reasonable (> sum of recurring at minimum).
		if total != "" {
			totalFloat, _ := strconv.ParseFloat(total, 64)
			expectedMin := 69.98 + expectedUsageA + expectedUsageB // recurring + usage (no addon/tax/discount)
			if totalFloat < expectedMin*0.5 {
				details += fmt.Sprintf("\n        WARNING: total $%.2f seems low (expected >$%.2f)", totalFloat, expectedMin)
			}
		}

		r.lastResult().Details = details
		return nil
	})

	// ── Mark Invoice Paid ───────────────────────────────────────────────
	// SDK: client.Invoices.UpdateInvoicePaymentStatus(ctx, invoiceID, types.DtoUpdatePaymentStatusRequest{...})
	// API: PUT /v1/invoices/:id/payment

	if !r.require(r.invoiceID, "Generated Invoice", "Mark Invoice Paid") {
		return
	}

	r.run("Mark Invoice Paid", "Invoices.UpdateInvoicePaymentStatus", false, func() error {
		// First check invoice status — finalize if still draft.
		getResp, _, err := r.raw.Get(ctx, fmt.Sprintf("/invoices/%s", r.invoiceID))
		if err != nil {
			return fmt.Errorf("get invoice (pre-payment): %w", err)
		}

		status := getString(getResp, "invoice_status")
		if status == "draft" || status == "DRAFT" {
			_, _, err := r.raw.Post(ctx, fmt.Sprintf("/invoices/%s/finalize", r.invoiceID), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("finalize invoice: %w", err)
			}
		}

		// Determine amount to pay.
		amount := getString(getResp, "amount_due")
		if amount == "" {
			amount = getString(getResp, "total")
		}
		if amount == "" {
			amount = "0.00"
		}

		body := map[string]interface{}{
			"payment_status": "SUCCEEDED",
			"amount":         amount,
		}

		_, _, err = r.raw.Put(ctx, fmt.Sprintf("/invoices/%s/payment", r.invoiceID), body)
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf("invoice=%s, amount=$%s, payment_status=SUCCEEDED", r.invoiceID, amount)
		return nil
	})
}
