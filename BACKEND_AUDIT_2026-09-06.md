# 后端代码安全与质量审计报告

**审计日期**: 2026-09-06  
**审计范围**: Go API (`apps/api`) + Python Worker (`apps/worker`)  
**审计目标**: 安全漏洞、代码质量、性能问题

> 状态说明：`[已完成]` 表示本轮已完成代码修复并通过测试；`[部分完成]` 表示已完成缓解措施但仍需环境验证或后续优化；`[待处理]` 表示尚未实施。

---

## 一、安全漏洞

### 🔴 高危

#### 1.1 SQL注入风险 - LIKE查询拼接 `[已完成]`

**位置**: `apps/api/internal/catalog/catalog.go:146-147`

```go
keyword := strings.TrimSpace(req.Keyword)
query = query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
```

**问题描述**:  
- 虽然使用了参数化查询 `?`，但在应用层手动拼接 `%keyword%`
- 如果 `keyword` 包含通配符 `%` 或 `_`，可能导致意外的模糊匹配行为
- 虽然不是传统SQL注入，但属于**通配符注入**，可能被利用进行拒绝服务攻击

**修复方向**:  
- 对用户输入进行转义：`strings.ReplaceAll(keyword, "%", "\\%")` 和 `strings.ReplaceAll(keyword, "_", "\\_")`
- 或者使用全文搜索引擎（Elasticsearch/Meilisearch）替代 LIKE 查询

---

#### 1.2 Worker 内部 API 认证过于简单 `[部分完成]`

**位置**: `apps/api/internal/generation/handler.go:257-261`

```go
token := c.GetHeader("X-Worker-Token")
if token == "" || token != h.workerToken {
    c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "无效的 worker 令牌"}})
    return
}
```

**问题描述**:  
- Worker Token 仅为静态字符串，无签名、无过期时间、无轮换机制
- 如果 Token 泄露（日志、网络抓包），攻击者可伪造 Worker 请求修改生成任务状态
- 无请求来源 IP 白名单限制

**修复方向**:  
- 使用 HMAC 签名 + 时间戳的请求签名机制（类似 AWS Signature V4）
- 或使用 mTLS（双向TLS）验证 Worker 身份
- 添加 IP 白名单限制（仅允许内部容器网络）

---

#### 1.3 开发环境硬编码 JWT Secret `[已完成]`

**位置**: `apps/api/cmd/api/main.go:62-64`

```go
if jwtSecret == "" {
    jwtSecret = "dev-secret-change-in-production-b3f2a9c8d1e4"
}
```

**问题描述**:  
- 开发环境使用固定的 JWT Secret，容易被逆向
- 如果开发环境数据库包含真实用户数据，攻击者可伪造任意用户令牌
- 代码中包含默认值，可能被误用于生产环境

**修复方向**:  
- 在生产环境强制检查 `JWT_SECRET` 环境变量，未设置时拒绝启动
- 生成随机的开发环境 Secret（每次启动生成）
- 使用 `.env.example` 文件明确说明生产环境必须设置

---

### 🟡 中危

#### 1.4 密码重置令牌缺少使用次数限制 `[待处理]`

**位置**: `apps/api/internal/auth/auth.go:283-314` (Refresh Token)

**问题描述**:  
- Refresh Token 仅依赖家族检测（`token_family`）防止重放
- 如果攻击者在 Token 过期前窃取并使用，仅会触发家族失效，但无频率限制
- 缺少单个 Token 的使用次数记录（应为一次性消耗）

**修复方向**:  
- 在 Redis 中记录已使用的 Refresh Token（使用 TTL = Token 过期时间）
- 检测到重复使用立即失效整个家族

---

#### 1.5 订单竞态条件：SQLite 测试环境无行锁 `[部分完成]`

**位置**: `apps/api/internal/account/orders.go:378-383`

```go
func lockFirst(db *gorm.DB, dest interface{}, conds ...interface{}) error {
    query := db.First(dest, conds...)
    if strings.Contains(db.Dialector.Name(), "sqlite") {
        return query.Error
    }
    return query.Clauses(clause.Locking{Strength: "UPDATE"}).Error
}
```

