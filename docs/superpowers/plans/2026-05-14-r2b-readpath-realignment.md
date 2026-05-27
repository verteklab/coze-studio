# R2-B: Task / Document read-path realignment + size persistence — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-14-r2b-readpath-realignment-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation, base `78702f6a`)
**Goal:** Make coze's read path (GetTask, GetDocument, ListDocuments) match rag's `0e1f49b` response shapes; derive coarse progress from task status; persist file size on the coze side so the KB UI shows non-empty document metadata.

**Architecture:** Three thematic commits.
1. **Task contract + progress**: rewrite `contract.Task`, add a `progressForStatus` helper, update `MGetDocumentProgress` to use it and the renamed `ErrorMsg` field, lock the wire shape with an httptest.
2. **Document contract**: rewrite `contract.Document` (rename `Name → Filename`, add `FileType`/`ChunkCount`/`ErrorMsg`/`SourceModality`, drop `KBID`), update `MGetDocument` and `ListDocument`, lock the wire shape with two httptests.
3. **Size persistence**: Atlas migration adds `size` column to `rag_doc_mapping`, `DocMapping` struct + `InsertDoc` signature extend, `CreateDocument` writes the value, `MGetDocument`/`ListDocument` populate `entity.Document.Size`.

**Tech Stack:** Go 1.24 (pinned via `GOTOOLCHAIN`), `mime/multipart`, `net/http/httptest`, Atlas HCL + `make atlas-hash`, gorm.io/gorm.

---

## Pre-flight: facts the plan depends on

These were resolved during plan-writing; capture them here so executing tasks don't need to re-discover them.

- `backend/infra/contract/rag/types.go` after R2-A:
  - `Task` struct lives at lines 149-156 with fields `TaskID, DocID, Status, Progress, Error, UpdatedAt`. To be REPLACED in Phase A.
  - `Document` struct lives at lines 135-142 with fields `DocID, KBID, Name, Status, CreatedAt, UpdatedAt`. To be REPLACED in Phase B.
- `backend/domain/knowledge/service/ragimpl/document.go`:
  - `CreateDocument` per-doc loop at lines 86-167 (R2-A version). Phase C inserts a `len(fileBytes)` argument to `mapping.InsertDoc` at line 150.
  - `ListDocument` at lines 200-252. Phase B updates line 239 (`Name: rd.Filename`) and Phase C adds `Size`/`FileExtension` population.
  - `MGetDocument` at lines 254-293. Phase B updates line 283 (`Name: rd.Filename`) and Phase C adds `Size`/`FileExtension` population.
  - `MGetDocumentProgress` at lines 295-333. Phase A updates lines 327-329 (status → coarse progress, ErrorMsg → StatusMsg).
- `backend/domain/knowledge/service/ragimpl/mapping.go`:
  - `DocMapping` struct at lines 42-48 — Phase C adds `Size int64`.
  - `DocByCozeID` lines 186-209, `DocsByCozeIDs` lines 211-238, `docByRagID` lines 241-264 — Phase C extends each to SELECT `size` and populate the field.
  - `InsertDoc` lines 282-289 — Phase C adds `size int64` between `lastTaskID` and `nowMs`.
- `InsertDoc` callers (4 sites — all need updating in Phase C):
  - Production: `document.go:150`.
  - Tests: `mapping_test.go:212`, `document_test.go:84`, `retrieval_test.go:65`.
- `backend/domain/knowledge/service/ragimpl/knowledge_test.go::fakeClient`:
  - Method-level stubs at lines 138 (GetDocument), 146 (ListDocuments), 162 (GetTask). Their signatures don't change (they return `*contract.Task` / `*contract.Document` / `*contract.ListDocumentsResponse`). Test bodies that construct return values with the old field names need updating.
- `parser.FileExtension` is a `string` type alias (`backend/infra/document/parser/manager.go:86`). Cast `parser.FileExtension(rd.FileType)` is a no-op string conversion.
- Atlas mechanics: `docker/atlas/opencoze_latest_schema.hcl` is the source of truth; `make atlas-hash` regenerates the migration ledger hash. Schema file location for `rag_doc_mapping`: line 1877.
- Rag's authoritative wire shape (verified live 2026-05-14):
  - GetTask: `{task_id, type, status, retry_count, error_msg, created_at, started_at, finished_at}`. `started_at` / `finished_at` are `null` before transition.
  - GetDocument: `{doc_id, filename, file_type, status, chunk_count, error_msg, source_modality, created_at, updated_at, delete_cleanup_errors, processing_config, processing_summary}`. Coze decodes top-level scalars only.

---

## Phase A — Task contract + progress (commit 1)

### Task A1: Rewrite `Task` in `contract/rag/types.go`

**Files:**
- Modify: `backend/infra/contract/rag/types.go:149-156` (`Task` struct)

- [ ] **Step 1: Replace the `Task` struct definition.**

Find:

```go
type Task struct {
	TaskID    string    `json:"task_id"`
	DocID     string    `json:"doc_id"`
	Status    string    `json:"status"` // pending | running | retrying | success | failed
	Progress  int       `json:"progress"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt RagTime `json:"updated_at"`
}
```

Replace with:

```go
// Task mirrors rag's TaskDetail as of 0e1f49b. The wire shape changed in the
// 2026-05-14 round-2 audit: DocID and Progress were dropped; Error was renamed
// to ErrorMsg; UpdatedAt became FinishedAt; CreatedAt/StartedAt/Type/RetryCount
// are new. Pre-transition phases emit JSON null for StartedAt/FinishedAt, which
// is why they're pointer-typed — a value receiver would decode null into the
// unix epoch, masking the unset state.
type Task struct {
	TaskID     string   `json:"task_id"`
	Type       string   `json:"type"` // "ingestion" today; future types may exist
	Status     string   `json:"status"` // pending | running | retrying | success | failed
	RetryCount int      `json:"retry_count"`
	ErrorMsg   string   `json:"error_msg,omitempty"`
	CreatedAt  RagTime  `json:"created_at"`
	StartedAt  *RagTime `json:"started_at,omitempty"`
	FinishedAt *RagTime `json:"finished_at,omitempty"`
}
```

- [ ] **Step 2: Compile-check (will fail intentionally).**

Run: `cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: build errors in `document.go::MGetDocumentProgress` at lines that reference `task.Progress` and `task.Error`. Fixed in Tasks A3. Do NOT patch them yet.

- [ ] **Step 3: Do not commit yet.** Phase A commits at the end of Task A5.

---

### Task A2: Write the failing GetTask httptest

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append new test functions

- [ ] **Step 1: Append `TestGetTask_FieldShape` to `client_test.go`.**

Place after the existing `TestCreateDocument_Multipart_ErrorEnvelope` block (use Read to locate the last test in the file first). Note: `mime` and `mime/multipart` are already imported from R2-A; you only need `time` if not already present.

```go
// TestGetTask_FieldShape locks rag's GetTask wire shape after the 2026-05-14
// round-2 audit. The handler returns the full new envelope; the test asserts
// every coze-side field decodes correctly, including null-handling for
// StartedAt/FinishedAt.
func TestGetTask_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/tasks/task-abc") {
			t.Errorf("path = %s, want suffix /api/v1/tasks/task-abc", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-abc",
				"type":        "ingestion",
				"status":      "success",
				"retry_count": 2,
				"error_msg":   "transient embed error retried",
				"created_at":  "2026-05-14T13:25:57.009000",
				"started_at":  "2026-05-14T13:26:00.055000",
				"finished_at": "2026-05-14T13:26:04.484000",
			},
			"request_id": "req-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})
	got, err := c.GetTask(context.Background(), "t1", "task-abc")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskID != "task-abc" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "task-abc")
	}
	if got.Type != "ingestion" {
		t.Errorf("Type = %q, want ingestion", got.Type)
	}
	if got.Status != "success" {
		t.Errorf("Status = %q, want success", got.Status)
	}
	if got.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", got.RetryCount)
	}
	if got.ErrorMsg != "transient embed error retried" {
		t.Errorf("ErrorMsg = %q", got.ErrorMsg)
	}
	if got.StartedAt == nil {
		t.Errorf("StartedAt = nil, want non-nil")
	}
	if got.FinishedAt == nil {
		t.Errorf("FinishedAt = nil, want non-nil")
	}
}

// TestGetTask_NullableTimestamps verifies that JSON null for started_at /
// finished_at decodes to a nil pointer rather than zero-time.
func TestGetTask_NullableTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-pending",
				"type":        "ingestion",
				"status":      "pending",
				"retry_count": 0,
				"error_msg":   nil,
				"created_at":  "2026-05-14T13:25:57.009000",
				"started_at":  nil,
				"finished_at": nil,
			},
			"request_id": "req-2",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetTask(context.Background(), "t1", "task-pending")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil", got.StartedAt)
	}
	if got.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil", got.FinishedAt)
	}
	if got.ErrorMsg != "" {
		t.Errorf("ErrorMsg = %q, want empty (null)", got.ErrorMsg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/... -run TestGetTask -v`
