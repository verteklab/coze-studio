# Coze UI ↔ rag flow alignment (Phase 1.5)

**Date:** 2026-05-13
**Status:** Draft
**Predecessor:** `2026-05-12-replace-knowledge-module-with-rag-design.md` (Phase 1)
**Successor:** PR-2 legacy deletion (deferred)

## 1. Motivation

The 2026-05-13 end-to-end smoke against `KNOWLEDGE_BACKEND=rag` exposed that coze's frontend upload wizard always traverses the legacy "preview chunks → manually adjust → commit" sub-flow, which calls `CreateDocumentReview` — one of the bucket-B stubs in `backend/domain/knowledge/service/ragimpl/unsupported.go`. The user clicks "next" after upload, frontend hits `/api/knowledge/document_review/create`, ragimpl returns `ErrRagFeaturePendingCode`, and the modal dead-ends.

The same wall sits in front of two adjacent upload paths:

- Table KBs need `GetAlterTableSchema` / `ValidateTableSchema` / `GetDocumentTableInfo` / `GetImportDataTableSchema` for the legacy "configure column schema before commit" step.
- Image KBs need `ExtractPhotoCaption` for the legacy "review/edit auto-generated captions" step.

These three step categories — review, table-schema, photo-caption — are coze concepts that don't map onto rag's design (rag locks chunking strategy at KB-creation time and runs ingestion as a single async task with no human-in-the-loop intermediate). Implementing them in rag would be a pure adoption of legacy semantics, which the Phase 1 spec explicitly rejected ("rag owns all knowledge business logic; coze becomes a frontend display layer").

This document specifies the **inverse path**: refactor coze's upload UI so rag-backed KBs use a shorter wizard that matches rag's actual ingest model. Legacy KBs keep the current behavior unchanged; the divergence is gated per-KB so mixed-backend deployments work during the transition between Phase 1 and PR-2.

## 2. Goals & non-goals

### Goals

- A rag-backed KB's upload wizard never calls `CreateDocumentReview`, `MGetDocumentReview`, `SaveDocumentReview`, `GetAlterTableSchema`, `ValidateTableSchema`, `GetDocumentTableInfo`, `GetImportDataTableSchema`, or `ExtractPhotoCaption`.
- A legacy-backed KB's upload wizard is functionally identical to today's behavior — same components, same step count, same network calls.
- The frontend decides which wizard to mount based on a single explicit field on the KB detail response. Discovery is a one-shot read of data the frontend already fetches.
- PR-2 (legacy deletion) can remove the legacy upload wizard, its steps, and the router by deleting a small, contiguous set of directories without unraveling rag-mode code.

### Non-goals

- Implementing the removed step semantics on the rag side (e.g., adding a "preview chunking" endpoint to rag). The Phase 2 strategy decision in the project memory's queued item #8 leans against this; this spec assumes that decision stands.
- Handling the 11 other bucket-B stubs not in the upload path: doc metadata update, document re-segmentation, manual chunk CRUD (7), KB copy/move. These either don't sit on the upload happy path or correspond to features that won't have entry points in the rag-mode wizard (handled in §6 out-of-scope).
- Replacing the rag-mode "wait for ingest" step with a richer UX that surfaces chunk content (would depend on `ListSlice`, which is a bucket-B stub).

## 3. Architecture

### 3.1 Gating signal

A new field on the coze KB detail response:

```idl
struct DatasetInfo {
  // existing fields
  optional string backend  // "rag" | "legacy"
}
```

Derivation lives in the application-layer method that materializes the detail DTO (most likely `application/knowledge.GetDataset` — confirmed during implementation plan discovery, see §9 Q1):

```go
isRag, err := h.ragKBMappingDAO.Exists(ctx, datasetID)
if err != nil {
    return nil, err
}
info.Backend = ternary(isRag, "rag", "legacy")
```

The field is `optional` so older clients that don't read it degrade to legacy semantics (safe — the worst case is the same dead-end users already hit today, not a new failure mode).

### 3.2 Frontend layout

Branching happens at one spot per KB type — the wizard entry component — by an `<UploadEntry>` shell that mounts either the existing legacy wizard or a new rag-mode wizard:

```tsx
export const TextLocalUploadEntry = ({ kb }: { kb: KnowledgeInfo }) =>
  kb.backend === 'rag'
    ? <TextLocalAddRag kb={kb} />
    : <TextLocalAddLegacy kb={kb} />;  // alias for current TextLocalAdd
```

New directories alongside (not inside) the legacy upload directories:

```
features/knowledge-type/
  text/first-party/local/
    add/           (legacy — unchanged)
    add-rag/       (new)
  image/file/
    add/           (legacy — unchanged)
    add-rag/       (new)
  table/first-party/local/
    add/           (legacy — unchanged)
    add-rag/       (new)
```

Each `add-rag/` directory mirrors the structure of its legacy sibling — `index.tsx`, `config.tsx`, `steps/upload/`, `steps/progress/` — but with the segment / preview / table-schema / caption steps removed. PR-2 (legacy deletion) becomes "delete the three `add/` directories and inline the rag-mode children into their parent locations" — a single, contiguous cleanup.

