package main

import (
	"context"
	"fmt"
)

// runWalletSteps executes Phase 3b: Wallet Operations.
func (r *SanityRunner) runWalletSteps(ctx context.Context) {
	r.setPhase("PHASE 3b: Wallet Operations")
	r.printPhaseHeader(r.phase)

	if !r.require(r.customerID, "Create Customer", "Create Wallet") {
		r.skip("Top-Up Wallet", "depends on wallet creation")
		r.skip("Verify Wallet Balance", "depends on wallet creation")
		return
	}

	// ── Create Wallet ──────────────────────────────────────────────────
	// SDK: client.Wallets.CreateWallet(ctx, types.DtoCreateWalletRequest{...})
	// API: POST /v1/wallets

	r.run("Create Wallet", "Wallets.CreateWallet", false, func() error {
		body := map[string]interface{}{
			"customer_id":    r.customerID,
			"currency":       "USD",
			"wallet_type":    "PRE_PAID",
			"name":           fmt.Sprintf("Sanity Test Wallet %d", ts()),
			"description":    "Integration test prepaid wallet",
			"metadata":       map[string]string{"source": "sanity_test"},
		}

		resp, _, err := r.raw.Post(ctx, "/wallets", body)
		if err != nil {
			return err
		}
		id := getString(resp, "id")
		if id == "" {
			return fmt.Errorf("missing id in wallet response: %v", resp)
		}
		r.walletID = id
		r.lastResult().EntityID = id
		r.lastResult().Details = fmt.Sprintf("wallet_id=%s, type=PRE_PAID, currency=USD, customer=%s", id, r.customerID)
		return nil
	})

	// ── Top-Up Wallet ──────────────────────────────────────────────────
	// SDK: client.Wallets.TopUpWallet(ctx, walletID, types.DtoTopUpWalletRequest{...})
	// API: POST /v1/wallets/:id/top-up

	if !r.require(r.walletID, "Create Wallet", "Top-Up Wallet") {
		r.skip("Verify Wallet Balance", "depends on top-up")
		return
	}

	r.run("Top-Up Wallet (500 credits)", "Wallets.TopUpWallet", false, func() error {
		body := map[string]interface{}{
			"credits_to_add":     "500.00",
			"transaction_reason": "PURCHASED_CREDIT_DIRECT",
			"description":        "Integration test top-up",
		}

		resp, _, err := r.raw.Post(ctx, fmt.Sprintf("/wallets/%s/top-up", r.walletID), body)
		if err != nil {
			return err
		}

		txnID := getString(resp, "id")
		details := "500 credits added"
		if txnID != "" {
			details += fmt.Sprintf(", txn=%s", truncate(txnID, 18))
		}
		r.lastResult().Details = details
		return nil
	})

	// ── Verify Wallet Balance ──────────────────────────────────────────
	// SDK: client.Wallets.GetWalletBalance(ctx, walletID)
	// API: GET /v1/wallets/:id/balance/real-time

	r.run("Verify Wallet Balance", "Wallets.GetWalletBalance", false, func() error {
		resp, _, err := r.raw.Get(ctx, fmt.Sprintf("/wallets/%s/balance/real-time", r.walletID))
		if err != nil {
			return err
		}

		details := "balance retrieved"
		if balance := getString(resp, "balance"); balance != "" {
			details = fmt.Sprintf("balance=%s", balance)
		}
		if credits := getString(resp, "credits_available"); credits != "" {
			details += fmt.Sprintf(", credits_available=%s", credits)
		}
		r.lastResult().Details = details
		return nil
	})
}
