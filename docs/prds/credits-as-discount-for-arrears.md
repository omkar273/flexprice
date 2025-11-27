# PRD: Credits as Discount Instrument for Arrear Charges

## Executive Summary

This PRD outlines a fundamental change to how credits/wallets are used in the Flexprice system. Instead of acting as a payment instrument applied after invoice creation, credits will function as a discount instrument that reduces line item amounts during invoice finalization. Credits will **only** be applicable to **USAGE-based arrear charges** (past due usage amounts), not FIXED charges, advance charges, or current period charges.

## Current State Analysis

### Current Credit/Wallet Behavior

1. **Payment Instrument Model**: Credits are currently used as a payment method applied **after** invoice creation

   - Invoices are created with full line item amounts
   - Wallets are used to pay invoices via `ProcessInvoicePaymentWithWallets()`
   - Payment happens separately from invoice creation

2. **Price Type Restrictions**: Wallets have `AllowedPriceTypes` configuration

   - Can restrict wallets to USAGE, FIXED, or ALL price types
   - Used to control which types of charges a wallet can pay for
   - Applied during payment processing, not invoice creation

3. **Payment Processing Flow**:

   ```
   Invoice Created → Invoice Finalized → Payment Processing → Wallet Payment Applied
   ```

4. **Current Usage Points**:
   - `wallet_payment.go`: Categorizes wallets by price type, calculates allowed amounts
   - `subscription_payment_processor.go`: Processes wallet payments for subscriptions
   - `wallet.go`: Balance calculations consider price type restrictions
   - `creditgrant.go`: Auto-creates wallets with USAGE-only restriction

### Current Invoice Line Item Structure

- `InvoiceLineItem.Amount`: The full charge amount for the line item
- `InvoiceLineItem.PriceType`: The price type (USAGE or FIXED) - stored as `*string`
- Line items are created with full amounts
- Discounts are applied via `TotalDiscount` at invoice level or via coupon applications
- Arrear vs Advance classification is determined by `InvoiceCadence` on subscription line items
- **No field currently exists to track credits applied to line items**

### Current Arrear Charge Identification

- Arrear charges are identified by `InvoiceCadence == InvoiceCadenceArrear` on subscription line items
- In billing service (`billing.go`), arrear charges are filtered via `FilterLineItemsToBeInvoiced()` with classification `CurrentPeriodArrear`
- Arrear charges include both USAGE and FIXED price types
- Advance charges are invoiced separately with `InvoiceCadence == InvoiceCadenceAdvance`

## Proposed Changes

### 1. Remove AllowedPriceTypes Configuration

**Change**: Remove `AllowedPriceTypes` from `WalletConfig`

**Rationale**:

- Credits will only apply to USAGE arrear charges (specific use case)
- No need to restrict by price type in wallet config anymore (restriction is at application level)
- Simplifies wallet configuration and logic
- Clear separation: credits for usage-based charges only

**Impact**:

- Remove `AllowedPriceTypes` field from `WalletConfig` struct
- Remove all validation and usage logic related to `AllowedPriceTypes`
- Update default wallet config to not include this field
- Remove price type categorization logic in payment processing

### 2. Credits as Discount Instrument

**Change**: Credits reduce line item amounts directly, similar to how coupons work

**New Behavior**:

- Credits are applied **during invoice creation/finalization**
- Credits reduce `InvoiceLineItem.Amount` directly
- Credits are applied at the line item level (not invoice level)
- Credits act like discounts, reducing the total before tax calculation

**Flow**:

```
Invoice Creation → Identify Arrear Line Items → Apply Credits (Reduce Amounts) → Calculate Totals → Finalize
```

### 3. USAGE Arrear-Only Restriction

**Change**: Credits can **only** be applied to **USAGE-based arrear charges**

**Definition of Eligible Line Items**:

- Line items where the source subscription line item has `InvoiceCadence == InvoiceCadenceArrear`
- **AND** `PriceType == PRICE_TYPE_USAGE` (USAGE price type)
- Does NOT include:
  - FIXED arrear charges (`PriceType == PRICE_TYPE_FIXED`)
  - Advance charges (`InvoiceCadence == InvoiceCadenceAdvance`)
  - Any current period advance charges

**Application Logic**:

- During invoice creation, identify which line items are USAGE arrear charges
- Only apply credits to those specific line items
- FIXED arrear charges and advance charges remain at full amount

### 4. Application During Invoice Finalization

**Change**: Credits are applied during invoice creation/finalization, not during payment

**Implementation Points**:

1. **Invoice Creation** (`invoice.go:CreateInvoice()`):

   - After line items are created
   - Before coupons are applied (or after, depending on priority)
   - Before tax calculation
   - Reduce line item amounts based on available credits
   - Track credits applied in `credits_applied` field

2. **Invoice Finalization** (`invoice.go:FinalizeInvoice()`):

   - If invoice is in draft, apply credits during finalization
   - Ensure credits are applied before invoice is marked as finalized
   - Update `credits_applied` field on line items

3. **Subscription Invoice Creation** (`billing.go:CreateInvoiceRequestForCharges()`):
   - Apply credits to USAGE arrear line items during invoice request creation
   - Ensure FIXED and advance charges are not affected

### 5. Credits Applied Field

**Change**: Add `credits_applied` field to `InvoiceLineItem` to track credit usage