**问题描述**:  
- SQLite 环境下不使用 `FOR UPDATE`，可能导致并发下单时库存扣减不准确
- 测试环境与生产环境行为不一致，可能隐藏并发 Bug

**修复方向**:  
- 使用 SQLite 的 `BEGIN IMMEDIATE` 事务模式
- 或在测试时使用内存模式的 PostgreSQL/MySQL（如 `testcontainers`）

---

#### 1.6 错误信息泄露内部实现 `[部分完成]`

**位置**: 多处，例如 `apps/api/internal/generation/handler.go:123-125`

```go
c.JSON(500, gin.H{"error": gin.H{
    "code": "INTERNAL_ERROR", "message": err.Error(),
}})
```

**问题描述**:  
- 部分 500 错误曾直接返回 `err.Error()`，可能包含数据库查询、文件路径、依赖库版本等敏感信息
- 攻击者可通过错误信息收集系统架构信息

**已完成缓解**:  
- 生成任务响应已按错误代码映射为固定的用户可见文案
- Outbox `last_error` 已改为固定分类摘要，不再持久化底层错误原文

**剩余工作**:  
- 继续排查其他 Handler 的 500 响应，统一返回通用错误信息（"内部服务错误，请稍后重试"）
- 详细错误仅记录到日志，不返回给客户端
- 使用错误代码而非错误文本

---

### 🟢 低危

#### 1.7 CORS 配置允许所有来源（开发环境） `[待处理]`

**位置**: `apps/api/cmd/api/main.go:123-130`

```go
AllowOrigins: []string{"*"}
```

**问题描述**:  
- 开发环境允许所有来源，生产环境应限制为前端域名
- 虽然代码中使用环境变量配置，但需确保生产环境正确设置

**修复方向**:  
- 生产环境强制检查 `CORS_ALLOWED_ORIGINS`，不允许 `*`
- 或使用反向代理（Nginx）处理 CORS，后端不暴露

---

#### 1.8 MinIO 对象键可预测 `[待处理]`

**位置**: `apps/api/internal/asset/asset.go:94`

```go
key := objectKey(ownerID, productID, id, extensionFromKey(source.ObjectKey, "glb"))
```

**问题描述**:  
- 对象键基于 UUID 生成，但格式可预测（`{ownerID}/{productID}/{assetID}.{ext}`）
- 如果 MinIO 桶配置错误（公开读），攻击者可遍历资产

**修复方向**:  
- 对象键加盐哈希（如 `sha256(ownerID+productID+assetID+salt)`）
- 强制 MinIO 桶为私有，通过预签名 URL 下载

---

## 二、认证与授权逻辑

### 2.1 JWT 刷新机制设计合理 `[部分完成]`

**位置**: `apps/api/internal/auth/auth.go:283-314`

**优点**:  
- 使用 Token Family 检测重放攻击
- Refresh Token 采用 SHA256 哈希存储
- Access Token 短期有效（需确认具体过期时间）

**潜在问题**:  
- 未找到 Access Token 过期时间配置，建议设置为 15 分钟
- Refresh Token 过期时间建议设置为 7-30 天

---

### 2.2 权限检查缺失：跨用户资源访问 `[部分完成]`

**位置**: `apps/api/internal/catalog/catalog.go:427-454` (`deleteProduct`)

```go
var product Product
if err := s.db.WithContext(ctx).First(&product, "id = ?", productID).Error; err != nil {
    return err
}
if product.UserID != userID {
    return ErrForbidden
}
```

**优点**:  
- 删除操作正确检查了 `product.UserID == userID`

**需要检查的其他接口**:  
- `PATCH /products/:id` - 是否检查所有权？
- `GET /products/:id/assets/:assetId/content` - 是否检查所有权？
- `POST /orders` - 是否检查买家不是卖家本人？

**修复方向**:  
- 抽取通用的 `RequireOwnership` 中间件
- 在所有修改资源的接口前统一检查

---

### 2.3 管理员权限未实现细粒度控制 `[待处理]`

**位置**: `apps/api/internal/admin/admin.go` (未在本次读取范围内)

**假设问题**:  
- 如果管理员接口仅检查 `isAdmin` 字段，缺少操作级权限（如：只读管理员、内容审核员）
- 需要确认是否存在超级管理员权限过大的问题

