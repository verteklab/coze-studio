# R2-F: wire `query_strategy.llm_model_id` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-14-r2f-retrieval-llm-model-id-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation, base `75035b8c`)
**Goal:** Workflow knowledge-retrieve nodes with query rewrite enabled stop failing with rag 40004 by including `llm_model_id` from a new `RAG_DEFAULT_LLM_MODEL_ID` env var; when the env is empty, ragimpl drops the rewrite enhancement (logged WARN) so basic retrieval still completes.

**Architecture:** New `default_llm_model_id` config field threaded from `rag.yaml` → `ragconf.Config` → `application/knowledge/init.go` → `ragimpl.New(...)` → `Impl.defaultLLMModelID`. `ragimpl.Retrieve` checks the field at request-build time: present → include `llm_model_id` in `query_strategy`; empty → drop rewrite + WARN. Single-commit slice; mirrors R2-A Phase C's storage injection precedent verbatim.

**Tech Stack:** Go 1.24 (pinned via `GOTOOLCHAIN`), YAML config, no new dependencies.

---

## Pre-flight: facts the plan depends on

- `backend/conf/rag/config.go:44-45` has the two existing `DefaultTextEmbeddingModelID` / `DefaultImageEmbeddingModelID` fields. New field appends at line 46.
- `backend/conf/rag/rag.yaml:8-9` has the two `${RAG_DEFAULT_*_MODEL_ID}` substitutions. New line appends at line 10.
- `backend/domain/knowledge/service/ragimpl/factory.go:36-67` is the `Impl` struct + `New(...)` constructor. Current `New` signature: `New(rag, db, idgen, resolver, storage, defaultTextModel, defaultImageModel string)`. R2-F appends `defaultLLMModel string` as positional arg 8.
- `backend/domain/knowledge/service/ragimpl/retrieval.go:102-104` is the ONLY existing code path setting `ragReq.QueryStrategy`. The full replacement block lands here.
- `ragimpl.New(...)` callers (3 sites — all must update):
  - `backend/application/knowledge/init.go:98` — production call.
  - `backend/domain/knowledge/service/ragimpl/knowledge_test.go:237-249` — `newTestImpl` helper used by ~6 tests. Constructs `Impl{}` literal directly, NOT via `New(...)`, so the change is adding a field assignment to the literal.
  - `backend/domain/knowledge/service/ragimpl/integration_test.go` — build-tag-gated; calls `New(...)` directly.
