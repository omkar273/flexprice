# Install FlexPrice MCP Server in Claude and Cursor

This guide gives a production-friendly setup for FlexPrice MCP in:
- Claude Code CLI
- Claude Desktop
- Cursor

It includes:
- Remote MCP setup (recommended once hosted endpoint is available)
- Local MCP setup (works today with generated Node/Speakeasy runtime)
- Secure token handling
- Verification and troubleshooting
- Guidance on cleaning generated TS/JS runtime artifacts

---

## Prerequisites

- FlexPrice API credentials:
  - `FLEXPRICE_BASE_URL` (example: `https://api.cloud.flexprice.io`)
  - `FLEXPRICE_API_KEY`
- For local setup:
  - Node runtime available
  - Generated MCP artifacts under `sdk/mcp-server`

From repo root:

```bash
make speakeasy-install
make build-mcp
```

Optional local env bootstrap:

```bash
cp scripts/.env.mcp.sample scripts/.env.mcp.local
```

The local runtime wrapper auto-loads `scripts/.env.mcp.local` when present.

---

## Credential Safety

Avoid committing credentials in config JSON.

1. Store secrets in local env file (example):

```bash
FLEXPRICE_BASE_URL=https://api-dev.cloud.flexprice.io
FLEXPRICE_API_KEY=sk_your_api_key
```

2. Ensure local secret files are ignored by git.

---

## Claude Code CLI

### Remote Server Setup (recommended once remote endpoint exists)

When a hosted FlexPrice MCP endpoint is available (example: `https://mcp.flexprice.io/mcp`):

```bash
claude mcp add --transport http flexprice https://mcp.flexprice.io/mcp
```

If bearer header is required:

```bash
claude mcp add --transport http flexprice https://mcp.flexprice.io/mcp -H "Authorization: Bearer YOUR_TOKEN"
```

Then:
1. Restart Claude Code
2. Run `claude mcp list`
3. Run `claude mcp get flexprice`

### Local Server Setup (available now)

Use the local wrapper script:

```bash
claude mcp add flexprice -- bash /absolute/path/to/flexprice/scripts/run-flexprice-mcp-sdk.sh
```

If you need explicit env in command:

```bash
claude mcp add flexprice -e FLEXPRICE_BASE_URL=https://api-dev.cloud.flexprice.io -e FLEXPRICE_API_KEY=sk_your_api_key -- bash /absolute/path/to/flexprice/scripts/run-flexprice-mcp-sdk.sh
```

Then:
1. Restart Claude Code
2. Run `claude mcp list`
3. Run `claude mcp get flexprice`

---

## Claude Desktop

Config file:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

### Local Server Setup (recommended for current FlexPrice repo state)

Add this in your Claude Desktop config:

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "bash",
      "args": [
        "/absolute/path/to/flexprice/scripts/run-flexprice-mcp-sdk.sh"
      ],
      "env": {
        "FLEXPRICE_BASE_URL": "https://api-dev.cloud.flexprice.io",
        "FLEXPRICE_API_KEY": "sk_your_api_key"
      }
    }
  }
}
```

Restart Claude Desktop after saving.

### Remote Server Setup (future/hosted)

When hosted endpoint is available, use:

```json
{
  "mcpServers": {
    "flexprice": {
      "url": "https://mcp.flexprice.io/mcp",
      "type": "http"
    }
  }
}
```

---

## Cursor

Edit `.cursor/mcp.json`:

### Local server (available now)

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "bash",
      "args": [
        "/absolute/path/to/flexprice/scripts/run-flexprice-mcp-sdk.sh"
      ],
      "env": {
        "FLEXPRICE_BASE_URL": "https://api-dev.cloud.flexprice.io",
        "FLEXPRICE_API_KEY": "sk_your_api_key"
      }
    }
  }
}
```

### Remote server (future/hosted)

```json
{
  "mcpServers": {
    "flexprice": {
      "url": "https://mcp.flexprice.io/mcp",
      "type": "http"
    }
  }
}
```

