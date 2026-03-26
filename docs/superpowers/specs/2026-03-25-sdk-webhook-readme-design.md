# SDK Webhook README Design

**Date:** 2026-03-25
**Scope:** `api/custom/go/README.md`, `api/custom/python/README.md`, `api/custom/typescript/README.md`

## Goal

Add a `## Handling Webhooks` section to all three custom SDK READMEs. These files are `rsync`'d into generated SDK artifacts (`api/go/`, `api/python/`, `api/typescript/`) via `make merge-custom`, then pushed to the public SDK repos and published to GitHub/npm/PyPI on release.

## Decisions

- **Scope:** README updates only — no new example files
- **Signature verification:** Excluded — focus is parsing and routing
- **Placement:** Appended before `## Documentation` in each README
- **Structure:** Identical across all three SDKs (intro → flow → code → event table → rules)

## Section Structure (all three READMEs)

```
## Handling Webhooks

<one-line intro>

**Flow:**
1. Register endpoint in Flexprice dashboard
2. Receive POST with raw JSON body
3. Read `event_type` to route
4. Parse payload into typed struct
5. Handle business logic idempotently
6. Return 2xx quickly

<language-specific code example>

### Event types

| Category | Events |
|---|---|
...full table...

**Production rules:**
- Keep handlers idempotent — Flexprice retries on non-2xx
- Return 2xx for unknown event types — prevents unnecessary retries
- Do heavy processing async — respond fast, queue the work
```

## Go Implementation Detail

**Imports:**
- `github.com/flexprice/go-sdk/v2/models/types` for `WebhookEventName*` constants and `WebhookDto*` structs

**Pattern:**
1. Unmarshal raw body into a cheap envelope struct (`struct{ EventType types.WebhookEventName }`)
2. Switch on `env.EventType` using `types.WebhookEventName*` constants
3. For each matched case: `json.Unmarshal(body, &payload)` into the specific `types.WebhookDto*WebhookPayload` struct
4. Access data via nil-safe getter methods (`payload.GetPayment()`, `payload.GetSubscription()`, etc.)

**Representative events shown:** payment (success + failed), subscription (activated + cancelled), invoice (finalized + overdue)

## Python Implementation Detail

**Imports:**
- `from flexprice.models import WebhookDtoPaymentWebhookPayload, WebhookDtoSubscriptionWebhookPayload, WebhookDtoInvoiceWebhookPayload`
- `WebhookEventName` is a `Union[Literal[...], UnrecognizedStr]` — use string literals directly in match/if

**Pattern:**
1. `event = json.loads(raw_body)`
2. `match event.get("event_type"):` with `case "payment.success" | "payment.failed":` etc.
3. `Model.model_validate(event)` to parse into typed Pydantic model
4. Access `payload.payment`, `payload.subscription`, etc.

**Representative events shown:** payment (success + failed), subscription (activated + cancelled), invoice (finalized + overdue)

## TypeScript Implementation Detail

**Imports:**
- `WebhookEventName` and `webhookDto*FromJSON` from `@flexprice/sdk` root (re-exported via `index.extras.ts` → `sdk/models/index.js`)
- `webhookDto*FromJSON` helpers (e.g. `webhookDtoPaymentWebhookPayloadFromJSON`) — return `SafeParseResult<T, SDKValidationError>`
- Fields auto-camelCased: `event_type` → `eventType`, `invoice_status` → `invoiceStatus`

**Pattern:**
1. `const env = JSON.parse(rawBody)` to read `event_type`
2. `switch (env.event_type as WebhookEventName)` using `WebhookEventName.PaymentSuccess` etc.
3. `webhookDto*FromJSON(rawBody)` → check `result.ok` before accessing `result.value`

**Representative events shown:** payment (success + failed), subscription (activated + cancelled), invoice (finalized + overdue)

## Event Types Table (shared across all three READMEs)

| Category | Events |
|---|---|
| **Payment** | `payment.created` · `payment.updated` · `payment.success` · `payment.failed` · `payment.pending` |
| **Invoice** | `invoice.create.drafted` · `invoice.update` · `invoice.update.finalized` · `invoice.update.payment` · `invoice.update.voided` · `invoice.payment.overdue` · `invoice.communication.triggered` |
| **Subscription** | `subscription.created` · `subscription.draft.created` · `subscription.activated` · `subscription.updated` · `subscription.paused` · `subscription.resumed` · `subscription.cancelled` · `subscription.renewal.due` |
| **Subscription Phase** | `subscription.phase.created` · `subscription.phase.updated` · `subscription.phase.deleted` |
| **Customer** | `customer.created` · `customer.updated` · `customer.deleted` |
| **Wallet** | `wallet.created` · `wallet.updated` · `wallet.terminated` · `wallet.transaction.created` · `wallet.credit_balance.dropped` · `wallet.credit_balance.recovered` · `wallet.ongoing_balance.dropped` · `wallet.ongoing_balance.recovered` |
| **Feature / Entitlement** | `feature.created` · `feature.updated` · `feature.deleted` · `feature.wallet_balance.alert` · `entitlement.created` · `entitlement.updated` · `entitlement.deleted` |
| **Credit Note** | `credit_note.created` · `credit_note.updated` |

## Files Changed

| File | Change |
|---|---|
| `api/custom/go/README.md` | Append `## Handling Webhooks` before `## Documentation` |
| `api/custom/python/README.md` | Append `## Handling Webhooks` before `## Documentation` |
| `api/custom/typescript/README.md` | Append `## Handling Webhooks` before `## Documentation` |

No new files. No changes to examples, go.mod, requirements.txt, or GitHub Actions workflow.