- `retrieval_test.go` already has `TestRetrieve_HappyPath` (line 49) and `TestRetrieve_RejectsNL2SQL` (line 33). R2-F appends two new tests below them, reusing the `fakeClient.retrieveFunc` capture pattern from `TestRetrieve_HappyPath`.
- `docker/.env.debug` template already exports `RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID` and `RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID` (per project memory item #2). New line follows the same `export RAG_DEFAULT_LLM_MODEL_ID="..."` shape.
- `logs.CtxWarnf(ctx, "...", args)` is the standard WARN log call (used elsewhere in ragimpl, e.g. `document.go::DeleteDocument` rollback path).

---

## Task 1: Add `DefaultLLMModelID` to config

**Files:**
- Modify: `backend/conf/rag/config.go:44-45`
- Modify: `backend/conf/rag/rag.yaml:8-9`

- [ ] **Step 1: Add the struct field.**

Find at `backend/conf/rag/config.go:44-45`:

```go
	DefaultTextEmbeddingModelID  string        `yaml:"default_text_embedding_model_id"`
	DefaultImageEmbeddingModelID string        `yaml:"default_image_embedding_model_id"`
```

Replace with:

```go
	DefaultTextEmbeddingModelID  string        `yaml:"default_text_embedding_model_id"`
	DefaultImageEmbeddingModelID string        `yaml:"default_image_embedding_model_id"`
	// DefaultLLMModelID is the LLM model id used for query enhancement
	// (rewrite / expansion / multi_query) on retrieval requests. When empty,
	// ragimpl drops the enhancement with a WARN log to avoid rag's 40004
	// "query_strategy.llm_model_id is required" validation error.
	DefaultLLMModelID            string        `yaml:"default_llm_model_id"`
```

- [ ] **Step 2: Add the YAML key.**

Find at `backend/conf/rag/rag.yaml:8-9`:

```yaml
  default_text_embedding_model_id: "${RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID}"
  default_image_embedding_model_id: "${RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID}"
```

Replace with:

```yaml
  default_text_embedding_model_id: "${RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID}"
  default_image_embedding_model_id: "${RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID}"
  # LLM model id for query enhancement (rewrite/expansion/multi_query) in retrieval.
  # Empty value disables enhancement at the ragimpl layer with a WARN log.
  default_llm_model_id: "${RAG_DEFAULT_LLM_MODEL_ID}"
```

- [ ] **Step 3: Build the config package.**

Run: `cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go build ./conf/rag/...`
Expected: clean.

- [ ] **Step 4: Do not commit yet.** Continues into Task 2.

---

## Task 2: Extend `ragimpl.Impl` + `New(...)` signature

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go:36-67`

- [ ] **Step 1: Add the field to `Impl`.**

Find at `factory.go:36-48`:

```go
type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver
	// storage is used by CreateDocument to fetch file bytes from MinIO and
	// forward them to rag as a multipart body. Required since the 2026-05-14
	// rag contract change; previously rag fetched by source_uri itself.
	storage storage.Storage

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
}
```

Replace with:

```go
type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver
	// storage is used by CreateDocument to fetch file bytes from MinIO and
	// forward them to rag as a multipart body. Required since the 2026-05-14
	// rag contract change; previously rag fetched by source_uri itself.
	storage storage.Storage

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
	// defaultLLMModelID is the rag model id used for query_strategy.llm_model_id
	// when the caller sets EnableQueryRewrite. Empty value disables the
	// enhancement at request-build time (see retrieval.go); rag would otherwise
	// reject with 40004 "llm_model_id is required when query enhancement is
	// enabled".
	defaultLLMModelID string
}
```

- [ ] **Step 2: Extend `New(...)` signature + body.**

Find at `factory.go:50-67`:

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

Replace with:

```go
func New(
	rag contract.Client,
	db *gorm.DB,
	idgen idgen.IDGenerator,
	resolver TenantResolver,
	storage storage.Storage,
	defaultTextModel, defaultImageModel, defaultLLMModel string,
) *Impl {
	return &Impl{
		rag:                          rag,
		mapping:                      NewMappingRepo(db),
		idgen:                        idgen,
		resolver:                     resolver,
		storage:                      storage,
		defaultTextEmbeddingModelID:  defaultTextModel,
		defaultImageEmbeddingModelID: defaultImageModel,
		defaultLLMModelID:            defaultLLMModel,
	}
}
```

- [ ] **Step 3: Build the ragimpl package alone (expect failures).**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: clean. (`Impl{}` literal in `newTestImpl` doesn't yet set the new field, but that's just a zero-value default which is valid Go — empty LLM id is the "drop enhancement" branch and behaves correctly. Tests stay green at this step.)

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: FAILS at `application/knowledge/init.go:98` because the call site passes only 7 args. Fixed in Task 4.

- [ ] **Step 4: Do not commit yet.** Continues into Task 3.

---

## Task 3: Rewrite `retrieval.go` to honor `defaultLLMModelID`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/retrieval.go:102-108`

- [ ] **Step 1: Replace the rewrite-translation block.**

Find at `retrieval.go:102-108`:

```go
		if req.Strategy.EnableQueryRewrite {
			ragReq.QueryStrategy = map[string]any{"rewrite": true}
		}
		// EnableRerank is exposed through fusion_policy or retriever_params on
		// rag; the precise mapping is pending (see rag/docs §10.5). Leaving
		// the field un-translated keeps us forward-compatible.
```

Replace with:

```go
		if req.Strategy.EnableQueryRewrite {
			if i.defaultLLMModelID != "" {
				ragReq.QueryStrategy = map[string]any{
					"rewrite":      true,
					"llm_model_id": i.defaultLLMModelID,
				}
			} else {
				// EnableQueryRewrite was requested but RAG_DEFAULT_LLM_MODEL_ID
				// is unset. Rag's validator rejects {rewrite:true} without an
				// llm_model_id (40004), so dropping the enhancement is
				// preferable to failing the whole retrieval. Basic retrieval
				// still completes.
				logs.CtxWarnf(ctx, "ragimpl.Retrieve: EnableQueryRewrite=true but RAG_DEFAULT_LLM_MODEL_ID is empty; dropping rewrite to avoid rag 40004")
			}
		}
		// EnableRerank is exposed through fusion_policy or retriever_params on
		// rag; the precise mapping is pending (see rag/docs §10.5). Leaving
		// the field un-translated keeps us forward-compatible.
```

