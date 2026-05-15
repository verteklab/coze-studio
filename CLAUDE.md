# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Coze Studio is an all-in-one AI agent development platform with both frontend (React + TypeScript) and backend (Go) components. The project uses a sophisticated monorepo architecture managed by Rush.js with 135+ frontend packages organized in a hierarchical dependency system.

**This checkout is `verteklab/coze-studio`** — a downstream fork of `coze-dev/coze-studio`. The `upstream` remote points at the upstream repo. When picking up new work, prefer rebasing fork-specific commits onto upstream over duplicating logic. The fork is also embedded inside a larger workspace (`../backend/`, `../frontend/`, `../coze_publish_sdk/`) that talks to this Coze instance over its OpenAPI surface; see the parent repo's `CLAUDE.md` for the gateway story.

### Fork-specific deltas (read before editing affected areas)

These are the verteklab customizations on top of upstream — find them by feature, not by greedy file search:

- **Custom HTTP model class** (`backend/bizpkg/llm/`, `backend/application/modelmgr/`, frontend `packages/workflow/playground/src/nodes-v2/llm/custom-http-utils.ts`). The "Other" model class (renamed from `CustomHTTP`) lets users wire arbitrary HTTP-backed LLMs. The fork distinguishes **chat-shaped** vs **raw HTTP** custom_http models via `isChatShapedCustomHTTP` / `showsLLMFields` helpers; chat-shaped models keep the prompt/skills UI and preserve those fields on submit, raw HTTP clears them. `payload_template` is exposed on `Model.custom_http`. For class "Other", defaults are `max_tokens=0`, min `1 → 0`.
- **Model admin APIs** — `UpdateModel` and capability/connection/display patches in `backend/application/modelmgr/` (GORM `Select` so only the patched columns move). Capability is propagated from `CreateModelReq` through to `bizpkg`, and `pickCapability` lets the request override the meta default. Thrift bindings under `idl/` + generated handlers in `backend/api/handler/` were regenerated (`chore(thrift): regenerate admin/config bindings`); regenerate after any IDL change.
- **OpenAI proxy client** (`backend/bizpkg/llm/modelbuilder/` + related) for the Custom HTTP integration.
- **Workflow version history** — `VersionHistoryList` and `RevertDraft` are fork-added endpoints. Touch points: `backend/api/handler/coze/workflow_service.go`, `backend/api/model/workflow/version_history.go`, `backend/api/router/coze/api.go`, `backend/application/workflow/workflow.go`. The version-control UI is exposed in the OSS build (upstream hides it). `GetHistorySchema` had a panic fix — keep nil-safety when extending.
- **OCR workflow node** — `backend/domain/workflow/internal/nodes/ocr/{ocr.go,helpers.go,ocr_test.go}`, with 4 provider protocol templates. The Go type is `OCRSetting` (renamed from `OCRNode.Setting` to fix an ambiguous selector); the node is enabled in the workflow node panel.
- **Iframe / embed UX** — the fork supports embedding Coze pages inside the parent gateway. Auth accepts `session_key` from header **and** query string (`feat(auth): support session_key from header and query for iframe access`) for iframe consumers. Knowledge pages and workflow pages have iframe-aware layout tweaks; docx preview renders client-side instead of routing through PDF.js.
- **Admin-key OpenAPI** — additional admin-bearer endpoints for dataset/photo details and workflow integration so the parent gateway can drive Coze without a user session. New middlewares in `backend/api/middleware/`.
- **Admin-context plumbing** — `backend/pkg/ctxutil` exposes `IsAdminFromCtx`; `SaveWorkflow` now enforces admin checks for user-space access.
- **Workflow `NodeProps`** gained `invalid` / `invalidReason` fields — propagated through canvas and node renderers.
- **i18n default is `zh-CN`** (user profile updated to match) — when adding strings, ensure both zh-CN and en-US bundles exist.
- **Docker** — Dockerfile pinned to `golang:1.24.4-alpine3.21`, Tsinghua mirrors for Alpine and Python packages, Go module proxy configured. The image build was unblocked with `c1a368d7`; don't reintroduce the broken base.

## Development Commands

### Environment Setup
```bash
# Clone and setup
git clone https://github.com/coze-dev/coze-studio.git
cd coze-studio

# Install frontend dependencies
rush update

# For Docker-based development
cd docker
cp .env.example .env
# Configure model settings in backend/conf/model/
docker compose up -d
# Access at http://localhost:8888
```

### Development Workflow
```bash
# Start middleware services (MySQL, Redis, Elasticsearch, etc.)
make middleware

# Start Go backend in development mode
make server

# Start frontend development server
cd frontend/apps/coze-studio
npm run dev

# Full development environment
make debug
```

