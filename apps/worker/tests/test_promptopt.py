import json
import unittest
from pathlib import Path

from promptopt.knowledge import TerminologyIndex
from promptopt.optimizer import PromptOptimizer

ROOT = Path(__file__).resolve().parents[3]
TERMS = ROOT / "rag" / "terminology" / "data" / "terms.jsonl"
RULES = ROOT / "rag" / "terminology" / "rules" / "compatibility.json"


class PromptOptimizerTests(unittest.IsolatedAsyncioTestCase):
    async def test_fallback_uses_terminology(self) -> None:
        index = TerminologyIndex(TERMS, RULES)
        optimizer = PromptOptimizer(index=index)
        result = await optimizer.optimize("做一个棉花娃", "棉花娃")
        self.assertEqual(result.source, "rag_fallback")
        self.assertIn("棉花娃", result.text)
        self.assertTrue(result.rag_context["term_ids"])
        await optimizer.aclose()

    async def test_llm_json_is_preferred(self) -> None:
        try:
            import httpx
        except ImportError:
            self.skipTest("httpx is not installed")

        index = TerminologyIndex(TERMS, RULES)

        def handler(request: httpx.Request) -> httpx.Response:
            payload = {
                "choices": [
                    {
                        "message": {
                            "content": json.dumps(
                                {
                                    "text_to_3d_prompt": "棉花娃，软填充布料分片，干净拓扑，适合导出 GLB。",
                                    "product_type": "棉花娃",
                                    "negative_prompt": ["硬质树脂"],
                                },
                                ensure_ascii=False,
                            )
                        }
                    }
                ]
            }
            return httpx.Response(200, json=payload)

        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        optimizer = PromptOptimizer(index=index, client=client)
        optimizer.api_key = "test"
        optimizer.base_url = "https://llm.test/v1"
        result = await optimizer.optimize("做一个棉花娃", "棉花娃")
        self.assertEqual(result.source, "llm")
        self.assertIn("软填充", result.text)
        await optimizer.aclose()


if __name__ == "__main__":
    unittest.main()