**修复方向**:  
- 引入 RBAC（基于角色的访问控制）
- 使用 Casbin 等权限库管理细粒度权限

---

## 三、输入校验

### 3.1 Multipart 文件上传校验完善 `[部分完成]`

**位置**: `apps/api/internal/generation/handler.go:197-221`

**优点**:  
- 限制文件大小为 20MB
- 使用 `http.DetectContentType` 验证 MIME 类型
- 检查文件扩展名与 MIME 的一致性

**问题**:  
- 未检查文件名中的路径遍历字符（`../`）
- 未限制上传频率（可能被用于 DDoS）

**修复方向**:  
- 使用 `filepath.Base(filename)` 去除路径
- 添加 IP 级别的上传频率限制（如：每分钟最多 10 次）

---

### 3.2 Prompt 长度限制合理但缺少内容过滤 `[部分完成]`

**位置**: `apps/api/internal/generation/service.go:206`

```go
if finalPrompt == "" || utf8.RuneCountInString(finalPrompt) > 1024 || ...
```

**优点**:  
- 使用 `utf8.RuneCountInString` 而非 `len`，正确处理多字节字符
- 限制 1024 字符，防止超长 Prompt

**问题**:  
- 未过滤恶意 Prompt（如：尝试注入系统提示词的内容）
- 未检查重复字符（如：`aaaa...` 1000 次）

**修复方向**:  
- 引入敏感词过滤（政治、色情、暴力）
- 检测重复字符比例，超过阈值拒绝

---

### 3.3 订单金额校验不足 `[已完成]`

**位置**: `apps/api/internal/account/orders.go:142-149`

```go
if req.Quantity <= 0 || req.TotalAmount <= 0 {
    return nil, errInvalidArgument
}
```

**问题**:  
- 未验证 `TotalAmount` 是否等于 `商品单价 × Quantity`
- 客户端可能伪造更低的金额

**修复方向**:  
- 在服务端重新计算总价：`totalAmount = product.Price * req.Quantity`
- 拒绝客户端传入的 `TotalAmount`

---

## 四、错误处理与日志

### 4.1 Python Worker 异常处理完善 `[已完成]`

**位置**: `apps/worker/processor.py:176-191`

**优点**:  
- 区分 `JobCanceled`、`WorkerAPIError`、`ProviderError` 不同异常
- 使用结构化日志（`logger.exception`）
- 失败时调用 `api.fail()` 回写状态

**问题**:  
- 第 227 行：失败回调本身异常时 `raise` 会导致消息未 ACK，造成无限重试
- 建议在失败回调异常时仍 ACK 消息，记录到死信队列

---

### 4.2 Go API 日志缺少结构化字段 `[已完成]`

**位置**: 多处使用 `log.Printf`

**问题**:  
- 未使用结构化日志库（如 `zap`、`zerolog`）
- 难以进行日志聚合分析（如：统计某个用户的所有操作）

**修复方向**:  
- 引入 `zap` 或 `zerolog`
- 统一日志格式：`{"level":"info","time":"2026-09-06T10:00:00Z","user_id":"xxx","msg":"..."}`

---

### 4.3 敏感信息可能记录到日志 `[已完成]`

**位置**: `apps/worker/main.py:42-45`

```python
logger.info(
    "processed stream message",
    extra={"message_id": message_id, "job_id": str(message.job_id), "result": result},
)
```

**问题**:  
- 如果 `fields` 包含用户 Prompt，可能记录到日志
- 日志可能被非授权人员访问（如：运维人员）

**修复方向**:  
- 日志中不记录 `raw_prompt`、`optimized_prompt` 等用户内容
- 使用日志脱敏工具（如：替换为 `prompt="<redacted>"`）

---

## 五、数据库查询与事务

### 5.1 事务处理规范 `[部分完成]`

**位置**: `apps/api/internal/account/orders.go:157-282` (`placeOrder`)

**优点**:  
- 使用 `db.Transaction` 保证订单创建、库存扣减、资金扣减的原子性
- 使用 `FOR UPDATE` 行锁防止并发超卖
- 事务失败自动回滚

**问题**:  
- 事务较长（100+ 行），包含多个数据库查询
- 建议拆分为多个小函数（`lockProduct`、`debitBuyer`、`createOrder`）

