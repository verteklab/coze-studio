# R2-D-fe-Retry: enable document retry end-to-end

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-14-r2d-backend-three-endpoints-design.md` (R2-D-backend)
**Sibling slices:** R2-D-fe-Wizard (capabilities + parameter-schemas + wizard rework), R2-E (broader test scaffolding) — deferred

## 1. Motivation

R2-D-backend wired three new rag endpoints (`RetryDocument`, `GetCapabilities`, `ListDocumentParameterSchemas`) through to `ragimpl.Impl` as pass-throughs. None are reachable from the frontend yet: they're not on `service.Knowledge`, no application handler, no IDL, no UI consumer.

Of the three, **`RetryDocument` is the most urgent**:
- Project memory queued item #11 has been live for ~weeks: `<UploadProgressPoll />` renders a disabled "联系管理员" button on failed uploads because no retry method exists end-to-end.
- It's a vertical-slice exemplar — exercises every layer (service interface, application handler, IDL+regen, UI button) on the smallest surface, so it teaches us how the IDL/regen flow works for this branch before the wizard rework lands.
- The other two endpoints (`GetCapabilities`, `ListDocumentParameterSchemas`) feed wizard config, which requires a UI redesign. Retry is "click button, call API, refresh state" — much smaller cognitive load.

This slice closes the loop for retry. The wizard rework gets its own R2-D-fe-Wizard spec.

## 2. Goals & non-goals

### Goals

- `service.Knowledge` interface has a `RetryDocument` method with proper service-layer DTOs.
- `ragimpl.Impl.RetryDocument` matches the interface signature, calls rag's `/documents/{id}/retry`, AND updates `rag_doc_mapping.last_task_id` so `MGetDocumentProgress` automatically follows the retry's new task.
- Legacy `knowledgeSVC.RetryDocument` returns a not-supported error so legacy-mode builds compile.
- `KnowledgeApplicationService.RetryDocument` HTTP handler exists in `application/knowledge/`, follows the `ListRagModelProviders` precedent.
- IDL definition for the new RPC in `idl/data/knowledge/`; Go server and TypeScript client bindings regenerated.
- `<UploadProgressPoll />` retry button is enabled (no longer `<Button disabled>`); clicking it calls the new RPC and triggers re-render so the existing poll picks up the new task via the updated `last_task_id`.
- All existing tests stay green; new tests cover the mapping-update behavior and the frontend retry-click path.

### Non-goals

- `GetCapabilities` and `ListDocumentParameterSchemas` interface/handler/IDL/UI — deferred to R2-D-fe-Wizard.
- Wizard parameter-form rework, capabilities-driven step gating.
- Caching strategy for any of the three endpoints.
- Bucket-B stub UI hiding (queued item #12).
- R2-E's broader test scaffolding.
- Retry-with-modified-parameters UX (user can only retry as-is; parameter modification belongs to the wizard rework slice).
- Visual polish on the retry button (label, icon, loading state) beyond functional enablement.
- A separate `RetryDocumentResponse` IDL struct if the response carries no useful payload to the UI; a thin envelope (`{code, msg, BaseResp}`) plus the existing poll path may be enough. Plan resolves the exact shape.

## 3. Contract change

### 3.1 Service-layer DTOs (new)

In `backend/domain/knowledge/service/interface.go`, add the method to the `Knowledge` interface, grouped with the existing document methods (after `MGetDocumentProgress`, before `ResegmentDocument`):

```go
RetryDocument(ctx context.Context, request *RetryDocumentRequest) (response *RetryDocumentResponse, err error)
```

And add the request/response types alongside the other `*DocumentRequest`/`*DocumentResponse` types:

```go
type RetryDocumentRequest struct {
    DocumentID int64
}

