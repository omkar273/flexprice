#!/bin/bash

# Renames Swagger definitions from github_com_flexprice_flexprice_internal_types.X to types.X
# in swagger.json, swagger.yaml, and docs.go so API docs show clean names like types.Status.
# Run from repository root.

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

SWAGGER_JSON="docs/swagger/swagger.json"
SWAGGER_YAML="docs/swagger/swagger.yaml"
DOCS_GO="docs/swagger/docs.go"
PREFIX="github_com_flexprice_flexprice_internal_types."
REPLACEMENT="types."

# --- swagger.json: rename definition keys and all $refs ---
if [ ! -f "$SWAGGER_JSON" ]; then
    echo "Error: $SWAGGER_JSON not found. Run 'make swagger-2-0' first."
    exit 1
fi

python3 << PYEOF
import json
import os

base = os.getcwd()
path_json = os.path.join(base, "docs", "swagger", "swagger.json")
path_yaml = os.path.join(base, "docs", "swagger", "swagger.yaml")
path_go = os.path.join(base, "docs", "swagger", "docs.go")
prefix = "github_com_flexprice_flexprice_internal_types."
replacement = "types."

# --- 1. swagger.json ---
with open(path_json) as f:
    spec = json.load(f)

definitions = spec.get("definitions", {})
new_defs = {}
for k, v in definitions.items():
    if k.startswith(prefix):
        new_defs[replacement + k[len(prefix):]] = v
    else:
        new_defs[k] = v
spec["definitions"] = new_defs

content = json.dumps(spec, indent=2)
content = content.replace("#/definitions/" + prefix, "#/definitions/" + replacement)
with open(path_json, "w") as f:
    f.write(content)
print("Updated", path_json)

# --- 2. swagger.yaml: replace refs and definition keys ---
with open(path_yaml) as f:
    content = f.read()
content = content.replace(prefix, replacement)
with open(path_yaml, "w") as f:
    f.write(content)
print("Updated", path_yaml)

# --- 3. docs.go: replace in embedded spec string ---
with open(path_go) as f:
    content = f.read()
content = content.replace(prefix, replacement)
with open(path_go, "w") as f:
    f.write(content)
print("Updated", path_go)
PYEOF

echo "Renamed internal types to types.* in swagger.json, swagger.yaml, and docs.go"