---

### 5.2 潜在的 N+1 查询问题 `[待处理]`

**位置**: `apps/api/internal/catalog/catalog.go:100-118` (`List`)

```go
for i := range products {
    // 可能在循环内查询每个商品的图片
}
```

**问题**:  
- 如果在循环内查询商品的关联数据（如：图片、标签），会产生 N+1 查询
- 未在代码中发现 `Preload`，需要确认是否存在此问题

**修复方向**:  
- 使用 `db.Preload("Images").Find(&products)`
- 或使用 JOIN 一次性查询

---

### 5.3 缺少数据库索引优化 `[待处理]`

**位置**: `apps/api/internal/catalog/catalog.go:146-147`

```go
query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
```

**问题**:  
- `LIKE '%keyword%'` 无法使用索引，性能差
- 数据量大时会全表扫描

**修复方向**:  
- 使用全文索引（MySQL `FULLTEXT`，PostgreSQL `ts_vector`）
- 或迁移到 Elasticsearch

---

## 六、接口设计

### 6.1 幂等性设计合理 `[部分完成]`

**位置**: `apps/api/internal/generation/service.go:76-93`

**优点**:  
- 使用 `Idempotency-Key` (UUID) 防止重复创建
- 使用 `RequestHash` (SHA256) 检测请求内容变更
- 冲突时返回已存在的任务

**问题**:  
- 未找到幂等键的过期时间，建议保留 24-72 小时
- 订单接口同样使用幂等键，但未找到清理逻辑

---

### 6.2 REST 规范基本符合 `[部分完成]`

**优点**:  
- 使用标准 HTTP 方法（GET/POST/PATCH/DELETE）
- 状态码使用合理（200/201/400/401/404/500）
- URL 命名清晰（`/api/v1/products/:id`）

**问题**:  
- 部分接口缺少 `Content-Type: application/json` 检查
- 未找到 API 版本迁移策略（`/v1` -> `/v2`）

---

### 6.3 分页参数缺少默认值限制 `[部分完成]`

**位置**: `apps/api/internal/catalog/catalog.go:93-99`

```go
page := req.Page
limit := req.Limit
if limit <= 0 || limit > 100 {
    limit = 20
}
```

**优点**:  
- 限制最大页大小为 100

**问题**:  
- 如果客户端传入 `page=999999`，可能导致性能问题
- 建议限制最大页码（如：1000 页）

---

## 七、性能瓶颈

### 7.1 商品搜索 LIKE 查询性能差 `[待处理]`

**严重程度**: 🟡 中危  
**位置**: `apps/api/internal/catalog/catalog.go:146-147`  
**影响**: 数据量超过 1 万条时响应时间可能超过 1 秒

**修复方向**:  
- 短期：添加前缀索引（`INDEX idx_name_prefix ON products (name(10))`）
- 长期：迁移到 Elasticsearch

---

### 7.2 Worker 轮询间隔可能过短 `[待处理]`

**位置**: `apps/worker/processor.py:116-117`

```python
self.poll_interval = float(os.getenv("PROVIDER_POLL_INTERVAL", "8"))
```

**问题**:  
- 默认每 8 秒轮询一次 Provider 状态
- 如果 Provider 生成时间通常为 5-10 分钟，8 秒轮询可能导致无意义的 API 调用

**修复方向**:  
- 使用 WebHook 而非轮询（Provider 完成后主动回调）
- 或使用指数退避（第 1 次 5 秒，第 2 次 10 秒，第 3 次 20 秒...）

---

### 7.3 资产拷贝时全量读取到内存 `[待处理]`

**位置**: `apps/api/internal/asset/asset.go:83-86`

```go
data, err := io.ReadAll(io.LimitReader(body, limit+1))
```

**问题**:  
- 将整个 GLB 文件（最大 20MB）读入内存
- 并发拷贝多个文件时可能导致内存峰值

**修复方向**:  
- 使用流式拷贝（MinIO 的 `CopyObject` API）
- 或使用分块读取（每次 1MB）

---

## 八、并发与竞态条件

### 8.1 订单行锁机制合理 `[部分完成]`

**位置**: `apps/api/internal/account/orders.go:378-383`