**New Field**:

- `CreditsApplied *decimal.Decimal`: Amount of credits applied to this line item
- Defaults to `nil` or `0` if no credits applied
- Stored in database and included in API responses
- Used for audit trail and reporting

**Purpose**:

- Track exactly how much credit was applied to each line item
- Enable proper accounting and reporting
- Support invoice recalculation and voiding scenarios
- Display credit application details in invoice PDFs and UI

## Detailed Design

### Credit Application Algorithm

```go
func ApplyCreditsToInvoiceLineItems(
    ctx context.Context,
    inv *invoice.Invoice,
) error {
    // 1. Get all active wallets for customer in invoice currency
    wallets := GetActiveWalletsForCustomer(ctx, inv.CustomerID, inv.Currency)

    // 2. Calculate total available credits
    totalCredits := CalculateTotalAvailableCredits(wallets)

    // 3. Identify USAGE arrear line items only
    eligibleLineItems := FilterUsageArrearLineItems(inv.LineItems)

    // 4. Apply credits to eligible line items (FIFO strategy)
    remainingCredits := totalCredits
    totalCreditsApplied := decimal.Zero

    for _, lineItem := range eligibleLineItems {
        if remainingCredits.IsZero() {
            break
        }

        // Determine credit amount to apply (min of line item amount and remaining credits)
        creditToApply := decimal.Min(lineItem.Amount, remainingCredits)

        // Set credits_applied field
        lineItem.CreditsApplied = &creditToApply

        // Reduce line item amount
        lineItem.Amount = lineItem.Amount.Sub(creditToApply)

        // Track total credits applied
        totalCreditsApplied = totalCreditsApplied.Add(creditToApply)

        // Update remaining credits
        remainingCredits = remainingCredits.Sub(creditToApply)
    }

    // 5. Recalculate invoice totals (accounting for credits_applied)
    RecalculateInvoiceTotals(inv)

    // 6. Create wallet transactions to debit credits
    if totalCreditsApplied.GreaterThan(decimal.Zero) {
        CreateWalletTransactionsForCredits(wallets, totalCreditsApplied)
    }

    return nil
}
```

### Credits Applied Field Implementation

**Field Definition**:

```go
// In internal/domain/invoice/line_item.go
type InvoiceLineItem struct {
    // ... existing fields ...
    Amount        decimal.Decimal  `json:"amount"`
    CreditsApplied *decimal.Decimal `json:"credits_applied,omitempty"` // NEW FIELD
    // ... other fields ...
}
```

**Validation**:

- `CreditsApplied` must be non-negative
- `CreditsApplied` must be <= original line item amount (before credit application)
- `Amount` after credit application = original amount - credits_applied

**Accounting**:

- Original line item amount = `Amount` + `CreditsApplied`
- Final line item amount = `Amount` (after credit reduction)
- Total credits applied to invoice = sum of all `CreditsApplied` values

### Line Item Identification

**Eligible Line Item Criteria** (ALL must be true):

1. Line item must have `SubscriptionID` (not one-off invoices)
2. Source subscription line item must have `InvoiceCadence == InvoiceCadenceArrear`
3. **Line item must have `PriceType == PRICE_TYPE_USAGE`** (USAGE price type only)
4. Line item must not be an advance charge

**Implementation**:

- Filter line items where `PriceType != nil && *PriceType == string(types.PRICE_TYPE_USAGE)`
- Query subscription line items to verify `InvoiceCadence == InvoiceCadenceArrear`
- May need to add metadata or reference to subscription line item ID for verification
- Or use existing `PriceType` field on invoice line item (already populated)

**Filter Function**:

```go
func FilterUsageArrearLineItems(lineItems []*InvoiceLineItem, subscription *Subscription) []*InvoiceLineItem {
    eligible := make([]*InvoiceLineItem, 0)

    for _, item := range lineItems {
        // Must have subscription ID
        if item.SubscriptionID == nil {
            continue
        }

        // Must be USAGE price type
        if item.PriceType == nil || *item.PriceType != string(types.PRICE_TYPE_USAGE) {
            continue
        }

        // Must be arrear cadence (verify from subscription line items)
        if !IsArrearLineItem(item, subscription) {
            continue
        }

        eligible = append(eligible, item)
    }

    return eligible
}
```

### Credit Application Priority

**Question**: Should credits be applied before or after coupons?

**Recommendation**: Apply credits **after coupons** to maximize discount value

- Coupons may have percentage discounts that benefit from higher base amounts
- Credits are fixed amounts, so applying after coupons ensures maximum total discount

**Alternative**: Apply credits **before coupons** if we want credits to reduce the base for percentage coupons

**Decision Needed**: Product team to decide on priority

### Credit Allocation Strategy

**Options**:

1. **Proportional Allocation**: Allocate credits proportionally across all arrear line items

   - Example: If 3 line items of $100, $200, $300 and $300 credits available
   - Allocate: $50, $100, $150 respectively

2. **FIFO (First In First Out)**: Apply credits to line items in order

   - Example: Apply $100 to first item, $200 to second, $0 to third

3. **Largest First**: Apply credits to largest line items first
   - Example: Apply $300 to $300 item, $0 to others

**Recommendation**: **FIFO** - simplest to understand and audit

### Credit Consumption from Wallet Transactions

