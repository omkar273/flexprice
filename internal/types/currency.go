package types

import (
	"database/sql/driver"
	"encoding/json"
	"strings"

	"golang.org/x/text/currency"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Currency is a string-based type that represents an ISO 4217 currency code
// It provides methods for validation, normalization, and accessing currency properties
type Currency string

// CurrencyConfig holds configuration for different currencies and their symbols
var CURRENCY_CONFIG = map[string]CurrencyConfig{
	"usd": {Symbol: "$", Precision: 2},
	"eur": {Symbol: "€", Precision: 2},
	"gbp": {Symbol: "£", Precision: 2},
	"aud": {Symbol: "AUS", Precision: 2},
	"cad": {Symbol: "CAD", Precision: 2},
	"jpy": {Symbol: "¥", Precision: 0},
	"inr": {Symbol: "₹", Precision: 2},
	"idr": {Symbol: "Rp", Precision: 2},
	"sgd": {Symbol: "S$", Precision: 2},
	"thb": {Symbol: "฿", Precision: 2},
	"myr": {Symbol: "RM", Precision: 2},
	"php": {Symbol: "₱", Precision: 2},
	"vnd": {Symbol: "₫", Precision: 0},
	"hkd": {Symbol: "HK$", Precision: 2},
	"krw": {Symbol: "₩", Precision: 0},
	"nzd": {Symbol: "NZ$", Precision: 2},
	"brl": {Symbol: "R$", Precision: 2},
	"chf": {Symbol: "CHF", Precision: 2},
	"clp": {Symbol: "CLP$", Precision: 0},
	"cny": {Symbol: "CN¥", Precision: 2},
	"czk": {Symbol: "CZK", Precision: 2},
	"dkk": {Symbol: "DKK", Precision: 2},
	"huf": {Symbol: "HUF", Precision: 2},
	"ils": {Symbol: "₪", Precision: 2},
	"mxn": {Symbol: "MX$", Precision: 2},
	"nok": {Symbol: "NOK", Precision: 2},
	"pln": {Symbol: "PLN", Precision: 2},
	"ron": {Symbol: "RON", Precision: 2},
	"rub": {Symbol: "₽", Precision: 2},
	"sar": {Symbol: "SAR", Precision: 2},
	"sek": {Symbol: "SEK", Precision: 2},
	"try": {Symbol: "TRY", Precision: 2},
	"twd": {Symbol: "NT$", Precision: 2},
	"zar": {Symbol: "ZAR", Precision: 2},
	// TODO add more currencies later
}

type CurrencyConfig struct {
	Precision int32
	Symbol    string
}

const (
	DEFAULT_PRECISION = 2
)

// GetCurrencySymbol returns the symbol for a given currency code
// if the code is not found, it returns the code itself
// Now delegates to Currency type
func GetCurrencySymbol(code string) string {
	return Currency(code).Symbol()
}

// GetCurrencyPrecision returns the precision for a given currency code
// if the code is not found, it returns the default precision of 2
// Now delegates to Currency type
func GetCurrencyPrecision(code string) int32 {
	return int32(Currency(code).Precision())
}

func GetCurrencyConfig(code string) CurrencyConfig {
	if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
		return config
	}
	return CurrencyConfig{Precision: DEFAULT_PRECISION}
}

func IsMatchingCurrency(a, b string) bool {
	return strings.EqualFold(a, b)
}

// String returns the normalized uppercase currency code (implements fmt.Stringer)
func (c Currency) String() string {
	return strings.ToUpper(string(c))
}