**优点**:  
- 使用 `FOR UPDATE` 锁定商品行
- 先锁定再扣减库存，避免超卖

**问题**:  
- SQLite 环境下无行锁（已在 1.5 中提及）

---

### 8.2 Redis Streams 消费者组设计合理 `[部分完成]`

**位置**: `apps/worker/main.py:54-68`

**优点**:  
- 使用 `XAUTOCLAIM` 回收空闲消息（默认 5 分钟）
- 使用 `XACK` 确认消息已处理
- 支持多 Worker 并发消费

**问题**:  
- 未找到最大重试次数限制，消息可能无限重试
- 建议在 `processor.py` 中记录失败次数，超过 3 次后移到死信队列

---

### 8.3 生成任务状态机缺少并发保护 `[已完成]`

**位置**: `apps/api/internal/generation/handler.go:269-310` (`handleClaim`)

```go
if job.Status != StatusQueued && job.Status != StatusFailed {
    c.JSON(409, ...)
    return
}
// ... 更新状态为 RUNNING
```

**问题**:  
- 检查状态与更新状态之间无原子性保证
- 如果两个 Worker 同时 Claim 同一个任务，可能导致重复执行

**修复方向**:  
- 使用乐观锁（`UPDATE ... WHERE id = ? AND version = ?`）
- 或使用分布式锁（Redis `SETNX`）

---

## 九、硬编码配置与密钥管理

### 9.1 开发环境默认配置过多 `[已完成]`

**位置**: `apps/api/cmd/api/main.go:49-75`

```go
if jwtSecret == "" {
    jwtSecret = "dev-secret-change-in-production-b3f2a9c8d1e4"
}
if workerToken == "" {
    workerToken = "worker-internal-token-change-me"
}
```

**问题**:  
- 多个敏感配置有默认值，容易误用于生产
- 代码中包含注释提示更改，但未强制检查

**修复方向**:  
- 生产环境强制检查所有敏感配置，未设置时拒绝启动
- 或使用配置验证库（如：`viper` + 环境检查）

---

### 9.2 MinIO 凭证未轮换 `[待处理]`

**位置**: `apps/api/cmd/api/main.go:91-95`

**问题**:  
- MinIO Access Key 和 Secret Key 长期不变
- 如果泄露，攻击者可永久访问对象存储

**修复方向**:  
- 使用 IAM 角色（如：阿里云 RAM，AWS IAM）
- 或定期轮换密钥（每 90 天）

---

### 9.3 数据库连接字符串包含密码 `[待处理]`

**位置**: `apps/api/cmd/api/main.go:81-83`

**问题**:  
- 连接字符串中包含明文密码
- 日志或错误堆栈可能泄露

**修复方向**:  
- 使用 Secret 管理工具（如：Vault，K8s Secrets）
- 或使用 IAM 认证（如：AWS RDS IAM）

---

## 十、代码结构与可维护性

### 10.1 事务函数过长

**位置**: `apps/api/internal/account/orders.go:157-282` (`placeOrder`)

**问题**:  
- 单个事务函数 125 行
- 包含业务逻辑、数据库操作、错误处理混杂

**修复方向**:  
- 拆分为 `lockProduct`、`debitBuyer`、`createOrder`、`creditSeller` 等小函数
- 事务函数仅负责协调调用

---

### 10.2 错误定义分散 `[待处理]`

**问题**:  
- 每个包独立定义错误（`errInvalidArgument`、`errForbidden`）
- 缺少统一的错误码规范

**修复方向**:  
- 创建 `internal/errors` 包，统一定义错误
- 使用错误码枚举（如：`E1001: 参数错误`、`E2001: 未授权`）

---

### 10.3 测试覆盖率不明 `[部分完成]`

**现状**: `[部分完成]` 已存在 Go 与 Python 单元测试，且本轮新增了 LIKE 转义和 Worker 失败路径测试；仍需补充覆盖率统计及真实数据库集成测试。

**原问题**:  
- 关键业务逻辑的测试覆盖率和真实数据库并发行为仍未充分验证

**修复方向**:  
- 添加单元测试，覆盖率至少 60%
- 添加集成测试（使用 Testcontainers 启动真实数据库）

---

