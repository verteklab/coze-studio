# R2-D backend: wire rag's capabilities / retry / document-parameter-schemas

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-14-r2c-retrieve-and-error-decoder-design.md` (R2-C)
**Successor:** R2-D-frontend (separate spec, deferred)
**Sibling slices:** R2-E (broader test scaffolding) — deferred

## 1. Motivation

The 2026-05-14 round-2 contract audit found that rag exposes three endpoints coze does not yet consume:

1. **`GET /api/v1/knowledgebases/{kb_id}/capabilities`** — describes what a specific KB supports (enabled chunk types, supported modalities, retrievers, query modes, defaults, request-overrideable fields). The audit memo's intent: drive the upload-wizard config from this response instead of hardcoded per-KB-type config in the Phase 1.5 wizard files.
2. **`POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/retry`** — retries a failed ingestion task, returning the same `UploadDocumentResponse` shape as `CreateDocument`. Currently `<UploadProgressPoll />` shows a disabled "联系管理员" button on failed uploads because `ragimpl` has no retry method to call.
3. **`GET /api/v1/document-parameter-schemas`** — the system-wide catalog of per-`schema_id` parameter forms (typed `parameters[]` with `name/type/group/default/min/max/ui_label/ui_component/advanced/internal`). The audit memo's intent: replace hardcoded copies of these parameter forms in the Phase 1.5 rag wizards (`add-rag/config.tsx` family).

R2-D as originally scoped spans both backend (wire the three endpoints through coze's Go layers) and frontend (rework wizards to consume capabilities/parameter-schemas, enable the retry button). This spec covers **R2-D-backend only** — wiring the rag-side capabilities through to ragimpl so a future R2-D-frontend slice has a clean handle to consume. The frontend wizard rework gets its own spec when the UI's exact shape is concrete.

## 2. Goals & non-goals

### Goals

- `infra/rag.Client` exposes three new methods: `GetCapabilities`, `RetryDocument`, `ListDocumentParameterSchemas`.
- `infra/contract/rag` defines DTOs that mirror rag's wire shapes byte-for-byte (top-level fields only; nested rag-internal sub-fields ignored when coze doesn't need to interpret them).
- `domain/knowledge/service/ragimpl.Impl` gains the same three methods, doing the coze→rag id mapping (for KB-scoped and doc-scoped calls) and passing the rag-side typed response through unchanged. Pass-through, not entity translation — translation belongs to R2-D-frontend where the UI's needs are known.
- httptest contract tests in `client_test.go` lock the wire shape so future drift fails a unit test rather than a smoke. Ragimpl unit tests verify the mapping-lookup wiring.
- All existing tests stay green.

### Non-goals

- `service.Knowledge` interface methods. `ragimpl.Impl`'s method set is a Go superset of the interface; the new methods are reachable via direct `*Impl` calls or future interface additions. The interface stays untouched to avoid speculative DTO design.
- `application/knowledge` HTTP handlers, IDL definitions, thrift codegen. R2-D-frontend wires these.
- Frontend code — wizard rework, retry button enable, anything UI-facing. All deferred to R2-D-frontend.
- Coze-side entity types (`entity.KBCapabilities`, `entity.DocumentParameterSchema`). Defer until UI needs are concrete.
- Caching of capabilities or parameter-schemas responses. Both are read-mostly; if the wizard becomes hot, caching belongs at the application layer (where TTL semantics are clearest).
- Bucket-B stub UI hiding (project memory queued item #12) — capabilities could DRIVE this, but it's a separate consumer concern.
- R2-E's broader httptest scaffolding for endpoints outside R2-A through R2-D.

## 3. Contract change

### 3.1 Rag's authoritative shapes (verified live against rag `0e1f49b`)

**`GET /api/v1/knowledgebases/{kb_id}/capabilities`** envelope `data`:

```json
{
  "kb_id": "102df3b2-0952-4b4b-90be-94f3088ebfd4",
  "enabled_chunk_types": ["text_chunk", "image_chunk"],
  "supported_source_modalities": ["text_source", "image_source", "scanned_document_source"],
  "enabled_retrievers": ["dense", "bm25", "image_vector"],
  "supported_query_modes": ["text_input", "image_input"],
  "supported_search_types": ["dense", "bm25", "hybrid", "image_vector"],
  "metadata_schema": {},
  "filterable_fields": [],
  "retrievable_fields": [],
  "default_chunk_size": null,
  "default_chunk_overlap": null,
  "default_search_type": null,
  "default_candidate_k": null,
  "default_top_k": null,
  "default_fusion_policy": {"mode": "weighted_rrf", "rrf_k": 60, "weights": {"text": 0.6, "image": 0.4}},
  "retriever_defaults": {},
  "supported_query_strategies": ["rewrite", "expansion", "multi_query", "enable_rerank"],
  "request_overrideable_fields": ["query_mode", "search_type", "top_k", "candidate_k", "filters", "target_chunk_types", "retrievers", "fusion_policy", "retriever_params", "query_strategy"]
}
```

**`GET /api/v1/document-parameter-schemas`** envelope `data` (list):

```json
[
  {
    "schema_id": "text_document",
    "description": "Plain text paragraph processing parameters.",
    "file_types": ["txt", "text"],
    "source_modalities": ["text_source"],
    "parameters": [
      {
        "name": "merge_blank_line_paragraphs",
        "type": "boolean",
        "group": "text_paragraph",
        "required": false,
        "default": true,
        "allowed_values": [],
        "min_value": null,
        "max_value": null,
        "description": "Merge paragraphs separated by blank lines when packing chunks.",
        "ui_label": "Merge blank-line paragraphs",
        "ui_component": "switch",
        "advanced": false,
        "internal": false
      },
      ...
    ]
  },
  ...
]
```

**`POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/retry`** envelope `data`: identical shape to `CreateDocument`'s `UploadDocumentResponse` — `{doc_id, task_id, status}`.

### 3.2 Coze-side DTOs

New types in `backend/infra/contract/rag/types.go`:

**`KBCapabilities`** — mirrors all top-level fields. Nullable numeric defaults use pointer types so JSON `null` distinguishes "no default" from "default is zero."

| Field | Type | JSON tag |
|---|---|---|
| `KBID` | `string` | `kb_id` |
| `EnabledChunkTypes` | `[]string` | `enabled_chunk_types` |
| `SupportedSourceModalities` | `[]string` | `supported_source_modalities` |
| `EnabledRetrievers` | `[]string` | `enabled_retrievers` |
| `SupportedQueryModes` | `[]string` | `supported_query_modes` |
| `SupportedSearchTypes` | `[]string` | `supported_search_types` |
| `MetadataSchema` | `map[string]any` | `metadata_schema,omitempty` |
| `FilterableFields` | `[]string` | `filterable_fields` |
| `RetrievableFields` | `[]string` | `retrievable_fields` |
| `DefaultChunkSize` | `*int` | `default_chunk_size,omitempty` |
| `DefaultChunkOverlap` | `*int` | `default_chunk_overlap,omitempty` |
| `DefaultSearchType` | `*string` | `default_search_type,omitempty` |
| `DefaultCandidateK` | `*int` | `default_candidate_k,omitempty` |
| `DefaultTopK` | `*int` | `default_top_k,omitempty` |
| `DefaultFusionPolicy` | `FusionPolicy` | `default_fusion_policy` |
| `RetrieverDefaults` | `map[string]any` | `retriever_defaults,omitempty` |
| `SupportedQueryStrategies` | `[]string` | `supported_query_strategies` |
| `RequestOverrideableFields` | `[]string` | `request_overrideable_fields` |

The existing `FusionPolicy` type (used by `CreateKBRequest`) is reused.

**`DocumentParameterSchema`** + **`DocumentParameter`** — nested.

| `DocumentParameterSchema` field | Type | JSON tag |
|---|---|---|
| `SchemaID` | `string` | `schema_id` |
| `Description` | `string` | `description` |
| `FileTypes` | `[]string` | `file_types` |
| `SourceModalities` | `[]string` | `source_modalities` |
| `Parameters` | `[]DocumentParameter` | `parameters` |

| `DocumentParameter` field | Type | JSON tag |
|---|---|---|
| `Name` | `string` | `name` |
| `Type` | `string` | `type` |
| `Group` | `string` | `group` |
| `Required` | `bool` | `required` |
| `Default` | `any` | `default,omitempty` |
| `AllowedValues` | `[]any` | `allowed_values,omitempty` |
| `MinValue` | `*float64` | `min_value,omitempty` |
| `MaxValue` | `*float64` | `max_value,omitempty` |
| `Description` | `string` | `description` |
| `UILabel` | `string` | `ui_label` |
| `UIComponent` | `string` | `ui_component` |
| `Advanced` | `bool` | `advanced` |
| `Internal` | `bool` | `internal` |

`Default` and `AllowedValues` are `any` because their JSON type depends on the `Type` field (a boolean parameter's default is a bool; an integer parameter's is a number). R2-D-frontend narrows at consumption time.

**Retry response** — reuses the existing `contract.CreateDocumentResponse`. The wire shape is identical. No new type.

## 4. Architecture

### 4.1 Layer responsibilities

```
HTTP rag wire
  ↕
