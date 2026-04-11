# Meter Usage Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a benchmarking pipeline that publishes a thin Kafka event on every wallet `GetFeatureUsageBySubscription` call, then a consumer compares both pipeline results and writes diffs to a `usage_benchmark` ClickHouse table.

**Architecture:** Wallet service publishes `{subscription_id, start_time, end_time, tenant_id, environment_id}` to the `staging_benchmarking` Kafka topic (fire-and-forget, on success only). A new `UsageBenchmarkService` consumes that topic, calls `GetFeatureUsageBySubscription` twice (once for each "pipeline" — same call for now, swap one when `GetMeterUsageBySubscription` is ready), computes `diff = feature_amt - meter_amt`, and inserts one row into `usage_benchmark` in ClickHouse.

**Tech Stack:** Go 1.23, Watermill (Kafka), ClickHouse driver (`clickhouse-go`), Uber FX (DI), existing `ServiceParams` pattern.

---

## File Map

| Action | Path | Responsibility |
|--------|------|---------------|
| Create | `migrations/clickhouse/000008_create_usage_benchmark_table.up.sql` | DDL for `usage_benchmark` table |
| Create | `internal/domain/events/usage_benchmark.go` | Kafka payload struct, ClickHouse record struct, repository interface |
| Create | `internal/repository/clickhouse/usage_benchmark.go` | `Insert` implementation using ClickHouse batch API |
| Create | `internal/service/usage_benchmark.go` | Publisher + consumer service (publish event, register handler, process message) |
| Modify | `internal/types/pubsub.go` | Add `UsageBenchmarkPubSub` wrapper type |
| Modify | `internal/config/config.go` | Add `UsageBenchmarkConfig` struct + field on `Configuration` |
| Modify | `internal/config/config.yaml` | Add default config values for benchmark consumer |
| Modify | `internal/service/factory.go` | Add `UsageBenchmarkPubSub` to `ServiceParams` + `NewServiceParams` |
| Modify | `cmd/server/main.go` | Add FX provider for pubsub, wire service into `registerRouterHandlers` |
| Modify | `internal/service/wallet.go` | Publish benchmark event after each `GetFeatureUsageBySubscription` call |

---

## Task 1: ClickHouse Migration

**Files:**
- Create: `migrations/clickhouse/000008_create_usage_benchmark_table.up.sql`

- [ ] **Step 1: Write the migration file**

```sql
CREATE TABLE IF NOT EXISTS flexprice.usage_benchmark
(
    tenant_id             LowCardinality(String)  NOT NULL,
    environment_id        LowCardinality(String)  NOT NULL,
    subscription_id       String                  NOT NULL CODEC(ZSTD(1)),
    start_time            DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),
    end_time              DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),
    feature_usage_amount  Float64                 NOT NULL,
    meter_usage_amount    Float64                 NOT NULL,
    diff                  Float64                 NOT NULL,
    currency              LowCardinality(String)  NOT NULL DEFAULT '',
    created_at            DateTime64(3)           NOT NULL DEFAULT now64(3)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (tenant_id, environment_id, subscription_id, start_time)
SETTINGS
    index_granularity = 8192;
```

- [ ] **Step 2: Apply migration**

```bash
make migrate-clickhouse
```

Expected: `migration applied` log line; no errors.

- [ ] **Step 3: Commit**

```bash
git add migrations/clickhouse/000008_create_usage_benchmark_table.up.sql
git commit -m "feat(benchmark): add usage_benchmark ClickHouse table migration"
```

---

## Task 2: Domain Types

**Files:**
- Create: `internal/domain/events/usage_benchmark.go`

- [ ] **Step 1: Write the domain file**

```go
package events

import "time"

// UsageBenchmarkEvent is the thin Kafka payload published by the wallet service.
type UsageBenchmarkEvent struct {
	SubscriptionID string    `json:"subscription_id"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
}

