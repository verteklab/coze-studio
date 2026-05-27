# Knowledge-Retrieve Param Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align the coze workflow knowledge-retrieve node's request payload with rag's `POST /api/v1/retrieval` schema — hide UI fields rag silently drops, expose rag capabilities the node never surfaced, and stop sending request keys rag would reject.

**Architecture:** Coze-side only changes. `RetrievalStrategy` struct becomes the single source of truth aligned with rag's vocabulary (4-boolean `query_strategy` + new `filters / target_chunk_types / retrievers / fusion_policy / retriever_params / query_image / query_mode`). The deletion of legacy fields (`MinScore / MaxTokens / EnableQueryRewrite / EnableRerank / EnableNL2SQL / IsPersonalOnly`) propagates to `KNOWLEDGE_BACKEND=legacy` (`domain/knowledge/service/retrieve.go`) and the agent retriever (`agent/singleagent/.../node_retriever.go`); those non-rag paths lose the corresponding capabilities — release note must call this out.

**Tech Stack:** Go (backend: workflow node + RetrievalStrategy + ragimpl + legacy retrieve.go + agentflow). TypeScript / React + vitest (frontend: dataset-setting component refactor + new section components).

**Spec:** `docs/superpowers/specs/2026-05-20-knowledge-retrieve-param-alignment-design.md`

---

## File Structure

### Backend — modify

- `backend/crossdomain/knowledge/model/knowledge.go` — `RetrievalStrategy` struct fields rewritten
- `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go` — `RetrieveConfig.Adapt` rewrite + `Retrieve.Invoke` drops `documentIDs`
- `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve_test.go` — tests rewritten
- `backend/domain/knowledge/service/ragimpl/factory.go` — drop `defaultLLMModelID / defaultRerankModelID` fields and constructor args
- `backend/domain/knowledge/service/ragimpl/retrieval.go` — simplify Retrieve
- `backend/domain/knowledge/service/ragimpl/retrieval_test.go` — rewrite for new contract
- `backend/domain/knowledge/service/ragimpl/knowledge_test.go` — remove `defaultLLMModelID / defaultRerankModelID` from fixtures
- `backend/domain/knowledge/service/ragimpl/integration_test.go` — drop `RAG_DEFAULT_*_MODEL_ID` env reads
- `backend/domain/knowledge/service/retrieve.go` (legacy) — drop dead branches referencing deleted fields
- `backend/domain/agent/singleagent/internal/agentflow/node_retriever.go` — drop deleted-field assignments
- `backend/conf/rag/config.go` — drop `DefaultLLMModelID / DefaultRerankModelID`
- `backend/application/knowledge/init.go` — drop these args from `ragimpl.New(...)`
- `backend/infra/contract/rag/types.go` — drop `DocumentIDs / MinScore / MaxTokens` fields; fix stale comment

### Frontend — modify / create / delete

- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/type.ts` — modify (DataSetInfo field rename / add / delete)
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/index.tsx` — refactor into orchestrator (<100 lines)
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/BasicSection.tsx` — create
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryEnhancementSection.tsx` — create
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryInputSection.tsx` — create
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/FilterSection.tsx` — create
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/AdvancedSection.tsx` — create
- `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/components/DocumentIDsSelect/index.tsx` — delete (entire dir)

---

## Task Order Rationale

Backend first, type definitions before consumers, then frontend. Reasoning:
- `RetrievalStrategy` struct is the contract; every consumer references it. Changing it first surfaces all compile errors in one wave.
- Wire-level transport (`backend/infra/contract/rag/types.go` `RetrieveRequest`) changes also need to land before `ragimpl/retrieval.go` rewires its forwarding.
- Frontend can be done independently but is gated on the new backend param names (`rewrite / expansion / multiQuery / enableRerank / filters / ...`) being live, so frontend ships in the same release.

---

### Task 1: Refactor `RetrievalStrategy` struct fields

**Files:**
- Modify: `backend/crossdomain/knowledge/model/knowledge.go:101-124`

- [ ] **Step 1: Edit the struct**

Replace `RetrievalStrategy` definition (lines 113-124) and remove the
no-longer-used `DocumentIDs` from `RetrieveRequest` (line 107).

`RetrieveRequest` becomes:

```go
type RetrieveRequest struct {
    Query       string
    ChatHistory []*schema.Message

    // Recall from the specified knowledge base
    KnowledgeIDs []int64

    // recall strategy
    Strategy *RetrievalStrategy
}
```

`RetrievalStrategy` becomes:

```go
type RetrievalStrategy struct {
    // top_k. Defaults are handled by rag/legacy backends, not here.
    TopK *int64

    SelectType SelectType
    SearchType SearchType

    // query_strategy 4 booleans (rag wire-level keys are
    // rewrite / expansion / multi_query / enable_rerank)
    Rewrite      bool
    Expansion    bool
    MultiQuery   bool
    EnableRerank bool

    // New top-level rag fields surfaced via the workflow node
    QueryImage       *QueryImage
    QueryMode        string
    TargetChunkTypes []string
    Filters          map[string]any
    Retrievers       []string
    FusionPolicy     map[string]any
    RetrieverParams  map[string]any
}

// QueryImage is the inline-or-reference image payload mirroring rag's
// ImageQueryDTO. At least one of ImageBase64 / ImageRef must be non-empty.
type QueryImage struct {
    ImageBase64 string
    ImageRef    string
}
```

Deleted fields: `MinScore`, `MaxTokens`, `EnableQueryRewrite`,
`EnableNL2SQL`, `IsPersonalOnly`. `EnableRerank` is preserved by name (was already present).
`EnableQueryRewrite` is **renamed** to `Rewrite`.

- [ ] **Step 2: Compile and capture caller breakage**

```bash
cd /home/xinyuliu/coze-studio && go build ./...
```

Expected compile errors (these become the inputs to Tasks 2-9):
- `agent/singleagent/internal/agentflow/node_retriever.go` — `MinScore`, `EnableQueryRewrite`, `EnableNL2SQL` assignments
- `domain/knowledge/service/retrieve.go` — references to deleted fields in `queryRewriteNode`, `nl2SqlRetrieveNode`, `reRankNode`
- `domain/knowledge/service/ragimpl/retrieval.go` — references to deleted fields
- `domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go` — `IsPersonalOnly`, `EnableNL2SQL`, `MinScore`, `DocumentIDs`, `EnableQueryRewrite` references
- multiple `_test.go` files

Do not fix yet; just confirm the breakage is contained to expected files.

- [ ] **Step 3: Commit (broken build)**

```bash
git add backend/crossdomain/knowledge/model/knowledge.go
git commit -m "refactor(knowledge): rewrite RetrievalStrategy to match rag schema

Deletes MinScore, MaxTokens, EnableQueryRewrite, EnableNL2SQL,
IsPersonalOnly. Renames EnableQueryRewrite -> Rewrite. Adds Expansion,
MultiQuery, QueryImage, QueryMode, TargetChunkTypes, Filters,
Retrievers, FusionPolicy, RetrieverParams. Drops DocumentIDs from
RetrieveRequest. Build is intentionally broken; subsequent tasks fix
each consumer."
```

> Note: committing a non-compiling tree is acceptable because the
> follow-up tasks are scoped to repair specific consumers — the
> commit message documents the intent. If the project's pre-commit
> hook rejects a non-compiling tree, squash Tasks 1-3 into one.

---

### Task 2: Fix `agentflow/node_retriever.go`

**Files:**
- Modify: `backend/domain/agent/singleagent/internal/agentflow/node_retriever.go:101-131`

- [ ] **Step 1: Remove deleted-field assignments**

In `genKnowledgeRequest`, strip `MinScore`, `EnableQueryRewrite`,
`EnableNL2SQL` from the `RetrievalStrategy{...}` literal. `EnableRerank`
is preserved (still present on the struct).

After edit, the `Strategy` literal becomes:

```go
Strategy: &knowledgeEntity.RetrievalStrategy{
    TopK: conf.TopK,

    SelectType: func() knowledgeModel.SelectType {
        if conf.Auto != nil && *conf.Auto {
            return knowledgeModel.SelectTypeAuto
        }
        return knowledgeModel.SelectTypeOnDemand
    }(),

    SearchType: func() knowledgeModel.SearchType {
        if conf.SearchStrategy == nil {
            return knowledgeModel.SearchTypeSemantic
        }
        switch *conf.SearchStrategy {
        case bot_common.SearchStrategy_SemanticSearch:
            return knowledgeModel.SearchTypeSemantic
        case bot_common.SearchStrategy_FullTextSearch:
            return knowledgeModel.SearchTypeFullText
        case bot_common.SearchStrategy_HybirdSearch:
            return knowledgeModel.SearchTypeHybrid
        default:
            return knowledgeModel.SearchTypeSemantic
        }
    }(),

    EnableRerank: conf.RecallStrategy != nil && conf.RecallStrategy.UseRerank != nil && *conf.RecallStrategy.UseRerank,
},
```

> The `UseRewrite` and `UseNl2sql` checks become **dead reads of `conf`**
> in this function. Leave `conf.RecallStrategy.UseRewrite` /
> `conf.RecallStrategy.UseNl2sql` field definitions untouched (they're
> generated from IDL and consumed elsewhere — agent settings UI etc.) —
> just stop **using** them here.

- [ ] **Step 2: Compile**

```bash
cd /home/xinyuliu/coze-studio && go build ./backend/domain/agent/singleagent/...
```

Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add backend/domain/agent/singleagent/internal/agentflow/node_retriever.go
git commit -m "refactor(agent): drop deleted RetrievalStrategy fields

Stops setting MinScore / EnableQueryRewrite / EnableNL2SQL on the
agent retriever request now that those fields are gone from
RetrievalStrategy. UseRewrite / UseNl2sql on conf.RecallStrategy stay
in the IDL — they're unused by this code path going forward."
```

---

### Task 3: Fix legacy `service/retrieve.go`

**Files:**
- Modify: `backend/domain/knowledge/service/retrieve.go` lines 180-201, 313-356, 510-547, 558-563

- [ ] **Step 1: Remove `queryRewriteNode` body**

Find `queryRewriteNode` (around line 180) and make it a pass-through
that no longer reads `EnableQueryRewrite`:

```go
func (k *knowledgeSVC) queryRewriteNode(ctx context.Context, req *RetrieveContext) (newRetrieveContext *RetrieveContext, err error) {
    // Legacy query rewriting is no longer driven by RetrievalStrategy.EnableQueryRewrite;
    // the rag backend handles its own query enhancement, and the legacy KB
    // backend stops rewriting here. Kept as a pass-through so the eino
    // composition graph still has the named lambda.
    return req, nil
}
```

- [ ] **Step 2: Remove `nl2SqlRetrieveNode` NL2SQL branch**

In `nl2SqlRetrieveNode` (around line 313-356) the function body currently
gates execution on `hasTable && req.Strategy.EnableNL2SQL`. Replace the
function body with a no-op return:

```go
func (k *knowledgeSVC) nl2SqlRetrieveNode(ctx context.Context, req *RetrieveContext) (retrieveResult []*schema.Document, err error) {
    // NL2SQL has been retired from the legacy retrieval path along with
    // RetrievalStrategy.EnableNL2SQL. Returning empty so the eino graph
    // edges that consume "nl2SqlRetrieveNode" still get a defined value.
    return nil, nil
}
```

> Do not delete `nl2SqlExec` if other files still reference it. Run
> `grep -rn "nl2SqlExec" backend/` and only delete unused helpers in a
> follow-up cleanup if completely dead.

- [ ] **Step 3: Fix `reRankNode` references to deleted fields**

In `reRankNode` (around line 510-570):

Remove the `if retrieveCtx.Strategy.EnableNL2SQL { ... }` block (lines
528-531) that conditionally appends `nl2SqlRetrieveResult`.

Remove the `if retrieveCtx.Strategy.EnableQueryRewrite && ... { query =
ptr.From(retrieveCtx.RewrittenQuery) }` (lines 545-547) — query is
always `retrieveCtx.OriginQuery`.

Remove the `if item.Score < ptr.From(retrieveCtx.Strategy.MinScore) {
continue }` filter (lines 560-563) — score filtering is no longer
applied at this layer.

Net diff in `reRankNode`:

```go
// before:
if retrieveCtx.Strategy.EnableNL2SQL {
    retrieveResultArr = append(retrieveResultArr, docs2RerankData(nl2SqlRetrieveResult))
}
// after: removed

// before:
query := retrieveCtx.OriginQuery
if retrieveCtx.Strategy.EnableQueryRewrite && retrieveCtx.RewrittenQuery != nil {
    query = ptr.From(retrieveCtx.RewrittenQuery)
}
// after:
query := retrieveCtx.OriginQuery

// before:
for _, item := range resp.SortedData {
    if item.Score < ptr.From(retrieveCtx.Strategy.MinScore) {
        continue
    }
    doc := item.Document
    doc.WithScore(item.Score)
    retrieveResult = append(retrieveResult, doc)
}
// after:
for _, item := range resp.SortedData {
    doc := item.Document
    doc.WithScore(item.Score)
    retrieveResult = append(retrieveResult, doc)
}
```

- [ ] **Step 4: Remove `nl2SqlRetrieveResult` extraction in `reRankNode` if `nl2SqlRetrieveNode` now always returns nil**

The map lookup `resultMap["nl2SqlRetrieveNode"]` will now hit a nil
slice; the variable is unused after Step 3 deletes the `EnableNL2SQL`
branch. Delete:

```go
nl2SqlRetrieveResult, ok := resultMap["nl2SqlRetrieveNode"].([]*schema.Document)
if !ok {
    logs.CtxErrorf(ctx, "nl2sql retrieve result is not found")
    nl2SqlRetrieveResult = []*schema.Document{}
}
```

- [ ] **Step 5: Compile**

```bash
cd /home/xinyuliu/coze-studio && go build ./backend/domain/knowledge/...
```

Expected: pass (legacy retrieve.go no longer references deleted fields).

- [ ] **Step 6: Commit**

```bash
git add backend/domain/knowledge/service/retrieve.go
git commit -m "refactor(knowledge-legacy): drop EnableQueryRewrite/EnableNL2SQL/MinScore

Legacy retrieval pipeline no longer reads these fields after they were
removed from RetrievalStrategy. queryRewriteNode + nl2SqlRetrieveNode
become pass-throughs; reRankNode drops the NL2SQL fanout, query
rewrite reading, and the per-item MinScore filter."
```

---

### Task 4: Drop env-default model id config

**Files:**
- Modify: `backend/conf/rag/config.go:44-55`
- Modify: `backend/application/knowledge/init.go:98-108`

- [ ] **Step 1: Remove fields from `Config`**

In `backend/conf/rag/config.go`, delete `DefaultLLMModelID` (lines
46-50) and `DefaultRerankModelID` (lines 51-55) plus their tags. The
struct becomes:

```go
type Config struct {
    BaseURL                      string        `yaml:"base_url"`
    Timeout                      time.Duration `yaml:"-"`
    TimeoutMs                    int           `yaml:"timeout_ms"`
    UploadTimeoutMs              int           `yaml:"upload_timeout_ms"`
    RetrievalTimeoutMs           int           `yaml:"retrieval_timeout_ms"`
    MaxRetries                   int           `yaml:"max_retries"`
    RetryBackoffMs               int           `yaml:"retry_backoff_ms"`
    DefaultTextEmbeddingModelID  string        `yaml:"default_text_embedding_model_id"`
    DefaultImageEmbeddingModelID string        `yaml:"default_image_embedding_model_id"`
}
```

- [ ] **Step 2: Remove from `ragimpl.New(...)` call**

In `backend/application/knowledge/init.go` around line 98-108, delete
the last two args of the call:

```go
domainSVC := ragimpl.New(
    client,
    c.DB,
    c.IDGen,
    resolver,
    c.Storage,
    cfg.Rag.DefaultTextEmbeddingModelID,
    cfg.Rag.DefaultImageEmbeddingModelID,
)
```

> Build will break on `ragimpl.New`'s signature mismatch; that's fixed
> in Task 5.

- [ ] **Step 3: Check for stray env reads**

```bash
grep -rn "RAG_DEFAULT_LLM_MODEL_ID\|RAG_DEFAULT_RERANK_MODEL_ID" backend/
```

The only remaining hits should be in `_test.go` files (handled in Task 9).
If anything else surfaces (e.g. a `os.Getenv` call in production code),
delete it now.

- [ ] **Step 4: Commit (broken build)**

```bash
git add backend/conf/rag/config.go backend/application/knowledge/init.go
git commit -m "refactor(conf-rag): drop DefaultLLMModelID/DefaultRerankModelID

These env-defaulted model ids were wrapped into query_strategy as
llm_model_id / rerank_model_id, which rag now rejects with 40004.
Drop the config surface area entirely; ragimpl constructor signature
follows in the next task."
```

---

### Task 5: Simplify `ragimpl.New` constructor

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go:36-80`

- [ ] **Step 1: Remove `defaultLLMModelID / defaultRerankModelID` fields**

In `Impl` struct (lines 36-59):

```go
type Impl struct {
    rag      contract.Client
    mapping  *MappingRepo
    idgen    idgen.IDGenerator
    resolver TenantResolver
    storage  storage.Storage

    defaultTextEmbeddingModelID  string
    defaultImageEmbeddingModelID string
}
```

Delete `defaultLLMModelID` and `defaultRerankModelID`.

- [ ] **Step 2: Shorten `New` signature**

`New` becomes:

```go
func New(
    rag contract.Client,
    db *gorm.DB,
    idgen idgen.IDGenerator,
    resolver TenantResolver,
    storage storage.Storage,
    defaultTextModel, defaultImageModel string,
) *Impl {
    return &Impl{
        rag:                          rag,
        mapping:                      NewMappingRepo(db),
        idgen:                        idgen,
        resolver:                     resolver,
        storage:                      storage,
        defaultTextEmbeddingModelID:  defaultTextModel,
        defaultImageEmbeddingModelID: defaultImageModel,
    }
}
```

- [ ] **Step 3: Compile**

```bash
cd /home/xinyuliu/coze-studio && go build ./backend/application/knowledge/... ./backend/domain/knowledge/...
```

Expected: pass (call site in `init.go` already matches new signature
after Task 4 Step 2).

- [ ] **Step 4: Commit**

```bash
git add backend/domain/knowledge/service/ragimpl/factory.go
git commit -m "refactor(ragimpl): drop defaultLLMModelID/defaultRerankModelID

Aligns Impl with the post-r2 rag contract: query_strategy is a 4-key
boolean dict, model ids are deployment-level concerns of rag (not
per-request)."
```

---

### Task 6: Drop deleted fields from rag contract type

**Files:**
- Modify: `backend/infra/contract/rag/types.go:269-290`

- [ ] **Step 1: Remove `DocumentIDs / MinScore / MaxTokens` and the stale comment**

Replace `RetrieveRequest` (lines 269-290) with the cleaned-up version:

```go
// RetrieveRequest mirrors rag's RetrievalRequest. Tenant comes from the
// X-Tenant-Id header, not the body. Wire-level fields not consumed by
// rag (legacy document_ids / min_score / max_tokens) were removed in
// 2026-05-20 — they were pydantic extra="ignore" silent no-ops.
type RetrieveRequest struct {
    KBIDs            []string       `json:"kb_ids"`
    Query            *string        `json:"query,omitempty"`
    QueryImage       *QueryImage    `json:"query_image,omitempty"`
    QueryMode        string         `json:"query_mode,omitempty"`
    SearchType       string         `json:"search_type,omitempty"`
    TopK             *int           `json:"top_k,omitempty"`
    CandidateK       *int           `json:"candidate_k,omitempty"`
    Filters          map[string]any `json:"filters,omitempty"`
    TargetChunkTypes []string       `json:"target_chunk_types,omitempty"`
    Retrievers       []string       `json:"retrievers,omitempty"`
    FusionPolicy     map[string]any `json:"fusion_policy,omitempty"`
    RetrieverParams  map[string]any `json:"retriever_params,omitempty"`
    QueryStrategy    map[string]any `json:"query_strategy,omitempty"`
}
```

Deleted fields: `DocumentIDs`, `MinScore`, `MaxTokens`. `CandidateK` is
preserved on the contract type per design decision (other callers may
still use it; knowledge-retrieve stops forwarding it in Task 7).

- [ ] **Step 2: Compile and capture breakage**

```bash
cd /home/xinyuliu/coze-studio && go build ./...
```

Expected breakage:
- `domain/knowledge/service/ragimpl/retrieval.go` (DocumentIDs, MinScore, MaxTokens references; fixed in Task 7)
- `domain/knowledge/service/ragimpl/retrieval_test.go` (test fixtures; fixed in Task 8)

- [ ] **Step 3: Commit (broken build)**

```bash
git add backend/infra/contract/rag/types.go
git commit -m "refactor(rag-contract): drop DocumentIDs/MinScore/MaxTokens