**Overview**: Credits are stored in wallet transactions. Each credit transaction has a `CreditsAvailable` field that tracks how much credit is still available to be consumed. When credits are applied to invoices, they must be consumed from these transactions.

#### Credit Transaction Structure

Each wallet transaction that adds credits has:

- `Type`: `TransactionTypeCredit` (credit transaction)
- `CreditAmount`: Total credits added in this transaction
- `CreditsAvailable`: Remaining credits available from this transaction (decreases as credits are consumed)
- `ExpiryDate`: Optional expiry date for credits
- `Priority`: Optional priority (lower number = higher priority)
- `TransactionReason`: Why credits were granted (e.g., `FREE_CREDIT_GRANT`, `SUBSCRIPTION_CREDIT_GRANT`)

#### Finding Available Credits

**Algorithm**: `FindEligibleCredits(walletID, amount, limit)`

1. Query all credit transactions (`Type == TransactionTypeCredit`) for the wallet
2. Filter transactions where `CreditsAvailable > 0`
3. Filter out expired credits (if `ExpiryDate` is set and in the past)
4. Sort by priority (lower number first), then by expiry date (earliest first), then by creation date (oldest first) - **FIFO with expiry priority**
5. Return transactions until cumulative `CreditsAvailable >= amount` or limit reached

**Example**:

```
Wallet has 3 credit transactions:
- Tx1: CreditsAvailable = 50, Priority = 1, Expiry = 2024-12-31
- Tx2: CreditsAvailable = 100, Priority = 2, Expiry = 2024-12-31
- Tx3: CreditsAvailable = 75, Priority = 1, Expiry = 2024-01-31 (expired - excluded)

Need 120 credits:
- Select Tx1 (50 credits) - Priority 1, not expired
- Select Tx2 (100 credits) - Priority 2, not expired
- Total: 150 credits available (enough for 120)
```

#### Consuming Credits from Transactions

**Algorithm**: `ConsumeCredits(transactions, amountToConsume)`

1. Iterate through eligible transactions in order (priority → expiry → creation date)
2. For each transaction:
   - Calculate `creditsToConsume = min(transaction.CreditsAvailable, remainingAmount)`
   - Update `transaction.CreditsAvailable = transaction.CreditsAvailable - creditsToConsume`
   - Update transaction in database
   - Track which transaction provided which credits (for audit trail)
3. Continue until `amountToConsume` is fully consumed

**Example**:

```
Need to consume 120 credits from:
- Tx1: CreditsAvailable = 50
- Tx2: CreditsAvailable = 100

Consumption:
- From Tx1: Consume 50 credits → Tx1.CreditsAvailable = 0
- From Tx2: Consume 70 credits → Tx2.CreditsAvailable = 30
- Total consumed: 120 credits
```

#### Complete Credit Consumption Flow

```go
func ConsumeCreditsForInvoice(
    ctx context.Context,
    wallets []*wallet.Wallet,
    totalCreditsNeeded decimal.Decimal,
) ([]*CreditConsumptionRecord, error) {
    var consumptionRecords []*CreditConsumptionRecord
    remainingCredits := totalCreditsNeeded

    // Process wallets in order: promotional first, then prepaid
    sortedWallets := SortWalletsByType(wallets) // Promotional first

    for _, wallet := range sortedWallets {
        if remainingCredits.IsZero() {
            break
        }

        // Find eligible credits from this wallet
        creditsNeeded := remainingCredits
        eligibleCredits, err := FindEligibleCredits(ctx, wallet.ID, creditsNeeded, 100)
        if err != nil {
            return nil, err
        }

        // Calculate total available from this wallet
        walletAvailable := decimal.Zero
        for _, tx := range eligibleCredits {
            walletAvailable = walletAvailable.Add(tx.CreditsAvailable)
        }

        // Determine how much to consume from this wallet
        creditsToConsume := decimal.Min(remainingCredits, walletAvailable)

        // Consume credits from transactions
        consumptionDetails, err := ConsumeCredits(ctx, eligibleCredits, creditsToConsume)
        if err != nil {
            return nil, err
        }

        // Record consumption
        for _, detail := range consumptionDetails {
            consumptionRecords = append(consumptionRecords, &CreditConsumptionRecord{
                WalletID:            wallet.ID,
                TransactionID:       detail.TransactionID,
                CreditsConsumed:     detail.CreditsConsumed,
                CreditsAvailableBefore: detail.CreditsAvailableBefore,
                CreditsAvailableAfter:  detail.CreditsAvailableAfter,
            })
        }

        // Update remaining credits needed
        remainingCredits = remainingCredits.Sub(creditsToConsume)
    }

    if remainingCredits.GreaterThan(decimal.Zero) {
        return nil, ierr.NewError("insufficient credits")
    }

    return consumptionRecords, nil
}
```

### Wallet Transaction Creation

**Change**: Create debit wallet transactions during invoice creation, not payment

**Transaction Details**:

- `Type`: `TransactionTypeDebit` (debit transaction)
- `TransactionReason`: New reason `INVOICE_CREDIT_APPLICATION` or reuse `TransactionReasonInvoicePayment`
- `Amount`: Currency amount debited (calculated from credits using conversion rate)
- `CreditAmount`: Credits debited
- `ReferenceType`: `WalletTxReferenceTypeInvoice` (new type) or `WalletTxReferenceTypePayment`
- `ReferenceID`: Invoice ID
- `CreditsAvailable`: Always `0` for debit transactions
- `CreditBalanceBefore`: Wallet credit balance before this debit
- `CreditBalanceAfter`: Wallet credit balance after this debit
- `Metadata`: Include:
  - `invoice_id`: Invoice ID
  - `line_item_ids`: Array of line item IDs credits were applied to
  - `credits_applied_per_item`: Map of line item ID → credits applied
  - `source_transaction_ids`: Array of credit transaction IDs that were consumed
- `Description`: "Credit application for invoice {invoice_id}"

**Multiple Wallets**:

- Process wallets in order: **Promotional first, then Prepaid**
- For each wallet:
  1. Find eligible credits
  2. Consume credits from transactions
  3. Create debit transaction
  4. Update wallet balance
- Continue until all credits needed are consumed or no more wallets available

**Transaction Creation Flow**:

```go
func CreateDebitTransactionsForCredits(
    ctx context.Context,
    consumptionRecords []*CreditConsumptionRecord,
    invoiceID string,
    lineItemCredits map[string]decimal.Decimal, // lineItemID -> credits applied
) error {
    // Group consumption by wallet
    walletConsumptions := GroupByWallet(consumptionRecords)

    for walletID, records := range walletConsumptions {
        // Get wallet
        wallet, err := GetWalletByID(ctx, walletID)
        if err != nil {
            return err
        }

        // Calculate total credits consumed from this wallet
        totalCreditsConsumed := decimal.Zero
        sourceTransactionIDs := make([]string, 0)
        for _, record := range records {
            totalCreditsConsumed = totalCreditsConsumed.Add(record.CreditsConsumed)
            sourceTransactionIDs = append(sourceTransactionIDs, record.TransactionID)
        }

        // Calculate currency amount
        currencyAmount := GetCurrencyAmountFromCredits(totalCreditsConsumed, wallet.ConversionRate)

        // Calculate new credit balance
        newCreditBalance := wallet.CreditBalance.Sub(totalCreditsConsumed)

        // Create debit transaction
        debitTx := &wallet.Transaction{
            ID:                  GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
            WalletID:            walletID,
            Type:                types.TransactionTypeDebit,
            Amount:              currencyAmount,
            CreditAmount:        totalCreditsConsumed,
            CreditsAvailable:    decimal.Zero, // Always 0 for debits
            CreditBalanceBefore: wallet.CreditBalance,
            CreditBalanceAfter:  newCreditBalance,
            ReferenceType:       types.WalletTxReferenceTypeInvoice, // NEW TYPE
            ReferenceID:         invoiceID,
            TransactionReason:   types.TransactionReasonInvoiceCreditApplication, // NEW REASON
            Description:         fmt.Sprintf("Credit application for invoice %s", invoiceID),
            Metadata: types.Metadata{
                "invoice_id":                invoiceID,
                "line_item_ids":             GetLineItemIDs(lineItemCredits),
                "credits_applied_per_item":  lineItemCredits,
                "source_transaction_ids":    sourceTransactionIDs,
            },
            TxStatus:      types.TransactionStatusCompleted,
            EnvironmentID: GetEnvironmentID(ctx),
            BaseModel:     GetDefaultBaseModel(ctx),
        }

        // Save transaction
        if err := CreateWalletTransaction(ctx, debitTx); err != nil {
            return err
        }

        // Update wallet balance
        if err := UpdateWalletBalance(ctx, walletID,
            GetCurrencyAmountFromCredits(newCreditBalance, wallet.ConversionRate),
            newCreditBalance); err != nil {
            return err
        }
    }

    return nil
}
```

**New Transaction Reason**:

```go
// Add to internal/types/wallet.go
const (
    // ... existing reasons ...
    TransactionReasonInvoiceCreditApplication TransactionReason = "INVOICE_CREDIT_APPLICATION"
)
```

**New Reference Type** (optional):

```go
// Add to internal/types/wallet.go
const (
    // ... existing types ...
    WalletTxReferenceTypeInvoice WalletTxReferenceType = "INVOICE"
)
```

### Invoice Totals Recalculation

After applying credits to line items:

1. Recalculate `Subtotal` = sum of all line item `Amount` fields (after credit reduction)
2. Calculate `TotalCreditsApplied` = sum of all `CreditsApplied` values (for reporting/audit)
3. Recalculate `Total` = Subtotal - TotalDiscount - TotalTax (if credits applied after coupons)
4. Update `AmountDue` = Total
5. Update `AmountRemaining` = AmountDue - AmountPaid

**Important**:

- `Subtotal` uses the reduced `Amount` values (after credits applied)
- `CreditsApplied` is tracked separately for accounting purposes
- Original line item amount = `Amount` + `CreditsApplied`
- This ensures proper accounting and reporting

### Complete End-to-End Credit Consumption Flow

**Step-by-Step Process**:

#### Step 1: Invoice Creation with Line Items

```
1. Create invoice with line items
2. Line items have full amounts (e.g., $100, $200, $300)
3. Identify USAGE arrear line items:
   - LineItem1: $100 (USAGE, arrear) ✓ Eligible
   - LineItem2: $200 (FIXED, arrear) ✗ Not eligible
   - LineItem3: $300 (USAGE, arrear) ✓ Eligible
   - LineItem4: $50 (USAGE, advance) ✗ Not eligible
```