### 3.3 Step mapping

Per KB type, the legacy → rag-mode step set:

| KB type | Legacy steps | rag-mode steps | Removed |
|---|---|---|---|
| Text local | upload → segment → preview → processing | upload → progress | segment, preview (calls `CreateDocumentReview`) |
| Image file | upload → caption → preview → processing | upload → progress | caption (calls `ExtractPhotoCaption`), preview |
| Table local | upload → schema → validate → preview → processing | upload → progress | schema/validate/preview (calls `Get*TableSchema*` / `ValidateTableSchema` / `CreateDocumentReview`) |

All three rag-mode wizards have the same `[upload, progress]` shape. Only the upload step's `accept` MIME types and per-file validation rules differ — those are already factored out as configs in the legacy code (`textUploadChannelConfig`, etc.) and can be reused.

## 4. Components

### 4.1 New shared component: `<UploadProgressPoll />`

- **Inputs:** `docIds: string[]` (the documents pushed in the upload step), `kb: KnowledgeInfo`
- **Behavior:** polls `GetDocumentProgress` (existing coze endpoint, which fans out to ragimpl → rag `GET /documents/{doc_id}`) every 2 seconds, in parallel per doc. State machine per doc: `pending | processing | ready | failed`.
- **Render:** progress list (one row per file, status + percent), aggregate "N/M ready" summary at top.
- **Completion:** when all docs reach `ready`, auto-navigate to the KB detail page after a brief success toast.
- **Failure:** display the rag-supplied `error` string and a "重试" button. The retry click calls `RetryDocument` (coze service method). If ragimpl hasn't wired through to rag's `POST /documents/{doc_id}/retry` (a 2026-05-13 queued bonus item — implementation status TBD), the button is replaced with "联系管理员" placeholder text and a follow-up task is filed.
- **No client-side timeout.** rag's worker has its own retry/timeout policy. The frontend polls indefinitely; user can navigate away and the doc will keep processing server-side.

### 4.2 New per-type wizards: `<TextLocalAddRag />`, `<ImageFileAddRag />`, `<TableLocalAddRag />`

- Two-step containers (`upload`, `progress`)
- The `upload` step **reuses the legacy `<UploadUnitFile />` and per-type config** unchanged — file selection / drag-and-drop / per-file upload progress are not coze concepts that map back to rag's gaps, they're standard UI primitives
- Submit handler: POST `/api/knowledge/document/create` (existing), collect `doc_id`s, transition to `progress` step

### 4.3 New shell routers: `<TextLocalUploadEntry />` etc.

- 6-line components that read `kb.backend` and pick the wizard
- Mounted wherever `<TextLocalAdd />` (or equivalent) is currently mounted

### 4.4 Unchanged / reused

- `<UploadUnitFile />` (file picker)
- `<TextLocalTaskList />` (per-file upload progress shown during upload step)
- File size / type validation hooks
- All `FooterBtn` / footer plumbing
- Existing `GetDocumentProgress` API call (used by progress step)

## 5. Data flow

### 5.1 KB detail load (decides wizard)

```
frontend  ──GET /api/knowledge/dataset/detail──▶  coze handler
                                                      │
                                                      ▼
                                          application/knowledge.GetDataset
                                                      │
                                              ragKBMappingDAO.Exists
                                                      │
                                                      ▼
                                    DatasetInfo { ..., backend: "rag" | "legacy" }
                                                      │
frontend  ◀────────────────────────────────────────────┘
   │
   ▼
chooseUploadEntry(kb) → mounts AddRag or AddLegacy
```

### 5.2 rag-mode upload (replaces review flow)

```
upload step
   │ user clicks "提交"
   ▼
POST /api/knowledge/document/create
   │
   ▼
coze handler → ragimpl.CreateDocument → rag POST /knowledgebases/{kb_id}/documents
   │
   ▼ { doc_id, task_id, status: "pending" }
   │
frontend transitions to <UploadProgressPoll docIds={[doc_id, ...]} />
   │
   ▼ every 2s, parallel per doc
GET /api/knowledge/document/progress?id={doc_id}
   │
   ▼ { status, progress, error? }
   │
status="ready" for all docs  → success toast → navigate to KB detail page
status="failed" for any doc  → display error + retry CTA
otherwise                    → continue polling
```

### 5.3 `backend` field propagation

- IDL change in the thrift definition of `DatasetInfo`
- Go binding regenerates automatically (no hand edit)
- TS binding regenerates automatically (`KnowledgeInfo` gains `backend?: string`)
- A new helper `isRagBackend(kb): boolean` centralizes the comparison so the value space ("rag" / "legacy" / undefined) is checked in one place

## 6. Error handling