### 10.4 Python Worker 依赖版本未固定 `[已完成]`

**位置**: `apps/worker/requirements.txt`

**现状**: `[已完成]` 直接依赖已使用固定版本。

**原问题**:  
- 依赖版本未固定可能导致不同环境依赖版本不一致

**修复方向**:  
- 使用 `pip freeze > requirements.txt` 固定所有依赖版本
- 使用 Poetry 或 Pipenv 管理依赖

---

## 总结与优先级

### 🔴 P0 - 立即修复（安全风险高）

1. **Worker Token 认证过于简单** `[部分完成]` - `generation/handler.go:257`

### 🟡 P1 - 近期修复（影响稳定性）

2. **错误信息泄露** `[部分完成]` - 生成任务与 Outbox 错误已脱敏，其他 Handler 仍需排查
3. **SQLite 行锁缺失** `[部分完成]` - `orders.go:378`
4. **LIKE 查询性能** `[待处理]` - `catalog.go:146`
5. **Redis Streams 最大重试次数 / 死信队列** `[待处理]` - `apps/worker`

### 🟢 P2 - 长期优化（代码质量）

6. **事务函数过长** `[待处理]` - `orders.go:157`
7. **统一错误定义** `[待处理]` - `internal/errors`
8. **测试覆盖率与真实数据库集成验证** `[部分完成]` - 全局
9. **上传频率限制、CORS、凭证轮换和 Secret 管理** `[待处理]` - 多处

> 已完成项目：LIKE 通配符转义、订单金额服务端重算、生成任务 Claim 并发保护、Go 结构化日志、Worker 日志脱敏、生产敏感配置强校验、Worker 依赖版本固定。

---

## 附录：建议的后续行动

1. **安全加固**
   - 引入静态代码扫描（Gosec、Bandit）
   - 定期进行渗透测试
   - 添加 WAF（Web Application Firewall）

2. **性能优化**
   - 引入 APM 工具（如：Sentry、Datadog）
   - 添加慢查询日志分析
   - 压力测试订单并发场景

3. **代码质量**
   - 添加 CI/CD 流水线（单元测试 + 代码覆盖率检查）
   - 引入代码审查流程
   - 使用 `golangci-lint` 统一代码风格

4. **监控告警**
   - 添加关键业务指标监控（订单创建成功率、生成任务失败率）
   - 配置异常告警（如：订单金额异常、大量 500 错误）

---

**审计结束日期**: 2026-09-06  
**审计人**: Claude (AI Code Assistant)  
**审计版本**: Git Commit `working tree`（2026-09-06 修复进度见下）

## 修复进度（2026-09-06）

### 已完成

- 商品、管理员、收藏列表的用户关键词查询统一转义 `\\`、`%`、`_`，并显式使用 `ESCAPE '\\'`，避免通配符改变查询语义。
- 生产环境启动时强制校验 `JWT_SECRET`（至少 32 字符）和 `WORKER_INTERNAL_TOKEN`，不再允许敏感配置缺失时启动。
- Worker 失败回写异常不再向消费循环抛出，消息可完成 ACK，避免因回写服务异常造成无限重试。
- 订单服务端根据商品锁定后的 `PriceCents × Quantity` 计算订单金额，未信任客户端金额字段。
- 生成任务 Claim 已在事务中使用 `FOR UPDATE`（SQLite 除外）并校验状态与 attempt，避免同一任务被重复认领。
- Go API 已使用 `log/slog` JSON handler；Python Worker 处理日志仅记录任务标识和结果，不记录 Prompt 内容。
- Python Worker `requirements.txt` 已固定直接依赖版本。

### 尚未完成/需环境验证

- Worker 静态 Token 尚未升级为 HMAC/mTLS；当前通过生产强制配置、容器内网络隔离和不暴露内部接口降低风险。
- SQLite 不支持行锁，尚未引入 PostgreSQL/MySQL Testcontainers；生产 MySQL 行锁路径需在部署环境压测验证。
- `%keyword%` 仍属于包含匹配，数据量增长后应迁移全文索引；本轮只修复通配符语义，不宣称完成性能优化。
- 事务拆分、统一错误包、死信队列/最大重试次数和上传频率限制属于后续 P1/P2 工作。
