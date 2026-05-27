# Replace coze-studio Knowledge Module with the rag Service

Status: draft for review
Date: 2026-05-12
Authors: xinyu.liu@vorteklab.com (+ Claude)

## 0. Summary

Replace coze-studio's in-tree knowledge module (Go, ~11K LOC under `backend/{application,domain,crossdomain}/knowledge`) with the standalone `rag` service (Python/FastAPI + Celery), called over HTTP. Keep coze-studio's existing IDL and frontend contract intact. Features that rag does not yet support return HTTP 501 with a structured error; those gaps are tracked in rag's roadmap so they can be added later without further coze-side change.

Green-field deployment — no data migration.

## 1. Locked decisions (from brainstorming)

| # | Decision |
|---|---|
| 1 | Keep coze-studio's UI/IDL contract intact. Remove the Go implementation of the knowledge domain. Unsupported features return 501. Capture them in `rag/docs/notes/roadmap.md`. |
| 2 | Integration mode: HTTP/REST between coze-studio (Go) and rag (Python). Two separate services. |
| 3 | **rag-authoritative tenancy model.** Phase 1: a single global rag `tenant_id` is configured via `RAG_TENANT_ID` env var; every coze user sees every KB in that tenant. Resolution goes through a `TenantResolver` interface so Phase 2 can read `tenant_id` from a per-user attribute without further architectural change. coze's `space_id` / `app_id` / `creator_id` are **no longer used for KB isolation** — they continue to exist on the mapping table as informational audit columns only. |
| 4 | Model selection: rag is the source of truth for model providers. coze's create-KB UI calls rag's `/model_providers` and forwards the user's choice. coze's own model config continues to govern non-RAG things (LLM in workflows, agent reasoning, etc.). |
| 5 | ID mapping: coze keeps int64 IDs in its own DB and maps to rag's string UUIDs via **dedicated new mapping tables** (`rag_kb_mapping`, `rag_doc_mapping`). coze's existing `knowledge_*` tables are **not modified**. Mapping rows hold only: int64↔UUID, `icon_uri` (coze-only display), informational `app_id` / `creator_id`, and timestamps. KB name/description/status come from rag in real time. |
| 6 | Unsupported-feature behavior: a single new error `errno.ErrFeaturePendingRagSupport`; handler renders HTTP 501 + structured body with a roadmap pointer the frontend can branch on. |
| 7 | Architecture placement: **Approach A** — replace at `domain/knowledge/service` with a new `ragimpl` package. The wider `service.Knowledge` interface stays; unsupported methods return 501 in-place. |

## 2. Architecture

Two services, separate processes, HTTP between them.

```
┌──────────────────────────── coze-studio (Go) ────────────────────────────┐
│  frontend (React)                                                         │
│     │  thrift IDL (unchanged except optional model-id fields on CreateKB) │
│     ▼                                                                     │
│  api/handler/coze/knowledge_service.go    (unchanged)                     │
│     │                                                                     │
│     ▼                                                                     │
│  application/knowledge/knowledge.go       (lightly trimmed)               │
│   • coze-side concerns: event-bus publish, permission, icon URI,          │
│     int64↔uuid mapping lookup, status mirroring                           │
│     │  service.Knowledge interface                                        │
│     ▼                                                                     │
│  domain/knowledge/service/ragimpl   ← NEW (replaces old service impl)     │
│   • implements service.Knowledge by calling rag over HTTP                 │
│   • for unsupported methods → returns errno.ErrFeaturePendingRagSupport   │
│     │  contract/rag.Client interface                                      │
│     ▼                                                                     │
│  infra/rag/                         ← NEW                                 │
│   • thin HTTP client; one method per rag endpoint; auth header injection  │
│   • zero business logic                                                   │
│     │                                                                     │
│     └─── HTTP ──────────────────────────────────────────────┐             │
└─────────────────────────────────────────────────────────────│─────────────┘
                                                              │
┌─────────────────────────────────────────────────────────────▼─────────────┐
│ rag (Python/FastAPI + Celery)                                             │
│  app/api/routes/{knowledgebases,documents,retrieval,tasks,...}            │
│  app/policy ▶ app/services ▶ app/executors ▶ app/infrastructure          │
│  MongoDB · Elasticsearch · Redis · MinIO · Celery worker                  │
└───────────────────────────────────────────────────────────────────────────┘
```