Pydantic extra=ignore was silently dropping these on rag side. Remove
the dead surface area; CandidateK kept on the contract (may have
non-workflow callers)."
```

---

### Task 7: Simplify `ragimpl.Retrieve`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/retrieval.go:39-217`

- [ ] **Step 1: Rewrite the function body**

Replace the entire `Retrieve` function with the simplified version:

```go
func (i *Impl) Retrieve(ctx context.Context, req *service.RetrieveRequest) (*knowledgeModel.RetrieveResponse, error) {
    if len(req.KnowledgeIDs) == 0 {
        return nil, errors.New("ragimpl.Retrieve: at least one knowledge_id required")
    }

    kbs, err := i.mapping.KBsByCozeIDs(ctx, req.KnowledgeIDs)
    if err != nil {
        return nil, err
    }
    if len(kbs) == 0 {
        return nil, errors.New("ragimpl.Retrieve: no knowledge bases resolved from mapping")
    }
    tenant, err := i.tenant(ctx)
    if err != nil {
        return nil, err
    }
    ragKBIDs := make([]string, 0, len(kbs))
    for _, k := range kbs {
        ragKBIDs = append(ragKBIDs, k.RagKBID)
    }

    ragReq := &contract.RetrieveRequest{
        KBIDs:     ragKBIDs,
        QueryMode: "text_input",
    }

    if req.Query != "" {
        q := req.Query
        ragReq.Query = &q
    }

    if req.Strategy != nil {
        s := req.Strategy

        if s.TopK != nil && *s.TopK > 0 {
            tk := int(*s.TopK)
            ragReq.TopK = &tk
        }

        switch s.SearchType {
        case knowledgeModel.SearchTypeFullText:
            ragReq.SearchType = "bm25"
        case knowledgeModel.SearchTypeHybrid:
            ragReq.SearchType = "hybrid"
        default:
            ragReq.SearchType = "dense"
        }

        // query_strategy 4-boolean. Omit the dict entirely when all four are
        // false (matches the "no enhancement requested" wire shape).
        qs := map[string]any{}
        if s.Rewrite {
            qs["rewrite"] = true
        }
        if s.Expansion {
            qs["expansion"] = true
        }
        if s.MultiQuery {
            qs["multi_query"] = true
        }
        if s.EnableRerank {
            qs["enable_rerank"] = true
        }
        if len(qs) > 0 {
            ragReq.QueryStrategy = qs
        }

        // New top-level rag fields. Each is forwarded only when the caller
        // explicitly set a non-zero value; zero values let rag use its
        // own defaults.
        if s.QueryMode != "" {
            ragReq.QueryMode = s.QueryMode
        }
        if s.QueryImage != nil {
            ragReq.QueryImage = &contract.QueryImage{
                ImageBase64: s.QueryImage.ImageBase64,
                ImageRef:    s.QueryImage.ImageRef,
            }
        }
        if len(s.TargetChunkTypes) > 0 {
            ragReq.TargetChunkTypes = s.TargetChunkTypes
        }
        if len(s.Filters) > 0 {
            ragReq.Filters = s.Filters
        }
        if len(s.Retrievers) > 0 {
            ragReq.Retrievers = s.Retrievers
        }
        if len(s.FusionPolicy) > 0 {
            ragReq.FusionPolicy = s.FusionPolicy
        }
        if len(s.RetrieverParams) > 0 {
            ragReq.RetrieverParams = s.RetrieverParams
        }
    }

    resp, err := i.rag.Retrieve(ctx, tenant, ragReq)
    if err != nil {
        return nil, err
    }

    slices := make([]*knowledgeModel.RetrieveSlice, 0, len(resp.Items))
    for idx := range resp.Items {
        h := resp.Items[idx]
        m, err := i.mapping.docByRagID(ctx, h.DocID)
        if err != nil {
            logs.CtxWarnf(ctx, "ragimpl.Retrieve: docByRagID(%s) failed, skipping hit: %v", h.DocID, err)
            continue
        }
        cozeSliceID := i.resolveCozeSliceID(ctx, h.ChunkID, h.DocID, m.CozeID, m.CreatorID)
        text := h.Content
        s := &knowledgeModel.Slice{
            Info:        knowledgeModel.Info{ID: cozeSliceID, CreatorID: m.CreatorID},
            KnowledgeID: m.KBID,
            DocumentID:  m.CozeID,
            RawContent:  []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: &text}},
        }
        slices = append(slices, &knowledgeModel.RetrieveSlice{Slice: s, Score: h.Score})
    }
    return &knowledgeModel.RetrieveResponse{RetrieveSlices: slices}, nil
}
```

Key removals vs the old function:
- NL2SQL early-return 501 guard (entirely gone)
- DocumentIDs translation + empty-result short-circuit (entirely gone)
- MinScore / MaxTokens forwarding (entirely gone)
- env-gated query rewrite / rerank wrapping with llm_model_id /
  rerank_model_id (replaced with the 4-boolean dict)
- `i.defaultLLMModelID` / `i.defaultRerankModelID` reads (fields no
  longer exist)

- [ ] **Step 2: Add `QueryImage` type to the contract package if missing**

Verify `contract.QueryImage` exists:

```bash
grep -n "type QueryImage" backend/infra/contract/rag/types.go
```

Expected: line 264 (already exists). No new code needed.

- [ ] **Step 3: Compile**

```bash
cd /home/xinyuliu/coze-studio && go build ./backend/domain/knowledge/service/ragimpl/...
```

Expected: pass (`retrieval.go` compiles; tests still broken — fixed in Task 8).

- [ ] **Step 4: Commit (test build still broken)**

```bash
git add backend/domain/knowledge/service/ragimpl/retrieval.go
git commit -m "refactor(ragimpl): rewrite Retrieve to match new rag contract

Drops DocumentIDs/MinScore/MaxTokens forwarding, env-gated llm_model_id/
rerank_model_id wrapping, and the NL2SQL 501 guard. query_strategy is
now exactly the 4-boolean dict rag accepts; new top-level fields
(QueryImage/QueryMode/TargetChunkTypes/Filters/Retrievers/FusionPolicy/
RetrieverParams) forward when non-zero."
```

---

### Task 8: Rewrite ragimpl retrieval tests

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/retrieval_test.go` (entire file)

- [ ] **Step 1: Identify deletion targets**

```bash
grep -nE "EnableNL2SQL|EnableQueryRewrite|defaultLLMModelID|defaultRerankModelID|MinScore|MaxTokens|DocumentIDs.*Translated" backend/domain/knowledge/service/ragimpl/retrieval_test.go
```

These tests must be deleted (their behavior was either removed or
inverted):
- `TestRetrieve_NL2SQL_*`
- `TestRetrieve_DocumentIDs_*`
- `TestRetrieve_MinScore_*`
- `TestRetrieve_MaxTokens_*`
- `TestRetrieve_EnableQueryRewrite_*` (both `_WithLLMModelID` and `_NoLLMModelID_DropsEnhancement`)
- `TestRetrieve_EnableRerank_*` (both `_WithRerankModelID` and `_NoRerankModelID_DropsEnhancement`)
- Any test that initializes `i.defaultLLMModelID` or `i.defaultRerankModelID`

Keep tests that exercise:
- Basic happy path (query + kb_ids + topK + searchType)
- KB id mapping
- Tenant header resolution
- Hit translation (DocID → coze int64)

- [ ] **Step 2: Delete the obsolete tests**

Open the file, delete every function listed in Step 1. Leave the file
parseable (final function must end with `}` and no trailing partial
test).

- [ ] **Step 3: Write the failing test for 4-boolean `query_strategy`**

Add the following new test at the end of the file:

```go
// TestRetrieve_QueryStrategy_FourBooleanSubset_NoModelIDs verifies that
// when Strategy sets some subset of the 4 booleans (Rewrite / Expansion /
// MultiQuery / EnableRerank), ragimpl emits exactly those keys in
// query_strategy — no llm_model_id / rerank_model_id, even when the
// (now-removed) env was previously set.
func TestRetrieve_QueryStrategy_FourBooleanSubset_NoModelIDs(t *testing.T) {
    var capturedReq *contract.RetrieveRequest
    i := newTestImpl(t)
    i.rag = &stubRagClient{
        retrieveFn: func(ctx context.Context, tenant string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
            capturedReq = req
            return &contract.RetrieveResponse{}, nil
        },
    }
    seedKBMapping(t, i, 100, "rag-kb-1")

    _, err := i.Retrieve(context.Background(), &service.RetrieveRequest{
        Query:        "hello",
        KnowledgeIDs: []int64{100},
        Strategy: &knowledgeModel.RetrievalStrategy{
            Rewrite:      true,
            EnableRerank: true,
            // Expansion / MultiQuery left false
        },
    })
    require.NoError(t, err)

    require.NotNil(t, capturedReq.QueryStrategy)
    require.Equal(t, map[string]any{
        "rewrite":       true,
        "enable_rerank": true,
    }, capturedReq.QueryStrategy)

    // Wire-level assertion: no model_id keys appear in JSON
    body, err := json.Marshal(capturedReq)
    require.NoError(t, err)
    require.NotContains(t, string(body), "llm_model_id")
    require.NotContains(t, string(body), "rerank_model_id")
    require.NotContains(t, string(body), "min_score")
    require.NotContains(t, string(body), "document_ids")
    require.NotContains(t, string(body), "max_tokens")
}
```

> The helpers `newTestImpl(t)`, `stubRagClient`, `seedKBMapping` should
> already exist in this test file (they were used by the deleted
> tests). If unsure, scan: `grep -n "func newTestImpl\|type stubRagClient\|func seedKBMapping" backend/domain/knowledge/service/ragimpl/retrieval_test.go`. If they were also deleted in Step 2, restore the minimal version.

- [ ] **Step 4: Run the test — expect FAIL**

```bash
cd /home/xinyuliu/coze-studio && go test ./backend/domain/knowledge/service/ragimpl/ -run TestRetrieve_QueryStrategy_FourBooleanSubset_NoModelIDs -v
```

Expected: PASS already (the retrieval.go rewrite in Task 7 supports this
case). If it fails, the failure pinpoints the bug.

- [ ] **Step 5: Write test for all-false query_strategy emitting nothing**

```go
// TestRetrieve_QueryStrategy_AllFalse_Omitted verifies that when the
// caller sets no query_strategy booleans, the wire payload omits the
// query_strategy key entirely.
func TestRetrieve_QueryStrategy_AllFalse_Omitted(t *testing.T) {
    var capturedReq *contract.RetrieveRequest
    i := newTestImpl(t)
    i.rag = &stubRagClient{
        retrieveFn: func(ctx context.Context, tenant string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
            capturedReq = req
            return &contract.RetrieveResponse{}, nil
        },
    }
    seedKBMapping(t, i, 100, "rag-kb-1")

    _, err := i.Retrieve(context.Background(), &service.RetrieveRequest{
        Query:        "hello",
        KnowledgeIDs: []int64{100},
        Strategy:     &knowledgeModel.RetrievalStrategy{},
    })
    require.NoError(t, err)

    require.Nil(t, capturedReq.QueryStrategy)

    body, err := json.Marshal(capturedReq)
    require.NoError(t, err)
    require.NotContains(t, string(body), "query_strategy")
}
```

- [ ] **Step 6: Write test for new top-level forwarding**

```go
// TestRetrieve_NewTopLevelFields_Forwarded verifies that the new
// top-level rag fields (filters / target_chunk_types / retrievers /
// fusion_policy / retriever_params / query_image / query_mode) are
// transparently forwarded.
func TestRetrieve_NewTopLevelFields_Forwarded(t *testing.T) {
    var capturedReq *contract.RetrieveRequest
    i := newTestImpl(t)
    i.rag = &stubRagClient{
        retrieveFn: func(ctx context.Context, tenant string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
            capturedReq = req
            return &contract.RetrieveResponse{}, nil
        },
    }
    seedKBMapping(t, i, 100, "rag-kb-1")

    _, err := i.Retrieve(context.Background(), &service.RetrieveRequest{
        Query:        "hello",
        KnowledgeIDs: []int64{100},
        Strategy: &knowledgeModel.RetrievalStrategy{
            QueryMode:        "mixed_input",
            QueryImage:       &knowledgeModel.QueryImage{ImageRef: "ref-1"},
            TargetChunkTypes: []string{"text_chunk"},
            Filters:          map[string]any{"tag": "guides"},
            Retrievers:       []string{"dense", "bm25"},
            FusionPolicy:     map[string]any{"rrf_k": 60},
            RetrieverParams:  map[string]any{"dense": map[string]any{"candidate_k": 75}},
        },
    })
    require.NoError(t, err)

    require.Equal(t, "mixed_input", capturedReq.QueryMode)
    require.NotNil(t, capturedReq.QueryImage)
    require.Equal(t, "ref-1", capturedReq.QueryImage.ImageRef)
    require.Equal(t, []string{"text_chunk"}, capturedReq.TargetChunkTypes)
    require.Equal(t, map[string]any{"tag": "guides"}, capturedReq.Filters)
    require.Equal(t, []string{"dense", "bm25"}, capturedReq.Retrievers)
    require.Equal(t, map[string]any{"rrf_k": 60}, capturedReq.FusionPolicy)
    require.Equal(t, map[string]any{"dense": map[string]any{"candidate_k": 75}}, capturedReq.RetrieverParams)
}
```

- [ ] **Step 7: Run all retrieval tests**

```bash
cd /home/xinyuliu/coze-studio && go test ./backend/domain/knowledge/service/ragimpl/ -v
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/domain/knowledge/service/ragimpl/retrieval_test.go
git commit -m "test(ragimpl): rewrite retrieval tests for new rag contract

