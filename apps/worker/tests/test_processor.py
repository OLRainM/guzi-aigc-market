import unittest
from datetime import datetime, timezone
from uuid import uuid4

from contracts import GenerationJobMessage
from processor import JobProcessor
from providers.mock import MockProvider, minimal_glb


class FakeAPI:
    def __init__(self, prompt: str = "a collectible figure", status: str = "QUEUED") -> None:
        self.prompt = prompt
        self.status = status
        self.calls: list[tuple[str, dict]] = []
        self.completed: bytes | None = None

    async def claim(self, job_id: str, attempt: int) -> dict:
        self.calls.append(("claim", {"job_id": job_id, "attempt": attempt}))
        if self.status == "QUEUED":
            self.status = "RUNNING"
        return {"job": {"id": job_id, "status": self.status, "raw_prompt": self.prompt, "attempt": attempt}}

    async def progress(self, job_id: str, attempt: int, stage: str, progress: int) -> dict:
        self.calls.append(("progress", {"job_id": job_id, "stage": stage, "progress": progress}))
        return {"job": {"id": job_id, "status": self.status, "stage": stage, "progress": progress}}

    async def fail(self, job_id: str, attempt: int, error_code: str, error_message: str, retryable: bool) -> dict:
        self.calls.append(("fail", {"job_id": job_id, "error_code": error_code, "retryable": retryable}))
        self.status = "FAILED"
        return {"job": {"id": job_id, "status": self.status, "error": {"code": error_code, "message": error_message}}}

    async def complete(self, job_id: str, attempt: int, provider_job_id: str, filename: str, content: bytes, mime_type: str) -> dict:
        self.calls.append(("complete", {"job_id": job_id, "filename": filename, "mime_type": mime_type}))
        self.completed = content
        self.status = "SUCCEEDED"
        return {"job": {"id": job_id, "status": self.status, "progress": 100}}


def sample_message() -> GenerationJobMessage:
    return GenerationJobMessage(
        schema_version="1",
        event_type="generation.job.created",
        job_id=uuid4(),
        user_id=uuid4(),
        attempt=1,
        request_id="req-test",
        created_at=datetime.now(timezone.utc),
    )


class JobProcessorTests(unittest.IsolatedAsyncioTestCase):
    async def test_processes_mock_job(self) -> None:
        api = FakeAPI()
        processor = JobProcessor(api, MockProvider())
        result = await processor.process(sample_message())
        self.assertEqual(result, "SUCCEEDED")
        self.assertEqual([name for name, _ in api.calls][:2], ["claim", "progress"])
        self.assertEqual(api.calls[-1][0], "complete")
        self.assertTrue(api.completed.startswith(b"glTF"))
        self.assertGreaterEqual(len(api.completed), len(minimal_glb()))

    async def test_reports_provider_failure(self) -> None:
        api = FakeAPI(prompt="   ")
        processor = JobProcessor(api, MockProvider())
        result = await processor.process(sample_message())
        self.assertEqual(result, "FAILED")
        self.assertEqual(api.calls[-1][0], "fail")
        self.assertEqual(api.calls[-1][1]["error_code"], "INVALID_REQUEST")


if __name__ == "__main__":
    unittest.main()