// UsageBenchmarkRecord is one row in the usage_benchmark ClickHouse table.
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

// UsageBenchmarkRepository persists benchmark comparison rows.
type UsageBenchmarkRepository interface {
	Insert(ctx context.Context, record *UsageBenchmarkRecord) error
}
```

> Note: add `"context"` to the import block.

- [ ] **Step 2: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/events/usage_benchmark.go
git commit -m "feat(benchmark): add UsageBenchmarkEvent, UsageBenchmarkRecord domain types"
```

---

## Task 3: ClickHouse Repository

**Files:**
- Create: `internal/repository/clickhouse/usage_benchmark.go`

- [ ] **Step 1: Write the repository**

```go
package clickhouse

import (
	"context"
	"time"

	ch "github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

type UsageBenchmarkRepository struct {
	store  *ch.ClickHouseStore
	logger *logger.Logger
}

func NewUsageBenchmarkRepository(store *ch.ClickHouseStore, logger *logger.Logger) events.UsageBenchmarkRepository {
	return &UsageBenchmarkRepository{store: store, logger: logger}
}

func (r *UsageBenchmarkRepository) Insert(ctx context.Context, record *events.UsageBenchmarkRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	stmt, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO usage_benchmark (
			tenant_id, environment_id, subscription_id,
			start_time, end_time,
			feature_usage_amount, meter_usage_amount, diff,
			currency, created_at
		)
	`)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to prepare usage_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	if err := stmt.Append(
		record.TenantID,
		record.EnvironmentID,
		record.SubscriptionID,
		record.StartTime,
		record.EndTime,
		record.FeatureUsageAmount,
		record.MeterUsageAmount,
		record.Diff,
		record.Currency,
		record.CreatedAt,
	); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to append usage_benchmark row").
			Mark(ierr.ErrDatabase)
	}

	return stmt.Send()
}
```

- [ ] **Step 2: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/repository/clickhouse/usage_benchmark.go
git commit -m "feat(benchmark): add ClickHouse UsageBenchmarkRepository"
```

---

## Task 4: Config + PubSub Type

**Files:**
- Modify: `internal/types/pubsub.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config.yaml`

- [ ] **Step 1: Add `UsageBenchmarkPubSub` wrapper to `internal/types/pubsub.go`**

Append to the existing file (after the `IntegrationEventsPubSub` struct):

```go
// UsageBenchmarkPubSub is a named wrapper so FX can inject it independently.
type UsageBenchmarkPubSub struct {
	pubsub.PubSub
}
```

- [ ] **Step 2: Add `UsageBenchmarkConfig` struct to `internal/config/config.go`**

After the `MeterUsageTrackingConfig` block (around line 317), add:

```go
// UsageBenchmarkConfig configures the usage benchmarking consumer
type UsageBenchmarkConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"false"`
	Topic         string `mapstructure:"topic" default:"staging_benchmarking"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"10"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_usage_benchmark_service"`
}
```

- [ ] **Step 3: Add field to `Configuration` struct in `internal/config/config.go`**

In the `Configuration` struct (around line 47 where `MeterUsageTracking` is), add:

```go
UsageBenchmark UsageBenchmarkConfig `mapstructure:"usage_benchmark" validate:"required"`
```

- [ ] **Step 4: Add defaults to `internal/config/config.yaml`**

Find the `meter_usage_tracking:` block and add after it:

```yaml
usage_benchmark:
  enabled: false
  topic: staging_benchmarking
  rate_limit: 10
  consumer_group: v1_usage_benchmark_service
```

- [ ] **Step 5: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/types/pubsub.go internal/config/config.go internal/config/config.yaml
git commit -m "feat(benchmark): add UsageBenchmarkConfig and UsageBenchmarkPubSub type"
```

---

## Task 5: UsageBenchmarkService

**Files:**
- Create: `internal/service/usage_benchmark.go`

- [ ] **Step 1: Write the failing test first**

