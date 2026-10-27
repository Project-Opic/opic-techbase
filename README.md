# opic-techbase

> 简体中文: [README-zh.md](README-zh.md)

[![License](https://img.shields.io/badge/License-MulanPSL--2.0-2E7D32)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](migrations)

[Documentation](#documentation) | [Architecture](#architecture) | [Usage](#usage) | [Deployment](#deployment) | [Roadmap](#roadmap) | [License](#license)

**opic-techbase is the technical foundation for the OPIC open intelligent compute platform**: a single Go + React repository that packages the cross-cutting capabilities every internal system needs — accounts and permissions, RBAC, tamper-evident audit trails, a lightweight approval workflow engine, and the Temporal-based orchestration designer — behind a uniform `returnInfo` API envelope and the OPIC seven-model ontology registry.

The platform answers a recurring organizational question: how do you operate internal systems at scale? opic-techbase delivers these horizontal capabilities as a reusable foundation so that business domains (capability centers) can plug in their own tables and pages without re-implementing authentication, auditing, approvals, or monitoring.

This repository contains the backend service (Gin), the admin console frontend (React + Ant Design), database migrations, ontology models (YAML), deployment manifests, and developer documentation.

## Documentation

| Document | Contents |
|---|---|
| [Onboarding](docs/ONBOARDING.md) | Background, architecture, environment, first run, development workflow |
| [Build & Deployment](docs/DEPLOYMENT.md) | Build, production rollout, migration rollback, fork checklist |
| [Operations](docs/OPERATIONS.md) | Health checks, monitoring and alerting, logging, backup, credentials, troubleshooting |
| [FAQ](docs/FAQ.md) | Field notes on SSO, SQL, deployment, frontend, and AI |

## Architecture

opic-techbase runs as two units that share a single `internal/service` business layer:

- **`techbase`** — the primary backend. Gin serves the `/api` REST surface plus `/health` and `/metrics` probes. On startup it runs goose migrations and seeds idempotently, and it hosts the Temporal worker (orchestration interpreter and SLA guardian) along with the monitoring sampler.
- **`frontend`** — a React SPA served by nginx, covering both the admin console and the end-user workspace.

Request paths:

- Browser → Traefik (`techbase.<domain>`) → `/api`, `/health`, `/metrics` to `techbase`; everything else to `frontend`.

Data plane: PostgreSQL 16 (business database, 13 tables across goose migrations), Redis (token revocation blacklist, optional), NATS JetStream (domain events, optional), and Temporal (workflow runtime, optional).

Every business response uses a uniform envelope — `{"returnInfo":{"returnCode","errorMsg"},"data":...}` with `SUC0000` on success and the 7-digit `[3-letter prefix][4-digit]` OPIC O-ARC error codes. Authorization is JWT Bearer plus RBAC permission codes; a `*` code marks the super-admin position.

### Core Capabilities

| Capability | What it does |
|---|---|
| **Admin console** | Dashboard, system administration, log audit, message center, system tools, approval center, and business management — 57 REST endpoints in total |
| **Workflow orchestration** | Temporal-backed DSL designer (START / STEP / TIMER / CONDITION / END) with publish, dry run, and run ledger; the earlier in-house approval engine (V1) is retained for runtime compatibility |
| **Organization and access** | Users, roles, permissions, and a menu resource tree; role inheritance along the parent chain; `*` super-admin position |
| **Logging and audit** | Operation, login, and audit logs plus online users; login records capture IP, region, and user agent |
| **Ontology registry** | Seven-model YAML (M1 objects / M2 behaviors / M5 subjects / M6 flows / MU interfaces) consumed by seeds and the expression evaluator |
| **Observability and operations** | Four health probes, Prometheus text metrics (`opic_techbase_*`), graceful shutdown, and login rate limiting with backoff |

## How opic-techbase Relates to Other Tools

opic-techbase is a **business foundation**, not a replacement for a database, an identity provider, a workflow engine, or a gateway. It layers management and governance on top of them.

| Layer | Tools | Relationship |
|---|---|---|
| Engineering model and visual system | [gopherforge](https://github.com/SuperiorChuo/gopherforge) (MIT) | Serves as the baseline for technology choices and repository layout since the v3 refactor (from CloudWeGo Hertz). The console's glass theme is ported from its frontend with source headers preserved; this project extends the model with business modules |
| Identity and authentication | ZITADEL | Integrated as the OIDC provider (Authorization Code + PKCE, JWKS RS256 verification, role-claim mapping and auto-provisioning); `AUTH_MODE=local` keeps a local fallback |
| Workflow runtime | Temporal | The orchestration interpreter, human-task waits, and SLA timers run on Temporal; the V1 in-house engine remains for runtime compatibility |
| Event bus | NATS JetStream | Flow events (`instance.started`, `task.created`, `instance.completed`, `sla.exceeded`) are published on a side channel; failures never block the main approval path |
| Data and cache | PostgreSQL (provisioned by Pigsty), Redis | PostgreSQL holds business data and migrations; Redis is used only for token revocation and degrades silently when disabled |
| Ingress and distribution | Traefik, Harbor | Traefik handles subdomain routing and TLS; Harbor stores backend and frontend images |

If you need a foundation that manages accounts and permissions, audit trails, and approval workflows while exposing a uniform API envelope and ontology registry, that is the scope this project covers. If you only need CRUD for a single business system, a focused service will be lighter.

## Repository Map

| Component | Path | Description |
|---|---|---|
| **Backend service** | [`cmd/techbase/`](cmd/techbase) | Composition root: config → logging → PostgreSQL (goose) → seeds → Redis → Gin → Temporal worker → graceful shutdown |
| **HTTP layer** | [`internal/api/`](internal/api) | `routes.go` route table, `common` response helpers, and the `auth` / `meta` / `business` / `flow` / `system` controllers |
| **Business layer** | [`internal/service/`](internal/service) | Auth, RBAC, flows, workbench, customers, seeds — plus the V1 approval engine under `engine/` |
| **Shared libraries** | [`pkg/`](pkg) | Error codes, logging, Redis, tokens, metrics, health checks, graceful shutdown, event bus, workflow wrappers, Temporal flow DSL |
| **Migrations** | [`migrations/`](migrations) | Sequential goose migrations (13 tables), applied automatically at startup |
| **Ontology models** | [`models/`](models) | Seven-model YAML (M1/M2/M5/M6/MU) consumed by the ontology registry and seed flows |
| **Frontend** | [`frontend/`](frontend) | React 19 + Ant Design 6 + Redux Toolkit + React Router 7 + Vite; `@xyflow/react` for the flow designer |
| **Deployment** | [`docker-compose.yml`](docker-compose.yml), [`docker-compose.infra.yml`](docker-compose.infra.yml), [`Dockerfile`](Dockerfile) | Separate application stack (backend / frontend) and data stack (PG / Redis) |
| **Documentation** | [`docs/`](docs) | Onboarding, deployment, operations, FAQ |

## Base Software

Versions are pinned in `go.mod`, `frontend/package.json`, and the Dockerfiles. Infrastructure versions are those used by the production deployment.

### Runtime and infrastructure

| Component | Version | Upstream repository |
|---|---|---|
| Go | 1.27 | <https://github.com/golang/go> |
| PostgreSQL | 16 | <https://github.com/postgres/postgres> |
| Redis | 8 (optional) | <https://github.com/redis/redis> |
| NATS Server | 2.14 (optional) | <https://github.com/nats-io/nats-server> |
| Temporal | 1.29 (optional) | <https://github.com/temporalio/temporal> |
| Traefik | v3 | <https://github.com/traefik/traefik> |
| nginx | (frontend serving) | <https://github.com/nginx/nginx> |
| Node.js | 20+ (build stage) | <https://github.com/nodejs/node> |

### Backend (Go modules)

| Module | Version | Upstream repository |
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

### Frontend (npm)

| Package | Version | Upstream repository |
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

All third-party components above ship under permissive licenses (MIT / Apache-2.0 / BSD). Refer to each upstream repository for the exact terms.

## Usage

### Local Development Environment

Prerequisites: Go 1.27+, Node 20+, and a PostgreSQL 16 instance (Redis, NATS, and Temporal are optional).

```bash
# 1) Create a database and configure the connection (or override the whole DSN via GO_TECHBASE_DSN)
createdb go_techbase

# 2) Start the backend (migrations and seeds run automatically, idempotently)
make dev                 # → http://localhost:9680

# 3) Start the frontend dev server (proxies to :9680)
make frontend-install && make dev-frontend   # → http://localhost:5173
```

Default accounts (`AUTH_MODE=local`): `admin/admin123` (super admin), `sales/123456`, `cmanager/123456`, `gm/123456`.

Full stack with bundled PostgreSQL and Redis:

```bash
cp .env.example .env
make compose-up          # data stack, then app stack (gateway :8000, backend debug port 127.0.0.1:9680)
make compose-down
```

Smoke path: sign in as `sales` → create a draft → submit (starts the approval flow, status "pending sales-manager approval") → `cmanager` approves the todo → `gm` approves → the customer moves to "approved" and the flow instance ends as `APPROVED`.

### Building Individual Components

```bash
make build                 # backend binary → bin/techbase
make frontend-build        # frontend assets → frontend/dist

make test                  # unit tests
make lint                  # go vet
make migrate-status        # migration status
```

## Deployment

Production deployment runs through `make deploy-prod`: it rsyncs the source to the target host, builds the images there, copies compose and config files, then runs `docker compose up -d`. Migrations execute automatically when the container starts.

```bash
make deploy-prod           # build, deploy, start
make deploy-prod-down      # stop the application stack
```

Key environment variables (see the [Deployment Guide](docs/DEPLOYMENT.md) for the full list):

| Variable | Description |
|---|---|
| `GO_TECHBASE_DSN` | Whole-DSN override of the database section |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE` | PostgreSQL connection |
| `APP_PORT` | HTTP port (default 9680) |
| `JWT_SECRET` | HS256 session secret |
| `AUTH_MODE` | `local` or `zitadel` |
| `ZITADEL_ISSUER/ZITADEL_CLIENT_ID/ZITADEL_CLIENT_SECRET/ZITADEL_REDIRECT_URL` | ZITADEL OIDC parameters |
| `REDIS_ENABLED/REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | Redis (token revocation) |
| `METRICS_ENABLED` | `/metrics` and the sampling middleware |
| `LOG_LEVEL` | Log level (default `info`) |

### Distribution Channels

Images are pushed to the internal Harbor registry: backend `192.168.3.200:18700/opic/techbase`, frontend `192.168.3.200:18700/opic/techbase-frontend` (`make harbor-push`).

## Contributing

Before opening a pull request, please confirm:

1. `go build ./...` and `go vet ./...` pass, and `cd frontend && npm run build` passes.
2. Any added or changed endpoint keeps the `returnInfo` envelope and OPIC O-ARC error codes intact.
3. Schema changes ship as a new goose migration (never `AutoMigrate`), with a working `Down`.

Branch names and commit messages follow `feat|fix|docs|chore(scope): summary`.

By submitting a contribution, you agree to license it under the [Mulan PSL v2](LICENSE).

## Roadmap

This repository does not maintain a long-range feature roadmap. Planned work and progress are tracked in the open:

- **Recently completed**: the v3 refactor onto the gopherforge engineering pattern (Gin + goose + zap + health/metrics + graceful shutdown) with a 100% functional baseline, the Temporal orchestration designer (DSL designer, publish, dry run, run ledger), and the event-bus / workflow driver switches with production-verified NATS + Temporal wiring.
- **Known gaps**: console modules pending their backend services — departments, positions, tenants and tenant packages, system settings, SMS, code generation, OAuth2 applications, webhook subscriptions, file management, scheduled jobs, alert rules, and standalone Redis / MySQL monitoring pages.
- **Limitations**: the V1 approval engine and the Temporal orchestration designer are two separate semantics that share the `flow_definition` table via `flow_type`; the Temporal interpreter supports only `APPROVE` / `REJECT` outcomes on human nodes (no "return to a specific node").

## Community

- **Issues and suggestions**: open a repository issue with reproduction steps and environment details (version, migration number, deployment mode).
- **Internal channels**: the OPIC platform team; production incidents go through the on-call channel.

## Security & Support

- **Reporting a vulnerability**: please do not open a public issue; contact the platform team directly. The security baseline includes login rate limiting with backoff, operation auditing (IP, region, user agent), non-disclosure of database errors, and session invalidation.
- **Support scope**: the current main branch and the deployed production version.

## License

Copyright © 2026 wjluo

This project is licensed under the **Mulan Permissive Software License, Version 2 (Mulan PSL v2)**. The full text is in [`LICENSE`](LICENSE) and is also available at <http://license.coscl.org.cn/MulanPSL2>.

Subject to the terms of the license, you may copy, use, modify, and distribute this software, including for commercial purposes; copyright and license notices must be retained when redistributing. Section 3 grants no trademark license, and Section 5 provides the software on an "AS IS" basis without warranties of any kind.

### Third-Party and Ported Code

- **gopherforge** (MIT): the engineering pattern and technology selection baseline since the v3 refactor, and the admin console's visual system (glass theme, login page, dashboard styling) is ported from its frontend, with source headers preserved in the ported files. Its MIT notice continues to apply to that portion.