infra/rag.Client          ← thin HTTP shim; doJSON in/out; envelope decode
infra/contract/rag        ← wire-shape DTOs (KBCapabilities, DocumentParameterSchema, DocumentParameter)
domain/knowledge/service/ragimpl.Impl  ← mapping lookup + pass-through to rag client
                                          (NOT on service.Knowledge interface)
```

R2-D-frontend will layer above this:

```
service.Knowledge interface  ← R2-D-frontend adds method signatures + service DTOs
application/knowledge        ← R2-D-frontend adds HTTP handlers
IDL                          ← R2-D-frontend adds thrift definitions
frontend/...                 ← R2-D-frontend wires wizard + retry button
```

### 4.2 Flow: GetCapabilities

```
caller(cozeKBID) → ragimpl.GetCapabilities
  → tenant resolver → tenant
  → mapping.KBByCozeID(cozeKBID) → KBMapping{RagKBID}
  → rag.Client.GetCapabilities(tenant, RagKBID)
    → doJSON GET /api/v1/knowledgebases/{ragKBID}/capabilities
    ← envelope.data → *KBCapabilities
  ← *KBCapabilities (pass-through, no translation)
```

### 4.3 Flow: RetryDocument

```
caller(cozeDocID) → ragimpl.RetryDocument
  → tenant resolver → tenant
  → mapping.DocByCozeID(cozeDocID) → DocMapping{RagDocID, KBID(coze)}
  → mapping.KBByCozeID(DocMapping.KBID) → KBMapping{RagKBID}
  → rag.Client.RetryDocument(tenant, RagKBID, RagDocID)
    → doJSON POST /api/v1/knowledgebases/{ragKBID}/documents/{ragDocID}/retry
    ← envelope.data → *CreateDocumentResponse
  ← *CreateDocumentResponse
