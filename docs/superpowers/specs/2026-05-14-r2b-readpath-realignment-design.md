# R2-B: Task / Document read-path realignment + size persistence

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-14-r2a-createdocument-multipart-design.md` (R2-A)
**Sibling slices:** R2-C (Retrieve + ErrorBody decoder), R2-D (new endpoints), R2-E (broader test scaffolding) — deferred to follow-on specs

## 1. Motivation

The 2026-05-14 round-2 contract audit (against rag `0e1f49b`) found that beyond the multipart switch that R2-A handled, rag also renamed and reshaped the fields it returns on read endpoints:

- `GET /tasks/{task_id}` now returns `{task_id, type, status, retry_count, error_msg, created_at, started_at, finished_at}`. Coze's contract `Task` decodes `{task_id, doc_id, status, progress, error, updated_at}`. Only `task_id` and `status` actually survive; everything else silently drops.
- `GET /knowledgebases/{kb_id}/documents/{doc_id}` and `GET .../documents` renamed `name` → `filename`, dropped `kb_id` (the KB scope is in the URL path), and added new top-level fields `file_type`, `chunk_count`, `error_msg`, `source_modality`. Coze's contract `Document` still decodes `name`, so the UI gets empty file names.
- Rag emits no numeric progress anywhere — both `Task.Progress` (top-level) and the body of `GetDocumentProgress`'s response are now permanently 0 on coze.

The 2026-05-14 smoke confirmed the user-visible symptom: KB detail panel shows `file_list: [""]`, `all_file_size: "0"`, doc status stuck visually at "处理中" even after rag has flagged the task `success`.

R2-A unblocked the upload path. R2-B fixes the read path so the UI displays correct, up-to-date document metadata and the progress poller shows meaningful state transitions.

Additionally, rag does not expose a `size` field on its Document response at all (verified by hitting `GET /knowledgebases/.../documents/{doc_id}` against a live rag — the only top-level keys are `doc_id, filename, file_type, status, chunk_count, error_msg, source_modality, created_at, updated_at, delete_cleanup_errors, processing_config, processing_summary`). To populate the UI's `all_file_size` column, coze must persist file size locally at upload time and read it back on the document-detail path. This is folded into R2-B because the read-path is exactly where it surfaces, and the migration is small.

## 2. Goals & non-goals

### Goals

- `contract.Task` and `contract.Document` byte-for-byte match rag's `0e1f49b` response shapes.
- `MGetDocumentProgress` produces a useful `Progress` value derived from `task.Status` (coarse mapping) so the `<UploadProgressPoll />` progress bar reflects pipeline state instead of always 0.
- `MGetDocument` and `ListDocument` populate `entity.Document.Name` from `rd.Filename` so the UI shows the real document name.
- `entity.Document.Size` is populated from a new `size` column on `rag_doc_mapping`, written at upload time.
- Two new `httptest`-based contract tests (`GetTask`, `GetDocument`) lock the wire shape so the next field-rename drift fails a unit test rather than a smoke.
- All existing tests (`ragimpl`, `infra/rag`, `application/knowledge`) stay green.

### Non-goals

- Retrieve.query_image object shape and ErrorBody union decoder (→ R2-C).
- Wiring rag's new endpoints `/capabilities`, `POST .../retry`, `/document-parameter-schemas` (→ R2-D).
- Broader `httptest` scaffolding for the rest of the rag client surface or extending `rag-contract-check` to body schemas (→ R2-E).
- Hiding bucket-B stub UI entry points (manual chunk editor, document re-segmentation, etc.) — queued item #12 of the project memory, separate concern.
- Frontend code changes. The service-layer DTO returned by `MGetDocumentProgress` and `MGetDocument` keeps the same field names; only the source of values changes. `<UploadProgressPoll />` continues to consume `progress` / `status` / `status_descript` / `document_name` unchanged.
- Real-time progress beyond the coarse 4-state mapping. Future work could integrate rag's `/capabilities` once it exposes per-phase progress, but until then the coarse mapping is the best signal available.

## 3. Contract change

### 3.1 Rag's actual wire shape (frozen as of `0e1f49b`)

**`GET /api/v1/tasks/{task_id}`** envelope `data`:

```json
{
  "task_id": "...",
  "type": "ingestion",
  "status": "pending|running|retrying|success|failed",
  "retry_count": 0,
  "error_msg": "..." | null,
  "created_at": "2026-05-14T13:25:57.009000",
  "started_at": "2026-05-14T13:26:00.055000" | null,
  "finished_at": "2026-05-14T13:26:04.484000" | null
}
```

**`GET /api/v1/knowledgebases/{kb_id}/documents/{doc_id}`** envelope `data`:

```json
{
  "doc_id": "...",
  "filename": "...",
  "file_type": "pdf|txt|docx|...",
  "status": "pending|processing|ready|failed",
  "chunk_count": 80,
  "error_msg": "..." | null,
  "source_modality": "text_source|image_source|scanned_document_source",
  "created_at": "...",
  "updated_at": "...",
  "delete_cleanup_errors": [...],
  "processing_config": { ... nested ... },
  "processing_summary": { ... nested ... }
}
```

Coze decodes only the top-level scalar fields it needs; `delete_cleanup_errors`, `processing_config`, `processing_summary` are intentionally ignored.

### 3.2 Coze-side `Task` after R2-B

| Field | Type | JSON | Notes |
|---|---|---|---|
| `TaskID` | `string` | `task_id` | unchanged |
| `Type` | `string` | `type` | NEW |
| `Status` | `string` | `status` | unchanged |
| `RetryCount` | `int` | `retry_count` | NEW |
| `ErrorMsg` | `string` | `error_msg` | renamed from `Error` |
| `CreatedAt` | `RagTime` | `created_at` | NEW |
| `StartedAt` | `*RagTime` | `started_at,omitempty` | NEW; pointer because rag emits null pre-start |
| `FinishedAt` | `*RagTime` | `finished_at,omitempty` | renamed from `UpdatedAt`; pointer because rag emits null pre-completion |

Dropped: `DocID`, `Progress`, `UpdatedAt`.

`StartedAt` and `FinishedAt` are pointers because rag emits `null` for these fields before the task transitions. `RagTime.UnmarshalJSON` handles JSON `null` cleanly only when the field is a pointer — a value receiver would treat null as a missing field and leave the zero-value time in place, which is then indistinguishable from "task started at the unix epoch."

### 3.3 Coze-side `Document` after R2-B

| Field | Type | JSON | Notes |
|---|---|---|---|
| `DocID` | `string` | `doc_id` | unchanged |
| `Filename` | `string` | `filename` | renamed from `Name` |
| `FileType` | `string` | `file_type` | NEW |
| `Status` | `string` | `status` | unchanged |
| `ChunkCount` | `int` | `chunk_count` | NEW |
| `ErrorMsg` | `string` | `error_msg` | NEW (was on Task as `error`, but Document has its own) |
| `SourceModality` | `string` | `source_modality` | NEW |
| `CreatedAt` | `RagTime` | `created_at` | unchanged |
| `UpdatedAt` | `RagTime` | `updated_at` | unchanged |

Dropped: `Name`, `KBID`. Coze derives the KB id from the call context (the URL path already requires `kb_id`).

## 4. Architecture

### 4.1 Flow

```
HTTP GET /api/knowledge/document/progress/get
  → app/knowledge.GetDocumentProgress
    → svc.ragimpl.MGetDocumentProgress
        → for each coze doc_id:
            → mapping.DocByCozeID → {RagDocID, LastTaskID, KBID, Size}    [READ size]
            → rag.Client.GetTask(LastTaskID)
              ← Task{ Status, ErrorMsg, ... }
            → dp.Progress = progressForStatus(task.Status)             [NEW helper]
            → dp.StatusMsg = task.ErrorMsg
            → dp.Status = taskStatusToDoc(task.Status)
      ← []*service.DocumentProgress

