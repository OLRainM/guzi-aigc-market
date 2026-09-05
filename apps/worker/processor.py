from __future__ import annotations

import asyncio
import logging
import os
import time
from typing import Any, Mapping, Protocol

from contracts import GenerationJobMessage
from promptopt import PromptOptimizer
from providers.base import GenerationProvider, ProviderError, ProviderErrorCode, ProviderStatus
from providers.mock import MockProvider

logger = logging.getLogger("ai-worker")


class JobCanceled(RuntimeError):
    pass


class APIClient(Protocol):
    async def claim(self, job_id: str, attempt: int) -> dict[str, Any]: ...
    async def progress(
        self,
        job_id: str,
        attempt: int,
        stage: str,
        progress: int,
        extra: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]: ...
    async def fail(self, job_id: str, attempt: int, error_code: str, error_message: str, retryable: bool) -> dict[str, Any]: ...
    async def complete(self, job_id: str, attempt: int, provider_job_id: str, filename: str, content: bytes, mime_type: str) -> dict[str, Any]: ...


class HTTPAPIClient:
    def __init__(self, base_url: str, token: str, timeout: float = 30.0) -> None:
        import httpx

        self._client = httpx.AsyncClient(base_url=base_url.rstrip("/"), timeout=timeout, headers={"X-Worker-Token": token})

    async def aclose(self) -> None:
        await self._client.aclose()

    async def claim(self, job_id: str, attempt: int) -> dict[str, Any]:
        return await self._post(f"/api/v1/internal/generation-jobs/{job_id}/claim", json={"attempt": attempt})

    async def progress(
        self,
        job_id: str,
        attempt: int,
        stage: str,
        progress: int,
        extra: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]:
        payload: dict[str, Any] = {"attempt": attempt, "stage": stage, "progress": progress}
        if extra:
            payload.update(dict(extra))
        return await self._post(f"/api/v1/internal/generation-jobs/{job_id}/progress", json=payload)

    async def fail(self, job_id: str, attempt: int, error_code: str, error_message: str, retryable: bool) -> dict[str, Any]:
        return await self._post(
            f"/api/v1/internal/generation-jobs/{job_id}/fail",
            json={"attempt": attempt, "error_code": error_code, "error_message": error_message, "retryable": retryable},
        )

    async def complete(self, job_id: str, attempt: int, provider_job_id: str, filename: str, content: bytes, mime_type: str) -> dict[str, Any]:
        response = await self._client.post(
            f"/api/v1/internal/generation-jobs/{job_id}/complete",
            data={"attempt": str(attempt), "provider_job_id": provider_job_id},
            files={"file": (filename, content, mime_type)},
        )
        return self._parse(response)

    async def _post(self, path: str, json: Mapping[str, Any]) -> dict[str, Any]:
        response = await self._client.post(path, json=dict(json))
        return self._parse(response)

    def _parse(self, response: Any) -> dict[str, Any]:
        try:
            payload = response.json()
        except ValueError as exc:
            raise RuntimeError(f"worker api returned invalid json: {response.status_code}") from exc
        if response.status_code >= 400:
            error = payload.get("error") if isinstance(payload, dict) else None
            code = error.get("code") if isinstance(error, dict) else "INTERNAL_ERROR"
            message = error.get("message") if isinstance(error, dict) else response.text
            raise WorkerAPIError(response.status_code, str(code), str(message), payload)
        return payload


class WorkerAPIError(RuntimeError):
    def __init__(self, status_code: int, code: str, message: str, payload: Any) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.payload = payload


def job_status(payload: Mapping[str, Any]) -> str:
    job = payload.get("job") if isinstance(payload, dict) else None
    if isinstance(job, dict):
        return str(job.get("status") or "")
    return ""