- [ ] **Step 2: Verify `logs` import is present.**

Open `retrieval.go` and check the import block contains `"github.com/coze-dev/coze-studio/backend/pkg/logs"`. If absent (other ragimpl files like `document.go` already use it, so likely present), add it.

- [ ] **Step 3: Build ragimpl alone.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: clean.

- [ ] **Step 4: Do not commit yet.** Continues into Task 4.

---

## Task 4: Update all `New(...)` / `newTestImpl` callers

**Files:**
- Modify: `backend/application/knowledge/init.go:98-105`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go:237-249`
- Modify: `backend/domain/knowledge/service/ragimpl/integration_test.go` (the `New(...)` call inside the build-tag-gated test)
- Modify: `docker/.env.debug` template — add new env line

- [ ] **Step 1: Update `application/knowledge/init.go` production call.**

Find at `init.go:98-105`:

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

Replace with:

```go
	domainSVC := ragimpl.New(
		client,
		c.DB,
		c.IDGen,
		resolver,
		c.Storage,
		cfg.Rag.DefaultTextEmbeddingModelID,
		cfg.Rag.DefaultImageEmbeddingModelID,
		cfg.Rag.DefaultLLMModelID,
	)
```

- [ ] **Step 2: Update `newTestImpl` helper to set the new field.**

Find at `knowledge_test.go:237-249`:

```go
func newTestImpl(t *testing.T, fc *fakeClient, ids ...int64) *Impl {
	t.Helper()
	db := setupDB(t)
	return &Impl{
		rag:                          fc,
		mapping:                      NewMappingRepo(db),
		idgen:                        &stubIDGen{ids: ids},
		resolver:                     NewEnvTenantResolver("test-tenant"),
		storage:                      &stubStorage{},
		defaultTextEmbeddingModelID:  "text-model-default",
		defaultImageEmbeddingModelID: "image-model-default",
	}
}
```

Replace with:

```go
func newTestImpl(t *testing.T, fc *fakeClient, ids ...int64) *Impl {
	t.Helper()
	db := setupDB(t)
	return &Impl{
		rag:                          fc,
		mapping:                      NewMappingRepo(db),
		idgen:                        &stubIDGen{ids: ids},
		resolver:                     NewEnvTenantResolver("test-tenant"),
		storage:                      &stubStorage{},
		defaultTextEmbeddingModelID:  "text-model-default",
		defaultImageEmbeddingModelID: "image-model-default",
		defaultLLMModelID:            "llm-model-default",
	}
}
```

(All existing tests that use `newTestImpl` will get a non-empty `defaultLLMModelID` by default — `"llm-model-default"`. The R2-F retrieval tests added in Task 5 override per-test as needed.)

- [ ] **Step 3: Update integration test `New(...)` call.**

In `backend/domain/knowledge/service/ragimpl/integration_test.go`, find the `ragimpl.New(...)` call (build-tag-gated by `//go:build integration`). It currently passes 7 args (the two model id strings at the end coming from env). Append `os.Getenv("RAG_DEFAULT_LLM_MODEL_ID")` as the 8th positional arg, mirroring the existing two embedding model id env reads.

Example (the surrounding code may have slightly different variable names — read first to confirm the exact form, then add the line analogously):

```go
	impl := ragimpl.New(
		client,
		db,
		idgen,
		resolver,
		newSmokeStorage(t),
		os.Getenv("RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID"),
		os.Getenv("RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID"),
		os.Getenv("RAG_DEFAULT_LLM_MODEL_ID"),
	)
```

- [ ] **Step 4: Update `docker/.env.debug` template.**