type RetryDocumentResponse struct {
    // Document carries the refreshed entity with the post-retry status
    // (typically Init/Chunking — rag's UploadDocumentResponse returns
    // status="pending" or "processing"). Callers can immediately render
    // this to the UI; subsequent MGetDocumentProgress polls follow the
    // new task via the updated rag_doc_mapping.last_task_id.
    Document *entity.Document
}
```

### 3.2 Ragimpl signature realignment

R2-D-backend's `Impl.RetryDocument(ctx, cozeDocID int64) (*contract.CreateDocumentResponse, error)` is REPLACED with the interface-aligned signature:

```go
func (i *Impl) RetryDocument(ctx context.Context, req *service.RetryDocumentRequest) (*service.RetryDocumentResponse, error)
```

Body (sketch):
```go
tenant, _ := i.tenant(ctx)
dm, _ := i.mapping.DocByCozeID(ctx, req.DocumentID)
kb, _ := i.mapping.KBByCozeID(ctx, dm.KBID)
ragResp, _ := i.rag.RetryDocument(ctx, tenant, kb.RagKBID, dm.RagDocID)
// NEW: update last_task_id so MGetDocumentProgress follows the retry's new task.
i.mapping.UpdateLastTaskID(ctx, req.DocumentID, ragResp.TaskID)
// Build refreshed entity.Document from the mapping row + rag response.
return &service.RetryDocumentResponse{Document: ...}, nil
```

The two existing R2-D-backend unit tests (`TestRagimpl_RetryDocument`, `TestRagimpl_RetryDocument_MissingDocMapping`) are rewritten to match the new signature.

### 3.3 New mapping helper

`MappingRepo.UpdateLastTaskID(ctx context.Context, cozeDocID int64, taskID string) error` — a thin UPDATE statement on `rag_doc_mapping.last_task_id`. Mirrors the existing `SoftDeleteDoc`/`RestoreDoc` style.

No schema migration; the column already exists.

### 3.4 Legacy `knowledgeSVC.RetryDocument` stub

In `backend/domain/knowledge/service/knowledge.go`, add a method on `knowledgeSVC` returning a not-supported error:

```go
func (k *knowledgeSVC) RetryDocument(ctx context.Context, req *service.RetryDocumentRequest) (*service.RetryDocumentResponse, error) {
    return nil, errorx.New(errno.ErrKnowledgeFeatureNotSupportedInLegacyCode, errorx.KV("msg", "RetryDocument requires KNOWLEDGE_BACKEND=rag"))
}
```

Plan-time discovery: confirm the errno value exists. If absent, the closest existing fit is `ErrKnowledgeInvalidParamCode` with a clear msg; alternatively register a new errno. Plan resolves.

The frontend renders the retry button only inside the rag-mode wizards (`<UploadProgressPoll />` is mounted in `add-rag/` flows), so the legacy code path doesn't actually trigger in normal use.

### 3.5 Application handler

In `backend/application/knowledge/knowledge.go`, add a method on `KnowledgeApplicationService`:

```go
func (k *KnowledgeApplicationService) RetryDocument(ctx context.Context, req *dataset.RetryDocumentRequest) (*dataset.RetryDocumentResponse, error)
```

Where `dataset.RetryDocumentRequest` / `dataset.RetryDocumentResponse` come from regenerated thrift bindings. Handler:
1. Convert thrift request → service request (`{DocumentID}`)
2. Call `k.DomainSVC.RetryDocument(ctx, ...)` (which dispatches to ragimpl or legacy stub via the feature flag)
3. Convert service response → thrift response (a `DocumentInfo` field plus the standard `code`/`msg`/`BaseResp`).

No new internal helpers needed; mapping logic stays simple.

### 3.6 IDL definition

In `idl/data/knowledge/document.thrift`, add request and response structs grouped with `CreateDocumentRequest`/`Response`:

```thrift
struct RetryDocumentRequest {
    1: required i64 document_id (agw.js_conv='str', api.js_conv='true')
    255: optional base.Base Base
}