#### Step 2: Get Available Credits

```
1. Get all active wallets for customer in invoice currency
2. For each wallet, find eligible credit transactions:
   - Wallet1 (Promotional):
     * Tx1: CreditsAvailable = 50, Priority = 1, Expiry = 2024-12-31
     * Tx2: CreditsAvailable = 100, Priority = 2, Expiry = 2024-12-31
   - Wallet2 (Prepaid):
     * Tx3: CreditsAvailable = 200, Priority = 1, Expiry = 2024-12-31
3. Total available: 350 credits
```

#### Step 3: Calculate Credits Needed

```
Eligible line items:
- LineItem1: $100 → needs 100 credits (assuming 1:1 conversion)
- LineItem3: $300 → needs 300 credits
Total needed: 400 credits
Available: 350 credits
Result: Can apply 350 credits (partial application)
```

#### Step 4: Consume Credits from Transactions (FIFO)

```
Processing order: Wallet1 (Promotional) first, then Wallet2 (Prepaid)

Wallet1 (Promotional):
- Tx1: Consume 50 credits → CreditsAvailable: 50 → 0
- Tx2: Consume 100 credits → CreditsAvailable: 100 → 0
- Total from Wallet1: 150 credits

Wallet2 (Prepaid):
- Tx3: Consume 200 credits → CreditsAvailable: 200 → 0
- Total from Wallet2: 200 credits

Total consumed: 350 credits
Remaining needed: 50 credits (cannot be fulfilled)
```

#### Step 5: Apply Credits to Line Items

```
Apply credits to eligible line items in order:

LineItem1 ($100):
- Credits to apply: min($100, 350) = $100
- Set CreditsApplied = $100
- Reduce Amount: $100 → $0
- Remaining credits: 350 - 100 = 250

LineItem3 ($300):
- Credits to apply: min($300, 250) = $250
- Set CreditsApplied = $250
- Reduce Amount: $300 → $50
- Remaining credits: 250 - 250 = 0

Final state:
- LineItem1: Amount = $0, CreditsApplied = $100
- LineItem2: Amount = $200, CreditsApplied = nil (not eligible)
- LineItem3: Amount = $50, CreditsApplied = $250
- LineItem4: Amount = $50, CreditsApplied = nil (not eligible)
```

#### Step 6: Create Debit Wallet Transactions

```
For Wallet1 (Promotional):
- Debit Transaction:
  * Type: DEBIT
  * CreditAmount: 150 credits
  * Amount: $150 (currency)
  * ReferenceID: invoice_id
  * ReferenceType: INVOICE
  * TransactionReason: INVOICE_CREDIT_APPLICATION
  * Metadata:
    - invoice_id: "inv_123"
    - line_item_ids: ["li_1", "li_3"]
    - credits_applied_per_item: {"li_1": 100, "li_3": 50}
    - source_transaction_ids: ["tx_1", "tx_2"]
  * CreditBalanceBefore: 150
  * CreditBalanceAfter: 0

For Wallet2 (Prepaid):
- Debit Transaction:
  * Type: DEBIT
  * CreditAmount: 200 credits
  * Amount: $200 (currency)
  * ReferenceID: invoice_id
  * ReferenceType: INVOICE
  * TransactionReason: INVOICE_CREDIT_APPLICATION
  * Metadata:
    - invoice_id: "inv_123"
    - line_item_ids: ["li_3"]
    - credits_applied_per_item: {"li_3": 200}
    - source_transaction_ids: ["tx_3"]
  * CreditBalanceBefore: 200
  * CreditBalanceAfter: 0
```

#### Step 7: Update Wallet Balances

```
Wallet1 (Promotional):
- CreditBalance: 150 → 0
- Balance: $150 → $0

Wallet2 (Prepaid):
- CreditBalance: 200 → 0
- Balance: $200 → $0
```

#### Step 8: Recalculate Invoice Totals

```
Line Item Amounts (after credits):
- LineItem1: $0
- LineItem2: $200
- LineItem3: $50
- LineItem4: $50

Subtotal = $0 + $200 + $50 + $50 = $300
TotalCreditsApplied = $100 + $250 = $350
Total = Subtotal - TotalDiscount - TotalTax
AmountDue = Total
```

#### Step 9: Save Invoice and Transactions

```
1. Update invoice line items with CreditsApplied values
2. Save invoice with updated totals
3. Save debit wallet transactions
4. Update wallet balances
5. All operations in a single database transaction
```

**Complete Flow Diagram**:

```
Invoice Creation
    ↓
Identify USAGE Arrear Line Items
    ↓
Get Active Wallets (Promotional → Prepaid)
    ↓
Find Eligible Credit Transactions (FIFO: Priority → Expiry → Creation Date)
    ↓
Calculate Total Available Credits
    ↓
Calculate Credits Needed for Eligible Line Items
    ↓
Consume Credits from Transactions (Update CreditsAvailable)
    ↓
Apply Credits to Line Items (Set CreditsApplied, Reduce Amount)
    ↓
Create Debit Wallet Transactions
    ↓
Update Wallet Balances
    ↓
Recalculate Invoice Totals
    ↓
Save Invoice & Transactions (Atomic Transaction)
    ↓
Invoice Finalized
```