```

Two mapping lookups (doc-by-coze-id THEN kb-by-coze-id) because rag's retry URL needs the KB id in the path. Each is a single-row SELECT; not worth combining into a JOIN at this scale.

### 4.4 Flow: ListDocumentParameterSchemas

```
caller() → ragimpl.ListDocumentParameterSchemas
  → tenant resolver → tenant
  → rag.Client.ListDocumentParameterSchemas(tenant)
    → doJSON GET /api/v1/document-parameter-schemas
    ← envelope.data → []DocumentParameterSchema
  ← []DocumentParameterSchema
```

No mapping lookup — the rag endpoint is system-wide, not KB-scoped. (Verified: the path has no `{kb_id}` segment.) Tenant header is still required per rag's `Depends(build_request_context)` on every business router.

### 4.5 Touched files

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | Add `KBCapabilities`, `DocumentParameterSchema`, `DocumentParameter` types. |
| `backend/infra/rag/client.go` | Add `GetCapabilities`, `RetryDocument`, `ListDocumentParameterSchemas` methods. Each calls `doJSON` with the appropriate path + method + out target. |
| `backend/infra/rag/client_test.go` | Three new httptest contract tests (one per endpoint). |
| `backend/domain/knowledge/service/ragimpl/document.go` (or new file) | Add `RetryDocument` method. Document-scoped; lives alongside other document methods. |
| `backend/domain/knowledge/service/ragimpl/knowledge.go` (or wherever KB-scoped lives) | Add `GetCapabilities` method. KB-scoped. |
| `backend/domain/knowledge/service/ragimpl/parameter_schemas.go` (NEW file, OR existing) | Add `ListDocumentParameterSchemas` method. Not strongly tied to any existing file; placement is plan-time judgment. |
| `backend/domain/knowledge/service/ragimpl/document_test.go` and/or `knowledge_test.go` | Three new unit tests verifying mapping lookup + pass-through. |
| `backend/domain/knowledge/service/ragimpl/knowledge_test.go::fakeClient` | Add stub methods to satisfy the contract.Client interface for the three new endpoints. |

## 5. Components

### 5.1 Client methods

```go
// GetCapabilities fetches the rag-side capability descriptor for a KB.
// The response describes what's enabled / supported / default for this
// specific KB; the UI consumes it to drive wizard config and feature gating.
func (c *Client) GetCapabilities(ctx context.Context, tenantID, kbID string) (*contract.KBCapabilities, error) {
    out := &contract.KBCapabilities{}
    path := apiPrefix + "/knowledgebases/" + kbID + "/capabilities"
    if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
        return nil, err
    }
    return out, nil
}