struct RetryDocumentResponse {
    1: optional DocumentInfo document_info
    253: required i64 code
    254: required string msg
    255: required base.BaseResp BaseResp
}
```

In `idl/data/knowledge/knowledge_svc.thrift`, add the RPC to `service DatasetService` block, grouped with the other document RPCs:

```thrift
document.RetryDocumentResponse RetryDocument(1: document.RetryDocumentRequest request)
    (api.post="/api/knowledge/document/retry", api.category="dataset")
```

(Path follows the existing convention: `/api/knowledge/document/<verb>` — match `progress/get`, `list`, etc.)

After IDL edits, regenerate Go server bindings and TypeScript client bindings per the project's standard codegen (Task 0 of the plan will discover the exact command — Phase 1.5's `<ModelSelector />` IDL change at commit `d76f5163` is the precedent).

### 3.7 Frontend changes

In `frontend/.../upload-progress-poll/index.tsx`:

```tsx
// Before:
<Button disabled>联系管理员</Button>

// After:
<Button onClick={handleRetry}>重试</Button>
```

Where `handleRetry` (added in the same file) calls `KnowledgeApi.RetryDocument({ document_id: id })` and on success, clears the local progress state for that doc so the next `tick()` re-polls. Failure surfaces as a toast (the existing failure path in the polling effect already handles transient blips silently — manual click failure deserves a visible signal).

i18n: the button label `联系管理员` had a TODO comment about waiting for an i18n key. The new "重试" label should use `I18n.t('datasets_retry_button')` or the closest existing key; if absent, plan adds it.

## 4. Architecture

### 4.1 Flow (happy path)

```
[user] click 重试 button
  → frontend handleRetry(docId)
    → KnowledgeApi.RetryDocument({document_id: docId})
      → HTTP POST /api/knowledge/document/retry {document_id}
        → coze.RetryDocument handler (registered via thrift codegen)
          → KnowledgeApplicationService.RetryDocument(ctx, thriftReq)
            → DomainSVC.RetryDocument(ctx, *service.RetryDocumentRequest{DocumentID})
              → ragimpl.Impl.RetryDocument
                → tenant resolve
                → mapping.DocByCozeID → {RagDocID, KBID}
                → mapping.KBByCozeID(KBID) → {RagKBID}
                → rag.Client.RetryDocument(tenant, RagKBID, RagDocID)
                  → POST .../{rag_kb_id}/documents/{rag_doc_id}/retry
                  ← UploadDocumentResponse{doc_id, task_id, status}
                → mapping.UpdateLastTaskID(DocumentID, task_id)  [NEW]
                ← *service.RetryDocumentResponse{Document: refreshedEntity}
            ← thrift response with document_info
          ← HTTP 200
        ← frontend receives response
      → setProgress(prev => {...prev, [docId]: undefined})  // force re-poll
    → next tick() picks up rag's new task via the updated mapping