### Credit Consumption Data Structures

**CreditConsumptionRecord**: Tracks which credits were consumed from which transactions

```go
type CreditConsumptionRecord struct {
    WalletID                string
    TransactionID           string          // Source credit transaction ID
    CreditsConsumed         decimal.Decimal
    CreditsAvailableBefore   decimal.Decimal
    CreditsAvailableAfter    decimal.Decimal
}
```

**CreditApplicationResult**: Result of applying credits to an invoice

```go
type CreditApplicationResult struct {
    TotalCreditsApplied     decimal.Decimal
    LineItemCredits         map[string]decimal.Decimal // lineItemID -> credits applied
    ConsumptionRecords       []*CreditConsumptionRecord
    WalletsUsed             []string // wallet IDs that provided credits
    InsufficientCredits     bool
    RemainingCreditsNeeded   decimal.Decimal // if insufficient
}
```

**Transaction Metadata Structure**:

```json
{
  "invoice_id": "inv_123",
  "line_item_ids": ["li_1", "li_3"],
  "credits_applied_per_item": {
    "li_1": 100,
    "li_3": 250
  },
  "source_transaction_ids": ["tx_1", "tx_2", "tx_3"],
  "wallet_allocation": {
    "wallet_1": 150,
    "wallet_2": 200
  }
}
```

### Credit Consumption Edge Cases

1. **Partial Transaction Consumption**:

   - If a credit transaction has 100 credits available but only 50 are needed
   - Consume 50 credits, update `CreditsAvailable` from 100 to 50
   - Transaction remains active with 50 credits still available

2. **Multiple Transactions for One Line Item**:

   - Line item needs 200 credits
   - Tx1 has 50 credits, Tx2 has 100 credits, Tx3 has 100 credits
   - Consume from all three transactions
   - Track all three transaction IDs in debit transaction metadata

3. **Multiple Wallets for One Line Item**:

   - Line item needs 300 credits
   - Wallet1 provides 150 credits, Wallet2 provides 150 credits
   - Create two debit transactions (one per wallet)
   - Both reference the same line item in metadata

4. **Expired Credits**:

   - Credits with `ExpiryDate` in the past are excluded
   - Only non-expired credits are considered
   - Expired credits remain in transactions but are not consumed

5. **Insufficient Credits**:

   - If total available credits < total needed
   - Apply credits to line items in FIFO order until credits exhausted
   - Remaining line items stay at full amount
   - Invoice is still created (partial credit application)

6. **Credit Transaction Priority**:
   - Lower priority number = higher priority
   - Priority 1 credits consumed before Priority 2
   - If same priority, earliest expiry consumed first
   - If same priority and expiry, oldest transaction consumed first

## Impact Analysis

### Files to Modify

1. **Type Definitions**:

   - `internal/types/wallet.go`: Remove `AllowedPriceTypes` from `WalletConfig`
   - `internal/types/wallet.go`: Remove `WalletConfigPriceType` type and constants
   - `internal/types/wallet.go`: Update `GetDefaultWalletConfig()`

2. **Service Layer**:

   - `internal/service/invoice.go`: Add credit application logic
   - `internal/service/billing.go`: Apply credits during invoice request creation
   - `internal/service/wallet.go`:
     - Remove price type restriction logic
     - Add `ConsumeCreditsForInvoice()` method
     - Add `CreateDebitTransactionsForCredits()` method
   - `internal/service/wallet_payment.go`: **DEPRECATE** - no longer needed for invoice payment
   - `internal/service/subscription_payment_processor.go`: Remove wallet payment logic
   - `internal/service/creditgrant.go`: Remove `AllowedPriceTypes` from auto-created wallets
   - **NEW**: `internal/service/credit_application.go`: New service for credit application logic
     - `ApplyCreditsToInvoice()` method
     - `FilterUsageArrearLineItems()` method
     - `ConsumeCreditsFromTransactions()` method
     - `AllocateCreditsToLineItems()` method

3. **Repository Layer**:

   - `internal/repository/ent/wallet.go`:
     - Remove `AllowedPriceTypes` handling
     - Ensure `FindEligibleCredits()` supports FIFO ordering (priority → expiry → creation date)
     - Ensure `ConsumeCredits()` properly updates `CreditsAvailable` field
     - Add method to create debit transactions for invoice credit applications

4. **Domain Models**:

   - `internal/domain/wallet/model.go`: Remove `AllowedPriceTypes` field
   - `internal/domain/invoice/line_item.go`: **ADD** `CreditsApplied *decimal.Decimal` field

5. **DTOs**:

   - `internal/api/dto/wallet.go`: Remove `AllowedPriceTypes` from create/update requests
   - `internal/api/dto/invoice.go`: Add `credits_applied` field to line item responses

6. **Types**:

   - `internal/types/wallet.go`:
     - Remove `WalletConfigPriceType` and related constants
     - Add `TransactionReasonInvoiceCreditApplication` constant
     - Add `WalletTxReferenceTypeInvoice` constant (optional)

7. **Database Schema**:
   - Add `credits_applied` column to `invoice_line_items` table (nullable decimal)
   - Migration needed to add this column

### Breaking Changes

