# 未完成阶段与后续执行计划

- **项目**：谷子交易与 3D/AIGC 平台
- **记录日期**：2026-09-05
- **当前阶段**：阶段 1–5 与阶段 6 的认证、商品/资产、3D 查看器、收藏/个人中心、交易沙盒、模拟订单、AI 生成 HTTP 闭环、一键发布、生成可靠性和最小管理后台/审计均已完成；阶段 7 的发布前验证与运维尚未完成。
- **当前基线**：核心 MVP 已可部署并在 `http://8.154.28.98` 验收；Mock Provider 可用，真实 AI Provider 仍需人工配置密钥并联调。
- **权威未完成清单**：见本文「十一、未完成事项与推荐开发顺序」中的 P1/P2 表格；该节按依赖和优先级排序，后文阶段 6/7 仅作范围对照。

## 当前进行中的任务

- **任务名称**：未完成事项整理与开发顺序重排
- **任务描述**：汇总代码、PRD、MVP 计划和阶段清单中的未完成内容，区分核心 MVP 缺口、发布验收工作和后续扩展。
- **开始时间**：2026-09-05
- **当前进度状态**：P0 核心闭环和 P1 API 关键测试已完成，进入发布前 E2E 与运维验收。
- **已完成进展**：核心业务 P0、模拟订单、一键发布、生成可靠性、管理员 RBAC/审计测试及订单跨用户/异常状态契约测试已完成，API 全量测试通过。
- **当前阻塞事项**：真实 Provider 密钥、正式支付资质/账号和内测参与者需要人工提供。
- **下一步**：执行浏览器 E2E 与失败场景验收，随后进行恢复、备份、安全和运维验收。
- **明确后置**：真实支付、退款账本、真实物流、复杂审核/风控、CDN/模型压缩和生产级弹性架构。

## 远端只读环境监测（2026-08-31）

- SSH 已通：`root` 登录成功；系统为 Alibaba Cloud Linux 4.0.5，x86_64，2 vCPU / 3.5Gi 内存 / 根盘 40G（约 33%），项目目录 `/opt/aigc-3d-platform`。
- Docker 24.0.9、Compose 2.26.1、Buildx、Git 已安装；`docker` 开机启动；`deploy` 用户在 `docker` 组。已配置多家镜像加速。
- 六个容器均 `healthy`（已运行约 11–12 天）：MySQL / Redis / MinIO / API / Worker / Web。数据卷 `mysql-data`、`redis-data`、`minio-data` 存在。
- 内部端口均绑定 `127.0.0.1`（3306/6379/9000/9001/8080/8000/5173）。公网监听为宿主机 Nginx `:80` 与 SSH `:22`；无 `:443`。`firewalld` 未启用。
- 本机与服务器访问 `http://<公网IP>/` 返回前端 200；API 直连 `127.0.0.1:8080/healthz`、`/readyz` 为 200，依赖检查 mysql/redis/minio 均为 ok。
- 与当前仓库不一致、影响验收的问题：
  1. 远端 `WEB_PORT=127.0.0.1:5173`，Web 容器未直接占用 80；公网入口是宿主机 Nginx，且 `server_name` 为域名，不是 IP 统一入口。
  2. 公网 `/healthz` 返回前端 HTML，不是 API JSON；公网 `/api/healthz` 被转到 Go 的 `/api/healthz` 因而 404（API 实际在 `/healthz`）。
  3. Web 容器内 Nginx 仍是旧配置，没有仓库里的 `/api/`、`/healthz`、`/readyz` 反代。
  4. `.env` 中 `COOKIE_SECURE=true`，但服务器没有 HTTPS，HTTP IP 下 Refresh Cookie 会被浏览器丢弃。
  5. `CORS_ALLOW_ORIGIN` 指向域名 HTTPS；`vm.overcommit_memory=0`；Docker 未配日志轮转；未见备份目录。
  6. 远端代码时间戳为 8 月 18–20 日，需重新部署才能带上本地商品资产上传等更新。

## 远端部署验收（2026-08-31）

- 已备份到 `/opt/backup/aigc-3d-platform-20260831-161024.tar.gz`；宿主机 Nginx 已停止并禁用；公网 80 改由 Web 容器占用。
- 远端 `.env` 已改为 `WEB_PORT=80`、`COOKIE_SECURE=false`、`CORS_ALLOW_ORIGIN=http://8.154.28.98`、`VITE_API_BASE_URL=`；`vm.overcommit_memory=1`；Docker 日志轮转 `json-file 10m×3`。
- 当前代码已同步并重建：六个容器均 `healthy`。API `version=0.3.0`，`APP_ENV=production`。
- 本机与公网验收：`/` 返回前端 200；`/healthz`、`/readyz`、`/api/v1/version` 均为 API JSON 200。注册/登录/刷新返回 `HttpOnly; SameSite=Lax` 的 `refresh_token`（无 `Secure`），退出 204。

## 阶段 2 运行环境验收（2026-08-31）

- Docker 24.0.9 开机启动；Compose 2.26.1、Buildx、Git、Curl、OpenSSL 可用；`deploy` 用户在 `docker` 组。
- `vm.overcommit_memory=1` 已写入 `/etc/sysctl.d/99-redis-overcommit.conf`；Docker 日志轮转为 `json-file 10m×3`。
- 项目目录 `/opt/aigc-3d-platform`，备份目录 `/opt/backup` 已存在，最近备份为 `aigc-3d-platform-20260831-161024.tar.gz`。
- `firewalld` 已启用，公网仅开放 `ssh` 与 `http`；MySQL/Redis/MinIO 仅监听 `127.0.0.1`，Web 占用 `:80`。
- 时间同步：`chronyd` 启用且时钟已同步（Asia/Shanghai）。磁盘告警脚本每小时检查根盘使用率（阈值 80%），每日 03:15 记录磁盘/内存/容器状态。
- 配置后六个容器均为 `healthy`；本机 `/` `/healthz` `/readyz` `/api/v1/version` 与公网 `/healthz` 均为 200。

> 本文档同步记录各阶段完成状态和剩余工作。远程连接所需的服务器地址、账号、密钥和密码不写入仓库，也不要提交到 Git。

## 一、当前已完成事项（实现步骤 / 方法 / 代码）

下面按落地顺序记录**已完成**能力。每步写清：改了什么、关键函数、调用约定、验收、以及本步明确不做的事。当前未完成项统一维护在第十一节；阶段 6/7 的清单仅作范围对照。

### 第 0 步：认证会话（可单独合入）

不新建聚合根，先把登录态做成可复用中间件。本步只读写 `User` / `Role` / `RefreshToken`，不引入订单或个人资料表。

抽出 `auth.Service.issueSession(user, familyID)`：

1. `bcrypt.GenerateFromPassword` 存 `User.PasswordHash`，明文不入库
2. `createAccessToken` 签发 HS256 JWT：`sub=user.id`，`roles` 进 claims，默认 Access TTL 15 分钟
3. `randomToken`（32 字节 RawURL）+ SHA-256 `tokenHash` 写入 `RefreshToken`，默认 Refresh TTL 30 天
4. `setRefreshCookie`：Cookie 名默认 `refresh_token`，Path=`/api/v1/auth`，`HttpOnly` + `SameSite=Lax`；HTTP IP 方案 `COOKIE_SECURE=false`
5. `Refresh`：按 hash 查 token；已撤销或过期则整 `family_id` 未撤销行全部 `revoked_at`（重放检测）
6. 轮换：旧 token `revoked_at` + `replaced_by`，同 family 发新 raw；`RowsAffected != 1` 当凭证无效
7. `Revoke` / `logout`：只撤当前 hash，清 Cookie；无 Cookie 也返回 204

调用约定（登录、注册走 `issueSession` 并**每次新建** `family_id`；刷新沿用原 family。这是会话，不是用户资料）：

- `POST /auth/register` → `Handler.register` → `Service.Register`：用户名 3–32、密码 ≥8，邮箱可空但非空必须含 `@`；默认角色 `USER`、状态 `ACTIVE`；冲突 409 `ACCOUNT_EXISTS`
- `POST /auth/login` → `Service.Login`：`identifier` 按小写匹配用户名或邮箱 + `bcrypt.CompareHashAndPassword`；非 `ACTIVE` 与密码错误一律 401 `INVALID_CREDENTIALS`；写 `last_login_at`
- `POST /auth/refresh` → `Service.Refresh`：只读 Cookie，不接受 body token；失败清 Cookie，401 `INVALID_REFRESH_TOKEN`
- `POST /auth/logout` → `Service.Revoke`
- `GET /auth/me` 与业务路由：`Handler.Authenticate` 解析 `Bearer`，用户必须仍是 `ACTIVE`；`RequireRole("ADMIN")` 只挂了 `/admin/access-check`
- `ResolveUser`：可选解析 Bearer，给未发布商品详情/资产用，失败不当 401