### Build Commands
```bash
# Build frontend only
make fe

# Build Go server
make build_server

# Build everything with Docker
make web

# Rush monorepo commands
rush build                    # Build all packages
rush rebuild -o @coze-studio/app  # Build specific package
rush test                     # Run all tests
rush lint                     # Lint all packages
```

### Testing
```bash
# Run tests (Vitest-based)
rush test
npm run test                  # In specific package
npm run test:cov             # With coverage

# Backend tests
cd backend && go test ./...
```

## Architecture Overview

### Frontend Architecture
- **Monorepo**: Rush.js with 135+ packages across 4 dependency levels
- **Build System**: Rsbuild (Rspack-based) for fast builds
- **UI Framework**: React 18 + TypeScript + Semi Design + Tailwind CSS
- **State Management**: Zustand for global state
- **Package Organization**:
  - `arch/`: Core infrastructure (level-1)
  - `common/`: Shared components and utilities (level-2)
  - `agent-ide/`, `workflow/`, `studio/`: Feature domains (level-3)
  - `apps/coze-studio`: Main application (level-4)

### Backend Architecture (Go)
- **Framework**: Hertz HTTP framework
- **Architecture**: Domain-Driven Design (DDD) with microservices
- **Structure**:
  - `domain/`: Business logic and entities
  - `application/`: Application services and use cases
  - `api/`: HTTP handlers and routing
  - `infra/`: Infrastructure implementations
  - `crossdomain/`: Cross-cutting concerns

### Key Architectural Patterns
- **Adapter Pattern**: Extensive use for loose coupling between layers
- **Interface Segregation**: Clear contracts between domains
- **Event-Driven**: NSQ message queue for async communication
- **API-First**: Comprehensive OpenAPI specifications

## Database & Infrastructure

### Docker Services Stack
- **Database**: MySQL 8.4.5
- **Cache**: Redis 8.0
- **Search**: Elasticsearch 8.18.0 with SmartCN analyzer
- **Vector DB**: Milvus v2.5.10 for embeddings
- **Storage**: MinIO for object storage
- **Message Queue**: NSQ (nsqlookupd, nsqd, nsqadmin)
- **Configuration**: etcd 3.5

### Database Management
```bash
# Sync database schema
make sync_db

# Dump database schema
make dump_db

# Initialize SQL data
make sql_init

# Atlas migration management
make atlas-hash
```

## Key Development Patterns

### Frontend Package Development
- Each package follows consistent structure with `README.md`, `package.json`, `tsconfig.json`, `eslint.config.js`
- Adapter pattern extensively used for decoupling (e.g., `-adapter` suffix packages)
- Base/Core pattern for shared functionality (e.g., `-base` suffix packages)
- Use workspace references (`workspace:*`) for internal dependencies

### Backend Development
- Follow DDD principles with clear domain boundaries
- Use dependency injection via interfaces
- Implement proper error handling with custom error types
- Write comprehensive tests for domain logic

### Model Configuration
Before deployment, configure AI models in `backend/conf/model/`:
1. Copy template from `backend/conf/model/template/`
2. Set `id`, `meta.conn_config.api_key`, and `meta.conn_config.model`
3. Supported providers: OpenAI, Volcengine Ark, Claude, Gemini, Qwen, DeepSeek, Ollama

## Testing Strategy

### Coverage Requirements by Package Level
- **Level 1**: 80% coverage, 90% increment
- **Level 2**: 30% coverage, 60% increment
- **Level 3-4**: 0% coverage (flexible)

### Testing Framework
- **Frontend**: Vitest for unit/integration tests
- **Backend**: Go's built-in testing framework
- **E2E**: Separate e2e subspace configuration

## Common Issues & Solutions

### Frontend Development
- Use `rush update` instead of `npm install` at root level
- Build packages in dependency order using `rush build`
- For hot reload issues, check Rsbuild configuration in specific package

### Backend Development
- Ensure middleware services are running (`make middleware`)
- Check database connectivity and schema sync
- Verify model configurations are properly set

### Docker Issues
- Ensure sufficient resources (minimum 2 Core, 4GB RAM)
- Check port conflicts (8888 for frontend, various for services)
- Use `make clean` to reset Docker volumes if needed

## IDL and Code Generation

The project uses Interface Definition Language (IDL) for API contract management:
- IDL files in `idl/` directory (Thrift format)
- Frontend code generation via `@coze-arch/idl2ts-*` packages
- Backend uses generated Go structs

## Plugin Development

For custom plugin development:
- Reference templates in `backend/conf/plugin/pluginproduct/`
- Follow OAuth schema in `backend/conf/plugin/common/oauth_schema.json`
- Configure authentication keys for third-party services

## Contributing

- Use conventional commits via `rush commit`
- Run linting with `rush lint-staged` (pre-commit hook)
- Ensure tests pass before submitting PRs
- Follow team-based package organization and tagging conventions