Expected: PASS — the contract struct already matches because Task A1 landed. The test exists to LOCK the shape against future drift, not to TDD-drive a change. (If Task A1's struct were wrong, these tests would catch it.)

If the tests FAIL, the most likely cause is a typo in Task A1's struct field types. Fix Task A1 and re-run.

- [ ] **Step 3: Do not commit yet.** Phase A commits at the end of Task A5.

---

### Task A3: Add `progressForStatus` helper + update `MGetDocumentProgress`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go` (append helper)
- Modify: `backend/domain/knowledge/service/ragimpl/document.go:327-329` (use new fields + helper)

- [ ] **Step 1: Append `progressForStatus` to `factory.go`.**

Append after the existing `RagStatusToEntity` function (use Read to confirm the file's current last line). Place it next to its sibling status-mapper.

```go
// progressForStatus maps rag's task status string to a coarse 0-100 progress
// value for UI display. Rag dropped its numeric progress field in 0e1f49b; this
// is the best approximation until /capabilities exposes per-phase progress
// (planned for R2-D).
//
// Pending shows a small non-zero so the UI's progress bar isn't visually
// indistinguishable from "no doc yet." Failed maps to 0 because a failed bar
// at 100% would be misleading; the failure state is communicated separately
// via dp.Status + dp.StatusMsg.
func progressForStatus(s string) int {
	switch s {
	case "pending":
		return 10
	case "running", "retrying":
		return 50
	case "success":
		return 100
	case "failed":
		return 0
	default:
		return 0
	}
}
```

- [ ] **Step 2: Update `MGetDocumentProgress`.**

Find at `document.go:327-329`:

```go
		dp.Status = taskStatusToDoc(task.Status)
		dp.Progress = task.Progress
		dp.StatusMsg = task.Error
```

Replace with:

```go
		dp.Status = taskStatusToDoc(task.Status)
		dp.Progress = progressForStatus(task.Status)
		dp.StatusMsg = task.ErrorMsg
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: package compiles. The package-level test target may still fail because `fakeClient`'s in-test return values reference the old field names. Fixed in Task A4.

- [ ] **Step 4: Do not commit yet.** Continues into Task A4.

---

### Task A4: Update existing tests that construct `Task` values

**Files:**
- Modify: any `*_test.go` in `backend/domain/knowledge/service/ragimpl/` that constructs `contract.Task{...}` with `Error:` or `UpdatedAt:` or that references `task.Progress`.

- [ ] **Step 1: Discover affected tests.**

Run from repo root:

```bash
grep -rn "contract\.Task{\|task\.Progress\|task\.Error\b\|task\.UpdatedAt" backend/domain/knowledge/service/ragimpl/ | grep -v "\.go:.*//"
```

Note each hit. The likely set includes `document_test.go` (which tests `MGetDocumentProgress` returning specific status/progress values) and possibly `knowledge_test.go::fakeClient` setup.

- [ ] **Step 2: For each hit, rewrite the literal.**

For literals like:

```go
&contract.Task{TaskID: "...", Status: "running", Progress: 30, Error: "..."}
```

Change to:

```go
&contract.Task{TaskID: "...", Status: "running", ErrorMsg: "..."}
```

Drop the `Progress` field — it no longer exists. Drop `UpdatedAt`, replace with `FinishedAt: &someRagTime` if the test asserts on a timestamp.

For test assertions on `dp.Progress`, update the expected value to match the new coarse mapping (`pending` → 10, `running`/`retrying` → 50, `success` → 100, `failed` → 0). If the test originally asserted `assert.Equal(t, 30, dp.Progress)`, change the expected to `50` (the running-status coarse value).

- [ ] **Step 3: Run the affected test packages.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task A5.

---

### Task A5: Commit Phase A

**Files:** (no edits; commit only)

- [ ] **Step 1: Verify the four affected packages are green.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 2: Commit.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/rag/client_test.go \
        backend/domain/knowledge/service/ragimpl/factory.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/document_test.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go
# Note: only include _test.go files that actually changed in Task A4.
# git diff --name-only HEAD before staging if uncertain.
git commit -m "$(cat <<'EOF'
refactor(rag): realign Task contract + coarse progress derivation

Rag's GetTask wire shape changed in 0e1f49b: DocID and Progress were
dropped, Error renamed to ErrorMsg, UpdatedAt replaced by FinishedAt,
and Type / RetryCount / CreatedAt / StartedAt are new. coze decoded
only TaskID and Status before this commit; the rest were silently
zero-valued.

Rewrites contract.Task to match, adds a progressForStatus helper that
derives a coarse 0-100 value from task.Status (pending=10, running=50,
success=100, failed=0), and updates MGetDocumentProgress to use it.
httptest contract tests (happy path + nullable timestamps) lock the
wire shape going forward.

Frontend unchanged: <UploadProgressPoll /> continues to read the same
service-layer DocumentProgress DTO; only the source of values changes.
EOF
)"
```

---

## Phase B — Document contract realignment (commit 2)

### Task B1: Rewrite `Document` in `contract/rag/types.go`

**Files:**
- Modify: `backend/infra/contract/rag/types.go:135-142` (`Document` struct)

- [ ] **Step 1: Replace the `Document` struct definition.**

Find:

```go
type Document struct {
	DocID     string    `json:"doc_id"`
	KBID      string    `json:"kb_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt RagTime `json:"created_at"`
	UpdatedAt RagTime `json:"updated_at"`
}
```

Replace with:

```go
// Document mirrors rag's DocumentDetail as of 0e1f49b. The wire shape changed
// in the 2026-05-14 round-2 audit: KBID was dropped (the kb_id lives in the
// URL path), Name was renamed to Filename, and FileType / ChunkCount /
// ErrorMsg / SourceModality are new. Rag also emits delete_cleanup_errors,
// processing_config, processing_summary at the top level — coze ignores those
// here; adding fields means adding contract surface we have to maintain.
type Document struct {
	DocID          string  `json:"doc_id"`
	Filename       string  `json:"filename"`
	FileType       string  `json:"file_type"`
	Status         string  `json:"status"`
	ChunkCount     int     `json:"chunk_count"`
	ErrorMsg       string  `json:"error_msg,omitempty"`
	SourceModality string  `json:"source_modality"`
	CreatedAt      RagTime `json:"created_at"`
	UpdatedAt      RagTime `json:"updated_at"`
}
```

- [ ] **Step 2: Compile-check (will fail intentionally).**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: build errors in `document.go::ListDocument` at `rd.Name` and `document.go::MGetDocument` at `rd.Name`. Fixed in Task B3.

- [ ] **Step 3: Do not commit yet.** Phase B commits at the end of Task B5.

---

### Task B2: Write the failing GetDocument + ListDocuments httptest

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append new tests

- [ ] **Step 1: Append `TestGetDocument_FieldShape` and `TestListDocuments_FieldShape` to `client_test.go`.**

Place after the GetTask tests from Phase A.

```go
// TestGetDocument_FieldShape locks rag's GetDocument wire shape after the
// 2026-05-14 round-2 audit. Asserts every renamed and new field decodes.
// Explicitly does NOT assert anything about Name, KBID, or Size — those
// fields are not on rag's response.
func TestGetDocument_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/documents/doc-1"
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
				"doc_id":          "doc-1",
				"filename":        "7.测试计划.docx",
				"file_type":       "docx",
				"status":          "ready",
				"chunk_count":     80,
				"error_msg":       nil,
				"source_modality": "text_source",
				"created_at":      "2026-05-14T13:25:57.009000",
				"updated_at":      "2026-05-14T13:26:04.484000",
			},
			"request_id": "req-3",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetDocument(context.Background(), "t1", "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.DocID != "doc-1" {
		t.Errorf("DocID = %q, want doc-1", got.DocID)
	}
	if got.Filename != "7.测试计划.docx" {
		t.Errorf("Filename = %q", got.Filename)
	}
	if got.FileType != "docx" {
		t.Errorf("FileType = %q, want docx", got.FileType)
	}
	if got.Status != "ready" {
		t.Errorf("Status = %q, want ready", got.Status)
	}
	if got.ChunkCount != 80 {
		t.Errorf("ChunkCount = %d, want 80", got.ChunkCount)
	}
	if got.ErrorMsg != "" {
		t.Errorf("ErrorMsg = %q, want empty (null)", got.ErrorMsg)
	}
	if got.SourceModality != "text_source" {
		t.Errorf("SourceModality = %q", got.SourceModality)
	}
}

