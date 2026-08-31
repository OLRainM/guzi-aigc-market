from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
from enum import Enum
from typing import Any, Mapping, Sequence


class ProviderStatus(str, Enum):
    PENDING = "PENDING"
    RUNNING = "RUNNING"
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    CANCELED = "CANCELED"


class ProviderErrorCode(str, Enum):
    INVALID_REQUEST = "INVALID_REQUEST"
    AUTHENTICATION_FAILED = "AUTHENTICATION_FAILED"
    RATE_LIMITED = "RATE_LIMITED"
    INSUFFICIENT_BALANCE = "INSUFFICIENT_BALANCE"
    PROVIDER_UNAVAILABLE = "PROVIDER_UNAVAILABLE"
    PROVIDER_TIMEOUT = "PROVIDER_TIMEOUT"
    OUTPUT_INVALID = "OUTPUT_INVALID"
    DOWNLOAD_FAILED = "DOWNLOAD_FAILED"
    UNKNOWN_PROVIDER_ERROR = "UNKNOWN_PROVIDER_ERROR"


class ProviderError(RuntimeError):
    def __init__(self, code: ProviderErrorCode, message: str, *, retryable: bool, diagnostic: str | None = None) -> None:
        super().__init__(message)
        self.code = code
        self.retryable = retryable
        self.diagnostic = diagnostic


@dataclass(frozen=True)
class ProviderSubmission:
    provider_job_id: str
    status: ProviderStatus


@dataclass(frozen=True)
class ProviderProgress:
    status: ProviderStatus
    progress: int
    error: ProviderError | None = None


@dataclass(frozen=True)
class ProviderOutput:
    output_type: str
    format: str
    uri: str
    mime_type: str
    metadata: Mapping[str, Any]


class GenerationProvider(ABC):
    @abstractmethod
    async def submit(self, prompt: str, parameters: Mapping[str, Any]) -> ProviderSubmission:
        raise NotImplementedError

    @abstractmethod
    async def get_status(self, provider_job_id: str) -> ProviderProgress:
        raise NotImplementedError

    @abstractmethod
    async def cancel(self, provider_job_id: str) -> bool:
        raise NotImplementedError

    @abstractmethod
    async def fetch_outputs(self, provider_job_id: str) -> Sequence[ProviderOutput]:
        raise NotImplementedError