1. **API Changes**:

   - `POST /api/v1/wallets`: Remove `config.allowed_price_types` field
   - `PUT /api/v1/wallets/:id`: Remove `config.allowed_price_types` field
   - Response will no longer include `allowed_price_types`

2. **Behavior Changes**:

   - Wallets can no longer be restricted by price type
   - Credits are automatically applied during invoice creation (no separate payment step)
   - Credits **only apply to USAGE arrear charges** (not FIXED arrear or advance charges)
   - Line items now track `credits_applied` for accounting purposes

3. **Database Changes**:
   - Remove `allowed_price_types` column from wallet config JSON field
   - **ADD** `credits_applied` column to `invoice_line_items` table (nullable decimal)
   - Migration needed to:
     - Remove `allowed_price_types` from existing wallets
     - Add `credits_applied` column to invoice_line_items table
     - Set default value to `NULL` or `0` for existing line items

### Edge Cases

1. **Insufficient Credits**:

   - If credits < total USAGE arrear charges, apply credits to first USAGE arrear line items (FIFO)
   - Remaining USAGE arrear line items stay at full amount
   - FIXED arrear line items are never affected (always stay at full amount)

2. **Multiple Wallets**:

   - Use promotional wallets first, then prepaid
   - Or use balance-optimized strategy
   - Need to track which wallet provided which credit

3. **Partial Credit Application**:

   - If a USAGE arrear line item is $100 and only $50 credits available:
     - Set `CreditsApplied = $50`
     - Reduce `Amount` from $100 to $50
     - Create wallet transaction for $50
   - FIXED arrear line items are never affected, even if credits remain

4. **Invoice Recalculation**:

   - If invoice is recalculated, credits should be re-applied
   - Need to handle case where credits were already consumed
   - May need to reverse previous credit applications
   - Reset `CreditsApplied` to `nil` before reapplication
   - Recalculate based on current available credits and current line item amounts

5. **Draft Invoices**:

   - Credits should be applied when invoice is finalized
   - Not applied during draft creation (to allow editing)

6. **One-Off Invoices**:

   - One-off invoices don't have subscription line items
   - No arrear classification
   - **Decision**: Should credits apply to one-off invoices?
   - **Recommendation**: No, only subscription invoices with USAGE arrear charges

7. **FIXED Arrear Charges**:

   - FIXED arrear charges are explicitly excluded from credit application
   - Even if they have `InvoiceCadence == InvoiceCadenceArrear`
   - Only USAGE arrear charges are eligible
   - FIXED charges always remain at full amount

8. **Credit Expiry**:

   - Credits with expiry dates should be prioritized (use oldest first)
   - Or exclude expired credits from application

9. **Currency Mismatch**:

   - Only apply credits from wallets matching invoice currency
   - Same as current behavior

10. **Invoice Voiding**:

    - If invoice is voided, credits should be refunded to wallets
    - Use `CreditsApplied` field to determine how much to refund per line item
    - Create reverse wallet transactions for total `CreditsApplied` amount
    - Reset `CreditsApplied` to `nil` after voiding

