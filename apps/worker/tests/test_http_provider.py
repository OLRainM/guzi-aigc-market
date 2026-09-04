import json
import os
import unittest
from unittest.mock import patch

try:
    import httpx
except ImportError:  # pragma: no cover
    httpx = None

from providers.factory import create_provider
from providers.mock import MockProvider, minimal_glb


class ProviderFactoryTests(unittest.TestCase):
    def test_factory_defaults_to_mock(self) -> None:
        self.assertIsInstance(create_provider("mock"), MockProvider)

    def test_factory_rejects_hy3d_without_key(self) -> None:
        with patch.dict(os.environ, {"TOKENHUB_API_KEY": "", "PROVIDER_API_KEY": ""}):
            with self.assertRaises(Exception):
                create_provider("hy3d")


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


@unittest.skipUnless(httpx, "httpx is not installed")
class Hy3DProviderTests(unittest.IsolatedAsyncioTestCase):
    async def test_hy3d_submit_query_and_glb(self) -> None:
        from providers.hy3d import Hy3DProvider
        from providers.mock import minimal_glb

        def handler(request: httpx.Request) -> httpx.Response:
            path = request.url.path
            if request.method == "POST" and path.endswith("/v1/api/3d/submit"):
                body = json.loads(request.content)
                assert body["prompt"]
                assert body["model"] == "hy-3d-3.1"
                return httpx.Response(200, json={"id": "task-1", "status": "QUEUED"})
            if request.method == "POST" and path.endswith("/v1/api/3d/query"):
                return httpx.Response(
                    200,
                    json={
                        "id": "task-1",
                        "status": "COMPLETED",
                        "data": [{"type": "glb", "url": "https://cdn.test/model.glb"}],
                    },
                )
            if request.method == "GET" and path.endswith("/model.glb"):
                return httpx.Response(200, content=minimal_glb())
            return httpx.Response(404, json={"message": "not found"})

        transport = httpx.MockTransport(handler)
        client = httpx.AsyncClient(base_url="https://tokenhub.test/", transport=transport)
        provider = Hy3DProvider(base_url="https://tokenhub.test", api_key="token", client=client)
        submission = await provider.submit("一只小狗", {"attempt": 1})
        self.assertEqual(submission.provider_job_id, "task-1")
        progress = await provider.get_status("task-1")
        self.assertEqual(progress.status.value, "SUCCEEDED")
        outputs = await provider.fetch_outputs("task-1")
        self.assertEqual(outputs[0].format, "glb")
        self.assertTrue(outputs[0].content.startswith(b"glTF"))
        await provider.aclose()


if __name__ == "__main__":
    unittest.main()