Create `internal/service/usage_benchmark_test.go`:

```go
package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryBenchmarkRepo captures inserted records for assertion.
type inMemoryBenchmarkRepo struct {
	records []*events.UsageBenchmarkRecord
}

func (r *inMemoryBenchmarkRepo) Insert(_ context.Context, rec *events.UsageBenchmarkRecord) error {
	r.records = append(r.records, rec)
	return nil
}

func TestUsageBenchmarkService_ProcessMessage_InsertsRow(t *testing.T) {
	repo := &inMemoryBenchmarkRepo{}
	pubSub := testutil.NewInMemoryPubSub()

	svc := service.NewUsageBenchmarkServiceForTest(repo, pubSub)

	evt := &events.UsageBenchmarkEvent{
		SubscriptionID: "sub_test",
		StartTime:      time.Now().Add(-24 * time.Hour).UTC(),
		EndTime:        time.Now().UTC(),
		TenantID:       "ten_test",
		EnvironmentID:  "env_test",
	}
	payload, err := json.Marshal(evt)
	require.NoError(t, err)

	msg := message.NewMessage("msg-1", payload)
	msg.Metadata.Set("tenant_id", evt.TenantID)
	msg.Metadata.Set("environment_id", evt.EnvironmentID)

	err = svc.ProcessMessageForTest(msg)
	require.NoError(t, err)
	require.Len(t, repo.records, 1)

	rec := repo.records[0]
	assert.Equal(t, "sub_test", rec.SubscriptionID)
	assert.Equal(t, "ten_test", rec.TenantID)
	assert.Equal(t, "env_test", rec.EnvironmentID)
	// Both pipelines call the same stub; diff must be 0.
	assert.InDelta(t, 0.0, rec.Diff, 0.0001)
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/service/... -run TestUsageBenchmarkService_ProcessMessage_InsertsRow -v
```

Expected: `FAIL` with "undefined: service.NewUsageBenchmarkServiceForTest".

- [ ] **Step 3: Implement `internal/service/usage_benchmark.go`**

