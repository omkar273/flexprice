# Meter Usage Pipeline Benchmarking Design

**Date:** 2026-04-11  
**Status:** Approved

---

## Goal

Validate that the new `meter_usage` pipeline produces identical results to the existing `feature_usage` pipeline (`GetFeatureUsageBySubscription`) for the same subscription and time window.

---

## Approach

**Option A (selected):** Thin Kafka event published by wallet service; consumer performs both pipeline calls independently and writes comparison results to ClickHouse.

---

## Data Flow

```
Wallet.GetWalletBalance()
  └─ GetFeatureUsageBySubscription(sub_id, start, end)  [succeeds]
       └─ [fire-and-forget] publish to Kafka: staging_benchmarking
            payload: { subscription_id, start_time, end_time, tenant_id, environment_id }

BenchmarkConsumer (consumer deployment mode)
  └─ consume from staging_benchmarking
       ├─ call GetFeatureUsageBySubscription() → feature_amt   [source of truth]
       ├─ call GetFeatureUsageBySubscription() → meter_amt     [placeholder for GetMeterUsageBySubscription]
       ├─ diff = feature_amt - meter_amt
       └─ INSERT row into ClickHouse usage_benchmark
```

**Swap point:** When `GetMeterUsageBySubscription` is built, replace the second consumer call with it — no other changes needed.

---

## Components

### New Files

| File | Purpose |
|------|---------|
| `internal/domain/events/usage_benchmark.go` | Kafka payload struct + ClickHouse record struct |
| `migrations/clickhouse/000008_create_usage_benchmark_table.up.sql` | Table DDL |
| `internal/repository/clickhouse/usage_benchmark.go` | `Insert(ctx, *UsageBenchmarkRecord) error` |
| `internal/service/benchmark_consumer.go` | Subscribe → compare → insert |

### Modified Files

| File | Change |
|------|--------|
| `internal/service/wallet.go` | Publish benchmark Kafka event at both `GetFeatureUsageBySubscription` call sites (~lines 2413, 2581) |
| `internal/config/config.go` | Add `BenchmarkConfig { Topic string }` |
| `cmd/server/main.go` | Wire new repo + consumer via FX |

---

## Kafka Event Payload

```go
type UsageBenchmarkEvent struct {
    SubscriptionID string    `json:"subscription_id"`
    StartTime      time.Time `json:"start_time"`
    EndTime        time.Time `json:"end_time"`
    TenantID       string    `json:"tenant_id"`
    EnvironmentID  string    `json:"environment_id"`
}
```

Topic: `staging_benchmarking` (configurable via `BenchmarkConfig.Topic`)

---

## ClickHouse Table

```sql
CREATE TABLE IF NOT EXISTS usage_benchmark (
    tenant_id             String,
    environment_id        String,
    subscription_id       String,
    start_time            DateTime64(3),
    end_time              DateTime64(3),
    feature_usage_amount  Float64,
    meter_usage_amount    Float64,
    diff                  Float64,
    currency              String,
    created_at            DateTime64(3) DEFAULT now()
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (tenant_id, environment_id, subscription_id, start_time);
```

---

## ClickHouse Record Struct

```go
type UsageBenchmarkRecord struct {
    TenantID            string    `ch:"tenant_id"`
    EnvironmentID       string    `ch:"environment_id"`
    SubscriptionID      string    `ch:"subscription_id"`
    StartTime           time.Time `ch:"start_time"`
    EndTime             time.Time `ch:"end_time"`
    FeatureUsageAmount  float64   `ch:"feature_usage_amount"`
    MeterUsageAmount    float64   `ch:"meter_usage_amount"`
    Diff                float64   `ch:"diff"`
    Currency            string    `ch:"currency"`
    CreatedAt           time.Time `ch:"created_at"`
}
```

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Kafka publish fails in wallet | Log warning, skip — never propagate to wallet caller |
| `GetFeatureUsageBySubscription` fails in wallet | Skip publish (only publish on success) |
| Either pipeline call fails in consumer | Log error, ack message, skip ClickHouse insert |
| ClickHouse insert fails | Log error, ack message (benchmark data is non-critical; avoid retry loop) |

---

## Edge Cases

- **Duplicate events:** Same `(subscription_id, start_time, end_time)` can produce multiple rows if wallet is called repeatedly for the same period. Acceptable — validation queries use `maxIf` aggregation.
- **Zero usage:** Subscriptions with no usage-type line items return `amount=0` from both pipelines. Still published and recorded — confirms both pipelines agree on zero.
- **Two wallet call sites:** Both sites in `wallet.go` (~2413 and ~2581) publish independently, giving broader coverage.
- **Source flag:** Both consumer calls use `UsageSourceAnalytics` (no `FINAL` clause) for consistent conditions.

---

## Validation Query

```sql
SELECT
    subscription_id,
    start_time,
    end_time,
    maxIf(feature_usage_amount, flow_type = 'feature_usage') AS feature_amt,
    maxIf(meter_usage_amount,   flow_type = 'meter_usage')   AS meter_amt,
    feature_amt - meter_amt AS diff
FROM usage_benchmark
GROUP BY subscription_id, start_time, end_time
HAVING diff != 0;
```

---

## Out of Scope

- DLQ / retry for failed benchmark events
- Per-meter quantity breakdown (total amount comparison only)
- Production environment (staging only for now)
- Alerting on diffs