// RetryDocument re-runs a failed ingestion task. Rag emits the standard
// UploadDocumentResponse, identical in shape to CreateDocument, so we reuse
// the existing CreateDocumentResponse type.
func (c *Client) RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
    out := &contract.CreateDocumentResponse{}
    path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID + "/retry"
    if err := c.doJSON(ctx, http.MethodPost, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
        return nil, err
    }
    return out, nil
}

// ListDocumentParameterSchemas returns rag's system-wide catalog of per-
// schema_id parameter forms. Rag's endpoint is global (no kb_id), so no
// mapping lookup is needed; the tenant header still travels per rag's
// invariant.
func (c *Client) ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]contract.DocumentParameterSchema, error) {
    var out []contract.DocumentParameterSchema
    path := apiPrefix + "/document-parameter-schemas"
    if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, &out, c.cfg.Timeout); err != nil {
        return nil, err
    }
    return out, nil
}
```

`doJSON` currently retries idempotent GETs and DELETEs but not POSTs. RetryDocument is POST and not auto-retried; the surrounding `ragimpl` layer doesn't retry either. If rag is genuinely down, the caller sees `ErrRagUpstreamUnavailableCode` (via R2-C's decoder) and decides whether to retry at the application layer.

### 5.2 Ragimpl methods

```go
func (i *Impl) GetCapabilities(ctx context.Context, cozeKBID int64) (*contract.KBCapabilities, error) {
    tenant, err := i.tenant(ctx)
    if err != nil {
        return nil, err
    }
    m, err := i.mapping.KBByCozeID(ctx, cozeKBID)
    if err != nil {
        return nil, err
    }
    return i.rag.GetCapabilities(ctx, tenant, m.RagKBID)
}

func (i *Impl) RetryDocument(ctx context.Context, cozeDocID int64) (*contract.CreateDocumentResponse, error) {
    tenant, err := i.tenant(ctx)
    if err != nil {
        return nil, err
    }
    dm, err := i.mapping.DocByCozeID(ctx, cozeDocID)
    if err != nil {
        return nil, err
    }
    kb, err := i.mapping.KBByCozeID(ctx, dm.KBID)
    if err != nil {
        return nil, err
    }
    return i.rag.RetryDocument(ctx, tenant, kb.RagKBID, dm.RagDocID)
}

