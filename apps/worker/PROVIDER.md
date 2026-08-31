# 外部 3D 模型调用接口

Worker 默认使用 `GENERATION_PROVIDER=mock`。接入真实文本生成 3D 服务时，只改环境变量，不必改业务代码。

## 配置

在服务器 `/opt/aigc-3d-platform/.env` 中填写：

```env
GENERATION_PROVIDER=http
PROVIDER_BASE_URL=https://your-3d-api.example.com
PROVIDER_API_KEY=your-token
PROVIDER_TIMEOUT=60
PROVIDER_POLL_INTERVAL=2
```

然后重建 Worker：

```bash
cd /opt/aigc-3d-platform
docker compose up -d --build worker
```

未填写 `PROVIDER_BASE_URL` 时不要把 `GENERATION_PROVIDER` 设为 `http`，否则任务会失败。密钥只放 `.env`，不要提交到 Git。

## 需要你实现的 HTTP 契约

Worker 会把 `Authorization: Bearer $PROVIDER_API_KEY` 带到下列路径（均相对 `PROVIDER_BASE_URL`）。

### 1. 提交任务

`POST /jobs`

```json
{
  "prompt": "a collectible figure",
  "parameters": {
    "attempt": 1,
    "product_type": "手办"
  }
}
```

成功响应 `200` 或 `202`：

```json
{
  "provider_job_id": "ext-123",
  "status": "PENDING"
}
```

`id` 也可代替 `provider_job_id`。`status` 可为 `PENDING` / `QUEUED` / `RUNNING` / `SUCCEEDED` / `FAILED`。

### 2. 查询状态

`GET /jobs/{provider_job_id}`

```json
{
  "status": "RUNNING",
  "progress": 40,
  "error": null
}
```

失败时：

```json
{
  "status": "FAILED",
  "progress": 40,
  "error": {
    "code": "PROVIDER_TIMEOUT",
    "message": "upstream timed out",
    "retryable": true
  }
}
```

`code` 建议使用：`INVALID_REQUEST`、`AUTHENTICATION_FAILED`、`RATE_LIMITED`、`INSUFFICIENT_BALANCE`、`PROVIDER_UNAVAILABLE`、`PROVIDER_TIMEOUT`、`OUTPUT_INVALID`、`DOWNLOAD_FAILED`。

### 3. 取消任务

`POST /jobs/{provider_job_id}/cancel`

```json
{ "canceled": true }
```

### 4. 拉取结果

`GET /jobs/{provider_job_id}/outputs`

```json
{
  "outputs": [
    {
      "output_type": "MODEL",
      "format": "glb",
      "uri": "https://your-cdn.example.com/result.glb",
      "mime_type": "model/gltf-binary",
      "metadata": {}
    }
  ]
}
```

Worker 会下载 `uri`/`url`。也可返回 `content_base64` 内嵌 GLB。第一个可用输出会存入平台对象存储，并在工作台预览。

## 错误响应

HTTP `4xx/5xx` 时返回：

```json
{
  "code": "PROVIDER_UNAVAILABLE",
  "message": "upstream 5xx",
  "error": {
    "code": "PROVIDER_UNAVAILABLE",
    "message": "upstream 5xx",
    "retryable": true
  }
}
```

`429` 和 `5xx` 会重试；`401/403` 和大部分 `4xx` 不会重试。
