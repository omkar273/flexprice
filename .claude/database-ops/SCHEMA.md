# FlexPrice Database Schema Reference

Quick reference for both PostgreSQL (Ent ORM) and ClickHouse. All PG tables have `tenant_id`, `environment_id`, `status` ('published'/'deleted'), `created_at`, `updated_at` from `BaseMixin`.

---

## PostgreSQL Tables

### customers
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | PK |
| external_id | varchar(255) | customer's own ID |
| name, email | varchar(255) | |
| address_* | varchar | line1/line2/city/state/postal_code/country |
| metadata | jsonb | |

Indexes: `UNIQUE(tenant_id, env_id, external_id)` WHERE published

### subscriptions
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | PK |
| customer_id, plan_id | varchar(50) | NOT NULL, IMMUTABLE |
| subscription_status | varchar(50) | active\|paused\|cancelled\|incomplete\|trialing\|draft |
| currency | varchar(10) | |
| billing_cadence | varchar | |
| billing_period | varchar | MONTHLY\|ANNUAL\|WEEKLY\|DAILY\|QUARTERLY\|SEMI_ANNUAL |
| billing_period_count | int | default 1 |
| billing_cycle | varchar(50) | anniversary\|calendar |
| invoice_cadence | varchar(20) | ARREAR\|ADVANCE |
| start_date, end_date | timestamp | |
| current_period_start, current_period_end | timestamp | |
| billing_anchor | timestamp | |
| cancelled_at, cancel_at | timestamp | nullable |
| trial_start, trial_end | timestamp | nullable |
| collection_method | varchar(50) | charge_automatically\|send_invoice |
| pause_status | varchar(50) | none\|paused |
| active_pause_id | varchar(50) | nullable |
| commitment_amount | decimal(20,6) | nullable |
| parent_subscription_id | varchar(50) | nullable (hierarchy) |
| subscription_type | varchar(20) | standalone\|parent\|inherited |
| invoicing_customer_id | varchar(50) | nullable |
| proration_behavior | varchar(50) | create_prorations\|none |
| metadata | jsonb | |
| version | int | default 1 |

Indexes: `(tenant_id, env_id, customer_id, status)`, `(tenant_id, env_id, subscription_status, status)`, `(tenant_id, env_id, current_period_end, subscription_status, status)`

### subscription_line_items
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| subscription_id, customer_id | varchar(50) | NOT NULL |
| price_id | varchar(50) | NOT NULL |
| price_type | varchar(50) | FIXED\|USAGE |
| meter_id | varchar(50) | nullable |
| entity_id, entity_type | varchar(50) | PLAN\|ADDON |
| quantity | decimal(20,8) | |
| currency | varchar(10) | |
| billing_period | varchar(50) | |
| start_date, end_date | timestamp | nullable |

### invoices
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | PK |
| customer_id | varchar(50) | NOT NULL, IMMUTABLE |
| subscription_id | varchar(50) | nullable |
| invoice_type | varchar(50) | SUBSCRIPTION\|ONE_OFF\|CREDIT |
| invoice_status | varchar(50) | DRAFT\|FINALIZED\|VOIDED\|SKIPPED |
| payment_status | varchar(50) | INITIATED\|PENDING\|PROCESSING\|SUCCEEDED\|OVERPAID\|FAILED\|REFUNDED\|PARTIALLY_REFUNDED |
| currency | varchar(10) | |
| amount_due, amount_paid, amount_remaining | decimal(20,8) | |
| subtotal, total | decimal(20,8) | |
| total_tax, total_discount | decimal(20,8) | nullable |
| adjustment_amount, refunded_amount | decimal(20,8) | |
| total_prepaid_credits_applied | decimal(20,8) | |
| period_start, period_end | timestamp | nullable, IMMUTABLE |
| due_date, paid_at, voided_at, finalized_at, last_computed_at | timestamp | nullable |
| invoice_number | varchar(50) | UNIQUE per tenant/env |
| idempotency_key | varchar(100) | UNIQUE per tenant/env |
| billing_sequence | int | nullable |
| billing_reason | varchar | |
| version | int | default 1 |
| recalculated_invoice_id | varchar(50) | nullable (post-void replacement) |