HTTP POST /api/knowledge/document/list
  → app/knowledge.ListDocument
    → svc.ragimpl.ListDocument
        → rag.Client.ListDocuments(...) → []Document{ Filename, FileType, ChunkCount, ... }
        → for each rag Document:
            → mapping.docByRagID(rd.DocID) → {CozeID, KBID, CreatorID, Size}
            → entity.Document{
                Name:          rd.Filename,             [WAS rd.Name]
                Size:          dm.Size,                  [NEW, from mapping]
                FileExtension: parser.FileExtension(rd.FileType),  [NEW]
                ...
              }

HTTP POST /api/knowledge/document/upload (R2-A path, slightly extended in R2-B)
  → ragimpl.CreateDocument
    → storage.GetObject(d.URI) → fileBytes
    → rag.Client.CreateDocument → UploadDocumentResponse
    → mapping.InsertDoc(..., int64(len(fileBytes)))     [NEW size param]
```

### 4.2 New helper

```go
// progressForStatus maps rag's task status string to a coarse 0-100 progress
// value for UI display. Rag dropped its numeric progress field in 0e1f49b; this
// is the best approximation until /capabilities exposes per-phase progress.
//
// Pending tasks show a small non-zero so the progress bar isn't visually
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

### 4.3 Schema migration (atlas)

