# Local Testing Guide for AI Agents

This guide covers everything needed to run Flexprice locally and do end-to-end validation of changes — particularly for features involving Kafka consumers, event ingestion, and settings.

---

## Prerequisites

- **OrbStack** (or Docker Desktop) installed and running
- **Go 1.23+** installed
- Production `.env` file present at the repo root (contains secrets — never commit it)

---

## Step 1: Start OrbStack / Docker

```bash
open -a OrbStack
# Wait ~5 seconds, then verify:
docker ps
```

---

## Step 2: Copy `.env` to Your Worktree

The production `.env` lives at the repo root. If working in a git worktree, copy it over:

```bash
cp /path/to/flexprice/.env /path/to/flexprice/.claude/worktrees/<your-worktree>/.env
```

> **Important:** The production `.env` points to production Kafka, Postgres, and ClickHouse. You must override these for local testing (see Step 4).

---

## Step 3: Start Local Infrastructure

```bash
docker compose up -d postgres kafka clickhouse
```

Wait ~10 seconds for Kafka to fully start before creating topics.

**Verify Kafka is ready:**
```bash
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --list
# Should return empty output (no error)
```

---

## Step 4: Create Required Kafka Topics

The local Kafka starts with no topics. Create all the ones the app needs:

```bash
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic raw_events --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic events --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic events_lazy --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic events_post_processing --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic events_backfill --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic system_events --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic onboarding_events --partitions 3 --replication-factor 1
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --create --topic balance_alert --partitions 3 --replication-factor 1
```

> If you skip any topic the consumer subscribes to, you'll see non-fatal `kafka server: Request was for a topic or partition that does not exist` errors in the consumer logs. The app keeps retrying and won't crash, but it's noisy.

---

## Step 5: Run Migrations

Run both Postgres and ClickHouse migrations **with local env overrides** (critical — the Makefile commands pick up the production `.env` by default):

**Postgres (SQL migrations):**
```bash
make migrate-postgres
# This one reads from the .env file correctly for local because it uses
# the docker-compose postgres service exposed on localhost:5432
```

**Ent schema migrations (adds new tables):**
```bash
FLEXPRICE_POSTGRES_HOST=localhost \
FLEXPRICE_POSTGRES_PORT=5432 \
FLEXPRICE_POSTGRES_USER=flexprice \
FLEXPRICE_POSTGRES_PASSWORD=flexprice123 \
FLEXPRICE_POSTGRES_DBNAME=flexprice \
FLEXPRICE_POSTGRES_SSLMODE=disable \
go run ./cmd/migrate/main.go
```

> **Warning:** `make migrate-ent` uses the `.env` file and will run against production if you don't override env vars explicitly. Always use the `go run` form above with explicit local overrides.

**ClickHouse migrations:**
```bash
make migrate-clickhouse
```

---

## Step 6: Build the Binary

```bash
go build -o /tmp/flexprice-local ./cmd/server/main.go
```

---

## Step 7: Define Local Environment Variables

The production `.env` must be overridden for all infrastructure endpoints. Use this block in every terminal that runs the binary:

```bash
# Local infrastructure overrides
export FLEXPRICE_KAFKA_BROKERS="localhost:29092"
export FLEXPRICE_KAFKA_USE_SASL="false"
export FLEXPRICE_KAFKA_SASL_MECHANISM=""
export FLEXPRICE_KAFKA_SASL_USER=""
export FLEXPRICE_KAFKA_SASL_PASSWORD=""
export FLEXPRICE_KAFKA_TOPIC="events"
export FLEXPRICE_KAFKA_TOPIC_LAZY="events_lazy"
export FLEXPRICE_KAFKA_CONSUMER_GROUP="local_events_consumer"

export FLEXPRICE_CLICKHOUSE_ADDRESS="localhost:9000"
export FLEXPRICE_CLICKHOUSE_USERNAME="flexprice"
export FLEXPRICE_CLICKHOUSE_PASSWORD="flexprice123"
export FLEXPRICE_CLICKHOUSE_DATABASE="flexprice"

export FLEXPRICE_POSTGRES_HOST="localhost"
export FLEXPRICE_POSTGRES_PORT="5432"
export FLEXPRICE_POSTGRES_USER="flexprice"
export FLEXPRICE_POSTGRES_PASSWORD="flexprice123"
export FLEXPRICE_POSTGRES_DBNAME="flexprice"
export FLEXPRICE_POSTGRES_SSLMODE="disable"
export FLEXPRICE_POSTGRES_READER_HOST="localhost"

export FLEXPRICE_RAW_EVENT_CONSUMPTION_ENABLED="true"
export FLEXPRICE_RAW_EVENT_CONSUMPTION_TOPIC="raw_events"
export FLEXPRICE_RAW_EVENT_CONSUMPTION_OUTPUT_TOPIC="events"
export FLEXPRICE_RAW_EVENT_CONSUMPTION_CONSUMER_GROUP="local_raw_events_consumer"
export FLEXPRICE_RAW_EVENT_CONSUMPTION_RATE_LIMIT="100"

export FLEXPRICE_EVENT_PROCESSING_CONSUMER_GROUP="local_events_consumer"
export FLEXPRICE_EVENT_PROCESSING_LAZY_CONSUMER_GROUP="local_events_lazy_consumer"
```