Indexes: `(tenant_id, env_id, customer_id, invoice_status, payment_status, status)`, `(tenant_id, env_id, subscription_id, invoice_status, payment_status)`, `UNIQUE(subscription_id, period_start, period_end)` WHERE not VOIDED

### invoice_line_items
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| invoice_id, customer_id | varchar(50) | NOT NULL, IMMUTABLE |
| subscription_id | varchar(50) | nullable |
| price_id, price_type | varchar | nullable, IMMUTABLE |
| meter_id | varchar(50) | nullable |
| amount, quantity | decimal(20,8) | IMMUTABLE |
| currency | varchar(10) | |
| period_start, period_end | timestamp | |
| prepaid_credits_applied | decimal(20,8) | |
| line_item_discount, invoice_level_discount | decimal(20,8) | |
| commitment_info | jsonb | |

### plans
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| lookup_key | varchar(255) | nullable, UNIQUE per tenant/env |
| name | varchar(255) | NOT NULL |
| description | text | |
| display_order | int | |
| metadata | jsonb | |

### prices
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| amount | decimal(25,15) | IMMUTABLE |
| currency | varchar(3) | |
| type | varchar(20) | FIXED\|USAGE |
| billing_model | varchar(20) | FLAT_FEE\|PACKAGE\|TIERED |
| billing_period | varchar(20) | |
| billing_cadence | varchar(20) | RECURRING\|ONETIME |
| invoice_cadence | varchar(20) | ARREAR\|ADVANCE |
| tier_mode | varchar(20) | VOLUME\|SLAB (nullable) |
| tiers | JSON | [{unit_amount, flat_amount, up_to}] |
| meter_id | varchar(50) | nullable (USAGE prices) |
| entity_type | varchar(20) | PLAN\|SUBSCRIPTION\|ADDON\|COSTSHEET |
| entity_id | varchar(50) | |
| parent_price_id | varchar(50) | nullable |
| lookup_key | varchar(255) | |
| start_date, end_date | timestamp | |
| filter_values | json | |

### meters
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| event_name | varchar(50) | NOT NULL |
| name | varchar(255) | |
| aggregation | JSON | {type, field, expression, multiplier, bucket_size, group_by} |
| filters | JSON | [{key, values[]}] |
| reset_usage | varchar(20) | billing_period (default) |

Aggregation types: `COUNT`, `SUM`, `AVG`, `COUNT_UNIQUE`, `LATEST`, `SUM_WITH_MULTIPLIER`, `MAX`, `WEIGHTED_SUM`

### features
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| lookup_key | varchar(255) | UNIQUE per tenant/env |
| name | varchar(255) | NOT NULL |
| type | varchar(50) | metered\|entitlement\|static |
| meter_id | varchar(50) | nullable |
| unit_singular, unit_plural | varchar(50) | |

### entitlements
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| entity_type | varchar(50) | PLAN\|ADDON\|SUBSCRIPTION |
| entity_id | varchar(50) | |
| feature_id | varchar(50) | |
| feature_type | varchar(50) | metered\|entitlement\|static |
| is_enabled | bool | |
| is_soft_limit | bool | |
| usage_limit | int64 | nullable |
| usage_reset_period | varchar(20) | daily\|monthly\|yearly\|billing_period |
| static_value | varchar | nullable |
| parent_entitlement_id | varchar(50) | nullable |

### wallets
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| customer_id | varchar(50) | |
| currency | varchar(10) | |
| wallet_status | varchar(50) | active\|inactive |
| wallet_type | varchar(50) | prepaid\|postpaid |
| balance, credit_balance | decimal(20,9) | |
| conversion_rate | decimal(10,5) | |

### wallet_transactions
Balance change records for wallets (credits, debits, topups).

### credit_grants
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| scope | varchar(50) | PLAN\|SUBSCRIPTION |
| plan_id, subscription_id | varchar(50) | nullable |
| credits | decimal(20,8) | IMMUTABLE |
| cadence | varchar(50) | ONETIME\|RECURRING |
| period | varchar(50) | billing period enum |
| expiration_type | varchar(50) | NEVER\|DURATION\|BILLING_CYCLE |
| start_date, end_date | timestamp | |

