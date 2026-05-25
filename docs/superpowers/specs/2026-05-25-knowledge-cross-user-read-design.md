# Knowledge base cross-user read access + workflow-copy compatibility

**Status:** Design — pending approval
**Date:** 2026-05-25
**Scope:** verteklab/coze-studio (this fork only)

## Problem

Today the knowledge base (KB) permission model gates every read and write on workspace membership (`backend/application/knowledge/knowledge.go:823` `checkPermission`). Because each workspace in this deployment has exactly one member (the owner), a user can only see and use their own KBs.

This blocks two things:

1. Users want to browse other users' KBs — list, document list, document content, chunks — but must not be able to modify them.
2. When a user copies someone else's workflow via `POST /api/workflow_api/copy_wk_template`, the copied workflow still references the original KB IDs. Today that workflow fails at runtime because the copier cannot read those KBs.

## Goals

- Any logged-in user can read any KB: list, detail, documents, chunks, retrieval.
- Only the KB creator can modify: create / update / delete docs, edit / delete chunks, change KB settings, delete KB.
- `copy_wk_template` continues to preserve original KB IDs in node schema; the copied workflow runs because the new owner now has read access.

## Non-goals

- Per-KB private/public visibility (every KB becomes globally readable; no opt-out).
- Workspace-member collaboration on writes (the deployment is one user per workspace; not relevant).
- Cloning KB entities on workflow copy (rejected during brainstorm — reference is enough).
- Changes to the rag service, vector index, ES index, or KB schema.

## Approach

Split the single `checkPermission` predicate into two:

- `checkReadAccess(ctx, uid)` — passes for any non-nil `uid`.
- `checkWriteAccess(ctx, uid, knowledgeID?, documentIDs?, sliceIDs?)` — resolves the target back to the owning KB(s) and passes only when every KB's `creator_id` equals `uid`.

Each KB-related handler is reclassified as read or write and migrated to the matching predicate. The KB list API drops its hard `space_id` filter and returns all KBs, annotated with `is_owner`. The frontend uses `is_owner` to gate edit UI; otherwise non-owners see read-only detail.

No changes to KB DB schema, vector store, or rag.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│ Frontend (apps/coze-studio + packages/data/knowledge/…) │
│  • KB list page: default scope = "all"                   │
│  • KB detail page: read-only mode driven by is_owner     │
│  • KB picker (workflow node / agent): same global list   │
└─────────────────────────┬────────────────────────────────┘
                          │ HTTP / IDL
┌─────────────────────────▼────────────────────────────────┐
│ backend/api/handler/coze/knowledge_service.go            │
│  (route layer dispatches to read vs write app methods)   │
└─────────────────────────┬────────────────────────────────┘
                          ▼
┌──────────────────────────────────────────────────────────┐
│ backend/application/knowledge/knowledge.go               │
│  • checkReadAccess(uid)             → logged-in → ok     │
│  • checkWriteAccess(uid, kbID…)     → creator_id match   │
│  • listKnowledge(): drop space filter; attach is_owner   │
└─────────────────────────┬────────────────────────────────┘
                          ▼
┌──────────────────────────────────────────────────────────┐
│ backend/domain/knowledge/service/                        │
│  • Business + DAL unchanged; KB schema unchanged         │
└──────────────────────────────────────────────────────────┘
```

## Backend changes

### 1. Predicate split

In `backend/application/knowledge/knowledge.go`:

```go
// Any logged-in user passes. Used by all read endpoints.
func (k *KnowledgeApplicationService) checkReadAccess(ctx context.Context, uid *int64) error {
    if uid == nil {
        return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
    }
    return nil
}