```go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
)

// UsageBenchmarkService publishes benchmark trigger events and consumes them
// to compare GetFeatureUsageBySubscription against the meter_usage pipeline.
type UsageBenchmarkService interface {
	// PublishEvent sends a thin benchmark trigger to Kafka. Non-blocking best-effort.
	PublishEvent(ctx context.Context, event *events.UsageBenchmarkEvent) error

	// RegisterHandler wires the consumer into the router.
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type usageBenchmarkService struct {
	ServiceParams
	pubSub     pubsub.PubSub
	benchRepo  events.UsageBenchmarkRepository
}

// NewUsageBenchmarkService is the production constructor wired by FX.
func NewUsageBenchmarkService(
	params ServiceParams,
	benchRepo events.UsageBenchmarkRepository,
) UsageBenchmarkService {
	svc := &usageBenchmarkService{
		ServiceParams: params,
		benchRepo:     benchRepo,
	}

	ps, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.UsageBenchmark.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub for usage benchmark", "error", err)
		return svc
	}
	svc.pubSub = ps
	return svc
}

// NewUsageBenchmarkServiceForTest builds a minimal service using injected deps (test only).
func NewUsageBenchmarkServiceForTest(
	benchRepo events.UsageBenchmarkRepository,
	ps pubsub.PubSub,
) *usageBenchmarkService {
	return &usageBenchmarkService{
		pubSub:    ps,
		benchRepo: benchRepo,
	}
}

// PublishEvent marshals and publishes a UsageBenchmarkEvent.
func (s *usageBenchmarkService) PublishEvent(ctx context.Context, event *events.UsageBenchmarkEvent) error {
	if s.pubSub == nil {
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("usage benchmark: failed to marshal event: %w", err)
	}

	msg := message.NewMessage(fmt.Sprintf("bench-%s-%d", event.SubscriptionID, time.Now().UnixNano()), payload)
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)

	topic := s.Config.UsageBenchmark.Topic
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return fmt.Errorf("usage benchmark: failed to publish to %s: %w", topic, err)
	}
	return nil
}

// RegisterHandler wires the benchmark consumer into the watermill router.
func (s *usageBenchmarkService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.UsageBenchmark.Enabled {
		s.Logger.Infow("usage benchmark consumer disabled by configuration")
		return
	}

	router.AddNoPublishHandler(
		"usage_benchmark_handler",
		cfg.UsageBenchmark.Topic,
		s.pubSub,
		s.processMessage,
	)

	s.Logger.Infow("registered usage benchmark handler",
		"topic", cfg.UsageBenchmark.Topic,
	)
}

// processMessage is the exported-for-test version's delegate.
func (s *usageBenchmarkService) processMessage(msg *message.Message) error {
	return s.ProcessMessageForTest(msg)
}

// ProcessMessageForTest is exported so unit tests can call it directly.
func (s *usageBenchmarkService) ProcessMessageForTest(msg *message.Message) error {
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	var evt events.UsageBenchmarkEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		// Log and ack — malformed message should not block the queue.
		if s.Logger != nil {
			s.Logger.Errorw("usage benchmark: failed to unmarshal event", "error", err)
		}
		return nil
	}

	ctx := types.NewBackgroundContext()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)

	featureAmt, currency := s.callFeatureUsagePipeline(ctx, &evt)
	meterAmt, _ := s.callMeterUsagePipeline(ctx, &evt)

	record := &events.UsageBenchmarkRecord{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		SubscriptionID:     evt.SubscriptionID,
		StartTime:          evt.StartTime,
		EndTime:            evt.EndTime,
		FeatureUsageAmount: featureAmt,
		MeterUsageAmount:   meterAmt,
		Diff:               featureAmt - meterAmt,
		Currency:           currency,
		CreatedAt:          time.Now().UTC(),
	}

	if err := s.benchRepo.Insert(ctx, record); err != nil {
		if s.Logger != nil {
			s.Logger.Errorw("usage benchmark: failed to insert record",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		// Ack anyway — benchmark data is non-critical.
	}
	return nil
}

// callFeatureUsagePipeline calls GetFeatureUsageBySubscription (source of truth).
func (s *usageBenchmarkService) callFeatureUsagePipeline(ctx context.Context, evt *events.UsageBenchmarkEvent) (float64, string) {
	if s.ServiceParams.FeatureUsageRepo == nil {
		return 0, ""
	}
	subSvc := NewSubscriptionService(s.ServiceParams)
	resp, err := subSvc.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: evt.SubscriptionID,
		StartTime:      evt.StartTime,
		EndTime:        evt.EndTime,
		Source:         string(types.UsageSourceAnalytics),
	})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warnw("usage benchmark: feature pipeline call failed",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		return 0, ""
	}
	return resp.Amount, resp.Currency
}

// callMeterUsagePipeline is the placeholder that will become GetMeterUsageBySubscription.
// Replace this call when GetMeterUsageBySubscription is ready.
func (s *usageBenchmarkService) callMeterUsagePipeline(ctx context.Context, evt *events.UsageBenchmarkEvent) (float64, string) {
	// TODO: replace with GetMeterUsageBySubscription(ctx, req) when available.
	return s.callFeatureUsagePipeline(ctx, evt)
}
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
go test ./internal/service/... -run TestUsageBenchmarkService_ProcessMessage_InsertsRow -v
```

Expected: `PASS`.

- [ ] **Step 5: Verify full compilation**

```bash
make build
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/usage_benchmark.go internal/service/usage_benchmark_test.go
git commit -m "feat(benchmark): add UsageBenchmarkService with publisher and consumer"
```

---

## Task 6: Wire Up ServiceParams and FX