```

### 4.2 Files touched

| Layer | File | Change |
|---|---|---|
| Service interface | `backend/domain/knowledge/service/interface.go` | Add `RetryDocument` method + `RetryDocumentRequest`/`RetryDocumentResponse` types |
| Ragimpl | `backend/domain/knowledge/service/ragimpl/document.go` | REWRITE `RetryDocument` to new signature; add `mapping.UpdateLastTaskID` call; build refreshed entity |
| Mapping | `backend/domain/knowledge/service/ragimpl/mapping.go` | Add `UpdateLastTaskID` helper |
| Legacy stub | `backend/domain/knowledge/service/knowledge.go` | Add `(knowledgeSVC).RetryDocument` returning not-supported error |
| Application | `backend/application/knowledge/knowledge.go` | Add `(KnowledgeApplicationService).RetryDocument` HTTP handler |
| IDL request/response | `idl/data/knowledge/document.thrift` | Add `RetryDocumentRequest` + `RetryDocumentResponse` |
| IDL service | `idl/data/knowledge/knowledge_svc.thrift` | Add `RetryDocument` RPC in `DatasetService` |
| Codegen artifacts | `backend/api/handler/coze/...` + `backend/api/model/...` + `frontend/packages/arch/idl/src/auto-generated/knowledge/...` | Regenerated; do not hand-edit |
| Frontend | `frontend/.../upload-progress-poll/index.tsx` | Enable button; add `handleRetry`; clear progress state on success |
| Tests | `backend/domain/knowledge/service/ragimpl/document_test.go` | Update existing 2 RetryDocument tests for new signature; add a `last_task_id`-updated assertion; add a "legacy stub returns error" test in `knowledge_test.go` or wherever the legacy tests live |
| Frontend tests | `frontend/.../upload-progress-poll/index.test.tsx` (if exists) | Test the enabled-button path |

## 5. Components

### 5.1 `MappingRepo.UpdateLastTaskID`

```go
func (m *MappingRepo) UpdateLastTaskID(ctx context.Context, cozeDocID int64, taskID string) error {
    return m.db.WithContext(ctx).Exec(
        `UPDATE rag_doc_mapping SET last_task_id = ? WHERE coze_doc_id = ? AND deleted_at IS NULL`,
        taskID, cozeDocID,
    ).Error
}
```

No row-count check: if the row doesn't exist, the call is a no-op rather than an error, matching the soft-delete / restore pattern. Caller has already verified the row via `DocByCozeID` upstream.

### 5.2 Ragimpl `RetryDocument` (new signature)

```go
func (i *Impl) RetryDocument(ctx context.Context, req *service.RetryDocumentRequest) (*service.RetryDocumentResponse, error) {
    tenant, err := i.tenant(ctx)
    if err != nil {
        return nil, err
    }
    dm, err := i.mapping.DocByCozeID(ctx, req.DocumentID)
    if err != nil {
        return nil, err
    }
    kb, err := i.mapping.KBByCozeID(ctx, dm.KBID)
    if err != nil {
        return nil, err
    }
    ragResp, err := i.rag.RetryDocument(ctx, tenant, kb.RagKBID, dm.RagDocID)
    if err != nil {
        return nil, err
    }
    if err := i.mapping.UpdateLastTaskID(ctx, req.DocumentID, ragResp.TaskID); err != nil {
        // Mapping update failed but rag already accepted the retry. Log + return
        // the response anyway; UI will poll the new task once the mapping catches
        // up on a subsequent retry, OR show stale "failed" until the user retries
        // again. Failing the whole call would suggest to the user that retry
        // didn't trigger, which is worse.
        logs.CtxWarnf(ctx, "ragimpl: RetryDocument: UpdateLastTaskID after retry %s failed: %v", ragResp.TaskID, err)
    }
    nowMs := time.Now().UnixMilli()
    refreshed := &entity.Document{
        Info: knowledgeModel.Info{
            ID:          dm.CozeID,
            CreatorID:   dm.CreatorID,
            UpdatedAtMs: nowMs,
        },
        KnowledgeID: dm.KBID,
        Status:      RagStatusToEntity(ragResp.Status),
    }
    return &service.RetryDocumentResponse{Document: refreshed}, nil
}
```

Entity is built from mapping + rag response. No second `GetDocument` round trip — the retry response carries the only fields needed for the immediate UI refresh (`Status`); name/size/etc. live on `dm` already if needed but spec keeps the response minimal.

### 5.3 Legacy stub

Plan resolves whether `errno.ErrKnowledgeFeatureNotSupportedInLegacyCode` exists. If not:
- Reuse `ErrRagFeaturePendingCode` (already used in ragimpl/unsupported.go) — symmetric inversion: "rag feature unavailable in legacy" mirrors "legacy feature unavailable in rag."
- Or add a new errno (small change, follows existing pattern in `types/errno/knowledge.go`).

Spec recommendation: reuse `ErrRagFeaturePendingCode` with a clear msg until a separate "feature gated by backend mode" errno is needed across more than one method.

### 5.4 Application handler

```go
func (k *KnowledgeApplicationService) RetryDocument(ctx context.Context, req *dataset.RetryDocumentRequest) (*dataset.RetryDocumentResponse, error) {
    svcReq := &service.RetryDocumentRequest{DocumentID: req.DocumentID}
    svcResp, err := k.DomainSVC.RetryDocument(ctx, svcReq)
    if err != nil {
        return nil, err
    }
    resp := &dataset.RetryDocumentResponse{
        DocumentInfo: convertDocumentEntityToInfo(svcResp.Document),
    }
    return resp, nil
}
```

`convertDocumentEntityToInfo` already exists in `application/knowledge/convertor.go` (used by `ListDocument` and `DatasetDetail`). Reuse it; do not duplicate.

### 5.5 Frontend `handleRetry`

```tsx
const handleRetry = async (docId: string) => {
  try {
    await KnowledgeApi.RetryDocument({ document_id: docId });
    // Clear progress for this doc so the next tick re-polls from the new
    // task; the mapping's last_task_id was bumped server-side.
    setProgress(prev => {
      const next = { ...prev };
      delete next[docId];
      return next;
    });
    completedRef.current = false; // re-arm onComplete in case it had fired
  } catch (err) {
    // Surface failure visibly — user just clicked, deserves feedback.
    Toast.error(I18n.t('datasets_retry_failed', '重试失败，请稍后再试'));
  }
};
```

Button placement and label changes:
```tsx
{failed ? (
  <div className={styles.error}>
    <span className={styles['error-text']}>
      {p?.status_descript ?? I18n.t('datasets_upload_failed', '上传失败')}
    </span>
    <Button onClick={() => handleRetry(id)}>
      {I18n.t('datasets_retry_button', '重试')}
    </Button>
  </div>
) : null}
```

## 6. Data flow & invariants

- **`last_task_id` is bumped to the retry's task on the SAME mapping row.** No second mapping row is inserted; retry is "do the same coze doc again with a fresh rag task."
- **The retried doc keeps the same `coze_doc_id`.** No new ID is allocated. The frontend's polling loop continues to reference the same id.
- **Mapping update is best-effort on the failure path** (logged + ignored). Rag's retry has already started; failing the whole API call would mislead the user into thinking the retry didn't trigger.
- **Frontend re-polling is triggered by clearing local state**, not by directly calling the progress endpoint. The existing polling effect picks up the cleared state on its next tick and the server's updated mapping returns the new task's status.

## 7. Error handling

| Scenario | Behavior |
|---|---|
| `mapping.DocByCozeID` returns `ErrMappingNotFound` | Propagated → application layer → HTTP 4xx with mapping error message. |
| `mapping.KBByCozeID` returns `ErrMappingNotFound` | Same. |
| `rag.RetryDocument` 404 (rag-side doc doesn't exist) | R2-C `DecodeErrorEnvelope` + `MapRagError` → `ErrKnowledgeDocumentNotExistCode`. |
| `rag.RetryDocument` 422 (e.g. retry on already-succeeded doc) | `ErrKnowledgeInvalidParamCode` with formatted rag message. |
| `UpdateLastTaskID` DB error | Logged WARN; retry response still succeeds. UI will poll the old (failed) task → user may need to refresh or retry again. Acceptable for the failure window. |
| Legacy mode invocation | `ErrRagFeaturePendingCode` (or fitter errno per plan); frontend shows toast. Should not actually trigger since the button only renders in rag-mode wizards. |
| Frontend network error / 5xx | Toast "重试失败"; user can click again. |

## 8. Testing

### 8.1 Ragimpl unit tests

Rewrite the two existing R2-D-backend tests for the new signature:

- **`TestRagimpl_RetryDocument`** — happy path. Inserts KB mapping + doc mapping (with `last_task_id = "task-old-1"`). fakeClient.retryDocumentFunc returns `task_id = "task-retry-9"`. Assert:
  - Response has `Document` non-nil with correct `ID`, `Status` (from rag's `pending`/`processing`).
  - **Mapping row's `last_task_id` was updated to `"task-retry-9"`** — query via `DocByCozeID` after the call.
  - rag client was called with `tenant="test-tenant"`, `kb="rag-kb-X"`, `doc="rag-doc-Y"`.

- **`TestRagimpl_RetryDocument_MissingDocMapping`** — coze doc 999 has no mapping → returns `ErrMappingNotFound`; rag client NOT called.

New test:

- **`TestRagimpl_RetryDocument_MappingUpdateFailureIsBestEffort`** — fakeClient.retryDocumentFunc returns success; force `UpdateLastTaskID` to fail (via a hook on the MappingRepo or by closing the DB connection mid-test). Assert: response is still successful, `last_task_id` is the OLD value, but a warning log was emitted. (Implementation may approximate this; the exact mocking shape is a plan-time decision.)

### 8.2 Application-layer test

The application layer's `RetryDocument` is thin (req conversion → DomainSVC call → resp conversion). One test through `application/knowledge` using a mocked `service.Knowledge` would suffice; plan judges whether this layer is worth a dedicated test or whether the ragimpl + IDL roundtrip tests are enough.

### 8.3 Legacy stub test

A test in `backend/domain/knowledge/service/knowledge_test.go` that calls `knowledgeSVC.RetryDocument` and asserts the returned error is `ErrRagFeaturePendingCode` (or the chosen errno).

### 8.4 Frontend test

`upload-progress-poll/index.test.tsx` likely already exists (R2-A/B test surface). Update the existing "shows disabled 联系管理员 button" test to instead:
- Render with a failed doc.
- Assert the retry button is NOT disabled.
- Click it; mock `KnowledgeApi.RetryDocument`; assert it was called with the correct docId.
- Assert local progress state for that doc was cleared.

### 8.5 Smoke

End-to-end UI smoke. Bring up stacks per project memory item #2. Upload a doc that will fail (e.g. by temporarily breaking rag's model_providers config and re-uploading, OR by uploading a corrupted-file-type that the parser rejects). Watch `<UploadProgressPoll />` show the retry button. Click → verify Network panel shows `POST /api/knowledge/document/retry` with 200, then the progress poll resumes against the new task.

## 9. Compatibility & rollout

- No schema migration (`last_task_id` column already exists).
- No external API contract break — the new `/api/knowledge/document/retry` endpoint is additive.
- Legacy backend path: stub returns "feature not supported"; UI won't trigger the button anyway because the rag-mode wizard is the only mount site.
- Frontend: the button toggles from `disabled` to enabled. No visual layout change.
- IDL regen: needs the `make idl` (or equivalent) toolchain available. Plan's Task 0 confirms the command.

## 10. Open questions

Two minor items deferred to plan-time:

1. **`ErrKnowledgeFeatureNotSupportedInLegacyCode` errno value** — does it exist, should one be added, or should we reuse `ErrRagFeaturePendingCode`? Spec recommends the reuse; plan confirms via grep.
2. **Exact frontend test surface** — `upload-progress-poll/index.test.tsx` exists in spec-time grep but its current shape may have its own retry-button test patterns. Plan reads the test file and decides whether to update an existing test or add a new one.

Two minor items deferred to R2-D-fe-Wizard (NOT this slice):

3. **Retry-with-modified-parameters** — once the wizard exposes parameter forms, "retry with new chunk_size" becomes possible. Out of scope here; the current retry simply re-runs the same ingestion config.
4. **Retry button visual polish** — loading spinner, confirmation modal, etc. Defer to wizard rework.
