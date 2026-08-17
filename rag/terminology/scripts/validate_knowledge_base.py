"""Validate generated terminology JSONL without third-party dependencies."""

from __future__ import annotations

import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "source" / "terminology.json"
TERMS = ROOT / "data" / "terms.jsonl"


def fail(message: str) -> None:
    raise SystemExit(f"validation failed: {message}")


def main() -> None:
    source = json.loads(SOURCE.read_text(encoding="utf-8"))
    expected = sum(len(group["items"]) for group in source["groups"])
    lines = TERMS.read_text(encoding="utf-8").splitlines()
    if len(lines) != expected:
        fail(f"expected {expected} JSONL documents, found {len(lines)}")

    ids: set[str] = set()
    terms: set[str] = set()
    pattern = re.compile(r"^anime\.[a-z0-9_]+\.[a-f0-9]{8}$")
    for line_number, line in enumerate(lines, start=1):
        try:
            doc = json.loads(line)
        except json.JSONDecodeError as exc:
            fail(f"line {line_number} is invalid JSON: {exc}")
        if not pattern.match(doc.get("id", "")):
            fail(f"line {line_number} has invalid id")
        if doc["id"] in ids:
            fail(f"duplicate id {doc['id']}")
        ids.add(doc["id"])
        metadata = doc.get("metadata", {})
        term = metadata.get("term")
        if not term or term in terms:
            fail(f"line {line_number} has missing or duplicate canonical term")
        terms.add(term)
        for key in ("content", "prompt_fragments", "retrieval_keywords", "prompt_targets"):
            if key == "content" and not isinstance(doc.get(key), str):
                fail(f"line {line_number} content must be a string")
            if key != "content" and key not in metadata:
                fail(f"line {line_number} missing metadata.{key}")
        if not set(metadata["prompt_targets"]) <= {"text_to_3d", "preview_image"}:
            fail(f"line {line_number} has unknown prompt target")
    print(f"validation passed: {len(lines)} documents, {len(terms)} unique terms")


if __name__ == "__main__":
    main()
