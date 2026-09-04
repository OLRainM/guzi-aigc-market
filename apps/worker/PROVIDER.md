# 3D 生成 Provider

Worker 默认 `GENERATION_PROVIDER=mock`。第一期真实链路是：

```text
用户文本 → 术语库检索 → LLM 优化 Prompt → TokenHub HY-3D submit/query → 下载 GLB → MinIO
```

不要把 TokenHub 官方地址填进通用 `PROVIDER_BASE_URL`。HY-3D 使用专用适配器 `GENERATION_PROVIDER=hy3d`。

## 第一期配置

在服务器 `/opt/aigc-3d-platform/.env` 填写：

```env
GENERATION_PROVIDER=hy3d
TOKENHUB_API_KEY=your-tokenhub-key
HY3D_BASE_URL=https://tokenhub.tencentmaas.com
HY3D_MODEL=hy-3d-3.1
HY3D_ENABLE_PBR=true
HY3D_FACE_COUNT=100000
HY3D_GENERATE_TYPE=normal
PROVIDER_POLL_INTERVAL=8
PROVIDER_POLL_TIMEOUT=900
GENERATION_JOB_TIMEOUT=15m

LLM_BASE_URL=https://your-openai-compatible-endpoint/v1
LLM_API_KEY=your-llm-key
LLM_MODEL=gpt-4o-mini
LLM_TIMEOUT=30
```

然后重建：

```bash
cd /opt/aigc-3d-platform
docker compose up -d --build worker web api
```

密钥只放 `.env`，不要入库。未配置 `TOKENHUB_API_KEY` 时不要把 `GENERATION_PROVIDER` 设为 `hy3d`。未配置 `LLM_API_KEY` / `LLM_BASE_URL` 时，Worker 仍会用术语库拼接 Prompt，不阻断生成。

## Prompt 优化

Worker 认领任务后、提交 HY-3D 前会：

1. 用 `rag/terminology/data/terms.jsonl` 做关键词检索
2. 读取 `optimizer-system.zh-CN.md` 和兼容规则
3. 调用 OpenAI 兼容 `POST {LLM_BASE_URL}/chat/completions`
4. 把 `optimized_prompt`、`rag_context`、`rag_version` 回写到任务
5. 用优化后的中文 Prompt（截断 1024 字）提交 HY-3D

LLM 失败时回退到术语片段拼接，不把原始短句直接丢给 3D 模型。

## HY-3D 接口

相对 `HY3D_BASE_URL`：

- 提交：`POST /v1/api/3d/submit`，`Authorization: Bearer $TOKENHUB_API_KEY`
- 查询：`POST /v1/api/3d/query`，body `{ "model", "id" }`
- 无官方取消接口；平台取消只停止本地轮询，上游任务可能继续占用默认并发 3
- 成功后立刻下载 `data[].url` 中的 GLB，存到自己的 MinIO

## 通用 HTTP Provider

`GENERATION_PROVIDER=http` 仍可用于自建 `/jobs` 契约服务，见历史接口：`POST /jobs`、`GET /jobs/{id}`、`GET /jobs/{id}/outputs`、`POST /jobs/{id}/cancel`。
