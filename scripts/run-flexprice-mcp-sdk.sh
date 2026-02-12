#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${FLEXPRICE_BASE_URL:-}" ]]; then
  echo "FLEXPRICE_BASE_URL is required"
  exit 1
fi

if [[ -z "${FLEXPRICE_API_KEY:-}" ]]; then
  echo "FLEXPRICE_API_KEY is required"
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK_DIR="$ROOT_DIR/sdk/mcp-server"
ENV_FILE="$ROOT_DIR/scripts/.env.mcp.local"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# Keep this in sync with generated package layout if Speakeasy adds MCP runtime files.
for candidate in \
  "$SDK_DIR/bin/mcp-server.js" \
  "$SDK_DIR/mcp-server/mcp-server.js" \
  "$SDK_DIR/mcp-server/server.js"; do
  if [[ -f "$candidate" ]]; then
    node "$candidate" start \
      --server-url "$FLEXPRICE_BASE_URL" \
      --api-key-auth "$FLEXPRICE_API_KEY"
    exit 0
  fi
done

echo "No runnable Node MCP entrypoint found in $SDK_DIR."
echo "This generated output currently appears to be SDK-only."
echo "Regenerate with 'make build-mcp' and ensure MCP runtime artifacts are present."
exit 1
