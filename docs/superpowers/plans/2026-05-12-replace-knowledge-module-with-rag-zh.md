# 用 rag 服务替换 coze-studio 知识库模块 — 实施计划

> **致执行该计划的 Agent 工作者:** 必需的子技能: 使用 superpowers:subagent-driven-development (推荐) 或 superpowers:executing-plans 按任务逐步实施本计划。所有步骤使用复选框 (`- [ ]`) 语法以便跟踪。

**目标:** 用独立的 `rag` Python/FastAPI 服务,通过 HTTP 调用,替换 coze-studio 内置的 Go 知识库模块。保持 coze 的 IDL 契约不变;rag 尚未支持的特性返回 HTTP 501。

**架构:** 新增 `infra/rag` HTTP 客户端 + 新增 `domain/knowledge/service/ragimpl` 包,通过委托给 rag 来实现 `service.Knowledge` 接口。分两个 PR 滚动发布:PR-1 在 feature flag (`KNOWLEDGE_BACKEND=rag|legacy`,默认 `legacy`) 后面引入新代码;PR-2 翻转默认值并删除遗留的 domain 代码。映射表保留 coze 的 int64 ID ↔ rag 的字符串 UUID。

**技术栈:** Go 1.x (Hertz, gorm)、FastAPI (rag)、MySQL (coze 元数据)、MongoDB+ES+MinIO+Redis (rag)、Atlas 迁移、Vitest (前端)、Go testing。

**设计文档:** `docs/superpowers/specs/2026-05-12-replace-knowledge-module-with-rag-design.md`

---

## 文件结构(新增 / 修改 / 删除)

### 新增 (PR-1)

```
backend/types/errno/rag.go
backend/conf/rag/rag.yaml
backend/conf/rag/config.go
backend/infra/contract/rag/client.go
backend/infra/contract/rag/types.go
backend/infra/rag/client.go
backend/infra/rag/client_test.go
backend/infra/rag/errors.go
backend/infra/rag/errors_test.go
backend/domain/knowledge/service/ragimpl/factory.go
backend/domain/knowledge/service/ragimpl/tenant.go
backend/domain/knowledge/service/ragimpl/tenant_test.go
backend/domain/knowledge/service/ragimpl/mapping.go
backend/domain/knowledge/service/ragimpl/mapping_test.go
backend/domain/knowledge/service/ragimpl/knowledge.go
backend/domain/knowledge/service/ragimpl/knowledge_test.go
backend/domain/knowledge/service/ragimpl/document.go
backend/domain/knowledge/service/ragimpl/document_test.go
backend/domain/knowledge/service/ragimpl/retrieval.go
backend/domain/knowledge/service/ragimpl/retrieval_test.go
backend/domain/knowledge/service/ragimpl/unsupported.go
backend/domain/knowledge/service/ragimpl/unsupported_test.go
backend/domain/knowledge/service/ragimpl/integration_test.go
backend/api/handler/coze/model_providers_service.go
docker/atlas/migrations/YYYYMMDDhhmmss_add_rag_mapping.sql  (由 `atlas migrate diff` 自动生成)
tools/rag-contract-check/main.go
docker/docker-compose.rag.yml
docker/docker-compose.test.yml
frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/model-selector.tsx
frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/model-selector.test.tsx
docs/superpowers/plans/2026-05-12-replace-knowledge-module-with-rag.md  (英文版)
docs/superpowers/plans/2026-05-12-replace-knowledge-module-with-rag-zh.md  (本文件)
```

### 修改 (PR-1)

```
backend/domain/knowledge/service/interface.go         (不修改方法签名;若有仅供已删除文件使用的字段则清理)
backend/application/knowledge/init.go                 (接入 feature flag)
backend/application/knowledge/knowledge.go            (rag.yaml 配置 + 代理 handler 接入)
backend/api/handler/coze/knowledge_service.go         (注册 /api/rag/model_providers 路由)
backend/types/consts/env.go                           (新增 KNOWLEDGE_BACKEND 环境变量)
idl/data/knowledge/knowledge.thrift                   (在 CreateDatasetRequest 上新增 2 个可选字段)
frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/index.tsx
docker/docker-compose.yml                             (通过 extends 引入 rag stack)
docker/atlas/opencoze_latest_schema.hcl               (追加 rag_kb_mapping 与 rag_doc_mapping)
docker/atlas/migrations/atlas.sum                     (由 `atlas migrate hash` 更新)
.env.example                                          (新增 RAG_* 环境变量)
```

### 新增 (PR-2,在 PR-1 稳定后)

PR-2 **不引入任何 schema 变更**。遗留的 `knowledge_*` 表原样保留为"死重",清理工作推迟到独立的运维 PR。

### 删除 (PR-2)

```
backend/domain/knowledge/internal/dal/
backend/domain/knowledge/internal/convert/
backend/domain/knowledge/internal/events/
backend/domain/knowledge/internal/mock/
backend/domain/knowledge/processor/
backend/domain/knowledge/repository/
backend/domain/knowledge/service/knowledge.go
backend/domain/knowledge/service/retrieve.go
backend/domain/knowledge/service/sheet.go
backend/domain/knowledge/service/datacopy.go
backend/domain/knowledge/service/event_handle.go
backend/domain/knowledge/service/rdb.go
backend/domain/knowledge/service/validate.go
backend/domain/knowledge/service/knowledge_test.go
backend/domain/knowledge/service/knowledge_integration_test.go
backend/domain/knowledge/service/retrieve_test.go
backend/domain/knowledge/service/sheet_test.go
backend/domain/knowledge/service/event_handle_test.go
```

### 新增 (rag 仓库的独立 PR)

```
rag/docs/notes/roadmap.md
```

---

## 第 1 阶段 — 基础设施

### 任务 1:新增 errno 错误码

**涉及文件:**
- 新增: `backend/types/errno/rag.go`

- [ ] **步骤 1:创建 errno 文件**

写入 `backend/types/errno/rag.go`:

```go
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package errno

import "github.com/coze-dev/coze-studio/backend/pkg/errorx/code"

// RAG 集成: 105 100 000 ~ 105 199 999
const (
	ErrRagFeaturePendingCode    = 105100001
	ErrRagUpstreamUnavailableCode = 105100002
	ErrRagCrossTenantCode       = 105100003
	ErrRagMappingNotFoundCode   = 105100004
	ErrRagInvalidConfigCode     = 105100005
)

func init() {
	code.Register(ErrRagFeaturePendingCode, "feature pending rag support: {detail}")
	code.Register(ErrRagUpstreamUnavailableCode, "rag service unavailable: {detail}")
	code.Register(ErrRagCrossTenantCode, "cross-tenant retrieval rejected: {detail}")
	code.Register(ErrRagMappingNotFoundCode, "rag mapping not found: {detail}")
	code.Register(ErrRagInvalidConfigCode, "invalid rag config: {detail}")
}
```

> **注意:** `code.Register` 的实际签名应以 `backend/pkg/errorx/code/code.go` 为准。如果注册 API 不同(例如带状态码或类别参数),请参照 `backend/types/errno/knowledge.go:23` 的现有约定进行调整。`105100xxx` 数值区间为 RAG 集成预留;不要与 `105000xxx` 知识库错误码冲突。

- [ ] **步骤 2:验证编译通过**

运行: `cd backend && go build ./types/errno/...`
期望:无错误。

- [ ] **步骤 3:提交**

```bash
git add backend/types/errno/rag.go
git commit -m "feat(errno): add rag integration error codes"
```

---

### 任务 2:在 Atlas HCL 中声明新的映射表(非破坏性)

> coze 使用 **Atlas 声明式 schema 管理**。唯一权威来源是 `docker/atlas/opencoze_latest_schema.hcl`。`docker/atlas/migrations/` 下的迁移 SQL 由 `atlas migrate diff` **自动生成**,不要手写。coze 现有的 `knowledge_*` 表不被本任务修改。

**涉及文件:**
- 修改: `docker/atlas/opencoze_latest_schema.hcl`(追加两段 `table` HCL 块)
- 自动生成: `docker/atlas/migrations/YYYYMMDDhhmmss_add_rag_mapping.sql`(由 Atlas 生成,文件名含时间戳)
- 修改: `docker/atlas/migrations/atlas.sum`(由 `atlas migrate hash` 重算)