前端约定：`apps/web/src/api.ts` 的 `request()` 带 `credentials: 'include'`；401 时 `refreshSession()` 单飞 POST `/api/v1/auth/refresh`；`AuthProvider` 启动调 `/auth/me` 恢复会话。

验收：

- 注册 201 / 登录 200 返回 `access_token` + `expires_in`，并 Set-Cookie `refresh_token`
- 刷新轮换 Cookie；重放旧 refresh 会使整族失效
- 退出 204，再 refresh 失败
- `JWT_SECRET` 少于 32 字符时 API 拒绝启动（`auth.New`）

本步核对缺口（未改代码）：

- 登录每次新 `family_id`，旧设备 refresh 不会被这次登录踢掉
- `last_login_at` 的 `Update` 错误被忽略
- Cookie Path 仅 `/api/v1/auth`，业务请求不带 refresh（依赖前端主动 refresh）

本步**不**做 OAuth、邮箱验证或管理业务；RBAC 只预置 `USER`/`ADMIN` 种子，管理后台与审计已在后续阶段完成。

代码：

- `apps/api/internal/auth/auth.go`：`New`、`RegisterRoutes`、`Authenticate`、`RequireRole`、`Register`、`Login`、`Refresh`、`Revoke`、`issueSession`
- `apps/api/cmd/api/main.go`：装配 JWT 配置并 `authHandler.RegisterRoutes(api)`
- `apps/web/src/api.ts`：`request` / `refreshSession` / `setAccessToken`
- `apps/web/src/main.tsx`：`AuthProvider`、`AuthPage`、`Protected`
- 测试：`apps/api/internal/auth/auth_test.go`

### 第 1 步：商品模型与状态机

新增 `apps/api/internal/catalog`，GORM `AutoMigrate(&Product{}, &ProductAsset{})`。状态只允许 `DRAFT → PUBLISHED → OFF_SHELF`，并支持已下架商品重新上架；禁止第三处改状态。

抽出 `validTransition(from, to)` + `transitionProduct`：

1. `create`：登录用户建草稿；可选 `generation_job_id`，成功则 `attachGenerationModel` 复制 GLB；绑定失败回滚删除草稿。`validateRequest`：标题 2–120 字、描述非空、价格 1–100,000,000 分、IP/分类/成色必填、库存 ≥0；交易类型空则 `SALE`，仅 `SALE|PREORDER|EXCHANGE`
2. `list`：只返回 `PUBLISHED`；`keyword` 匹配标题或描述；另支持 `ip_name`、`category`、`condition`、`transaction_type`、`min_price_cents`/`max_price_cents`；分页默认 20、最大 100
3. `listMine`：按 `seller_id` 看自己的全部状态
4. `get`：已发布公开；未发布仅卖家可见（`ResolveUser` 可选鉴权），外人 404 `PRODUCT_NOT_FOUND`
5. `update`：`ownedProduct`；**已下架不能编辑**；草稿和已发布可改
6. `delete`：仅草稿，先 `deleteBoundAssets` 再删行
7. `publish`：至少 1 张图片，再 `DRAFT → PUBLISHED`；无图 400 `ASSET_REQUIRED`
8. `offShelf`：`PUBLISHED → OFF_SHELF`；发布接口也支持 `OFF_SHELF → PUBLISHED`
9. `transitionProduct`：内存校验 + `WHERE id AND seller_id AND status=from`，`RowsAffected != 1` → 409「商品状态已发生变化」

调用约定：

- 读接口公开：`GET /products`、`GET /products/:id`、`GET /products/:id/assets/:asset_id/content`
- 写接口一律 `Authenticate`：`POST/PUT/DELETE /products...`、`POST /:id/{publish,off-shelf,images,model}`
- `ownedProduct`：不存在 404；非卖家 403 `FORBIDDEN`
- `getContent`：未发布商品对外统一 **404**；卖家可读取自己的资产
- 前端 `Market` 搜标题；`Publish` 创建后上传再 `POST /publish`；`/sell?job=` 创建时带 `generation_job_id`；`MyListings` 走 `/products/mine`

验收：

- 未登录不能创建；非卖家不能改别人的商品
- 无图不能发布；已发布不能直接删
- 下架后市场列表不可见，卖家「我的发布」仍可见

本步核对缺口（未改代码）：

- 已发布商品仍可 `update` 改价/改库存，收藏侧靠快照标 `UPDATED`
- `Stock` 默认 1，`validateRequest` 允许 0
- 已修：支持 `OFF_SHELF → PUBLISHED` 重新上架，未发布商品资产内容对外统一返回 404

本步**不**做真实库存扣减、订单绑定、推荐排序。库存扣减见第 5.1 步。

代码：

- `apps/api/internal/catalog/catalog.go`：`RegisterRoutes`、`create`、`attachGenerationModel`、`list`、`listMine`、`get`、`update`、`delete`、`publish`、`offShelf`、`ownedProduct`、`validateRequest`、`validTransition`
- `apps/web/src/main.tsx`：`Market`、`ProductDetail`、`Publish`、`MyListings`（`/me` 内链到我的发布）
- 测试：`apps/api/internal/catalog/catalog_test.go`

### 第 2 步：图片 / GLB 资产

不把文件塞进商品表。统一走 `asset.Service.Put`：嗅探文件头 → MinIO → `assets` 元数据 → `product_assets` 绑定。

抽出 `inspectFile` + `asset.Service.Put` + `catalog.abortUploadError`：

1. `Put` 先 `EnsureBucket`，再读 512 字节文件头交给 `inspectFile`
2. `sniff`：JPEG `FF D8 FF` / PNG 签名 / WEBP `RIFF….WEBP` / GLB `glTF`；对不上 → `ErrUnsupportedType`
3. `expectedKind` 与嗅探 kind 不一致 → `ErrKindMismatch`；扩展名、声明 MIME（忽略 `application/octet-stream`）必须一致
4. 图片 ≤ `MaxImageBytes`（10MB），GLB ≤ `MaxModelBytes`（20MB），否则 `ErrFileTooLarge`
5. object key 固定 `products/{ownerID}/{productID}/{assetID}.{ext}`（jpeg 落盘为 `jpg`）；生成任务把 `productID` 填成 `job.ID`
6. 先 MinIO `store.Put`，再写 `assets` 行 `READY`；元数据写入失败则 `store.Delete` 补偿。`Put` 走 `s.db`，**不**加入调用方事务
7. `catalog.upload`：`ownedProduct`；已下架拒传；图片最多 6；模型最多 1，再传则 `removeAsset` 替换旧模型
8. `readUpload` 用 `MaxBytesReader(maxBytes+512)` + `LimitReader`；失败走 `abortUploadError`

调用约定：

- `POST /products/:id/images` → `KindImage`；首张图写入 `cover_asset_id`
- `POST /products/:id/model` → `KindModel`（GLB）并更新 `model_asset_id`
- `GET /products/:id/assets/:asset_id/content` 给查看器和封面 `<img>`
- `abortUploadError`：`ErrInvalidFile`/`ErrUnsupportedType`/`ErrKindMismatch` → 400 `INVALID_FILE`；`ErrFileTooLarge` → 413 `FILE_TOO_LARGE`；其余 500
- 生成完成也复用 `Put`（见第 6 步），但 Worker `complete` **不**走 `abortUploadError`，无效 GLB 映射为 400 `INVALID_ARGUMENT`
- `asset.Service.Copy`：校验源资产属主 + `READY` + `MODEL`，读对象后写入新 key `products/{owner}/{productID}/{newID}.glb`，元数据失败则删对象。不复用同一 object key，删除生成结果不影响商品模型

验收：

- 改扩展名的假 GLB / 超大文件被拒
- 发布页可传图和模型；详情页能读 content URL
- MinIO 健康检查用 `/minio/health/live`（`docker-compose.yml`）

本步核对缺口（未改代码）：

