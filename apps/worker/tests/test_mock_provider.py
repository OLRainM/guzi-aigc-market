import unittest

from providers.base import ProviderError, ProviderErrorCode, ProviderStatus
from providers.mock import MockProvider


class MockProviderTests(unittest.IsolatedAsyncioTestCase):
    async def test_contract_happy_path(self) -> None:
        provider = MockProvider()
        submission = await provider.submit("a collectible figure", {"quality": "preview"})
        self.assertEqual(submission.status, ProviderStatus.SUCCEEDED)

        progress = await provider.get_status(submission.provider_job_id)
        self.assertEqual(progress.status, ProviderStatus.SUCCEEDED)
        self.assertEqual(progress.progress, 100)

        outputs = await provider.fetch_outputs(submission.provider_job_id)
        self.assertEqual(len(outputs), 1)
        self.assertEqual(outputs[0].format, "glb")

    async def test_rejects_empty_prompt(self) -> None:
        provider = MockProvider()
        with self.assertRaises(ProviderError) as raised:
            await provider.submit("  ", {})
        self.assertEqual(raised.exception.code, ProviderErrorCode.INVALID_REQUEST)
        self.assertFalse(raised.exception.retryable)

    async def test_cancel_changes_status(self) -> None:
        provider = MockProvider()
        submission = await provider.submit("a model", {})
        self.assertTrue(await provider.cancel(submission.provider_job_id))
        progress = await provider.get_status(submission.provider_job_id)
        self.assertEqual(progress.status, ProviderStatus.CANCELED)


if __name__ == "__main__":
    unittest.main()
