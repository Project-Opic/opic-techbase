# opic-techbase

> English: [README.md](README.md)

[![License](https://img.shields.io/badge/License-MulanPSL--2.0-2E7D32)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](migrations)

[文档](#文档) | [架构](#架构) | [使用](#使用) | [部署](#部署) | [路线图](#路线图) | [许可证](#许可证)

**opic-techbase 是 OPIC 开放智能算力平台的技术底座**：一个 Go + React 的单仓应用，把内部系统都需要的横向能力——账号与权限、RBAC、防篡改审计留痕、轻量审批工作流引擎，以及基于 Temporal 的流程编排设计器——统一收敛到 `returnInfo` 响应信封与 OPIC 本体七模型注册表之后。

平台侧关注的是「一个组织如何把内部系统管起来」。opic-techbase 把这些横向能力做成可复用的底座，业务域（各能力中心）只需接入自己的表与页面，不必重复实现鉴权、审计、审批与监控。

本仓库包含后端服务（Gin）、管理控制台前端（React + Ant Design）、数据库迁移、本体模型（YAML）、部署编排与开发文档。

## 文档

| 文档 | 内容 |
|---|---|
| [新人培训入门](docs/ONBOARDING.md) | 背景 / 架构 / 环境 / 首次启动 / 开发流程 |
| [编译与部署指南](docs/DEPLOYMENT.md) | 构建、生产部署、迁移回滚、fork 清单 |
| [运维手册](docs/OPERATIONS.md) | 健康检查 / 监控告警 / 日志 / 备份 / 凭据 / 故障速查 |
| [FAQ](docs/FAQ.md) | 实战踩坑速查（SSO / SQL / 部署 / 前端 / AI） |

## 架构

opic-techbase 由两个运行单元组成，共享同一套 `internal/service` 业务层：

- **`techbase`** —— 后端主服务。Gin 提供 `/api` REST 接口与 `/health`、`/metrics` 探针；启动时自动执行 goose 迁移与种子数据（幂等）；内置 Temporal worker（承载编排解释器与 SLA 看护工作流）与监控采样调度器。
- **`frontend`** —— nginx 托管的 React SPA，含管理控制台与用户工作台两套界面。

请求链路：

- 浏览器 → Traefik（`techbase.<domain>`）→ `/api`、`/health`、`/metrics` 转发到 `techbase`，其余路径到 `frontend`。

数据面依赖：PostgreSQL 16（业务库，goose 迁移共 13 张表）、Redis（令牌吊销黑名单，可选）、NATS JetStream（领域事件，可选）、Temporal（流程运行时，可选）。

所有业务接口使用统一的 `{"returnInfo":{"returnCode","errorMsg"},"data":...}` 信封，成功码 `SUC0000`，错误码为 OPIC O-ARC 七位码（3 位字母前缀 + 4 位数字）；鉴权为 JWT Bearer + RBAC 权限码，`*` 为超管位。

### 核心能力

| 能力 | 说明 |
|---|---|
| **管理控制台** | 仪表盘、系统管理、日志审计、消息中心、系统工具、审批中心、业务管理七大模块，共 57 个 REST 端点 |
| **流程编排** | Temporal 驱动的 DSL 设计器（START / STEP / TIMER / CONDITION / END），支持发布、试运行与运行台账；早期自研审批流引擎（V1）保留运行兼容 |
| **组织与权限** | 用户 / 角色 / 权限 / 资源树（菜单）；角色沿父级聚合继承；`*` 超管位 |
| **日志与审计** | 操作 / 登录 / 审计日志与在线用户；登录日志记录 IP、地域、UA |
| **本体注册表** | 七模型 YAML（M1 对象 / M2 行为 / M5 主体 / M6 流程 / MU 界面），供种子数据与表达式求值器消费 |
| **可观测与运维** | `/health` 四探针、Prometheus 文本指标（`opic_techbase_*`）、优雅退出、登录限速退避 |

## 与其他工具的关系

opic-techbase 是**业务底座**，不替代数据库、身份提供方、工作流引擎或网关，而是在它们之上提供管理与治理能力。

| 层次 | 相关工具 | 关系 |
|---|---|---|
| 工程模式与视觉体系 | [gopherforge](https://github.com/SuperiorChuo/gopherforge)（MIT） | v3 重构（自 CloudWeGo Hertz）起作为技术选型与工程布局的基准，控制台玻璃主题移植自其前端并保留来源注释；本项目在其模式上扩展业务模块 |
| 身份与认证 | ZITADEL | 作为 OIDC 身份提供方接入（Authorization Code + PKCE、JWKS RS256 验签、角色声明映射与自动 provisioning）；`AUTH_MODE=local` 保留本地回退 |
| 流程运行时 | Temporal | 编排解释器、人工任务等待与 SLA 定时器由 Temporal 承载；V1 自研引擎保留运行兼容 |
| 事件总线 | NATS JetStream | 流程事件（`instance.started` / `task.created` / `instance.completed` / `sla.exceeded`）旁路发布，失败不阻塞主流程 |
| 数据与缓存 | PostgreSQL（Pigsty 供给）、Redis | PG 承载业务数据与迁移；Redis 仅用于令牌吊销，未启用时静默降级 |
| 入口与分发 | Traefik、Harbor | Traefik 负责子域路由与 TLS；Harbor 存放后端与前端镜像 |

如果你需要一个能管理账号权限、审计留痕与审批流程，并提供统一 API 信封与本体注册表的内部平台底座，那是本项目覆盖的范围；如果只需要单个业务系统的 CRUD，直接写业务服务更轻。

## 仓库结构

| 组件 | 路径 | 说明 |
|---|---|---|
| **后端服务** | [`cmd/techbase/`](cmd/techbase) | 组合根：配置 → 日志 → PostgreSQL（goose）→ 种子 → Redis → Gin → Temporal worker → 优雅退出 |
| **HTTP 层** | [`internal/api/`](internal/api) | `routes.go` 路由表、`common` 响应助手、`auth` / `meta` / `business` / `flow` / `system` 控制器 |
| **业务层** | [`internal/service/`](internal/service) | 认证、RBAC、流程、工作台、客户、种子；`engine/` 为 V1 审批流引擎 |
| **共享库** | [`pkg/`](pkg) | 错误码、日志、Redis、令牌、指标、健康检查、优雅退出、事件总线、工作流封装、Temporal 流程 DSL |
| **数据库迁移** | [`migrations/`](migrations) | goose 序号迁移（13 张表），启动自动 Up |
| **本体模型** | [`models/`](models) | 七模型 YAML（M1/M2/M5/M6/MU），供本体注册表与种子流程使用 |
| **前端** | [`frontend/`](frontend) | React 19 + Ant Design 6 + Redux Toolkit + React Router 7 + Vite；`@xyflow/react` 流程设计器 |
| **部署编排** | [`docker-compose.yml`](docker-compose.yml)、[`docker-compose.infra.yml`](docker-compose.infra.yml)、[`Dockerfile`](Dockerfile) | 应用栈（后端 / 前端）与数据栈（PG / Redis）分离 |
| **文档** | [`docs/`](docs) | 入门、部署、运维、FAQ |

## 基础软件依赖

版本以 `go.mod`、`frontend/package.json` 与 Dockerfile 为准；基础设施版本为生产部署实际使用的版本。

### 运行时与基础设施

| 组件 | 版本 | 上游仓库 |
|---|---|---|
| Go | 1.27 | <https://github.com/golang/go> |
| PostgreSQL | 16 | <https://github.com/postgres/postgres> |
| Redis | 8（可选） | <https://github.com/redis/redis> |
| NATS Server | 2.14（可选） | <https://github.com/nats-io/nats-server> |
| Temporal | 1.29（可选） | <https://github.com/temporalio/temporal> |
| Traefik | v3 | <https://github.com/traefik/traefik> |
| nginx | （前端托管） | <https://github.com/nginx/nginx> |
| Node.js | 20+（构建阶段） | <https://github.com/nodejs/node> |

### 后端（Go 模块）

| 模块 | 版本 | 上游仓库 |
|---|---|---|
| `github.com/gin-gonic/gin` | v1.12.0 | <https://github.com/gin-gonic/gin> |
| `gorm.io/gorm` | v1.31.0 | <https://github.com/go-gorm/gorm> |
| `gorm.io/driver/postgres` | v1.6.0 | <https://github.com/go-gorm/postgres> |
| `github.com/pressly/goose/v3` | v3.27.3 | <https://github.com/pressly/goose> |
| `github.com/golang-jwt/jwt/v5` | v5.3.0 | <https://github.com/golang-jwt/jwt> |
| `github.com/redis/go-redis/v9` | v9.22.0 | <https://github.com/redis/go-redis> |
| `github.com/nats-io/nats.go` | v1.54.0 | <https://github.com/nats-io/nats.go> |
| `go.temporal.io/sdk` | v1.49.0 | <https://github.com/temporalio/sdk-go> |
| `go.uber.org/zap` | v1.28.0 | <https://github.com/uber-go/zap> |
| `gopkg.in/natefinch/lumberjack.v2` | v2.2.1 | <https://github.com/natefinch/lumberjack> |
| `golang.org/x/crypto` | v0.57.0 | <https://github.com/golang/crypto> |
| `gopkg.in/yaml.v3` | v3.0.1 | <https://github.com/go-yaml/yaml> |

### 前端（npm）

| 包 | 版本 | 上游仓库 |
|---|---|---|
| `react` / `react-dom` | 19 | <https://github.com/facebook/react> |
| `antd` | 6 | <https://github.com/ant-design/ant-design> |
| `@ant-design/icons` | 6 | <https://github.com/ant-design/ant-design-icons> |
| `@reduxjs/toolkit` | 2.5 | <https://github.com/reduxjs/redux-toolkit> |
| `react-router-dom` | 7.2 | <https://github.com/remix-run/react-router> |
| `axios` | 1.8 | <https://github.com/axios/axios> |
| `@xyflow/react` | 12.3 | <https://github.com/xyflow/xyflow> |
| `vite` | 8 | <https://github.com/vitejs/vite> |
| `typescript` | 5.8 | <https://github.com/microsoft/TypeScript> |

以上第三方组件均使用宽松许可证（MIT / Apache-2.0 / BSD），具体条款以上游仓库为准。

## 使用

### 本地开发环境

前置：Go 1.27+、Node 20+、一个 PostgreSQL 16 实例（Redis / NATS / Temporal 可选）。

```bash
# 1) 建库并配置连接（可用 GO_TECHBASE_DSN 整串覆盖）
createdb go_techbase

# 2) 启动后端（自动 goose 迁移 + 种子，幂等）
make dev                 # → http://localhost:9680

# 3) 启动前端开发服务器（proxy → 9680）
make frontend-install && make dev-frontend   # → http://localhost:5173
```

默认账号（`AUTH_MODE=local`）：`admin/admin123`（超管）、`sales/123456`、`cmanager/123456`、`gm/123456`。

一键全栈（自带 PG + Redis）：

```bash
cp .env.example .env
make compose-up          # 数据栈 → 应用栈（网关 :8000，后端调试口 127.0.0.1:9680）
make compose-down
```

冒烟路径：`sales` 登录 → 建草稿 → 提交（启动审批流，状态「待客户经理审批」）→ `cmanager` 待办通过 → `gm` 待办通过 → 客户状态「已通过」，流程实例 `APPROVED`。

### 单独构建

```bash
make build                 # 后端二进制 → bin/techbase
make frontend-build        # 前端产物 → frontend/dist

make test                  # 单元测试
make lint                  # go vet
make migrate-status        # 查看迁移状态
```

## 部署

生产部署通过 `make deploy-prod` 完成：rsync 源码到目标机 → 在目标机构建镜像 → 复制 compose 与配置 → `docker compose up -d`。迁移在容器启动时自动执行。

```bash
make deploy-prod           # 构建 + 部署 + 拉起
make deploy-prod-down      # 停止应用栈
```

关键环境变量（完整清单见 [部署指南](docs/DEPLOYMENT.md)）：

| 变量 | 说明 |
|---|---|
| `GO_TECHBASE_DSN` | 整串 DSN 覆盖 database 段 |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE` | PostgreSQL 连接 |
| `APP_PORT` | HTTP 端口（默认 9680） |
| `JWT_SECRET` | HS256 会话密钥 |
| `AUTH_MODE` | `local` 或 `zitadel` |
| `ZITADEL_ISSUER/ZITADEL_CLIENT_ID/ZITADEL_CLIENT_SECRET/ZITADEL_REDIRECT_URL` | ZITADEL OIDC 参数 |
| `REDIS_ENABLED/REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | Redis（令牌吊销） |
| `METRICS_ENABLED` | `/metrics` 与采集中间件 |
| `LOG_LEVEL` | 日志级别（默认 `info`） |

### 分发渠道

镜像推送到内部 Harbor：后端 `192.168.3.200:18700/opic/techbase`，前端 `192.168.3.200:18700/opic/techbase-frontend`（`make harbor-push`）。

## 贡献

提交前请确认：

1. `go build ./...`、`go vet ./...` 通过，且 `cd frontend && npm run build` 通过；
2. 新增或修改接口时保持 `returnInfo` 信封与 OPIC O-ARC 错误码口径不变；
3. 涉及数据库变更时新增 goose 迁移文件（禁止 AutoMigrate），并保证 `Down` 可回滚。

分支与提交信息沿用 `feat|fix|docs|chore(scope): 说明` 格式。

提交贡献即表示同意以[木兰宽松许可证第 2 版](LICENSE)授权该贡献。

## 路线图

本仓库不维护长期功能路线图，计划与进展以下列方式公开：

- **已完成**（近期）：v3 重构落到 gopherforge 工程模式（Gin + goose + zap + 健康检查/指标 + 优雅退出，功能基线 100% 保留）；Temporal 流程编排设计器（DSL 设计、发布、试运行、运行台账）；事件总线与工作流驱动切换，NATS + Temporal 生产接线实测通过。
- **待办**（已知缺口）：控制台模块中依赖后端服务的部分——部门管理 / 岗位管理 / 租户管理 / 租户套餐 / 系统设置 / 短信管理 / 代码生成 / OAuth2 应用 / Webhook 订阅 / 文件管理 / 定时任务 / 告警规则 / Redis·MySQL 独立监控页。
- **限制**：V1 审批流与 Temporal 编排设计器是两套语义，通过 `flow_definition` 表的 `flow_type` 分流；Temporal 解释器的人工节点仅支持 `APPROVE` / `REJECT` 结论（不支持「退回指定节点」）。

## 社区

- **问题与建议**：通过仓库 Issue 反馈，附复现步骤与环境信息（版本、迁移号、部署方式）。
- **内部沟通**：OPIC 平台团队；生产问题走运维值班渠道。

## 安全与支持

- **报告漏洞**：请勿公开提交 Issue，直接联系平台团队。安全基线包括登录限速退避、操作审计（含 IP / 地域 / UA）、数据库错误不回显、会话可强制失效。
- **支持范围**：当前主分支与生产部署版本。

## 许可证

Copyright © 2026 wjluo

本项目采用**木兰宽松许可证第 2 版（Mulan PSL v2）**，全文见 [`LICENSE`](LICENSE)，也可从 <http://license.coscl.org.cn/MulanPSL2> 获取。

在遵守许可证条款的前提下，你可以自由复制、使用、修改和分发本软件，包括商业使用；分发时需保留版权与许可声明。许可证第 3 条明确不授予商标许可，第 5 条按「现状」提供软件、不附带任何明示或默示担保。

### 第三方与移植代码

- **gopherforge**（MIT）：v3 重构起作为工程模式与技术选型基准；管理控制台视觉体系（玻璃主题、登录页、Dashboard 样式）移植自其前端，移植文件头部保留来源注释；其 MIT 许可声明继续适用于该部分代码。