- `Public()` 的 `content_url` 永远是商品路径；生成结果靠 `ToResponse` 改写成 `/generation-jobs/:id/outputs/:assetId/content`
- `Put` 在调用方事务外提交：`Complete` 若后续写 `generation_outputs` 失败，MinIO 对象和 `assets` 行会残留
- `upload` 更新封面/模型字段时未检查 `Updates` 错误
- 已修：`asset.Copy` + `POST /products` 的 `generation_job_id` 一键带入

本步**不**做转码、缩略图、CDN。内容始终经 API 读出，不把 MinIO 端口暴露公网。

代码：

- `apps/api/internal/asset/validate.go`：`inspectFile`、`sniff`、`objectKey`、`MaxImageBytes`/`MaxModelBytes`
- `apps/api/internal/asset/asset.go`：`Service.Put` / `Copy` / `Get` / `Open` / `Delete` / `Public`
- `apps/api/internal/asset/store.go`：MinIO `EnsureBucket` / `Put` / `Get`
- `apps/api/internal/catalog/catalog.go`：`upload`、`uploadImage`、`uploadModel`、`readUpload`、`abortUploadError`、`removeAsset`
- 测试：`apps/api/internal/asset/validate_test.go`、`apps/api/internal/asset/copy_test.go`、`apps/api/internal/catalog/catalog_upload_test.go`

### 第 3 步：网页 GLB 查看器

前端独立组件，不把 Three.js 打进首屏。`main.tsx` 里 `lazy(() => import('./ModelViewer'))`。

抽出 `ModelViewer`：

1. `requestBlob` 拉 GLB；声明 `size_bytes` 或实际 blob 超过 `MAX_MODEL_BYTES`（20MB）直接失败，不进 Canvas
2. `FittedModel`：`useGLTF` 加载后 `scene.clone(true)`，按包围盒缩放到最长边 1.7 并居中；0 mesh 抛 `empty model`
3. `CameraRig` + `OrbitControls`：阻尼旋转/缩放，距离 0.7–8；`RotateCcw` 重置到 `(0, 0.45, 2.5)`
4. `useProgress` 显示加载进度；下载阶段另有「正在准备 3D 预览」
5. `ViewerErrorBoundary` 解析失败 → 文案「模型无法解析，已切换为图片预览」，用 `fallbackImages[0]`
6. 卸载时 `URL.revokeObjectURL`、`disposeObject`、`useGLTF.clear`
7. 全屏：`shellRef.requestFullscreen`；Nginx `Permissions-Policy` 允许 `fullscreen=(self)`，API 头未放行 fullscreen

接入点：`main.tsx` 顶部 `lazy(() => import('./ModelViewer'))`；商品详情、发布页预览、工作台生成结果。同源 `content_url`，不直连 MinIO。

验收：

- 正常 GLB 可旋转缩放、重置、全屏
- 坏模型 / 超限显示错误并回退封面图
- 首屏不强制加载 R3F

本步核对缺口（未改代码）：

- 无封面图时降级只剩占位文案
- 超限前端与 API complete 均拦 20MB；商品上传与生成回写共用 `asset.MaxModelBytes`

本步**不**做材质编辑、动画、AR。

代码：

- `apps/web/src/ModelViewer.tsx`：`ModelViewer`、`FittedModel`、`ViewerErrorBoundary`
- `apps/web/src/api.ts`：`MAX_MODEL_BYTES`、`requestBlob`、`assetSrc`
- `apps/web/src/main.tsx`：`ProductDetail` / `Publish` / `GenerationWorkspace` 按需加载

### 第 4 步：收藏与个人中心

新增 `apps/api/internal/account`，`AutoMigrate` 资料/地址/偏好/收藏/通知/动态。收藏状态是**读时计算**，不另开后台任务。

抽出 `favoriteStatusOf(item, product, found)`：

1. 商品不存在 → `INVALID` /「商品已失效」
2. 非 `PUBLISHED` → `UNAVAILABLE` /「商品已下架或不可购买」
3. 标题或价格与快照不同 → `UPDATED` /「商品信息已更新」（分类/IP 变化不算）
4. 否则 `ACTIVE` /「收藏有效」
5. `addFavorite`：仅已发布商品；分类空则用偏好默认文件夹，最长 40 字；备注截断 200；写快照标题/价格/状态/分类/IP；同用户同商品已存在 → 409 `ALREADY_FAVORITED`
6. `ackFavorite`：写 `change_notified_at`；商品仍在则把快照同步成当前值（含已下架）
7. `batchDeleteFavorites`：一次最多 50 条，只删当前用户的 id
8. 资料/地址/偏好/通知/动态走 `/me/*`；缺记录时 `ensureProfile` / `ensurePreference` 懒创建

调用约定：

- `GET/POST /favorites`、`PATCH/DELETE /favorites/:id`、`POST /favorites/batch-delete`、`POST /favorites/:id/ack`
- `GET /favorites/folders`、`GET /favorites/status?product_id=`
- `GET/PUT /me/profile`、`/me/addresses`、`/me/preferences`、`/me/notifications`、`/me/activities`
- 前端 `/favorites` → `FavoritesPage`；`/me` → `ProfilePage`（含地址、通知、偏好、历史、我的发布入口）

验收：

- 可收藏/取消/改分类/批量删
- 商品改价或下架后收藏列表出现「已更新 / 不可购买」
- 个人中心能改资料和默认地址；通知可单条/全部已读

本步核对缺口（未改代码）：

- 已发布商品可直接改价，收藏只在下次列表读取时变成 `UPDATED`
- `ack` 已下架商品会把快照状态改成下架，之后显示 `UNAVAILABLE` 而不再是 `UPDATED`
- 批量删除不校验每条是否存在，返回实际 `deleted` 数

本步**不**做社交关注、真实物流地址校验。`GET /api/v1/me` 不存在，聚合入口是 `/me/profile`。

代码：

- `apps/api/internal/account/handler.go`：`RegisterRoutes`、`ensureProfile`、`recordActivity`、`notify`
- `apps/api/internal/account/favorites.go`：`favoriteStatusOf`、`listFavorites`、`addFavorite`、`updateFavorite`、`removeFavorite`、`batchDeleteFavorites`、`ackFavorite`
- `apps/api/internal/account/profile.go`：`getProfile`、`updateProfile`、地址/偏好/通知/动态
- `apps/api/internal/account/models.go`：状态常量与表结构
- `apps/web/src/accountPages.tsx`：`FavoritesPage`、`ProfilePage`
- 测试：`apps/api/internal/account/account_test.go`

### 第 5 步：交易沙盒（虚拟资金，不是订单）

本步允许买入/卖出同一套 `placeSandboxOrder`，靠 `side` 分支；这是模拟成交，不是待支付订单。

抽出 `placeSandboxOrder`：

1. 必须 `risk_acknowledged=true`，否则 400 `RISK_NOT_ACKNOWLEDGED`
2. `side` 仅 `BUY` / `SELL`，数量 1–99
3. 商品必须 `PUBLISHED`，否则 409 `PRODUCT_UNAVAILABLE`
4. `SellerID == 当前用户` 且买入 → 409 `SELF_TRADE_FORBIDDEN`；卖出自己的商品不拦
5. 买入：扣 `SandboxAccount.CashCents`，加权平均更新 `SandboxHolding`；余额不足写 `REJECTED` 订单后 409 `ORDER_REJECTED`，不改持仓
6. 卖出：无持仓或数量不足同样记拒绝单；成功加现金、减持仓，数量到 0 则删持仓行
7. **不扣商品库存**，与第 5.1 步模拟订单独立；成交价取当前标价
8. `resetSandbox`：body `{ confirm: true }`；现金回到 `SandboxStartingCashCents`（10,000,000 分 = 100,000 元），持仓清空，`generation+1`、`reset_count+1`；历史 `SandboxOrder` 保留。不改 `trade_orders` 里已扣的资金

调用约定：

- `GET /sandbox` 返回账户、持仓市值/浮盈、最近 50 条成交、风险文案
- `POST /sandbox/orders` 立即成交 201 或拒绝 409
- `POST /sandbox/reset` 成功后直接返回 `getSandbox` 的 JSON
- 前端 `/sandbox` → `SandboxPage`；详情页可带 `?product=` 预填
- 无账户时 `ensureSandbox` / 下单事务内按起始资金懒创建

验收：

