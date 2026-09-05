import asyncio
import logging
import os
from contextlib import asynccontextmanager

import redis.asyncio as redis
from fastapi import FastAPI, Header, HTTPException, Response, status
from pydantic import BaseModel, Field

from contracts import ContractError, GenerationJobMessage
from processor import HTTPAPIClient, JobProcessor
from promptopt import PromptOptimizer
from providers.factory import create_provider

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
logger = logging.getLogger("ai-worker")
REDIS_ADDR = os.getenv("REDIS_ADDR", "localhost:6379")
STREAM = os.getenv("REDIS_STREAM", "generation_jobs")
GROUP = os.getenv("REDIS_CONSUMER_GROUP", "ai-workers")
CONSUMER = os.getenv("HOSTNAME", "worker-1")
PEL_IDLE_MS = int(os.getenv("REDIS_PEL_IDLE_MS", "300000"))
API_INTERNAL_URL = os.getenv("API_INTERNAL_URL", "http://localhost:8080")
WORKER_INTERNAL_TOKEN = os.getenv("WORKER_INTERNAL_TOKEN", "")
client = redis.from_url(f"redis://{REDIS_ADDR}", decode_responses=True)
api_client = HTTPAPIClient(API_INTERNAL_URL, WORKER_INTERNAL_TOKEN)
provider = create_provider()
optimizer = PromptOptimizer()
processor = JobProcessor(api_client, provider, optimizer)
stop_event = asyncio.Event()

async def ensure_group() -> None:
    try:
        await client.xgroup_create(STREAM, GROUP, id="0", mkstream=True)
    except redis.ResponseError as exc:
        if "BUSYGROUP" not in str(exc):
            raise

async def handle_message(message_id: str, fields: dict[str, str]) -> None:
    try:
        message = GenerationJobMessage.from_stream(fields)
        result = await processor.process(message)
        logger.info(
            "processed stream message",
            extra={"message_id": message_id, "job_id": str(message.job_id), "result": result},
        )
        await client.xack(STREAM, GROUP, message_id)
    except ContractError:
        logger.exception("rejected invalid stream message", extra={"message_id": message_id})
        await client.xack(STREAM, GROUP, message_id)
    except Exception:
        logger.exception("failed to process stream message", extra={"message_id": message_id})


async def reclaim_pending() -> None:
    try:
        result = await client.xautoclaim(
            STREAM,
            GROUP,
            CONSUMER,
            min_idle_time=PEL_IDLE_MS,
            start_id="0-0",
            count=10,
        )
        entries = result[1] if isinstance(result, (list, tuple)) and len(result) > 1 else []
        for message_id, fields in entries:
            await handle_message(message_id, fields)
    except Exception:
        logger.exception("failed to reclaim pending stream messages")


async def consume() -> None:
    await ensure_group()
    while not stop_event.is_set():
        await reclaim_pending()
        messages = await client.xreadgroup(GROUP, CONSUMER, {STREAM: ">"}, count=1, block=1000)
        for _, entries in messages:
            for message_id, fields in entries:
                await handle_message(message_id, fields)
        await asyncio.sleep(0)

@asynccontextmanager
async def lifespan(_: FastAPI):
    task = asyncio.create_task(consume())
    yield
    stop_event.set()
    await task
    close = getattr(provider, "aclose", None)
    if close:
        await close()
    await optimizer.aclose()
    await api_client.aclose()
    await client.aclose()

app = FastAPI(title="AIGC AI Worker", version="0.2.0", lifespan=lifespan)


class PromptOptimizeRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=2000)
    product_type: str = Field(min_length=1, max_length=64)


@app.get("/healthz")
async def healthz():
    return {"status": "ok"}


@app.post("/internal/prompt-optimize")
async def prompt_optimize(payload: PromptOptimizeRequest, x_worker_token: str | None = Header(default=None)):
    if not WORKER_INTERNAL_TOKEN or x_worker_token != WORKER_INTERNAL_TOKEN:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="worker token is invalid")
    result = await optimizer.optimize(payload.prompt.strip(), payload.product_type.strip())
    return {
        "raw_prompt": payload.prompt.strip(),
        "product_type": payload.product_type.strip(),
        "optimized_prompt": result.text,
        "structured_prompt": result.structured,
        "rag_context": result.rag_context,
        "rag_version": result.rag_version,
        "template_version": result.template_version,
        "source": result.source,
    }


@app.get("/readyz")
async def readyz(response: Response):
    try:
        await client.ping()
        return {"status": "ready", "checks": {"redis": "ok"}}
    except Exception:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "not_ready", "checks": {"redis": "unavailable"}}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("WORKER_PORT", "8000")))
