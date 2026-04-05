package flexprice

import (
	"fmt"
	"math/rand"
	"regexp"
	"time"
)

// ---------------------------------------------------------------------------
// Error classification helpers
// ---------------------------------------------------------------------------

// IsNotFound reports whether the error is an API "not_found" error (HTTP 404).
func IsNotFound(err error) bool { return hasErrorCode(err, "not_found") }

// IsValidation reports whether the error is an API "validation_error" (HTTP 400).
func IsValidation(err error) bool { return hasErrorCode(err, "validation_error") }

// IsAlreadyExists reports whether the error is an API "already_exists" error (HTTP 409).
func IsAlreadyExists(err error) bool { return hasErrorCode(err, "already_exists") }

// IsPermissionDenied reports whether the error is an API "permission_denied" error (HTTP 403).
func IsPermissionDenied(err error) bool { return hasErrorCode(err, "permission_denied") }

// IsRateLimit reports whether the error is an API "service_unavailable" error (HTTP 503).
func IsRateLimit(err error) bool { return hasErrorCode(err, "service_unavailable") }

// IsInvalidOperation reports whether the error is an API "invalid_operation" error (HTTP 400).
func IsInvalidOperation(err error) bool { return hasErrorCode(err, "invalid_operation") }

// IsVersionConflict reports whether the error is an API "version_conflict" error (HTTP 409).
func IsVersionConflict(err error) bool { return hasErrorCode(err, "version_conflict") }

// hasErrorCode checks if err is an API error with the given code.
// Uses a local interface so no SDK import is needed — Speakeasy generates GetCode() on all error types.
func hasErrorCode(err error, code string) bool {
	if err == nil {
		return false
	}
	type withCode interface {
		GetCode() string
	}
	if c, ok := err.(withCode); ok {
		return c.GetCode() == code
	}
	return false
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// CustomHelpers provides utility functions for the FlexPrice Go SDK
type CustomHelpers struct{}

// FormatCurrency formats currency amount with proper formatting
func (h *CustomHelpers) FormatCurrency(amount float64, currency string) string {
	if currency == "USD" {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// GenerateID generates a unique ID with optional prefix
func (h *CustomHelpers) GenerateID(prefix string) string {
	if prefix == "" {
		prefix = "id"
	}
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	randomStr := generateRandomString(9)
	return fmt.Sprintf("%s_%d_%s", prefix, timestamp, randomStr)
}

// IsValidEmail validates email format
func (h *CustomHelpers) IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	return emailRegex.MatchString(email)
}

// FormatDate formats date to ISO string
func (h *CustomHelpers) FormatDate(t time.Time) string {
	return t.Format(time.RFC3339)
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// NewCustomHelpers creates a new instance of CustomHelpers
func NewCustomHelpers() *CustomHelpers {
	return &CustomHelpers{}
}