// Owner-only. Resolves target → owning KB(s) → compares creator_id.
// Pass any one of: knowledgeID / documentIDs / sliceIDs.
func (k *KnowledgeApplicationService) checkWriteAccess(
    ctx context.Context,
    uid *int64,
    knowledgeID *int64,
    documentIDs []int64,
    sliceIDs []int64,
) error {
    if uid == nil {
        return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
    }
    // 1. Resolve target → KB set (single batched query per shape):
    //    knowledgeID: direct
    //    documentIDs: SELECT knowledge_id FROM knowledge_document WHERE id IN (?)
    //    sliceIDs:    slice → document → kb (two batched joins)
    // 2. SELECT id, creator_id FROM knowledge WHERE id IN (?)
    // 3. All creator_id == *uid → ok; otherwise ErrKnowledgePermissionCode "not knowledge owner".
    //    If any KB missing → ErrKnowledgeNotFound.
    ...
}
```

The original `checkPermission` is deleted. Every call site migrates to one of the two.

### 2. Handler reclassification

Handlers live in `backend/api/handler/coze/knowledge_service.go`; they delegate to application methods. Each application method is migrated:

| Type | Application methods |
|---|---|
| **Read** → `checkReadAccess` | `DatasetDetail`, `ListDataset`, `ListDocument`, `GetDocumentProgress`, `ListPhoto`, `PhotoDetail`, `ListSlice`, `GetTableSchema`, `GetDocumentTableInfo`, `GetModeConfig`, `MGetDocumentReview`, `GetIconForDataset`, plus the `*OpenAPI` mirrors of the same set |
| **Write** → `checkWriteAccess` | `UpdateDataset`, `DeleteDataset`, `CreateDocument`, `UpdateDocument`, `DeleteDocument`, `RetryDocument`, `Resegment`, `UpdatePhotoCaption`, `ExtractPhotoCaption`, `ValidateTableSchema`, `CreateSlice`, `UpdateSlice`, `DeleteSlice`, `CreateDocumentReview`, `SaveDocumentReview` |
| **Create** (logged-in only; ownership established by the act) | `CreateDataset` — application sets `creator_id = uid` on insert |

### 3. List API

`buildListKnowledgeRequest` in `backend/application/knowledge/knowledge.go:301`:

- Default scope = `all`. The service-layer `SpaceID` filter is left nil so the DAL does not restrict by space.
- Accept an optional `scope` parameter: `all` (default) or `mine`. `mine` filters server-side to `creator_id == uid`.
- Existing `name` / `formatType` / pagination filters are preserved.

Response (per KB entry): existing fields **plus** a new boolean `is_owner` derived in the application layer (`creator_id == uid`). `DatasetDetail` adds the same field.

### 4. Knowledge retrieval runtime — no change needed

Verified during design: the workflow engine calls retrieval through `crossknowledge.DefaultSVC().Retrieve(ctx, req)` (see `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go:319` and `backend/domain/workflow/internal/nodes/llm/llm.go:1204`). This goes straight to the **domain** layer (`backend/domain/knowledge/service/retrieve.go:53`), which performs **no permission check**. `checkPermission` lives only in the application layer and only on user-facing handlers.

Likewise, `publishWorkflowResource` (`backend/application/workflow/workflow.go:3740`) delegates to `domain.Publish` + an async search-index update — it does not validate KB ownership.

**Consequence**: KB retrieval at workflow runtime already works across users today. The only failure mode is at editor-open / FE metadata fetch time, when the canvas loads node info and FE calls `DatasetDetail` / `ListDocument`. Those go through `checkPermission` and fail. Fixing the application-layer predicate is sufficient.

### 5. Unchanged

- KB DB schema (no migration).
- Vector store, ES index, rag service.
- `permission` framework itself (kept available for future use; KB code stops calling it).
- `CopyKnowledge` / `MoveKnowledgeToLibrary` (orthogonal to `copy_wk_template`).
- Workspace `permission` mechanism (single-member workspaces make it a no-op for write).

## Workflow-copy compatibility (`/api/workflow_api/copy_wk_template`)

Current chain (`backend/application/workflow/workflow.go:3625` `CopyWkTemplateApi` → `:1202 copyWorkflow` → `domain.CopyWorkflow`):

```
CopyWkTemplateApi(workflowIds, targetSpaceID)
 ├ checkUserSpace(uid, targetSpaceID)        ← workflow-side space check
 ├ for each workflowID:
 │   copyWorkflow(workflowID, policy)
 │    └ domain.CopyWorkflow                  ← copies workflow record + canvas
 │                                              KB IDs in dataset_param preserved
 │   publishWorkflowResource(v0.0.0)
 │   …populate inputs / outputs
