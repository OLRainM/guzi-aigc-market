from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable


@dataclass(frozen=True)
class TermDocument:
    id: str
    term: str
    category: str
    aliases: tuple[str, ...]
    keywords: tuple[str, ...]
    fragment_3d: str
    negatives: tuple[str, ...]
    content: str
    knowledge_version: str


def _safe_parent(path: Path, index: int) -> Path | None:
    if len(path.parents) <= index:
        return None
    return path.parents[index]


def _existing_path(*candidates: Path | None) -> Path:
    valid: list[Path] = []
    for path in candidates:
        if path is None or not str(path).strip():
            continue
        valid.append(path)
        if path.is_file():
            return path
    if not valid:
        raise FileNotFoundError("no RAG path candidates configured")
    return valid[0]


def default_terms_path() -> Path:
    here = Path(__file__).resolve()
    repo_root = _safe_parent(here, 3)
    return _existing_path(
        Path(os.getenv("RAG_TERMS_PATH", "")),
        Path("/app/rag/terminology/data/terms.jsonl"),
        (repo_root / "rag" / "terminology" / "data" / "terms.jsonl") if repo_root else None,
        here.parents[1] / "rag" / "terminology" / "data" / "terms.jsonl",
    )


def default_rules_path() -> Path:
    here = Path(__file__).resolve()
    repo_root = _safe_parent(here, 3)
    return _existing_path(
        Path(os.getenv("RAG_RULES_PATH", "")),
        Path("/app/rag/terminology/rules/compatibility.json"),
        (repo_root / "rag" / "terminology" / "rules" / "compatibility.json") if repo_root else None,
    )


def default_system_prompt_path() -> Path:
    here = Path(__file__).resolve()
    repo_root = _safe_parent(here, 3)
    return _existing_path(
        Path(os.getenv("RAG_SYSTEM_PROMPT_PATH", "")),
        Path("/app/rag/terminology/prompts/optimizer-system.zh-CN.md"),
        (repo_root / "rag" / "terminology" / "prompts" / "optimizer-system.zh-CN.md") if repo_root else None,
    )


class TerminologyIndex:
    def __init__(self, terms_path: Path | None = None, rules_path: Path | None = None) -> None:
        self.terms_path = terms_path or default_terms_path()
        self.rules_path = rules_path or default_rules_path()
        self.documents = tuple(load_terms(self.terms_path))
        self.rules = load_rules(self.rules_path)
        self.knowledge_version = self.documents[0].knowledge_version if self.documents else "0"

    def search(self, query: str, product_type: str = "", limit: int = 8) -> list[TermDocument]:
        haystack = f"{query} {product_type}".lower()
        scored: list[tuple[int, TermDocument]] = []
        for doc in self.documents:
            score = 0
            for keyword in (doc.term, *doc.aliases, *doc.keywords):
                token = keyword.strip().lower()
                if token and token in haystack:
                    score += 3 if token == doc.term.lower() else 2
            if product_type and doc.category == "goods_type" and product_type.lower() in " ".join((doc.term, *doc.aliases)).lower():
                score += 4
            if score:
                scored.append((score, doc))
        scored.sort(key=lambda item: (-item[0], item[1].term))
        selected: list[TermDocument] = []
        seen_goods = False
        for _, doc in scored:
            if doc.category == "goods_type":
                if seen_goods:
                    continue
                seen_goods = True
            selected.append(doc)
            if len(selected) >= limit:
                break
        return selected


def load_terms(path: Path) -> Iterable[TermDocument]:
    if not path.is_file():
        return []
    documents: list[TermDocument] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        raw = json.loads(line)
        metadata = raw.get("metadata") if isinstance(raw.get("metadata"), dict) else {}
        fragments = metadata.get("prompt_fragments") if isinstance(metadata.get("prompt_fragments"), dict) else {}
        documents.append(
            TermDocument(
                id=str(raw.get("id") or ""),
                term=str(metadata.get("term") or ""),
                category=str(metadata.get("category") or ""),
                aliases=tuple(str(item) for item in metadata.get("aliases") or []),
                keywords=tuple(str(item) for item in metadata.get("retrieval_keywords") or []),
                fragment_3d=str(fragments.get("text_to_3d") or ""),
                negatives=tuple(str(item) for item in fragments.get("negative") or []),
                content=str(raw.get("content") or ""),
                knowledge_version=str(metadata.get("knowledge_version") or "1.0.0"),
            )
        )
    return documents


def load_rules(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {"selection_rules": [], "negative_prompt_defaults": []}
    return json.loads(path.read_text(encoding="utf-8"))