Deletes tests covering removed behaviors (DocumentIDs translation,
MinScore/MaxTokens forwarding, env-gated query rewrite/rerank, NL2SQL
501 guard). Adds tests for 4-boolean query_strategy emission and
top-level field forwarding (filters/target_chunk_types/retrievers/
fusion_policy/retriever_params/query_image/query_mode)."
```

---

### Task 9: Fix integration_test.go + knowledge_test.go fixtures

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/integration_test.go:111-112`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go:353-354`

- [ ] **Step 1: integration_test.go — remove env reads**

Open `integration_test.go` and find lines 111-112:

```go
os.Getenv("RAG_DEFAULT_LLM_MODEL_ID"),
os.Getenv("RAG_DEFAULT_RERANK_MODEL_ID"),
```

These were passed as the last two args to `ragimpl.New(...)`. Now that
`New` no longer accepts them, delete those two lines. Verify the
surrounding `ragimpl.New(...)` call compiles.

- [ ] **Step 2: knowledge_test.go — remove field initializers**

Open `knowledge_test.go` and find lines 353-354:

```go
defaultLLMModelID:            "llm-model-default",
defaultRerankModelID:         "rerank-model-default",
```

Delete both lines. The surrounding struct literal must still compile
(it's an `Impl{...}` initializer; trailing comma rules apply).

- [ ] **Step 3: Run all tests**

```bash
cd /home/xinyuliu/coze-studio && go test ./backend/domain/knowledge/service/ragimpl/
```

Expected: all PASS. If integration_test.go is `//go:build integration`
gated and you don't have the integration env, the build itself still
needs to pass: `go vet ./backend/domain/knowledge/service/ragimpl/...`.

- [ ] **Step 4: Commit**

```bash
git add backend/domain/knowledge/service/ragimpl/integration_test.go backend/domain/knowledge/service/ragimpl/knowledge_test.go
git commit -m "test(ragimpl): drop env / field references for removed model ids"
```

---

### Task 10: Rewrite workflow node Adapt

**Files:**
- Modify: `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go`

- [ ] **Step 1: Rewrite `RetrieveConfig` struct**

In `knowledge_retrieve.go`, lines 49-58, the struct becomes:

```go
type RetrieveConfig struct {
    KnowledgeIDs       []int64
    RetrievalStrategy  *knowledge.RetrievalStrategy
    ChatHistorySetting *vo.ChatHistorySetting
}
```

Delete `DocumentIDs []int64` and the surrounding doc comment.

- [ ] **Step 2: Rewrite `Adapt` body parsing**

Replace the param-parsing block (lines 60-185, after `r.KnowledgeIDs =
knowledgeIDs` and `if inputs.ChatHistorySetting != nil { ... }`) with
the new param set. Final `Adapt` (skipping the unchanged header / kb-id
parse / ChatHistorySetting setup):

```go
retrievalStrategy := &knowledge.RetrievalStrategy{}

var getDesignatedParamContent = func(name string) (any, bool) {
    for _, param := range inputs.DatasetParam {
        if param.Name == name {
            return param.Input.Value.Content, true
        }
    }
    return nil, false
}

if content, ok := getDesignatedParamContent("topK"); ok && content != nil {
    topK, err := cast.ToInt64E(content)
    if err != nil {
        return nil, err
    }
    if topK > 0 {
        retrievalStrategy.TopK = &topK
    }
}

// 4-boolean query_strategy (rag wire-level keys: rewrite / expansion /
// multi_query / enable_rerank). Frontend param names use camelCase:
// rewrite / expansion / multiQuery / enableRerank.
if content, ok := getDesignatedParamContent("rewrite"); ok {
    v, err := cast.ToBoolE(content)
    if err != nil {
        return nil, err
    }
    retrievalStrategy.Rewrite = v
}
if content, ok := getDesignatedParamContent("expansion"); ok {
    v, err := cast.ToBoolE(content)
    if err != nil {
        return nil, err
    }
    retrievalStrategy.Expansion = v
}
if content, ok := getDesignatedParamContent("multiQuery"); ok {
    v, err := cast.ToBoolE(content)
    if err != nil {
        return nil, err
    }
    retrievalStrategy.MultiQuery = v
}
if content, ok := getDesignatedParamContent("enableRerank"); ok {
    v, err := cast.ToBoolE(content)
    if err != nil {
        return nil, err
    }
    retrievalStrategy.EnableRerank = v
}

if content, ok := getDesignatedParamContent("strategy"); ok {
    strategy, err := cast.ToInt64E(content)
    if err != nil {
        return nil, err
    }
    searchType, err := convertRetrievalSearchType(strategy)
    if err != nil {
        return nil, err
    }
    retrievalStrategy.SearchType = searchType
}

if content, ok := getDesignatedParamContent("queryMode"); ok && content != nil {
    mode, err := cast.ToStringE(content)
    if err != nil {
        return nil, err
    }
    switch mode {
    case "", "text_input", "image_input", "mixed_input":
        retrievalStrategy.QueryMode = mode
    default:
        return nil, errors.New("queryMode must be one of text_input / image_input / mixed_input")
    }
}

if content, ok := getDesignatedParamContent("queryImage"); ok && content != nil {
    m, ok := content.(map[string]any)
    if !ok {
        return nil, errors.New("queryImage param must be an object")
    }
    qi := &knowledge.QueryImage{}
    if v, present := m["image_base64"]; present {
        s, err := cast.ToStringE(v)
        if err != nil {
            return nil, err
        }
        qi.ImageBase64 = s
    }
    if v, present := m["image_ref"]; present {
        s, err := cast.ToStringE(v)
        if err != nil {
            return nil, err
        }
        qi.ImageRef = s
    }
    if qi.ImageBase64 != "" || qi.ImageRef != "" {
        retrievalStrategy.QueryImage = qi
    }
}

if content, ok := getDesignatedParamContent("targetChunkTypes"); ok && content != nil {
    raw, ok := content.([]any)
    if !ok {
        return nil, errors.New("targetChunkTypes param must be a list")
    }
    out := make([]string, 0, len(raw))
    for _, v := range raw {
        s, err := cast.ToStringE(v)
        if err != nil {
            return nil, err
        }
        out = append(out, s)
    }
    if len(out) > 0 {
        retrievalStrategy.TargetChunkTypes = out
    }
}

if content, ok := getDesignatedParamContent("filters"); ok && content != nil {
    m, ok := content.(map[string]any)
    if !ok {
        return nil, errors.New("filters param must be an object")
    }
    if len(m) > 0 {
        retrievalStrategy.Filters = m
    }
}

if content, ok := getDesignatedParamContent("retrievers"); ok && content != nil {
    raw, ok := content.([]any)
    if !ok {
        return nil, errors.New("retrievers param must be a list")
    }
    out := make([]string, 0, len(raw))
    for _, v := range raw {
        s, err := cast.ToStringE(v)
        if err != nil {
            return nil, err
        }
        out = append(out, s)
    }
    if len(out) > 0 {
        retrievalStrategy.Retrievers = out
    }
}

if content, ok := getDesignatedParamContent("fusionPolicy"); ok && content != nil {
    m, ok := content.(map[string]any)
    if !ok {
        return nil, errors.New("fusionPolicy param must be an object")
    }
    if len(m) > 0 {
        retrievalStrategy.FusionPolicy = m
    }
}

if content, ok := getDesignatedParamContent("retrieverParams"); ok && content != nil {
    m, ok := content.(map[string]any)
    if !ok {
        return nil, errors.New("retrieverParams param must be an object")
    }
    if len(m) > 0 {
        retrievalStrategy.RetrieverParams = m
    }
}

r.RetrievalStrategy = retrievalStrategy
```

