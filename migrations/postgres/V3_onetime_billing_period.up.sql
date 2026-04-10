-- Normalize all one-time prices to use billing_period=ONETIME instead of billing_cadence=ONETIME.
-- After this migration every price row has billing_cadence='RECURRING'.
UPDATE prices
SET billing_cadence = 'RECURRING',
    billing_period  = 'ONETIME'
WHERE billing_cadence = 'ONETIME';
