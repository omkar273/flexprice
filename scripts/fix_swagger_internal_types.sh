#!/bin/bash

# Replace string: github_com_flexprice_flexprice_internal_types. -> types.
# Applied to swagger.json, swagger.yaml, docs.go, and swagger-3-0.json (if present).
# Run from repository root.

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PREFIX="github_com_flexprice_flexprice_internal_types."
REPLACEMENT="types."

replace_in_file() {
    local f="$1"
    if [ -f "$f" ]; then
        python3 -c "
import sys
p = sys.argv[1]
old = sys.argv[2]
new = sys.argv[3]
with open(p) as f: s = f.read()
with open(p, 'w') as f: f.write(s.replace(old, new))
" "$f" "$PREFIX" "$REPLACEMENT"
        echo "Updated $f"
    fi
}

replace_in_file "docs/swagger/swagger.json"
replace_in_file "docs/swagger/swagger.yaml"
replace_in_file "docs/swagger/docs.go"
replace_in_file "docs/swagger/swagger-3-0.json"

echo "Replaced ${PREFIX} with ${REPLACEMENT} in swagger files."
