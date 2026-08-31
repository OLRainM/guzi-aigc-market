import asyncio
import logging
import os
from contextlib import asynccontextmanager

import redis.asyncio as redis

from contracts import ContractError, GenerationJobMessage
from fastapi import FastAPI, Response, status

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
logger = logging.getLogger("ai-worker")
REDIS_ADDR = os.getenv("REDIS_ADDR", "localhost:6379")
STREAM = os.getenv("REDIS_STREAM", "generation_jobs")
GROUP = os.getenv("REDIS_CONSUMER_GROUP", "ai-workers")
CONSUMER = os.getenv("HOSTNAME", "worker-1")
client = redis.from_url(f"redis://{REDIS_ADDR}", decode_responses=True)
stop_event = asyncio.Event()

async def ensure_group() -> None:
    try:
        await client.xgroup_create(STREAM, GROUP, id="0", mkstream=True)
    except redis.ResponseError as exc:
        if "BUSYGROUP" not in str(exc):
            raise

async def consume() -> None:
    await ensure_group()
    while not stop_event.is_set():
        messages = await client.xreadgroup(GROUP, CONSUMER, {STREAM: ">"}, count=1, block=1000)
        for _, entries in messages:
            for message_id, fields in entries:
                try:
                    message = GenerationJobMessage.from_stream(fields)
                    logger.info(
                        "validated stream message",
                        extra={"message_id": message_id, "job_id": str(message.job_id), "request_id": message.request_id},
                    )
                    await client.xack(STREAM, GROUP, message_id)
                except ContractError:
                    logger.exception("rejected invalid stream message", extra={"message_id": message_id})
                except Exception:
                    logger.exception("failed to process stream message", extra={"message_id": message_id})
        await asyncio.sleep(0)

@asynccontextmanager
async def lifespan(_: FastAPI):
    task = asyncio.create_task(consume())
    yield
    stop_event.set()
    await task
    await client.aclose()

app = FastAPI(title="AIGC AI Worker", version="0.1.0", lifespan=lifespan)

@app.get("/healthz")
async def healthz():
    return {"status": "ok"}

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