// TestListDocuments_FieldShape locks the list endpoint's envelope shape.
// Returns one item with the same fields as the singleton case to share
// assertion logic; the meaningful new bit is the {items, total} wrapper.
func TestListDocuments_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"items": []map[string]any{
					{
						"doc_id":          "doc-1",
						"filename":        "a.txt",
						"file_type":       "txt",
						"status":          "ready",
						"chunk_count":     3,
						"error_msg":       nil,
						"source_modality": "text_source",
						"created_at":      "2026-05-14T13:25:57.009000",
						"updated_at":      "2026-05-14T13:26:04.484000",
					},
				},
				"total": 1,
			},
			"request_id": "req-4",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.ListDocuments(context.Background(), "t1", "kb-1", 1, 50)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("Total = %d, want 1", got.Total)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	item := got.Items[0]
	if item.Filename != "a.txt" {
		t.Errorf("Items[0].Filename = %q, want a.txt", item.Filename)
	}
	if item.FileType != "txt" {
		t.Errorf("Items[0].FileType = %q", item.FileType)
	}
	if item.ChunkCount != 3 {
		t.Errorf("Items[0].ChunkCount = %d", item.ChunkCount)
	}
}
```

- [ ] **Step 2: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/... -run "TestGetDocument|TestListDocuments" -v`
Expected: tests in `client_test.go` compile and run. They should pass once Task B1 lands (which already happened). If they fail, the most likely cause is a typo in Task B1's struct.

- [ ] **Step 3: Do not commit yet.** Continues into Task B3.

---

### Task B3: Update `MGetDocument` and `ListDocument` to use new field names

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go:236-246` (ListDocument loop body)
- Modify: `backend/domain/knowledge/service/ragimpl/document.go:280-290` (MGetDocument loop body)

- [ ] **Step 1: Update `ListDocument`.**

Find at `document.go:236-246`:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          dm.CozeID,
				Name:        rd.Name,
				CreatorID:   dm.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID: dm.KBID,
			Status:      RagStatusToEntity(rd.Status),
		})
```

Replace with:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          dm.CozeID,
				Name:        rd.Filename,
				CreatorID:   dm.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID:   dm.KBID,
			Status:        RagStatusToEntity(rd.Status),
			FileExtension: parser.FileExtension(rd.FileType),
		})
```

- [ ] **Step 2: Update `MGetDocument`.**

Find at `document.go:280-290`:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          m.CozeID,
				Name:        rd.Name,
				CreatorID:   m.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID: m.KBID,
			Status:      RagStatusToEntity(rd.Status),
		})
```

Replace with:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          m.CozeID,
				Name:        rd.Filename,
				CreatorID:   m.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID:   m.KBID,
			Status:        RagStatusToEntity(rd.Status),
			FileExtension: parser.FileExtension(rd.FileType),
		})
