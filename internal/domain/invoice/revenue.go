package invoice

import "github.com/shopspring/decimal"

// RevenueByCustomerRow represents a single row from the revenue aggregation query,
// grouped by customer_id and price_type.
type RevenueByCustomerRow struct {
	CustomerID string
	PriceType  string // "USAGE" or "FIXED"
	Amount     decimal.Decimal
}

// VoiceMinutesRow represents a single row from the voice minutes aggregation query,
// grouped by customer_id.
type VoiceMinutesRow struct {
	CustomerID string
	UsageMs    decimal.Decimal // raw milliseconds from SUM(quantity)
}