- 新用户打开沙盒即有 10,000,000 分虚拟资金
- 不能买自己的商品
- 未勾选风险提示不能下单
- 重置后现金/持仓回初始，成交记录仍在

本步核对缺口（未改代码）：

- 沙盒成交不占库存、不生成 `trade_orders`，同一商品可同时被沙盒「买」和模拟下单
- 拒绝单也会入库，列表里能看到 `REJECTED`
- 重置只动沙盒账户，已支付未完成的模拟订单资金不会被冲正

本步**不**把「待支付订单、模拟支付、发货、确认收货」并列为本步终态。那些见第 5.1 步。

代码：

- `apps/api/internal/account/sandbox.go`：`getSandbox`、`placeSandboxOrder`、`resetSandbox`、`holdingViews`
- `apps/api/internal/account/models.go`：`SandboxStartingCashCents`、`SideBuy`/`SideSell`
- `apps/web/src/accountPages.tsx`：`SandboxPage`
- 测试：`apps/api/internal/account/account_test.go`

### 第 5.1 步：模拟订单闭环（虚拟资金，不是真实支付）

不引入第三方支付。订单状态机单独抽出，HTTP 入口只调 `placeOrder` / `transitionOrder`。资金账户复用 `SandboxAccount`，库存走 `catalog.Product.Stock`。

抽出 `CanTransitionOrder` + `placeOrder` + `transitionOrder`：

状态只允许 `PENDING_PAYMENT → PAID|CANCELED`，`PAID → SHIPPED|CANCELED`，`SHIPPED → COMPLETED`。已发货不能取消。

1. `placeOrder`：`Idempotency-Key` 必须是 UUID；同买家同 key 同 hash 返回原单（HTTP 200），hash 不同则 `IDEMPOTENCY_CONFLICT`
2. 商品必须 `PUBLISHED`；`SellerID == 买家` → `SELF_TRADE_FORBIDDEN`
3. 数量 1–99 且 ≤ 库存；地址必须属于买家，否则当参数无效
4. 事务 `lockFirst`（非 sqlite 用 `FOR UPDATE`）锁商品、扣库存、写 `PENDING_PAYMENT` + `OrderEvent`；金额 = 单价 × 数量，收件信息快照进订单
5. `pay`：买家角色；`debitBuyer` 扣 `SandboxAccount.CashCents`（无账户则按 `SandboxStartingCashCents` 懒创建），`PENDING_PAYMENT → PAID`
6. `cancel`：待支付仅买家可取消并还库存；已支付仅卖家可取消，还库存并 `creditUser` 退款给买家；原因截断 120 字
7. `ship`：卖家填非空模拟运单号（截断 64），`PAID → SHIPPED`
8. `confirm`：买家确认收货，`creditUser` 把金额打给卖家，`SHIPPED → COMPLETED`
9. `authorizeOrderAction` 卡角色；`abortOrderError`：参数 400、冲突类 409、无权 403、不存在 404

调用约定：

- `POST /orders` 必须带 `Idempotency-Key`；首次创建返回 201，幂等命中返回 200
- `GET /orders?role=BUY|SELL`（默认 BUY）可再滤 `status`；`GET /orders/:id` 买卖双方可读
- `POST /orders/:id/{pay,cancel,ship,confirm}`
- 前端 `/checkout` 创建订单；`/orders` 买卖列表；`/orders/:id` 按角色显示按钮
- 详情页「模拟下单」与沙盒即时成交并存，互不替代；资金账户共用 `SandboxAccount`

验收：

- 重复同一 Idempotency-Key 不双建、不双扣库存（`TestSimulatedOrderLifecycle`）
- 不能买自己的商品；库存不足拒绝
- 待支付取消还库存；已支付卖家取消退款（`TestSimulatedOrderCancelRestoresStockAndRefund`）
- 买家不能发货，卖家不能确认收货
- 完成订单后卖家虚拟资金增加、买家减少

本步核对缺口（未改代码）：

- 已修：幂等命中返回 200，首次创建返回 201
- `SHIPPED` 后不能取消；无售后/退货状态
- 支付时才扣虚拟资金，下单只占库存；买家账户在支付时才懒创建
- 沙盒重置会清现金/持仓，**不会**改未完成订单里已扣的资金

本步**不**做真实支付通道、物流轨迹、售后纠纷。沙盒即时成交仍走第 5 步。

代码：

- `apps/api/internal/account/order_status.go`：`CanTransitionOrder` / `ValidateOrderTransition`
- `apps/api/internal/account/orders.go`：`placeOrder`、`transitionOrder`、`debitBuyer`、`creditUser`
- `apps/api/internal/account/models.go`：`Order` / `OrderEvent`
- `apps/api/internal/account/handler.go`：`/orders*` 路由
- `apps/web/src/accountPages.tsx`：`CheckoutPage`、`OrdersPage`、`OrderDetailPage`
- 测试：`TestSimulatedOrderLifecycle`、`TestSimulatedOrderCancelRestoresStockAndRefund`

### 第 6 步：AI 生成 HTTP 闭环

任务状态机在 API，执行在 Worker。创建任务只写库 + outbox，不在 HTTP 里调模型。

状态：`QUEUED → RUNNING → SUCCEEDED|FAILED|CANCELED`；`FAILED → QUEUED` 必须显式 retry。阶段：`QUEUED → OPTIMIZING_PROMPT → SUBMITTING_PROVIDER → GENERATING → FETCHING_OUTPUT → STORING_OUTPUT → COMPLETED`。

抽出 API `generation.Service.Create`：

1. `Idempotency-Key` 必须是 UUID；同用户同 key 同 hash 返回原任务，hash 不同则冲突
2. 每用户最多 `maxActiveJobsPerUser=5` 个 `QUEUED|RUNNING`（创建与重试共用）
3. 事务写入 `GenerationJob`（`QUEUED`）+ `GenerationOutbox`（`generation.job.created`）
4. `StartDispatcher` 每秒 `DispatchOnce`：pending outbox → `RedisPublisher.Publish` 到 stream `generation_jobs`
5. Worker `consume`：`xreadgroup(..., ">")` → `GenerationJobMessage.from_stream` → `JobProcessor.process`；成功或 `ContractError` 才 `xack`，其它异常不 ack
6. `process`：`claim`（已 `SUCCEEDED|CANCELED` 直接返回）→ `PromptOptimizer.optimize` → `provider.submit` → `_wait_for_success` → `fetch_outputs` → `complete` 存 GLB
7. `_wait_for_success`：默认 `PROVIDER_POLL_INTERVAL=8s`、`PROVIDER_POLL_TIMEOUT=900s`；进度映射 `40 + provider_progress*0.3`；provider `CANCELED` 变 `JobCanceled`
8. 空 output content 记 `OUTPUT_INVALID`，不再回退 `minimal_glb()`；`WorkerAPIError` 404/409 记 `SKIPPED` 不调 fail（取消 CONFLICT 除外）
9. 用户侧：`cancel` / `retry`；`StartTimeoutWatcher` 每 10s 调 `FailTimedOut`

Worker 回写约定（均 `lockJob` + 乐观 `version`；取消优先于成功/失败）：

- `Claim`：已请求取消则 `markCanceled`；同 attempt 的 `RUNNING` 幂等返回；`QUEUED` + attempt 匹配才 `QUEUED → RUNNING`，阶段 `OPTIMIZING_PROMPT`、进度 5
- `ReportProgress`：仅 `RUNNING` 且 attempt 匹配；可回写 `optimized_prompt` / `rag_context` / `rag_version` / template / structured prompt；终态直接返回
- `Fail`：`error_code` 必填；消息空则「生成任务失败」，截断 500 字；`QUEUED|RUNNING` → `FAILED`；同 attempt 已失败幂等
- `Complete`：`Body` 为空拒绝；已取消且未成功则取消且**不存文件**；已 `SUCCEEDED` 幂等返回已有 outputs；必须 `RUNNING` + attempt 匹配；`assets.Put(..., KindModel)` 后写 `generation_outputs`（`MODEL`/`glb`），任务 `SUCCEEDED` / `COMPLETED` / 进度 100，清空 error
- HTTP `POST /internal/generation-jobs/:id/complete`：multipart `file` + `attempt` + 可选 `provider_job_id`；`MaxBytesReader` + `LimitReader` 卡 `asset.MaxModelBytes`（20MB）；缺文件/attempt → 400；超限 → 413 `FILE_TOO_LARGE`；无效 GLB → 400 `INVALID_ARGUMENT`；需 `X-Worker-Token`
- `Cancel`：非本人当不存在；已取消幂等；`SUCCEEDED|FAILED` 拒绝；`QUEUED` 立即 `markCanceled`（`GENERATION_CANCELED`）；`RUNNING` 只写 `cancel_requested_at`，等 Worker 下次 claim/progress/fail/complete 落地
- `Retry`：仅 `FAILED` 且 `attempt < max_attempts`；再入队并写新 outbox；不清除旧 outputs / 优化 prompt / `cancel_requested_at`
- `FailTimedOut`：默认超时 15 分钟（`GENERATION_JOB_TIMEOUT`，与 Worker `PROVIDER_POLL_TIMEOUT=900s` 对齐）；`QUEUED.created_at` 或 `RUNNING.started_at` 超过 cutoff 则调 `Fail(GENERATION_TIMEOUT)`。按 `started_at` 而不是最近进度时间
- `markCanceled`：走 `ValidateTransition`；保持当前 `stage`，写 `finished_at`

