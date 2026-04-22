-- Rename trial columns for parity with Ent schema (trial_period_days).
ALTER TABLE prices RENAME COLUMN trial_period TO trial_period_days;
ALTER TABLE subscription_line_items RENAME COLUMN trial_period TO trial_period_days;