> **Crucially:** any param the user no longer sees but lingers in legacy
> workflow JSON — `useRewrite / useRerank / useNl2sql / isPersonalOnly /
> minScore / documentIDs` — has no `getDesignatedParamContent` lookup, so
> it's silently ignored. No need for a "drop legacy params" loop.

- [ ] **Step 3: Strip `DocumentIDs` from `Build` and `Retrieve`**

In `Build` (around line 198-213):

```go
func (r *RetrieveConfig) Build(_ context.Context, _ *schema.NodeSchema, _ ...schema.BuildOption) (any, error) {
    if len(r.KnowledgeIDs) == 0 {
        return nil, errors.New("knowledge ids are required")
    }
    if r.RetrievalStrategy == nil {
        return nil, errors.New("retrieval strategy is required")
    }
    return &Retrieve{
        knowledgeIDs:       r.KnowledgeIDs,
        retrievalStrategy:  r.RetrievalStrategy,
        ChatHistorySetting: r.ChatHistorySetting,
    }, nil
}
```

In `Retrieve` struct (around line 226-234):

```go
type Retrieve struct {
    knowledgeIDs       []int64
    retrievalStrategy  *knowledge.RetrievalStrategy
    ChatHistorySetting *vo.ChatHistorySetting
}
```

In `Invoke` (around line 236-263), drop `DocumentIDs` from the request build:

```go
req := &knowledge.RetrieveRequest{
    Query:        query,
    KnowledgeIDs: kr.knowledgeIDs,
    ChatHistory:  kr.GetChatHistoryOrNil(ctx, kr.ChatHistorySetting),
    Strategy:     kr.retrievalStrategy,
}
```

- [ ] **Step 4: Compile**

```bash
cd /home/xinyuliu/coze-studio && go build ./backend/domain/workflow/internal/nodes/knowledge/...
```

Expected: pass (tests still broken — fixed in Task 11).

- [ ] **Step 5: Commit (test build still broken)**

```bash
git add backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go
git commit -m "feat(workflow-knowledge): align Adapt with new RetrievalStrategy

Drops parsing of legacy params (useRewrite/useRerank/useNl2sql/
isPersonalOnly/minScore/documentIDs). Renames useRewrite->rewrite and
useRerank->enableRerank. Adds parsing for expansion/multiQuery/
queryMode/queryImage/targetChunkTypes/filters/retrievers/fusionPolicy/
retrieverParams. Legacy params in old workflow JSON are silently
ignored (no schema migration)."
```

---

### Task 11: Rewrite workflow node tests

**Files:**
- Modify: `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve_test.go`

- [ ] **Step 1: Delete tests for removed fields**

Open the file and delete these tests:
- `TestAdapt_ParsesDocumentIDs`
- `TestAdapt_NoDocumentIDsKeepsNil`

The 3 topK regression tests (`TestAdapt_BlankTopKDoesNotEmitZero`,
`TestAdapt_ExplicitZeroTopKAlsoDropped`, `TestAdapt_PositiveTopKIsKept`)
**stay** — they still apply because rag requires `top_k > 0`.

- [ ] **Step 2: Add test for 4-boolean parsing**

Append at end of file:

```go
// TestAdapt_ParsesNewQueryStrategy verifies the 4 new boolean params
// (rewrite / expansion / multiQuery / enableRerank) land on the
// matching RetrievalStrategy fields.
func TestAdapt_ParsesNewQueryStrategy(t *testing.T) {
    cfg := &RetrieveConfig{}
    node := &vo.Node{
        ID:   "n1",
        Type: "knowledge-retrieve",
        Data: &vo.Data{
            Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
            Inputs: &vo.Inputs{
                InputParameters: []*vo.Param{},
                Knowledge: &vo.Knowledge{
                    DatasetParam: []*vo.Param{
                        newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
                        newScalarParam("rewrite", vo.VariableTypeBoolean, true),
                        newScalarParam("expansion", vo.VariableTypeBoolean, false),
                        newScalarParam("multiQuery", vo.VariableTypeBoolean, true),
                        newScalarParam("enableRerank", vo.VariableTypeBoolean, true),
                    },
                },
            },
        },
    }

    if _, err := cfg.Adapt(context.Background(), node); err != nil {
        t.Fatalf("Adapt error: %v", err)
    }
    if cfg.RetrievalStrategy == nil {
        t.Fatalf("RetrievalStrategy is nil")
    }
    if !cfg.RetrievalStrategy.Rewrite {
        t.Errorf("Rewrite = false, want true")
    }
    if cfg.RetrievalStrategy.Expansion {
        t.Errorf("Expansion = true, want false")
    }
    if !cfg.RetrievalStrategy.MultiQuery {
        t.Errorf("MultiQuery = false, want true")
    }
    if !cfg.RetrievalStrategy.EnableRerank {
        t.Errorf("EnableRerank = false, want true")
    }
}
```

- [ ] **Step 3: Add test for filters / retrievers / targetChunkTypes**

```go
// TestAdapt_ParsesFiltersRetrieversTargetChunkTypes verifies map and
// list params hydrate the corresponding RetrievalStrategy fields.
func TestAdapt_ParsesFiltersRetrieversTargetChunkTypes(t *testing.T) {
    cfg := &RetrieveConfig{}
    node := &vo.Node{
        ID:   "n1",
        Type: "knowledge-retrieve",
        Data: &vo.Data{
            Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
            Inputs: &vo.Inputs{
                InputParameters: []*vo.Param{},
                Knowledge: &vo.Knowledge{
                    DatasetParam: []*vo.Param{
                        newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
                        newDatasetParam("targetChunkTypes", vo.VariableTypeString, []any{"text_chunk"}),
                        newDatasetParam("retrievers", vo.VariableTypeString, []any{"dense", "bm25"}),
                        newMapParam("filters", map[string]any{"tag": "guides", "year": int64(2026)}),
                    },
                },
            },
        },
    }

    if _, err := cfg.Adapt(context.Background(), node); err != nil {
        t.Fatalf("Adapt error: %v", err)
    }
    if got := cfg.RetrievalStrategy.TargetChunkTypes; len(got) != 1 || got[0] != "text_chunk" {
        t.Errorf("TargetChunkTypes = %v, want [text_chunk]", got)
    }
    if got := cfg.RetrievalStrategy.Retrievers; len(got) != 2 || got[0] != "dense" || got[1] != "bm25" {
        t.Errorf("Retrievers = %v, want [dense bm25]", got)
    }
    if got := cfg.RetrievalStrategy.Filters; got["tag"] != "guides" || got["year"] != int64(2026) {
        t.Errorf("Filters = %+v, want {tag:guides year:2026}", got)
    }
}

// newMapParam helper builds a vo.Param of object shape.
func newMapParam(name string, content map[string]any) *vo.Param {
    return &vo.Param{
        Name: name,
        Input: &vo.BlockInput{
            Type: vo.VariableTypeObject,
            Value: &vo.BlockInputValue{
                Type:    "literal",
                Content: content,
            },
        },
    }
}
```

- [ ] **Step 4: Add test for queryImage parsing**

```go
// TestAdapt_ParsesQueryImage verifies image_base64 / image_ref hydrate
// the QueryImage; an empty payload leaves the field nil.
func TestAdapt_ParsesQueryImage(t *testing.T) {
    cfg := &RetrieveConfig{}
    node := &vo.Node{
        ID:   "n1",
        Type: "knowledge-retrieve",
        Data: &vo.Data{
            Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
            Inputs: &vo.Inputs{
                InputParameters: []*vo.Param{},
                Knowledge: &vo.Knowledge{
                    DatasetParam: []*vo.Param{
                        newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
                        newMapParam("queryImage", map[string]any{"image_ref": "ref-1"}),
                    },
                },
            },
        },
    }

    if _, err := cfg.Adapt(context.Background(), node); err != nil {
        t.Fatalf("Adapt error: %v", err)
    }
    if cfg.RetrievalStrategy.QueryImage == nil {
        t.Fatalf("QueryImage is nil")
    }
    if cfg.RetrievalStrategy.QueryImage.ImageRef != "ref-1" {
        t.Errorf("ImageRef = %q, want ref-1", cfg.RetrievalStrategy.QueryImage.ImageRef)
    }
}
```

- [ ] **Step 5: Add test for queryMode validation**

```go
// TestAdapt_RejectsInvalidQueryMode verifies a non-enum queryMode
// returns an error rather than silently propagating to rag.
func TestAdapt_RejectsInvalidQueryMode(t *testing.T) {
    cfg := &RetrieveConfig{}
    node := &vo.Node{
        ID:   "n1",
        Type: "knowledge-retrieve",
        Data: &vo.Data{
            Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
            Inputs: &vo.Inputs{
                InputParameters: []*vo.Param{},
                Knowledge: &vo.Knowledge{
                    DatasetParam: []*vo.Param{
                        newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
                        newScalarParam("queryMode", vo.VariableTypeString, "garbage"),
                    },
                },
            },
        },
    }

    _, err := cfg.Adapt(context.Background(), node)
    if err == nil {
        t.Fatalf("Adapt returned no error; want validation error for queryMode=garbage")
    }
}
```

- [ ] **Step 6: Add test for legacy param ignore**