Find the block in `docker/.env.debug` where the two existing `RAG_DEFAULT_*_MODEL_ID` exports live (project memory item #2 has the recipe). After the existing `RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID` export, add:

```bash
export RAG_DEFAULT_LLM_MODEL_ID="model-openai-gpt-4o-mini"
```

(Use the model id known to be present in the user's `config/model_providers.json`. If a different id is appropriate for this checkout, the operator overrides; the default points at the model the user already has registered.)

If `docker/.env.debug` is gitignored (project memory says it's per-session), and the canonical template lives elsewhere (e.g. `.env.example` or a docs file), update the canonical location instead. Plan reader: verify with `ls -la docker/.env.debug*` before editing — pick the tracked file.

- [ ] **Step 5: Full backend build.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean.

- [ ] **Step 6: Do not commit yet.** Continues into Task 5.

---

## Task 5: Add retrieval unit tests + commit

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/retrieval_test.go` — append two new tests

- [ ] **Step 1: Append the two new tests after `TestRetrieve_HappyPath`.**

Use Read to locate the file's last line. Append:

```go
// TestRetrieve_EnableQueryRewrite_WithLLMModelID verifies that when the caller
// requests EnableQueryRewrite AND defaultLLMModelID is configured, ragimpl
// sends both rewrite=true AND llm_model_id in the rag query_strategy. This is
// the post-R2-F happy path; before R2-F, only rewrite was sent and rag 40004'd.
func TestRetrieve_EnableQueryRewrite_WithLLMModelID(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	i.defaultLLMModelID = "model-openai-gpt-4o-mini" // explicit override; newTestImpl's default is "llm-model-default"
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			EnableQueryRewrite: true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.QueryStrategy, "rewrite enhancement should be sent when LLM id is configured")
	require.Equal(t, true, capturedReq.QueryStrategy["rewrite"])
	require.Equal(t, "model-openai-gpt-4o-mini", capturedReq.QueryStrategy["llm_model_id"])
}

// TestRetrieve_EnableQueryRewrite_NoLLMModelID_DropsEnhancement verifies that
// when EnableQueryRewrite is true but defaultLLMModelID is empty, ragimpl drops
// the enhancement entirely (no query_strategy sent) rather than triggering rag
// 40004. Basic retrieval still completes. The WARN log is fire-and-forget; the
// test asserts the wire-level behavior, not the log.
func TestRetrieve_EnableQueryRewrite_NoLLMModelID_DropsEnhancement(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	i.defaultLLMModelID = "" // explicit empty — simulates RAG_DEFAULT_LLM_MODEL_ID unset
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			EnableQueryRewrite: true,
		},
	})
	require.NoError(t, err, "basic retrieval should still succeed; enhancement is dropped silently")
	require.NotNil(t, capturedReq)
	require.Nil(t, capturedReq.QueryStrategy, "query_strategy must be nil when LLM id is empty, even with EnableQueryRewrite=true")
}
```

- [ ] **Step 2: Run the new tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run "TestRetrieve_EnableQueryRewrite" -v`
Expected: both PASS.

- [ ] **Step 3: Run the full retrieval test set + adjacent tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run "TestRetrieve" -v`
Expected: all `TestRetrieve_*` PASS (HappyPath, RejectsNL2SQL, EnableQueryRewrite_WithLLMModelID, EnableQueryRewrite_NoLLMModelID_DropsEnhancement).

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS. (`newTestImpl`'s new default `"llm-model-default"` doesn't affect existing tests because none of them set `EnableQueryRewrite`.)

- [ ] **Step 4: Build + vet.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean.

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go vet ./domain/knowledge/service/ragimpl/... ./application/knowledge/... ./conf/rag/...`
Expected: clean.

- [ ] **Step 5: Commit.**

Stage exactly what changed (`git diff --name-only HEAD` to confirm scope), then:

```bash
git add backend/conf/rag/config.go \
        backend/conf/rag/rag.yaml \
        backend/domain/knowledge/service/ragimpl/factory.go \
        backend/domain/knowledge/service/ragimpl/retrieval.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go \
        backend/domain/knowledge/service/ragimpl/integration_test.go \
        backend/domain/knowledge/service/ragimpl/retrieval_test.go \
        backend/application/knowledge/init.go \
        docker/.env.debug
# Note: .env.debug is per-session per project memory — include only if it's
# tracked. If untracked, skip it from the commit but still update it locally.
git commit -m "$(cat <<'EOF'
feat(rag): wire query_strategy.llm_model_id for retrieval enhancement

Workflow knowledge-retrieve nodes with query rewrite enabled have been
failing with rag 40004 "query_strategy.llm_model_id is required when
query enhancement is enabled" since coze's ragimpl.Retrieve always sent
{rewrite:true} without an LLM model id. R2-C's decoder surfaced the
upstream error cleanly during R2-D-fe-Retry smoke; this slice fixes
the underlying contract gap.

Adds RAG_DEFAULT_LLM_MODEL_ID env var → ragconf.Config.DefaultLLMModelID
→ ragimpl.Impl.defaultLLMModelID. retrieval.go's rewrite branch now:
  - if LLM id configured: sends {rewrite:true, llm_model_id:<id>} →
    rag accepts → enhanced retrieval works
  - if LLM id empty: drops query_strategy entirely + logs WARN → basic
    retrieval still succeeds, no 40004

EnableRerank wiring deferred to R2-F-Rerank (rag's local config has no
rerank model registered today). Per-call LLM model id override from
workflow node UI deferred to a future slice. Service.Knowledge interface
unchanged.
EOF
)"
```