Stream 契约：`StreamName=generation_jobs`，`ConsumerGroupName=ai-workers`，`JobCreatedEvent=generation.job.created`，字段见 `StreamMessage.Fields()`。

Prompt 优化约定（LLM 失败必须同一条 `optimize()` 回退，禁止第三处拼词）：

- `TerminologyIndex.search` 读 `rag/terminology/data/terms.jsonl`
- 有 `LLM_BASE_URL` + `LLM_API_KEY` 走 `_llm_optimize`；否则或异常走 `_fallback`
- 进度 extra 回写 `optimized_prompt` / `rag_context` / `rag_version`
- 容器内路径：`default_terms_path` 先环境变量，再 `/app/rag/...`，`parents` 不足不越界

Provider 约定：

- `create_provider()`：`mock`（默认）/ `http` / `hy3d`
- `Hy3DProvider.submit` → TokenHub `v1/api/3d/submit`；`get_status` 轮询；不要把官方基址填进通用 `PROVIDER_BASE_URL`
- 生产仍是 mock，直到 `.env` 填 `TOKENHUB_API_KEY` 并设 `GENERATION_PROVIDER=hy3d`

前端：`GenerationWorkspace` 提交带 `Idempotency-Key`，`provider` 写死 `hy3d`，`copyright_confirmed: true`；每 2 秒轮询未终态任务；`QUEUED|RUNNING` 显示取消，`FAILED && attempt < max_attempts` 显示重试；成功用 `ModelViewer` 预览，并提供「带入发布」跳到 `/sell?job=&prompt=&productType=`。布局 class：`generation-layout` / `job-list` / `job-item`。

验收：

- 创建任务立刻 `QUEUED`，随后 Worker `RUNNING`，成功可下 GLB（`TestWorkerCompletesJobWithGLB`）
- 重复同一 Idempotency-Key 不双建
- 排队中取消立即 `CANCELED`（`TestCancelQueuedJob`）；运行中取消靠 Worker 回写落地
- 失败可 retry（`TestRetryFailedJob`，202 + attempt+1）；超时变 `FAILED` / `GENERATION_TIMEOUT`（`TestFailTimedOutJobs`）
- 工作台能看到优化后的 prompt（有 LLM 或术语库回退）

本步核对缺口（已转入后续事项）：

- 已修：Worker 使用 `XAUTOCLAIM` 回收空闲 PEL，失败回调异常不 ACK。
- 已修：`Complete` 事务失败后补偿删除已写入的资产记录和对象；生成正确性、20MB 限制、一键发布均已完成。
- `Retry` 不删旧 outputs；`CancelRequestedAt` 重试时不重置（失败任务通常也没有该字段），并入事项 4、6 的一致性测试与恢复验证。
- 当前每用户活跃任务上限为 5，与实现一致。
- 用户确认优化结果已由事项 2 的预览与创建契约覆盖；管理员能力见事项 3。

本步**不**做管理后台、审计日志；相关能力已在阶段 6 后续步骤完成。

代码：

- `apps/api/internal/generation/handler.go`：用户路由 + `/internal/generation-jobs/:id/{claim,progress,fail,complete}`
- `apps/api/internal/generation/service.go`：`Create`
- `apps/api/internal/generation/worker.go`：`Claim`、`ReportProgress`、`Fail`、`Complete`、`Cancel`、`Retry`、`FailTimedOut`、`markCanceled`、`lockJob`
- `apps/api/internal/generation/dispatcher.go` / `publisher.go`：outbox → Redis Streams
- `apps/api/internal/generation/status.go`：`CanTransition` / `ValidateTransition`
- `apps/api/internal/generation/contract.go`：`StreamName=generation_jobs`
- `apps/worker/main.py`：`consume`、`ensure_group`
- `apps/worker/processor.py`：`JobProcessor.process`
- `apps/worker/promptopt/optimizer.py`：`PromptOptimizer.optimize`
- `apps/worker/promptopt/knowledge.py`：`TerminologyIndex`、`default_terms_path`
- `apps/worker/providers/factory.py`、`mock.py`、`http.py`、`hy3d.py`
- `apps/worker/Dockerfile`：拷贝 `rag/terminology` 并设 `RAG_*_PATH`
- `apps/worker/PROVIDER.md`：接入说明
- `apps/web/src/main.tsx`：`GenerationWorkspace`
- 测试：`apps/api/internal/generation/*_test.go`，`apps/worker/tests/test_*.py`

### 第 7 步：公网 IP 同源入口与运行环境

不引入域名/备案/HTTPS。Web 容器占 80，Nginx 反代 API；内部端口绑 `127.0.0.1`。

抽出入口：

1. `apps/web/nginx.conf`：`listen 80 default_server`；`/api/`、`/healthz`、`/readyz` → `api:8080`；`client_max_body_size 50m`；安全头含 `fullscreen=(self)`；前端走 `try_files ... /index.html`
2. 前端 `VITE_API_BASE_URL` 为空，请求走相对 `/api`
3. `docker-compose.yml`：MySQL/Redis/MinIO 发布 `127.0.0.1:*`；API/Worker 仅 `expose`
4. API `securityHeaders()`：`nosniff` / `DENY` / `strict-origin-when-cross-origin` / `camera=(), microphone=(), geolocation=()`（**不含** fullscreen、CSP、HSTS）
5. CORS：`AllowCredentials=true`，默认源 `http://localhost:5173`，允许头含 `Authorization`、`Idempotency-Key`
6. `/healthz` 恒 200；`/readyz` 2 秒内 ping MySQL/Redis/MinIO，任一失败 503；`/api/v1/version` 当前 `0.3.0`
7. 阶段 2：`.deploy/setup_phase2.py` 配 `vm.overcommit_memory=1`、Docker 日志 `json-file 10m×3`、`firewalld` 仅 ssh/http、chronyd、`/opt/backup`、磁盘告警
8. 部署：`.deploy/sync_and_deploy.py` 备份 → 上传 `apps/*` 与 `rag/terminology` → `docker compose build/up` → 健康检查与登录 Cookie 验收

验收：

- `http://8.154.28.98/` 前端 200
- 公网 `/healthz` `/readyz` `/api/v1/version` 为 API JSON
- Refresh Cookie 在 HTTP IP 下可发送（无 `Secure`，Path=`/api/v1/auth`）
- 六容器 `healthy`；`:3306/:6379/:9000/:8080/:8000` 不对公网开放

本步核对缺口（未改代码）：

- Nginx 上传上限 50m，商品图/模型分别 10m/20m，Worker complete 与商品模型同为 20MB
- API `Permissions-Policy` 未放行 fullscreen，全屏预览依赖浏览器对页面自身策略；页面响应头以 Nginx 为准
- 无 CSP / HSTS（本步明确不做 HTTPS）

本步**不**做 TLS、域名、自动备份恢复演练（阶段 7 仍未完成）。

代码：

- `apps/web/nginx.conf`
- `apps/api/cmd/api/main.go`：`securityHeaders`、`readyHandler`
- `docker-compose.yml`
- `.deploy/setup_phase2.py`、`.deploy/check_phase2.py`、`.deploy/sync_and_deploy.py`
- 凭据只在被忽略的 `.deploy/server.env` / 远端 `.env`，不入库

### 已完成清单（对照）

