# R2-F: wire `query_strategy.llm_model_id` for retrieval enhancement

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-14-r2d-fe-retry-design.md` (R2-D-fe-Retry)
**Sibling slices:** R2-F-Rerank (deferred — needs a rerank model registered first), R2-D-fe-Wizard (deferred), R2-E (deferred)

## 1. Motivation

The 2026-05-14 R2-D-fe-Retry smoke session caught a different bug live in the workflow execution path:

```
[Error] node 知识库检索 ID 111517 failed on 0 attempt, err:
  code=105000000 message=invalid parameter :
  rag 40004: query_strategy.llm_model_id is required when query enhancement is enabled
```

Coze's `ragimpl.Retrieve` at `backend/domain/knowledge/service/ragimpl/retrieval.go:102-104` sends `query_strategy: {rewrite: true}` when the caller's `RetrievalStrategy.EnableQueryRewrite` is true. Rag's validator at `app/policy/validators/retrieval_validator.py:266-273` requires `llm_model_id` whenever ANY of `rewrite` / `expansion` / `multi_query` is true. Coze never includes `llm_model_id`. Result: every workflow knowledge-retrieve node with the "query rewrite" checkbox enabled fails with rag 40004; basic retrieval (no enhancement) still works because the field is omitted.

R2-C's error decoder did its job — the rag code + message now surface cleanly to the workflow logs (this was actually invisible before R2-C, classified as "upstream unavailable"). But the underlying contract gap remains.

R2-F fixes this gap with a system-wide default LLM model id, configured via env var to match the existing `RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID` / `RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID` pattern. Per-call LLM-model-id override (e.g. from workflow node UI) is deliberately deferred — adding env-config only is a one-commit slice; UI plumbing is a much bigger scope.

## 2. Goals & non-goals

### Goals

- `backend/conf/rag/config.go` has a new `DefaultLLMModelID string` field, populated from `${RAG_DEFAULT_LLM_MODEL_ID}` in `rag.yaml`.
- `ragimpl.Impl` holds the value at construction time, threaded through `application/knowledge/init.go`.
- `ragimpl.Retrieve` includes `llm_model_id` in `query_strategy` when `EnableQueryRewrite` is true AND the configured value is non-empty.
- When `EnableQueryRewrite` is true but no LLM model id is configured, ragimpl drops the rewrite enhancement (does NOT set `query_strategy` at all) and logs a WARN. This keeps basic retrieval working instead of failing with rag 40004.
- Unit tests cover both branches (LLM id set → rewrite + id sent; LLM id empty + rewrite requested → dropped + WARN).
- `.env.example` / `docker/.env.debug` template gains an `RAG_DEFAULT_LLM_MODEL_ID=` line (commented or with a placeholder value) so operators discover the knob naturally.

### Non-goals

- `EnableRerank` → `query_strategy.{enable_rerank, rerank_model_id}` translation. Rag's local config has no rerank model registered today; wiring this without a model available would produce a different 40004 ("rerank_model_id is required when enable_rerank is true"). Deferred to R2-F-Rerank.
- Per-call LLM model id override from workflow node UI. R2-D-fe-Wizard or a dedicated slice covers this.
- `req.DocumentIDs` filter, `MinScore` / `MaxTokens` ragimpl translation. All listed in R2-A's queued item #3; independent micro-slices.
- Adding new LLM model entries to rag's `config/model_providers.json`. The user already has `model-openai-gpt-4o-mini` registered — the env var just needs to point at it.
- Documentation overhaul. The new env var gets a one-line comment in `rag.yaml` describing its role; no broader rewrite.
- Frontend changes. The `RetrievalStrategy.EnableQueryRewrite` field already exists on the workflow node config; this slice changes only how coze translates it.

## 3. Contract change

### 3.1 New config field

`backend/conf/rag/config.go`:

```go
type RagConfig struct {
    // ... existing fields ...
    DefaultTextEmbeddingModelID  string        `yaml:"default_text_embedding_model_id"`
    DefaultImageEmbeddingModelID string        `yaml:"default_image_embedding_model_id"`
    DefaultLLMModelID            string        `yaml:"default_llm_model_id"` // NEW
}
```

`backend/conf/rag/rag.yaml`:

```yaml
rag:
  # ... existing fields ...
  default_text_embedding_model_id: "${RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID}"
  default_image_embedding_model_id: "${RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID}"
  # LLM model id used for query enhancement (rewrite / expansion / multi_query)
  # in retrieval. When empty, EnableQueryRewrite on a retrieval request is
  # silently dropped with a WARN log to avoid rag's 40004 validation error.
  default_llm_model_id: "${RAG_DEFAULT_LLM_MODEL_ID}"