func (i *Impl) ListDocumentParameterSchemas(ctx context.Context) ([]contract.DocumentParameterSchema, error) {
    tenant, err := i.tenant(ctx)
    if err != nil {
        return nil, err
    }
    return i.rag.ListDocumentParameterSchemas(ctx, tenant)
}
```

Pass-through. No `entity.*` translation. No persistence side-effect (no mapping insert, no task tracking). RetryDocument intentionally does NOT update `rag_doc_mapping.last_task_id` even though rag's response includes a fresh `task_id`; the document mapping's `last_task_id` is set at upload time and represents the most recent ingestion attempt for the mapping row. **Open question deferred to plan-time:** whether retry should bump `last_task_id` to the new task_id so `MGetDocumentProgress` polls the right task. See §10.

### 5.3 `contract.Client` interface

`contract.Client` is an interface (verified at `backend/infra/contract/rag/client.go:28`). The concrete `*Client` in `infra/rag/client.go` satisfies it via `var _ contract.Client = (*Client)(nil)` at line 47. The `fakeClient` in `domain/knowledge/service/ragimpl/knowledge_test.go` also satisfies the interface (used by ragimpl tests to inject controlled responses).

R2-D-backend MUST add three new method signatures to `contract.Client`:

```go
GetCapabilities(ctx context.Context, tenantID, kbID string) (*KBCapabilities, error)
RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*CreateDocumentResponse, error)
ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]DocumentParameterSchema, error)
```

And BOTH implementations (`*Client` and `fakeClient`) gain matching methods, otherwise the compile-time interface-satisfaction checks fail.

## 6. Data flow & invariants

- **No coze-side state changes.** All three methods are pure rag pass-throughs after a mapping lookup. No INSERT, no UPDATE, no MinIO read.
- **Mapping lookups are non-destructive.** A missing mapping row returns `ErrMappingNotFound` to the caller; no fallback fetch from rag.
- **Tenant header always travels.** Even ListDocumentParameterSchemas (no kb_id) sends `X-Tenant-Id` because rag's middleware requires it on every business endpoint.
- **Retry doesn't update `last_task_id`** (per current spec; see §10 open question). If the future R2-D-frontend wants progress polling to follow the retry's new task, it'll need explicit mapping mutation.

## 7. Error handling

| Scenario | Behavior |
|---|---|
| `mapping.KBByCozeID(...)` returns `ErrMappingNotFound` | Propagated unchanged to caller. |
| `mapping.DocByCozeID(...)` returns `ErrMappingNotFound` | Propagated unchanged. |
| Tenant resolver fails | Propagated unchanged. |
| rag returns 404 (unknown rag-side KB or doc) | R2-C's `DecodeErrorEnvelope` + `MapRagError` → `ErrKnowledgeNotExistCode` / `ErrKnowledgeDocumentNotExistCode`. |
| rag returns 422 (pydantic — e.g. retry on non-failed doc) | R2-C path → `ErrKnowledgeInvalidParamCode` with formatted detail message. |
| rag 5xx or non-JSON | R2-C path → `ErrRagUpstreamUnavailableCode`. |
| Retry on a doc that doesn't exist on rag's side (rare drift) | Same as rag 404 above. |

## 8. Testing

### 8.1 httptest contract tests (`backend/infra/rag/client_test.go`)

**`TestGetCapabilities_FieldShape`** — handler returns the full capability envelope (live shape verified). Assertions:
- Method = GET, path suffix `/api/v1/knowledgebases/kb-1/capabilities`, `X-Tenant-Id` header.
- Decoded struct exposes every scalar/slice/map field; lengths of slice fields match; non-nil pointer defaults when wire is non-null; nil pointer when wire is null.
- Sub-case for "all defaults present as numbers" exercises pointer-dereference path.

**`TestRetryDocument`** — handler asserts POST + path + tenant header. Returns `UploadDocumentResponse` envelope. Decoded response has `DocID`, `TaskID`, `Status`.

**`TestListDocumentParameterSchemas_FieldShape`** — handler returns a list of two schemas (text and image) with ≥2 parameters each, covering boolean/integer/string `Type` values, integer `MinValue`/`MaxValue` (pointer-decode), and at least one parameter where `AllowedValues` is non-empty. Assert the decoded list length, nested `Parameters` field count, and one representative value per parameter type.

### 8.2 Ragimpl unit tests

`backend/domain/knowledge/service/ragimpl/document_test.go` (or wherever fits the existing file structure):

**`TestRagimpl_GetCapabilities`** — uses `fakeClient` and the existing in-memory mapping (via `newTestImpl` helper). Insert a KB mapping (`InsertKB` with coze id → rag uuid). Set `fakeClient.getCapabilitiesFunc` to return canned data; assert ragimpl forwards correctly using the rag uuid.

**`TestRagimpl_RetryDocument`** — insert KB mapping + doc mapping. Set `fakeClient.retryDocumentFunc` to return canned `CreateDocumentResponse`. Assert: tenant resolved, doc mapping consulted, KB mapping consulted, client called with correct rag IDs.

**`TestRagimpl_ListDocumentParameterSchemas`** — no mapping needed. Set `fakeClient.listDocumentParameterSchemasFunc` to return a canned list. Assert: tenant resolver called once, pass-through correct.

### 8.3 `fakeClient` updates

`knowledge_test.go::fakeClient` is a test-only stub implementing `contract.Client`. With the three new methods added to the interface (§5.3), `fakeClient` must gain matching methods or the package fails to compile. Each:
- A method-stub field (`getCapabilitiesFunc`, `retryDocumentFunc`, `listDocumentParameterSchemasFunc`) lets tests inject controlled responses.
- The method body checks `if fn != nil { return fn(args...) }`; otherwise returns zero values. Matches the existing pattern for `getTaskFunc`, `createKBFunc`, etc.

### 8.4 Existing tests

`make middleware && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...` stays green.

### 8.5 Smoke (optional)

`curl` against rag is sufficient to verify the wire shapes (done during spec-writing). End-to-end UI smoke is meaningless for R2-D-backend — no consumer wired through application layer yet.

## 9. Compatibility & rollout

- Fully additive. No schema change, no IDL change, no frontend change, no behavior change for existing callers.
- Legacy backend (`KNOWLEDGE_BACKEND=legacy`) path untouched (legacy doesn't go through ragimpl).
- New methods on `*ragimpl.Impl` are not part of `service.Knowledge` interface; callers wanting to use them today must hold a concrete `*ragimpl.Impl`. The composition root in `application/knowledge/init.go` already exposes `KnowledgeSVC.DomainSVC` as `service.Knowledge`; reaching the new methods requires either type-asserting back to `*ragimpl.Impl` (ugly) or waiting for R2-D-frontend to widen the interface. R2-D-backend deliberately accepts this constraint — no production caller for the new methods exists yet.

## 10. Open questions

Two minor items deferred to plan-time:

1. **Retry's effect on `rag_doc_mapping.last_task_id`.** Today the field is set once at upload time. A retry produces a new task_id on the rag side. If `MGetDocumentProgress` is still polling the old `last_task_id` after retry, it will keep polling a long-finished failed task indefinitely. Options:
   - (a) **Leave alone for R2-D-backend** — explicit non-goal; R2-D-frontend will handle this when wiring the retry button. Caller becomes responsible for whatever bookkeeping is needed.
   - (b) **Add a `mapping.UpdateLastTaskID(ctx, cozeDocID, newTaskID)` helper** and call it from `ragimpl.RetryDocument`. Plumbs the state update into the right layer.
   - The spec recommends (a) for R2-D-backend (this slice is pass-through). Plan can reconsider if it proves awkward.

2. **`parameter_schemas.go` file placement.** Coze's `ragimpl/` package currently has `document.go`, `knowledge.go`, `retrieval.go`, `factory.go`, `mapping.go`, `tenant.go`, `unsupported.go`. `ListDocumentParameterSchemas` doesn't strongly fit any of them — it's not KB-scoped, not document-scoped. Options: a new `parameter_schemas.go` file, or fold into `document.go` as "related to document upload parameters." Plan-time judgment.