```

This path does **not** call `appknowledge.CopyKnowledge` (that runs only on app→library moves). KB IDs survive verbatim.

Behavior after the permission change:

| Phase | Before | After |
|---|---|---|
| Copy itself | ✓ (no KB perm check) | ✓ unchanged |
| Publish v0.0.0 | ✓ | ✓ unchanged |
| Copier opens workflow; FE fetches KB metadata | ✗ `DatasetDetail` blocked by workspace check | ✓ `checkReadAccess` passes; read-only detail UI |
| Copier runs workflow; KB retrieve node fires | ✗ retrieve blocked by workspace check | ✓ `checkReadAccess` passes; returns chunks |

What this implies for implementation tasks:

1. `DatasetDetail`, `ListDocument`, `ListSlice`, `ListPhoto` must all be on `checkReadAccess` — FE pulls these when opening a copied workflow.
2. Domain retrieve and `publishWorkflowResource` already skip KB perm checks (verified during design); no changes needed there.

What does **not** change:

- `CopyWkTemplateApi` itself (already preserves references; that's the desired semantic).
- KB clone / move paths.

## Frontend changes

### KB list page (`packages/data/knowledge/knowledge-modal-base/` and consumers)

- Reuse existing `DatasetScopeType`: default `ScopeAll`; request carries `scope=all`.
- Tab / dropdown at top: `全部 / 我创建的`. `我创建的` sets `scope=mine`.
- Each KB row shows author identifier; the "owned by me" rows get an inline badge.
- Top-level entry buttons (`新建知识库` etc.) remain visible to all.
- Per-row action buttons (delete / rename / settings) render only when `is_owner === true`.

### KB detail page

When `is_owner === false`:

- Hide: upload document, delete document, resegment, edit chunk, delete chunk, KB settings, delete KB.
- Keep: document list browsing, document metadata, chunk preview, retrieval test (read-only operation).
- Render a top banner: `你正在查看 @<creator> 创建的知识库，无修改权限。`

### KB picker (workflow knowledge-retrieve node / agent KB binding)

- Uses the same global list API.
- Each option shows author identifier.
- Sort: my KBs first (descending by `updated_at`), others after (descending by `updated_at`).

### Workflow copy

No FE changes. Copy button visibility already reflects "visible = readable"; the copy itself preserves references — once backend grants read access, the result just works.

### i18n

New strings (zh-CN + en, per fork convention — see [[coze-rag-params-i18n-namespace]]):

- Read-only banner template
- Tab labels: `全部 / 我创建的`
- Author byline: `由 @{name} 创建`

## Data flow

```
Scenario 1: Bob browses Alice's KB
  Bob → GET /list_dataset?scope=all
    ↳ checkReadAccess(Bob) ✓ → returns […{id, creator_id, is_owner=false}]
  Bob → GET /dataset_detail?id=K
    ↳ checkReadAccess(Bob) ✓ → full fields + is_owner=false
  Bob → DELETE /document (attempts)
    ↳ checkWriteAccess(Bob, docID) → KB.creator_id=Alice != Bob
    ↳ 403 ErrKnowledgePermissionCode "not knowledge owner"

Scenario 2: Bob copies Alice's workflow (references KB K)
  Bob → POST /api/workflow_api/copy_wk_template
    ↳ workflow layer copies schema; dataset_param=[K] preserved
    ↳ publish v0.0.0
  Bob → run workflow, hits KB retrieve node
    ↳ knowledge.Retrieve(K) → checkReadAccess(Bob) ✓ → chunks returned

Scenario 3: Alice deletes K; Bob's workflow later hits K
  Alice → DELETE /dataset K ✓ (creator-only allows)
  Bob's run → Retrieve(K) → not-found (pre-existing behavior; no new cleanup)