```

### 3.2 ragimpl.Impl extension

`backend/domain/knowledge/service/ragimpl/factory.go`:

`Impl` struct gains:

```go
type Impl struct {
    // ... existing fields ...
    defaultTextEmbeddingModelID  string
    defaultImageEmbeddingModelID string
    defaultLLMModelID            string // NEW
    // ... rest ...
}
```

`New(...)` constructor signature appends one parameter:

```go
func New(
    rag contract.Client,
    db *gorm.DB,
    idgen idgen.IDGenerator,
    resolver TenantResolver,
    storage storage.Storage,
    defaultTextModel, defaultImageModel, defaultLLMModel string, // CHANGED: third string is new
) *Impl
```

(Adding a third positional string at the end mirrors the existing two-string tail.)

### 3.3 Retrieval translation

`backend/domain/knowledge/service/ragimpl/retrieval.go:102-104` becomes:

```go
if req.Strategy.EnableQueryRewrite {
    if i.defaultLLMModelID != "" {
        ragReq.QueryStrategy = map[string]any{
            "rewrite":      true,
            "llm_model_id": i.defaultLLMModelID,
        }
    } else {
        // EnableQueryRewrite was requested but RAG_DEFAULT_LLM_MODEL_ID is
        // unset. Rag's validator rejects {rewrite:true} without an LLM
        // model id (40004), so dropping the enhancement is preferable to
        // failing the whole retrieval. Basic retrieval still completes.
        logs.CtxWarnf(ctx, "ragimpl.Retrieve: EnableQueryRewrite=true but RAG_DEFAULT_LLM_MODEL_ID is empty; dropping rewrite to avoid rag 40004")
    }
}
```

### 3.4 Composition wiring

`backend/application/knowledge/init.go` — the `ragimpl.New(...)` call gains `cfg.Rag.DefaultLLMModelID` as the new positional argument, sitting alongside the existing two model id strings.

## 4. Architecture

### 4.1 Flow

```
workflow node → service.RetrieveRequest{Strategy{EnableQueryRewrite: true}}
  → ragimpl.Retrieve
    → if Strategy.EnableQueryRewrite && i.defaultLLMModelID != "":
        ragReq.QueryStrategy = {rewrite: true, llm_model_id: i.defaultLLMModelID}
      else (EnableQueryRewrite && id empty):
        log WARN; QueryStrategy stays nil
    → rag.Client.Retrieve(...) → rag accepts (validator passes)
  ← results
