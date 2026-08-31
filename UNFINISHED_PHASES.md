# 未完成阶段与后续执行计划

- **项目**：谷子交易与 3D/AIGC 平台
- **记录日期**：2026-08-31
- **当前阶段**：阶段 2 CentOS 运行环境已完成；商品资产上传、公网 IP 入口和网页 GLB 3D 查看器已完成；AI 生成 HTTP 闭环进行中
- **下一目标**：完成 AI 生成最小 HTTP 闭环（任务入队、轮询、结果落为 GLB）

## 当前进行中的任务

- **任务名称**：AI 生成最小 HTTP 闭环
- **任务描述**：登录用户创建生成任务、Redis Streams 入队、Worker 消费、轮询状态、结果存为 GLB，并覆盖超时、失败、重试和取消。
- **开始时间**：2026-08-31
- **当前进度状态**：进行中
- **已完成进展**：已实现 `POST /api/v1/generation-jobs`：登录校验、参数校验、Idempotency-Key 幂等、进行中任务上限，以及 `generation_jobs`/`generation_outbox` 持久化。
- **当前阻塞事项**：任务尚未写入 Redis Streams；Worker 仍只校验消息；前端工作台仍为占位页。
- **下一步**：将 outbox 事件发布到 Redis Streams `generation_jobs`。

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

## 一、当前已完成事项

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
- [x] 阶段 2 CentOS/Alibaba Cloud Linux 运行环境基线已完成（Docker、防火墙、日志轮转、备份目录、时间同步和磁盘告警）

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

认证完成后，仍需完成以下 MVP 业务模块：

### 商品与资产

- [x] 商品模型、GORM 自动迁移和 CRUD API
- [x] 商品列表、分页、关键词搜索及 IP、分类、成色、交易类型、价格筛选
- [x] 商品详情和 `DRAFT → PUBLISHED → OFF_SHELF` 状态流转
- [x] 商品图片上传和 GLB 上传
- [x] MinIO/S3 资产元数据记录
- [x] 文件类型、MIME、文件头、大小和归属校验
- [x] 商品编辑、草稿删除、发布、下架和所有权校验
- [x] 商品输入校验和状态机单元测试

### 3D 展示

- [x] Three.js/React Three Fiber GLB 查看器
- [x] 旋转、缩放、重置视角和全屏
- [x] 加载进度、错误提示和图片降级
- [x] 20 MB GLB 大小限制和异常模型处理

### 收藏与个人中心

- [ ] 收藏与取消收藏
- [x] 我的发布
- [ ] 我的收藏
- [ ] 基础用户资料
- [ ] 收货地址管理

### 模拟交易

- [ ] 创建订单和订单明细
- [ ] 模拟支付成功
- [ ] 待支付订单取消
- [ ] 卖家模拟发货
- [ ] 买家确认收货
- [ ] 订单状态机与订单事件
- [ ] 防止购买自己的商品
- [ ] 幂等、防重复提交和库存校验

### AI 建模工作台

- [ ] RAG 知识库和检索流程
- [ ] Prompt 优化与结构化参数展示
- [ ] 用户确认优化结果
- [x] 创建生成任务 HTTP 接口（`POST /api/v1/generation-jobs`）
- [ ] Redis Streams 任务入队
- [ ] Python AI Worker 业务执行闭环（消息校验入口已完成）
- [x] Mock Provider 及契约测试
- [ ] 第三方文本生成 3D Provider Adapter
- [ ] 任务轮询、超时、失败、重试和取消
- [ ] 生成结果存储为 GLB 资产
- [ ] 从 AI 结果一键带入商品发布

### 管理与审计

- [ ] 管理员查看用户和商品
- [ ] 管理员下架商品
- [ ] 查看和重试失败 AI 任务
- [ ] 管理操作审计日志

## 八、阶段 7：发布前验证与运维

- [ ] 补齐 API 单元测试和关键集成测试
- [ ] 执行注册、越权、上传、商品、订单和 AI 失败场景测试
- [ ] 验证页面刷新、服务重启和数据持久化
- [ ] 设置数据库和对象存储备份策略
- [ ] 验证备份恢复流程
- [ ] 配置结构化日志、Request ID 和基础监控
- [ ] 检查公网暴露端口和默认凭据
- [ ] 检查依赖漏洞和生产构建产物
- [ ] 准备演示数据和测试账号
- [ ] 邀请内测用户并记录核心指标
- [ ] 汇总问题清单并安排 MVP 发布前修复

## 九、建议执行顺序

1. 阶段 2 运行环境、阶段 4 健康检查和阶段 5 公网 IP 入口已完成
2. 将含 GLB 查看器的当前版本重新部署到公网
3. 阶段 6 按“AI 生成最小 HTTP 闭环 → 收藏与个人中心 → 交易 → 管理”完成剩余 P0
4. 阶段 7 执行发布验收、备份和内测

## 十、远程操作安全约定

- 不在聊天记录、仓库或脚本中粘贴私钥、密码、数据库密钥和 JWT 密钥。
- 优先使用本机已有 SSH Agent、SSH 配置或 CLI 的安全凭据存储。
- 远程执行前先进行只读检查，再执行安装、配置和部署操作。
- 修改生产配置前保留备份；删除数据卷、重置数据库等破坏性操作必须单独确认。
- 每次部署记录代码版本、配置变更、容器状态和健康检查结果。
