# AllowedPriceTypes Usage and Configuration Analysis

## Overview

`AllowedPriceTypes` is a configuration field in `WalletConfig` that restricts which price types (USAGE, FIXED, or ALL) a wallet can be used to pay for. This allows fine-grained control over wallet usage based on price type.

## Type Definition

**Location**: `internal/types/wallet.go` (lines 285-297)

```go
type WalletConfigPriceType string

const (
    WalletConfigPriceTypeAll   WalletConfigPriceType = "ALL"
    WalletConfigPriceTypeUsage WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_USAGE)  // "USAGE"
    WalletConfigPriceTypeFixed WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_FIXED)  // "FIXED"
)

type WalletConfig struct {
    // AllowedPriceTypes is a list of price types that are allowed for the wallet
    // nil means all price types are allowed
    AllowedPriceTypes []WalletConfigPriceType `json:"allowed_price_types,omitempty"`
}
```

**Default Behavior**:

- If `nil` or empty → ALL price types are allowed (default behavior)
- Default config uses `WalletConfigPriceTypeAll` (see `GetDefaultWalletConfig()`)

## Configuration Points

### 1. Wallet Creation

**Location**: `internal/api/dto/wallet.go` (lines 137-139)

When creating a wallet via `CreateWalletRequest`:

- If `Config` is `nil`, it defaults to `GetDefaultWalletConfig()` which sets `AllowedPriceTypes: [WalletConfigPriceTypeAll]`
- Users can explicitly set `Config.AllowedPriceTypes` in the request
- The config is validated via `WalletConfig.Validate()`

**API Endpoint**: `POST /api/v1/wallets`

- Request body includes optional `config.allowed_price_types` field
- Can be set to `["ALL"]`, `["USAGE"]`, `["FIXED"]`, `["USAGE", "FIXED"]`, or `null` (allows all)

### 2. Wallet Update

**Location**: `internal/service/wallet.go` (lines 957-959)

When updating a wallet via `UpdateWalletRequest`:

- `Config` field is optional
- If provided, it replaces the existing config
- The config is validated before update

**API Endpoint**: `PUT /api/v1/wallets/:id`

- Request body includes optional `config.allowed_price_types` field

### 3. Automatic Wallet Creation (Credit Grants)

**Location**: `internal/service/creditgrant.go` (lines 414-417)

When a wallet is automatically created for subscription credit grants:

```go
Config: &types.WalletConfig{
    AllowedPriceTypes: []types.WalletConfigPriceType{
        types.WalletConfigPriceTypeUsage,  // Only USAGE allowed
    },
},
```

**Note**: Subscription credit grant wallets are restricted to USAGE price types only.

## Usage Points

### 1. Wallet Balance Calculation (Real-time Balance)

**Location**: `internal/service/wallet.go` (lines 787-789, 1793-1795)

Used in two methods:

- `GetWalletBalance()` - line 787
- `GetWalletRealTimeBalance()` - line 1793

**Logic**:

```go
shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
    lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
    lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)
```

**Purpose**: Determines if usage-based charges should be included in pending balance calculations. If the wallet doesn't allow USAGE prices, usage charges are excluded from the balance calculation.

### 2. Wallet Payment Processing - Wallet Categorization

**Location**: `internal/service/wallet_payment.go` (lines 154-188)

**Function**: `categorizeWalletsByPriceType()`

**Logic**:

- Categorizes wallets into three groups: `usageWallets`, `fixedWallets`, `allWallets`
- If `AllowedPriceTypes` is empty → treat as ALL
- If contains `WalletConfigPriceTypeAll` → add to `allWallets`
- If contains both USAGE and FIXED → add to `allWallets`
- If only USAGE → add to `usageWallets`
- If only FIXED → add to `fixedWallets`

**Purpose**: Optimizes payment processing by matching wallets to appropriate price types, minimizing wallet usage.

### 3. Wallet Payment Processing - Allowed Amount Calculation

**Location**: `internal/service/wallet_payment.go` (lines 336-372)

**Function**: `calculateAllowedPaymentAmount()`

**Logic**:

```go
if len(w.Config.AllowedPriceTypes) == 0 {
    // Can pay for everything (up to balance)
    return decimal.Min(remainingAmount, w.Balance)
}

for _, allowedPriceType := range w.Config.AllowedPriceTypes {
    switch allowedPriceType {
    case types.WalletConfigPriceTypeAll:
        return decimal.Min(remainingAmount, w.Balance)
    case types.WalletConfigPriceTypeUsage:
        allowedAmount += priceTypeAmounts["USAGE"]
    case types.WalletConfigPriceTypeFixed:
        allowedAmount += priceTypeAmounts["FIXED"]
    }
}
return decimal.Min(allowedAmount, w.Balance)
```

**Purpose**: Calculates how much a wallet can pay based on its price type restrictions and remaining amounts by price type.

### 4. Wallet Payment Processing - Amount Deduction

**Location**: `internal/service/wallet_payment.go` (lines 374-410)

**Function**: `updatePriceTypeAmountsAfterPayment()`

**Logic**:

- If `AllowedPriceTypes` is empty → can deduct from any price type
- If contains `WalletConfigPriceTypeAll` → can deduct from any price type
- Otherwise, only deducts from allowed price types (USAGE or FIXED)

**Purpose**: Updates remaining amounts after a payment, ensuring deductions only occur from allowed price types.

### 5. Subscription Payment Processing

**Location**: `internal/service/subscription_payment_processor.go` (lines 666-696)

**Function**: `calculateWalletAllowedAmount()`

**Logic**:

```go
if len(wallet.Config.AllowedPriceTypes) == 0 {
    // Can pay for everything
    return sum of all priceTypeAmounts
}

for _, allowedType := range wallet.Config.AllowedPriceTypes {
    if allowedType == "ALL" {
        return sum of all priceTypeAmounts
    } else if amount exists for allowedType {
        allowedAmount += amount
    }
}
```

**Purpose**: Calculates how much a wallet can contribute to subscription payment based on price type restrictions.

### 6. Repository - Wallet Update

**Location**: `internal/repository/ent/wallet.go` (lines 849-851)

**Logic**:

```go
if w.Config.AllowedPriceTypes != nil {
    update.SetConfig(w.Config)
}
```

**Purpose**: Persists the `AllowedPriceTypes` configuration to the database when updating a wallet.

## Validation

**Location**: `internal/types/wallet.go` (lines 306-327)

**Function**: `WalletConfig.Validate()`

**Rules**:

- Validates each price type in `AllowedPriceTypes` against allowed values: `["ALL", "USAGE", "FIXED"]`
- Returns validation error if any invalid price type is found
- `nil` or empty slice is valid (means all types allowed)

## Summary of Behavior

| AllowedPriceTypes Value | Behavior                                                       |
| ----------------------- | -------------------------------------------------------------- |
| `nil` or `[]`           | **ALL price types allowed** (default behavior)                 |
| `["ALL"]`               | All price types allowed                                        |
| `["USAGE"]`             | Only USAGE prices allowed                                      |
| `["FIXED"]`             | Only FIXED prices allowed                                      |
| `["USAGE", "FIXED"]`    | Both USAGE and FIXED allowed (treated as ALL in some contexts) |

## Key Use Cases

1. **Subscription Credit Grants**: Automatically created wallets are restricted to USAGE only
2. **Balance Calculations**: Usage charges are excluded from balance if wallet doesn't allow USAGE
3. **Payment Processing**: Wallets are matched to appropriate price types to optimize payment allocation
4. **User Control**: Users can create wallets with specific price type restrictions via API

## Files Modified/Using AllowedPriceTypes

1. `internal/types/wallet.go` - Type definition and validation
2. `internal/api/dto/wallet.go` - DTOs for create/update requests
3. `internal/service/wallet.go` - Balance calculations
4. `internal/service/wallet_payment.go` - Payment processing logic
5. `internal/service/subscription_payment_processor.go` - Subscription payment processing
6. `internal/service/creditgrant.go` - Automatic wallet creation
7. `internal/repository/ent/wallet.go` - Database persistence