`docker/atlas/opencoze_latest_schema.hcl::table "rag_doc_mapping"` gains:

```hcl
column "size" {
  null     = false
  type     = bigint
  unsigned = true
  default  = 0
  comment  = "Document file size in bytes; coze-side, since rag does not return size on its Document response."
}
```

`size` is appended after the existing columns. Default `0` so existing rows (created before R2-B) remain valid; the read path treats 0 as "size unknown" and the UI shows blank rather than failing.

### 4.4 Mapping repo changes

`DocMapping` struct (mapping.go:42) gains `Size int64`.

`InsertDoc` signature gains `size int64` between `lastTaskID` and `nowMs`:

```go
func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, size int64, nowMs int64) error
```

`DocByCozeID`, `docByRagID`, `DocsByCozeIDs` SELECT clauses extend to include `size` and populate the field. The compile-time check via `gorm:"column:size"` in the row structs catches missing migrations at startup if the column doesn't exist.

### 4.5 Caller updates in `ragimpl/document.go`

| Line (current) | Change |
|---|---|
| `CreateDocument` per-doc loop, ~line 148 | Pass `int64(len(fileBytes))` to `mapping.InsertDoc` |
| `MGetDocumentProgress`, line 328 | `dp.Progress = progressForStatus(task.Status)` (was `task.Progress`) |
| `MGetDocumentProgress`, line 329 | `dp.StatusMsg = task.ErrorMsg` (was `task.Error`) |
| `ListDocument`, line 239 | `Name: rd.Filename` (was `rd.Name`) |
| `ListDocument`, line 239 | Also set `Size: dm.Size`, `FileExtension: parser.FileExtension(rd.FileType)` |
| `MGetDocument`, line 283 | `Name: rd.Filename` (was `rd.Name`) |
| `MGetDocument`, line 283 | Also set `Size: m.Size`, `FileExtension: parser.FileExtension(rd.FileType)` |

`buildDocMetadata` (line 43-55) still uses `d.Name` from the input `entity.Document` — that's the coze-side filename the caller has at hand. No change there.

## 5. Components

### 5.1 Migration ordering

Single forward-only migration step:
1. ALTER TABLE adds `size BIGINT UNSIGNED NOT NULL DEFAULT 0`.

Atlas's diff command (`make atlas-hash`) produces the migration file. The migration is idempotent under `IF NOT EXISTS` semantics, but since this column is fresh, the standard ALTER is sufficient.

### 5.2 `progressForStatus` location

Place next to `taskStatusToDoc` and `RagStatusToEntity` (factory.go bottom or document.go). It's a small pure function tightly coupled with the other status mappers and should live with them.

### 5.3 httptest contract tests

Append to `backend/infra/rag/client_test.go` (already grew in R2-A):

**`TestGetTask_FieldShape`** — handler returns rag-shaped envelope with the full new field set; assert decoded `Task` exposes `TaskID`, `Type`, `Status`, `RetryCount`, `ErrorMsg`, `CreatedAt`, `StartedAt`, `FinishedAt` with correct values. Include a sub-case where `started_at` and `finished_at` are JSON `null` to verify pointer-field handling.

**`TestGetDocument_FieldShape`** — handler returns the new Document envelope; assert `DocID`, `Filename`, `FileType`, `Status`, `ChunkCount`, `ErrorMsg`, `SourceModality`, `CreatedAt`, `UpdatedAt`. Note the test must NOT assert anything about `Name`, `KBID`, or `Size` — those are not on rag's response.

**`TestListDocuments_FieldShape`** — same as GetDocument but for list response; envelope `data` is `{items: [...], total: int}`.

## 6. Data flow & invariants