```go
// TestAdapt_IgnoresLegacyParams locks in that the new Adapt silently
// drops legacy param names (useRewrite/useRerank/useNl2sql/
// isPersonalOnly/minScore/documentIDs) so old workflow JSON loads
// without error.
func TestAdapt_IgnoresLegacyParams(t *testing.T) {
    cfg := &RetrieveConfig{}
    node := &vo.Node{
        ID:   "n1",
        Type: "knowledge-retrieve",
        Data: &vo.Data{
            Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
            Inputs: &vo.Inputs{
                InputParameters: []*vo.Param{},
                Knowledge: &vo.Knowledge{
                    DatasetParam: []*vo.Param{
                        newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
                        newScalarParam("useRewrite", vo.VariableTypeBoolean, true),
                        newScalarParam("useRerank", vo.VariableTypeBoolean, true),
                        newScalarParam("useNl2sql", vo.VariableTypeBoolean, true),
                        newScalarParam("isPersonalOnly", vo.VariableTypeBoolean, true),
                        newScalarParam("minScore", vo.VariableTypeFloat, 0.7),
                        newDatasetParam("documentIDs", vo.VariableTypeInteger, []any{int64(1), int64(2)}),
                    },
                },
            },
        },
    }

    if _, err := cfg.Adapt(context.Background(), node); err != nil {
        t.Fatalf("Adapt returned error for legacy params: %v", err)
    }
    if cfg.RetrievalStrategy == nil {
        t.Fatalf("RetrievalStrategy is nil")
    }
    // None of the new fields should be set from legacy params.
    if cfg.RetrievalStrategy.Rewrite || cfg.RetrievalStrategy.EnableRerank {
        t.Errorf("legacy useRewrite/useRerank leaked into new fields")
    }
}
```

- [ ] **Step 7: Run tests**

```bash
cd /home/xinyuliu/coze-studio && go test ./backend/domain/workflow/internal/nodes/knowledge/ -v
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve_test.go
git commit -m "test(workflow-knowledge): cover new Adapt parsing surface

Deletes DocumentIDs tests. Adds tests for 4-boolean query_strategy,
filters/retrievers/targetChunkTypes, queryImage, queryMode validation,
legacy-param-ignore behavior."
```

---

### Task 12: Backend smoke — full build + grep for dead references

**Files:** (no edits)

- [ ] **Step 1: Full build**

```bash
cd /home/xinyuliu/coze-studio && go build ./...
```

Expected: pass.

- [ ] **Step 2: Full test pass for touched packages**

```bash
cd /home/xinyuliu/coze-studio && go test \
  ./backend/crossdomain/knowledge/... \
  ./backend/domain/knowledge/... \
  ./backend/domain/workflow/internal/nodes/knowledge/... \
  ./backend/domain/agent/singleagent/... \
  ./backend/infra/contract/rag/... \
  -count=1
```

Expected: all PASS.

- [ ] **Step 3: Grep verification — no dead references**

```bash
grep -r "RAG_DEFAULT_LLM_MODEL_ID\|RAG_DEFAULT_RERANK_MODEL_ID\|defaultLLMModelID\|defaultRerankModelID" backend/
```

Expected: empty.

```bash
grep -rn "EnableNL2SQL\|IsPersonalOnly\|EnableQueryRewrite" backend/ --include="*.go" | grep -v "_test.go" | grep -v "/conf/"
```

Expected: empty (legacy production code no longer references these).

```bash
grep -rn "MinScore\|MaxTokens" backend/domain/knowledge/service/ragimpl/retrieval.go
```

Expected: empty.

```bash
grep -rn "DocumentIDs" backend/domain/workflow/internal/nodes/knowledge/
```

Expected: empty (workflow node no longer touches the field; other
DocumentIDs in document service paths are unrelated).

- [ ] **Step 4: Commit verification log if needed**

No commit; this is a verification gate. If any grep returns hits, stop
and fix the offending file before moving to the frontend.

---

### Task 13: Frontend — refactor DataSetInfo type

**Files:**
- Modify: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/type.ts`

- [ ] **Step 1: Read existing type**

```bash
cat frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/type.ts
```

Note the current `DataSetInfo` shape. Existing field names are
snake_case (`min_score`, `use_rerank`, `document_ids`, ...).

- [ ] **Step 2: Rewrite the type**

Replace `DataSetInfo` with:

```ts
export interface QueryImage {
  image_base64?: string;
  image_ref?: string;
}

export interface DataSetInfo {
  top_k?: number;
  strategy?: Strategy;

  // query_strategy 4 booleans (wire-level rag keys)
  rewrite?: boolean;
  expansion?: boolean;
  multi_query?: boolean;
  enable_rerank?: boolean;

  // new top-level rag fields
  query_image?: QueryImage;
  query_mode?: 'text_input' | 'image_input' | 'mixed_input';
  target_chunk_types?: Array<'text_chunk' | 'image_chunk'>;
  filters?: Record<string, unknown>;
  retrievers?: Array<'dense' | 'bm25' | 'image_vector'>;
  fusion_policy?: Record<string, unknown>;
  retriever_params?: Record<string, unknown>;
}
```

Deleted fields: `min_score`, `document_ids`, `use_nl2sql`,
`is_personal_only`, `use_rewrite` (→ `rewrite`), `use_rerank` (→
`enable_rerank`).

`Strategy` enum (semantic / fulltext / hybrid) is preserved.

- [ ] **Step 3: Type-check the package**

```bash
cd frontend && rush rebuild -o @coze-workflow/playground
```

Expected: ts errors enumerating callers that reference deleted fields
— these are inputs to Tasks 14-19.

- [ ] **Step 4: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/type.ts
git commit -m "refactor(workflow-knowledge-ui): rewrite DataSetInfo for new rag contract

Deletes min_score/document_ids/use_nl2sql/is_personal_only. Renames
use_rewrite->rewrite, use_rerank->enable_rerank. Adds expansion/
multi_query/query_image/query_mode/target_chunk_types/filters/
retrievers/fusion_policy/retriever_params."
```

---

### Task 14: Create `BasicSection.tsx`

**Files:**
- Create: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/BasicSection.tsx`

- [ ] **Step 1: Create the file**

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC } from 'react';

import { I18n } from '@coze-arch/i18n';

import { TitleArea, SliderArea, SearchStrategy } from '../components';
import { type DataSetInfo, Strategy } from '../type';

import s from '../index.module.less';

const DEFAULT_TOP_K = 10;

export interface BasicSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
}

export const BasicSection: FC<BasicSectionProps> = ({
  value,
  onChange,
  readonly,
  disabled,
}) => {
  const strategy = value?.strategy;
  const topK = value?.top_k;

  return (
    <>
      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('knowledge_search_strategy_title')}
          tip={I18n.t('knowledge_search_strategy_tooltip')}
        />
        <SearchStrategy
          readonly={readonly}
          value={strategy as Strategy}
          onChange={v => onChange({ ...value, strategy: v })}
        />
      </div>

      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('dataset_max_recall')}
          tip={I18n.t('bot_edit_datasetsSettings_MaxTip')}
        />
        <SliderArea
          min={1}
          max={50}
          step={1}
          value={topK}
          customStyles={{
            sliderAreaStyle: { width: '160px' },
            boundaryStyle: { width: '158px', margin: 0 },
          }}
          isDataSet
          marks={{
            markKey: DEFAULT_TOP_K,
            markText: <span className="ml-2">Default</span>,
          }}
          onChange={v => onChange({ ...value, top_k: v })}
          onClickDefault={() => onChange({ ...value, top_k: DEFAULT_TOP_K })}
          disabled={readonly || disabled}
        />
      </div>
    </>
  );
};
```

> The slider range expands to `max=50` to match rag's
> `retrieval_max_top_k`. Default mark moves to 10 (rag's
> `retrieval_default_top_k`).

- [ ] **Step 2: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/BasicSection.tsx
git commit -m "feat(workflow-knowledge-ui): add BasicSection (search strategy + topK)"
```

---

### Task 15: Create `QueryEnhancementSection.tsx`

**Files:**
- Create: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryEnhancementSection.tsx`

- [ ] **Step 1: Create the file**

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC } from 'react';

import { useNodeTestId } from '@coze-workflow/base';
import { I18n } from '@coze-arch/i18n';

import { CheckboxWithLabel } from '../../checkbox-with-label';
import { TitleArea } from '../components';
import { type DataSetInfo } from '../type';

import s from '../index.module.less';

const RAG_LLM_TOOLTIP = I18n.t(
  'workflow_knowledge_rag_llm_required',
  {},
  '需 rag 部署侧配置 llm_base_url 才生效；否则原样检索且 trace 标记 llm_failed。',
);

const RAG_RERANK_TOOLTIP = I18n.t(
  'workflow_knowledge_rag_rerank_required',
  {},
  '需 rag 部署侧配置 rerank_base_url 才生效；否则跳过 rerank 且不报错。',
);

export interface QueryEnhancementSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

export const QueryEnhancementSection: FC<QueryEnhancementSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const { getNodeSetterId } = useNodeTestId();

  return (
    <div>
      <TitleArea
        title={I18n.t('workflow_knowledge_query_enhancement', {}, '查询增强')}
      />
      <CheckboxWithLabel
        checked={!!value?.rewrite}
        onChange={checked => onChange({ ...value, rewrite: checked })}
        readonly={readonly}
        label={I18n.t('workflow_knowledge_rewrite', {}, '查询改写')}
        description={I18n.t(
          'workflow_knowledge_rewrite_desc',
          {},
          '改写为更适合检索的形式。',
        )}
        dataTestId={getNodeSetterId('dataset_rewrite')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.expansion}
        onChange={checked => onChange({ ...value, expansion: checked })}
        readonly={readonly}
        label={I18n.t('workflow_knowledge_expansion', {}, '查询扩展')}
        description={I18n.t(
          'workflow_knowledge_expansion_desc',
          {},
          '加入相关词。',
        )}
        dataTestId={getNodeSetterId('dataset_expansion')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.multi_query}
        onChange={checked => onChange({ ...value, multi_query: checked })}
        readonly={readonly}
        label={I18n.t('workflow_knowledge_multi_query', {}, '多重查询')}
        description={I18n.t(
          'workflow_knowledge_multi_query_desc',
          {},
          '生成多个语义等价查询并行检索。',
        )}
        dataTestId={getNodeSetterId('dataset_multi_query')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.enable_rerank}
        onChange={checked => onChange({ ...value, enable_rerank: checked })}
        readonly={readonly}
        label={I18n.t('workflow_knowledge_enable_rerank', {}, '启用 Rerank')}
        description={I18n.t(
          'workflow_knowledge_enable_rerank_desc',
          {},
          '使用 reranker 对召回结果重新排序。',
        )}
        dataTestId={getNodeSetterId('dataset_enable_rerank')}
        tooltip={RAG_RERANK_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
    </div>
  );
};
```

- [ ] **Step 2: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryEnhancementSection.tsx
git commit -m "feat(workflow-knowledge-ui): add QueryEnhancementSection (4 booleans)"
```