```

## Error handling

| Situation | Behavior | Error code |
|---|---|---|
| Unauthenticated request to any KB endpoint | 401 | `ErrKnowledgePermissionCode` `"session required"` |
| Non-owner calls write endpoint | 403 | `ErrKnowledgePermissionCode` `"not knowledge owner"` |
| Write request spans multiple KBs, one of which is not owned | 403, whole batch rejected | same; no partial success |
| `checkWriteAccess` cannot resolve KB | 404 | `ErrKnowledgeNotFound` (pre-existing) |
| Bob's workflow hits a KB Alice has deleted | retrieval not-found surfaces as workflow node error | pre-existing |

## Testing

| Layer | Cases |
|---|---|
| Unit (Go) | `checkReadAccess`: unauthenticated → fail; logged-in → pass. `checkWriteAccess`: owner → pass; non-owner → fail; mixed batch → fail; KB missing → not-found. |
| Integration (Go) | A creates KB; B's token calls `DatasetDetail` / `ListDataset` / `ListDocument` / `ListSlice` / retrieve → all 200. B calls `UpdateDataset` / `DeleteDataset` / `CreateDocument` → 403. |
| Workflow integration | A creates workflow that references A's KB. B calls `copy_wk_template`. B runs the copy. Retrieval returns A's chunks. |
| Frontend | `is_owner=false` detail page: upload / delete / settings buttons not rendered; banner shown. `is_owner=true` page: full controls. |
| Regression | Existing KB CRUD tests still pass. rag is unchanged; no new tests there. |

## Rollback

- No schema migration. Pure code change.
- `git revert` of the diff is sufficient to restore prior behavior.
- No feature flag. The behavior change is the desired end state; old behavior under single-member workspaces was indistinguishable from the new behavior for owner-only flows.

## Risks

1. **Owner-resolution DB cost.** Write endpoints add one batched `SELECT creator_id` per call. Write QPS is low; acceptable. A KB→creator LRU could be added later if hot — not now.
2. **OpenAPI / admin-bearer endpoints.** The fork's admin-bearer routes have their own middleware and do not go through `checkPermission`; the predicate split does not affect them. Verified during research.
3. **Future "private KB" need.** Out of scope. If introduced later, add a `visibility` column and extend `checkReadAccess`. We do not pre-stub this.
4. **Missed handler in reclassification.** Mitigated by deleting `checkPermission` entirely — any unmigrated call site fails to compile.

## Deferred follow-ups (post-implementation)

These were surfaced during execution and are NOT covered by this branch:

1. **ragimpl `ListKnowledge` ignores `UserID` / `SpaceID` filters.** `backend/domain/knowledge/service/ragimpl/knowledge.go:315` already has a NOTE about this. Consequence: on rag-backed deployments, the FE "我创建的" (ScopeSelf) tab returns the tenant-wide list instead of filtering — visually equivalent to "全部". Fix is a small change to the ragimpl side. Not a correctness bug for the primary copy-workflow use case.
2. **Workspace library page cross-user view.** `backend/application/search/resource_search.go:127` returns `ErrSearchPermissionCode` on the first non-owner row, blocking cross-user browse on the workspace `library_resource_list` surface. The KB list modal and workflow KB picker (the primary surfaces) use `ListDataset` and are unaffected.
3. **`Dataset.CreatorName` / `avatar_url` not populated by backend.** `batchConvertKnowledgeEntity2Model` populates `creator_id` but not the display name/avatar. FE handles gracefully (`creator_name ? ... : null`), so the "@<creator>" banner template and per-row author byline render as a blank slot. Backfill is a small follow-up — needs a join to `user` (or equivalent) at convert time.
4. **Backend curl smoke (plan Task 5).** Deferred to user — needs two distinct logged-in user sessions. Plan §"Task 5" has the exact curl commands.
5. **End-to-end `copy_wk_template` smoke (plan Task 11).** Deferred to user — needs Alice + Bob accounts, an Alice-created workflow with a KB-retrieve node, and a copy operation as Bob. Plan §"Task 11" has the step-by-step.
