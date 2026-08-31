import unittest

from contracts import ContractError, GenerationJobMessage


VALID_MESSAGE = {
    "schema_version": "1",
    "event_type": "generation.job.created",
    "job_id": "f0ab13fc-8ab1-4dd5-9416-c9861c659fca",
    "user_id": "05414806-56e5-432d-9ea4-f641b43cab58",
    "attempt": "1",
    "request_id": "req-test",
    "created_at": "2026-08-23T12:00:00Z",
}


class GenerationJobMessageTests(unittest.TestCase):
    def test_parses_valid_message(self) -> None:
        message = GenerationJobMessage.from_stream(VALID_MESSAGE)
        self.assertEqual(message.schema_version, "1")
        self.assertEqual(message.attempt, 1)

    def test_rejects_missing_job_id(self) -> None:
        fields = dict(VALID_MESSAGE)
        fields.pop("job_id")
        with self.assertRaises(ContractError):
            GenerationJobMessage.from_stream(fields)

    def test_rejects_unknown_schema_version(self) -> None:
        fields = dict(VALID_MESSAGE, schema_version="2")
        with self.assertRaises(ContractError):
            GenerationJobMessage.from_stream(fields)

    def test_rejects_naive_created_at(self) -> None:
        fields = dict(VALID_MESSAGE, created_at="2026-08-23T12:00:00")
        with self.assertRaises(ContractError):
            GenerationJobMessage.from_stream(fields)


if __name__ == "__main__":
    unittest.main()
