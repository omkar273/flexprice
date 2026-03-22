# Local Testing Guide for AI Agents

Everything needed to run Flexprice locally and validate changes end-to-end.
**No manual env var exports or configuration required** — `.env.local` is committed and wired in automatically.

---

## Prerequisites

- **OrbStack** (or Docker Desktop) installed
- **Go 1.23+** installed

---

## Pre-configured Local API Key

Use this key for all local API calls — no setup needed:

```
x-api-key: sk_local_flexprice_test_key
x-environment-id: 00000000-0000-0000-0000-000000000000
```

Tenant ID: `00000000-0000-0000-0000-000000000000` (Default Tenant)
Environment ID: `00000000-0000-0000-0000-000000000000` (Sandbox)

---

## Quick Start

### 1. Start OrbStack

```bash
open -a OrbStack && sleep 3
```

### 2. Start local infrastructure

```bash
docker compose up -d postgres kafka clickhouse
sleep 10  # wait for Kafka to fully initialize
```

### 3. Create Kafka topics

Run once (safe to repeat — fails gracefully if topics exist):

```bash
for topic in raw_events events events_lazy events_post_processing events_backfill system_events onboarding_events balance_alert; do
  docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 \
    --create --topic $topic --partitions 3 --replication-factor 1 2>/dev/null || true
done
```

### 4. Run migrations

```bash
# PostgreSQL SQL migrations
make migrate-postgres

# Ent schema migrations (local postgres — explicit overrides required!)
FLEXPRICE_POSTGRES_HOST=localhost \
FLEXPRICE_POSTGRES_PORT=5432 \
FLEXPRICE_POSTGRES_USER=flexprice \
FLEXPRICE_POSTGRES_PASSWORD=flexprice123 \
FLEXPRICE_POSTGRES_DBNAME=flexprice \
FLEXPRICE_POSTGRES_SSLMODE=disable \
go run ./cmd/migrate/main.go

# ClickHouse migrations
make migrate-clickhouse
```

> **Warning:** `make migrate-ent` reads `.env` first and will run against production if that file points there. Always use `go run ./cmd/migrate/main.go` with explicit local overrides as shown above.

### 5. Build and run

**Terminal 1 — API server:**
```bash
go build -o /tmp/flexprice-local ./cmd/server/main.go
FLEXPRICE_DEPLOYMENT_MODE=api /tmp/flexprice-local
```

**Terminal 2 — Consumer:**
```bash
FLEXPRICE_DEPLOYMENT_MODE=consumer /tmp/flexprice-local
```

Or run everything in a single process (API + consumer + worker):
```bash
FLEXPRICE_DEPLOYMENT_MODE=local /tmp/flexprice-local
```

### 6. Verify

```bash
# Health check
curl http://localhost:8080/health
# → {"status":"ok"}

# Auth check
curl http://localhost:8080/v1/meters \
  -H "x-api-key: sk_local_flexprice_test_key" \
  -H "x-environment-id: 00000000-0000-0000-0000-000000000000"
# → {"items":[],"pagination":{...}}
```

---

## How `.env.local` Works

`config.go` loads env files in this order (later wins):

```
.env          ← production/shared defaults (gitignored, contains real secrets)
.env.local    ← local overrides (committed, safe — no real secrets)
```

`.env.local` overrides all infra endpoints to local Docker, disables Supabase/Sentry/email/DynamoDB, and wires the pre-configured API key. You never need to manually export env vars.

---

## End-to-End Test Example

```bash
BASE="http://localhost:8080/v1"
KEY="sk_local_flexprice_test_key"
ENV_HDR="x-environment-id: 00000000-0000-0000-0000-000000000000"

# 1. Create a customer
curl -s -X POST $BASE/customers \
  -H "x-api-key: $KEY" -H "$ENV_HDR" -H "Content-Type: application/json" \
  -d '{"external_id": "org_test_001", "name": "Test Org"}'

# 2. Configure event ingestion filter
curl -s -X PUT $BASE/settings/event_ingestion_filter \
  -H "x-api-key: $KEY" -H "$ENV_HDR" -H "Content-Type: application/json" \
  -d '{"value": {"enabled": true, "allowed_external_customer_ids": ["org_test_001"]}}'

# 3. Ingest raw events (Bento format → raw_events Kafka topic)
curl -s -X POST $BASE/events/raw/bulk \
  -H "x-api-key: $KEY" -H "$ENV_HDR" -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"id":"evt-001","orgId":"org_test_001","methodName":"api_call","providerName":"openai","createdAt":"2026-01-01T00:00:00Z","data":{}},
      {"id":"evt-002","orgId":"org_blocked_001","methodName":"api_call","providerName":"openai","createdAt":"2026-01-01T00:00:00Z","data":{}}
    ]
  }'
# → {"batch_size":2,"message":"Raw events accepted for processing"}
# Consumer will process: success_count=1, skip_count=1

# 4. Verify in ClickHouse — only the allowed org's event should appear
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT id, external_customer_id, event_name FROM events ORDER BY timestamp DESC LIMIT 5 FORMAT PrettyCompact"
```

---

## Useful Commands

```bash
# PostgreSQL shell
docker compose exec postgres psql -U flexprice -d flexprice

# ClickHouse shell
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice

# Watch consumer logs (filter to signal lines)
tail -f /tmp/consumer.log | grep -E "processing|completed|filter|error"

# Stop everything
pkill -f flexprice-local
docker compose down
```

---

## Local Infrastructure Reference

| Service | Host:Port | User | Password | DB |
|---------|-----------|------|----------|----|
| PostgreSQL | `localhost:5432` | `flexprice` | `flexprice123` | `flexprice` |
| ClickHouse | `localhost:9000` | `flexprice` | `flexprice123` | `flexprice` |
| Kafka | `localhost:29092` | — | — | — |

| Entity | ID |
|--------|----|
| Default Tenant | `00000000-0000-0000-0000-000000000000` |
| Sandbox Environment | `00000000-0000-0000-0000-000000000000` |
| Production Environment | `00000000-0000-0000-0000-000000000001` |

---

## Adding a New Local API Key

```bash
# 1. Pick any key string and compute its SHA-256
echo -n "sk_my_new_key" | sha256sum | awk '{print $1}'

# 2. Add to FLEXPRICE_AUTH_API_KEY_KEYS in .env.local:
# {"<hash>": {"tenant_id": "00000000-...", "user_id": "dev", "name": "my-key", "is_active": true}}
```

---

## Common Pitfalls

| Problem | Cause | Fix |
|---------|-------|-----|
| `make migrate-ent` hits production | Makefile loads `.env` before overrides | Use `go run ./cmd/migrate/main.go` with explicit env vars (Step 4) |
| `listen tcp :8080: address already in use` | Another process on 8080 | `lsof -ti:8080 | xargs kill` or use `FLEXPRICE_SERVER_ADDRESS=":8081"` |
| Consumer: `topic does not exist` errors | Topic not created yet | Run the topic creation loop in Step 3 |
| `{"error":"Unauthorized"}` | API key not matching | Use `sk_local_flexprice_test_key` — it's pre-wired in `.env.local` |
| ClickHouse `raw_events` table empty | Consumer doesn't write there | The consumer writes to ClickHouse `events`, not `raw_events` (Bento writes raw_events directly) |
| Settings lookup fails with "not found" | Missing `x-environment-id` header | Always include `-H "x-environment-id: 00000000-0000-0000-0000-000000000000"` |
