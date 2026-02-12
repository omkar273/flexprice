# FlexPrice MCP Server Guide

This is the canonical MCP guide for this repository.

For client onboarding (Cursor + Claude), see:

- `docs/mcp/client-setup.md`

The MCP pipeline is OpenAPI-driven and generated with Speakeasy:

- source of truth: `docs/swagger/swagger-3-0.json`
- generated output: `sdk/mcp-server`
- runtime wrapper: `scripts/run-flexprice-mcp-sdk.sh`

---

## Prerequisites

- Speakeasy CLI
- Node.js runtime
- valid FlexPrice API credentials

Install Speakeasy:

```bash
make speakeasy-install
speakeasy --version
```

---

## Build MCP

```bash
make build-mcp
```

This runs a standalone MCP workflow and writes output to:

```bash
sdk/mcp-server
```

---

## Run MCP

```bash
make run-mcp
```

`run-mcp` is an alias for `run-mcp-sdk` and starts the generated Node/Speakeasy runtime wrapper.

Required environment variables:

- `FLEXPRICE_BASE_URL`
- `FLEXPRICE_API_KEY`

You can load them from:

- `scripts/.env.mcp.local` (recommended for local development)

---

## CI validation

Use `.github/workflows/mcp-validate.yml` to validate:

1. OpenAPI generation
2. MCP generation
3. MCP output isolation (`sdk/mcp-server`)
4. idempotency (second run yields no diff)

---

## Troubleshooting

| Symptom                               | Cause                        | Fix                                                     |
| ------------------------------------- | ---------------------------- | ------------------------------------------------------- |
| `speakeasy: command not found`        | CLI not installed            | `make speakeasy-install`                                |
| OpenAPI spec not found                | swagger not generated        | `make swagger`                                          |
| No runnable Node MCP entrypoint found | generated output is SDK-only | rerun `make build-mcp`, check `sdk/mcp-server` contents |
| MCP auth failures                     | wrong key or URL             | verify `FLEXPRICE_BASE_URL` + `FLEXPRICE_API_KEY`       |

---

## Security

- never commit API keys/tokens
- rotate keys if exposed
- scope credentials to least privilege
