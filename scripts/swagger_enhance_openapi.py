#!/usr/bin/env python3
"""
Post-process generated OpenAPI JSON to improve generator compatibility.

Phase 1 scope:
- Ensure every operation has operationId.
- Ensure top-level tags include all operation tags.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


HTTP_METHODS = {"get", "post", "put", "patch", "delete", "head", "options", "trace"}


def slugify_path(path: str) -> str:
    # /customers/{id}/wallets -> customers_by_id_wallets
    parts = [p for p in path.strip("/").split("/") if p]
    converted = []
    for part in parts:
        if part.startswith("{") and part.endswith("}"):
            key = part[1:-1].strip()
            converted.append(f"by_{re.sub(r'[^a-zA-Z0-9_]+', '_', key).lower()}")
        else:
            converted.append(re.sub(r"[^a-zA-Z0-9_]+", "_", part).lower())
    return "_".join([p for p in converted if p]) or "root"


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: swagger_enhance_openapi.py <openapi-json-path>", file=sys.stderr)
        return 2

    spec_path = Path(sys.argv[1])
    if not spec_path.exists():
        print(f"error: file not found: {spec_path}", file=sys.stderr)
        return 2

    data = json.loads(spec_path.read_text(encoding="utf-8"))

    paths = data.get("paths", {})
    if not isinstance(paths, dict):
        print("error: invalid OpenAPI 'paths' section", file=sys.stderr)
        return 2

    operation_count = 0
    generated_operation_ids = 0
    discovered_tags: set[str] = set()

    for path, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method.lower() not in HTTP_METHODS or not isinstance(op, dict):
                continue

            operation_count += 1

            if not op.get("operationId"):
                op["operationId"] = f"{method.lower()}_{slugify_path(path)}"
                generated_operation_ids += 1

            tags = op.get("tags", [])
            if isinstance(tags, list):
                for tag in tags:
                    if isinstance(tag, str) and tag.strip():
                        discovered_tags.add(tag.strip())

    existing_tag_entries = data.get("tags", [])
    existing_tag_names = set()
    if isinstance(existing_tag_entries, list):
        for entry in existing_tag_entries:
            if isinstance(entry, dict) and isinstance(entry.get("name"), str):
                existing_tag_names.add(entry["name"].strip())
    else:
        existing_tag_entries = []

    for tag_name in sorted(discovered_tags):
        if tag_name not in existing_tag_names:
            existing_tag_entries.append({"name": tag_name})

    data["tags"] = existing_tag_entries

    spec_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )

    print(
        f"Enhanced {spec_path}: operations={operation_count}, "
        f"generated_operation_ids={generated_operation_ids}, "
        f"top_level_tags={len(existing_tag_entries)}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