> **Local Kafka broker port:** The docker-compose Kafka advertises on `kafka:9092` internally and `localhost:29092` externally. Always use `localhost:29092` from the host machine.

---

## Step 8: Set Up a Test API Key

API key validation works in two modes: config-based (fast, no DB) or DB-based (via secrets table). For local testing, use **config-based** by injecting the SHA-256 hash of your test key.

**Compute the hash of any key you want to use:**
```bash
echo -n "your-test-api-key" | sha256sum
# e.g. sk_01KM416QMD5N07GPKRR3QXQYFB → 155f941ce338505f9e9fa1ed85776da0433b2e16f47ae26a016049984437c632
```

**Build the JSON config and export it:**
```bash
API_KEY_HASH="155f941ce338505f9e9fa1ed85776da0433b2e16f47ae26a016049984437c632"
export FLEXPRICE_AUTH_API_KEY_KEYS="{\"${API_KEY_HASH}\":{\"tenant_id\":\"00000000-0000-0000-0000-000000000000\",\"user_id\":\"test-user\",\"name\":\"local-test-key\",\"is_active\":true}}"
export FLEXPRICE_AUTH_API_KEY_HEADER="x-api-key"
```

> The `tenant_id` `00000000-0000-0000-0000-000000000000` and environment `00000000-0000-0000-0000-000000000000` are the default seed records that migrations create. Use these for local testing.

---

## Step 9: Run API Server (Terminal 1)

```bash
# (with all the env vars above already exported)
FLEXPRICE_DEPLOYMENT_MODE=api \
FLEXPRICE_SERVER_ADDRESS=":8081" \
/tmp/flexprice-local
```

> **Port conflict:** Port `8080` may already be in use (another local server). Use `:8081` or any free port.

**Verify:**
```bash
curl -s http://localhost:8081/health
# → {"status":"ok"}
```

---

## Step 10: Run Consumer (Terminal 2)

```bash
# (with all the env vars above already exported)
FLEXPRICE_DEPLOYMENT_MODE=consumer \
/tmp/flexprice-local
```

**Verify raw event consumer is active:**
```bash
grep "raw_event_consumption_handler\|Subscribing to Kafka topic.*raw" /tmp/consumer.log
# Should show:
# Adding handler  handler_name=raw_event_consumption_handler topic=raw_events
# Subscribing to Kafka topic ... topic=raw_events
```

---

## Step 11: Authenticate API Calls

All private API calls need:
- `x-api-key: <your-test-key>` header
- `x-environment-id: 00000000-0000-0000-0000-000000000000` header

```bash
curl -s http://localhost:8081/v1/meters \
  -H "x-api-key: sk_01KM416QMD5N07GPKRR3QXQYFB" \
  -H "x-environment-id: 00000000-0000-0000-0000-000000000000"
```

---

## Useful Local DB Access

```bash
# PostgreSQL
docker compose exec postgres psql -U flexprice -d flexprice

# ClickHouse
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice

# Check events in ClickHouse
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT id, external_customer_id, event_name FROM events ORDER BY timestamp DESC LIMIT 10 FORMAT PrettyCompact"

# Check raw_events in ClickHouse
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT count() FROM raw_events"
```

---

## Shutdown

```bash
# Kill the Go processes
pkill -f "flexprice-local"

# Stop all Docker services
docker compose down
```

---

## Common Pitfalls

| Problem | Cause | Fix |
|---------|-------|-----|
| `listen tcp :8080: address already in use` | Another server on port 8080 | Use `FLEXPRICE_SERVER_ADDRESS=":8081"` |
| `make migrate-ent` runs against production | Makefile loads `.env` first | Use `go run ./cmd/migrate/main.go` with explicit env overrides |
| Consumer errors: `topic does not exist` | Not all topics created | Run the full topic creation block in Step 4 |
| `{"error":"Unauthorized"}` on every request | API key not wired into config | Set `FLEXPRICE_AUTH_API_KEY_KEYS` JSON (Step 8) |
| Settings lookup returns "not found" | Wrong tenant/env in context | Always pass `x-environment-id` header; check that env vars have the right tenant/env |
| ClickHouse `raw_events` table is empty | The consumer doesn't write there directly | Raw events are stored in ClickHouse `raw_events` by the Bento collector; the Go consumer reads from Kafka `raw_events` topic and writes transformed events to ClickHouse `events` table |
| Consumer log flooded with `balance_alert` errors | Topic not created | Create it: `kafka-topics --create --topic balance_alert ...` |
| `go build` fails on `api/custom/go/...` | Generated SDK has broken dep | Use `go build ./internal/... ./cmd/...` instead of `./...` |

---

## Local Credentials Reference

| Service | Host | Port | User | Password | DB |
|---------|------|------|------|----------|----|
| PostgreSQL | localhost | 5432 | flexprice | flexprice123 | flexprice |
| ClickHouse | localhost | 9000 | flexprice | flexprice123 | flexprice |
| Kafka (external) | localhost | 29092 | — | — | — |
| Kafka (internal) | kafka | 9092 | — | — | — |

Default seed data:
- Tenant ID: `00000000-0000-0000-0000-000000000000`
- Environment ID (Sandbox): `00000000-0000-0000-0000-000000000000`
- Environment ID (Production): `00000000-0000-0000-0000-000000000001`