---

## Task 6: Manual smoke (optional)

The unit tests assert the wire-shape contract end-to-end through ragimpl. Live smoke verifies the env wiring works in a real environment.

- [ ] **Step 1: Add `RAG_DEFAULT_LLM_MODEL_ID="model-openai-gpt-4o-mini"` to `docker/.env.debug`.** (Already done in Task 4 if applicable; this step is the reminder.)

- [ ] **Step 2: Restart coze server** with the new env loaded:

```bash
pkill -f opencoze 2>/dev/null
cd /Users/liuxinyu/workspace/coze-studio && GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
until lsof -iTCP:8888 -sTCP:LISTEN >/dev/null 2>&1 || grep -qE "panic:|FATAL" /tmp/coze-server.log; do sleep 3; done
```

- [ ] **Step 3: Trigger a workflow that uses the knowledge-retrieve node with query rewrite enabled.**

In the coze UI workflow editor, find or create a workflow with a knowledge-retrieve node pointing at a rag-backed KB and with the "query rewrite" checkbox checked. Execute the workflow.

Expected: the workflow executes successfully (no `rag 40004` error). The retrieval returns hits.

In `monitor` or `/tmp/coze-server.log`, look for the rag retrieval request — `req.query_strategy` should contain `{"rewrite": true, "llm_model_id": "model-openai-gpt-4o-mini"}`.

- [ ] **Step 4: Test the drop path.** Unset `RAG_DEFAULT_LLM_MODEL_ID` in `.env.debug` (or set to empty string), restart server, re-run the same workflow.

Expected: workflow still completes (basic retrieval, no enhancement). Coze log contains `[Warn] ragimpl.Retrieve: EnableQueryRewrite=true but RAG_DEFAULT_LLM_MODEL_ID is empty; dropping rewrite to avoid rag 40004`.

- [ ] **Step 5: No commit.** Smoke does not change tracked files.

---

## Out of scope (do not address in this plan)

- `EnableRerank` → `query_strategy.{enable_rerank, rerank_model_id}` translation (R2-F-Rerank — needs a rerank model registered first).
- Per-call LLM model id override from workflow node UI.
- `req.DocumentIDs` filter, `MinScore` / `MaxTokens` ragimpl translation (R2-A queued #3 — independent micro-slices).
- Adding new LLM model entries to rag's `config/model_providers.json` (operator-side concern, not a code change).
- Service.Knowledge interface changes (this slice stays inside ragimpl).
- IDL changes, frontend changes.

---

## Self-review checklist (filled in)

1. **Spec coverage:**
   - §3.1 config field + yaml → Task 1
   - §3.2 ragimpl.Impl field + New(...) signature → Task 2
   - §3.3 retrieval translation block → Task 3
   - §3.4 composition wiring (init.go) → Task 4 Step 1
   - §4.2 file table → Tasks 1-5 cover all 8 files
   - §6.1 two unit tests → Task 5 Step 1
   - §6.2 existing tests stay green → Task 5 Step 3 (full sweep)
   - §6.4 smoke → Task 6
   - §9 open questions (param placement; .env.debug wording) → resolved inline in Task 2 (append after defaultImage) and Task 4 Step 4 (real value `model-openai-gpt-4o-mini`)

2. **Placeholders:** none. Task 4 Step 3 has a defensive "read first to confirm the exact form" note for the integration test edit; this is plan-time prudence, not a TBD. Task 4 Step 4 acknowledges `.env.debug` may be gitignored and instructs the executor to check.

3. **Type consistency:**
   - `DefaultLLMModelID` config field ↔ `default_llm_model_id` yaml key ↔ `defaultLLMModelID` struct field ↔ `defaultLLMModel` constructor param: all consistent (Go yaml unmarshal maps snake_case → CamelCase via the tag).
   - `New(...)` 8-arg signature: 5 deps + 3 model id strings — matches between Task 2 (definition) and Task 4 Step 1 (production call) and Task 4 Step 3 (integration test call).
   - `newTestImpl` adds `defaultLLMModelID: "llm-model-default"` literal — matches the existing two model id fields' convention.
   - Retrieval tests override `i.defaultLLMModelID` directly on the returned `*Impl` — works because the field is unexported but same-package.