---

### Task 16: Create `QueryInputSection.tsx`

**Files:**
- Create: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryInputSection.tsx`

- [ ] **Step 1: Create the file**

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC, useState } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Input, Select, Collapse } from '@coze-arch/coze-design';

import { TitleArea } from '../components';
import { type DataSetInfo } from '../type';

import s from '../index.module.less';

const QUERY_MODE_OPTIONS = [
  { label: 'Text', value: 'text_input' },
  { label: 'Image', value: 'image_input' },
  { label: 'Text + Image', value: 'mixed_input' },
];

export interface QueryInputSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

export const QueryInputSection: FC<QueryInputSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const [open, setOpen] = useState(false);
  const queryMode = value?.query_mode ?? 'text_input';
  const queryImage = value?.query_image;

  return (
    <Collapse
      activeKey={open ? 'qi' : undefined}
      onChange={k => setOpen(Array.isArray(k) ? k.includes('qi') : k === 'qi')}
    >
      <Collapse.Panel
        itemKey="qi"
        header={I18n.t(
          'workflow_knowledge_image_query',
          {},
          '图片查询（可选）',
        )}
      >
        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t('workflow_knowledge_query_mode', {}, '查询模式')}
          />
          <Select
            disabled={readonly}
            value={queryMode}
            optionList={QUERY_MODE_OPTIONS}
            onChange={v =>
              onChange({
                ...value,
                query_mode: v as DataSetInfo['query_mode'],
              })
            }
          />
        </div>

        {queryMode !== 'text_input' ? (
          <div className={s['setting-item']}>
            <TitleArea
              title={I18n.t(
                'workflow_knowledge_image_ref',
                {},
                '图片引用 (image_ref)',
              )}
              tip={I18n.t(
                'workflow_knowledge_image_ref_tip',
                {},
                '已上传到 rag 对象存储的引用键。',
              )}
            />
            <Input
              disabled={readonly}
              value={queryImage?.image_ref ?? ''}
              onChange={v =>
                onChange({
                  ...value,
                  query_image: { ...queryImage, image_ref: v },
                })
              }
            />
          </div>
        ) : null}
      </Collapse.Panel>
    </Collapse>
  );
};
```

> Image base64 inline upload is intentionally out of scope here; users
> who need it can switch to `image_ref` after uploading via the KB
> manager. This keeps the section's UX bounded.

- [ ] **Step 2: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/QueryInputSection.tsx
git commit -m "feat(workflow-knowledge-ui): add QueryInputSection (image query)"
```

---

### Task 17: Create `FilterSection.tsx`

**Files:**
- Create: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/FilterSection.tsx`

- [ ] **Step 1: Create the file**

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Checkbox, Input, Button } from '@coze-arch/coze-design';

import { TitleArea } from '../components';
import { type DataSetInfo } from '../type';

import s from '../index.module.less';

type ChunkType = 'text_chunk' | 'image_chunk';

export interface FilterSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

const toggleChunkType = (
  current: ChunkType[] | undefined,
  type: ChunkType,
  checked: boolean,
): ChunkType[] => {
  const set = new Set(current ?? []);
  if (checked) {
    set.add(type);
  } else {
    set.delete(type);
  }
  return Array.from(set);
};

export const FilterSection: FC<FilterSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const target = (value?.target_chunk_types ?? []) as ChunkType[];
  const filters = value?.filters ?? {};
  const filterEntries = Object.entries(filters);

  return (
    <div>
      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t(
            'workflow_knowledge_target_chunk_types',
            {},
            'Chunk 类型',
          )}
          tip={I18n.t(
            'workflow_knowledge_target_chunk_types_tip',
            {},
            '限定只检索 text chunk 或 image chunk；留空由 query_mode 决定。',
          )}
        />
        <div className="flex gap-3">
          <Checkbox
            disabled={readonly}
            checked={target.includes('text_chunk')}
            onChange={e =>
              onChange({
                ...value,
                target_chunk_types: toggleChunkType(target, 'text_chunk', !!e.target.checked),
              })
            }
          >
            text_chunk
          </Checkbox>
          <Checkbox
            disabled={readonly}
            checked={target.includes('image_chunk')}
            onChange={e =>
              onChange({
                ...value,
                target_chunk_types: toggleChunkType(target, 'image_chunk', !!e.target.checked),
              })
            }
          >
            image_chunk
          </Checkbox>
        </div>
      </div>

      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('workflow_knowledge_filters', {}, '过滤条件 (filters)')}
          tip={I18n.t(
            'workflow_knowledge_filters_tip',
            {},
            'KB metadata 字段过滤；key/value 由 KB 自身定义。',
          )}
        />
        {filterEntries.map(([k, v], idx) => (
          <div className="flex gap-2 mb-2" key={`${k}-${idx}`}>
            <Input
              disabled={readonly}
              value={k}
              placeholder="key"
              onChange={nk => {
                const next = { ...filters };
                delete next[k];
                next[nk] = v;
                onChange({ ...value, filters: next });
              }}
            />
            <Input
              disabled={readonly}
              value={typeof v === 'string' ? v : JSON.stringify(v)}
              placeholder="value"
              onChange={nv => onChange({ ...value, filters: { ...filters, [k]: nv } })}
            />
            <Button
              type="tertiary"
              disabled={readonly}
              onClick={() => {
                const next = { ...filters };
                delete next[k];
                onChange({ ...value, filters: next });
              }}
            >
              ×
            </Button>
          </div>
        ))}
        <Button
          type="tertiary"
          disabled={readonly}
          onClick={() => onChange({ ...value, filters: { ...filters, '': '' } })}
        >
          + {I18n.t('workflow_knowledge_filters_add', {}, '添加过滤条件')}
        </Button>
      </div>
    </div>
  );
};
```

- [ ] **Step 2: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/FilterSection.tsx
git commit -m "feat(workflow-knowledge-ui): add FilterSection (target chunk types + filters KV editor)"
```

---

### Task 18: Create `AdvancedSection.tsx`

**Files:**
- Create: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/AdvancedSection.tsx`

- [ ] **Step 1: Create the file**

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC, useState } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Checkbox, TextArea, Collapse } from '@coze-arch/coze-design';

import { TitleArea } from '../components';
import { type DataSetInfo } from '../type';

import s from '../index.module.less';

type Retriever = 'dense' | 'bm25' | 'image_vector';

const RETRIEVERS: Retriever[] = ['dense', 'bm25', 'image_vector'];

export interface AdvancedSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

const stringifyJSON = (v: unknown): string => {
  if (v === undefined || v === null) return '';
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return '';
  }
};

const parseJSONLoose = (raw: string): { ok: boolean; value: Record<string, unknown> } => {
  const trimmed = raw.trim();
  if (!trimmed) return { ok: true, value: {} };
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return { ok: true, value: parsed as Record<string, unknown> };
    }
    return { ok: false, value: {} };
  } catch {
    return { ok: false, value: {} };
  }
};

export const AdvancedSection: FC<AdvancedSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const [open, setOpen] = useState(false);
  const retrievers = (value?.retrievers ?? []) as Retriever[];

  const [fpText, setFpText] = useState(stringifyJSON(value?.fusion_policy));
  const [fpError, setFpError] = useState<string | null>(null);
  const [rpText, setRpText] = useState(stringifyJSON(value?.retriever_params));
  const [rpError, setRpError] = useState<string | null>(null);

  return (
    <Collapse
      activeKey={open ? 'adv' : undefined}
      onChange={k => setOpen(Array.isArray(k) ? k.includes('adv') : k === 'adv')}
    >
      <Collapse.Panel
        itemKey="adv"
        header={
          <span>
            ⚠️{' '}
            {I18n.t(
              'workflow_knowledge_advanced',
              {},
              '高级（需 RAG 调参经验）',
            )}
          </span>
        }
      >
        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t('workflow_knowledge_retrievers', {}, 'Retrievers')}
            tip={I18n.t(
              'workflow_knowledge_retrievers_tip',
              {},
              '显式指定召回路：dense / bm25 / image_vector。留空由 target_chunk_types 推导。',
            )}
          />
          <div className="flex gap-3">
            {RETRIEVERS.map(r => (
              <Checkbox
                key={r}
                disabled={readonly}
                checked={retrievers.includes(r)}
                onChange={e => {
                  const set = new Set(retrievers);
                  if (e.target.checked) {
                    set.add(r);
                  } else {
                    set.delete(r);
                  }
                  onChange({ ...value, retrievers: Array.from(set) });
                }}
              >
                {r}
              </Checkbox>
            ))}
          </div>
        </div>

        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t(
              'workflow_knowledge_fusion_policy',
              {},
              'Fusion policy (JSON)',
            )}
            tip={I18n.t(
              'workflow_knowledge_fusion_policy_tip',
              {},
              '{"rrf_k":60, "weights":{"text":0.6, "image":0.4}}',
            )}
          />
          <TextArea
            disabled={readonly}
            value={fpText}
            rows={4}
            onChange={v => setFpText(v)}
            onBlur={() => {
              const r = parseJSONLoose(fpText);
              if (!r.ok) {
                setFpError(I18n.t('workflow_json_invalid', {}, 'JSON 无效'));
                return;
              }
              setFpError(null);
              onChange({ ...value, fusion_policy: r.value });
            }}
          />
          {fpError ? <div className="text-red-500 text-xs">{fpError}</div> : null}
        </div>

        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t(
              'workflow_knowledge_retriever_params',
              {},
              'Retriever params (JSON)',
            )}
            tip={I18n.t(
              'workflow_knowledge_retriever_params_tip',
              {},
              '{"dense":{"candidate_k":75}, "bm25":{"candidate_k":40}}',
            )}
          />
          <TextArea
            disabled={readonly}
            value={rpText}
            rows={4}
            onChange={v => setRpText(v)}
            onBlur={() => {
              const r = parseJSONLoose(rpText);
              if (!r.ok) {
                setRpError(I18n.t('workflow_json_invalid', {}, 'JSON 无效'));
                return;
              }
              setRpError(null);
              onChange({ ...value, retriever_params: r.value });
            }}
          />
          {rpError ? <div className="text-red-500 text-xs">{rpError}</div> : null}
        </div>
      </Collapse.Panel>
    </Collapse>
  );
};
```

