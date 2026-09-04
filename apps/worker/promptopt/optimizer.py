from __future__ import annotations

import json
import logging
import os
import re
from dataclasses import dataclass
from typing import Any, Mapping

try:
    import httpx
except ImportError:  # pragma: no cover - local tests without worker deps
    httpx = None  # type: ignore[assignment]

from promptopt.knowledge import TerminologyIndex, TermDocument, default_system_prompt_path

logger = logging.getLogger("ai-worker")

PROMPT_CHAR_LIMIT = 1024
TEMPLATE_VERSION = "text-to-3d-template.zh-CN@1.0.0"
FALLBACK_CONSTRAINTS = (
    "干净拓扑，左右结构合理，无悬浮几何体，无自相交，封闭流形网格，适合导出 GLB。"
    "主体完整可独立展示，轮廓和分件清晰。"
)


@dataclass(frozen=True)
class OptimizedPrompt:
    text: str
    product_type: str | None
    structured: dict[str, Any]
    rag_context: dict[str, Any]
    rag_version: str
    template_version: str
    source: str


class PromptOptimizer:
    def __init__(
        self,
        index: TerminologyIndex | None = None,
        client: Any = None,
    ) -> None:
        self.index = index or TerminologyIndex()
        self.system_prompt = _load_system_prompt()
        self.model = os.getenv("LLM_MODEL", "gpt-4o-mini")
        self.base_url = (os.getenv("LLM_BASE_URL") or "").rstrip("/")
        self.api_key = os.getenv("LLM_API_KEY", "")
        self._owns_client = client is None
        timeout = float(os.getenv("LLM_TIMEOUT", "30"))
        headers = {"Accept": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        if client is not None:
            self._client = client
        elif httpx is not None:
            self._client = httpx.AsyncClient(timeout=timeout, headers=headers)
        else:
            self._client = None
            self._owns_client = False

    async def aclose(self) -> None:
        if self._owns_client and self._client is not None:
            await self._client.aclose()

    async def optimize(self, raw_prompt: str, product_type: str = "") -> OptimizedPrompt:
        query = raw_prompt.strip()
        hits = self.index.search(query, product_type=product_type)
        fallback = self._fallback(query, product_type, hits)
        if not self.api_key or not self.base_url or self._client is None:
            return fallback
        try:
            llm = await self._llm_optimize(query, product_type, hits)
            if llm is not None:
                return llm
        except Exception:
            logger.exception("llm prompt optimization failed; using RAG fallback")
        return fallback

    def _fallback(self, query: str, product_type: str, hits: list[TermDocument]) -> OptimizedPrompt:
        parts = [query]
        if product_type:
            parts.append(f"商品类型：{product_type}")
        for doc in hits:
            if doc.fragment_3d:
                parts.append(doc.fragment_3d)
        parts.append(FALLBACK_CONSTRAINTS)
        text = _truncate("；".join(part.strip("；。 ") for part in parts if part.strip()))
        negatives = list(dict.fromkeys(
            list(self.index.rules.get("negative_prompt_defaults") or [])
            + [item for doc in hits for item in doc.negatives]
        ))
        structured = {
            "normalized_intent": query,
            "product_type": product_type or None,
            "text_to_3d_prompt": text,
            "negative_prompt": negatives,
            "retrieved_term_ids": [doc.id for doc in hits],
            "assumptions": ["LLM 未启用或调用失败，已用术语库拼接文生 3D Prompt。"],
        }
        return OptimizedPrompt(
            text=text,
            product_type=product_type or None,
            structured=structured,
            rag_context=_rag_context(hits, "lexical_fallback"),
            rag_version=self.index.knowledge_version,
            template_version=TEMPLATE_VERSION,
            source="rag_fallback",
        )

    async def _llm_optimize(self, query: str, product_type: str, hits: list[TermDocument]) -> OptimizedPrompt | None:
        user = _user_message(query, product_type, hits, self.index.rules)
        payload = {
            "model": self.model,
            "temperature": 0.2,
            "messages": [
                {"role": "system", "content": self.system_prompt},
                {"role": "user", "content": user},
            ],
        }
        url = f"{self.base_url}/chat/completions"
        response = await self._client.post(url, json=payload)
        response.raise_for_status()
        body = response.json()
        content = _message_content(body)
        parsed = _parse_json_object(content)
        if parsed is None:
            return None
        text = str(parsed.get("text_to_3d_prompt") or "").strip()
        if not text:
            return None
        text = _truncate(text)
        parsed["retrieved_term_ids"] = parsed.get("retrieved_term_ids") or [doc.id for doc in hits]
        return OptimizedPrompt(
            text=text,
            product_type=(str(parsed.get("product_type") or product_type) or None),
            structured=parsed,
            rag_context=_rag_context(hits, "llm"),
            rag_version=self.index.knowledge_version,
            template_version=TEMPLATE_VERSION,
            source="llm",
        )


def _rag_context(hits: list[TermDocument], mode: str) -> dict[str, Any]:
    return {
        "mode": mode,
        "term_ids": [doc.id for doc in hits],
        "terms": [doc.term for doc in hits],
        "categories": [doc.category for doc in hits],
    }


def _user_message(query: str, product_type: str, hits: list[TermDocument], rules: Mapping[str, Any]) -> str:
    retrieved = [
        {
            "id": doc.id,
            "term": doc.term,
            "category": doc.category,
            "fragment": doc.fragment_3d,
        }
        for doc in hits
    ]
    return json.dumps(
        {
            "user_prompt": query,
            "product_type": product_type,
            "retrieved_terms": retrieved,
            "compatibility_rules": rules.get("selection_rules") or [],
            "negative_prompt_defaults": rules.get("negative_prompt_defaults") or [],
            "instruction": "只输出 JSON。text_to_3d_prompt 必须是中文，适合混元 HY-3D 文生 3D，不超过 1024 字。",
        },
        ensure_ascii=False,
    )


def _message_content(body: Mapping[str, Any]) -> str:
    choices = body.get("choices")
    if isinstance(choices, list) and choices:
        message = choices[0].get("message") if isinstance(choices[0], dict) else None
        if isinstance(message, dict):
            return str(message.get("content") or "")
    return str(body.get("content") or "")


def _parse_json_object(raw: str) -> dict[str, Any] | None:
    text = raw.strip()
    if not text:
        return None
    fenced = re.search(r"```(?:json)?\s*(\{.*\})\s*```", text, re.DOTALL)
    if fenced:
        text = fenced.group(1)
    try:
        value = json.loads(text)
    except json.JSONDecodeError:
        start, end = text.find("{"), text.rfind("}")
        if start < 0 or end <= start:
            return None
        try:
            value = json.loads(text[start : end + 1])
        except json.JSONDecodeError:
            return None
    return value if isinstance(value, dict) else None


def _load_system_prompt() -> str:
    path = default_system_prompt_path()
    if path.is_file():
        return path.read_text(encoding="utf-8")
    return "你是二次元谷子与 3D 商品生成的领域 Prompt 优化器。只输出 JSON。"


def _truncate(text: str, limit: int = PROMPT_CHAR_LIMIT) -> str:
    text = re.sub(r"\s+", " ", text).strip()
    return text if len(text) <= limit else text[:limit]
