from __future__ import annotations

import os

from providers.base import GenerationProvider
from providers.mock import MockProvider


def create_provider(name: str | None = None) -> GenerationProvider:
    provider = (name or os.getenv("GENERATION_PROVIDER", "mock")).strip().lower()
    if provider in {"", "mock"}:
        return MockProvider()
    if provider in {"http", "external", "manual"}:
        from providers.http import HTTPProvider

        return HTTPProvider()
    if provider in {"hy3d", "hy-3d", "tokenhub"}:
        from providers.hy3d import Hy3DProvider

        return Hy3DProvider()
    raise ValueError(f"unsupported GENERATION_PROVIDER: {provider}")