- [ ] **Step 2: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/AdvancedSection.tsx
git commit -m "feat(workflow-knowledge-ui): add AdvancedSection (retrievers + JSON editors)"
```

---

### Task 19: Refactor `index.tsx` to orchestrator

**Files:**
- Modify: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/index.tsx`

- [ ] **Step 1: Replace the file body**

Replace the entire file with the orchestrator:

```tsx
/*
 * Copyright 2025 coze-dev Authors
 * Licensed under the Apache License, Version 2.0.
 */
import { type FC, useEffect, useState } from 'react';

import { isNil } from 'lodash-es';
import { type Dataset } from '@coze-arch/idl/knowledge';

import { Strategy, type DataSetInfo } from './type';
import {
  BasicSection,
  QueryEnhancementSection,
  QueryInputSection,
  FilterSection,
  AdvancedSection,
} from './sections';

import s from './index.module.less';

const DEFAULT_TOP_K = 10;

export interface DataSetSettingProps {
  dataSetInfo: DataSetInfo;
  onDataSetInfoChange: (v: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
  style?: Record<string, unknown>;
  isReady?: boolean;
  dataSets?: Dataset[];
}

export const DataSetSetting: FC<DataSetSettingProps> = ({
  dataSetInfo,
  onDataSetInfoChange,
  readonly,
  disabled,
  style,
  isReady,
  dataSets,
}) => {
  const [datasetEmpty, setDatasetEmpty] = useState(true);

  useEffect(() => {
    if (!isReady) return;
    setDatasetEmpty(!dataSets?.length);
    if (!dataSets?.length) {
      onDataSetInfoChange({} as DataSetInfo);
    }
  }, [dataSets, isReady]);

  useEffect(() => {
    if (datasetEmpty) return;
    if (isNil(dataSetInfo?.top_k) && isNil(dataSetInfo?.strategy)) {
      onDataSetInfoChange({
        ...dataSetInfo,
        top_k: DEFAULT_TOP_K,
        strategy: Strategy.Hybird,
      });
    } else if (isNil(dataSetInfo?.strategy)) {
      onDataSetInfoChange({ ...dataSetInfo, strategy: Strategy.Hybird });
    }
  }, [dataSetInfo, datasetEmpty]);

  if (datasetEmpty) return <></>;

  return (
    <div className={s.setting} style={{ ...style, position: 'relative' }}>
      <BasicSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
        disabled={disabled}
      />
      <QueryEnhancementSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
      />
      <QueryInputSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
      />
      <FilterSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
      />
      <AdvancedSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
      />
    </div>
  );
};
```

- [ ] **Step 2: Create `sections/index.ts` barrel export**

Create `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/index.ts`:

```ts
export { BasicSection } from './BasicSection';
export { QueryEnhancementSection } from './QueryEnhancementSection';
export { QueryInputSection } from './QueryInputSection';
export { FilterSection } from './FilterSection';
export { AdvancedSection } from './AdvancedSection';
```

- [ ] **Step 3: Type-check + build**

```bash
cd frontend && rush rebuild -o @coze-workflow/playground
```

Expected: pass. The `DocumentIDsSelect` import that used to live in
`components/` is no longer referenced because we deleted its consumer.

- [ ] **Step 4: Commit**

```bash
git add frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/index.tsx \
         frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/sections/index.ts
git commit -m "refactor(workflow-knowledge-ui): orchestrate sections from index.tsx

index.tsx is now a thin orchestrator (<100 lines) that delegates each
form area to its own section component. Sets default top_k=10 and
Hybrid strategy on first load."
```

---

### Task 20: Delete `DocumentIDsSelect`

**Files:**
- Delete: `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/components/DocumentIDsSelect/`

- [ ] **Step 1: Verify no callers**

```bash
grep -rn "DocumentIDsSelect" frontend/ --include="*.ts" --include="*.tsx"
```

Expected: only the file's own definition (now orphaned).

- [ ] **Step 2: Remove the directory**

```bash
rm -rf frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/components/DocumentIDsSelect
```

- [ ] **Step 3: Update the components barrel export**

Open `frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/components/index.ts`
and remove the `DocumentIDsSelect` export line.

- [ ] **Step 4: Type-check + build**

```bash
cd frontend && rush rebuild -o @coze-workflow/playground
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add -A frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/components
git commit -m "chore(workflow-knowledge-ui): remove DocumentIDsSelect (no longer used)"
```

---

### Task 21: End-to-end wire body smoke test

**Files:** (no edits — manual / scripted verification)

- [ ] **Step 1: Bring up the local rag + coze stack**

Reference: user memory `[[rag-stack-dev-stack-refs]]`.

```bash
cd /home/xinyuliu/coze-studio/docker && docker compose up -d
```

Wait for `coze-server-v2` to report healthy (the binary should be a
fresh rebuild with the new code — see memory
`[[coze-server-v2-stale-debug-binary]]` for an environment caveat).

- [ ] **Step 2: Capture the rag retrieval request**

In a workflow node `knowledge-retrieve`:
- Select 1+ KB via DatasetSelectField
- Set topK to 5 (override the default 10)
- Toggle `Rewrite` on, `Multi Query` on
- In FilterSection, check `text_chunk` and add a filter row
  `{ key: "tag", value: "guides" }`
- Save and run the workflow

Then on the rag-web container, observe the incoming body. Two options:
- tail logs: `docker logs -f <rag-web>` and look for the POST /api/v1/retrieval request
- tcpdump on the bridge network
- Or add a one-liner debug print to rag's retrieval route, then revert

- [ ] **Step 3: Assert wire shape**

The body must contain:
- `kb_ids: [...]`
- `query: "..."`
- `top_k: 5`
- `search_type: "hybrid"` (assuming Hybrid default)
- `query_strategy: { "rewrite": true, "multi_query": true }`
- `target_chunk_types: ["text_chunk"]`
- `filters: { "tag": "guides" }`

The body must **not** contain:
- `document_ids` (any value)
- `min_score`
- `max_tokens`
- `candidate_k` (top-level)
- `query_strategy.llm_model_id`
- `query_strategy.rerank_model_id`
- Any `query_strategy` key not in {rewrite, expansion, multi_query, enable_rerank}

- [ ] **Step 4: Load a legacy workflow with old params**

If there's a workflow in the test environment that was saved before
this change (with `useRewrite / useRerank / minScore / documentIDs`
params), open it. Confirm:
- the node loads without errors
- the new UI shows defaults (rewrite=false, enable_rerank=false, etc.)
- saving the node and re-running emits a body **without** any legacy
  keys

- [ ] **Step 5: Record findings**

This is a verification gate; no commit. If anything fails, fix the
offending step before declaring the feature ready.

---

### Task 22: Final pass — release note + verification checklist

**Files:**
- Create or modify: `docs/superpowers/specs/2026-05-20-knowledge-retrieve-param-alignment-design.md` (post-implementation appendix; optional)

- [ ] **Step 1: Run the design doc's verification checklist**

From the spec file's "验证清单" section:

```bash
cd /home/xinyuliu/coze-studio
go test ./backend/domain/workflow/internal/nodes/knowledge/... &&
go test ./backend/domain/knowledge/... &&
(cd frontend && rush test --to @coze-workflow/playground)

grep -r "RAG_DEFAULT_LLM_MODEL_ID\|RAG_DEFAULT_RERANK_MODEL_ID" backend/
grep -r "EnableNL2SQL\|IsPersonalOnly" backend/domain/knowledge backend/crossdomain/knowledge
grep -rn "MinScore\|MaxTokens\|DocumentIDs" backend/domain/knowledge/service/ragimpl/retrieval.go
```

Last three greps must be empty.

- [ ] **Step 2: Draft release-note one-liner**

The release note must call out the behavior change for legacy / agent
paths. Suggested wording:

> **Knowledge-retrieve alignment.** The workflow knowledge-retrieve
> node now sends exactly the request shape rag accepts. Fields that
> were silently dropped on the rag side (`min_score`, `document_ids`,
> `max_tokens`, `is_personal_only`, `use_nl2sql`) have been removed
> from the UI and the request payload. **As a side effect of the
> `RetrievalStrategy` struct cleanup, the legacy KB backend
> (`KNOWLEDGE_BACKEND=legacy`) and the agent retriever no longer
> apply `query rewrite`, `NL2SQL`, `personal-only`, or `min_score`
> filtering.** New top-level rag capabilities (`filters`,
> `target_chunk_types`, `retrievers`, `fusion_policy`,
> `retriever_params`, `query_image`) are exposed on the node form.

Drop this into the project's release notes file (location depends on
project conventions — `CHANGELOG.md` if present, otherwise the next
release PR description).

- [ ] **Step 3: Commit release note (if a tracked file changed)**

```bash
git add CHANGELOG.md  # or wherever the note went
git commit -m "docs: release note for knowledge-retrieve param alignment"
```

---

## Self-Review Notes

- **Spec coverage:** Every section in the spec maps to one or more
  tasks above:
  - field mapping table → Tasks 1, 6, 7, 10, 13
  - default values → Tasks 14 (topK 10), spec is canonical reference
  - frontend component structure → Tasks 14-20
  - DataSetInfo type changes → Task 13
  - rendering order → Task 19 orchestrator
  - filters KV editor + JSON textareas → Tasks 17, 18
  - legacy JSON ignore → Task 11 Step 6 test, Task 10's "no legacy
    branch" Adapt
  - Go backend: RetrieveConfig, RetrievalStrategy, RetrieveRequest,
    ragimpl, factory, env cleanup, NL2SQL guard delete, service-layer
    cleanup → Tasks 1-7
  - tests → Tasks 8, 9, 11
  - error handling → Tasks 10 (validation), 18 (JSON parse), and
    backend tests in 8 and 11
  - end-to-end smoke → Task 21
  - rollback / release note → Task 22

- **Placeholder scan:** No `TBD`, `TODO`, "fill in details", "similar
  to Task N", or vague handwaves. Every code step contains the exact
  code to write. Verification steps include exact commands with
  expected outputs.

- **Type consistency:** `RetrievalStrategy.Rewrite` (Go) ↔ `rewrite`
  (frontend DataSetInfo) ↔ `rewrite` (rag wire key). Same pattern for
  `Expansion / MultiQuery / EnableRerank`. Param names emitted by
  frontend (`rewrite / expansion / multiQuery / enableRerank /
  queryMode / queryImage / targetChunkTypes / filters / retrievers /
  fusionPolicy / retrieverParams`) match the backend Adapt's
  `getDesignatedParamContent` lookups in Task 10.