- [x] Go API 基础服务和 Web 前端基础页面
- [x] 用户注册、登录、退出和会话恢复
- [x] bcrypt 密码哈希
- [x] JWT Access Token
- [x] HttpOnly Refresh Cookie
- [x] Refresh Token 轮换、撤销和重放检测
- [x] `USER` / `ADMIN` 基础 RBAC
- [x] 认证接口和前端登录态接入
- [x] 本地 Go 测试、前端构建、Lint 和 diff 检查
- [x] 用户手动安装远程 CLI/SSH 工具
- [x] 远程服务器 Docker Compose 环境和生产配置已就绪
- [x] MySQL、Redis、MinIO、API、Worker、Web 六个容器均通过健康检查
- [x] MinIO 健康检查改用 `/minio/health/live`
- [x] Go 与 Python 镜像构建网络配置已修复并同步到仓库
- [x] Web 容器 Nginx 已完成 Web 与 `/api/` 同源反向代理，支持公网 IP 直接访问
- [x] 商品模型、迁移、CRUD、搜索筛选、所有权校验和状态流转已完成
- [x] 商品输入边界与状态机测试已通过
- [x] 商品图片/GLB 上传、MinIO 资产元数据、文件校验和内容读取已完成
- [x] 发布商品页、市场列表、商品详情和我的发布已接入资产
- [x] 商品详情和发布页已接入 GLB 3D 查看器（旋转、缩放、重置、全屏、进度、错误降级）
- [x] API 与 Web 已配置基础安全响应头
- [x] 域名、DNS、备案和 HTTPS 已从当前 MVP 部署范围移除
- [x] 阶段 2 CentOS/Alibaba Cloud Linux 运行环境基线已完成
- [x] 收藏、个人中心、交易沙盒已部署公网
- [x] 模拟订单闭环（待支付、支付、取消、发货、确认收货、库存与幂等）
- [x] AI 生成最小 HTTP 闭环（含 RAG/LLM 优化与 HY-3D 适配器代码）

## 二、阶段 1：确认远程 CLI 与服务器连接

### 目标

确认当前 Windows 工作区可以通过已安装的 CLI 访问个人 CentOS 服务器，并建立可重复执行的非交互式远程操作方式。

### 待办

- [x] 在本机新 PowerShell 会话中确认 CLI 命令已加入 `PATH`
- [x] 查看 CLI 的版本和帮助信息
- [x] 使用用户已有的 SSH 配置或密钥测试连接
- [x] 确认远端系统版本、CPU 架构、磁盘、内存和可用端口
- [x] 确认远端部署用户具备 Docker 操作权限
- [x] 确认远端项目目录为 `/opt/aigc-3d-platform`
- [x] 记录连接方式和部署目录，但不记录密码、私钥或 Token

### 验收标准

- 可以执行远程命令并返回服务器信息
- 不需要在仓库中保存任何敏感凭据
- 后续部署命令可以重复执行，且失败时能定位具体步骤

## 三、阶段 2：配置 CentOS 运行环境

### 目标

为 Docker Compose 部署准备稳定、安全的服务器基础环境。

### 待办

- [x] 安装并启用 Docker Engine
- [x] 安装 Docker Compose Plugin、Buildx、Git、Curl、OpenSSL 等工具
- [x] 将部署用户加入 `docker` 用户组并重新登录确认
- [x] 配置 Docker 开机启动
- [x] 检查 Docker Hub 或镜像源访问能力
- [x] 配置 Redis 所需的 `vm.overcommit_memory=1`
- [x] 配置 Docker 容器日志轮转，避免磁盘被日志占满
- [x] 配置 `firewalld`，仅开放 SSH 和 HTTP 80
- [x] 不向公网开放 MySQL、Redis 和 MinIO 内部管理端口
- [x] 创建项目部署目录和备份目录
- [x] 配置服务器时间同步、磁盘空间告警和基础资源检查

### 验收标准

```text
Docker 可用
Docker Compose 可用
部署用户无需 sudo 即可执行 docker 命令
必要基础镜像可以正常拉取
公网仅暴露必要服务端口
```

## 四、阶段 3：生产环境配置与密钥管理

### 目标

生成独立于本地开发环境的生产配置，避免使用默认凭据或弱密钥。

### 待办

- [x] 从 `.env.example` 创建远端生产环境 `.env`
- [x] 设置 `APP_ENV=production`
- [x] 生成高强度 `JWT_SECRET`
- [x] 设置唯一的 `JWT_ISSUER`
- [x] 配置生产数据库名、用户名和随机密码
- [x] 配置 Redis 网络隔离策略
- [x] 配置 MinIO 管理账号和随机密码
- [x] 配置前端 API 为同源 `/api`，无需写死域名或服务器 IP
- [x] 配置公网 IP + `WEB_PORT` 统一访问入口
- [x] HTTP IP 方案保持 `COOKIE_SECURE=false`；正式启用 HTTPS 时再切换为 `true`
- [x] 限制远端 `.env` 文件权限和所属组
- [x] 确认生产环境没有使用开发默认 JWT 密钥
- [x] 确认敏感配置不会被提交到 Git 或写入日志

### 验收标准

- 生产环境缺少 `JWT_SECRET` 时 API 会拒绝启动
- 所有数据库、对象存储和 JWT 凭据均为独立随机值
- `.env` 不出现在版本控制提交中

## 五、阶段 4：上传、启动与健康检查

### 目标

将当前项目部署到个人 CentOS 服务器，并确认所有容器能够稳定运行。

### 待办

- [x] 将当前部署版本同步到远端项目目录
- [x] 在远端执行 Compose 配置校验
- [x] 拉取或构建 API、Web、Worker 和基础设施镜像
- [x] 启动 MySQL、Redis、MinIO、API、Worker 和 Web
- [x] 检查容器状态和启动日志，六个服务均为 `healthy`
- [x] 检查 `/healthz`，API 返回 `200 OK`
- [x] 检查 `/readyz`
- [x] 检查 `/api/v1/version`
- [x] 检查 Web 页面、`/healthz` 和 `/api/` 通过公网 IP 入口返回预期结果
- [x] 验证注册、登录、刷新会话和退出登录
- [x] 验证服务重启后数据库、Redis 和对象存储数据仍然存在
- [x] 将本地新增的商品 API 版本重新部署到远端

### 重要说明

当前本地曾遇到 Docker Hub 拉取 `minio/minio:latest` 时出现 EOF。远端部署时需要先单独验证镜像仓库网络；如果仍失败，应优先处理服务器出口网络、代理或镜像源，不要直接修改业务代码绕过问题。

## 六、阶段 5：公网 IP 与同源反向代理

### 目标

仅通过服务器公网 IP 和 Web 端口提供 Web/API 同源入口，不依赖域名、DNS、备案或 TLS 证书；内部服务不直接暴露到公网。

### 待办

- [x] Web 容器 Nginx 接受任意 IP Host
- [x] Web 站点由 `WEB_PORT` 暴露统一入口
- [x] `/api/`、`/healthz` 和 `/readyz` 反向代理到 Go API
- [x] 前端默认使用同源相对 API 地址
- [x] 移除页面备案号和域名专属配置
- [x] 配置上传请求体大小限制为 `50m`
- [x] MySQL、Redis 和 MinIO 仅绑定 `127.0.0.1`
- [x] API 与 Worker 仅在 Compose 内部网络暴露
- [x] 远端设置 `WEB_PORT=80`，并仅开放 SSH 与 TCP 80
- [x] 配置安全响应头（API 中间件与 Web Nginx 已写入仓库；公网入口已部署验收）
- [x] 重新验证公网 IP 下的登录 Cookie 和刷新令牌

### 验收标准

- 用户通过 `http://<服务器公网IP>` 访问 Web
- API 和 Web 使用同一 IP、端口和 `/api/` 路径
- Refresh Cookie 在 HTTP IP 入口下能够发送且不会被浏览器拦截
- MySQL、Redis、MinIO、API 和 Worker 内部端口不对公网开放

## 七、阶段 6：核心业务 P0 开发

已勾选的实现步骤、方法与代码位置见上文「一、当前已完成事项」。此处只保留已完成能力的对照清单；后续工作统一见第十一节。

### 商品与资产

