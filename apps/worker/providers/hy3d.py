from __future__ import annotations

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
    "QUEUED": ProviderStatus.PENDING,
    "IN_PROGRESS": ProviderStatus.RUNNING,
    "COMPLETED": ProviderStatus.SUCCEEDED,
    "FAILED": ProviderStatus.FAILED,
}

PROGRESS_MAP = {
    ProviderStatus.PENDING: 40,
    ProviderStatus.RUNNING: 55,
    ProviderStatus.SUCCEEDED: 100,
    ProviderStatus.FAILED: 100,
    ProviderStatus.CANCELED: 0,
}

PROMPT_CHAR_LIMIT = 1024


def _truncate(text: str, limit: int = PROMPT_CHAR_LIMIT) -> str:
    text = text.strip()
    return text if len(text) <= limit else text[:limit]


def _env_bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None or raw == "":
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


class Hy3DProvider(GenerationProvider):
    """TokenHub HY-3D text-to-3D adapter. First phase: prompt -> GLB only."""

    def __init__(
        self,
        base_url: str | None = None,
        api_key: str | None = None,
        model: str | None = None,
        timeout: float | None = None,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self.base_url = (base_url or os.getenv("HY3D_BASE_URL") or "https://tokenhub.tencentmaas.com").rstrip("/") + "/"
        token = api_key if api_key is not None else os.getenv("TOKENHUB_API_KEY") or os.getenv("PROVIDER_API_KEY", "")
        if not token:
            raise ProviderError(
                ProviderErrorCode.AUTHENTICATION_FAILED,
                "TOKENHUB_API_KEY is required when GENERATION_PROVIDER=hy3d",
                retryable=False,
            )
        self.model = model or os.getenv("HY3D_MODEL", "hy-3d-3.1")
        self.enable_pbr = _env_bool("HY3D_ENABLE_PBR", True)
        self.generate_type = os.getenv("HY3D_GENERATE_TYPE", "normal")
        try:
            self.face_count = int(os.getenv("HY3D_FACE_COUNT", "100000"))
        except ValueError:
            self.face_count = 100000
        self._outputs: dict[str, list[ProviderOutput]] = {}
        self._owns_client = client is None
        self._client = client or httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout or float(os.getenv("PROVIDER_TIMEOUT", "60")),
            headers={"Authorization": f"Bearer {token}", "Accept": "application/json"},
        )

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def submit(self, prompt: str, parameters: Mapping[str, Any]) -> ProviderSubmission:
        text = _truncate(str(prompt or ""))
        if not text:
            raise ProviderError(ProviderErrorCode.INVALID_REQUEST, "prompt is required", retryable=False)
        payload = {
            "model": self.model,
            "prompt": text,
            "enable_pbr": bool(parameters.get("enable_pbr", self.enable_pbr)),
            "face_count": int(parameters.get("face_count", self.face_count)),
            "generate_type": str(parameters.get("generate_type") or self.generate_type),
        }
        body = await self._request("POST", "v1/api/3d/submit", json=payload)
        job_id = str(body.get("id") or "")
        if not job_id:
            raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "hy3d did not return task id", retryable=False)
        return ProviderSubmission(job_id, self._status(body.get("status"), ProviderStatus.PENDING))

    async def get_status(self, provider_job_id: str) -> ProviderProgress:
        body = await self._query(provider_job_id)
        status = self._status(body.get("status"), ProviderStatus.RUNNING)
        if status == ProviderStatus.SUCCEEDED:
            self._outputs[provider_job_id] = self._parse_outputs(body)
        if status == ProviderStatus.FAILED:
            return ProviderProgress(
                status=status,
                progress=100,
                error=ProviderError(
                    ProviderErrorCode.UNKNOWN_PROVIDER_ERROR,
                    str(body.get("message") or body.get("error") or "hy3d task failed"),
                    retryable=False,
                    diagnostic=str(body.get("request_id") or ""),
                ),
            )
        return ProviderProgress(status=status, progress=PROGRESS_MAP.get(status, 55))

    async def cancel(self, provider_job_id: str) -> bool:
        # TokenHub HY-3D has no cancel API; local polling stops in the worker.
        return True

    async def fetch_outputs(self, provider_job_id: str) -> Sequence[ProviderOutput]:
        outputs = self._outputs.get(provider_job_id)
        if not outputs:
            body = await self._query(provider_job_id)
            if self._status(body.get("status"), ProviderStatus.RUNNING) != ProviderStatus.SUCCEEDED:
                raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "hy3d task is not completed", retryable=True)
            outputs = self._parse_outputs(body)
            self._outputs[provider_job_id] = outputs
        glb = [item for item in outputs if item.format == "glb"]
        selected = glb or list(outputs)
        if not selected:
            raise ProviderError(ProviderErrorCode.OUTPUT_INVALID, "hy3d returned no GLB output", retryable=True)
        filled: list[ProviderOutput] = []
        for item in selected:
            content = item.content if item.content is not None else await self._download(item.uri)
            filled.append(
                ProviderOutput(
                    output_type=item.output_type,
                    format=item.format,
                    uri=item.uri,
                    mime_type=item.mime_type,
                    metadata=item.metadata,
                    content=content,
                )
            )
        return filled

    async def _query(self, provider_job_id: str) -> dict[str, Any]:
        return await self._request("POST", "v1/api/3d/query", json={"model": self.model, "id": provider_job_id})

    def _parse_outputs(self, body: Mapping[str, Any]) -> list[ProviderOutput]:
        items = body.get("data")
        if not isinstance(items, list):
            return []
        outputs: list[ProviderOutput] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            fmt = str(item.get("type") or "").lower()
            url = str(item.get("url") or "")
            if not fmt or not url:
                continue
            outputs.append(
                ProviderOutput(
                    output_type="MODEL",
                    format=fmt,
                    uri=url,
                    mime_type="model/gltf-binary" if fmt == "glb" else "application/octet-stream",
                    metadata={"preview_image_url": item.get("preview_image_url"), "request_id": body.get("request_id")},
                )
            )
        return outputs

    async def _download(self, uri: str) -> bytes:
        url = uri if uri.startswith("http") else urljoin(self.base_url, uri)
        try:
            response = await self._client.get(url)
        except httpx.TimeoutException as err:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, "timed out downloading hy3d output", retryable=True) from err
        except httpx.HTTPError as err:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, "failed to download hy3d output", retryable=True) from err
        if response.status_code >= 400:
            raise ProviderError(ProviderErrorCode.DOWNLOAD_FAILED, f"download failed: {response.status_code}", retryable=True)
        return response.content

    async def _request(self, method: str, path: str, json: Mapping[str, Any] | None = None) -> dict[str, Any]:
        try:
            response = await self._client.request(method, path, json=dict(json) if json is not None else None)
        except httpx.TimeoutException as err:
            raise ProviderError(ProviderErrorCode.PROVIDER_TIMEOUT, "hy3d request timed out", retryable=True) from err
        except httpx.HTTPError as err:
            raise ProviderError(ProviderErrorCode.PROVIDER_UNAVAILABLE, "hy3d is unavailable", retryable=True) from err
        try:
            payload = response.json()
        except ValueError:
            payload = {"message": response.text}
        if not isinstance(payload, dict):
            payload = {"data": payload}
        if response.status_code >= 400:
            raise self._http_error(response.status_code, payload)
        return payload

    def _http_error(self, status_code: int, payload: Mapping[str, Any]) -> ProviderError:
        message = str(payload.get("message") or payload.get("error") or f"hy3d http {status_code}")
        diagnostic = str(payload.get("request_id") or payload.get("code") or "")
        if status_code in {401}:
            code, retryable = ProviderErrorCode.AUTHENTICATION_FAILED, False
        elif status_code in {402, 403}:
            code, retryable = ProviderErrorCode.INSUFFICIENT_BALANCE, False
        elif status_code == 429:
            code, retryable = ProviderErrorCode.RATE_LIMITED, True
        elif status_code in {400, 422, 451}:
            code, retryable = ProviderErrorCode.INVALID_REQUEST, False
        elif status_code >= 500:
            code, retryable = ProviderErrorCode.PROVIDER_UNAVAILABLE, True
        else:
            code, retryable = ProviderErrorCode.UNKNOWN_PROVIDER_ERROR, status_code >= 500
        return ProviderError(code, message, retryable=retryable, diagnostic=diagnostic)

    def _status(self, value: Any, fallback: ProviderStatus) -> ProviderStatus:
        if value is None:
            return fallback
        return STATUS_MAP.get(str(value).upper(), fallback)