### coupons
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| name | varchar(255) | |
| type | varchar(20) | fixed\|percentage |
| amount_off | decimal(20,8) | |
| percentage_off | decimal(7,4) | |
| cadence | varchar(20) | once\|repeated\|forever |
| duration_in_periods | int | nullable |
| max_redemptions, total_redemptions | int | |

### coupon_applications
Audit trail: which coupon applied to which invoice/line_item, amounts discounted.

### credit_notes
| Column | Type | Notes |
|--------|------|-------|
| id | varchar(50) | |
| invoice_id, customer_id | varchar(50) | |
| credit_note_status | varchar(50) | DRAFT\|FINALIZED\|VOIDED |
| credit_note_type | varchar(50) | REFUND\|ADJUSTMENT\|CREDIT_MEMO |
| reason | varchar(50) | DUPLICATE\|FRAUD\|CUSTOMER_REQUEST\|etc |
| total_amount | decimal(20,8) | |

### addons + addon_associations
Optional plan add-ons linked to subscriptions.

### tax_rates / tax_associations / tax_applied
Tax configuration, linkage, and application audit.

### subscription_pauses / subscription_phases / subscription_schedules
Subscription lifecycle management entities.

### payments / payment_attempts
Payment transaction records.

---

## ClickHouse Tables

### events  *(primary events table)*
Engine: `ReplacingMergeTree(ingested_at)`, partition by `toYYYYMMDD(timestamp)`

| Column | Type | Notes |
|--------|------|-------|
| id | String | UUID |
| tenant_id, environment_id | String | |
| external_customer_id | String | Bloom filter index |
| event_name | String NOT NULL | Set index |
| customer_id | Nullable(String) | resolved from external_id |
| source | Nullable(String) | |
| timestamp | DateTime64(3) | event time |
| ingested_at | DateTime64(3) | arrival time |
| properties | String | JSON payload |

ORDER BY: `(tenant_id, environment_id, timestamp, id)`

**Query tip**: Do NOT use `FINAL` — it triggers a full dedup merge and is very expensive on large tables. Filter on `timestamp` for time-range queries. Accept some duplicate tolerance from ReplacingMergeTree background merges.

### raw_events
Engine: `ReplacingMergeTree(version)`, partition by `toYYYYMMDD(timestamp)`

| Column | Type | Notes |
|--------|------|-------|
| id, tenant_id, environment_id, external_customer_id, event_name | String | |
| source | Nullable(String) | |
| payload | String CODEC(ZSTD(3)) | full compressed payload |
| field1..field10 | Nullable(String) | flexible schema fields |
| timestamp, ingested_at | DateTime64(3) | |
| version | UInt64 | for dedup |
| sign | Int8 | default 1 |

ORDER BY: `(tenant_id, environment_id, external_customer_id, timestamp, id)`

### meter_usage  *(lightweight meter-level events)*
Engine: `ReplacingMergeTree(ingested_at)`, partition by `toYYYYMMDD(timestamp)`

| Column | Type | Notes |
|--------|------|-------|
| id | String CODEC(ZSTD(1)) | |
| tenant_id, environment_id, external_customer_id, meter_id, event_name | LowCardinality(String) | |
| timestamp | DateTime (no millis) CODEC(DoubleDelta, ZSTD(1)) | |
| ingested_at | DateTime64(3) | |
| qty_total | Decimal(18,8) | |
| unique_hash | String | dedup key (default '') |
| source | LowCardinality(String) | |
| properties | String CODEC(ZSTD(3)) | |

ORDER BY: `(tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)`

### feature_usage
Engine: `ReplacingMergeTree(version)`, partition by `toYYYYMMDD(timestamp)`

| Column | Type | Notes |
|--------|------|-------|
| id, tenant_id, environment_id, external_customer_id, event_name | String | |
| customer_id, subscription_id, sub_line_item_id, price_id, feature_id | String | |
| meter_id | Nullable(String) | |
| period_id | UInt64 | billing period start (epoch-ms) |
| timestamp, ingested_at, processed_at | DateTime64(3) | |
| qty_total | Decimal(25,15) | |
| unique_hash | Nullable(String) | |
| version | UInt64 | |
| sign | Int8 | |