- [x] 商品模型、GORM 自动迁移和 CRUD API → 第 1 步 `catalog.RegisterRoutes` / `create` / `list`
- [x] 商品列表、分页、关键词搜索及 IP、分类、成色、交易类型、价格筛选 → `list` 的 query 过滤
- [x] 商品详情和 `DRAFT → PUBLISHED → OFF_SHELF` 状态流转 → `validTransition` / `publish` / `offShelf`
- [x] 商品图片上传和 GLB 上传 → 第 2 步 `uploadImage` / `uploadModel`
- [x] MinIO/S3 资产元数据记录 → `asset.Service.Put`
- [x] 文件类型、MIME、文件头、大小和归属校验 → `inspectFile` / `sniff`
- [x] 商品编辑、草稿删除、发布、下架和所有权校验 → `ownedProduct` / `update` / `delete`
- [x] 商品输入校验和状态机单元测试 → `catalog_test.go`

### 3D 展示

- [x] Three.js/React Three Fiber GLB 查看器 → 第 3 步 `ModelViewer`
- [x] 旋转、缩放、重置视角和全屏 → `OrbitControls` + 重置/全屏按钮
- [x] 加载进度、错误提示和图片降级 → `useProgress` / `ViewerErrorBoundary`
- [x] 20 MB GLB 大小限制和异常模型处理 → `MAX_MODEL_BYTES` / 0 mesh 检测

### 收藏与个人中心

- [x] 收藏与取消收藏 → 第 4 步 `addFavorite` / `removeFavorite`
- [x] 我的发布 → `GET /products/mine` + `MyListings`
- [x] 我的收藏（分类筛选、批量管理、更新/失效提示） → `favoriteStatusOf` / `batchDeleteFavorites` / `ackFavorite`
- [x] 基础用户资料 → `getProfile` / `updateProfile`
- [x] 收货地址管理 → `listAddresses` / `saveAddress` / `deleteAddress`
- [x] 消息通知、偏好设置、浏览与操作历史 → `/me/notifications` `/me/preferences` `/me/activities`

### 模拟交易

- [x] 交易沙盒：虚拟资金、买入/卖出、成交记录、风险提示与重置 → 第 5 步 `placeSandboxOrder` / `resetSandbox`
- [x] 模拟订单和订单明细 → 第 5.1 步 `placeOrder` / `GET /orders`
- [x] 模拟支付成功 → `POST /orders/:id/pay`
- [x] 待支付订单取消 → 买家 `cancel` 还库存
- [x] 卖家模拟发货 → `POST /orders/:id/ship`
- [x] 买家确认收货 → `POST /orders/:id/confirm`
- [x] 订单状态机与订单事件 → `CanTransitionOrder` / `OrderEvent`
- [x] 防止购买自己的商品（沙盒与订单均拦截） → `SELF_TRADE_FORBIDDEN`
- [x] 幂等、防重复提交和库存校验（模拟订单侧） → `Idempotency-Key` + `Product.Stock`

### AI 建模工作台

- [x] RAG 知识库和检索流程 → 第 6 步 `TerminologyIndex.search`
- [x] Prompt 优化（LLM 优先，失败则术语库拼接） → `PromptOptimizer.optimize`
- [x] 用户确认或修改优化结果 → `POST /generation-jobs/prompt-preview` + `CreateJobRequest.FinalPrompt` / `CopyrightConfirmed`
- [x] 创建生成任务 HTTP 接口 → `POST /generation-jobs` / `Service.Create`
- [x] Redis Streams 任务入队 → `StartDispatcher` / `RedisPublisher.Publish`
- [x] Python AI Worker 业务执行闭环 → `JobProcessor.process`
- [x] Mock Provider 及契约测试 → `providers/mock.py` + `test_provider_http.py`
- [x] 第三方文本生成 3D Provider HTTP 适配器 → `HttpProvider` / `Hy3DProvider`，说明见 `apps/worker/PROVIDER.md`
- [x] 任务轮询、超时、失败、重试和取消 → `FailTimedOut` / `Retry` / `Cancel`
- [x] 生成结果存储为 GLB 资产 → `Complete` + `asset.Service`
- [x] 从 AI 结果一键带入商品发布 → `asset.Copy` / `ReadyModel` / `POST /products` `generation_job_id`；工作台「带入发布」仍要补图才能 `publish`

### 管理与审计

- [x] 管理员查看用户和商品 → `admin.Handler.users` / `admin.Handler.products` + 前端 `/admin`
- [x] 管理员下架商品 → `admin.Handler.offShelf`，事务锁与 `PRODUCT_OFF_SHELF` 审计
- [x] 查看和重试失败 AI 任务 → `admin.Handler.failedJobs` / `generation.Service.AdminRetryWithAudit`
- [x] 管理操作审计日志 → `admin.AuditLog`、`audit_logs` 迁移与 `/admin/audit-logs`

## 八、阶段 7：发布前验证与运维

本阶段的未完成事项已按依赖和验收顺序集中整理到「十一、未完成事项与推荐开发顺序」，避免与阶段 6 清单重复维护。

阶段 7 对应事项：

- 事项 4：补齐 API 单元测试与关键集成测试
- 事项 5：发布前端到端与失败场景验收
- 事项 6：数据持久化与重启恢复验证
- 事项 7：备份策略与恢复演练
- 事项 8：结构化日志、Request ID 与基础监控
- 事项 9：生产安全与依赖检查
- 事项 10：真实 Provider 配置与联调
- 事项 11：演示数据、测试账号与内测指标
- 事项 12：发布问题清单与 MVP 修复闭环

## 九、建议执行顺序

历史上已经完成的生成正确性、一键发布、生成可靠性和最小管理后台不再列入待办。当前所有未完成事项及其唯一推荐顺序见「十一、未完成事项与推荐开发顺序」中的 P1/P2 表格。

近期执行主线：

1. 商品与订单行为一致性修正（已完成）。
2. 用户确认或修改优化后的 Prompt（已完成）。
3. 最小管理后台与审计日志（已完成）。
4. API 管理权限/跨模块关键测试已完成；下一步进行浏览器 E2E、恢复、安全和运维验收。
5. 真实 Provider 联调、演示数据与内测。
6. MVP 验收后再进入支付、售后、物流、审核和性能扩展。

已发布商品可改价、沙盒不扣库存属于当前产品约定；`SHIPPED` 后取消归入未来售后设计，不作为当前缺陷处理。

## 十、远程操作安全约定

- 不在聊天记录、仓库或脚本中粘贴私钥、密码、数据库密钥和 JWT 密钥。
- 优先使用本机已有 SSH Agent、SSH 配置或 CLI 的安全凭据存储。
- 远程执行前先进行只读检查，再执行安装、配置和部署操作。
- 修改生产配置前保留备份；删除数据卷、重置数据库等破坏性操作必须单独确认。
- 每次部署记录代码版本、配置变更、容器状态和健康检查结果。

## 十一、未完成事项与推荐开发顺序

本节是当前唯一的进度与未完成事项清单：P0 记录已完成的 MVP 基线，P1/P2 记录后续工作。排序原则是：先补齐测试与核心验收，再完成发布验收与运维，最后接入真实支付、审核和生产化扩展。`P0` 为已完成的核心闭环，`P1` 为影响可靠性、验收或小范围内测的事项，`P2` 为试运营和生产化扩展。

### 11.1 P0：核心 MVP 基线（已完成）

| 顺序 | 事项名称 | 简要说明 | 当前状态 | 优先级 / 依赖 |
| --- | --- | --- | --- | --- |
| 1 | 商品与订单行为一致性修正 | 增加 `OFF_SHELF → PUBLISHED` 重新上架；未发布商品的资产内容对外统一返回 404；订单幂等命中返回 200 而非 201。 | **已完成**；已补状态迁移、资产访问边界、幂等 HTTP 状态码及回归测试。 | `P0`；已完成，后续测试与验收以该行为契约为基线。 |
| 2 | 用户确认或修改优化后的 Prompt | 创建生成任务前展示原始 Prompt、RAG 摘要、结构化参数和优化 Prompt，允许编辑并确认；服务端保存确认版本、最终 Prompt 和版权确认。 | **已完成**；API 提供 Prompt 预览，创建任务强制引用未消费预览并提交最终 Prompt 与版权确认，前端工作台已接入确认流程。 | `P0`；已完成，后续仅随测试和发布验收维护。 |
| 3 | 最小管理后台与审计日志 | 管理员查看用户/商品、下架商品、查看及重试失败 AI 任务；记录操作者、目标、动作、前后状态、请求 ID 和时间。 | **已完成**；API 管理路由、RBAC、事务审计、失败任务重试、前端 `/admin` 和 OpenAPI 契约均已落地，后续随发布验收维护。 | `P0`；已完成，依赖 RBAC、商品状态机和生成重试。 |

