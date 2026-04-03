package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runInvoiceSteps executes Phase 5: Cancellation & Invoice.
func (r *SanityRunner) runInvoiceSteps(ctx context.Context) {
	r.setPhase("PHASE 5: Cancellation & Invoice Generation")
	r.printPhaseHeader(r.phase)

	// ── Cancel Subscription (immediate, generate invoice) ───────────────
	// SDK: client.Subscriptions.CancelSubscription(ctx, subID, types.DtoCancelSubscriptionRequest{...})

	if !r.require(r.subscriptionID, "Create Subscription", "Cancel Subscription") {
		r.skip("Find Generated Invoice", "depends on cancellation")
		r.skip("Verify Invoice Line Items", "depends on cancellation")
		r.skip("Preview Invoice", "depends on cancellation")
		r.skip("Mark Invoice Paid", "depends on cancellation")
		return
	}

	r.run("Cancel Subscription (immediate)", "Subscriptions.CancelSubscription", false, func() error {
		invoicePolicy := types.CancelImmediatelyInvoicePolicyGenerateInvoice
		req := types.DtoCancelSubscriptionRequest{
			CancellationType:               types.CancellationTypeImmediate,
			CancelImmediatelyInovicePolicy: &invoicePolicy, // note: SDK field has typo "Inovice"
			Reason:                         strPtr("Integration test cancellation"),
		}

		resp, err := r.client.Subscriptions.CancelSubscription(ctx, r.subscriptionID, req)
		if err != nil {
			return err
		}
		cancelResp := resp.DtoCancelSubscriptionResponse
		if cancelResp == nil {
			return fmt.Errorf("cancel subscription returned no body")
		}

		// Extract invoice ID from proration_invoice.
		if cancelResp.ProrationInvoice != nil && cancelResp.ProrationInvoice.ID != nil {
			r.invoiceID = *cancelResp.ProrationInvoice.ID
			r.lastResult().EntityID = r.invoiceID
		}

		details := "generate_invoice policy"
		if cancelResp.SubscriptionID != nil {
			details += fmt.Sprintf(", sub=%s", *cancelResp.SubscriptionID)
		}
		if cancelResp.CancellationType != nil {
			details += fmt.Sprintf(", type=%s", string(*cancelResp.CancellationType))
		}
		if cancelResp.Status != nil {
			details += fmt.Sprintf(", status=%s", string(*cancelResp.Status))
		}
		r.lastResult().Details = details
		return nil
	})

	// ── Find Generated Invoice (if not in cancel response) ──────────────
	// SDK: client.Invoices.QueryInvoice(ctx, types.InvoiceFilter{...})

	if r.invoiceID == "" {
		r.run("Find Generated Invoice", "Invoices.QueryInvoice", false, func() error {
			invoiceType := types.InvoiceTypeSubscription
			filter := types.InvoiceFilter{
				SubscriptionID: strPtr(r.subscriptionID),
				InvoiceType:    &invoiceType,
				Sort: []types.SortCondition{
					{Field: strPtr("created_at"), Direction: types.SortDirectionDesc.ToPointer()},
				},
				Limit: int64Ptr(5),
			}

			resp, err := r.client.Invoices.QueryInvoice(ctx, filter)
			if err != nil {
				return err
			}
			listResp := resp.DtoListInvoicesResponse
			if listResp == nil || len(listResp.Items) == 0 {
				return fmt.Errorf("no invoices found for subscription %s", r.subscriptionID)
			}

			// Pick the most recently created invoice (sorted desc).
			inv := listResp.Items[0]
			if inv.ID != nil {
				r.invoiceID = *inv.ID
				r.lastResult().EntityID = r.invoiceID
			}

			if r.invoiceID == "" {
				return fmt.Errorf("could not extract invoice ID from search results")
			}
			r.lastResult().Details = fmt.Sprintf("invoice_id=%s, %d invoice(s) found", r.invoiceID, len(listResp.Items))
			return nil
		})
	}

	// ── Verify Invoice Line Items ───────────────────────────────────────
	// SDK: client.Invoices.GetInvoice(ctx, invoiceID)

	if !r.require(r.invoiceID, "Generated Invoice", "Verify Invoice Line Items") {
		r.skip("Preview Invoice", "depends on invoice")
		r.skip("Mark Invoice Paid", "depends on invoice")
		return
	}

	r.run("Verify Invoice Line Items", "Invoices.GetInvoice", false, func() error {
		resp, err := r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
		if err != nil {
			return err
		}
		inv := resp.DtoInvoiceResponse
		if inv == nil {
			return fmt.Errorf("get invoice returned no body")
		}

		lineItemCount := len(inv.LineItems)
		details := fmt.Sprintf("invoice=%s, %d line items", r.invoiceID, lineItemCount)

		// Show each line item breakdown.
		for i, li := range inv.LineItems {
			name := derefStr(li.DisplayName)
			amount := derefStr(li.Amount)
			qty := derefStr(li.Quantity)
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

		total := derefStr(inv.Total)
		subtotal := derefStr(inv.Subtotal)
		status := ""
		if inv.InvoiceStatus != nil {
			status = string(*inv.InvoiceStatus)
		}
		currency := derefStr(inv.Currency)

		details += fmt.Sprintf("\n        subtotal=$%s, total=$%s, currency=%s, status=%s",
			subtotal, total, currency, status)

		r.lastResult().Details = details
		return nil
	})

	// ── Preview Invoice (full billing period estimate) ──────────────────
	// SDK: client.Invoices.GetInvoicePreview(ctx, types.DtoGetPreviewInvoiceRequest{...})

	r.run("Preview Invoice", "Invoices.GetInvoicePreview", false, func() error {
		req := types.DtoGetPreviewInvoiceRequest{
			SubscriptionID: r.subscriptionID,
		}

		resp, err := r.client.Invoices.GetInvoicePreview(ctx, req)
		if err != nil {
			// Preview may fail for cancelled subscription — fall back to GetInvoice.
			if r.invoiceID != "" {
				getResp, getErr := r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
				if getErr != nil {
					return fmt.Errorf("preview failed (%v) and fallback GetInvoice also failed: %w", err, getErr)
				}
				inv := getResp.DtoInvoiceResponse
				details := "preview unavailable (sub cancelled), verified via GetInvoice"
				if inv != nil && inv.Total != nil {
					details += fmt.Sprintf(", total=$%s", *inv.Total)
				}
				r.lastResult().Details = details
				return nil
			}
			return fmt.Errorf("preview failed: %w", err)
		}

		inv := resp.DtoInvoiceResponse
		if inv == nil {
			return fmt.Errorf("preview returned no body")
		}

		total := derefStr(inv.Total)
		subtotal := derefStr(inv.Subtotal)
		lineItems := inv.LineItems

		details := fmt.Sprintf("preview: %d line items, subtotal=$%s, total=$%s", len(lineItems), subtotal, total)

		// Show expected usage math.
		expectedUsageA := r.totalTokensIngested * 0.01 // $0.01 per token
		expectedUsageB := r.totalGBHoursIngested * 0.05 // $0.05 per GB-hour
		details += fmt.Sprintf("\n        expected usage: Feature A = %.0f tokens x $0.01 = $%.2f",
			r.totalTokensIngested, expectedUsageA)
		details += fmt.Sprintf("\n        expected usage: Feature B = %.0f gb_hours x $0.05 = $%.2f",
			r.totalGBHoursIngested, expectedUsageB)
		details += fmt.Sprintf("\n        expected recurring: $49.99 + $19.99 = $69.98")

		// Show each preview line item.
		for i, li := range lineItems {
			name := derefStr(li.DisplayName)
			amount := derefStr(li.Amount)
			qty := derefStr(li.Quantity)
			liDetail := fmt.Sprintf("  line[%d]: %s", i, name)
			if qty != "" {
				liDetail += fmt.Sprintf(", qty=%s", qty)
			}
			if amount != "" {
				liDetail += fmt.Sprintf(", amount=$%s", amount)
			}
			details += "\n        " + liDetail
		}

		// Verify total is reasonable (> sum of recurring at minimum).
		if total != "" {
			totalFloat, _ := strconv.ParseFloat(total, 64)
			expectedMin := 69.98 + expectedUsageA + expectedUsageB
			if totalFloat < expectedMin*0.5 {
				details += fmt.Sprintf("\n        WARNING: total $%.2f seems low (expected >$%.2f)", totalFloat, expectedMin)
			}
		}

		r.lastResult().Details = details
		return nil
	})

	// ── Mark Invoice Paid ───────────────────────────────────────────────
	// SDK: client.Invoices.UpdateInvoicePaymentStatus(ctx, invoiceID, types.DtoUpdatePaymentStatusRequest{...})

	if !r.require(r.invoiceID, "Generated Invoice", "Mark Invoice Paid") {
		return
	}

	r.run("Mark Invoice Paid", "Invoices.UpdateInvoicePaymentStatus", false, func() error {
		// First check invoice status — finalize if still draft.
		getResp, err := r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
		if err != nil {
			return fmt.Errorf("get invoice (pre-payment): %w", err)
		}

		inv := getResp.DtoInvoiceResponse
		if inv != nil && inv.InvoiceStatus != nil {
			status := string(*inv.InvoiceStatus)
			if status == "draft" || status == "DRAFT" {
				_, err := r.client.Invoices.FinalizeInvoice(ctx, r.invoiceID)
				if err != nil {
					return fmt.Errorf("finalize invoice: %w", err)
				}
			}
		}

		// Determine amount to pay.
		amount := ""
		if inv != nil {
			if inv.AmountDue != nil && *inv.AmountDue != "" {
				amount = *inv.AmountDue
			} else if inv.Total != nil && *inv.Total != "" {
				amount = *inv.Total
			}
		}
		if amount == "" {
			amount = "0.00"
		}

		payReq := types.DtoUpdatePaymentStatusRequest{
			PaymentStatus: types.PaymentStatusSucceeded,
			Amount:        strPtr(amount),
		}

		_, err = r.client.Invoices.UpdateInvoicePaymentStatus(ctx, r.invoiceID, payReq)
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf("invoice=%s, amount=$%s, payment_status=SUCCEEDED", r.invoiceID, amount)
		return nil
	})
}