### events_processed
Engine: `ReplacingMergeTree(version)`, partition by `toYYYYMM(timestamp)`

Extended events with billing resolution: `subscription_id`, `sub_line_item_id`, `price_id`, `meter_id`, `feature_id`, `period_id`, `qty_total`, `qty_billable`, `qty_free_applied`, `unit_cost`, `cost`, `currency`, `tier_snapshot`.

Materialized view `agg_usage_period_totals` pre-aggregates by `(tenant_id, env_id, customer_id, period_id, feature_id, sub_line_item_id)`.

### costsheet_usage
Similar to feature_usage but for non-subscription costsheet billing. Has `costsheet_id` instead of `subscription_id`.

---

## Common Query Patterns

### PostgreSQL

```sql
-- Active subscriptions for a tenant
SELECT s.id, c.external_id, s.subscription_status, s.current_period_end
FROM subscriptions s
JOIN customers c ON c.id = s.customer_id
WHERE s.tenant_id = '<tid>' AND s.environment_id = '<eid>'
  AND s.subscription_status = 'active' AND s.status = 'published'
ORDER BY s.created_at DESC;

-- Invoice summary for a period
SELECT invoice_status, payment_status, count(*), sum(total)
FROM invoices
WHERE tenant_id = '<tid>' AND environment_id = '<eid>'
  AND finalized_at BETWEEN '<start>' AND '<end>'
  AND status = 'published'
GROUP BY invoice_status, payment_status;

-- Customer wallet balances
SELECT c.external_id, w.currency, w.balance, w.credit_balance
FROM wallets w JOIN customers c ON c.id = w.customer_id
WHERE w.tenant_id = '<tid>' AND w.status = 'published'
  AND w.wallet_status = 'active';
```

### ClickHouse

```sql
-- Event count by date (deduplicated)
SELECT toDate(timestamp) as day, count() as events
FROM eventsWHERE tenant_id = '<tid>' AND environment_id = '<eid>'
  AND timestamp BETWEEN toDateTime64('2026-01-01 00:00:00', 3)
                    AND toDateTime64('2026-01-31 23:59:59', 3)
GROUP BY day ORDER BY day;

-- Top customers by event volume
SELECT external_customer_id, count() as cnt
FROM eventsWHERE tenant_id = '<tid>' AND timestamp >= now() - INTERVAL 7 DAY
GROUP BY external_customer_id ORDER BY cnt DESC LIMIT 20;

-- Meter usage for a customer
SELECT meter_id, sum(qty_total) as total_qty,
       min(timestamp) as first_event, max(timestamp) as last_event
FROM meter_usageWHERE tenant_id = '<tid>' AND external_customer_id = '<ext_cid>'
  AND timestamp >= toDateTime('2026-04-01 00:00:00')
GROUP BY meter_id;

-- Hourly ingest rate
SELECT toStartOfHour(ingested_at) as hour, count() as events
FROM eventsWHERE ingested_at >= now() - INTERVAL 24 HOUR
GROUP BY hour ORDER BY hour;
```

---

## Environment Variables (`.env`)

```
FLEXPRICE_POSTGRES_READER_HOST   # Read replica host
FLEXPRICE_POSTGRES_DBNAME        # Database name
FLEXPRICE_POSTGRES_USER          # Username
FLEXPRICE_POSTGRES_PASSWORD      # Password
FLEXPRICE_POSTGRES_PORT          # 5432
FLEXPRICE_POSTGRES_SSLMODE       # require

FLEXPRICE_CLICKHOUSE_ADDRESS     # host:port (e.g. internal-lb.aws.com:9000)
FLEXPRICE_CLICKHOUSE_DATABASE    # flexprice
FLEXPRICE_CLICKHOUSE_USERNAME    # readonly
FLEXPRICE_CLICKHOUSE_PASSWORD    # ...
```

Scripts auto-load `.env` from their own directory. All scripts use read-only credentials.