| Scenario | Behavior |
|---|---|
| `rag_kb_mapping` exists but rag service is down | KB detail still reports `backend="rag"`; user enters rag wizard; upload step's POST fails at `ragimpl.CreateDocument` → returns 5xx → standard toast (no special path) |
| `backend` field missing on KB detail (old client cache, untracked KB) | `chooseUploadEntry` defaults to legacy. Worst case is a rag KB falling into the legacy wizard and hitting the same dead-end as today's smoke — no new failure mode introduced |
| Upload succeeds, ingest task fails server-side (OpenAI rate limit, file corruption, etc.) | `<UploadProgressPoll />` observes `status="failed"`, surfaces rag's `error` string, offers retry CTA |
| Retry button clicked | Calls coze's `RetryDocument`. If ragimpl's `RetryDocument` is not yet wired through to rag's `POST /documents/{doc_id}/retry`, button reads "联系管理员"; a follow-up task is filed against ragimpl |
| Multi-file upload | Upload step pushes N files in parallel, collects N doc_ids, progress step polls all N in parallel, "N/M ready" header. Navigation only fires when all N are ready |
| Polling never sees `ready` | No client-side timeout. User can leave the page; doc continues ingesting server-side. If they come back via KB detail later, it'll show the up-to-date status from the same `GetDocumentProgress` endpoint |

## 7. Testing strategy

### Backend

- Unit test `application/knowledge.GetDataset` with a faked `ragKBMappingDAO`: returns `Exists=true` → response.Backend="rag"; returns `Exists=false` → response.Backend="legacy"; returns error → error propagates
- IDL change verified via existing Atlas hash workflow (no DB schema impact)

### Frontend

- Unit test `chooseUploadEntry(kb)`: returns rag component for `backend="rag"`, legacy component for `backend="legacy"`, legacy component for `backend=undefined`
- Unit test `isRagBackend()` helper: matrix of valid / unknown / undefined values
- Unit test `<UploadProgressPoll />` with mocked polling sequences:
  - `pending → processing → ready` (single doc, happy path; asserts navigation fires)
  - `pending → processing → failed` (single doc; asserts error string + retry CTA shown)
  - `pending → processing → ready` (multi doc, staggered completion; asserts aggregate counter updates correctly and nav only fires after all-ready)
- RTL "smoke" test per rag-mode wizard: render with a faked rag-backed KB, fill upload step, confirm transition to progress step

### Manual / E2E

- Repeat the 2026-05-13 UI smoke (register → login → create rag-backed KB → upload text file) and assert it now reaches the KB detail page with the document visible in the list. Verify rag-side logs show only `CreateDocument` + `GetDocument` polling calls (no `CreateDocumentReview`).
- Spot-check legacy path: create a legacy KB (toggle `KNOWLEDGE_BACKEND=legacy` for a session, or use an old KB pre-dating rag), upload a file, confirm the original 4-step wizard still renders and works.

## 8. Out of scope

The following bucket-B stubs are knowingly left unhandled by this work, and their UI entry points should either be hidden in the rag-mode UI or, if too deeply entangled with legacy components, removed in PR-2 as part of the broader legacy cleanup. None of them sit on the upload happy path:

- `UpdateDocument`, `ResegmentDocument` (document metadata / re-segmentation menus)
- `CreateSlice`, `UpdateSlice`, `DeleteSlice`, `ListSlice`, `GetSlice`, `MGetSlice`, `ListPhotoSlice` (manual chunk editor on KB detail page)
- `CopyKnowledge`, `MoveKnowledgeToLibrary` (KB management menu)

If any of these entry points are visible from the KB detail page that the rag-mode wizard navigates into on completion, the UI should detect `kb.backend === "rag"` and hide them. That hiding work is its own follow-up task and not part of this design.

## 9. Open questions

The following are not blocking the design decision but need answers before implementation:

- **Q1: Which exact handlers populate `DatasetInfo`?** Find every handler that returns a `DatasetInfo` (or its DTO ancestor) and ensure the `Backend` field is populated by each. List during implementation plan discovery.
- **Q2: Is the `KnowledgeInfo` TS type hand-written or generated?** If hand-written, the new `backend` field is a manual edit; if generated from IDL, regeneration is the source of truth. The 2026-05-12 spec mentioned IDL was hand-edited for `text_embedding_model_id` because there was no idl2ts entry point — confirm whether the same applies here.
- **Q3: Is `ragimpl.RetryDocument` already wired to rag's `POST /documents/{doc_id}/retry`?** If yes, the retry button works end-to-end; if no, this design's "联系管理员" stub path applies and a follow-up task lands.
- **Q4: How many places mount the per-type wizards today?** Each call site needs to swap to the `<*UploadEntry>` shell. Most likely a single mount per type (the KB-add modal scene), but worth verifying.

## 10. Rollout

- Lands as a single PR on the same branch as Phase 1 (`feat/replace-knowledge-base`) or as a follow-up branch — implementation plan decides based on size.
- No feature flag needed: legacy behavior is preserved per-KB by the gating signal; no flip is required to enable rag-mode wizards safely.
- PR-2 (legacy deletion) becomes simpler: delete `add/` directories, inline `add-rag/` children up one level, remove the `<*UploadEntry>` shell. The thrift `backend` field stays (becomes redundant but harmless) until a separate IDL cleanup.