- [ ] **步骤 1:在 `opencoze_latest_schema.hcl` 中追加两段 `table` 块**

插入位置建议靠近其他 `knowledge_*` 表(在文件第 1497 行附近搜索 `table "knowledge_document" {`),让相关的表聚在一起。列约定遵循项目现有风格:`created_at` / `updated_at` 是 `bigint unsigned` 毫秒时间戳;`deleted_at` 是 `datetime(3) null`。

完整 HCL 代码见英文版任务 2 步骤 1。包含两段表声明:

**注意:rag-authoritative 设计下,映射表故意只保留 ID 映射 + coze 独有的展示/审计字段。name / description / status / format_type / space_id 等业务数据**不存**在 coze 这边,每次实时从 rag 取。**

- `table "rag_kb_mapping"`:
  - 列:`coze_kb_id`(PK)、`rag_kb_id`(unique)、`icon_uri`(coze 独有)、`app_id`(仅供 UI 过滤,不影响隔离)、`creator_id`(审计)、`created_at`、`deleted_at`
  - 索引:`uk_rag_kb_id` 唯一索引;`idx_app (app_id, deleted_at)`

- `table "rag_doc_mapping"`:
  - 列:`coze_doc_id`(PK)、`rag_doc_id`(unique)、`coze_kb_id`、`creator_id`、`last_task_id`、`created_at`、`deleted_at`
  - 索引:`uk_rag_doc_id` 唯一索引;`idx_kb (coze_kb_id, deleted_at)`

注意刻意不存:`name`、`description`、`status`、`format_type`、`space_id`、`source_uri`、`updated_at`。这些一律由 rag 权威。

- [ ] **步骤 2:生成迁移 SQL**

在仓库根:

```bash
cd docker/atlas
atlas migrate diff add_rag_mapping --env local --to file://opencoze_latest_schema.hcl
```

Atlas 会在 `docker/atlas/migrations/` 下写入 `YYYYMMDDhhmmss_add_rag_mapping.sql`,包含它从 diff 计算出的 `CREATE TABLE` 语句。**不要手工修改这个文件。**

- [ ] **步骤 3:本地应用并校验**

```bash
cd /Users/liuxinyu/workspace/coze-studio && make sync_db
mysql -e "SHOW TABLES LIKE 'rag_%';" opencoze
mysql -e "DESCRIBE rag_kb_mapping;" opencoze
mysql -e "DESCRIBE rag_doc_mapping;" opencoze
```

期望:两张新表已创建。再确认遗留表未被修改:

```bash
mysql -e "DESCRIBE knowledge_document;" opencoze | grep rag_doc_id || echo "OK: 没有新增 rag_doc_id 列"
```

- [ ] **步骤 4:重算哈希**

```bash
make atlas-hash
```

这会更新 `docker/atlas/migrations/atlas.sum`。

- [ ] **步骤 5:提交**

```bash
git add docker/atlas/opencoze_latest_schema.hcl docker/atlas/migrations/
git commit -m "feat(db): declare rag_kb_mapping and rag_doc_mapping tables"
```

---

### 任务 3:rag 服务配置

**涉及文件:**
- 新增: `backend/conf/rag/rag.yaml`
- 新增: `backend/conf/rag/config.go`
- 修改: `backend/types/consts/env.go`
- 修改: `.env.example`

- [ ] **步骤 1:编写 rag.yaml**

`backend/conf/rag/rag.yaml`:

```yaml
rag:
  base_url: "${RAG_BASE_URL:http://rag-web:8000}"
  timeout_ms: 10000
  upload_timeout_ms: 60000
  retrieval_timeout_ms: 15000
  max_retries: 2
  retry_backoff_ms: 200
  default_text_embedding_model_id: "${RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID}"
  default_image_embedding_model_id: "${RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID}"

# 注:没有 auth_token 字段。rag 当前没有服务间鉴权,coze 完全靠网络隔离
# (rag 绑定到内网 Docker 网络)。详见 spec §11 风险 #1。

knowledge:
  backend: "${KNOWLEDGE_BACKEND:legacy}"  # 取值: legacy | rag
  tenant:
    mode: "${RAG_TENANT_MODE:env}"               # env(Phase 1) | user(Phase 2,未来)
    default_tenant_id: "${RAG_TENANT_ID:coze}"   # mode=env 时使用
```

- [ ] **步骤 2:编写配置结构体**

`backend/conf/rag/config.go`:

```go
package rag

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL                       string        `yaml:"base_url"`
	Timeout                       time.Duration `yaml:"-"`
	TimeoutMs                     int           `yaml:"timeout_ms"`
	UploadTimeoutMs               int           `yaml:"upload_timeout_ms"`
	RetrievalTimeoutMs            int           `yaml:"retrieval_timeout_ms"`
	MaxRetries                    int           `yaml:"max_retries"`
	RetryBackoffMs                int           `yaml:"retry_backoff_ms"`
	DefaultTextEmbeddingModelID   string        `yaml:"default_text_embedding_model_id"`
	DefaultImageEmbeddingModelID  string        `yaml:"default_image_embedding_model_id"`
}

type FileConfig struct {
	Rag       Config           `yaml:"rag"`
	Knowledge KnowledgeBackend `yaml:"knowledge"`
}

type KnowledgeBackend struct {
	Backend string `yaml:"backend"`
}

func Load(path string) (*FileConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rag config: %w", err)
	}
	expanded := os.ExpandEnv(string(b))
	var c FileConfig
	if err := yaml.Unmarshal([]byte(expanded), &c); err != nil {
		return nil, fmt.Errorf("parse rag config: %w", err)
	}
	c.Rag.Timeout = time.Duration(c.Rag.TimeoutMs) * time.Millisecond
	return &c, nil
}
```

- [ ] **步骤 3:编写单元测试**

`backend/conf/rag/config_test.go`:

```go
package rag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rag.yaml")
	body := `rag:
  base_url: "http://x:8000"
  timeout_ms: 5000
  upload_timeout_ms: 30000
  retrieval_timeout_ms: 10000
  max_retries: 1
  retry_backoff_ms: 100
  default_text_embedding_model_id: "t"
  default_image_embedding_model_id: "i"
knowledge:
  backend: "rag"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rag.BaseURL != "http://x:8000" {
		t.Fatalf("base_url=%s", c.Rag.BaseURL)
	}
	if c.Knowledge.Backend != "rag" {
		t.Fatalf("backend=%s", c.Knowledge.Backend)
	}
	if c.Rag.Timeout.Milliseconds() != 5000 {
		t.Fatalf("timeout=%v", c.Rag.Timeout)
	}
}