```

- [ ] **Step 3: Add the `parser` import if not already present.**

The package `github.com/coze-dev/coze-studio/backend/infra/document/parser` may or may not already be imported in `document.go`. Check `document.go`'s import block — if absent, add it grouped with the other internal imports.

- [ ] **Step 4: Build the package.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: clean build.

- [ ] **Step 5: Do not commit yet.** Continues into Task B4.

---

### Task B4: Update existing tests that construct `Document` values

**Files:**
- Modify: any `*_test.go` in `backend/domain/knowledge/service/ragimpl/` that constructs `contract.Document{...}` with `Name:` or `KBID:`.

- [ ] **Step 1: Discover affected tests.**

```bash
grep -rn "contract\.Document{" backend/domain/knowledge/service/ragimpl/ | grep -v "\.go:.*//"
```

Likely hits include `document_test.go` and possibly `retrieval_test.go`.

- [ ] **Step 2: For each literal, rename `Name:` → `Filename:` and drop `KBID:`.**

If the test asserts on `entity.Document.Name`, the assertion still works because the entity field is still `Name`. The rename is only on the rag-side `contract.Document`.

- [ ] **Step 3: Run the affected test packages.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task B5.

---

### Task B5: Commit Phase B

**Files:** (no edits; commit only)

- [ ] **Step 1: Verify all four affected packages green.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 2: Commit.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/rag/client_test.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/document_test.go
# Add other _test.go files only if they actually changed in B4.
git commit -m "$(cat <<'EOF'
refactor(rag): realign Document contract; populate Filename + FileType

Rag's GetDocument / ListDocuments wire shape changed in 0e1f49b: KBID
was dropped (kb_id lives in the URL path), Name renamed to Filename,
and FileType / ChunkCount / ErrorMsg / SourceModality are new fields.
The 2026-05-14 smoke confirmed coze was rendering empty document names
on the KB detail page because it decoded the old field name.

Rewrites contract.Document to match, updates MGetDocument and
ListDocument to populate entity.Document.Name from rd.Filename and
entity.Document.FileExtension from rd.FileType. Two httptest contract
tests (singleton + list envelope) lock the wire shape going forward.

Size persistence (so the UI's all_file_size column stops showing 0)
follows in the next commit.
EOF
)"
```

---

## Phase C — Size persistence (commit 3)

### Task C1: Add `size` column to atlas HCL

**Files:**
- Modify: `docker/atlas/opencoze_latest_schema.hcl` — the `table "rag_doc_mapping"` block (currently lines 1877-1928)

- [ ] **Step 1: Insert the `size` column definition.**

Find the existing `created_at` column block inside `table "rag_doc_mapping"`:

```hcl
  column "created_at" {
    null     = false
    type     = bigint
    default  = 0
    unsigned = true
    comment  = "Create Time in Milliseconds"
  }
```

Insert a new column block immediately BEFORE it:

```hcl
  column "size" {
    null     = false
    type     = bigint
    default  = 0
    unsigned = true
    comment  = "Document file size in bytes; coze-side, since rag does not return size on its Document response."
  }
```

- [ ] **Step 2: Regenerate the migration hash.**

Run: `cd /Users/liuxinyu/workspace/coze-studio && make atlas-hash`
Expected: the atlas command emits an updated `atlas.sum` or migration ledger file under `docker/atlas/`. Inspect `git status` to confirm what changed:

```bash
git status docker/atlas/
```

If a new migration file appeared (e.g. `docker/atlas/migrations/YYYYMMDDHHMMSS_*.sql`), inspect its contents to confirm it ADDS a column and does nothing else. If `make atlas-hash` reports drift but produces no migration file, the project may use a `dump_db` / autogen pattern — check `Makefile` for the target the team actually uses to add a column. (The R2-A spec didn't touch schema, so this is the first time this plan exercises the path.)

- [ ] **Step 3: Apply the migration locally.**

The atlas command itself only updates the HCL ledger. To actually create the column in your dev DB, the existing `make sync_db` target runs the migration:

```bash
cd /Users/liuxinyu/workspace/coze-studio && make sync_db
```

Verify the column exists:

```bash
docker exec coze-mysql mysql -uroot -proot opencoze -e "DESC rag_doc_mapping;"
```

Expected: the `size` column appears with type `bigint(20) unsigned NOT NULL DEFAULT 0`.

- [ ] **Step 4: Do not commit yet.** Phase C commits at the end of Task C5.

---

### Task C2: Add `Size` to `DocMapping` + extend `InsertDoc` + read functions

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/mapping.go`

- [ ] **Step 1: Extend `DocMapping` struct.**

Find at `mapping.go:42-48`:

```go
type DocMapping struct {
	CozeID     int64
	RagDocID   string
	KBID       int64
	CreatorID  int64
	LastTaskID string
}
```

Replace with:

```go
type DocMapping struct {
	CozeID     int64
	RagDocID   string
	KBID       int64
	CreatorID  int64
	LastTaskID string
	Size       int64 // file size in bytes; populated at upload, read on display
}
```

- [ ] **Step 2: Extend `DocByCozeID`.**

Find at `mapping.go:186-209`. Update the inline `row` struct to add `Size`, extend the `Select(...)`, and populate the returned `DocMapping`:

```go
func (m *MappingRepo) DocByCozeID(ctx context.Context, cozeID int64) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("coze_doc_id = ? AND (deleted_at IS NULL)", cozeID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: doc id=%d", ErrMappingNotFound, cozeID)
		}
		return nil, err
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
	}, nil
}
```

- [ ] **Step 3: Extend `DocsByCozeIDs`.**

Find at `mapping.go:211-238`. Same shape of change:

```go
func (m *MappingRepo) DocsByCozeIDs(ctx context.Context, ids []int64) ([]*DocMapping, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("coze_doc_id IN ? AND (deleted_at IS NULL)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*DocMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &DocMapping{
			CozeID: r.CozeDocID, RagDocID: r.RagDocID, KBID: r.CozeKBID,
			CreatorID: r.CreatorID, LastTaskID: r.LastTaskID, Size: r.Size,
		})
	}
	return out, nil
}
```

- [ ] **Step 4: Extend `docByRagID`.**

Find at `mapping.go:241-264`. Same shape:

```go
func (m *MappingRepo) docByRagID(ctx context.Context, ragDocID string) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("rag_doc_id = ? AND (deleted_at IS NULL)", ragDocID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: rag_doc_id=%s", ErrMappingNotFound, ragDocID)
		}
		return nil, err
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
	}, nil
}
```

- [ ] **Step 5: Extend `InsertDoc` signature.**

Find at `mapping.go:282-289`:

```go
func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, nowMs int64) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_doc_mapping
		 (coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cozeID, ragDocID, kbID, creatorID, lastTaskID, nowMs,
	).Error
}
```

Replace with:

```go
func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, size int64, nowMs int64) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_doc_mapping
		 (coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cozeID, ragDocID, kbID, creatorID, lastTaskID, size, nowMs,
	).Error
}
```

The `size` parameter slots between `lastTaskID` and `nowMs` to keep "string keys then numeric audit metadata" grouped.

- [ ] **Step 6: Build the package alone (expect call-site failures).**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: build errors at all four `InsertDoc` callers (production + tests) — wrong number of arguments. Fixed in Tasks C3 and C4.

- [ ] **Step 7: Do not commit yet.** Continues into Task C3.

---

### Task C3: `CreateDocument` writes the file size

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go:150` (the `InsertDoc` call inside `CreateDocument`)