**Files:**
- Modify: `internal/service/factory.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add `UsageBenchmarkPubSub` to `ServiceParams` in `factory.go`**

In the `ServiceParams` struct (around line 129 where `WalletBalanceAlertPubSub` is):

```go
UsageBenchmarkPubSub types.UsageBenchmarkPubSub
```

In the `NewServiceParams` function signature (find the `walletBalanceAlertPubSub types.WalletBalanceAlertPubSub` param and add after it):

```go
usageBenchmarkPubSub types.UsageBenchmarkPubSub,
```

In the `ServiceParams` return value construction (find `WalletBalanceAlertPubSub: walletBalanceAlertPubSub`):

```go
UsageBenchmarkPubSub: usageBenchmarkPubSub,
```

- [ ] **Step 2: Add `provideUsageBenchmarkPubSub` to `cmd/server/main.go`**

After the `provideWalletBalanceAlertPubSub` function (around line 664), add:

```go
func provideUsageBenchmarkPubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) types.UsageBenchmarkPubSub {
	pubSub, err := kafkaPubsub.NewPubSubFromConfig(
		cfg,
		logger,
		cfg.UsageBenchmark.ConsumerGroup,
	)
	if err != nil {
		logger.Fatalw("failed to create pubsub for usage benchmark", "error", err)
		return types.UsageBenchmarkPubSub{}
	}
	return types.UsageBenchmarkPubSub{PubSub: pubSub}
}
```

- [ ] **Step 3: Register in FX provide list in `cmd/server/main.go`**

In the `fx.Provide(...)` block (near line 201 where `provideWalletBalanceAlertPubSub` is listed), add:

```go
provideUsageBenchmarkPubSub,
```

Also add `service.NewUsageBenchmarkService` to the provides list (near `service.NewMeterUsageTrackingService`):

```go
service.NewUsageBenchmarkService,
```

- [ ] **Step 4: Add `usageBenchmarkSvc` to `startConsumerMode` and `registerRouterHandlers` in `main.go`**

Find the function that accepts `meterUsageTrackingSvc service.MeterUsageTrackingService` (around line 493 and line 605). Add the new parameter after it in both signatures:

```go
usageBenchmarkSvc service.UsageBenchmarkService,
```

In the `registerRouterHandlers` call sites (lines ~512, 519, 526, 535), add `usageBenchmarkSvc` as the last argument.

In the `registerRouterHandlers` function body (after `meterUsageTrackingSvc.RegisterHandler(router, cfg)`, line ~624):

```go
usageBenchmarkSvc.RegisterHandler(router, cfg)
```

- [ ] **Step 5: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/service/factory.go cmd/server/main.go internal/types/pubsub.go
git commit -m "feat(benchmark): wire UsageBenchmarkService into FX and consumer router"
```

---

## Task 7: Wallet Integration

**Files:**
- Modify: `internal/service/wallet.go`

There are two `GetFeatureUsageBySubscription` call sites in `wallet.go`:
1. Around line **2413** (first usage computation)
2. Around line **2581** (second usage computation)

Add the publish call after each one, immediately after the `if err != nil { return nil, err }` check.

- [ ] **Step 1: Add publish helper at the top of the wallet service (after existing `PublishWalletBalanceAlertEvent` method)**

Find and add to the `walletService` struct or as a standalone method — add this helper function to `wallet.go`:

```go
// publishBenchmarkEvent publishes a usage benchmark event to Kafka.
// Fire-and-forget: errors are logged but never returned to the caller.
func (s *walletService) publishBenchmarkEvent(ctx context.Context, subscriptionID string, startTime, endTime time.Time) {
	benchSvc := NewUsageBenchmarkService(s.ServiceParams, nil)
	evt := &events.UsageBenchmarkEvent{
		SubscriptionID: subscriptionID,
		StartTime:      startTime,
		EndTime:        endTime,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
	}
	if err := benchSvc.PublishEvent(ctx, evt); err != nil {
		s.Logger.WarnwCtx(ctx, "usage benchmark: failed to publish event",
			"subscription_id", subscriptionID,
			"error", err,
		)
	}
}
```