### 11.2 P1：发布前质量与运维验收

| 顺序 | 事项名称 | 简要说明 | 当前状态 | 优先级 / 依赖 |
| --- | --- | --- | --- | --- |
| 4 | 补齐 API 单元测试与关键集成测试 | 覆盖认证、越权、上传、商品状态、订单幂等/库存、AI 全生命周期、重复回调和管理员权限。 | **部分完成**；核心包已有测试，管理能力、跨模块集成和部分异常路径尚不完整。 | `P1`；依赖事项 1–3 的接口稳定；可提前准备测试矩阵。 |
| 5 | 发布前端到端与失败场景验收 | 执行注册登录、越权、伪造/超限上传、商品发布、订单闭环、AI 失败恢复、页面刷新和错误降级。 | **未完成**；已有单元测试和公网基础验收，尚无统一的浏览器 E2E/黑盒验收记录。 | `P1`；依赖事项 1–4 和可用部署环境；是内测前门槛。 |
| 6 | 数据持久化与重启恢复验证 | 验证 API/Worker/Web 重启后业务数据、对象资产和 Redis Streams 未确认消息保持一致，并检查孤儿资产与重复回调。 | **部分完成**；持久卷和生成可靠性已实现，尚无可重复的恢复验收记录。 | `P1`；依赖事项 4、5；需在真实 Compose 环境执行。 |
| 7 | 备份策略与恢复演练 | 制定 MySQL、对象存储和必要配置的备份保留、校验和恢复步骤，至少完成一次恢复演练。 | **未完成**；已有部署前备份目录和记录，无自动备份策略及恢复验收。 | `P1`；依赖事项 6；恢复演练不得破坏生产数据。 |
| 8 | 结构化日志、Request ID 与基础监控 | 统一 API/Worker 日志字段，串联请求和任务，补充关键指标、容器健康、队列积压与错误告警。 | **部分完成**；API 已有 `X-Request-ID` 和健康检查，结构化日志、Worker 链路和指标告警不完整。 | `P1`；依赖事项 5、6；先覆盖核心链路。 |
| 9 | 生产安全与依赖检查 | 检查公网端口、默认凭据、Cookie/CORS、敏感配置、依赖漏洞、生产构建产物及上传/鉴权边界。 | **部分完成**；公网隔离、JWT 和基础安全头已验收，漏洞扫描及完整发布清单未完成。 | `P1`；依赖部署环境和事项 4–8；公开内测前完成。 |
| 10 | 真实 Provider 配置与联调 | 注入 AI/LLM 密钥，切换 `GENERATION_PROVIDER=hy3d`，验证提交、轮询、下载、超时、限流、余额不足和输出校验。 | **未完成**；适配器代码已完成，当前默认 Mock，密钥和真实服务验收依赖人工。 | `P1`；依赖事项 2、4、5 及供应商账号；若只验收 Mock 可暂缓。 |
| 11 | 演示数据、测试账号与内测指标 | 准备授权素材、GLB、买卖双方及管理员账号，执行目标用户内测，记录路径完成率、阻断缺陷和 3D/AI 使用指标。 | **未完成**；公网环境可访问，尚无完整演示数据和用户测试记录。 | `P1`；依赖事项 3–10 的核心部分；属于 MVP 完成定义。 |
| 12 | 发布问题清单与 MVP 修复闭环 | 汇总测试/内测问题，按严重度分级，修复发布阻断项并重新验收，形成版本基线。 | **未完成**；当前只有开发缺口记录，没有正式发布问题清单和关闭标准。 | `P1`；依赖事项 5–11；作为阶段 7 收尾。 |

### 11.3 P2：试运营与产品扩展

| 顺序 | 事项名称 | 简要说明 | 当前状态 | 优先级 / 依赖 |
| --- | --- | --- | --- | --- |
| 13 | 支付宝沙箱与支付回调 | 用支付宝沙箱替换模拟支付，增加支付流水、回调验签、金额/商户订单校验和重复回调幂等。 | **未开始**；当前订单使用虚拟资金。 | `P2`；依赖订单状态机稳定、支付账号和回调公网入口。 |
| 14 | 退款、售后与资金账本 | 建立退款状态、支付流水、不可变账本和异常补偿，不能只修改订单状态。 | **未开始**；当前模拟退款不是真实资金链路。 | `P2`；依赖事项 13、审计和合规设计。 |
| 15 | 真实物流与履约轨迹 | 接入单一物流适配器，保存运单和状态事件，处理签收、异常和回调幂等。 | **未开始**；当前只有模拟运单号。 | `P2`；依赖订单及售后规则，通常晚于支付。 |
| 16 | 内容审核、举报与用户治理 | 增加商品/生成内容审核、举报、用户封禁和违规资产处置，并记录审核决定。 | **未开始**；当前无审核、举报和封禁流程。 | `P2`；依赖管理后台、审计和版权规则；真实运营前置。 |
| 17 | 通知与实时任务进度 | 增加站内/邮件通知，必要时将 AI 轮询升级为 SSE，并处理断线重连和重复通知。 | **未开始**；当前以约 2 秒轮询任务状态。 | `P2`；依赖稳定的任务契约；有性能或体验数据后实施。 |
| 18 | 资产处理与性能优化 | 增加图片压缩/缩略图、GLB Draco/KTX2 优化、模型检查和 CDN/签名 URL 策略。 | **未开始**；当前保存原始文件并经 API 读取。 | `P2`；依赖资产访问策略和性能基线，以指标证明收益。 |
| 19 | 参考图生成与更多生成能力 | 增加参考图上传、图生 3D、多 Provider 和生成资产版本管理。 | **未开始**；当前只支持文本到 3D。 | `P2`；依赖事项 2、10 和资产处理；不先于文本链路稳定。 |
| 20 | 完整生产化平台能力 | 按实际规模补充限流、细粒度权限、成本统计、弹性 Worker、完整观测、灾备和必要的服务拆分。 | **未开始**；当前为模块化单体、独立 Worker 和 Compose。 | `P2`；依赖内测和性能指标，不应在 MVP 前置建设。 |

### 11.4 已知小缺口与当前处理决定

以下内容已核对，但不全部作为近期独立开发任务：

- `last_login_at` 更新错误处理、Refresh Cookie Path 扩大、登录踢旧设备：并入事项 9 的认证安全复核；若不影响内测，可后置。
- 商品上传后更新封面/模型字段的错误检查、生成输出唯一性约束：并入事项 4 和 6 的一致性测试与恢复验证，发现可复现问题后修复。
- 沙盒与模拟订单共用资金账户、沙盒重置不冲正订单资金：当前是演示设计；真实资金隔离归入事项 13–14。
- 已发布商品可直接改价：当前通过收藏快照标记更新，保持现状。
- `SHIPPED` 后不能取消：归入事项 14 的售后状态设计。
- OpenAPI 已补齐管理接口，但认证、账户、收藏和订单仍未完整覆盖，且生成任务取消接口仍残留“预留”描述：并入事项 4 的契约与测试补齐，不单独延后。

### 11.5 暂不列为当前开发任务的范围

以下能力不属于当前 MVP，除非产品范围重新确认：

- 正式支付、平台担保、自动分账和提现；事项 13–14 仅描述未来试运营能力。
- 求购悬赏、投稿竞标、交换、多人竞价、即时聊天和议价。
- 多物流聚合、复杂推荐、信用体系、自动风控和完整合规运营。
- 自托管 GPU 模型、Kubernetes、多区域容灾和过早的微服务拆分。

### 11.6 推荐执行顺序摘要

1. 事项 1–4 已完成关键范围，作为当前 MVP 行为与 API 测试基线。
2. 从事项 5 开始执行浏览器 E2E 与失败场景验收。
3. 通过事项 6–9 建立恢复、备份、安全和可观测性基线。
4. 按演示目标决定事项 10；无真实 Provider 账号时保持 Mock 并明确标识。
5. 完成事项 11–12，执行内测并关闭发布阻断问题，作为 MVP 收尾。
6. MVP 验收后按业务数据推进事项 13–20，不提前建设高成本扩展。
