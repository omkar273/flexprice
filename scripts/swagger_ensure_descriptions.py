#!/usr/bin/env python3
"""
Ensure Swagger endpoint descriptions exist in handler comments.

Rule:
- If @Description already exists for an endpoint doc block, keep it unchanged.
- If missing, insert one derived from @Summary.
"""

from __future__ import annotations

import re
from pathlib import Path


SUMMARY_RE = re.compile(r"^(\s*//\s*@Summary\s+)(.+)$")
DESCRIPTION_RE = re.compile(r"^\s*//\s*@Description\b")


def default_description(summary: str) -> str:
    s = summary.strip()
    low = s.lower()
    if low.startswith("list "):
        return "Returns a filtered, paginated list for this endpoint."
    if low.startswith("get ") or low.startswith("retrieve "):
        return "Returns the requested resource if found and accessible."
    if low.startswith("create "):
        return "Validates input and creates a new resource."
    if low.startswith("update "):
        return "Validates input and updates the target resource."
    if low.startswith("delete ") or low.startswith("remove "):
        return "Deletes the target resource and returns the operation result."
    if low.startswith("health check"):
        return "Returns service health and readiness information."
    return "Handles this endpoint operation."


def process_file(path: Path) -> int:
    lines = path.read_text(encoding="utf-8").splitlines()
    changed = 0
    i = 0
    while i < len(lines):
        sm = SUMMARY_RE.match(lines[i])
        if not sm:
            i += 1
            continue

        block_start = i
        block_end = i + 1
        while block_end < len(lines) and lines[block_end].lstrip().startswith("//"):
            block_end += 1

        has_description = any(DESCRIPTION_RE.match(lines[j]) for j in range(block_start, block_end))
        if not has_description:
            indent = sm.group(1).split("//")[0]
            desc = default_description(sm.group(2))
            lines.insert(i + 1, f"{indent}// @Description {desc}")
            changed += 1
            i = block_end + 1
        else:
            i = block_end

    if changed:
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return changed


def main() -> int:
    root = Path(__file__).resolve().parents[1] / "internal" / "api" / "v1"
    total_changes = 0
    files_changed = 0
    for file_path in sorted(root.glob("*.go")):
        c = process_file(file_path)
        if c:
            files_changed += 1
            total_changes += c

    print(f"swagger descriptions ensured: files_changed={files_changed} inserted={total_changes}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
