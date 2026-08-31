from __future__ import annotations

from typing import Any, Mapping, Sequence
from uuid import uuid4

from providers.base import (
    GenerationProvider,
    ProviderError,
    ProviderErrorCode,
    ProviderOutput,
    ProviderProgress,
    ProviderStatus,
    ProviderSubmission,
)


class MockProvider(GenerationProvider):
    def __init__(self) -> None:
        self._jobs: dict[str, ProviderStatus] = {}

    async def submit(self, prompt: str, parameters: Mapping[str, Any]) -> ProviderSubmission:
        if not prompt.strip():
            raise ProviderError(
                ProviderErrorCode.INVALID_REQUEST,
                "prompt must not be empty",
                retryable=False,
            )
        provider_job_id = f"mock-{uuid4()}"
        self._jobs[provider_job_id] = ProviderStatus.SUCCEEDED
        return ProviderSubmission(provider_job_id, ProviderStatus.SUCCEEDED)

    async def get_status(self, provider_job_id: str) -> ProviderProgress:
        status = self._require_job(provider_job_id)
        return ProviderProgress(status=status, progress=100 if status == ProviderStatus.SUCCEEDED else 0)

    async def cancel(self, provider_job_id: str) -> bool:
        self._require_job(provider_job_id)
        self._jobs[provider_job_id] = ProviderStatus.CANCELED
        return True

    async def fetch_outputs(self, provider_job_id: str) -> Sequence[ProviderOutput]:
        status = self._require_job(provider_job_id)
        if status != ProviderStatus.SUCCEEDED:
            raise ProviderError(
                ProviderErrorCode.OUTPUT_INVALID,
                "provider output is not ready",
                retryable=status in {ProviderStatus.PENDING, ProviderStatus.RUNNING},
            )
        return [
            ProviderOutput(
                output_type="MODEL",
                format="glb",
                uri="mock://assets/sample.glb",
                mime_type="model/gltf-binary",
                metadata={"provider": "mock"},
            )
        ]

    def _require_job(self, provider_job_id: str) -> ProviderStatus:
        try:
            return self._jobs[provider_job_id]
        except KeyError as exc:
            raise ProviderError(
                ProviderErrorCode.INVALID_REQUEST,
                "unknown provider job",
                retryable=False,
            ) from exc