- `rag_doc_mapping.size` is set once at `CreateDocument` time and never updated. If a doc is re-uploaded under a new coze id, a new mapping row is created; the old row is soft-deleted (existing behavior).
- `progressForStatus` is pure and stable across invocations. No persistence; no side effects.
- `task.ErrorMsg` flows directly into `service.DocumentProgress.StatusMsg`. Empty string when rag emits null (Go's zero-value for `string` matches JSON null silently for non-pointer types — acceptable because the UI conditionally renders only when non-empty).
- `Task.StartedAt` and `Task.FinishedAt` as pointers: callers must nil-check before dereferencing. Currently no production caller reads these — they're surfaced for future use (e.g., R2-D's progress-detail UI). No new behavior depends on them in R2-B.

## 7. Error handling

| Failure | Behavior |
|---|---|
| Migration fails at startup | Service init refuses to start (existing Atlas behavior); operator sees a clear error. |
| Old row read where `size = 0` (pre-migration data) | UI shows blank in the size column. Logged as DEBUG not WARN — this is expected during the transition window. |
| `task.ErrorMsg = null` | Decodes as `""`; `dp.StatusMsg = ""`; UI conditional hides error block. |
| `rd.Filename = ""` (shouldn't happen — rag enforces filename on upload) | Falls through as empty Name; UI shows doc id as fallback (existing `<UploadProgressPoll />` behavior). |
| `task.Status` is an unknown string | `progressForStatus(unknown) = 0`; `taskStatusToDoc(unknown) = Failed`. Fail closed. |

## 8. Testing

### 8.1 New contract tests

Three httptest tests in `client_test.go` per §5.3 above. Each spins up `httptest.NewServer`, returns a rag-shaped envelope, asserts the decoded struct byte-for-byte.

### 8.2 Updated unit tests

Tests in `backend/domain/knowledge/service/ragimpl/document_test.go` (and any other `*_test.go` in this package) that stub `fakeClient.GetTask` or `fakeClient.GetDocument`/`ListDocuments` need their stub return values updated to the new field names. The compile-time impact is mechanical: `Name:` becomes `Filename:`, `Error:` becomes `ErrorMsg:`, dropped fields disappear, new fields are populated where the test asserts on them.

`fakeClient` already lives in `knowledge_test.go`; its method signatures don't change (signatures use the contract types, which change shape but not name).

### 8.3 New direct-tests for the helper

`progressForStatus_test.go` (or inline in document_test.go) — table-driven, one row per status value plus an "unknown" case.

### 8.4 Existing tests

`make middleware && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...` stays green.

### 8.5 Smoke validation

Re-run the R2-A smoke recipe (project memory item #2). Expected differences vs. R2-A smoke:
- `MGetDocumentProgress`: while task is pending → `status: 1, progress: 10`; while running → `status: 2, progress: 50`; after success → `status: 4, progress: 100`.
- `ListDocument`: `name: "7.测试计划.docx"` (real filename), `type: "docx"`, `size: <bytes>`, `slice_count: 80` for the test doc.
- `<UploadProgressPoll />` UI: progress bar visibly transitions through 10% → 50% → 100% as the doc moves through the pipeline.

## 9. Compatibility & rollout

- Atlas migration is forward-only. Rollback requires a new migration adding the column back if it gets dropped — but R2-B never drops a column, only adds.
- Existing rows in `rag_doc_mapping` (before R2-B) get `size = 0` by default. Read path returns 0; UI shows blank. Acceptable for the transition window.
- No frontend code change. The service-layer DTO `entity.Document` / `service.DocumentProgress` keeps the same field names; only the source of values changes.
- No external API contract break. Coze's `/api/knowledge/document/progress/get` and `/api/knowledge/document/list` endpoints emit the same shape; the field values inside change from "empty/zero" to "populated."
- Legacy backend (`KNOWLEDGE_BACKEND=legacy`) path is untouched.

## 10. Open questions

None that block writing the implementation plan. Minor judgment calls deferred to impl-time:

1. **Where exactly `progressForStatus` lives** — `factory.go` (next to other helpers) vs. `document.go` (next to its only caller). Cosmetic. Recommend factory.go for cohesion.
2. **JSON tag for `chunk_count`** — coze entity has `SliceCount`, rag emits `chunk_count`. The struct field `ChunkCount` is on the *contract* (rag side); the *entity*'s SliceCount is separately populated. Mapping happens in ragimpl. No naming collision.
3. **Whether to keep `Task.StartedAt`/`FinishedAt`** if no caller reads them yet. Keep them — the audit memo explicitly calls them out as "surface what rag emits"; YAGNI argues for removing, but the marginal cost is two pointer fields and they document the upstream shape for future readers. Keep.
