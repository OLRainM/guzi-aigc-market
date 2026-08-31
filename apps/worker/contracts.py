from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Mapping
from uuid import UUID

MESSAGE_SCHEMA_VERSION = "1"
JOB_CREATED_EVENT = "generation.job.created"


class ContractError(ValueError):
    pass


@dataclass(frozen=True)
class GenerationJobMessage:
    schema_version: str
    event_type: str
    job_id: UUID
    user_id: UUID
    attempt: int
    request_id: str
    created_at: datetime

    @classmethod
    def from_stream(cls, fields: Mapping[str, str]) -> "GenerationJobMessage":
        required = {
            "schema_version",
            "event_type",
            "job_id",
            "user_id",
            "attempt",
            "request_id",
            "created_at",
        }
        missing = required.difference(fields)
        if missing:
            raise ContractError(f"missing required stream fields: {', '.join(sorted(missing))}")
        if fields["schema_version"] != MESSAGE_SCHEMA_VERSION:
            raise ContractError(f"unsupported schema_version: {fields['schema_version']}")
        if fields["event_type"] != JOB_CREATED_EVENT:
            raise ContractError(f"unsupported event_type: {fields['event_type']}")
        try:
            attempt = int(fields["attempt"])
            if attempt < 1:
                raise ValueError("attempt must be positive")
            created_at = datetime.fromisoformat(fields["created_at"].replace("Z", "+00:00"))
            if created_at.tzinfo is None:
                raise ValueError("created_at must include a timezone")
            return cls(
                schema_version=fields["schema_version"],
                event_type=fields["event_type"],
                job_id=UUID(fields["job_id"]),
                user_id=UUID(fields["user_id"]),
                attempt=attempt,
                request_id=fields["request_id"],
                created_at=created_at,
            )
        except (TypeError, ValueError) as exc:
            raise ContractError(f"invalid generation job message: {exc}") from exc
