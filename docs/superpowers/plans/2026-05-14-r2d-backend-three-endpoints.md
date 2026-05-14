# R2-D backend: three rag endpoints — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-14-r2d-backend-three-endpoints-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation, base `3729a8bb`)
**Goal:** Wire three new rag endpoints (`/knowledgebases/{kb_id}/capabilities`, `POST /knowledgebases/{kb_id}/documents/{doc_id}/retry`, `/document-parameter-schemas`) through coze's Go layers up to `ragimpl.Impl`, with contract types and tests. Stop at ragimpl — `service.Knowledge` interface untouched (deferred to R2-D-frontend).

**Architecture:** Three thematic commits, one per endpoint. Each commit adds: (a) wire-shape DTOs in `contract/rag/types.go`; (b) interface method on `contract.Client`; (c) implementation on `*Client` in `infra/rag/client.go`; (d) implementation on `fakeClient` in `ragimpl/knowledge_test.go`; (e) implementation on `ragimpl.Impl`; (f) httptest contract test locking the wire shape; (g) ragimpl unit test verifying mapping lookup + pass-through. Pass-through only — no `entity.*` translation, no coze-side state mutation, no service interface change.

**Tech Stack:** Go 1.24 (pinned via `GOTOOLCHAIN`), `encoding/json`, `net/http/httptest`. No new external deps.

---

## Pre-flight: facts the plan depends on

Locked during plan-writing.

- `backend/infra/contract/rag/client.go:28-55` defines `Client` as an INTERFACE. The concrete `*Client` in `infra/rag/client.go:47` and the `fakeClient` in `ragimpl/knowledge_test.go:41` both satisfy it via compile-time `var _ contract.Client = (*X)(nil)` checks. Adding methods to the interface REQUIRES adding to both implementations or the package fails to compile.
- `backend/infra/rag/client.go` method order (post-R2-C): `Ready` → `doJSON`/`doOnce`/`doMultipart` helpers → `ListModelProviders` → KB methods (`CreateKB`, `GetKB`, `UpdateKB`, `DeleteKB`, `ListKBs`) → Document methods (`CreateDocument`, `GetDocument`, `ListDocuments`, `DeleteDocument`) → `GetTask` → `Retrieve`. New methods slot in by domain.
- `backend/domain/knowledge/service/ragimpl/` file split: `document.go` for doc-scoped, `knowledge.go` for KB-scoped, `retrieval.go` for retrieval, `unsupported.go` for bucket-B stubs. `parameter_schemas.go` does not yet exist; Phase C creates it.
- `fakeClient` stub pattern (`knowledge_test.go`): each interface method has a corresponding `Func`-suffixed field; method body is `if f.fooFunc != nil { return f.fooFunc(args) }; return &Zero{}, nil`. Plan matches this pattern verbatim.
- `service.Knowledge` interface (`backend/domain/knowledge/service/interface.go:32`) — **NOT TOUCHED** by R2-D-backend per spec §2 non-goals. ragimpl's method set widens past the interface; Go allows this and the `var _ service.Knowledge = (*Impl)(nil)` check still passes.
- Mapping helpers available: `MappingRepo.KBByCozeID(ctx, cozeKBID) → *KBMapping{RagKBID, ...}` and `MappingRepo.DocByCozeID(ctx, cozeDocID) → *DocMapping{RagDocID, KBID, ...}`. Both return `ErrMappingNotFound` (wrapping `gorm.ErrRecordNotFound`) when absent — the plan's tests don't need new mapping helpers.
- `newTestImpl(t, fc, ids...)` helper at `knowledge_test.go:213` constructs an `*Impl` with the in-memory SQLite db, the fakeClient, an idgen seeded with `ids`, and an env tenant resolver. Plan's ragimpl tests use this.
- Live wire shapes verified against rag `0e1f49b` during spec-writing — see spec §3.1 for the exact JSON.

### Pre-flight resolutions for spec §10 open questions

1. **Retry's effect on `rag_doc_mapping.last_task_id`** → **NO update** in R2-D-backend. Per spec §10 recommendation (a). The ragimpl `RetryDocument` is a pass-through; mapping mutation is R2-D-frontend's concern when wiring the actual retry-button UX.
2. **Where `ListDocumentParameterSchemas` lives** → **new file** `backend/domain/knowledge/service/ragimpl/parameter_schemas.go`. The method is not KB-scoped, not document-scoped, and not retrieval-scoped; existing files don't fit. A new file with one method is small but unambiguous.

---

## Phase A — `RetryDocument` (commit 1)

### Task A1: Add `RetryDocument` to `contract.Client` interface

**Files:**
- Modify: `backend/infra/contract/rag/client.go:44-48` (Documents group in interface)

- [ ] **Step 1: Insert the method signature inside the Documents group.**

Find at `client.go:44-48`:

```go
	// Documents — all nested under their KB on the rag side.
	CreateDocument(ctx context.Context, tenantID, kbID string, req *CreateDocumentRequest) (*CreateDocumentResponse, error)
	GetDocument(ctx context.Context, tenantID, kbID, docID string) (*Document, error)
	ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*ListDocumentsResponse, error)
	DeleteDocument(ctx context.Context, tenantID, kbID, docID string) error
```

Append the new signature inside the Documents group (after `DeleteDocument`):

```go
	// Documents — all nested under their KB on the rag side.
	CreateDocument(ctx context.Context, tenantID, kbID string, req *CreateDocumentRequest) (*CreateDocumentResponse, error)
	GetDocument(ctx context.Context, tenantID, kbID, docID string) (*Document, error)
	ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*ListDocumentsResponse, error)
	DeleteDocument(ctx context.Context, tenantID, kbID, docID string) error
	// RetryDocument re-runs ingestion for a failed task. Rag returns the
	// standard UploadDocumentResponse, identical in shape to CreateDocument,
	// so the response type is reused.
	RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*CreateDocumentResponse, error)
```