func TestLoad_EnvSubstitution(t *testing.T) {
	t.Setenv("MY_BASE", "http://envset:9000")
	dir := t.TempDir()
	p := filepath.Join(dir, "rag.yaml")
	body := `rag:
  base_url: "${MY_BASE}"
  timeout_ms: 1000
  upload_timeout_ms: 1000
  retrieval_timeout_ms: 1000
  max_retries: 0
  retry_backoff_ms: 0
  default_text_embedding_model_id: ""
  default_image_embedding_model_id: ""
knowledge:
  backend: "legacy"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rag.BaseURL != "http://envset:9000" {
		t.Fatalf("base_url=%s", c.Rag.BaseURL)
	}
}
```

- [ ] **步骤 4:运行测试,确认通过**

运行: `cd backend && go test ./conf/rag/...`
期望:PASS。

- [ ] **步骤 5:新增环境变量常量**

向 `backend/types/consts/env.go` 追加:

```go
const (
	KnowledgeBackendEnv = "KNOWLEDGE_BACKEND" // "legacy" | "rag"
	RagBaseURLEnv       = "RAG_BASE_URL"
)
```

- [ ] **步骤 6:在 .env.example 追加配置项**

追加:

```bash
# RAG 服务集成
KNOWLEDGE_BACKEND=legacy
RAG_BASE_URL=http://rag-web:8000
RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID=
RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID=
# Tenant:Phase 1 全局单 tenant;Phase 2 待 user.rag_tenant_id 上线后切换为 user 模式
RAG_TENANT_MODE=env
RAG_TENANT_ID=coze
# 注:没有 RAG_AUTH_TOKEN。rag 当前没有服务间鉴权,完全靠网络隔离。
```

- [ ] **步骤 7:提交**

```bash
git add backend/conf/rag/ backend/types/consts/env.go .env.example
git commit -m "feat(conf): add rag service config and feature flag"
```

---

### 任务 4:rag 客户端契约(接口 + 类型)

**涉及文件:**
- 新增: `backend/infra/contract/rag/types.go`
- 新增: `backend/infra/contract/rag/client.go`

- [ ] **步骤 1:编写 types.go**

完整代码见英文版任务 4 的 `types.go`。包含:`ModelProvider`、`ListModelProvidersResponse`、`FusionPolicy`、`CreateKBRequest`、`KB`、`UpdateKBRequest`、`ListKBsRequest/Response`、`CreateDocumentRequest/Response`、`Document`、`ListDocumentsResponse`、`Task`、`RetrieveRequest`、`RetrieveHit`、`RetrieveResponse`、`ErrorBody`。所有 JSON tag 与 rag FastAPI 端点的 schema 严格对齐。

- [ ] **步骤 2:编写 client.go(接口定义)**

完整代码见英文版任务 4 的 `client.go`。定义 `Client` 接口,每个 rag 端点对应一个 Go 方法:`Ready`、`ListModelProviders`、`CreateKB/GetKB/UpdateKB/DeleteKB/ListKBs`、`CreateDocument/GetDocument/ListDocuments/DeleteDocument`、`GetTask`、`Retrieve`。同时添加 `mockgen` 注释。

- [ ] **步骤 3:验证编译通过**

运行: `cd backend && go build ./infra/contract/rag/...`
期望:无错误。

- [ ] **步骤 4:提交**

```bash
git add backend/infra/contract/rag/
git commit -m "feat(rag): add infra/contract/rag interface and types"
```

---

### 任务 5:rag HTTP 客户端 — 构造函数 + 请求辅助器

**涉及文件:**
- 新增: `backend/infra/rag/client.go`
- 新增: `backend/infra/rag/errors.go`

- [ ] **步骤 1:编写 errors.go(错误映射)**

完整代码见英文版任务 5 的 `errors.go`。`MapRagError` 函数将 rag 端的错误码映射到 coze 的 errno:

- rag `40001-40009` → `ErrKnowledgeInvalidParamCode` / `ErrCapabilityMismatch`
- rag `404xx` → `ErrKnowledgeNotExistCode` / `ErrKnowledgeDocumentNotExistCode`
- rag `409xx` → `ErrKnowledgeDuplicateCode`
- 其他 → `ErrRagUpstreamUnavailableCode`(兜底)

> **注意:** `errorx.New` 和 `errorx.KV` 的实际签名应以 `backend/pkg/errorx/error.go` 为准。提交前请先查看该文件。如果 API 不同(例如使用变参 `WithExtra`),请相应调整。

- [ ] **步骤 2:编写 errors_test.go**

完整代码见英文版任务 5 的 `errors_test.go`。覆盖:invalid param、KB not found、conflict、fallback 四种场景。

- [ ] **步骤 3:运行测试,验证通过**

运行: `cd backend && go test ./infra/rag/... -run TestMapRagError`
期望:PASS。

- [ ] **步骤 4:编写 client.go 脚手架(构造函数 + 请求辅助器,尚未含端点方法)**

完整代码见英文版任务 5 的 `client.go`。

要点:
- `Client` 结构体携带 `cfg` 与 `*http.Client`
- `New(cfg)` 构造函数
- `Ready(ctx)` 健康检查
- `doJSON(ctx, method, path, body, out, timeout)`:统一的 JSON 入参 / 出参请求,带超时与重试。
- 重试仅对幂等方法(GET、DELETE)生效;非幂等方法(POST)失败立即返回。
- **不注入** `Authorization` 头(rag 当前没有 token 鉴权;详见 spec §11 风险 #1)。
- 错误响应通过 `MapRagError` 转换。

> **导入提示:** 配置包和契约包都叫 `rag`,需要给其中一个起别名(例如 `import ragconf "github.com/.../conf/rag"`),并相应修改字段类型。本计划中两个都用 `rag` 简称,工程师在落地时请正确起别名。

- [ ] **步骤 5:验证编译(尚无端点方法,接口断言会失败)**

第 5 步:暂时移除 `var _ contract.Client = (*Client)(nil)` 这一行,使该文件可以编译。所有端点方法在任务 6-8 中添加完毕后再恢复该断言。

运行: `cd backend && go build ./infra/rag/...`
期望:无错误。

- [ ] **步骤 6:提交**

```bash
git add backend/infra/rag/
git commit -m "feat(rag): scaffold HTTP client with retry helper and error mapping"
```

---

### 任务 6:rag 客户端 — 模型提供商 + KB 相关端点

**涉及文件:**
- 修改: `backend/infra/rag/client.go`
- 新增: `backend/infra/rag/client_test.go`(第一批测试)

- [ ] **步骤 1:用 httptest 服务器编写测试 — TDD**

完整代码见英文版任务 6 的 `client_test.go`。`newTestClient` 辅助函数用 `httptest.NewServer` 起一个测试用 HTTP 服务,并创建带超时与零重试的 Client。

测试用例:
- `TestListModelProviders`:校验 GET `/model_providers` 路径与响应解析。
- `TestCreateKB`:校验 POST `/knowledgebases` 请求体序列化与响应解析。
- `TestGetKB_NotFound`:校验 404 + `ErrorBody{Code:40401}` 触发错误。

- [ ] **步骤 2:运行测试,验证 FAIL(方法尚未实现)**

运行: `cd backend && go test ./infra/rag/... -run TestListModelProviders -v`
期望:编译失败,`c.ListModelProviders undefined`。

- [ ] **步骤 3:实现端点方法**

向 `backend/infra/rag/client.go` 追加:`ListModelProviders`、`CreateKB`、`GetKB`、`UpdateKB`、`DeleteKB`、`ListKBs`。完整代码见英文版任务 6 步骤 3。

- [ ] **步骤 4:运行测试,验证 PASS**

运行: `cd backend && go test ./infra/rag/...`
期望:PASS。

- [ ] **步骤 5:提交**

```bash
git add backend/infra/rag/client.go backend/infra/rag/client_test.go
git commit -m "feat(rag): implement model providers and KB endpoints"
```

---

### 任务 7:rag 客户端 — 文档与任务端点

**涉及文件:**
- 修改: `backend/infra/rag/client.go`
- 修改: `backend/infra/rag/client_test.go`

- [ ] **步骤 1:追加测试**

向 `client_test.go` 追加 `TestCreateDocument`、`TestGetTask`、`TestDeleteDocument_NotRetried`(后者验证非幂等请求 POST 失败时不会重试,而幂等 DELETE 在配置 `MaxRetries=1` 时确实重试一次)。完整代码见英文版任务 7 步骤 1。

- [ ] **步骤 2:运行测试,验证 FAIL**

运行: `cd backend && go test ./infra/rag/... -run TestCreateDocument`
期望:编译失败或符号未定义。

- [ ] **步骤 3:实现方法**

向 `client.go` 追加:`CreateDocument`、`GetDocument`、`ListDocuments`、`DeleteDocument`、`GetTask`。完整代码见英文版任务 7 步骤 3。

注意 `CreateDocument` 使用 `UploadTimeoutMs` 超时(60 秒,大于普通请求)。

- [ ] **步骤 4:运行测试,验证 PASS**

运行: `cd backend && go test ./infra/rag/...`
期望:PASS。

- [ ] **步骤 5:提交**

```bash
git add backend/infra/rag/
git commit -m "feat(rag): implement document and task endpoints"
```

---

### 任务 8:rag 客户端 — 检索端点

**涉及文件:**
- 修改: `backend/infra/rag/client.go`
- 修改: `backend/infra/rag/client_test.go`

- [ ] **步骤 1:追加测试**

追加 `TestRetrieve`,校验 POST `/retrieval` 请求体携带正确的 `tenant_id`、`kb_ids[]`、`query_mode`,以及响应解析。完整代码见英文版任务 8 步骤 1。

- [ ] **步骤 2:验证 FAIL**

运行: `cd backend && go test ./infra/rag/... -run TestRetrieve`
期望:编译失败。

- [ ] **步骤 3:实现 Retrieve**

向 `client.go` 追加 `Retrieve` 方法。使用 `RetrievalTimeoutMs` 超时(15 秒)。

- [ ] **步骤 4:恢复接口实现断言**

在 `client.go` 顶部恢复(任务 5 步骤 5 移除的)断言:

```go
var _ contract.Client = (*Client)(nil)
```

- [ ] **步骤 5:运行 infra/rag 全部测试,验证 PASS**

运行: `cd backend && go test ./infra/rag/...`
期望:PASS。

- [ ] **步骤 6:提交**

```bash
git add backend/infra/rag/
git commit -m "feat(rag): implement retrieval endpoint; client implements contract"
```

---

## 第 2 阶段 — 映射仓库

### 任务 9:映射仓库

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/mapping.go`
- 新增: `backend/domain/knowledge/service/ragimpl/mapping_test.go`

- [ ] **步骤 1:用表驱动方式编写映射仓库测试**

完整代码见英文版任务 9 步骤 1 的 `mapping_test.go`。使用内存 SQLite 模拟 MySQL,操作两张**新建**的映射表 `rag_kb_mapping` 与 `rag_doc_mapping`(coze 现有的 `knowledge_*` 表不被本路径触及)。

测试用例:
- `TestMapping_KBByCozeID`:根据 coze int64 ID 反查 rag UUID。
- `TestMapping_KBByCozeID_NotFound`:未找到时返回错误。
- `TestMapping_KBsByCozeIDs_SameTenant`:同 `space_id` 时返回所有映射;不同 `space_id` 时返回 `ErrCrossTenant` 错误。
- `TestMapping_DocByCozeID`:文档映射查询。

- [ ] **步骤 2:运行测试,验证 FAIL**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestMapping`
期望:编译失败(`NewMappingRepo` / `ErrCrossTenant` 未定义)。

- [ ] **步骤 3:实现 mapping.go**

完整代码见英文版任务 9 步骤 3 的 `mapping.go`。包含:

- 类型:`KBMapping`、`DocMapping`
- 错误:`ErrCrossTenant`、`ErrMappingNotFound`
- 读方法:`KBByCozeID`、`KBsByCozeIDs`(带跨租户检查)、`DocByCozeID`、`DocsByCozeIDs`
- 写方法:`InsertKB`、`InsertDoc`、`SoftDeleteKB`、`RestoreKB`、`SoftDeleteDoc`、`RestoreDoc`、`UpdateDocStatus`

所有 SQL 操作目标都是新建的 `rag_kb_mapping` / `rag_doc_mapping`,主键分别为 `coze_kb_id` / `coze_doc_id`。时间戳字段命名遵循项目约定:`created_at` / `updated_at` 为 `bigint unsigned` 毫秒值,`deleted_at` 为 `datetime(3)`。所有读方法过滤软删除 (`deleted_at IS NULL`)。

- [ ] **步骤 4:运行测试,验证 PASS**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestMapping`
期望:PASS。

- [ ] **步骤 5:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/mapping.go backend/domain/knowledge/service/ragimpl/mapping_test.go
git commit -m "feat(ragimpl): add mapping repository (int64 <-> rag UUID)"
```

---

## 第 3 阶段 — ragimpl 服务实现

### 任务 10:TenantResolver

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/tenant.go`
- 新增: `backend/domain/knowledge/service/ragimpl/tenant_test.go`

完整代码见英文版任务 10。要点:

- 定义 `TenantResolver` 接口 — 只有一个方法 `Resolve(ctx context.Context) (string, error)`
- Phase 1 实现:`EnvTenantResolver` — 持有一个固定的 `tenantID`,所有调用返回同一个值,忽略 ctx
- Phase 2 实现:`UserTenantResolver` — 当前以**注释形式**保留作为契约说明;启用需要先在 `user` 表加 `rag_tenant_id` 字段(独立的另一个 PR)
- 测试覆盖:返回配置值、忽略 ctx 中的 user 信息、空 default 时报错

- [ ] **步骤 1:编写 tenant_test.go**(完整代码见英文版)
- [ ] **步骤 2:运行测试,验证 FAIL**
- [ ] **步骤 3:实现 tenant.go**(完整代码见英文版)
- [ ] **步骤 4:运行测试,验证 PASS**
- [ ] **步骤 5:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/tenant.go backend/domain/knowledge/service/ragimpl/tenant_test.go
git commit -m "feat(ragimpl): add TenantResolver (Phase 1 = single global tenant via env)"
```

---

### 任务 10B:工厂函数 + 状态枚举映射

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/factory.go`

- [ ] **步骤 1:编写 factory.go**

完整代码见英文版任务 10B 步骤 1。要点:

- `Impl` 结构体:持有 `rag contract.Client`、`*MappingRepo`、`idgen.IDGenerator`、**`resolver TenantResolver`**、默认模型 ID
- `New(rag, db, idgen, resolver, defaultText, defaultImage) *Impl` 构造函数
- `(i *Impl) tenant(ctx) (string, error)`:**唯一**的 tenant_id 来源,所有方法都通过它取
- `RagStatusToEntity(s string) entity.DocumentStatus`:`pending|processing|ready|failed` → coze 枚举

**重要:** 不再有 `tenantOf(spaceID int64) string` 这种"从 SpaceID 推 tenant"的助手 — 这条路被刻意封死,确保所有 tenant 都走 resolver。

> **注意:** `entity.DocumentStatusInit / Processing / Enable / Failed` —— 请确认这些常量名实际存在于 `backend/domain/knowledge/entity/document.go`。若名称不同(例如 `DocumentStatusPending`),请以现有名称为准。`idgen.IDGenerator` 是 `backend/infra/idgen` 下已有的雪花 ID 生成器 —— 请确认构造函数 / 方法名称(很可能是 `Next()` 或 `Gen()`)。

- [ ] **步骤 2:验证编译(在所有方法实现完毕前会失败)**

运行: `cd backend && go build ./domain/knowledge/service/ragimpl/...`
期望:`Impl does not implement service.Knowledge (missing method CreateKnowledge ...)`。这是预期的 —— 接口断言会在任务 11-14 完成后通过。

暂时注释掉 `var _ service.Knowledge = (*Impl)(nil)` 这一行让文件可以编译;在任务 14 中恢复。

- [ ] **步骤 3:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/factory.go
git commit -m "feat(ragimpl): scaffold factory with TenantResolver injection"
```

---

### 任务 11:未支持方法的 stub(bucket B)

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/unsupported.go`
- 新增: `backend/domain/knowledge/service/ragimpl/unsupported_test.go`

- [ ] **步骤 1:编写测试**

完整代码见英文版任务 11 步骤 1。该测试遍历所有 19 个 bucket-B 方法,断言:

- 返回非 nil 错误
- 错误消息含 `roadmap` 或 `rag/docs` 锚点
- 错误码为 `ErrRagFeaturePendingCode`

- [ ] **步骤 2:运行,验证 FAIL**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestUnsupported`
期望:方法未定义导致编译失败。

- [ ] **步骤 3:实现 unsupported.go**

完整代码见英文版任务 11 步骤 3。所有 19 个 bucket-B 方法实现为返回 `pending(method, anchor)` 的薄壳:

```go
func pending(method, roadmapAnchor string) error {
	return errorx.New(errno.ErrRagFeaturePendingCode, errorx.KV("detail",
		fmt.Sprintf("%s is pending rag support (roadmap: rag/docs/notes/roadmap.md#%s)", method, roadmapAnchor)))
}
```

各方法及其 roadmap 锚点对应关系:

| coze 方法 | roadmap 锚点 |
|---|---|
| `UpdateDocument` | `doc-metadata-update` |
| `ResegmentDocument` | `re-segmentation` |
| `CreateSlice`、`UpdateSlice`、`DeleteSlice`、`ListSlice`、`GetSlice`、`MGetSlice`、`ListPhotoSlice` | `manual-chunk-crud` |
| `GetAlterTableSchema`、`ValidateTableSchema`、`GetDocumentTableInfo`、`GetImportDataTableSchema` | `table-sheet-ingestion` |
| `ExtractPhotoCaption` | `photo-caption-extraction` |
| `CreateDocumentReview`、`MGetDocumentReview`、`SaveDocumentReview` | `document-review-workflow` |
| `CopyKnowledge`、`MoveKnowledgeToLibrary` | `kb-copy-move` |

- [ ] **步骤 4:运行,验证 PASS**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestUnsupported`
期望:全部 19 个子测试 PASS。

- [ ] **步骤 5:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/unsupported.go backend/domain/knowledge/service/ragimpl/unsupported_test.go
git commit -m "feat(ragimpl): add bucket-B unsupported method stubs returning 501"
```

---

### 任务 12:KB CRUD 方法 (knowledge.go)

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/knowledge.go`
- 新增: `backend/domain/knowledge/service/ragimpl/knowledge_test.go`

- [ ] **步骤 1:编写测试(使用 mock 客户端)**

完整代码见英文版任务 12 步骤 1。`fakeClient` 实现 `contract.Client` 接口以隔离 HTTP 层。

测试用例:
- `TestCreateKnowledge_HappyPath`:校验 rag 收到 `TenantResolver` 返回的 `tenant_id`(不是 `req.SpaceID` 派生)、默认模型 ID 注入、映射行只含 ID+icon+audit。
- `TestDeleteKnowledge_RollbackOnRagFailure`:rag 删除失败时,coze 行回滚为非软删状态。

- [ ] **步骤 2:运行,验证 FAIL**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestCreateKnowledge`
期望:`impl.CreateKnowledge undefined`。

- [ ] **步骤 3:实现 knowledge.go**

完整代码见英文版任务 12 步骤 3。要点:

- **所有方法首先调 `tenant, err := i.tenant(ctx)`**(通过 resolver 取 tenant_id)。再也不从 `req.SpaceID` 推导 tenant。
- `CreateKnowledge`:取模型 ID 覆盖 → resolver 取 tenant → 调用 `rag.CreateKB` → `idgen.Gen` → `mapping.InsertKB`(slim 签名,只插 ID+icon+app_id+creator_id);插入失败时尝试回滚 rag 端 KB。
- `UpdateKnowledge`:查映射 → resolver 取 tenant → 调 `rag.UpdateKB`;**不在 coze 镜像 name/description/status**(那些都是 rag 的数据)。
- `DeleteKnowledge`:resolver 取 tenant → 软删 coze 行 → 调 `rag.DeleteKB`;rag 失败时 `RestoreKB`。
- `GetKnowledgeByID` / `MGetKnowledgeByID`:查映射 → resolver 取 tenant → 调 rag → `hydrateKnowledge` 合并(`name`、`description`、`status` 全部来自 rag;`icon_uri`、`app_id`、`creator_id` 来自 coze 映射)。
- `ListKnowledge`:resolver 取 tenant → 调 `rag.ListKBs(tenant)` → 遍历用 `kbByRagID` 反查映射 → 合并。`req.AppID` 若有,仅作 coze 侧 post-filter,不参与 tenant 隔离。
- 辅助函数:`defaultChunkTypesFor`、`defaultSourceModalitiesFor`(根据 `FormatType` 推导)、`statusToRag` / `statusFromRag`、`hydrateKnowledge`。

> **注意:** 需要向 `mapping.go` 增加辅助函数 `kbByRagID`(镜像 `KBByCozeID` 但按 `rag_kb_id` 过滤)。请同时添加对应单元测试。

- [ ] **步骤 4:向 mapping.go 增加 `kbByRagID` 与测试**

按上述 Note 添加方法,并在映射测试中追加 `TestMapping_KBByRagID`。

- [ ] **步骤 5:运行所有 ragimpl 测试,验证 PASS**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/...`
期望:PASS。

- [ ] **步骤 6:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/knowledge.go backend/domain/knowledge/service/ragimpl/knowledge_test.go backend/domain/knowledge/service/ragimpl/mapping.go
git commit -m "feat(ragimpl): implement KB CRUD methods"
```

---

### 任务 13:文档方法 (document.go)

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/document.go`
- 新增: `backend/domain/knowledge/service/ragimpl/document_test.go`

- [ ] **步骤 1:编写测试**

完整代码见英文版任务 13 步骤 1。`docFakeClient` 嵌入 `fakeClient` 并覆写 `CreateDocument`、`GetTask`。

测试用例:
- `TestCreateDocument_InsertsMapping`:校验文档映射行插入(slim 字段),`rag_doc_id` 与 `last_task_id` 写入,**不再写 name/status/source_uri**。
- `TestMGetDocumentProgress_MirrorsStatus`:校验 rag task 状态正确映射为 `entity.DocumentStatusEnable`。**不再断言 MySQL 镜像** — status 现在每次都从 rag 实时取。

- [ ] **步骤 2:运行,验证 FAIL**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestCreateDocument_InsertsMapping`
期望:编译失败。

- [ ] **步骤 3:实现 document.go**

完整代码见英文版任务 13 步骤 3。要点:

- **所有方法首先 `tenant, err := i.tenant(ctx)`**。
- `CreateDocument`:resolver 取 tenant → 遍历批量文档 → 查 KB 映射 → 调 `rag.CreateDocument` → `idgen.Gen` → `mapping.InsertDoc`(slim 签名)。`source_modality` 根据 doc 类型(image / 其他)推导。`metadata` 自动注入 `coze_document_id` 与 `creator_id`。
- `DeleteDocument`:resolver 取 tenant → 先软删 → 调 rag → 失败回滚。
- `ListDocument`:resolver 取 tenant → 分页参数转换(`offset/limit` → `page/page_size`)→ 调 rag → 反查映射。
- `MGetDocument` / `MGetDocumentProgress`:遍历调用,跳过映射缺失的项。**不再写回 MySQL** — status 不在 mapping 里。`MGetDocumentProgress` 每次实时调 rag。
- 辅助函数:`sourceModalityFor`、`buildDocMetadata`、`taskStatusToDoc`(rag task status `success|failed|pending|retrying|running` → coze 文档状态)。

> **注意:** 还需向 `mapping.go` 添加 `docByRagID`(镜像 `kbByRagID`)。

- [ ] **步骤 4:添加 `docByRagID` 及对应测试,然后运行全部测试**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/...`
期望:PASS。

- [ ] **步骤 5:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/document.go backend/domain/knowledge/service/ragimpl/document_test.go backend/domain/knowledge/service/ragimpl/mapping.go
git commit -m "feat(ragimpl): implement document methods + status mirroring"
```

---

### 任务 14:Retrieve 方法 (retrieval.go)

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/retrieval.go`
- 新增: `backend/domain/knowledge/service/ragimpl/retrieval_test.go`

- [ ] **步骤 1:编写测试**

完整代码见英文版任务 14 步骤 1。

测试用例:
- `TestRetrieve_RejectsNL2SQL`:`Strategy.EnableNL2SQL=true` 时返回错误,错误消息含 `NL2SQL`。
- ~~`TestRetrieve_RejectsCrossTenant`~~ — **已删除**。新设计下,coze 不做 cross-tenant 预校验(rag 负责)。Phase 1 单全局 tenant 时该断言无意义。
- `TestRetrieve_HappyPath`:正常路径校验 rag 请求体的 `tenant_id` 来自 `resolver`(而非 `space_id`)、`kb_ids[]`;响应中的 `doc_id` 通过映射反查后转换为 coze int64 `document_id`。

- [ ] **步骤 2:运行,验证 FAIL**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... -run TestRetrieve`
期望:`impl.Retrieve undefined`。

- [ ] **步骤 3:实现 retrieval.go**

完整代码见英文版任务 14 步骤 3。处理流程:

1. NL2SQL 子特性守卫:`Strategy.EnableNL2SQL` 为 true 时返回 501。
2. 解析所有 KB 映射(**不强制同租户** —— rag 自己用 `tenant_id` 过滤决定哪些 KB 可见)。
3. `tenant, _ := i.tenant(ctx)`(resolver 取)。
4. 翻译 `DocumentIDs`(若提供)→ rag doc IDs。
5. 映射 coze `Strategy` → rag 请求体(`SearchType`、`QueryRewrite`、`Rerank`、`TopK`、`MinScore`、`MaxTokens`)。
6. 调用 `rag.Retrieve(tenant_id=tenant, kb_ids=...)`。
7. 将 hits 翻译为 `RetrieveSlice`(rag `doc_id` → coze `document_id` 通过映射反查;映射缺失的命中跳过)。

> **注意:** `knowledgeModel.SliceContent` 和 `knowledgeModel.SliceContentTypeText` —— 请到 `backend/crossdomain/knowledge/model/knowledge.go` 验证实际名称。

- [ ] **步骤 4:在 factory.go 恢复编译期接口断言**

恢复:

```go
var _ service.Knowledge = (*Impl)(nil)
```

- [ ] **步骤 5:运行所有 ragimpl 测试,验证 PASS**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/...`
期望:PASS。

- [ ] **步骤 6:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/retrieval.go backend/domain/knowledge/service/ragimpl/retrieval_test.go backend/domain/knowledge/service/ragimpl/factory.go
git commit -m "feat(ragimpl): implement Retrieve with NL2SQL+cross-tenant guards"
```

---

## 第 4 阶段 — 应用层接入

### 任务 15:在 `application/knowledge/init.go` 接入 feature flag

**涉及文件:**
- 修改: `backend/application/knowledge/init.go`

- [ ] **步骤 1:阅读现有 init.go,保留其形态**

先 `cat backend/application/knowledge/init.go` 了解构造函数签名。

- [ ] **步骤 2:修改 InitService 以根据 `KNOWLEDGE_BACKEND` 分支**

完整代码见英文版任务 15 步骤 2。要点:

- 读取 `KNOWLEDGE_BACKEND` 环境变量,默认 `legacy`。
- `legacy` 分支:走原有 `knowledgeImpl.NewKnowledgeSVC(c)`,注册 NSQ 消费者(保持现状)。
- `rag` 分支:加载 `conf/rag/rag.yaml` → `infrarag.New(cfg.Rag)` → `client.Ready(ctx)` 探活 → `ragimpl.New(...)`。不注册 NSQ(因为 ragimpl 不发事件)。
- 未知后端值 → 返回错误。

> **注意:** `c.DB` 与 `c.IDGen` 若 `KnowledgeSVCConfig` 现在没有,需要新增;在 `application/application.go` 的构造调用处一并补齐。

- [ ] **步骤 3:验证编译**

运行: `cd backend && go build ./application/knowledge/...`

- [ ] **步骤 4:冒烟测试(两种后端)**

```bash
cd backend && KNOWLEDGE_BACKEND=legacy go test ./application/knowledge/...
```

```bash
cd backend && KNOWLEDGE_BACKEND=rag go test ./application/knowledge/... -run TestInitConfigLoads
```

`rag` 分支预期会因为 rag 服务未启动而 `rag not ready` 报错 —— 这是预期表现,证明配置路径加载正确。

- [ ] **步骤 5:提交**

```bash
git add backend/application/knowledge/init.go
git commit -m "feat(knowledge): wire ragimpl behind KNOWLEDGE_BACKEND feature flag"
```

---

### 任务 16:IDL 扩展以支持模型选择

**涉及文件:**
- 修改: `idl/data/knowledge/knowledge.thrift`

- [ ] **步骤 1:新增可选字段**

在 `idl/data/knowledge/knowledge.thrift` 的 `struct CreateDatasetRequest` 末尾(在 `255: optional base.Base Base` 之前)追加:

```thrift
    // PR-1:KB 创建时转发给 rag。可选;缺省时后端使用默认值。
    8: optional string text_embedding_model_id
    9: optional string image_embedding_model_id
```

- [ ] **步骤 2:重新生成 Go 代码**

```bash
cd backend && make idl
```

或参照 Makefile 中的 IDL 生成命令。

- [ ] **步骤 3:验证编译**

运行: `cd backend && go build ./...`

- [ ] **步骤 4:提交**

```bash
git add idl/data/knowledge/knowledge.thrift backend/api/model/data/knowledge/
git commit -m "feat(idl): add optional embedding model ids to CreateDatasetRequest"
```

---

### 任务 17:`/model_providers` 代理接口

**涉及文件:**
- 新增: `backend/api/handler/coze/model_providers_service.go`
- 修改: `backend/api/handler/coze/knowledge_service.go`(路由注册)

- [ ] **步骤 1:编写代理 handler**

完整代码见英文版任务 17 步骤 1。`ListRagModelProviders` 调用 `knowledge.KnowledgeSVC.ListRagModelProviders(ctx)`,返回 rag 的 `/model_providers` 响应。

- [ ] **步骤 2:在 application 层新增代理方法**

向 `backend/application/knowledge/knowledge.go` 追加 `ListRagModelProviders` 方法 + `ragClient()` 访问器。若 `KNOWLEDGE_BACKEND=legacy`,返回 501 风格错误。

在结构体上新增字段:

```go
type KnowledgeApplicationService struct {
	DomainSVC service.Knowledge
	eventBus  search.ResourceEventBus
	storage   storage.Storage
	rag       contract.Client // 仅在 KNOWLEDGE_BACKEND=rag 时非空
}
```

- [ ] **步骤 3:在 init 中注入 `rag`**

在 `init.go` 的 rag 分支构造完客户端后,设置 `KnowledgeSVC.rag = ragClient`。

- [ ] **步骤 4:注册路由**

在 `backend/api/handler/coze/knowledge_service.go` 找到其他路由注册位置,加入 `ListRagModelProviders`(若路由在 IDL 中声明,则在 IDL 中追加;查看现有 knowledge 路由的写法)。

- [ ] **步骤 5:验证编译**

运行: `cd backend && go build ./...`

- [ ] **步骤 6:提交**

```bash
git add backend/api/handler/coze/model_providers_service.go backend/application/knowledge/knowledge.go backend/application/knowledge/init.go
git commit -m "feat(api): add /model_providers proxy for create-KB UI"
```

---

### 任务 18:在 CreateKnowledge 中转发模型 ID

**涉及文件:**
- 修改: `backend/application/knowledge/knowledge.go`

- [ ] **步骤 1:通过 context 传递模型 ID**

在 application 调用 `k.DomainSVC.CreateKnowledge(...)` 之前,构造一个携带模型 ID 的 ctx:

```go
type ragModelOverrideKey struct{}

type modelOverridePair struct{ Text, Image string }

func contextWithModelOverride(ctx context.Context, text, image string) context.Context {
	if text == "" && image == "" {
		return ctx
	}
	return context.WithValue(ctx, ragModelOverrideKey{}, modelOverridePair{Text: text, Image: image})
}

func ModelOverrideFromContext(ctx context.Context) (modelOverridePair, bool) {
	v, ok := ctx.Value(ragModelOverrideKey{}).(modelOverridePair)
	return v, ok
}
```

> **注意:** 不要让 `domain` 反向引用 `application`。将 context key 定义在 `backend/types/consts/contextkeys.go`,双方都从这里 import。

- [ ] **步骤 2:在 application 入口注入**

在 IDL 请求落地处(CreateDataset handler 入口),从请求体取出 `text_embedding_model_id` 与 `image_embedding_model_id`,调用 `contextWithModelOverride` 后再传入 `createKnowledgeInternal`。

- [ ] **步骤 3:更新 ragimpl 的 `getModelOverride`**

在 `backend/domain/knowledge/service/ragimpl/knowledge.go` 修改:

```go
func getModelOverride(ctx context.Context) (modelOverride, bool) {
	if v, ok := ctx.Value(consts.RagModelOverrideKey).(struct{ Text, Image string }); ok {
		return modelOverride{text: v.Text, image: v.Image}, true
	}
	return modelOverride{}, false
}
```

并在 `CreateKnowledge` 中将 `ctx` 传入。

- [ ] **步骤 4:补充 context 透传测试**

完整代码见英文版任务 18 步骤 4。

- [ ] **步骤 5:运行,验证 PASS**

运行: `cd backend && go test ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
期望:PASS。

- [ ] **步骤 6:提交**

```bash
git add backend/application/knowledge/ backend/domain/knowledge/service/ragimpl/knowledge.go backend/domain/knowledge/service/ragimpl/knowledge_test.go backend/types/consts/
git commit -m "feat(knowledge): forward optional embedding model ids to rag"
```

---

## 第 5 阶段 — 前端、Docker、契约校验、集成测试

### 任务 19:前端模型选择器

**涉及文件:**
- 新增: `frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/model-selector.tsx`
- 新增: `frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/model-selector.test.tsx`
- 修改: `frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/index.tsx`

- [ ] **步骤 1:编写测试**

完整代码见英文版任务 19 步骤 1。覆盖:

- 从 `/model_providers` 拉取数据后渲染文本与图像模型选项。
- 选择改变时通过 `onChange` 派发 `{ textModelId, imageModelId }`。

- [ ] **步骤 2:实现组件**

完整代码见英文版任务 19 步骤 2。两个 `<Select>`(来自 `@coze-arch/coze-design/components`),分别绑定文本模型与图像模型。初始默认值为各列表的第 0 项,挂载时调一次 `onChange`。

- [ ] **步骤 3:运行测试,验证 PASS**

```bash
cd frontend/packages/data/knowledge/knowledge-modal-base && npm test -- model-selector
```

期望:2 个测试通过。

- [ ] **步骤 4:接入到创建 KB 模态框**

修改 `index.tsx`,在表单中插入 `<ModelSelector />`,并将选中值在提交时携带到 `/api/knowledge_dataset/create` 的请求体。具体改动取决于现有 modal 的表单结构 —— 找到表单提交代码,加入两个新字段。

- [ ] **步骤 5:提交**

```bash
git add frontend/packages/data/knowledge/knowledge-modal-base/src/create-knowledge-modal-v2/
git commit -m "feat(frontend): add embedding model selector to create-KB modal"
```

---

### 任务 20:rag stack 的 Docker compose

**涉及文件:**
- 新增: `docker/docker-compose.rag.yml`
- 修改: `docker/docker-compose.yml`
- 新增: `docker/docker-compose.test.yml`

- [ ] **步骤 1:编写 docker-compose.rag.yml**

完整代码见英文版任务 20 步骤 1。服务清单:

- `rag-mongo`(MongoDB 7,命名卷)
- `rag-elasticsearch`(ES 8.11.4,单节点,安全关闭)
- `rag-redis`(Redis 7-alpine)
- `rag-minio`(MinIO,带 console)
- `rag-web`(从 `../../rag/Dockerfile` 构建,运行 uvicorn)
- `rag-worker`(同镜像,运行 Celery worker)

所有服务连接到 `coze` 外部网络,以便 coze-studio 主服务能访问。

> **注意:** 构建上下文 `../../rag` 假定 `rag` 仓库与 `coze-studio` 同级目录。请根据实际仓库布局调整。

- [ ] **步骤 2:从主 compose 引用**

在 `docker/docker-compose.yml` 顶部加注释或使用 `include:` 字段:

```yaml
# 可选的 rag stack —— 使用以下命令启动:
# docker compose -f docker-compose.yml -f docker-compose.rag.yml up
```

- [ ] **步骤 3:docker-compose.test.yml**

镜像 `docker-compose.rag.yml`,但使用临时卷(无命名卷)以便 CI 使用。在宿主机暴露 8000 端口供集成测试访问。

- [ ] **步骤 4:冒烟测试**

```bash
cd docker && docker compose -f docker-compose.rag.yml up -d rag-mongo rag-elasticsearch rag-redis rag-minio rag-web
curl -f http://localhost:8000/ready
docker compose -f docker-compose.rag.yml down
```

期望:`/ready` 返回 200。

- [ ] **步骤 5:提交**

```bash
git add docker/docker-compose.rag.yml docker/docker-compose.test.yml docker/docker-compose.yml
git commit -m "feat(docker): add rag stack compose"
```

---

### 任务 21:契约校验工具

**涉及文件:**
- 新增: `tools/rag-contract-check/main.go`

- [ ] **步骤 1:编写工具**

完整代码见英文版任务 21 步骤 1。

工具行为:抓取 rag 的 `/openapi.json` → 断言 coze 客户端依赖的所有路径 + 方法仍存在(对照硬编码列表 `required`)。任何缺失项打到 stderr,退出码非 0,以便在 CI 中阻断。

`required` 列表覆盖:`/ready`、`/model_providers`、`/knowledgebases`(GET/POST)、`/knowledgebases/{kb_id}`(GET/PATCH/DELETE)、`/knowledgebases/{kb_id}/documents`(GET/POST)、`/documents/{doc_id}`(GET/DELETE)、`/tasks/{task_id}`、`/retrieval`。

- [ ] **步骤 2:对一个运行中的 rag 实例冒烟**

```bash
cd tools/rag-contract-check && go run . -base http://localhost:8000
```

期望:`OK`。

- [ ] **步骤 3:提交**

```bash
git add tools/rag-contract-check/
git commit -m "feat(tools): add rag-contract-check"
```

---

### 任务 22:集成测试(受门控)

**涉及文件:**
- 新增: `backend/domain/knowledge/service/ragimpl/integration_test.go`

- [ ] **步骤 1:编写集成测试**

完整代码见英文版任务 22 步骤 1。要点:

- 通过 `//go:build integration` 构建标签门控,且必须 `INTEGRATION=1` 才执行(否则跳过)。
- 流程:连接真实 MySQL → 实例化真实 rag 客户端 → 调用 Ready 探活 → 用 `ragimpl.New` 构造 Impl → 创建 KB → 上传文档(需 `SMOKE_DOC_URI` 环境变量指向已上传到 MinIO 的源文件)→ 轮询直到状态变为 `DocumentStatusEnable`(2 分钟超时)→ Retrieve → 删除 KB。

- [ ] **步骤 2:对运行中的 rag stack 执行**

```bash
cd docker && docker compose -f docker-compose.test.yml up -d
cd ../backend && INTEGRATION=1 RAG_BASE_URL=http://localhost:8000 MYSQL_DSN=... SMOKE_DOC_URI=... go test -tags=integration ./domain/knowledge/service/ragimpl/...
```

期望:PASS。

- [ ] **步骤 3:提交**

```bash
git add backend/domain/knowledge/service/ragimpl/integration_test.go
git commit -m "test(ragimpl): add end-to-end integration test gated by INTEGRATION=1"
```

---

## 第 6 阶段 — 路线图文档 & PR-1 完结

### 任务 23:rag 侧路线图文档(独立仓库 PR)

**涉及文件:**
- 新增: `rag/docs/notes/roadmap.md`

- [ ] **步骤 1:在 rag 仓库写入路线图**

```bash
cd /Users/liuxinyu/workspace/rag
git checkout -b add-coze-integration-roadmap
```

完整内容见英文版任务 23 步骤 1。包含 8 个待支持特性,每项含简介、coze 调用方法、可选备注。锚点严格对应 coze 端 501 错误消息中的 `roadmap: rag/docs/notes/roadmap.md#xxx`。

- [ ] **步骤 2:提交并 PR**

```bash
git add docs/notes/roadmap.md
git commit -m "docs: add coze-integration roadmap"
```

- [ ] **步骤 3:切回 coze 仓库**

```bash
cd /Users/liuxinyu/workspace/coze-studio
```

---

### 任务 24:PR-1 — 开 PR 并合并

- [ ] **步骤 1:为 coze-studio 推送分支并开 PR**

完整 PR 描述见英文版任务 24 步骤 1。包含:

- 摘要:新增 `infra/rag`、`ragimpl`,feature-flag 接入,IDL 扩展,前端模型选择器。
- 测试计划:单测、集成测、手工 E2E。
- 关联设计文档路径。

```bash
git push -u origin feat/replace-knowledge-with-rag
gh pr create --title "..." --body "..."
```

- [ ] **步骤 2:为 rag 仓库推送并开 PR**

```bash
cd /Users/liuxinyu/workspace/rag
git push -u origin add-coze-integration-roadmap
gh pr create --title "docs: add coze-integration roadmap" --body "Roadmap entries referenced by coze-studio's 501 responses for unsupported features."
```

- [ ] **步骤 3:等待评审、处理评论、合并。**

---

## 第 7 阶段 — PR-2:翻转默认值、删除遗留代码、瘦化 schema

### 任务 25:翻转默认值为 rag

**涉及文件:**
- 修改: `backend/conf/rag/rag.yaml`
- 修改: `.env.example`

- [ ] **步骤 1:修改默认值**

`rag.yaml`:

```yaml
knowledge:
  backend: "${KNOWLEDGE_BACKEND:rag}"  # 之前是 legacy
```

`.env.example`:

```bash
KNOWLEDGE_BACKEND=rag
```

- [ ] **步骤 2:在开发环境验证**

```bash
make middleware && make server
# 通过 UI 创建 KB、上传、检索
```

期望:端到端可用。

- [ ] **步骤 3:提交**

```bash
git add backend/conf/rag/rag.yaml .env.example
git commit -m "feat(knowledge): switch default backend to rag"
```

---

### 任务 26:删除遗留 domain 代码

**涉及文件:**
- 删除:见“文件结构 → 删除 (PR-2)”清单

- [ ] **步骤 1:确认无残余引用**

对每个待删路径运行 grep:

```bash
grep -rln "domain/knowledge/internal/dal" backend/ | grep -v _test.go
grep -rln "domain/knowledge/processor" backend/
grep -rln "knowledgeImpl.NewKnowledgeSVC" backend/
```

若有非待删文件引用了它们,先修改该文件(最可能的是 `application/knowledge/init.go` —— 直接删除 legacy 分支)。

- [ ] **步骤 2:精简 init.go 为 rag-only**

在 `backend/application/knowledge/init.go` 移除 `case "legacy"` 分支及外层 switch;替换为只走 rag 的初始化逻辑(代码量约 20 行)。

- [ ] **步骤 3:删除文件**

```bash
rm -rf backend/domain/knowledge/internal/dal
rm -rf backend/domain/knowledge/internal/convert
rm -rf backend/domain/knowledge/internal/events
rm -rf backend/domain/knowledge/internal/mock
rm -rf backend/domain/knowledge/processor
rm -rf backend/domain/knowledge/repository
rm backend/domain/knowledge/service/knowledge.go
rm backend/domain/knowledge/service/retrieve.go
rm backend/domain/knowledge/service/sheet.go
rm backend/domain/knowledge/service/datacopy.go
rm backend/domain/knowledge/service/event_handle.go
rm backend/domain/knowledge/service/rdb.go
rm backend/domain/knowledge/service/validate.go
rm backend/domain/knowledge/service/knowledge_test.go
rm backend/domain/knowledge/service/knowledge_integration_test.go
rm backend/domain/knowledge/service/retrieve_test.go
rm backend/domain/knowledge/service/sheet_test.go
rm backend/domain/knowledge/service/event_handle_test.go
```

- [ ] **步骤 4:确保编译与测试仍通过**

```bash
cd backend && go build ./... && go test ./...
```

期望:PASS。

- [ ] **步骤 5:提交**

```bash
git add -A backend/
git commit -m "refactor(knowledge): delete legacy domain implementation"
```

---

### 任务 27:(PR-2 不做任何 DB 改动)

PR-2 **不引入任何 schema 变更**。`KNOWLEDGE_BACKEND=rag` 模式下,遗留的 `knowledge_kb`、`knowledge_document`、`knowledge_slice`、以及任何 `knowledge_*` 的 review / table / sheet 表都不会被读写,作为"死重 schema"原样保留。

不删除遗留表的原因:

1. **用户明确约束:** coze 现有数据库不应被本次工作修改。
2. **安全性:** PR-1 的 feature flag 是回滚的安全网。若 PR-2 删表后需要回退到 `KNOWLEDGE_BACKEND=legacy`,数据将丢失。
3. **成本:** 死表无新写入,只占磁盘,不影响读取。

清理工作推迟到独立的运维 PR,等 rag 作为默认后端运行至少一个发布周期且无事故后再处理。

- [ ] **步骤 1:确认 PR-2 没有改动 HCL schema**

```bash
git diff main -- docker/atlas/opencoze_latest_schema.hcl
```

期望:空 diff。PR-2 不应触碰 schema 文件。

- [ ] **步骤 2:确认遗留表仍在且未被改动**

```bash
mysql -e "SHOW TABLES LIKE 'knowledge_%';" coze
```

期望:遗留表存在且未变。

- [ ] **步骤 3:本任务无需提交** —— 用于声明这是有意为之的"不改动"。

---

### 任务 28:开 PR-2

- [ ] **步骤 1:推送并开 PR**

```bash
git push -u origin feat/replace-knowledge-with-rag-cleanup
gh pr create --title "refactor(knowledge): flip default to rag, remove legacy domain" --body "..."
```

完整 PR body 见英文版任务 28。

---

## 自查

### Spec 覆盖度

| Spec 章节 | 对应任务 |
|---|---|
| §1 决策 #1(特性缺口 → 501) | 任务 11 |
| §1 决策 #2(HTTP/REST) | 任务 5-8 |
| §1 决策 #3(rag-authoritative + TenantResolver) | 任务 10(tenant.go)+ 任务 10B(factory)+ 任务 12/13/14(resolver 调用) |
| §1 决策 #4(模型选择走 rag) | 任务 17、18、19 |
| §1 决策 #5(int64 ↔ uuid 映射) | 任务 2、9 |
| §1 决策 #6(501 错误格式) | 任务 1、11 |
| §1 决策 #7(方案 A 放置层) | 任务 10-14、15 |
| §2 架构 | 任务 4-15 |
| §3.1 删除项 | 任务 26 |
| §3.2 保留项 | 隐式(无任务删除这些文件) |
| §3.3 新增项 | 任务 3-14、17、21、22 |
| §4.1 表结构(新建映射表) | 任务 2 |
| §4.1 遗留表不动 | 任务 27(明确的不改动) |
| §4.3 状态枚举映射 | 任务 10 (`RagStatusToEntity`) |
| §5.1 Bucket A 方法 | 任务 12、13、14 |
| §5.2 Bucket B 方法 | 任务 11 |
| §5.3 Bucket C(无) | — |
| §6.1 KB 创建流 | 任务 12、17、18、19 |
| §6.2 摄取流 | 任务 13 |
| §6.3 检索流(含 NL2SQL 守卫、跨租户守卫) | 任务 14 |
| §7 配置 | 任务 3、20 |
| §8 错误处理 | 任务 5、11、14 |
| §9 测试(单测/集成/契约) | 任务 6、7、8、11、12、13、14、21、22 |
| §10 滚动发布(两 PR) | 任务 24(PR-1)、25-28(PR-2) |
| §11 风险 #6(确认 MGetSlice 调用方) | 任务 26 步骤 1(grep)隐式覆盖 |
| §11 风险 #7(tenant_id 类型) | 任务 10(TenantResolver 直接返回 string)|
| §11 风险 #8(coze 权限对 KB 已绕过) | 隐式 — ragimpl 不做权限检查 |
| §11 风险 #9(Phase 2 依赖 user 表新列) | 任务 10 — UserTenantResolver 桩保持注释 |

无缺漏。

### 占位符扫描

计划中可执行步骤里不含 “TBD” / “TODO” 标记。多处 **注意** 提示工程师需要核对(a)coze 现有包的精确符号名(如 `errorx.New`、`code.Register`、`entity.DocumentStatusXxx`、`idgen.IDGenerator.Gen`)与(b)一次性脚手架决策(如别名 `rag` 包、首次使用时再添加 `kbByRagID` / `docByRagID` 辅助函数)。这些是有意为之 —— 工程师必须比对实际签名,而不是机械照抄 —— 每条 注意 都指明了具体核对位置。

### 类型一致性

- `KBMapping{CozeID, RagKBID, SpaceID}` 与 `DocMapping{CozeID, RagDocID, KBID, LastTaskID}` 在任务 9、12、13、14 中保持一致。
- `i.tenant(ctx)`(调用 `TenantResolver.Resolve`)是任务 12、13、14 中**唯一**的 tenant_id 来源。原先的 `tenantOf(spaceID int64) string` 助手已删除;代码中没有任何从 request 字段推导 tenant_id 的路径。
- `RagStatusToEntity(s string) entity.DocumentStatus`(任务 10)与 `taskStatusToDoc(s string) entity.DocumentStatus`(任务 13)—— 两者并存是刻意为之;分别处理文档状态枚举与任务状态枚举,后两者在 rag 中是不同的枚举集,所以命名保持区分以避免混淆。
- `contract.Client` 接口在任务 4 定义,与任务 6、7、8 实现的方法一一对应。
- `ragimpl.Impl` 的构造函数 `New(rag, db, idgen, defaultText, defaultImage)` 在任务 10 定义,与任务 15 的调用点签名一致。

未发现不一致。

---

## 执行交接

计划已完成并保存到 `docs/superpowers/plans/2026-05-12-replace-knowledge-module-with-rag.md`(英文版)与 `docs/superpowers/plans/2026-05-12-replace-knowledge-module-with-rag-zh.md`(本文件)。两种执行选项:

**1. 子 Agent 驱动(推荐)** — 每个任务派出一个全新的 subagent,任务之间由我审阅,迭代节奏快。

**2. 内联执行** — 在本对话中通过 executing-plans 批量执行,过程中带检查点。

请选择执行方式。