// Validate validates the currency code against ISO 4217 standard
func (c Currency) Validate() error {
	code := strings.TrimSpace(string(c))
	if code == "" {
		return ierr.NewError("currency code cannot be empty").
			WithHint("Currency code is required").
			Mark(ierr.ErrValidation)
	}

	normalized := strings.ToUpper(code)
	_, err := currency.ParseISO(normalized)
	if err != nil {
		return ierr.NewError("invalid currency code").
			WithHint("Currency code must be a valid ISO 4217 code").
			WithReportableDetails(map[string]interface{}{
				"currency_code": code,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// IsValid returns true if the currency code is valid
func (c Currency) IsValid() bool {
	return c.Validate() == nil
}

// Precision returns the number of decimal places for the currency
// Uses ISO standard via golang.org/x/text/currency
func (c Currency) Precision() int {
	code := strings.ToUpper(strings.TrimSpace(string(c)))
	if code == "" {
		return int(DEFAULT_PRECISION)
	}

	unit, err := currency.ParseISO(code)
	if err != nil {
		// Fallback to CURRENCY_CONFIG if ISO lookup fails
		if config, ok := CURRENCY_CONFIG[strings.ToLower(code)]; ok {
			return int(config.Precision)
		}
		return int(DEFAULT_PRECISION)
	}

	// Use Standard kind to get fraction digits (precision)
	scale, _ := currency.Standard.Rounding(unit)
	return int(scale)
}

// Symbol returns the currency symbol from CURRENCY_CONFIG
// Falls back to uppercase code if symbol not found
func (c Currency) Symbol() string {
	code := strings.ToLower(strings.TrimSpace(string(c)))
	if config, ok := CURRENCY_CONFIG[code]; ok {
		return config.Symbol
	}
	return strings.ToUpper(string(c))
}

// Normalize returns a normalized uppercase Currency
func (c Currency) Normalize() Currency {
	code := strings.TrimSpace(string(c))
	if code == "" {
		return Currency("")
	}
	return Currency(strings.ToUpper(code))
}

// Equal compares two currencies (case-insensitive)
func (c Currency) Equal(other Currency) bool {
	return strings.EqualFold(string(c), string(other))
}

// NewCurrency creates a Currency from a string, validates and normalizes it
func NewCurrency(code string) (Currency, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Currency(""), ierr.NewError("currency code cannot be empty").
			WithHint("Currency code is required").
			Mark(ierr.ErrValidation)
	}

	normalized := strings.ToUpper(code)
	_, err := currency.ParseISO(normalized)
	if err != nil {
		return Currency(""), ierr.NewError("invalid currency code").
			WithHint("Currency code must be a valid ISO 4217 code").
			WithReportableDetails(map[string]interface{}{
				"currency_code": code,
			}).
			Mark(ierr.ErrValidation)
	}

	return Currency(normalized), nil
}

// MustCurrency creates a Currency from a string and panics if invalid
// Useful for tests and known valid values
func MustCurrency(code string) Currency {
	curr, err := NewCurrency(code)
	if err != nil {
		panic(err)
	}
	return curr
}

// ParseCurrency is an alias for NewCurrency
func ParseCurrency(code string) (Currency, error) {
	return NewCurrency(code)
}

// Scan implements the database/sql.Scanner interface for reading from database
func (c *Currency) Scan(value interface{}) error {
	if value == nil {
		*c = Currency("")
		return nil
	}

	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return ierr.NewError("invalid type for currency").
			WithHint("Currency must be a string").
			Mark(ierr.ErrValidation)
	}

	// Normalize and validate
	normalized := strings.ToUpper(strings.TrimSpace(str))
	if normalized == "" {
		*c = Currency("")
		return nil
	}

	// Validate against ISO standard
	_, err := currency.ParseISO(normalized)
	if err != nil {
		// If invalid, still store it but log the issue
		// This allows reading existing invalid data
		*c = Currency(normalized)
		return nil
	}

	*c = Currency(normalized)
	return nil
}

// Value implements the database/sql/driver.Valuer interface for writing to database
func (c Currency) Value() (driver.Value, error) {
	code := strings.TrimSpace(string(c))
	if code == "" {
		return "", nil
	}
	return strings.ToUpper(code), nil
}

// MarshalJSON implements json.Marshaler interface
// Serializes Currency as uppercase string
func (c Currency) MarshalJSON() ([]byte, error) {
	code := strings.TrimSpace(string(c))
	if code == "" {
		return json.Marshal("")
	}
	return json.Marshal(strings.ToUpper(code))
}

// UnmarshalJSON implements json.Unmarshaler interface
// Accepts string in any case, validates and normalizes to uppercase
func (c *Currency) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return ierr.WithError(err).
			WithHint("Currency must be a string").
			Mark(ierr.ErrValidation)
	}

	// Normalize and validate
	normalized := strings.ToUpper(strings.TrimSpace(str))
	if normalized == "" {
		*c = Currency("")
		return nil
	}

	// Validate against ISO standard
	_, err := currency.ParseISO(normalized)
	if err != nil {
		return ierr.NewError("invalid currency code").
			WithHint("Currency code must be a valid ISO 4217 code").
			WithReportableDetails(map[string]interface{}{
				"currency_code": str,
			}).
			Mark(ierr.ErrValidation)
	}

	*c = Currency(normalized)
	return nil
}

// ValidateCurrencyCode validates a currency code (backward compatibility)
// Deprecated: Use Currency.Validate() instead
func ValidateCurrencyCode(currency string) error {
	return Currency(currency).Validate()
}