- [ ] **Step 1: Update the call.**

Find at `document.go:150`:

```go
		if err := i.mapping.InsertDoc(ctx, cozeID, ragResp.DocID, d.KnowledgeID, d.CreatorID, ragResp.TaskID, nowMs); err != nil {
```

Replace with:

```go
		if err := i.mapping.InsertDoc(ctx, cozeID, ragResp.DocID, d.KnowledgeID, d.CreatorID, ragResp.TaskID, int64(len(fileBytes)), nowMs); err != nil {
```

`fileBytes` is already in scope from R2-A's `i.storage.GetObject` call earlier in the same loop.

- [ ] **Step 2: Build the package.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: production code compiles. Test files still fail at the other three `InsertDoc` call sites.

- [ ] **Step 3: Do not commit yet.** Continues into Task C4.

---

### Task C4: Update read paths to populate `Size`

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go` (ListDocument loop + MGetDocument loop)

- [ ] **Step 1: Update `ListDocument` to set `Size: dm.Size`.**

In the loop body inside `ListDocument` (around line 236-246, last touched in Task B3), find the `entity.Document` literal and add `Size: dm.Size` inside the `knowledgeModel.Info{...}` block — `Size` is a field on the embedded `Info` struct.

Verify by reading `backend/crossdomain/knowledge/model/info.go` (or wherever the `Info` struct lives) to confirm the field is called `Size` and has type `int64`. If the field is on `entity.Document` itself rather than on the embedded `Info`, set it at the entity level instead. The grep is:

```bash
grep -n "Size\s\+int64" backend/crossdomain/knowledge/model/*.go backend/domain/knowledge/entity/document.go
```

`entity.Document.Size` (verified during plan-writing — see entity/document.go:33) is on the entity directly, NOT inside `Info`. So the assignment is at the outer struct level.

After update:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          dm.CozeID,
				Name:        rd.Filename,
				CreatorID:   dm.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID:   dm.KBID,
			Status:        RagStatusToEntity(rd.Status),
			FileExtension: parser.FileExtension(rd.FileType),
			Size:          dm.Size,
		})
```

- [ ] **Step 2: Update `MGetDocument` to set `Size: m.Size`.**

Same shape; in the MGetDocument loop body (post-Task-B3 version), add `Size: m.Size` to the `entity.Document` literal at the outer struct level:

```go
		out = append(out, &entity.Document{
			Info: knowledgeModel.Info{
				ID:          m.CozeID,
				Name:        rd.Filename,
				CreatorID:   m.CreatorID,
				CreatedAtMs: rd.CreatedAt.UnixMilli(),
				UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
			},
			KnowledgeID:   m.KBID,
			Status:        RagStatusToEntity(rd.Status),
			FileExtension: parser.FileExtension(rd.FileType),
			Size:          m.Size,
		})
```

- [ ] **Step 3: Build the package.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./domain/knowledge/service/ragimpl/...`
Expected: production code compiles. Tests still fail at three `InsertDoc` call sites.

- [ ] **Step 4: Do not commit yet.** Continues into Task C5.

---

### Task C5: Update test call sites for new `InsertDoc` signature

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/mapping_test.go:212` (TestMapping_InsertDocAndSoftDelete)
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go:84` (some test setup)
- Modify: `backend/domain/knowledge/service/ragimpl/retrieval_test.go:65` (TestRetrieve setup)

- [ ] **Step 1: Update each `InsertDoc` call.**

For each of the three test sites, insert a `size` argument between the existing `lastTaskID` and `nowMs` arguments. Use a representative value (e.g., `1024` for a 1KB doc, or `0` if the test doesn't care).

Example for `mapping_test.go:212`:

```go
require.NoError(t, m.InsertDoc(context.Background(), 500, "rag-doc-500", 100, 7, "task-99", 1024, 1700000000))
```

(`1024` is the new `size` argument; `1700000000` stays as the millis-timestamp.)

For `document_test.go:84`:

```go
require.NoError(t, i.mapping.InsertDoc(context.Background(), 4242, "rag-doc-Z", 100, 7, "task-Z", 0, 1700000000))
```

For `retrieval_test.go:65`:

```go
require.NoError(t, i.mapping.InsertDoc(ctx, 555, "rag-doc-X", 100, 7, "task-1", 0, 0))
```

- [ ] **Step 2: If `mapping_test.go::TestMapping_InsertDocAndSoftDelete` asserts on row contents, extend it to also assert `Size == 1024`.**

If the test fetches the inserted row and only checks specific columns, add a `require.Equal(t, int64(1024), got.Size)` assertion. If it doesn't fetch the row in detail, skip — the value will still be exercised via the new column path.

- [ ] **Step 3: Run all tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 4: Do not commit yet.** Continues into Task C6.

---

### Task C6: Commit Phase C

**Files:** (no edits; commit only — note this commit includes the atlas migration file from Task C1 Step 2 if one was generated)

- [ ] **Step 1: Inspect what changed.**

Run: `git status` from `/Users/liuxinyu/workspace/coze-studio`. Confirm changes are limited to:
- `docker/atlas/opencoze_latest_schema.hcl` (and possibly `docker/atlas/migrations/*` if the toolchain regenerated a migration file)
- `backend/domain/knowledge/service/ragimpl/mapping.go`
- `backend/domain/knowledge/service/ragimpl/document.go`
- `backend/domain/knowledge/service/ragimpl/mapping_test.go`
- `backend/domain/knowledge/service/ragimpl/document_test.go`
- `backend/domain/knowledge/service/ragimpl/retrieval_test.go`

- [ ] **Step 2: Commit.**

```bash
git add docker/atlas/opencoze_latest_schema.hcl \
        docker/atlas/migrations/ \
        backend/domain/knowledge/service/ragimpl/mapping.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/mapping_test.go \
        backend/domain/knowledge/service/ragimpl/document_test.go \
        backend/domain/knowledge/service/ragimpl/retrieval_test.go
# Note: `docker/atlas/migrations/` may be empty (no new SQL generated) if the
# project's migration flow regenerates lazily; that's fine.
git commit -m "$(cat <<'EOF'
feat(ragimpl): persist document file size on the coze side

Rag does not return a file size field on its Document response (verified
against 0e1f49b: top-level keys are doc_id, filename, file_type, status,
chunk_count, error_msg, source_modality, created_at, updated_at, plus
nested processing_config / processing_summary — no size anywhere).

Adds a size column to rag_doc_mapping, populated at CreateDocument time
from len(fileBytes), and reads it back in MGetDocument / ListDocument so
entity.Document.Size is non-zero on rag-backed KBs. Existing rows (pre-
migration) have size=0; the UI renders blank rather than failing — they
were uploaded before R2-A landed, so size data simply doesn't exist for
them.
EOF
)"
```

---

## Phase D — Verification

### Task D1: Full backend test sweep + go vet

**Files:** (no edits; verification only)

- [ ] **Step 1: Run the test sweep.**

```bash
cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go test \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: PASS for all packages.

- [ ] **Step 2: Run the full backend build to confirm no transitive regressions.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean build, no warnings.

- [ ] **Step 3: Run go vet on the changed packages.**

```bash
cd backend && GOTOOLCHAIN=go1.24.0 go vet \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: clean.

- [ ] **Step 4: If vet flags anything, fix it and commit as a chore.** Do NOT amend a published commit.

---

### Task D2: Manual end-to-end smoke

This is the real verification. The httptest tests prove the wire shape; only a real upload + UI poll proves the user-visible improvements landed.

- [ ] **Step 1: Bring up rag + coze middleware** per the recipe in the project memory's queued item #2. If the stacks are already up from R2-A's smoke, just confirm they're still healthy:

```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | head -20
curl -s http://localhost:8000/ready
```

- [ ] **Step 2: Rebuild and restart coze server.**

```bash
pkill -f opencoze 2>/dev/null
cd /Users/liuxinyu/workspace/coze-studio && GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
until lsof -iTCP:8888 -sTCP:LISTEN >/dev/null 2>&1 || grep -qE "panic:|FATAL" /tmp/coze-server.log; do sleep 3; done
echo "server: $(lsof -iTCP:8888 -sTCP:LISTEN >/dev/null 2>&1 && echo OK || echo FAIL)"
```

Note: `make server` will rebuild the Go binary. The Atlas migration from Phase C should auto-apply via `make sync_db` (run during middleware startup) — verify with:

```bash
docker exec coze-mysql mysql -uroot -proot opencoze -e "DESC rag_doc_mapping;" | grep size
```

If the `size` column is missing, run `make sync_db` manually:

```bash
cd /Users/liuxinyu/workspace/coze-studio && make sync_db
```

- [ ] **Step 3: Smoke through the UI.**

1. Log in at `http://localhost:8888`.
2. Create a fresh rag-backed KB (text mode) — old KBs created before R2-B will have `size=0` for their docs, so test with a fresh one.
3. Upload a small `.txt` or `.pdf`.
4. Open DevTools Network panel.

Expected during the upload + progress poll:
- `POST .../documents` → 200 (R2-A behavior, unchanged).
- `POST .../api/knowledge/document/progress/get` (polled every 2s):
  - First poll (rag still pending): `status: 1, progress: 10` ← new vs R2-A's 0
  - Mid-pipeline: `status: 2, progress: 50`
  - After success: `status: 4, progress: 100`
- `POST .../api/knowledge/document/list`:
  - `document_infos[0].name: "<your-filename>"` ← non-empty
  - `document_infos[0].type: "txt"` or `"pdf"` ← non-empty
  - `document_infos[0].size: <bytes>` ← non-zero

UI:
- `<UploadProgressPoll />` progress bar visibly fills (10% → 50% → 100%) instead of staying at 0%.
- KB detail page shows the real document name and size.

- [ ] **Step 4: Capture artifacts on failure.**

If anything regresses or doesn't show the expected values:
- Browser network panel for the failing request.
- `tail -200 /tmp/coze-server.log`.
- `docker compose -f /Users/liuxinyu/workspace/rag/docker-compose.yml -f /Users/liuxinyu/workspace/rag/docker-compose.local.yml logs --since=5m web worker`.

A common failure mode: `size: 0` on a fresh upload would indicate `len(fileBytes)` wasn't passed through correctly in Task C3. Re-check `document.go:150`.

- [ ] **Step 5: Update the project memory.** If the smoke passes, append one line under `## What's done` in `/Users/liuxinyu/.claude/projects/-Users-liuxinyu-workspace/memory/project-coze-rag-replacement-paused.md`:

```markdown
- **R2-B landed YYYY-MM-DD**: Task / Document field renames + coarse progress + size persistence. Smoke green: UI shows real filename + size + progress transitions.
```

Also mark item #10 R2-B as DONE in the same file.

- [ ] **Step 6: No commit.** Manual smoke does not change tracked files.

---

## Out of scope (do not address in this plan)

- **R2-C**: `Retrieve.query_image` object shape; union-friendly `ErrorBody` decoder. The current ErrorBody decoder misses rag's flat `{code, message, data, request_id}` envelope and FastAPI pydantic 422 array shape — but R2-B does NOT touch error handling.
- **R2-D**: New endpoints (`/capabilities`, `POST .../retry`, `/document-parameter-schemas`) and the wizard rework that consumes them.
- **R2-E**: Broader httptest scaffolding for the rest of the rag client; extending `rag-contract-check` to body schemas.
- **Bucket-B stub UI hiding** (queued item #12 — Manual chunk editor / re-segment / metadata-update entry points dead-end at the rag-pending stub when navigating into a rag-backed KB detail).
- **Frontend code changes** — none needed. The service-layer DTO field names are unchanged.
- **KB-level aggregate `all_file_size`** in the dataset detail response — that's a sum across docs computed in `application/knowledge/convertor.go` (not touched here). Once individual docs have non-zero `Size`, the aggregate should naturally become non-zero too; verify in smoke Step 3 but no code change planned in R2-B for it.

---

## Self-review checklist (filled in)

1. **Spec coverage** — every section in the spec has a corresponding task:
   - §3.2 Task contract change → Task A1
   - §3.3 Document contract change → Task B1
   - §4.2 progressForStatus helper → Task A3
   - §4.3 Atlas migration → Task C1
   - §4.4 mapping repo changes → Task C2
   - §4.5 caller updates → Tasks A3 (MGetDocumentProgress), B3 (MGetDocument / ListDocument), C3 (CreateDocument), C4 (Size population)
   - §5.3 httptest contract tests → Task A2 (GetTask + Nullable timestamps), Task B2 (GetDocument + ListDocuments)
   - §8.3 progressForStatus direct test — folded into Task A4 (covered by the existing MGetDocumentProgress tests being re-assertion-updated to the new coarse values; if a standalone unit test is preferred, add it in Task A4 Step 1 as an extra hit).
   - §8.5 smoke → Task D2.

2. **Placeholders** — none. The Atlas migration step has a defensive note ("if the toolchain regenerated a migration file") because the project's exact Atlas flow wasn't verified at plan-writing time; the executor must inspect `git status` after running `make atlas-hash` to know what to stage.

3. **Type consistency** — `Task.ErrorMsg` matches between A1 (definition), A3 (usage), and A4 (test stub updates). `Document.Filename`/`FileType`/`ChunkCount` match between B1 (definition), B2 (test), B3 (caller), B4 (test stub updates). `DocMapping.Size` and `InsertDoc(..., size int64, ...)` signature match between C2 (definition), C3 (production caller), C5 (test callers), and C4 (Size population in entity.Document).
