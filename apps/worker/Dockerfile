FROM python:3.12-slim
WORKDIR /app
COPY apps/worker/requirements.txt .
RUN pip install --no-cache-dir \
    --index-url https://mirrors.aliyun.com/pypi/simple/ \
    --trusted-host mirrors.aliyun.com \
    --default-timeout=120 \
    --retries=8 \
    -r requirements.txt
COPY apps/worker/ .
COPY rag/terminology /app/rag/terminology
ENV RAG_TERMS_PATH=/app/rag/terminology/data/terms.jsonl
ENV RAG_RULES_PATH=/app/rag/terminology/rules/compatibility.json
ENV RAG_SYSTEM_PROMPT_PATH=/app/rag/terminology/prompts/optimizer-system.zh-CN.md
EXPOSE 8000
CMD ["python", "main.py"]