```

No changes to anything else in the retrieval path. `Strategy.TopK`, `Strategy.SearchType` translations stay as-is.

### 4.2 Files touched

| File | Change |
|---|---|
| `backend/conf/rag/config.go` | Add `DefaultLLMModelID string` field |
| `backend/conf/rag/rag.yaml` | Add `default_llm_model_id` line + comment |
| `backend/domain/knowledge/service/ragimpl/factory.go` | Add field + extend `New` signature |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | Replace lines 102-104 per §3.3 |
| `backend/application/knowledge/init.go` | Thread `cfg.Rag.DefaultLLMModelID` into `ragimpl.New` |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | Two new test cases (LLM id set; LLM id empty) |
| `backend/domain/knowledge/service/ragimpl/knowledge_test.go` | Update `newTestImpl(...)` to pass an empty / configurable LLM id; existing tests stay green |
| `backend/domain/knowledge/service/ragimpl/integration_test.go` | Update the `New(...)` call in the integration test smoke (build-tag-gated) |
| `docker/.env.debug` (template) | Add `export RAG_DEFAULT_LLM_MODEL_ID="..."` line, commented |

## 5. Components

### 5.1 Config wiring

The pattern mirrors R2-A Phase C's `storage.Storage` injection: a new field added at construction time, threaded from `application/knowledge/init.go` through ragimpl's `New`. No dependency on database state; no migration; no runtime surprise.

### 5.2 Drop-on-empty rationale

The alternative is to send `{rewrite: true, llm_model_id: ""}` and let rag reject. That would surface as `ErrKnowledgeInvalidParamCode` ("must be a non-empty string"), classified correctly by R2-C's decoder. But the user-facing outcome is worse: retrieval fails entirely instead of degrading gracefully to basic retrieval.

Logging at WARN level (not ERROR) reflects the severity: the request still succeeds; the user just doesn't get the enhancement they asked for. A WARN line per retrieval is acceptable noise — production deployments will set the env var once and the line disappears.

### 5.3 `newTestImpl` consistency

`backend/domain/knowledge/service/ragimpl/knowledge_test.go::newTestImpl` already pins `defaultTextEmbeddingModelID` and `defaultImageEmbeddingModelID` to hardcoded test values. Adding a third hardcoded value (`"test-llm-model"` or `""`, depending on what each test needs) mirrors the existing shape. The retrieval test suite gains specific cases that override per-test as needed.

## 6. Testing

### 6.1 Unit tests (`retrieval_test.go`)

New tests:

- `TestRagimpl_Retrieve_EnableQueryRewrite_WithLLMModelID` — set `i.defaultLLMModelID = "model-openai-gpt-4o-mini"`, call `Retrieve` with `EnableQueryRewrite: true`. Assert: `fakeClient.retrieveFunc` receives a request whose `QueryStrategy["rewrite"] == true` AND `QueryStrategy["llm_model_id"] == "model-openai-gpt-4o-mini"`.
- `TestRagimpl_Retrieve_EnableQueryRewrite_NoLLMModelID_DropsEnhancement` — set `i.defaultLLMModelID = ""`, call `Retrieve` with `EnableQueryRewrite: true`. Assert: `fakeClient.retrieveFunc` receives a request whose `QueryStrategy` is `nil` (not set). The rag call still succeeds with basic retrieval.

The existing `TestRetrieve_*` tests stay green because they don't set `EnableQueryRewrite`. Verify in plan.

### 6.2 Existing tests

Tests that construct `newTestImpl` or call `ragimpl.New(...)` directly will not compile after the signature change. The plan enumerates these sites (R2-A Phase C did the same exercise for storage). Likely: `knowledge_test.go::newTestImpl`, `integration_test.go::TestRetryIntegration` (or whatever it's called).

### 6.3 No httptest changes

The rag-client side (`backend/infra/rag/client.go::Retrieve`) is unchanged — it forwards whatever `QueryStrategy` map ragimpl built. R2-A's httptest tests for `Retrieve` lock the wire shape at the client layer; this slice changes only what ragimpl puts INTO the wire.

### 6.4 Smoke

Set `RAG_DEFAULT_LLM_MODEL_ID=model-openai-gpt-4o-mini` in `docker/.env.debug`, restart coze server, trigger a workflow that calls the knowledge-retrieve node with query rewrite enabled. Expect: workflow succeeds, retrieval returns hits, `monitor` shows the rag retrieval request hitting `query_strategy.llm_model_id`. Optional verification via rag worker logs that the LLM was actually invoked for query rewrite.

## 7. Failure modes

| Scenario | Behavior |
|---|---|
| Env var set, model id valid | Rewrite sent with llm_model_id → rag accepts → enhanced retrieval. |
| Env var set, model id is bogus (e.g. typo) | Rag returns 4xx "model not found"; R2-C decoder surfaces the rag code + message; workflow node fails with a clear error. User can fix the env. |
| Env var empty, EnableQueryRewrite=true | WARN logged, rewrite dropped, basic retrieval succeeds. User sees the WARN in coze logs if they look. |
| Env var empty, EnableQueryRewrite=false | No change from today. Basic retrieval. No WARN. |
| Env var contains a non-LLM model id (e.g. an embedding model) | Rag's validator may accept it (just checks "model exists with capability LLM"); if the model lacks llm capability, rag returns 4xx. Same surface as bogus id case. |

## 8. Compatibility & rollout

- Fully additive at the config layer: deploys without `RAG_DEFAULT_LLM_MODEL_ID` set continue to work, just with rewrite silently dropped.
- The `ragimpl.New` signature change is internal to the rag-backed path (legacy mode is unaffected).
- No DB migration. No IDL change. No frontend change.
- Documentation: the new env var is discoverable in `.env.debug` template and `rag.yaml`'s inline comment.

## 9. Open questions

None blocking. Two minor items deferred to plan-time:

1. **Exact placement of the new `New(...)` param** — at the end (after `defaultImageModel`), grouped with the two existing model id strings. Plan reads the current signature and matches the order verbatim.
2. **`docker/.env.debug` template line wording** — whether to comment-out the line or set a default value. Plan picks the most consistent option with the surrounding env-var conventions (the two other `RAG_DEFAULT_*` model id vars have no commented-out template line today; matching them means we add a real line with a placeholder like `model-openai-gpt-4o-mini` or leave the value empty).