class JobProcessor:
    def __init__(
        self,
        api: APIClient,
        provider: GenerationProvider | None = None,
        optimizer: PromptOptimizer | None = None,
    ) -> None:
        self.api = api
        self.provider = provider or MockProvider()
        self.optimizer = optimizer or PromptOptimizer()
        self.poll_interval = float(os.getenv("PROVIDER_POLL_INTERVAL", "8"))
        self.poll_timeout = float(os.getenv("PROVIDER_POLL_TIMEOUT", "900"))

    async def process(self, message: GenerationJobMessage) -> str:
        job_id = str(message.job_id)
        attempt = message.attempt
        try:
            claimed = await self.api.claim(job_id, attempt)
            status = job_status(claimed)
            if status in {"SUCCEEDED", "CANCELED"}:
                return status
            if status != "RUNNING":
                raise RuntimeError(f"unexpected claim status: {status}")
            job = claimed.get("job") if isinstance(claimed.get("job"), dict) else {}
            raw_prompt = str(job.get("raw_prompt") or "").strip()
            product_type = str(job.get("product_type") or "")
            parameters = {"attempt": attempt, "product_type": product_type}
            if not raw_prompt:
                raise ProviderError(ProviderErrorCode.INVALID_REQUEST, "prompt is required", retryable=False)
            await self._progress(job_id, attempt, "OPTIMIZING_PROMPT", 25)
            optimized = await self.optimizer.optimize(raw_prompt, product_type)
            prompt = optimized.text.strip()
            await self._progress(
                job_id,
                attempt,
                "OPTIMIZING_PROMPT",
                25,
                {
                    "optimized_prompt": prompt,
                    "rag_context": optimized.rag_context,
                    "rag_version": optimized.rag_version,
                    "template_version": optimized.template_version,
                    "structured_prompt": optimized.structured,
                },
            )
            await self._progress(job_id, attempt, "SUBMITTING_PROVIDER", 40)
            submission = await self.provider.submit(prompt, parameters)
            progress = await self._wait_for_success(job_id, attempt, submission.provider_job_id)
            if progress.status != ProviderStatus.SUCCEEDED:
                if progress.error:
                    raise progress.error
                raise ProviderError(ProviderErrorCode.UNKNOWN_PROVIDER_ERROR, "provider did not succeed", retryable=True)
            await self._progress(job_id, attempt, "FETCHING_OUTPUT", 70)
            outputs = await self.provider.fetch_outputs(submission.provider_job_id)
            if not outputs:
                raise RuntimeError("provider returned no outputs")
            output = outputs[0]
            if not output.content:
                raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "provider returned empty GLB", retryable=True)
            content = output.content
            await self._progress(job_id, attempt, "STORING_OUTPUT", 90)
            completed = await self.api.complete(
                job_id,
                attempt,
                submission.provider_job_id,
                f"{job_id}.glb",
                content,
                output.mime_type or "model/gltf-binary",
            )
            return job_status(completed) or "SUCCEEDED"
        except JobCanceled:
            return "CANCELED"
        except WorkerAPIError as exc:
            if exc.code == "CONFLICT" and "取消" in str(exc):
                return "CANCELED"
            if exc.status_code in {404, 409}:
                logger.warning("skipping job %s: %s", job_id, exc)
                return "SKIPPED"
            await self._fail(job_id, attempt, exc.code, str(exc), retryable=exc.status_code >= 500)
            return "FAILED"
        except ProviderError as exc:
            await self._fail(job_id, attempt, exc.code.value, str(exc), retryable=exc.retryable)
            return "FAILED"
        except Exception as exc:
            await self._fail(job_id, attempt, "WORKER_ERROR", str(exc), retryable=True)
            return "FAILED"

    async def _wait_for_success(self, job_id: str, attempt: int, provider_job_id: str):
        deadline = time.monotonic() + self.poll_timeout
        while True:
            progress = await self.provider.get_status(provider_job_id)
            mapped = 40 + int(max(0, min(100, progress.progress)) * 0.3)
            await self._progress(job_id, attempt, "GENERATING", mapped)
            if progress.status == ProviderStatus.SUCCEEDED:
                return progress
            if progress.status == ProviderStatus.CANCELED:
                raise JobCanceled()
            if progress.status == ProviderStatus.FAILED:
                if progress.error:
                    raise progress.error
                raise ProviderError(ProviderErrorCode.UNKNOWN_PROVIDER_ERROR, "provider failed", retryable=True)
            if time.monotonic() >= deadline:
                raise ProviderError(ProviderErrorCode.PROVIDER_TIMEOUT, "provider polling timed out", retryable=True)
            await asyncio.sleep(self.poll_interval)

    async def _progress(
        self,
        job_id: str,
        attempt: int,
        stage: str,
        progress: int,
        extra: Mapping[str, Any] | None = None,
    ) -> None:
        payload = await self.api.progress(job_id, attempt, stage, progress, extra)
        if job_status(payload) == "CANCELED":
            raise JobCanceled()

    async def _fail(self, job_id: str, attempt: int, error_code: str, error_message: str, retryable: bool) -> None:
        try:
            await self.api.fail(job_id, attempt, error_code, error_message, retryable)
        except Exception:
            logger.exception("failed to report job failure", extra={"job_id": job_id})
            raise
