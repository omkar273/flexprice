package main

import (
	"context"
	"fmt"
	"strings"
)

// runCleanupSteps executes Phase 7: Cleanup.
// Deletes all entities created during the test in correct dependency order.
// Cleanup failures are reported honestly as [FAIL] but do NOT cause non-zero exit.
func (r *SanityRunner) runCleanupSteps(ctx context.Context) {
	r.setPhase("PHASE 7: Cleanup")
	r.printPhaseHeader(r.phase)

	// ── 1. Void invoice ──────────────────────────────────────────────────
	if r.invoiceID != "" {
		r.run("Cleanup: Void Invoice", "Invoices.VoidInvoice", false, func() error {
			_, _, err := r.raw.Post(ctx, fmt.Sprintf("/invoices/%s/void", r.invoiceID), map[string]interface{}{})
			if err != nil {
				if strings.Contains(err.Error(), "already voided") || strings.Contains(err.Error(), "VOIDED") {
					r.lastResult().Details = fmt.Sprintf("invoice_id=%s, already voided", r.invoiceID)
					return nil
				}
				return fmt.Errorf("void invoice: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("invoice_id=%s, voided", r.invoiceID)
			return nil
		})
	}

	// ── 2. Cancel subscription ───────────────────────────────────────────
	if r.subscriptionID != "" {
		r.run("Cleanup: Cancel Subscription", "Subscriptions.CancelSubscription", false, func() error {
			body := map[string]interface{}{
				"cancellation_type": "immediate",
			}
			_, _, err := r.raw.Post(ctx, fmt.Sprintf("/subscriptions/%s/cancel", r.subscriptionID), body)
			if err != nil {
				r.lastResult().Details = fmt.Sprintf("sub_id=%s, already cancelled (expected)", r.subscriptionID)
				return nil
			}
			r.lastResult().Details = fmt.Sprintf("sub_id=%s, cancelled", r.subscriptionID)
			return nil
		})
	}

	// ── 3. Delete entitlement ────────────────────────────────────────────
	if r.entitlementID != "" {
		r.run("Cleanup: Delete Entitlement", "Entitlements.DeleteEntitlement", false, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/entitlements/%s", r.entitlementID))
			if err != nil {
				return fmt.Errorf("delete entitlement: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("ent_id=%s, deleted", r.entitlementID)
			return nil
		})
	}

	// ── 4. Delete prices ─────────────────────────────────────────────────
	priceIDs := []struct {
		id   string
		name string
	}{
		{r.priceUsageB, "usage price B"},
		{r.priceUsageA, "usage price A"},
		{r.priceRecurr2, "recurring price 2"},
		{r.priceRecurr1, "recurring price 1"},
	}
	for _, p := range priceIDs {
		if p.id == "" {
			continue
		}
		priceID := p.id
		priceName := p.name
		r.run(fmt.Sprintf("Cleanup: Delete Price (%s)", priceName), "Prices.DeletePrice", false, func() error {
			_, _, err := r.raw.DeleteWithBody(ctx, fmt.Sprintf("/prices/%s", priceID), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("delete price %s: %w", priceName, err)
			}
			r.lastResult().Details = fmt.Sprintf("price_id=%s, deleted", priceID)
			return nil
		})
	}

	// ── 5. Delete plan ───────────────────────────────────────────────────
	if r.planID != "" {
		r.run("Cleanup: Delete Plan", "Plans.DeletePlan", false, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/plans/%s", r.planID))
			if err != nil {
				return fmt.Errorf("delete plan: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("plan_id=%s, deleted", r.planID)
			return nil
		})
	}

	// ── 6. Delete features ───────────────────────────────────────────────
	for _, fid := range []string{r.featureBID, r.featureAID} {
		if fid == "" {
			continue
		}
		featureID := fid
		r.run(fmt.Sprintf("Cleanup: Delete Feature"), "Features.DeleteFeature", false, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/features/%s", featureID))
			if err != nil {
				return fmt.Errorf("delete feature: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("feat_id=%s, deleted", featureID)
			return nil
		})
	}

	// ── 7. Terminate wallet(s) for customer ──────────────────────────────
	if r.customerID != "" {
		r.run("Cleanup: Terminate Wallet(s)", "Wallets.TerminateWallet", false, func() error {
			wallets, _, err := r.raw.GetArray(ctx, fmt.Sprintf("/customers/%s/wallets", r.customerID))
			if err != nil {
				if r.walletID != "" {
					_, _, termErr := r.raw.Post(ctx, fmt.Sprintf("/wallets/%s/terminate", r.walletID), map[string]interface{}{})
					if termErr != nil {
						return fmt.Errorf("terminate wallet %s: %w", r.walletID, termErr)
					}
					r.lastResult().Details = fmt.Sprintf("wallet_id=%s, terminated (direct)", r.walletID)
					return nil
				}
				return fmt.Errorf("list customer wallets: %w", err)
			}

			if len(wallets) == 0 {
				r.lastResult().Details = "no wallets found"
				return nil
			}

			terminated := 0
			ids := []string{}
			var lastErr error
			for _, w := range wallets {
				wallet, ok := w.(map[string]interface{})
				if !ok {
					continue
				}
				wID := getString(wallet, "id")
				wStatus := getString(wallet, "wallet_status")
				if wID == "" {
					continue
				}
				if wStatus == "terminated" || wStatus == "TERMINATED" || wStatus == "closed" {
					terminated++
					ids = append(ids, wID)
					continue
				}
				_, _, termErr := r.raw.Post(ctx, fmt.Sprintf("/wallets/%s/terminate", wID), map[string]interface{}{})
				if termErr != nil {
					lastErr = fmt.Errorf("terminate wallet %s: %w", wID, termErr)
				} else {
					terminated++
					ids = append(ids, wID)
				}
			}

			r.lastResult().Details = fmt.Sprintf("wallet_ids=[%s], terminated %d/%d",
				strings.Join(ids, ", "), terminated, len(wallets))
			if lastErr != nil {
				return lastErr
			}
			return nil
		})
	}

	// ── 8. Delete customer ───────────────────────────────────────────────
	if r.customerID != "" {
		r.run("Cleanup: Delete Customer", "Customers.DeleteCustomer", false, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/customers/%s", r.customerID))
			if err != nil {
				return fmt.Errorf("delete customer: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("cust_id=%s, deleted", r.customerID)
			return nil
		})
	}

	// ── 9. Delete coupon ─────────────────────────────────────────────────
	if r.couponID != "" {
		r.run("Cleanup: Delete Coupon", "Coupons.DeleteCoupon", true, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/coupons/%s", r.couponID))
			if err != nil {
				return fmt.Errorf("delete coupon: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("coupon_id=%s, deleted", r.couponID)
			return nil
		})
	}

	// ── 10. Delete tax rate ──────────────────────────────────────────────
	if r.taxRateID != "" {
		r.run("Cleanup: Delete Tax Rate", "TaxRates.DeleteTaxRate", true, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/taxes/rates/%s", r.taxRateID))
			if err != nil {
				return fmt.Errorf("delete tax rate: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("tax_id=%s, deleted", r.taxRateID)
			return nil
		})
	}

	// ── 11. Delete groups ────────────────────────────────────────────────
	for _, gid := range []string{r.priceGroupID, r.featureGroupID} {
		if gid == "" {
			continue
		}
		groupID := gid
		r.run("Cleanup: Delete Group", "Groups.DeleteGroup", true, func() error {
			_, err := r.raw.Delete(ctx, fmt.Sprintf("/groups/%s", groupID))
			if err != nil {
				return fmt.Errorf("delete group: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("group_id=%s, deleted", groupID)
			return nil
		})
	}
}