The `crossdomain/knowledge.Knowledge` interface (consumed by workflow, agent, app, search, permission) is **unchanged**. Those consumers see no behavioral change beyond 501s on methods they typically don't call (Retrieve / MGetSlice / Store / Delete / MGetDocument / ListKnowledgeDetail — all bucket A).

## 3. Code placement

### 3.1 Delete (replaced by rag service)

- `backend/domain/knowledge/internal/dal/` — MySQL/Milvus access for chunks/documents/slices
- `backend/domain/knowledge/internal/{convert,events,mock}/` — coupled to deleted DAL
- `backend/domain/knowledge/processor/` — ingestion processor (rag's Celery worker replaces it)
- `backend/domain/knowledge/service/{knowledge.go, retrieve.go, sheet.go, datacopy.go, event_handle.go, rdb.go, validate.go}`
- `backend/domain/knowledge/service/*_test.go` — tests of deleted code
- `backend/domain/knowledge/repository/`

### 3.2 Keep

- `backend/domain/knowledge/entity/` — pure value/entity types stay (entity.Document, entity.Slice, entity.RetrievalStrategy, entity.DocumentStatus, etc.)
- `backend/domain/knowledge/service/interface.go` — the public interface; method signatures stay unchanged so `application/knowledge/knowledge.go` and other handlers do not need to change. Bucket-B methods return 501 in the new impl; their signatures and parameter types stay intact.
- `backend/crossdomain/knowledge/{contract.go, model/, impl/}` — unchanged
- `backend/application/knowledge/{knowledge.go, convertor.go, init.go}` — lightly edited to swap the implementation and drop references to deleted internals

### 3.3 Add

| Path | Purpose |
|---|---|
| `backend/infra/contract/rag/client.go` | Interface defining the rag client surface (every rag endpoint as one Go method) — depended on by ragimpl |
| `backend/infra/rag/client.go` | Concrete HTTP client implementation; auth header injection, timeout per call class, structured logging |
| `backend/infra/rag/errors.go` | Error mapping from rag error codes → coze `errno` codes |
| `backend/domain/knowledge/service/ragimpl/knowledge.go` | KB-related methods (Create/Update/Delete/List/Get/MGet) |
| `backend/domain/knowledge/service/ragimpl/document.go` | Document-related methods (Create/Delete/List/MGet/MGetProgress) |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | Retrieve, including tenant-scope safety check and result translation |
| `backend/domain/knowledge/service/ragimpl/mapping.go` | int64 ↔ uuid mapping helpers (reads/writes `rag_kb_mapping` / `rag_doc_mapping` rows) |
| `backend/domain/knowledge/service/ragimpl/tenant.go` | `TenantResolver` interface + `EnvTenantResolver` (Phase 1) reading `RAG_TENANT_ID` env. Stub for `UserTenantResolver` (Phase 2) included as commented future code. |
| `backend/domain/knowledge/service/ragimpl/unsupported.go` | All bucket-B methods returning `errno.ErrFeaturePendingRagSupport` with per-method roadmap pointer |
| `backend/domain/knowledge/service/ragimpl/factory.go` | Constructor wiring the rag client, mapping repo, and config |
| `backend/types/errno/rag.go` | `ErrFeaturePendingRagSupport`, `ErrUpstreamRagUnavailable`, `ErrCrossTenantRetrieval` |
| `backend/conf/rag/rag.yaml` | Base URL, timeouts, auth token, retry policy |
| `docker/atlas/opencoze_latest_schema.hcl` | (**modified**) Append `table "rag_kb_mapping"` and `table "rag_doc_mapping"` HCL blocks. Atlas auto-generates the corresponding `.sql` migration under `docker/atlas/migrations/`. |
| `rag/docs/notes/roadmap.md` | (rag repo) Roadmap entries for the 7 deferred features |

## 4. Data model

### 4.1 coze-studio MySQL (new mapping tables, existing tables untouched)

The rag path uses two **new** tables. coze's existing `knowledge_kb`, `knowledge_document`, `knowledge_document_slice`, etc. are **never read or written** by the rag path; they remain as-is so that switching back to legacy (or running mixed-mode in CI) keeps working.

**Schema is declared in Atlas HCL,** not hand-written SQL. The authoritative file is `docker/atlas/opencoze_latest_schema.hcl`. The `*.sql` migration under `docker/atlas/migrations/` is auto-generated by `atlas migrate diff` from the HCL diff — engineers do not hand-write it. Conventions match the project's existing knowledge tables (`created_at` / `updated_at` are `bigint unsigned` millisecond timestamps; `deleted_at` is `datetime(3) null`).

Logical schema (kept deliberately thin — rag is the source of truth for everything except coze-only display fields):

| Table | Columns |
|---|---|
| `rag_kb_mapping` | `coze_kb_id` (PK, bigint unsigned), `rag_kb_id` (varchar(64), unique), `icon_uri` (varchar(255) null — coze-only display), `app_id` (bigint unsigned default 0 — informational filter), `creator_id` (bigint unsigned default 0 — informational), `created_at` (bigint unsigned ms), `deleted_at` (datetime(3) null). Indexes: unique `uk_rag_kb_id`, `idx_app (app_id, deleted_at)`. |
| `rag_doc_mapping` | `coze_doc_id` (PK), `rag_doc_id` (varchar(64), unique), `coze_kb_id` (bigint unsigned, FK semantics), `creator_id` (informational), `last_task_id` (varchar(64) null — rag task tracking), `created_at`, `deleted_at`. Indexes: unique `uk_rag_doc_id`, `idx_kb (coze_kb_id, deleted_at)`. |

**Deliberately not stored on the coze side** (queried live from rag every time): KB / document name, description, status, format_type, embedding model ids, all chunk configuration, source_uri. These are rag's authoritative data and any mirror in coze would risk drift.

The exact HCL appears in the implementation plan, Task 2.

**No existing-table changes.** `knowledge_kb`, `knowledge_document`, `knowledge_document_slice`, `knowledge_document_review`, and any other `knowledge_*` tables are not modified or dropped by this work. Under `KNOWLEDGE_BACKEND=rag` they become dead-weight schema — never queried, never written. Cleanup of legacy tables (optional removal of those HCL `table` blocks) is left as a future housekeeping PR; it is **not** part of either PR-1 or PR-2.

### 4.2 rag MongoDB (unchanged)

Owns `knowledgebases`, `documents`, `chunks`, `tasks`, `model_providers`. Mongo's `tenant_id` field equals coze's `space_id` (stringified at the boundary if rag's tenant_id type is string).

### 4.3 Status enum mapping

| rag status | coze `entity.DocumentStatus` |
|---|---|
| `pending` | `DocumentStatusInit` |
| `processing` | `DocumentStatusProcessing` |
| `ready` | `DocumentStatusEnable` |
| `failed` | `DocumentStatusFailed` |

The application layer translates on every `MGetDocumentProgress` and on document `Get`/`List`; coze's `knowledge_document.status` column mirrors the result of the latest poll.

## 5. API mapping

Every method on `domain/knowledge/service.Knowledge` falls into one of three buckets.

### 5.1 Bucket A — delegates to rag

| coze method | rag endpoint | Notes |
|---|---|---|
| `CreateKnowledge` | `POST /knowledgebases` | Insert into `knowledge_kb` after rag returns kb_id |
| `UpdateKnowledge` | `PATCH /knowledgebases/{rag_kb_id}` | name / description / status; embedding-model fields rejected (immutable in rag) |
| `DeleteKnowledge` | `DELETE /knowledgebases/{rag_kb_id}` | Soft-delete coze row first; rollback on rag failure |
| `ListKnowledge` | `GET /knowledgebases?tenant_id=...` | Join with coze MySQL for app_id / creator_id filters |
| `GetKnowledgeByID` | `GET /knowledgebases/{rag_kb_id}` | Hydrate with coze-side fields (icon_uri, app_id, creator_id) |
| `MGetKnowledgeByID` | Loop `GET /knowledgebases/{id}` (sequential for v1) | Roadmap item: batch endpoint |
| `CreateDocument` | `POST /knowledgebases/{rag_kb_id}/documents` | Insert `knowledge_document` mapping after rag returns doc_id+task_id |
| `DeleteDocument` | `DELETE /documents/{rag_doc_id}` | Soft-delete coze row first |
| `ListDocument` | `GET /knowledgebases/{rag_kb_id}/documents` | Joined with coze mapping for name/status |
| `MGetDocument` | `GET /documents/{id}` per id (sequential for v1) | |
| `MGetDocumentProgress` | `GET /tasks/{task_id}` per doc's `last_task_id` | Mirrors status into coze row |
| `Retrieve` | `POST /retrieval` | Supported. **Exception:** if `Strategy.EnableNL2SQL=true`, ragimpl rejects with `ErrFeaturePendingRagSupport` before calling rag (NL2SQL is not in rag's scope). See flow §6.3 |

### 5.2 Bucket B — returns 501 (unsupported in rag's current scope)

| coze method | Reason | Roadmap entry |
|---|---|---|
| `UpdateDocument` | doc metadata is immutable post-ingest in current rag scope | "doc metadata update" |
| `ResegmentDocument` | re-vectorization explicitly out of scope | "re-segmentation / re-vectorization" |
| `CreateSlice`, `UpdateSlice`, `DeleteSlice`, `ListSlice`, `GetSlice`, `MGetSlice`, `ListPhotoSlice` | manual slice CRUD not in rag's 7-flow scope | "manual chunk CRUD" |
| `GetAlterTableSchema`, `ValidateTableSchema`, `GetDocumentTableInfo`, `GetImportDataTableSchema` | table/sheet docs not in rag's source-modality set | "table / sheet ingestion" |
| `ExtractPhotoCaption` | photo caption extraction not a standalone rag op | "photo caption extraction" |
| `CreateDocumentReview`, `MGetDocumentReview`, `SaveDocumentReview` | review workflow not in rag scope | "document review workflow" |
| `CopyKnowledge`, `MoveKnowledgeToLibrary` | rag has no copy/move primitive | "KB copy / move" |

Every 501 method's return wraps `errno.ErrFeaturePendingRagSupport` with a `Detail` field carrying a roadmap pointer such as `"rag/docs/notes/roadmap.md#manual-chunk-crud"`. The application/handler layer maps to HTTP 501 with a structured body the frontend can use to display a "coming soon" state.

### 5.3 Bucket C — stays coze-local

None. Every method either delegates or 501s. Coze-side concerns (event-bus emission, permission checks, icon-URI lookup, mapping-table writes) happen in `application/knowledge/knowledge.go` **around** the delegation call.

## 6. Flows

### 6.1 KB creation (with model selection)

```
[Frontend create-KB modal]
   │  1. GET /api/rag/model_providers
   │     coze handler proxies → GET /model_providers (rag)
   │     ◄── { text_models: [...], image_models: [...] }
   │
   │  2. User picks name, format_type, text_model_id, image_model_id, icon
   │
   │  3. POST /api/knowledge_dataset/create
   │     (CreateDatasetRequest extended with optional text/image embedding model ids)
   ▼
KnowledgeApplicationService.CreateDataset
   ▼
service.Knowledge.CreateKnowledge  (ragimpl)
   │  4. POST /knowledgebases (rag)
   │     body: { name, description, tenant_id=resolver.Resolve(ctx),
   │             text_embedding_model_id, image_embedding_model_id,
   │             enabled_chunk_types, supported_source_modalities, ... }
   │     ◄── { kb_id }
   │  5. INSERT rag_kb_mapping (coze_kb_id=snowflake, rag_kb_id, icon_uri, app_id, creator_id, created_at)
   │     (no name/description/status — those live in rag)
   ▼
   ◄── { knowledge_id: <int64> }
```

IDL change: `dataset.CreateDatasetRequest` gets two optional fields, `text_embedding_model_id` and `image_embedding_model_id`. If absent, the application layer fills in system-configured defaults from `rag.yaml` (`default_text_embedding_model_id`, `default_image_embedding_model_id`) before forwarding.

### 6.2 Ingestion (async)

```
[Frontend upload]
   │  POST /api/document/create (binary or pre-signed URI)
   ▼
KnowledgeApplicationService.CreateDocument
   │  upload to MinIO via existing infra/storage → source_uri
   ▼
service.Knowledge.CreateDocument (ragimpl)
   │  POST /knowledgebases/{rag_kb_id}/documents
   │     body: { source_uri, source_modality, parsing_strategy, chunking_strategy, metadata }
   │     ◄── { doc_id, task_id, status: "pending" }
   │  INSERT rag_doc_mapping (..., last_task_id=task_id, status=Init)
   ▼
   ◄── { document_id: <int64>, status: "pending" }

[Frontend polls progress]
   GET /api/document/progress → MGetDocumentProgress
        for each doc: GET /tasks/{last_task_id} (rag)
        translate rag status → coze enum; UPDATE rag_doc_mapping.status
        return progress list
```

Invariants inherited from rag:
- A document is searchable only when `status=ready` AND its task is `success`.
- Status flow is one-way except for re-ingestion (not in v1 scope).

### 6.3 Retrieval (single-KB and cross-KB)

```
[Workflow knowledge node / Agent retriever / Bot]
   │  service.Knowledge.Retrieve(ctx, RetrieveRequest{
   │      Query, KnowledgeIDs[], DocumentIDs[], Strategy{...}
   │  })
   ▼
ragimpl.Retrieve
   │
   │  1. SELECT rag_kb_id FROM rag_kb_mapping WHERE coze_kb_id IN (?) AND deleted_at IS NULL
   │     - Tenant comes from resolver.Resolve(ctx) — NOT inferred from rows
   │     - No coze-side cross-tenant verification: rag enforces tenant scope from its side
   │       (a KB ID that maps to a UUID in a different tenant just yields no hits)
   │     - Translate coze DocumentIDs → rag doc_ids via rag_doc_mapping
   │
   │  2. Map coze Strategy → rag request body:
   │       TopK, MinScore, MaxTokens          → top_k, min_score, max_tokens
   │       SearchType {semantic|fulltext|hybrid} → search_type
   │       EnableQueryRewrite                 → query_strategy.rewrite
   │       EnableRerank                       → rerank.enabled
   │       EnableNL2SQL                       → REJECT with 501
   │
   │  3. POST /retrieval (rag)
   │       body: { tenant_id: resolver.Resolve(ctx), kb_ids[], doc_ids?, query, query_mode: "text_input",
   │               top_k, min_score, search_type, rerank, ... }
   │     ◄── { hits: [{ chunk_id, doc_id, score, content, metadata, ... }],
   │            debug: { resolved_policy: {...} } }
   │
   │  4. Translate hits → entity.RetrieveSlice (rag doc_id → coze document_id via mapping)
   ▼
   ◄── RetrieveResponse{ RetrieveSlices: [...] }
```

- **Cross-KB:** handled natively by rag (`kb_ids: [a, b, c]`). Per-KB local recall + local rerank + business-layer merge happens **inside rag**; coze does not re-implement merging.
- **Cross-modal (text→image, image→text):** reachable via rag's query-transform → target-modality path; coze sets `query_mode` and `target_chunk_types`. If a particular pair is not yet expanded in rag's flow doc, ragimpl returns 501 with a pointer.
- **Tenant safety:** in Phase 1 (single global tenant) coze does not pre-check cross-tenant — there is exactly one tenant by construction. The `TenantResolver` returns the same value for every call. Phase 2 will keep the same call shape; the resolver simply returns a different value per user. rag itself enforces tenant scoping by filtering on `tenant_id` in the request body.

## 7. Configuration

`backend/conf/rag/rag.yaml`:

```yaml
rag:
  base_url: "http://rag-web:8000"
  timeout_ms: 10000
  upload_timeout_ms: 60000
  retrieval_timeout_ms: 15000
  max_retries: 2
  retry_backoff_ms: 200
  default_text_embedding_model_id: "${RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID}"
  default_image_embedding_model_id: "${RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID}"

knowledge:
  backend: "${KNOWLEDGE_BACKEND:legacy}"
  tenant:
    mode: "${RAG_TENANT_MODE:env}"               # env (Phase 1) | user (Phase 2)
    default_tenant_id: "${RAG_TENANT_ID:coze}"   # used when mode=env
```

`TenantResolver` is selected at startup by `tenant.mode`. Phase 1 = `env` (returns `default_tenant_id` for every request). Phase 2 = `user` (reads `user.rag_tenant_id` from the user repo — left as commented future code in `tenant.go`; will be activated by an unrelated future PR that adds the column to the `user` table).

Loaded once into `infra/rag.Client` at startup. **There is no service-to-service auth token between coze and rag:** rag's current design (`app/api/deps/auth_context.py`) explicitly delegates authentication "upstream" and ships no token-validation middleware. Tenant scoping is enforced by rag based on the body's `tenant_id`. Network isolation (Docker bridge / internal VPC) is the only barrier protecting rag — see §11 risk #1.

**Docker / compose:** `docker/docker-compose.yml` adds the rag stack (web, worker, MongoDB, Elasticsearch, Redis, MinIO) on the same Docker network as coze-studio. coze-studio's existing ES / Redis / MinIO instances stay; rag gets its own (separate ports, volumes) to keep deployment models independent and to match rag's standalone design.

**Health gating:** coze-studio's readiness probe waits for `GET {rag.base_url}/ready` → 200 before marking itself ready, so the knowledge module never serves traffic against a cold rag.

## 8. Error handling

### 8.1 Three classes

1. **rag returns 4xx** (param / capability / not-found): map by code.
   - rag `40001-40009` → coze `errno.ErrInvalidParam` / `ErrCapabilityMismatch`
   - rag `404xx` → coze `errno.ErrKnowledgeNotFound` / `ErrDocumentNotFound`
   - rag `409xx` → coze `errno.ErrConflict`
   The rag error message + code is preserved in the logged error chain.

2. **rag returns 5xx / network error / timeout:** retry per policy (max 2, exponential backoff) for **idempotent operations only** (GET, DELETE). Never auto-retry `POST /documents` (would risk duplicate ingestion tasks). On final failure → `errno.ErrUpstreamRagUnavailable`.

3. **Unsupported feature:** `errno.ErrFeaturePendingRagSupport` with per-method `Detail` (roadmap pointer). Handler renders HTTP 501 + structured body the frontend can branch on.

### 8.2 Idempotency

- `CreateKnowledge` / `CreateDocument`: not retried. Frontend's existing client request id rides through; if rag adds dedupe support later, the client id will line up.
- `DeleteKnowledge` / `DeleteDocument`: coze soft-deletes its row first, then calls rag. On rag failure, restore the coze row. Eventually-consistent path (rows soft-deleted in coze but still live in rag) is **out of v1 scope**; see §10 risk #2.

### 8.3 Logging

Every rag call logs: `coze_request_id`, `rag_request_id` (from response header), `tenant_id`, `kb_id` (when known), `method`, `path`, `latency_ms`, `status_code`. Failures additionally dump the rag error body.

## 9. Testing

- **Unit (infra/rag):** table-driven tests with `httptest` server; success / 4xx / 5xx / timeout per endpoint.
- **Unit (ragimpl):** uses a `contract/rag.Client` mock; verifies request mapping, ID translation, status mirroring, tenant-scope safety check, and that every bucket-B method returns the expected `ErrFeaturePendingRagSupport` with the right roadmap pointer.
- **Unit (application/knowledge):** existing tests adapted; mocks `service.Knowledge` so application-layer concerns (event bus, permission, icon URI, mapping-table writes) are tested independently from rag.
- **Integration:** `backend/domain/knowledge/service/ragimpl/integration_test.go`, runnable against a live rag container brought up by `docker compose -f docker/docker-compose.test.yml up rag-stack`. Covers: create KB → upload doc → poll until ready → retrieve → delete. Gated by `INTEGRATION=1`.
- **Contract check:** `tools/rag-contract-check` calls rag's `/openapi.json` and asserts the endpoint shapes the Go client expects are still present. Runs in CI for both repos to catch drift.

Coverage targets:
- ragimpl ≥ 80% (new core)
- infra/rag ≥ 70% (mostly transport)
- Every bucket-B 501 method must be exercised so its message string is real.

## 10. Rollout

Green-field — no data migration script.

1. **Land spec → writing-plans → executing-plans.**
2. **Two PRs:**
   - **PR-1:** new code (`infra/rag`, `ragimpl`, config, IDL extension for model selection, frontend create-KB model selectors) + **CREATE** of the two new mapping tables (`rag_kb_mapping`, `rag_doc_mapping`). Wired behind a feature flag `KNOWLEDGE_BACKEND=rag|legacy`, defaulting to `legacy`. Legacy domain Go code and legacy MySQL tables untouched.
   - **PR-2:** flip default to `rag`, delete legacy domain Go code listed in §3.1. **No schema changes** — legacy MySQL tables remain as dead-weight. Atomic.
3. **rag-side companion PR:** add `rag/docs/notes/roadmap.md` listing the 7 deferred features with their coze-side caller methods.

The feature flag lets operators stage: bring up rag containers, validate end-to-end with `KNOWLEDGE_BACKEND=rag` on staging, then flip prod.

## 11. Risks & open questions

1. **rag has no service-to-service authentication.** rag's middleware stack (`app/middlewares/`) contains logging / trace / request-context / exception handlers only — no auth check. Its `auth_context.py` is a documented stub that delegates authentication "upstream." Any process that can reach rag's port can call any endpoint with any `tenant_id` in the body. **Mitigation:** rag must run on an internal-only network (Docker bridge or private VPC), with its port never exposed to the host or to a public LB. Document this as a hard deploy constraint in `rag/README.md`. If rag is ever to be exposed beyond the trust boundary of coze, a token-check middleware must be added to rag in a follow-up — this work does not include that change.

2. **Status reconciliation drift.** If a status update is lost between rag and coze (e.g., network blip during poll), the coze mirror can lag. **Mitigation:** every `MGetDocumentProgress` re-pulls from rag, so no stale value is ever returned without a fresh read. A background reconciler is **v2 nice-to-have**, not v1.

3. **Cross-KB retrieval depends on rag's `kb_ids[]` batching.** rag's `/retrieval` already accepts `kb_ids[]`, so v1 is fine. If rag ever regresses to per-KB calls, coze would need a parallel-call layer.

4. **IDL extension for model selection** is a breaking change to `dataset.CreateDatasetRequest`. **Mitigation:** PR-1 lands IDL + frontend modal change together behind the feature flag; under `legacy`, the new fields are ignored.

5. **Permissions / app-resource events** continue to flow through coze's existing `eventbus` (NSQ). rag emits nothing here. This is correct — rag has no view of coze's permission model. Application layer keeps emitting on create/update/delete.

6. **Workflow / agent code paths** (`domain/workflow/internal/nodes/knowledge/*.go`, `domain/agent/.../node_retriever.go`, `node_tool_knowledge.go`) call `crossdomain/knowledge.Knowledge` and should keep working unchanged — they only invoke bucket-A methods (Retrieve, MGetSlice, Store, Delete, MGetDocument, ListKnowledgeDetail). Bucket-B methods surface primarily through the dataset UI, so flow execution should not regress. **Caveat:** `MGetSlice` is in bucket B because rag has no slice-level read — workflow/agent paths that call it (if any) will need to either be removed in PR-2 or accept 501. To-verify before PR-1: enumerate every caller of `MGetSlice` outside the dataset UI.

7. **rag's `tenant_id` type.** rag's KB schema shows `tenant_id` as a string. `TenantResolver` returns a `string` directly (no `strconv` boundary anymore now that `space_id` is no longer the source of truth).

8. **coze's permission system for knowledge is intentionally bypassed.** Under `KNOWLEDGE_BACKEND=rag`, the rag tenant is the only isolation primitive; coze's `domain/permission/resource_queryiers.go` knowledge resource branch becomes inert. Two consequences:
   - Workflow/agent flows that previously enforced "user X can only retrieve from KBs in their space" no longer do so — they retrieve from anything in the resolver's tenant.
   - The `event_bus` `PublishResources` calls in `application/knowledge` are **kept**, so `search` domain still indexes the resource events. But the events lose `space_id` as a meaningful filter.

   Phase 1 (single global tenant) makes this trivially safe — there's only one tenant. Phase 2 (per-user tenant) inherits the model that rag-level isolation is sufficient. If finer-grained ACL is ever required, it must be added to rag, not retrofitted into coze's permission domain.

9. **Phase 2 readiness depends on a separate piece of work** that's not in this plan: adding `rag_tenant_id` to the `user` table, plus an admin UI to assign users to tenants. The `UserTenantResolver` exists in code as a commented stub; flipping it on without the user-table column will cause runtime failures. PR-1's `tenant.mode=env` default must stay until that work lands.

## 12. Out of scope

- Data migration from coze's existing knowledge tables — green-field deployment.
- **Dropping the legacy knowledge_* tables** — deferred to a future housekeeping PR; not part of this work.
- Re-vectorization or any flow that requires changing a KB's bound embedding model.
- Implementing the bucket-B features in rag — captured in roadmap; future work.
- Replacing coze's other domains (workflow, agent, app, search, permission) — they continue to consume `crossdomain/knowledge.Knowledge` unchanged.
- Adding any authentication middleware to rag (service-to-service token, per-tenant token, mTLS, etc.) — see risk #1. Network isolation is the sole protection in this work; auth middleware is a follow-up.