Restart Cursor after config updates.

---

## Cursor Setup Guide (Step-by-Step)

Use this section if you only want a Cursor-focused setup flow.

### Prerequisites

- Cursor installed
- FlexPrice credentials:
  - `FLEXPRICE_BASE_URL`
  - `FLEXPRICE_API_KEY`
- For local setup, generated MCP runtime:

```bash
make speakeasy-install
make build-mcp
```

### Option A: Local Server (works today)

1. Open your project in Cursor.
2. Create or edit `.cursor/mcp.json`.
3. Add:

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "bash",
      "args": [
        "/absolute/path/to/flexprice/scripts/run-flexprice-mcp-sdk.sh"
      ],
      "env": {
        "FLEXPRICE_BASE_URL": "https://api-dev.cloud.flexprice.io",
        "FLEXPRICE_API_KEY": "sk_your_api_key"
      }
    }
  }
}
```

4. Save file and fully restart Cursor.
5. Start a new chat and ask to list MCP tools.

### Option B: Remote Server (when hosted endpoint is available)

When FlexPrice remote MCP is live, use:

```json
{
  "mcpServers": {
    "flexprice": {
      "url": "https://mcp.flexprice.io/mcp",
      "type": "http"
    }
  }
}
```

Then restart Cursor and verify MCP tools are visible.

### Cursor Verification Checklist

1. Open a fresh Cursor chat/session.
2. Ask to list FlexPrice MCP tools.
3. Run a safe read request (for example list customers).
4. Confirm no auth errors (`401`/`403`).

### Cursor Troubleshooting

- **Tools do not appear**
  - Confirm `.cursor/mcp.json` JSON is valid.
  - Restart Cursor fully.
- **Auth errors**
  - Recheck `FLEXPRICE_BASE_URL` and `FLEXPRICE_API_KEY`.
- **Local command fails**
  - Ensure absolute path to `scripts/run-flexprice-mcp-sdk.sh` is correct.
  - Ensure `make build-mcp` was run.

---

## Verification

1. Start a fresh chat/session in your MCP client.
2. Ask the assistant to list available FlexPrice MCP tools.
3. Run one safe read operation (for example list customers).
4. Confirm no auth error (`401`/`403`).

For Claude Code:

```bash
claude mcp list
claude mcp get flexprice
```

---

## Generated TS/JS Runtime Cleanup Policy

Short answer: do not delete generated TS/JS files required by runtime if you still run local MCP.

### What is safe

- Keep only runtime-required generated output under `sdk/mcp-server`.
- Remove temporary build caches or intermediate artifacts not referenced by runtime.
- Regenerate artifacts with `make build-mcp` whenever spec changes.

### What is not safe

- Deleting JS entrypoints consumed by `scripts/run-flexprice-mcp-sdk.sh`.
- Deleting package metadata or runtime dependency files required to launch Node MCP.

### Recommended pattern

1. Generate in CI with `make build-mcp`.
2. Keep generated runtime artifacts versioned if needed for reproducible local setup.
3. Add an explicit cleanup target that removes only non-runtime artifacts.
4. Validate cleanup by running:

```bash
make run-mcp
```

If runtime starts and tools are visible, cleanup policy is correct.

---

## Troubleshooting

### "FLEXPRICE_BASE_URL is required" or "FLEXPRICE_API_KEY is required"
- Missing env vars in MCP config or local env file.

### "OpenAPI spec not found"
- Run `make swagger` to regenerate `docs/swagger/swagger-3-0.json`.

### "No runnable Node MCP entrypoint found"
- Generated output is SDK-only or incomplete.
- Re-run `make build-mcp`.

### Tools not visible
- Verify absolute path to `scripts/run-flexprice-mcp-sdk.sh`.
- Ensure script is executable.
- Fully restart MCP client.

---

## Notes

- The local FlexPrice setup currently uses command-based stdio transport.
- Remote HTTP setup is the target for Figma-like onboarding UX.
- Do not commit API keys; rotate immediately if exposed.