11. **Invoice Updates**:

    - If invoice line items are updated, credits may need to be reapplied
    - Complex scenario - may need to prevent updates after credit application
    - If line item amount increases, `CreditsApplied` remains the same (doesn't increase)
    - If line item amount decreases below `CreditsApplied`, adjust `CreditsApplied` to match new amount

12. **Credits Applied Tracking**:
    - `CreditsApplied` field must always be <= original line item amount
    - Sum of all `CreditsApplied` values = total credits consumed for invoice
    - Used for reporting, audit trail, and invoice PDF generation
    - Must be preserved during invoice recalculation and updates

## Implementation Phases

### Phase 1: Remove AllowedPriceTypes (Non-Breaking)

1. Mark `AllowedPriceTypes` as deprecated in code
2. Update API documentation
3. Add migration to remove field from existing wallets (set to default)
4. Update all code that uses `AllowedPriceTypes` to treat nil/empty as "all allowed"

### Phase 2: Add Credits Applied Field

1. **Database Migration**:

   - Add `credits_applied` column to `invoice_line_items` table
   - Set default to `NULL` for existing records

2. **Domain Model Update**:

   - Add `CreditsApplied *decimal.Decimal` to `InvoiceLineItem` struct
   - Update `FromEnt()` method to map the new field
   - Add validation for `CreditsApplied` field

3. **Repository Update**:
   - Update ent schema to include `credits_applied` field
   - Regenerate ent code

### Phase 3: Implement Credit Application Logic

1. Create `CreditApplicationService`:

   - `ApplyCreditsToInvoice()` method
   - `FilterUsageArrearLineItems()` method (USAGE only)
   - `AllocateCreditsToLineItems()` method (FIFO strategy)
   - `CalculateTotalCreditsApplied()` method

2. Integrate into invoice creation flow:

   - Add credit application after line item creation
   - Before coupon application (or after, based on decision)
   - Set `CreditsApplied` field on eligible line items

3. Integrate into invoice finalization:
   - Apply credits if not already applied
   - Update `CreditsApplied` field on line items

### Phase 4: Update Wallet Transaction Creation

1. Move wallet transaction creation from payment processing to invoice creation
2. Update transaction reason and metadata
3. Handle multiple wallet allocation

### Phase 5: Remove Payment Processing Logic

1. Deprecate `wallet_payment.go` service
2. Remove wallet payment logic from subscription payment processor
3. Update balance calculation logic (remove price type restrictions)

### Phase 6: Testing & Migration

1. Comprehensive testing:

   - Unit tests for credit application logic
   - Integration tests for invoice creation with credits
   - Edge case testing

2. Data migration:

   - Remove `allowed_price_types` from existing wallets
   - Ensure no breaking changes for existing invoices

3. Documentation:
   - Update API documentation
   - Update user guides
   - Update internal documentation

## Success Metrics

1. **Functional**:

   - Credits successfully applied to **USAGE arrear charges only** (not FIXED)
   - Line item amounts correctly reduced
   - `CreditsApplied` field correctly populated and tracked
   - Invoice totals correctly calculated (accounting for credits_applied)
   - Wallet transactions correctly created
   - FIXED arrear charges remain unaffected

2. **Performance**:

   - Invoice creation time not significantly increased
   - Credit application logic completes in < 100ms

3. **User Experience**:

   - Simpler wallet configuration (no price type restrictions)
   - Automatic credit application (no manual payment step)
   - Clear invoice line items showing reduced amounts and credits applied
   - Transparent tracking of credit usage per line item

4. **Accounting & Reporting**:
   - `CreditsApplied` field enables accurate credit usage reporting
   - Proper audit trail of credit applications
   - Support for invoice recalculation and voiding scenarios

## Open Questions

1. **Credit Application Priority**: Before or after coupons? (DECIDED: After coupons)
2. **Credit Allocation Strategy**: FIFO, proportional, or largest first? (DECIDED: FIFO)
3. **One-Off Invoices**: Should credits apply to one-off invoices? (DECIDED: No)
4. **Invoice Recalculation**: How to handle credit reapplication? (Need to reverse and reapply)
5. **Partial Application**: Should we allow partial credit application or require full coverage? (DECIDED: Allow partial)
6. **Credit Expiry**: How to prioritize credits with expiry dates? (DECIDED: FIFO - oldest first)
7. **Multiple Wallets**: Which allocation strategy for multiple wallets? (DECIDED: Promotional first, then prepaid)
8. **Credits Applied Display**: How to show credits_applied in invoice PDFs and UI? (Show as separate line or reduce amount?)

## Risks & Mitigation

1. **Risk**: Breaking existing invoices that used wallet payments

   - **Mitigation**: Phase 1 ensures backward compatibility, gradual rollout

2. **Risk**: Performance impact of credit application during invoice creation

   - **Mitigation**: Optimize credit lookup and application logic, add caching

3. **Risk**: Complex edge cases in credit allocation

   - **Mitigation**: Start with simple FIFO strategy, iterate based on feedback

4. **Risk**: Data migration issues

   - **Mitigation**: Thorough testing, rollback plan, staged migration

5. **Risk**: User confusion with automatic credit application
   - **Mitigation**: Clear documentation, UI indicators, audit trail

## Timeline Estimate

- **Phase 1**: 1-2 weeks (remove AllowedPriceTypes)
- **Phase 2**: 1 week (add credits_applied field - database, domain, repository)
- **Phase 3**: 2-3 weeks (implement credit application logic)
- **Phase 4**: 1 week (update wallet transactions)
- **Phase 5**: 1 week (remove payment logic)
- **Phase 6**: 2-3 weeks (testing & migration)

**Total**: 8-11 weeks

## Dependencies

1. Decision on credit application priority (before/after coupons)
2. Decision on credit allocation strategy
3. Decision on one-off invoice handling
4. Database migration plan approval
5. API versioning strategy (if needed)

## Summary of Key Changes

### Core Requirements

1. **USAGE Arrear Charges Only**: Credits apply exclusively to line items that are:

   - USAGE price type (`PriceType == PRICE_TYPE_USAGE`)
   - Arrear cadence (`InvoiceCadence == InvoiceCadenceArrear`)
   - Part of a subscription invoice (not one-off)

2. **Credits Applied Field**: New field on `InvoiceLineItem`:

   - `CreditsApplied *decimal.Decimal` - tracks amount of credits applied
   - Used for accounting, reporting, and audit trail
   - Original amount = `Amount` + `CreditsApplied`

3. **Discount Instrument**: Credits reduce line item amounts directly:

   - Applied during invoice creation/finalization
   - Reduces `Amount` field on line items
   - Tracks reduction in `CreditsApplied` field
   - Applied before tax calculation

4. **Remove AllowedPriceTypes**: No longer needed:
   - Wallet configuration simplified
   - Restriction is at application level (USAGE arrear only)
   - All existing price type restriction logic removed

### Implementation Highlights

- **Database**: Add `credits_applied` column to `invoice_line_items` table
- **Domain**: Add `CreditsApplied` field to `InvoiceLineItem` struct
- **Service**: New credit application service with USAGE arrear filtering
- **Algorithm**: FIFO allocation strategy for credits
- **Accounting**: Proper tracking of credits applied per line item

## Related Documents

- `ALLOWED_PRICE_TYPES_ANALYSIS.md` - Current implementation analysis
- `invoice/invoice_lifecycle.md` - Invoice creation and finalization flow
- `billing-engine-design.md` - Billing engine architecture
- `advance_arrear_billing_implementation.md` - Arrear charge classification
- `coupon_service.md` - Coupon application logic (similar pattern)
