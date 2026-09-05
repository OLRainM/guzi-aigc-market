# v1.0 发布检查清单

## 自动检查

在仓库根目录执行：

```powershell
.deploy\verify_release.ps1
```

脚本会依次执行：

- API：`go test ./...`
- Worker：`python -m unittest discover -s tests -p test_*.py -v`（Windows 自动回退到 `py -3`）
- Web：`npm run build`
- Compose：`docker compose config --quiet`
- 可选安全检查：`govulncheck`、`pip-audit`、`npm audit --omit=dev`、`trivy`

未安装的可选工具只会被标记为 skipped，不会伪造为通过。依赖审计应在网络和工具可用时单独补跑，并把输出归档到发布记录。

## 远端验收

1. 发布前保留服务器备份，确认备份文件权限为 `600`，并记录 SHA-256。
2. 确认六个 Compose 服务为 `healthy`。
3. 确认 `/healthz`、`/readyz`、`/api/v1/version` 和首页返回预期状态。
4. 确认公网只暴露 SSH 和 HTTP，MySQL、Redis、MinIO、API、Worker 内部端口不直出。
5. 确认 HTTP IP 方案下 Refresh Cookie 为 `HttpOnly; SameSite=Lax` 且不带 `Secure`。
6. 只读压测使用固定请求数和并发上限；禁止默认对公网执行注册、下单或生成任务压测。

## 尚未自动关闭的事项

- MySQL、MinIO、Redis 数据备份及隔离恢复演练。
- 真实 HY-3D/LLM Provider 联调及成本、限流、余额不足验收。
- 演示数据、测试账号和目标用户内测记录。
- 依赖漏洞扫描在扫描工具和网络可用前不能标记为通过。
