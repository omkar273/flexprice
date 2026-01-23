-- Normalize currency codes to uppercase in all tables
-- This migration ensures all existing currency codes are stored in uppercase format
-- The Currency type will handle normalization going forward

UPDATE prices SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE wallets SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE subscriptions SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE subscription_line_items SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE invoices SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE invoice_line_items SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE payments SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE coupons SET currency = UPPER(currency) WHERE currency IS NOT NULL AND currency != UPPER(currency);
UPDATE coupon_applications SET currency = UPPER(currency) WHERE currency IS NOT NULL AND currency != UPPER(currency);
UPDATE wallet_transactions SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE tax_applied SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE tax_associations SET currency = UPPER(currency) WHERE currency IS NOT NULL AND currency != UPPER(currency);
UPDATE credit_notes SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE credit_note_line_items SET currency = UPPER(currency) WHERE currency != UPPER(currency);
UPDATE price_units SET base_currency = UPPER(base_currency) WHERE base_currency != UPPER(base_currency);
