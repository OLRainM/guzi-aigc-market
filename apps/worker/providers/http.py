from __future__ import annotations

import base64
import os
from typing import Any, Mapping, Sequence
from urllib.parse import urljoin

import httpx

from providers.base import (
    GenerationProvider,
    ProviderError,
    ProviderErrorCode,
    ProviderOutput,
    ProviderProgress,
    ProviderStatus,
    ProviderSubmission,
)

STATUS_MAP = {
    "PENDING": ProviderStatus.PENDING,
    "QUEUED": ProviderStatus.PENDING,
    "RUNNING": ProviderStatus.RUNNING,
    "SUCCEEDED": ProviderStatus.SUCCEEDED,
    "SUCCESS": ProviderStatus.SUCCEEDED,
    "FAILED": ProviderStatus.FAILED,
    "CANCELED": ProviderStatus.CANCELED,
    "CANCELLED": ProviderStatus.CANCELED,
}

ERROR_MAP = {
    "INVALID_REQUEST": ProviderErrorCode.INVALID_REQUEST,
    "AUTHENTICATION_FAILED": ProviderErrorCode.AUTHENTICATION_FAILED,
    "RATE_LIMITED": ProviderErrorCode.RATE_LIMITED,
    "INSUFFICIENT_BALANCE": ProviderErrorCode.INSUFFICIENT_BALANCE,
    "PROVIDER_UNAVAILABLE": ProviderErrorCode.PROVIDER_UNAVAILABLE,
    "PROVIDER_TIMEOUT": ProviderErrorCode.PROVIDER_TIMEOUT,
    "OUTPUT_INVALID": ProviderErrorCode.OUTPUT_INVALID,
    "DOWNLOAD_FAILED": ProviderErrorCode.DOWNLOAD_FAILED,
}


class HTTPProvider(GenerationProvider):
    """Calls an external text-to-3D HTTP API. Fill PROVIDER_BASE_URL to enable."""

    def __init__(
        self,
        base_url: str | None = None,
        api_key: str | None = None,
        timeout: float | None = None,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self.base_url = (base_url or os.getenv("PROVIDER_BASE_URL", "")).rstrip("/") + "/"
        if self.base_url == "/":
            raise ProviderError(
                ProviderErrorCode.INVALID_REQUEST,
                "PROVIDER_BASE_URL is required when GENERATION_PROVIDER=http",
                retryable=False,
            )
        headers = {"Accept": "application/json"}
        token = api_key if api_key is not None else os.getenv("PROVIDER_API_KEY", "")
        if token:
            headers["Authorization"] = f"Bearer {token}"
        self._owns_client = client is None
        self._client = client or httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout or float(os.getenv("PROVIDER_TIMEOUT", "60")),
            headers=headers,
        )

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def submit(self, prompt: str, parameters: Mapping[str, Any]) -> ProviderSubmission:
        payload = await self._request("POST", "jobs", json={"prompt": prompt, "parameters": dict(parameters)})
        job_id = str(payload.get("provider_job_id") or payload.get("id") or "")
        if not job_id:
            raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "provider did not return job id", retryable=False)
        return ProviderSubmission(job_id, self._status(payload.get("status"), ProviderStatus.PENDING))

    async def get_status(self, provider_job_id: str) -> ProviderProgress:
        payload = await self._request("GET", f"jobs/{provider_job_id}")
        try:
            progress_value = max(0, min(100, int(payload.get("progress", 0))))
        except (TypeError, ValueError):
            progress_value = 0
        return ProviderProgress(
            status=self._status(payload.get("status"), ProviderStatus.RUNNING),
            progress=progress_value,
            error=self._error(payload.get("error")),
        )

    async def cancel(self, provider_job_id: str) -> bool:
        payload = await self._request("POST", f"jobs/{provider_job_id}/cancel")
        return bool(payload.get("canceled", True))

    async def fetch_outputs(self, provider_job_id: str) -> Sequence[ProviderOutput]:
        payload = await self._request("GET", f"jobs/{provider_job_id}/outputs")
        items = payload.get("outputs") if isinstance(payload, dict) else payload
        if not isinstance(items, list) or not items:
            raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "provider returned no outputs", retryable=True)
        outputs: list[ProviderOutput] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            uri = str(item.get("uri") or item.get("url") or "")
            content = await self._download(uri) if uri else None
            if content is None and item.get("content_base64"):
                content = base64.b64decode(str(item["content_base64"]))
            outputs.append(
                ProviderOutput(
                    output_type=str(item.get("output_type") or "MODEL"),
                    format=str(item.get("format") or "glb"),
                    uri=uri,
                    mime_type=str(item.get("mime_type") or "model/gltf-binary"),
                    metadata=item.get("metadata") if isinstance(item.get("metadata"), dict) else {},
                    content=content,
                )
            )
        if not outputs:
            raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "provider returned no usable outputs", retryable=True)
        return outputs

    async def _download(self, uri: str) -> bytes:
        url = uri if uri.startswith("http") else urljoin(self.base_url, uri)
        try:
            response = await self._client.get(url)
        except httpx.TimeoutException as err:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, "timed out downloading output", retryable=True) from err
        except httpx.HTTPError as err:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, "failed to download output", retryable=True) from err
        if response.status_code >= 400:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, f"download failed: {response.status_code}", retryable=True)
        return response.content

    async def _request(self, method: str, path: str, json: Mapping[str, Any] | None = None) -> dict[str, Any]:
        try:
            response = await self._client.request(method, path, json=dict(json) if json is not None else None)
        except httpx.TimeoutException as err:
            raise ProviderError(ProviderErrorCode.PROVIDER_TIMEOUT, "provider request timed out", retryable=True) from err
        except httpx.HTTPError as err:
            raise ProviderError(ProviderErrorCode.PROVIDER_UNAVAILABLE, "provider is unavailable", retryable=True) from err
        try:
            payload = response.json()
        except ValueError:
            payload = {"message": response.text}
        if not isinstance(payload, dict):
            payload = {"data": payload}
        if response.status_code >= 400:
            error = self._error(payload.get("error")) or ProviderError(
                self._error_code(payload.get("code") or response.status_code),
                str(payload.get("message") or payload.get("error") or f"provider http {response.status_code}"),
                retryable=response.status_code >= 500 or response.status_code == 429,
            )
            raise error
        return payload

    def _status(self, value: Any, fallback: ProviderStatus) -> ProviderStatus:
        if value is None:
            return fallback
        return STATUS_MAP.get(str(value).upper(), fallback)

    def _error(self, value: Any) -> ProviderError | None:
        if not isinstance(value, dict):
            return None
        return ProviderError(
            self._error_code(value.get("code")),
            str(value.get("message") or "provider error"),
            retryable=bool(value.get("retryable", False)),
        )

    def _error_code(self, value: Any) -> ProviderErrorCode:
        if isinstance(value, int):
            if value in {401, 403}:
                return ProviderErrorCode.AUTHENTICATION_FAILED
            if value == 429:
                return ProviderErrorCode.RATE_LIMITED
            if value >= 500:
                return ProviderErrorCode.PROVIDER_UNAVAILABLE
            return ProviderErrorCode.INVALID_REQUEST
        return ERROR_MAP.get(str(value or "").upper(), ProviderErrorCode.UNKNOWN_PROVIDER_ERROR)