> Note: `NewUsageBenchmarkService` needs to handle `nil` benchRepo gracefully for the publish-only path. Verify the constructor does not panic when `benchRepo` is nil — it should not, since it only uses `benchRepo` in the consumer path.

- [ ] **Step 2: Call `publishBenchmarkEvent` at call site 1 (~line 2413)**

Find this block:
```go
usage, err := subscriptionService.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
    SubscriptionID: sub.ID,
    StartTime:      periodStart,
    EndTime:        periodEnd,
    Source:         string(types.UsageSourceWallet),
})
if err != nil {
    return nil, err
}
```

After the `if err != nil` block, add:
```go
go s.publishBenchmarkEvent(ctx, sub.ID, periodStart, periodEnd)
```

- [ ] **Step 3: Call `publishBenchmarkEvent` at call site 2 (~line 2581)**

Find the second block:
```go
usage, err := subscriptionService.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
    SubscriptionID: sub.ID,
    StartTime:      periodStart,
    EndTime:        periodEnd,
    Source:         string(types.UsageSourceWallet),
})
if err != nil {
    return nil, err
}
```

After the `if err != nil` block, add:
```go
go s.publishBenchmarkEvent(ctx, sub.ID, periodStart, periodEnd)
```

- [ ] **Step 4: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 5: Run existing wallet tests to confirm no regression**

```bash
go test -v -race ./internal/service/... -run TestWallet -count=1
```

Expected: all existing wallet tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/service/wallet.go
git commit -m "feat(benchmark): publish usage benchmark event from wallet GetFeatureUsageBySubscription"
```

---

## Task 8: Also Register UsageBenchmarkRepository in FX

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add `clickhouse.NewUsageBenchmarkRepository` to FX provides**

In `cmd/server/main.go`, find the block where ClickHouse repos are provided (search for `clickhouse.NewMeterUsageRepository`) and add after it:

```go
clickhouse.NewUsageBenchmarkRepository,
```

- [ ] **Step 2: Verify compilation**

```bash
make build
```

Expected: no errors.

- [ ] **Step 3: Run full test suite**

```bash
make test
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(benchmark): register UsageBenchmarkRepository in FX dependency graph"
```

---

## Validation

Once deployed to staging:

1. Enable the consumer in staging config: `usage_benchmark.enabled: true`
2. Create the Kafka topic `staging_benchmarking` in staging
3. Call a wallet balance endpoint for a subscription with usage
4. Query ClickHouse to confirm rows appear:

```sql
SELECT subscription_id, start_time, end_time,
       feature_usage_amount, meter_usage_amount, diff
FROM flexprice.usage_benchmark
ORDER BY created_at DESC
LIMIT 20;
```

5. Run diff check (should all be 0 since both pipelines use the same call for now):

```sql
SELECT subscription_id, start_time, end_time, diff
FROM flexprice.usage_benchmark
WHERE diff != 0
ORDER BY created_at DESC;
```

---

## Future: Swapping the Meter Pipeline

When `GetMeterUsageBySubscription` is implemented, replace this one function in `internal/service/usage_benchmark.go`:

```go
// callMeterUsagePipeline — REPLACE BODY with:
func (s *usageBenchmarkService) callMeterUsagePipeline(ctx context.Context, evt *events.UsageBenchmarkEvent) (float64, string) {
    subSvc := NewSubscriptionService(s.ServiceParams)
    resp, err := subSvc.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
        SubscriptionID: evt.SubscriptionID,
        StartTime:      evt.StartTime,
        EndTime:        evt.EndTime,
        Source:         string(types.UsageSourceAnalytics),
    })
    if err != nil {
        // log and return 0
        return 0, ""
    }
    return resp.Amount, resp.Currency
}
```