- [ ] **Step 2: Build the contract package to see expected failures.**

Run: `cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go build ./infra/contract/rag/...`
Expected: clean (interface methods don't need implementations to compile within the same package).

- [ ] **Step 3: Build downstream to see what needs implementing.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: fails at `infra/rag/client.go:47` (`*Client does not implement contract.Client`) and at `ragimpl/knowledge_test.go` (fakeClient assertion). Both fixed in Tasks A2 and A4.

- [ ] **Step 4: Do not commit yet.** Phase A commits at the end of Task A6.

---

### Task A2: Implement `RetryDocument` on `*Client`

**Files:**
- Modify: `backend/infra/rag/client.go` (insert method after `DeleteDocument`, around line 393)

- [ ] **Step 1: Append the method.**

Find `DeleteDocument` (currently ends around line 394). Insert the new method directly after:

```go
// RetryDocument re-runs ingestion for a failed document task. Rag's response
// is the standard UploadDocumentResponse shape — same as CreateDocument — so
// we reuse contract.CreateDocumentResponse rather than introducing an alias.
func (c *Client) RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
	out := &contract.CreateDocumentResponse{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID + "/retry"
	if err := c.doJSON(ctx, http.MethodPost, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 2: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./infra/rag/...`
Expected: clean. `var _ contract.Client = (*Client)(nil)` at line 47 now passes.

- [ ] **Step 3: Do not commit yet.** Continues into Task A3.

---

### Task A3: httptest contract test for `RetryDocument`

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append test after the last existing test

- [ ] **Step 1: Append `TestRetryDocument` to client_test.go.**

Use Read to locate the file's last function; insert after it. The file already imports `context`, `encoding/json`, `net/http`, `net/http/httptest`, `strings`, `time`, plus `ragconf` and `contract` — all needed here.

```go
// TestRetryDocument locks rag's POST .../documents/{doc_id}/retry wire shape.
// Rag emits the standard UploadDocumentResponse envelope (same as CreateDocument);
// the test asserts the wire path + headers and that the response decodes into
// the existing CreateDocumentResponse type.
func TestRetryDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/documents/doc-1/retry"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"doc_id":  "doc-1",
				"task_id": "task-retry-1",
				"status":  "pending",
			},
			"request_id": "req-r1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.RetryDocument(context.Background(), "t1", "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("RetryDocument: %v", err)
	}
	if got.DocID != "doc-1" || got.TaskID != "task-retry-1" || got.Status != "pending" {
		t.Errorf("decoded = %+v, want {doc-1, task-retry-1, pending}", got)
	}
}
```

- [ ] **Step 2: Run the test.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/ -run TestRetryDocument -v`
Expected: PASS.

But the FULL package test will still fail at the `ragimpl` package because `fakeClient` doesn't implement `RetryDocument` yet (Task A4 fixes).

- [ ] **Step 3: Do not commit yet.** Continues into Task A4.

---

### Task A4: Implement `RetryDocument` on `fakeClient`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` — add stub field + method

- [ ] **Step 1: Add the stub field to `fakeClient` struct.**

Find the `fakeClient` struct (around `knowledge_test.go:41-80`). It has fields like `createKBFunc`, `createDocFunc`, `getTaskFunc`. Add a new field grouped with the document funcs:

```go
type fakeClient struct {
	// ... existing fields ...
	createDocFunc func(tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error)
	// ... other existing fields ...
	retryDocumentFunc func(tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error)
	// ... rest of fields ...
}
```

(Inspect the actual struct layout first; insert `retryDocumentFunc` adjacent to `createDocFunc` for cohesion. Don't reorder unrelated fields.)

- [ ] **Step 2: Add the method below the existing `DeleteDocument` method.**

Find `func (f *fakeClient) DeleteDocument(...)` (around line 154). Append:

```go
func (f *fakeClient) RetryDocument(_ context.Context, tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
	if f.retryDocumentFunc != nil {
		return f.retryDocumentFunc(tenantID, kbID, docID)
	}
	return &contract.CreateDocumentResponse{}, nil
}
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean. The `var _ contract.Client = (*fakeClient)(nil)` assertion (if it exists; verify by grep) now passes.

- [ ] **Step 4: Do not commit yet.** Continues into Task A5.

---

### Task A5: Implement `RetryDocument` on `ragimpl.Impl` + unit test

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go` — append method after `MGetDocumentProgress`
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go` — append unit test

- [ ] **Step 1: Append the ragimpl method to `document.go`.**

Find the end of `MGetDocumentProgress` (around line 333). Append:

```go
// RetryDocument re-runs ingestion for a previously-failed coze document by
// resolving the coze doc id to its rag-side UUID + owning rag KB UUID, then
// passing through to the rag client. The mapping table's last_task_id is NOT
// updated here — R2-D-backend is intentionally pass-through; the caller (or
// a future R2-D-frontend) decides whether to record the new task_id.
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
```

- [ ] **Step 2: Append the unit test to `document_test.go`.**

Use Read to locate the file's last test function. Append:

```go
// TestRagimpl_RetryDocument verifies that ragimpl.RetryDocument resolves the
// coze doc id to its rag UUID and the owning KB's rag UUID via the mapping
// table, then forwards the call to the rag client.
func TestRagimpl_RetryDocument(t *testing.T) {
	var gotTenant, gotKBID, gotDocID string
	fc := &fakeClient{
		retryDocumentFunc: func(tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
			gotTenant, gotKBID, gotDocID = tenantID, kbID, docID
			return &contract.CreateDocumentResponse{
				DocID: docID, TaskID: "task-retry-9", Status: "pending",
			}, nil
		},
	}
	i := newTestImpl(t, fc)

	// Wire mapping rows: coze KB 100 → rag UUID "rag-kb-X"; coze doc 500 → rag UUID "rag-doc-Y" in KB 100.
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-old-1", 1700000000, 0))

	resp, err := i.RetryDocument(context.Background(), 500)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "rag-doc-Y", resp.DocID)
	require.Equal(t, "task-retry-9", resp.TaskID)
	require.Equal(t, "pending", resp.Status)

	// Mapping lookups should have routed through to the rag UUIDs:
	require.Equal(t, "test-tenant", gotTenant) // matches newTestImpl's env resolver
	require.Equal(t, "rag-kb-X", gotKBID)
	require.Equal(t, "rag-doc-Y", gotDocID)
}

// TestRagimpl_RetryDocument_MissingDocMapping verifies that a missing doc
// mapping row surfaces ErrMappingNotFound without calling the rag client.
func TestRagimpl_RetryDocument_MissingDocMapping(t *testing.T) {
	called := false
	fc := &fakeClient{
		retryDocumentFunc: func(_, _, _ string) (*contract.CreateDocumentResponse, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	_, err := i.RetryDocument(context.Background(), 999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client should NOT be called when mapping is missing")
}
```

(Verify the exact `InsertDoc` signature — Phase C of R2-B reordered args to `(ctx, cozeID, ragDocID, kbID, creatorID, lastTaskID, nowMs, size)`. The plan above matches that order: `..., "task-old-1", 1700000000, 0` = `lastTaskID, nowMs, size`. Read mapping.go to confirm before running.)

- [ ] **Step 3: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run "TestRagimpl_RetryDocument" -v`
Expected: both PASS.

Run the full package sweep: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task A6.

---

### Task A6: Commit Phase A

**Files:** (no edits; commit only)

- [ ] **Step 1: Inspect what changed.**

Run: `git status`. Confirm:
- Modified: `backend/infra/contract/rag/client.go` (interface signature)
- Modified: `backend/infra/rag/client.go` (Client.RetryDocument)
- Modified: `backend/infra/rag/client_test.go` (httptest)
- Modified: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` (fakeClient field + method)
- Modified: `backend/domain/knowledge/service/ragimpl/document.go` (Impl.RetryDocument)
- Modified: `backend/domain/knowledge/service/ragimpl/document_test.go` (two unit tests)

- [ ] **Step 2: Commit.**

```bash
git add backend/infra/contract/rag/client.go \
        backend/infra/rag/client.go \
        backend/infra/rag/client_test.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/document_test.go
git commit -m "$(cat <<'EOF'
feat(rag): wire RetryDocument endpoint

Adds POST /knowledgebases/{kb_id}/documents/{doc_id}/retry to the rag
client + contract.Client interface + fakeClient stub + ragimpl.Impl
method. Rag emits the standard UploadDocumentResponse envelope on retry,
identical in shape to CreateDocument, so the response type is reused.

ragimpl.RetryDocument is pass-through: it resolves coze→rag IDs via the
mapping table and forwards. last_task_id is NOT updated — R2-D-frontend
will decide whether to bump it when wiring the retry-button UX.

Service.Knowledge interface intentionally untouched (the new method
lives only on *ragimpl.Impl); the application-layer plumbing belongs to
R2-D-frontend.
EOF
)"
```

---

## Phase B — `GetCapabilities` (commit 2)

### Task B1: Add `KBCapabilities` DTO + interface method

**Files:**
- Modify: `backend/infra/contract/rag/types.go` — append `KBCapabilities` type after `KB`
- Modify: `backend/infra/contract/rag/client.go` — add interface method in KB group

- [ ] **Step 1: Append `KBCapabilities` to `types.go`.**

Use Read to find a logical insertion point. The struct describes a KB's capabilities; placing it after the `KB` type (around line 90-99 in the post-R2-B file) keeps related types adjacent.

```go
// KBCapabilities mirrors rag's KnowledgeBaseCapabilityDescriptor as of 0e1f49b.
// Returned by GET /api/v1/knowledgebases/{kb_id}/capabilities. Describes what
// the KB supports — chunk types, modalities, retrievers, search types — and
// what defaults it carries. Nullable numeric defaults are pointer-typed so JSON
// null distinguishes "no default set" from "default is zero."
//
// MetadataSchema and RetrieverDefaults are opaque map[string]any because rag's
// shape varies per provider and coze does not interpret these client-side.
type KBCapabilities struct {
	KBID                      string         `json:"kb_id"`
	EnabledChunkTypes         []string       `json:"enabled_chunk_types"`
	SupportedSourceModalities []string       `json:"supported_source_modalities"`
	EnabledRetrievers         []string       `json:"enabled_retrievers"`
	SupportedQueryModes       []string       `json:"supported_query_modes"`
	SupportedSearchTypes      []string       `json:"supported_search_types"`
	MetadataSchema            map[string]any `json:"metadata_schema,omitempty"`
	FilterableFields          []string       `json:"filterable_fields"`
	RetrievableFields         []string       `json:"retrievable_fields"`
	DefaultChunkSize          *int           `json:"default_chunk_size,omitempty"`
	DefaultChunkOverlap       *int           `json:"default_chunk_overlap,omitempty"`
	DefaultSearchType         *string        `json:"default_search_type,omitempty"`
	DefaultCandidateK         *int           `json:"default_candidate_k,omitempty"`
	DefaultTopK               *int           `json:"default_top_k,omitempty"`
	DefaultFusionPolicy       FusionPolicy   `json:"default_fusion_policy"`
	RetrieverDefaults         map[string]any `json:"retriever_defaults,omitempty"`
	SupportedQueryStrategies  []string       `json:"supported_query_strategies"`
	RequestOverrideableFields []string       `json:"request_overrideable_fields"`
}
```

`FusionPolicy` is the existing type (used by `CreateKBRequest`). No new type.

- [ ] **Step 2: Add interface method.**

In `backend/infra/contract/rag/client.go`, find the "Knowledge bases" group (around lines 37-42). Append the new method INSIDE that group, after `ListKBs`:

```go
	// Knowledge bases.
	CreateKB(ctx context.Context, tenantID string, req *CreateKBRequest) (*KB, error)
	GetKB(ctx context.Context, tenantID, kbID string) (*KB, error)
	UpdateKB(ctx context.Context, tenantID, kbID string, req *UpdateKBRequest) (*KB, error)
	DeleteKB(ctx context.Context, tenantID, kbID string) error
	ListKBs(ctx context.Context, req *ListKBsRequest) (*ListKBsResponse, error)
	// GetCapabilities fetches the KB's capability descriptor (enabled chunk
	// types, modalities, retrievers, defaults). Read-only.
	GetCapabilities(ctx context.Context, tenantID, kbID string) (*KBCapabilities, error)
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: fails at `*Client` and `fakeClient` (don't implement GetCapabilities yet). Fixed in B2 and B4.

- [ ] **Step 4: Do not commit yet.** Continues into Task B2.

---

### Task B2: Implement `GetCapabilities` on `*Client`

**Files:**
- Modify: `backend/infra/rag/client.go` — insert after `ListKBs` (around line 320)

- [ ] **Step 1: Append the method.**

Find `ListKBs`. Insert after it:

```go
// GetCapabilities fetches the rag-side capability descriptor for a KB. Used
// to drive wizard config and feature gating in the UI layer. Read-only; safe
// to retry on transient failures (doJSON handles GET retry idempotently).
func (c *Client) GetCapabilities(ctx context.Context, tenantID, kbID string) (*contract.KBCapabilities, error) {
	out := &contract.KBCapabilities{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/capabilities"
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 2: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./infra/rag/...`
Expected: clean.

- [ ] **Step 3: Do not commit yet.** Continues into Task B3.

---

### Task B3: httptest contract test for `GetCapabilities`

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append test after Phase A's

- [ ] **Step 1: Append the test.**

```go
// TestGetCapabilities_FieldShape locks rag's GET .../capabilities wire shape.
// Asserts every top-level scalar/slice/map field; covers both the "all
// defaults are null" path (pointer-nil) and the "defaults set" path
// (non-nil pointers with correct values).
func TestGetCapabilities_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/capabilities"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"kb_id":                       "kb-1",
				"enabled_chunk_types":         []string{"text_chunk", "image_chunk"},
				"supported_source_modalities": []string{"text_source", "image_source", "scanned_document_source"},
				"enabled_retrievers":          []string{"dense", "bm25"},
				"supported_query_modes":       []string{"text_input", "image_input"},
				"supported_search_types":      []string{"dense", "hybrid"},
				"metadata_schema":             map[string]any{},
				"filterable_fields":           []string{},
				"retrievable_fields":          []string{},
				"default_chunk_size":          512,
				"default_chunk_overlap":       64,
				"default_search_type":         "hybrid",
				"default_candidate_k":         100,
				"default_top_k":               10,
				"default_fusion_policy": map[string]any{
					"mode":    "weighted_rrf",
					"rrf_k":   60,
					"weights": map[string]float64{"text": 0.6, "image": 0.4},
				},
				"retriever_defaults":         map[string]any{},
				"supported_query_strategies": []string{"rewrite", "expansion"},
				"request_overrideable_fields": []string{
					"query_mode", "search_type", "top_k", "candidate_k", "filters",
					"target_chunk_types", "retrievers", "fusion_policy",
					"retriever_params", "query_strategy",
				},
			},
			"request_id": "req-cap-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetCapabilities(context.Background(), "t1", "kb-1")
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}

	if got.KBID != "kb-1" {
		t.Errorf("KBID = %q, want kb-1", got.KBID)
	}
	if len(got.EnabledChunkTypes) != 2 || got.EnabledChunkTypes[0] != "text_chunk" {
		t.Errorf("EnabledChunkTypes = %v", got.EnabledChunkTypes)
	}
	if len(got.SupportedSourceModalities) != 3 {
		t.Errorf("SupportedSourceModalities len = %d, want 3", len(got.SupportedSourceModalities))
	}
	if got.DefaultChunkSize == nil || *got.DefaultChunkSize != 512 {
		t.Errorf("DefaultChunkSize = %v, want *int(512)", got.DefaultChunkSize)
	}
	if got.DefaultChunkOverlap == nil || *got.DefaultChunkOverlap != 64 {
		t.Errorf("DefaultChunkOverlap = %v, want *int(64)", got.DefaultChunkOverlap)
	}
	if got.DefaultSearchType == nil || *got.DefaultSearchType != "hybrid" {
		t.Errorf("DefaultSearchType = %v, want *string(hybrid)", got.DefaultSearchType)
	}
	if got.DefaultFusionPolicy.Mode != "weighted_rrf" || got.DefaultFusionPolicy.RrfK != 60 {
		t.Errorf("DefaultFusionPolicy = %+v", got.DefaultFusionPolicy)
	}
	if len(got.SupportedQueryStrategies) != 2 {
		t.Errorf("SupportedQueryStrategies len = %d, want 2", len(got.SupportedQueryStrategies))
	}
	if len(got.RequestOverrideableFields) != 10 {
		t.Errorf("RequestOverrideableFields len = %d, want 10", len(got.RequestOverrideableFields))
	}
}

// TestGetCapabilities_NullableDefaults verifies that JSON null in default_*
// fields decodes to nil pointers, not zero-valued integers/strings.
func TestGetCapabilities_NullableDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"kb_id":                       "kb-empty",
				"enabled_chunk_types":         []string{"text_chunk"},
				"supported_source_modalities": []string{"text_source"},
				"enabled_retrievers":          []string{"dense"},
				"supported_query_modes":       []string{"text_input"},
				"supported_search_types":      []string{"dense"},
				"metadata_schema":             map[string]any{},
				"filterable_fields":           []string{},
				"retrievable_fields":          []string{},
				"default_chunk_size":          nil,
				"default_chunk_overlap":       nil,
				"default_search_type":         nil,
				"default_candidate_k":         nil,
				"default_top_k":               nil,
				"default_fusion_policy": map[string]any{
					"mode":  "weighted_rrf",
					"rrf_k": 60,
				},
				"retriever_defaults":          map[string]any{},
				"supported_query_strategies":  []string{},
				"request_overrideable_fields": []string{},
			},
			"request_id": "req-cap-2",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetCapabilities(context.Background(), "t1", "kb-empty")
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if got.DefaultChunkSize != nil {
		t.Errorf("DefaultChunkSize = %v, want nil", got.DefaultChunkSize)
	}
	if got.DefaultChunkOverlap != nil {
		t.Errorf("DefaultChunkOverlap = %v, want nil", got.DefaultChunkOverlap)
	}
	if got.DefaultSearchType != nil {
		t.Errorf("DefaultSearchType = %v, want nil", got.DefaultSearchType)
	}
	if got.DefaultCandidateK != nil {
		t.Errorf("DefaultCandidateK = %v, want nil", got.DefaultCandidateK)
	}
	if got.DefaultTopK != nil {
		t.Errorf("DefaultTopK = %v, want nil", got.DefaultTopK)
	}
}
```

- [ ] **Step 2: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/ -run TestGetCapabilities -v`
Expected: both PASS.

Full package sweep will still fail at ragimpl (fakeClient missing method). Fixed in B4.

- [ ] **Step 3: Do not commit yet.** Continues into Task B4.

---

### Task B4: Implement `GetCapabilities` on `fakeClient`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go`

- [ ] **Step 1: Add the stub field grouped with other KB funcs.**

Find the `fakeClient` struct. Locate the KB-related func fields (e.g., `createKBFunc`); insert the new field adjacent:

```go
	getCapabilitiesFunc func(tenantID, kbID string) (*contract.KBCapabilities, error)
```

- [ ] **Step 2: Add the method.**

Insert near the other KB methods (after `ListKBs`):

```go
func (f *fakeClient) GetCapabilities(_ context.Context, tenantID, kbID string) (*contract.KBCapabilities, error) {
	if f.getCapabilitiesFunc != nil {
		return f.getCapabilitiesFunc(tenantID, kbID)
	}
	return &contract.KBCapabilities{}, nil
}
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean.

- [ ] **Step 4: Do not commit yet.** Continues into Task B5.

---

### Task B5: Implement `GetCapabilities` on `ragimpl.Impl` + unit test

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge.go` — append method after `ListKnowledge`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` — append unit test

- [ ] **Step 1: Append the method to `knowledge.go`.**

Find the end of `ListKnowledge` (around line 273+). Append:

```go
// GetCapabilities fetches rag-side capabilities for a coze KB. Resolves the
// coze KB id to its rag UUID via the mapping table and passes through. The
// response is the rag-side typed shape; coze does not translate to an entity
// type — R2-D-frontend will introduce that translation when the UI's needs
// are concrete.
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
```

- [ ] **Step 2: Append the unit test to `knowledge_test.go`.**

Use Read to find the last test function. Append:

```go
// TestRagimpl_GetCapabilities verifies that ragimpl.GetCapabilities resolves
// coze KB id → rag UUID via mapping, then passes through.
func TestRagimpl_GetCapabilities(t *testing.T) {
	var gotTenant, gotKBID string
	fc := &fakeClient{
		getCapabilitiesFunc: func(tenantID, kbID string) (*contract.KBCapabilities, error) {
			gotTenant, gotKBID = tenantID, kbID
			return &contract.KBCapabilities{
				KBID:                "rag-kb-Z",
				EnabledChunkTypes:   []string{"text_chunk"},
				SupportedQueryModes: []string{"text_input"},
			}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 200, "rag-kb-Z", "icon", 0, 0, 1700000000))

	got, err := i.GetCapabilities(context.Background(), 200)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "rag-kb-Z", got.KBID)
	require.Equal(t, []string{"text_chunk"}, got.EnabledChunkTypes)

	require.Equal(t, "test-tenant", gotTenant)
	require.Equal(t, "rag-kb-Z", gotKBID)
}

// TestRagimpl_GetCapabilities_MissingMapping verifies that an unknown coze KB
// id surfaces ErrMappingNotFound without calling rag.
func TestRagimpl_GetCapabilities_MissingMapping(t *testing.T) {
	called := false
	fc := &fakeClient{
		getCapabilitiesFunc: func(_, _ string) (*contract.KBCapabilities, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	_, err := i.GetCapabilities(context.Background(), 999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client should NOT be called when mapping is missing")
}
```

- [ ] **Step 3: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run "TestRagimpl_GetCapabilities" -v`
Expected: both PASS.

Full package sweep: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task B6.

---

### Task B6: Commit Phase B

**Files:** (commit only)

- [ ] **Step 1: Commit.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/contract/rag/client.go \
        backend/infra/rag/client.go \
        backend/infra/rag/client_test.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go \
        backend/domain/knowledge/service/ragimpl/knowledge.go
git commit -m "$(cat <<'EOF'
feat(rag): wire GetCapabilities endpoint

Adds GET /knowledgebases/{kb_id}/capabilities to the rag client +
contract.Client interface + fakeClient stub + ragimpl.Impl method.
Returns rag's KnowledgeBaseCapabilityDescriptor — enabled chunk types,
supported modalities, retrievers, defaults, request_overrideable_fields.

New contract.KBCapabilities mirrors all top-level fields of rag's
response. Nullable numeric defaults (default_chunk_size, default_top_k,
etc.) are pointer-typed so JSON null distinguishes "no default" from
"default is zero." httptest covers both branches (defaults present +
defaults null).

ragimpl.GetCapabilities is pass-through: it resolves coze KB id to rag
UUID via mapping, then forwards. Service.Knowledge interface intentionally
untouched (deferred to R2-D-frontend).
EOF
)"
```

---

## Phase C — `ListDocumentParameterSchemas` (commit 3)

### Task C1: Add `DocumentParameterSchema` + `DocumentParameter` DTOs + interface method

**Files:**
- Modify: `backend/infra/contract/rag/types.go` — append both types at the end of the file
- Modify: `backend/infra/contract/rag/client.go` — add interface method in a new "Document parameter schemas" group

- [ ] **Step 1: Append both types to `types.go`.**

Place at the end of the file (or after `RetrieveHit` family — pick the bottom for a clean separation):

```go
// DocumentParameterSchema mirrors one entry in rag's response to
// GET /api/v1/document-parameter-schemas. Each schema scopes a typed
// parameter form to a set of file_types and source_modalities. The
// list is system-wide (not KB-scoped); the consumer is the upload
// wizard, which picks the schema matching the document being uploaded.
type DocumentParameterSchema struct {
	SchemaID         string              `json:"schema_id"`
	Description      string              `json:"description"`
	FileTypes        []string            `json:"file_types"`
	SourceModalities []string            `json:"source_modalities"`
	Parameters       []DocumentParameter `json:"parameters"`
}

// DocumentParameter describes a single tunable knob in a schema. Default
// and AllowedValues are `any` because their JSON type depends on the
// Type field (a boolean param's default is a bool, an integer's is a
// number, etc.); the consumer narrows at use time.
type DocumentParameter struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"` // boolean | integer | string | ...
	Group         string   `json:"group"`
	Required      bool     `json:"required"`
	Default       any      `json:"default,omitempty"`
	AllowedValues []any    `json:"allowed_values,omitempty"`
	MinValue      *float64 `json:"min_value,omitempty"`
	MaxValue      *float64 `json:"max_value,omitempty"`
	Description   string   `json:"description"`
	UILabel       string   `json:"ui_label"`
	UIComponent   string   `json:"ui_component"`
	Advanced      bool     `json:"advanced"`
	Internal      bool     `json:"internal"`
}
```

- [ ] **Step 2: Add interface method.**

In `backend/infra/contract/rag/client.go`, append a NEW group after `Retrieve` (which is currently the last in the interface):

```go
	// Retrieval.
	Retrieve(ctx context.Context, tenantID string, req *RetrieveRequest) (*RetrieveResponse, error)

	// Document parameter schemas — system-wide (no kb_id); the UI's source
	// of truth for upload-wizard parameter forms.
	ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]DocumentParameterSchema, error)
}
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: fails at `*Client` and `fakeClient`. Fixed in C2 and C4.

- [ ] **Step 4: Do not commit yet.** Continues into Task C2.

---

### Task C2: Implement `ListDocumentParameterSchemas` on `*Client`

**Files:**
- Modify: `backend/infra/rag/client.go` — append at the end (after `Retrieve`)

- [ ] **Step 1: Append the method.**

Find the last method in the file (`Retrieve`, around line 405-410). Append:

```go
// ListDocumentParameterSchemas returns rag's system-wide catalog of per-
// schema_id parameter forms. The rag endpoint is NOT KB-scoped (no kb_id
// in the path), but the tenant header still travels per rag's request-
// context invariant.
func (c *Client) ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]contract.DocumentParameterSchema, error) {
	var out []contract.DocumentParameterSchema
	path := apiPrefix + "/document-parameter-schemas"
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, &out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 2: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./infra/rag/...`
Expected: clean.

- [ ] **Step 3: Do not commit yet.** Continues into Task C3.

---

### Task C3: httptest contract test for `ListDocumentParameterSchemas`

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append test

- [ ] **Step 1: Append the test.**

```go
// TestListDocumentParameterSchemas_FieldShape locks rag's
// GET /document-parameter-schemas wire shape. The response is a list; each
// entry has nested Parameters. The test asserts both the outer envelope
// (no kb_id in path; tenant header still required) and the nested
// parameter shape across multiple parameter Type values (boolean, integer)
// to cover the `any`-typed Default and pointer-typed Min/Max fields.
func TestListDocumentParameterSchemas_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/document-parameter-schemas") {
			t.Errorf("path = %s, want suffix /api/v1/document-parameter-schemas", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": []map[string]any{
				{
					"schema_id":         "text_document",
					"description":       "Plain text paragraph processing parameters.",
					"file_types":        []string{"txt", "text"},
					"source_modalities": []string{"text_source"},
					"parameters": []map[string]any{
						{
							"name":           "merge_blank_line_paragraphs",
							"type":           "boolean",
							"group":          "text_paragraph",
							"required":       false,
							"default":        true,
							"allowed_values": []any{},
							"min_value":      nil,
							"max_value":      nil,
							"description":    "Merge paragraphs separated by blank lines when packing chunks.",
							"ui_label":       "Merge blank-line paragraphs",
							"ui_component":   "switch",
							"advanced":       false,
							"internal":       false,
						},
						{
							"name":           "chunk_size",
							"type":           "integer",
							"group":          "chunking",
							"required":       false,
							"default":        512,
							"allowed_values": []any{},
							"min_value":      64,
							"max_value":      8192,
							"description":    "Maximum chunk size for text chunking.",
							"ui_label":       "Maximum merged paragraph length",
							"ui_component":   "number",
							"advanced":       false,
							"internal":       false,
						},
					},
				},
				{
					"schema_id":         "image_document",
					"description":       "Image processing parameters.",
					"file_types":        []string{"jpg", "png"},
					"source_modalities": []string{"image_source"},
					"parameters":        []map[string]any{},
				},
			},
			"request_id": "req-ps-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.ListDocumentParameterSchemas(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ListDocumentParameterSchemas: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d schemas, want 2", len(got))
	}
	text := got[0]
	if text.SchemaID != "text_document" {
		t.Errorf("schemas[0].SchemaID = %q, want text_document", text.SchemaID)
	}
	if len(text.FileTypes) != 2 || text.FileTypes[0] != "txt" {
		t.Errorf("schemas[0].FileTypes = %v", text.FileTypes)
	}
	if len(text.Parameters) != 2 {
		t.Fatalf("schemas[0].Parameters len = %d, want 2", len(text.Parameters))
	}

	merge := text.Parameters[0]
	if merge.Name != "merge_blank_line_paragraphs" || merge.Type != "boolean" {
		t.Errorf("Parameters[0] name/type = %q/%q", merge.Name, merge.Type)
	}
	if merge.Default != true {
		t.Errorf("Parameters[0].Default = %v, want true (bool)", merge.Default)
	}
	if merge.MinValue != nil || merge.MaxValue != nil {
		t.Errorf("Parameters[0] min/max = %v/%v, want nil/nil for boolean", merge.MinValue, merge.MaxValue)
	}

	chunk := text.Parameters[1]
	if chunk.Name != "chunk_size" || chunk.Type != "integer" {
		t.Errorf("Parameters[1] name/type = %q/%q", chunk.Name, chunk.Type)
	}
	// JSON-decoded numbers arrive as float64 in `any`; coerce when asserting.
	if v, ok := chunk.Default.(float64); !ok || v != 512 {
		t.Errorf("Parameters[1].Default = %v (%T), want float64(512)", chunk.Default, chunk.Default)
	}
	if chunk.MinValue == nil || *chunk.MinValue != 64 {
		t.Errorf("Parameters[1].MinValue = %v, want *float64(64)", chunk.MinValue)
	}
	if chunk.MaxValue == nil || *chunk.MaxValue != 8192 {
		t.Errorf("Parameters[1].MaxValue = %v, want *float64(8192)", chunk.MaxValue)
	}

	image := got[1]
	if image.SchemaID != "image_document" {
		t.Errorf("schemas[1].SchemaID = %q", image.SchemaID)
	}
	if len(image.Parameters) != 0 {
		t.Errorf("schemas[1].Parameters should be empty, got %d", len(image.Parameters))
	}
}
```

- [ ] **Step 2: Run.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/ -run TestListDocumentParameterSchemas -v`
Expected: PASS.

- [ ] **Step 3: Do not commit yet.** Continues into Task C4.

---

### Task C4: Implement `ListDocumentParameterSchemas` on `fakeClient`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go`

- [ ] **Step 1: Add the stub field at the end of the fakeClient struct's func fields.**

```go
	listDocumentParameterSchemasFunc func(tenantID string) ([]contract.DocumentParameterSchema, error)
```

- [ ] **Step 2: Add the method at the end of the `func (f *fakeClient) ...` block (after `Retrieve`).**

```go
func (f *fakeClient) ListDocumentParameterSchemas(_ context.Context, tenantID string) ([]contract.DocumentParameterSchema, error) {
	if f.listDocumentParameterSchemasFunc != nil {
		return f.listDocumentParameterSchemasFunc(tenantID)
	}
	return nil, nil
}
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean.

- [ ] **Step 4: Do not commit yet.** Continues into Task C5.

---

### Task C5: Create `parameter_schemas.go` in ragimpl + unit test

**Files:**
- Create: `backend/domain/knowledge/service/ragimpl/parameter_schemas.go`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` — append unit test (knowledge_test.go is fine since the helper file is small and KB-scoped resolver lives in knowledge_test.go's package)

- [ ] **Step 1: Create the new file.**

```go
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ragimpl

import (
	"context"

	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// ListDocumentParameterSchemas returns rag's system-wide catalog of per-
// schema_id parameter forms. The rag endpoint is NOT KB-scoped, so this
// pass-through performs only a tenant resolver call before forwarding to
// the rag client.
//
// The response shape is the rag-side typed value; coze does not translate
// to an entity. R2-D-frontend will introduce the UI-side translation
// (or hide the indirection behind a service-layer DTO) when the wizard
// rework needs concrete data.
func (i *Impl) ListDocumentParameterSchemas(ctx context.Context) ([]contract.DocumentParameterSchema, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	return i.rag.ListDocumentParameterSchemas(ctx, tenant)
}
```

- [ ] **Step 2: Append unit test to `knowledge_test.go`.**

Use Read to find the last test. Append:

```go
// TestRagimpl_ListDocumentParameterSchemas verifies the pass-through
// behavior: tenant resolver runs, no mapping lookup happens (rag's
// endpoint is system-wide), and the rag client's return value
// propagates unchanged.
func TestRagimpl_ListDocumentParameterSchemas(t *testing.T) {
	var gotTenant string
	canned := []contract.DocumentParameterSchema{
		{
			SchemaID:    "text_document",
			Description: "Plain text",
			FileTypes:   []string{"txt"},
			Parameters: []contract.DocumentParameter{
				{Name: "chunk_size", Type: "integer", Default: 512.0},
			},
		},
	}
	fc := &fakeClient{
		listDocumentParameterSchemasFunc: func(tenantID string) ([]contract.DocumentParameterSchema, error) {
			gotTenant = tenantID
			return canned, nil
		},
	}
	i := newTestImpl(t, fc)

	got, err := i.ListDocumentParameterSchemas(context.Background())
	require.NoError(t, err)
	require.Equal(t, "test-tenant", gotTenant)
	require.Len(t, got, 1)
	require.Equal(t, "text_document", got[0].SchemaID)
	require.Len(t, got[0].Parameters, 1)
	require.Equal(t, "chunk_size", got[0].Parameters[0].Name)
}
```

- [ ] **Step 3: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run TestRagimpl_ListDocumentParameterSchemas -v`
Expected: PASS.

Full package sweep: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task C6.

---

### Task C6: Commit Phase C

**Files:** (commit only)

- [ ] **Step 1: Commit.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/contract/rag/client.go \
        backend/infra/rag/client.go \
        backend/infra/rag/client_test.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go \
        backend/domain/knowledge/service/ragimpl/parameter_schemas.go
git commit -m "$(cat <<'EOF'
feat(rag): wire ListDocumentParameterSchemas endpoint

Adds GET /document-parameter-schemas to the rag client + contract.Client
interface + fakeClient stub + ragimpl.Impl method. Returns the system-
wide catalog of per-schema_id parameter forms that drives the upload
wizard's parameter UI.

New contract types:
- DocumentParameterSchema (schema_id, file_types, source_modalities,
  parameters[])
- DocumentParameter (name, type, group, default, allowed_values,
  min_value, max_value, ui_label, ui_component, ...)

Default and AllowedValues are `any` because their JSON type depends on
the Type field. R2-D-frontend will narrow at consumption time.

ragimpl.ListDocumentParameterSchemas lives in a new parameter_schemas.go
file — the method is not KB-scoped, not document-scoped, and doesn't fit
existing files. The endpoint is system-wide; no mapping lookup, only
tenant resolver before pass-through to the rag client.

Service.Knowledge interface intentionally untouched.
EOF
)"
```

---

## Phase D — Verification

### Task D1: Full backend test sweep + go vet

- [ ] **Step 1: Test sweep.**

```bash
cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go test \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: PASS for all packages.

- [ ] **Step 2: Full backend build.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean.

- [ ] **Step 3: Vet.**

```bash
cd backend && GOTOOLCHAIN=go1.24.0 go vet \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: clean.

- [ ] **Step 4: If anything fails, fix and commit as a chore.** Do not amend a published commit.

---

### Task D2: Live wire-shape probe (optional, exploratory)

R2-D-backend has no UI consumer, so no end-to-end smoke. But hitting rag directly with `curl` verifies the wire shapes coze's tests assume are still real.

If rag is up:

```bash
# Capabilities
KB_ID=$(docker exec coze-mysql mysql -uroot -proot opencoze -sN -e "SELECT rag_kb_id FROM rag_kb_mapping LIMIT 1;")
curl -s -H "X-Tenant-Id: coze" http://localhost:8000/api/v1/knowledgebases/$KB_ID/capabilities | python3 -m json.tool | head -30

# Parameter schemas
curl -s -H "X-Tenant-Id: coze" http://localhost:8000/api/v1/document-parameter-schemas | python3 -m json.tool | head -50
```

Expected: shape matches spec §3.1 (top-level keys, nested structure). If a field name has drifted since spec-writing, that's a new R2-* delta — file and re-spec rather than patching this slice.

Retry endpoint needs a failed doc to exercise; not worth setting up. Unit + httptest coverage stands in.

- [ ] **Step 1: No commit.** Probe only.

---

## Out of scope (do not address in this plan)

- `service.Knowledge` interface methods (deferred to R2-D-frontend).
- `application/knowledge` HTTP handlers, IDL definitions, thrift codegen (R2-D-frontend).
- Frontend wizard rework, retry button enable, anything UI-facing (R2-D-frontend).
- Coze-side entity types for capabilities / parameter-schemas (R2-D-frontend).
- Caching of capabilities or parameter-schemas responses (R2-D-frontend's call).
- Mapping mutation on RetryDocument (`last_task_id` bump) — pre-flight resolution defers to R2-D-frontend.
- Bucket-B stub UI hiding (queued item #12).
- R2-E's broader httptest scaffolding for endpoints outside R2-A through R2-D.

---

## Self-review checklist (filled in)

1. **Spec coverage:**
   - §3.2 KBCapabilities DTO → Task B1
   - §3.2 DocumentParameterSchema + DocumentParameter DTOs → Task C1
   - §3.2 Retry response reuse of CreateDocumentResponse → Task A2 (no new type)
   - §4 architecture flows → mirrored in each phase's task ordering
   - §4.5 file table → Task layout (Tasks A1-A6 = Retry files, B1-B6 = Capabilities files, C1-C6 = ParameterSchemas files including new parameter_schemas.go)
   - §5.1 client method signatures → Tasks A2, B2, C2 (exact code)
   - §5.2 ragimpl method signatures → Tasks A5, B5, C5 (exact code)
   - §5.3 interface additions → Tasks A1, B1, C1 (one per phase)
   - §8.1 httptest contract tests → Tasks A3, B3, C3 (covering pointer-nil branch for capabilities; nested parameter shape for schemas)
   - §8.2 ragimpl unit tests → Tasks A5, B5, C5 (each includes a missing-mapping negative case)
   - §8.3 fakeClient updates → Tasks A4, B4, C4
   - §10 open question 1 (last_task_id) → resolved in pre-flight (NO update)
   - §10 open question 2 (parameter_schemas.go file placement) → resolved in pre-flight (new file)

2. **Placeholder scan:** none. Task A5 Step 2 has a defensive note ("Verify the exact `InsertDoc` signature") because Phase C of R2-B's arg reorder is recent and the plan should not silently assume; this is a one-line sanity check, not a TBD.

3. **Type consistency:**
   - `contract.KBCapabilities` (B1) ↔ `Client.GetCapabilities` return (B2) ↔ `*fakeClient.GetCapabilities` return (B4) ↔ `ragimpl.GetCapabilities` return (B5): all `*contract.KBCapabilities`. Consistent.
   - `contract.DocumentParameterSchema` slice (C1) ↔ `Client.ListDocumentParameterSchemas` return (C2) ↔ `*fakeClient` return (C4) ↔ `ragimpl` return (C5): all `[]contract.DocumentParameterSchema`. Consistent.
   - Retry path reuses `contract.CreateDocumentResponse` everywhere (A1-A5). Consistent.
   - `InsertDoc(ctx, cozeID, ragDocID, kbID, creatorID, lastTaskID, nowMs, size)` argument order used in Task A5's test matches the post-R2-B-Phase-C signature.
