import json
import unittest

try:
    import httpx
except ImportError:  # pragma: no cover
    httpx = None

from providers.factory import create_provider
from providers.mock import MockProvider, minimal_glb


class ProviderFactoryTests(unittest.TestCase):
    def test_factory_defaults_to_mock(self) -> None:
        self.assertIsInstance(create_provider("mock"), MockProvider)


@unittest.skipUnless(httpx, "httpx is not installed")
class HTTPProviderTests(unittest.IsolatedAsyncioTestCase):
    async def test_http_provider_roundtrip(self) -> None:
        from providers.http import HTTPProvider

        def handler(request: httpx.Request) -> httpx.Response:
            path = request.url.path
            if request.method == "POST" and path == "/jobs":
                body = json.loads(request.content)
                assert body["prompt"] == "a collectible figure"
                return httpx.Response(202, json={"provider_job_id": "ext-1", "status": "PENDING"})
            if request.method == "GET" and path == "/jobs/ext-1":
                return httpx.Response(200, json={"status": "SUCCEEDED", "progress": 100})
            if request.method == "POST" and path == "/jobs/ext-1/cancel":
                return httpx.Response(200, json={"canceled": True})
            if request.method == "GET" and path == "/jobs/ext-1/outputs":
                return httpx.Response(
                    200,
                    json={"outputs": [{"output_type": "MODEL", "format": "glb", "uri": "/files/model.glb", "mime_type": "model/gltf-binary"}]},
                )
            if request.method == "GET" and path == "/files/model.glb":
                return httpx.Response(200, content=minimal_glb())
            return httpx.Response(404, json={"message": "not found"})

        transport = httpx.MockTransport(handler)
        client = httpx.AsyncClient(base_url="https://provider.test/", transport=transport)
        provider = HTTPProvider(base_url="https://provider.test", api_key="token", client=client)
        submission = await provider.submit("a collectible figure", {"attempt": 1})
        self.assertEqual(submission.provider_job_id, "ext-1")
        progress = await provider.get_status("ext-1")
        self.assertEqual(progress.progress, 100)
        self.assertTrue(await provider.cancel("ext-1"))
        outputs = await provider.fetch_outputs("ext-1")
        self.assertEqual(outputs[0].format, "glb")
        self.assertTrue(outputs[0].content.startswith(b"glTF"))
        await provider.aclose()


if __name__ == "__main__":
    unittest.main()
