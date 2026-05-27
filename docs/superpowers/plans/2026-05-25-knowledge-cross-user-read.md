# Knowledge cross-user read access — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let every logged-in user read every KB (list / detail / docs / chunks / retrieval) while keeping writes owner-only, so workflows copied via `/api/workflow_api/copy_wk_template` resolve their KB references in both the canvas editor and at runtime.

**Architecture:** Pure application-layer change in `backend/application/knowledge/knowledge.go` — split `checkPermission` into `checkReadAccess` + `checkWriteAccess`, drop the `space_id` filter in `ListKnowledge`, populate the already-existing `Dataset.can_edit` thrift field with `creator_id == uid`. Frontend uses `canEdit` to gate edit UI and renders a read-only banner; KB list/picker default to `ScopeAll`. No schema migration. Domain-layer retrieve and `publishWorkflowResource` already do no permission checks — verified during design.

**Tech Stack:** Go (Hertz), Thrift, React + TypeScript, Vitest, Semi Design, Tailwind.

**Spec:** `docs/superpowers/specs/2026-05-25-knowledge-cross-user-read-design.md`

**Pre-existing constraints:**
- `go test` in the backend may hit a sonic/loader linker error (see memory `ragimpl-go-test-sonic-loader-fail`). Each backend task lists `go test` as preferred and `go vet ./... && go build ./...` + curl smoke as fallback.
- i18n strings must land in zh-CN and **en.json** (not en-US.json); see memory `coze-rag-params-i18n-namespace`.
- Dataset.can_edit already exists in the IDL (`idl/data/knowledge/common.thrift:168`) and generated Go bindings — no IDL change needed.
- `DatasetScopeType.ScopeAll` / `ScopeSelf` already exist in the IDL; reuse via `DatasetFilter.scope_type`.

---

## File map

**Backend (Go):**
- `backend/application/knowledge/knowledge.go` — predicate split, all call-site migrations, list / detail field population
- `backend/application/knowledge/permission_test.go` — NEW; unit tests for the two predicates (skipped if linker error)

**Frontend (TypeScript/React):**
- `frontend/packages/data/knowledge/common/services/src/...` or wherever the dataset list/detail API caller lives — pass `scope_type` through; surface `canEdit` from response
- `frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/...` — scope tabs, per-row gating, author byline
- `frontend/packages/data/knowledge/knowledge-ide-base/src/...` — detail page read-only mode + banner (text / table / image variants)
- `frontend/packages/data/knowledge/knowledge-ide-base/src/features/text-knowledge-workspace/components/text-toolbar.tsx` and siblings — hide write controls when `!canEdit`
- workflow KB picker (workflow knowledge-retrieve node) — global scope + author sort
- `frontend/apps/coze-studio/src/locales/zh-CN.json` + `frontend/apps/coze-studio/src/locales/en.json` — new strings

**Spec / plan:**
- `docs/superpowers/specs/2026-05-25-knowledge-cross-user-read-design.md` (already exists)
- This file

---

## Phase 1 — Backend predicate split

### Task 1: Add the two new predicates (TDD)

**Files:**
- Modify: `backend/application/knowledge/knowledge.go` around line 823 (where `checkPermission` lives)
- Create: `backend/application/knowledge/permission_test.go`

- [ ] **Step 1.1: Write a failing unit test**

Create `backend/application/knowledge/permission_test.go`:

```go
package knowledge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

func TestCheckReadAccess_LoggedIn(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	uid := int64(42)
	err := svc.checkReadAccess(context.Background(), &uid)
	assert.NoError(t, err)
}

func TestCheckReadAccess_Unauthenticated(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	err := svc.checkReadAccess(context.Background(), nil)
	assert.Error(t, err)
	statusErr, ok := errorx.FromStatusError(err)
	assert.True(t, ok)
	assert.Equal(t, int32(errno.ErrKnowledgePermissionCode), statusErr.Code())
}
```

- [ ] **Step 1.2: Run the test (preferred path) and confirm it fails**

```
cd backend
go test ./application/knowledge/ -run TestCheckReadAccess -v
```

Expected: FAIL with `undefined: (*KnowledgeApplicationService).checkReadAccess`.

If the run fails with a sonic/loader linker error rather than the undefined-method error, switch to the fallback: `go vet ./application/knowledge/...` and continue without running tests (Task 7 covers smoke testing).

- [ ] **Step 1.3: Implement `checkReadAccess`**

Add immediately above the existing `checkPermission` function (~line 823) in `knowledge.go`:

```go
// checkReadAccess allows any authenticated user. Use for read endpoints
// (list, detail, list docs, list slices, retrieve). Knowledge data is
// globally readable by design; only writes are owner-gated.
func (k *KnowledgeApplicationService) checkReadAccess(ctx context.Context, uid *int64) error {
	if uid == nil {
		return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	return nil
}
```

- [ ] **Step 1.4: Re-run the test and confirm it passes**

```
cd backend
go test ./application/knowledge/ -run TestCheckReadAccess -v
```

Expected: both `TestCheckReadAccess_LoggedIn` and `TestCheckReadAccess_Unauthenticated` PASS.

(Fallback path: `go build ./application/knowledge/...` should succeed without errors.)

- [ ] **Step 1.5: Write a failing test for `checkWriteAccess`**

Append to `backend/application/knowledge/permission_test.go`:

```go
func TestCheckWriteAccess_Unauthenticated(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	err := svc.checkWriteAccess(context.Background(), nil, nil, nil, nil)
	assert.Error(t, err)
}

// Owner / non-owner cases need a mocked DomainSVC; gated behind build tag in
// follow-up if needed. For now we only verify the unauthenticated guard.
```

- [ ] **Step 1.6: Run the test and confirm it fails**

```
cd backend
go test ./application/knowledge/ -run TestCheckWriteAccess -v
```

Expected: FAIL with `undefined: (*KnowledgeApplicationService).checkWriteAccess`.

- [ ] **Step 1.7: Implement `checkWriteAccess`**

Add immediately below `checkReadAccess`:

```go
// checkWriteAccess resolves the target identifier(s) back to the owning KB(s)
// and passes only when every owning KB.creator_id == *uid.
//
// Pass exactly one of: knowledgeID, documentIDs, sliceIDs. (Multiple are
// allowed but the union is checked.)
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

	// Resolve target → KB set. Batched queries; no N+1.
	kbIDSet := map[int64]struct{}{}
	if knowledgeID != nil {
		kbIDSet[*knowledgeID] = struct{}{}
	}
	if len(documentIDs) > 0 {
		docResp, err := k.DomainSVC.MGetDocuments(ctx, &service.MGetDocumentsRequest{DocumentIDs: documentIDs})
		if err != nil {
			return err
		}
		if len(docResp.Documents) != len(documentIDs) {
			return errorx.New(errno.ErrKnowledgeNotFound, errorx.KV("msg", "document not found"))
		}
		for _, d := range docResp.Documents {
			kbIDSet[d.KnowledgeID] = struct{}{}
		}
	}
	if len(sliceIDs) > 0 {
		sliceResp, err := k.DomainSVC.MGetSlices(ctx, &service.MGetSlicesRequest{IDs: sliceIDs})
		if err != nil {
			return err
		}
		if len(sliceResp.Slices) != len(sliceIDs) {
			return errorx.New(errno.ErrKnowledgeNotFound, errorx.KV("msg", "slice not found"))
		}
		for _, s := range sliceResp.Slices {
			kbIDSet[s.KnowledgeID] = struct{}{}
		}
	}

	if len(kbIDSet) == 0 {
		// Defensive: caller passed no targets at all. Treat as misuse → deny.
		return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "no write target"))
	}

	kbIDs := make([]int64, 0, len(kbIDSet))
	for id := range kbIDSet {
		kbIDs = append(kbIDs, id)
	}

	kbResp, err := k.DomainSVC.ListKnowledge(ctx, &service.ListKnowledgeRequest{IDs: kbIDs})
	if err != nil {
		return err
	}
	if len(kbResp.KnowledgeList) != len(kbIDs) {
		return errorx.New(errno.ErrKnowledgeNotFound, errorx.KV("msg", "knowledge not found"))
	}
	for _, kb := range kbResp.KnowledgeList {
		if kb.CreatorID != *uid {
			return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "not knowledge owner"))
		}
	}
	return nil
}
```

Note: the exact `DomainSVC` method names (`MGetDocuments`, `MGetSlices`) may differ from what's already in the codebase. If grep does not find them, search `backend/domain/knowledge/service/` for methods that batch-fetch documents by ID and slices by ID; use whichever signature already exists. The point is one batched query per shape.

- [ ] **Step 1.8: Run the unauth test and confirm it passes; build**

```
cd backend
go test ./application/knowledge/ -run TestCheckWriteAccess -v
go build ./application/knowledge/...
```

Expected: test PASS; build success.

- [ ] **Step 1.9: Commit**

```bash
git add backend/application/knowledge/knowledge.go backend/application/knowledge/permission_test.go
git commit -m "feat(knowledge): add checkReadAccess/checkWriteAccess predicates"
```

---

### Task 2: Migrate every `checkPermission` call site

**Files:**
- Modify: `backend/application/knowledge/knowledge.go` (every line listed below)

The complete list of `checkPermission` call sites and their target predicate:

| Line | Function | Migrate to |
|---|---|---|
| 405 | `CreateKnowledge` | **delete the check entirely** — creator becomes owner; only the `if uid == nil` guard above it stays |
| 449 | `datasetDetail` | `checkReadAccess(ctx, uid)` |
| 522 | (write — likely UpdateKnowledge) | `checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)` *(use the dataset id field present at the call site)* |
| 572 | (write) | `checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)` |
| 596 | (write) | `checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)` |
| 775 | (doc-level write) | `checkWriteAccess(ctx, uid, nil, transformedIDs, nil)` |
| 804 | (doc-level write) | `checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)` |
| 891 | `GetDocumentProgress` | `checkReadAccess(ctx, uid)` |
| 932 | (doc read/write — inspect surrounding handler name) | `checkReadAccess` if a read endpoint (list/get) ; `checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)` if it mutates |
| 1053 | (slice write) | `checkWriteAccess(ctx, uid, nil, nil, sliceIDs)` |
| 1187 | (dataset-scoped) | inspect; if read → `checkReadAccess`; if write → `checkWriteAccess(ctx, uid, ptr.Of(...), nil, nil)` |
| 1237 | (doc-scoped) | same rule |
| 1315 | (doc-scoped) | same rule |
| 1349 | (doc-scoped) | same rule |
| 1387 | (dataset-scoped) | same rule |
| 1428 | (dataset-scoped) | same rule |
| 1462 | (dataset-scoped) | same rule |
| 1554 | (doc-scoped) | same rule |
| 1619 | (dataset-scoped) | same rule |
| 1711 | (dataset-scoped) | same rule |
| 1758 | (doc-scoped) | same rule |
| 1791 | (dataset-scoped) | same rule |
| 1831 | (dataset-scoped) | same rule |
| 1861 | (dataset-scoped) | same rule |
| 1890 | (doc-scoped, bulk) | same rule |
| 1933 | (dataset-scoped) | same rule |
| 2002 | (dataset-scoped) | same rule |

Classification rule for the "inspect" rows: look at the enclosing function name. If it's `Get*`, `List*`, `MGet*`, `*Detail`, `*Preview`, `Retrieve*`, or `Validate*Schema` — it's read. If it's `Create*`, `Update*`, `Delete*`, `Retry*`, `Resegment`, `*Caption` (write), `Save*`, `Set*` — it's write.

Classification reference: spec §"Backend changes — Handler reclassification".

- [ ] **Step 2.1: Open `backend/application/knowledge/knowledge.go` and migrate every site listed above**

For each row in the table:
1. Locate the call by line number.
2. Determine the right predicate using the table + classification rule.
3. Replace.

Example for line 449 (`datasetDetail` — read):

```go
// before
err = k.checkPermission(ctx, uid, ptr.Of(req.SpaceID), nil, nil, nil)
if err != nil {
    return dataset.NewDatasetDetailResponse(), err
}

// after
err = k.checkReadAccess(ctx, uid)
if err != nil {
    return dataset.NewDatasetDetailResponse(), err
}
```

Example for line 522 (write — UpdateDataset region):

```go
// before
err = k.checkPermission(ctx, uid, ptr.Of(req.SpaceID), nil, nil, nil)

// after
err = k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
```

Example for line 405 (`CreateKnowledge` — drop entirely):

```go
// before
err := k.checkPermission(ctx, uid, ptr.Of(req.SpaceID), nil, nil, nil)
if err != nil {
    return dataset.NewCreateDatasetResponse(), err
}

// after  (deleted; uid != nil guard above this line is sufficient)
```

- [ ] **Step 2.2: Delete the old `checkPermission` function**

Delete the entire `checkPermission` function body (currently around lines 823–875). This forces the compiler to catch any missed call site.

- [ ] **Step 2.3: Verify build**

```
cd backend
go build ./application/knowledge/...
```

Expected: build success. Any `undefined: checkPermission` failure means a call site was missed — go back to step 2.1.

- [ ] **Step 2.4: Verify imports are still tidy**

```
cd backend
goimports -l ./application/knowledge/knowledge.go
```

Expected: no output (file is already correctly imported). If `permission` import is now unused, remove it.

- [ ] **Step 2.5: Run all knowledge package tests / vet**

```
cd backend
go test ./application/knowledge/... -v
```

If linker error, fallback:

```
cd backend
go vet ./application/knowledge/...
```

Expected: all green.

- [ ] **Step 2.6: Commit**

```bash
git add backend/application/knowledge/knowledge.go
git commit -m "refactor(knowledge): migrate checkPermission sites to read/write predicates"
```

---

### Task 3: ListKnowledge — drop space filter when scope=All

**Files:**
- Modify: `backend/application/knowledge/knowledge.go` (function `buildListKnowledgeRequest` near line 301, and its caller `ListKnowledge`)

Goal: when the request's `filter.scope_type == ScopeAll` (the default), do **not** filter by `space_id`. When `scope_type == ScopeSelf`, additionally filter by `CreatorID == uid` server-side.

- [ ] **Step 3.1: Locate the caller `ListKnowledge` (around line 1791) and see how it builds the service request**

Read the surrounding code — find where `buildListKnowledgeRequest` is invoked and what `scope_type` value is available.

- [ ] **Step 3.2: Modify `buildListKnowledgeRequest` to accept scope + uid**

Change the signature and body. Existing signature looks like:

```go
func (k *KnowledgeApplicationService) buildListKnowledgeRequest(ctx context.Context, spaceID int64, name *string, formatType *dataset.FormatType, page, pageSize int, projectIDStr string) (*service.ListKnowledgeRequest, error) {
    ...
    if spaceID != 0 {
        request.SpaceID = &spaceID
    }
    ...
}
```

Replace with:

```go
func (k *KnowledgeApplicationService) buildListKnowledgeRequest(
    ctx context.Context,
    spaceID int64,
    name *string,
    formatType *dataset.FormatType,
    page, pageSize int,
    projectIDStr string,
    scope knowledgeCommon.DatasetScopeType,
    uid int64,
) (*service.ListKnowledgeRequest, error) {
    // ...existing body for name, format, page, etc. unchanged...

    // Scope logic: ScopeAll (default) returns every KB regardless of space.
    // ScopeSelf restricts to KBs the caller created.
    switch scope {
    case knowledgeCommon.DatasetScopeType_ScopeSelf:
        request.CreatorID = &uid
    default: // ScopeAll, zero value
        // no space_id filter; no creator filter
    }
    _ = spaceID // keep parameter for backward compat / project scope below
    return request, nil
}
```

If `service.ListKnowledgeRequest` does not yet have a `CreatorID` field, add it. Search:

```
grep -n "type ListKnowledgeRequest" backend/domain/knowledge/service/*.go
```

Add the field:

```go
type ListKnowledgeRequest struct {
    // ...existing fields...
    CreatorID *int64
}
```

And in the DAL layer, when `CreatorID != nil`, add `AND creator_id = ?` to the WHERE clause. Search `backend/domain/knowledge/internal/dal/` for the existing list query to find where to add this clause.

- [ ] **Step 3.3: Update the call site in `ListKnowledge`**

Find where `buildListKnowledgeRequest` is called inside `ListKnowledge`. Read `req.Filter.GetScopeType()` (default zero = `ScopeAll`) and pass it plus `*uid`:

```go
scope := req.Filter.GetScopeType() // zero value = ScopeAll
listReq, err := k.buildListKnowledgeRequest(
    ctx, req.SpaceID, /* name */ name, /* formatType */ formatType,
    int(req.GetPage()), int(req.GetSize()), req.GetProjectID(),
    scope, *uid,
)
```

- [ ] **Step 3.4: Build**

```
cd backend
go build ./application/knowledge/... ./domain/knowledge/...
```

Expected: success.

- [ ] **Step 3.5: Commit**

```bash
git add backend/application/knowledge/knowledge.go backend/domain/knowledge/service/ backend/domain/knowledge/internal/dal/
git commit -m "feat(knowledge): drop space filter for scope=all; honor scope=mine via creator_id"
```

---

### Task 4: Populate `Dataset.can_edit` from `creator_id`

**Files:**
- Modify: `backend/application/knowledge/knowledge.go` — the converter that builds `dataset.Dataset` from the domain entity

- [ ] **Step 4.1: Find the converter `batchConvertKnowledgeEntity2Model`**

```
grep -n "batchConvertKnowledgeEntity2Model\|convertKnowledgeEntity2Model" backend/application/knowledge/*.go
```

This function (or its singular sibling) is where `dataset.Dataset` is built. The result is used by both `DatasetDetail` and `ListKnowledge`.

- [ ] **Step 4.2: Pass `uid` into the converter**

Change its signature to accept `uid int64`. Update all callers (likely 2: `datasetDetail` and `ListKnowledge`).

- [ ] **Step 4.3: Set `CanEdit`**

Inside the per-entity loop:

```go
m.CanEdit = (entity.CreatorID == uid)
```

(Field name might be `IsOwner` if you prefer — but `CanEdit` is already on the Dataset thrift struct and unused, so we just populate it. This avoids any IDL change.)

- [ ] **Step 4.4: Build and run any existing knowledge tests**

```
cd backend
go build ./...
go test ./application/knowledge/... -v
```

(Fallback to `go vet ./application/knowledge/...` if linker error.)

Expected: success.

- [ ] **Step 4.5: Commit**

```bash
git add backend/application/knowledge/knowledge.go
git commit -m "feat(knowledge): populate Dataset.can_edit from creator_id == uid"
```

---

### Task 5: Smoke test the backend changes via curl

**Files:** none — this is verification only.

- [ ] **Step 5.1: Start the dev stack**

```
make middleware
make server
```

Wait until `coze-server` logs `server is running on :8888`.

- [ ] **Step 5.2: Create two users in the admin panel (or via API)**

If users already exist in the dev DB, skip and grab two distinct user sessions. Save their session cookies as `$COOKIE_A` (Alice — KB owner) and `$COOKIE_B` (Bob — non-owner).

- [ ] **Step 5.3: As Alice, create a KB and upload one doc**

Easiest via UI at http://localhost:8888 (login as Alice). Note the KB `dataset_id` for the next steps; call it `$KB_ID`.

- [ ] **Step 5.4: As Bob, hit `ListDataset` and confirm Alice's KB appears**

```
curl -s -X POST http://localhost:8888/api/knowledge/dataset/list \
  -H "Cookie: $COOKIE_B" \
  -H "Content-Type: application/json" \
  -d '{"page":1,"size":50,"filter":{"scope_type":1}}' | jq '.dataset_list[] | {dataset_id, name, creator_id, can_edit}'
```

Expected: Alice's KB appears in the list with `can_edit: false`.

- [ ] **Step 5.5: As Bob, fetch detail of Alice's KB → 200 + `can_edit:false`**

```
curl -s -X POST http://localhost:8888/api/knowledge/dataset/detail \
  -H "Cookie: $COOKIE_B" \
  -H "Content-Type: application/json" \
  -d "{\"dataset_ids\":[\"$KB_ID\"]}" | jq '.dataset_details'
```

Expected: full Dataset object returned, `can_edit: false`.

- [ ] **Step 5.6: As Bob, attempt to delete Alice's KB → 403 / ErrKnowledgePermissionCode**

```
curl -s -X POST http://localhost:8888/api/knowledge/dataset/delete \
  -H "Cookie: $COOKIE_B" \
  -H "Content-Type: application/json" \
  -d "{\"dataset_id\":\"$KB_ID\"}" | jq '.'
```

Expected: response shows `code: <ErrKnowledgePermissionCode>` and `msg` includes `"not knowledge owner"`.

- [ ] **Step 5.7: As Bob, list documents in Alice's KB → 200**

```
curl -s -X POST http://localhost:8888/api/knowledge/document/list \
  -H "Cookie: $COOKIE_B" \
  -H "Content-Type: application/json" \
  -d "{\"dataset_id\":\"$KB_ID\",\"page\":1,\"size\":20}" | jq '.documents | length'
```

Expected: returns the doc count Alice uploaded (≥1).

- [ ] **Step 5.8: As Bob, list slices in Alice's first doc → 200**

(Take a document id from step 5.7 and call the slice list endpoint.)

Expected: slices returned.

- [ ] **Step 5.9: As Alice, perform the same write that failed for Bob → 200**

```
curl -s -X POST http://localhost:8888/api/knowledge/dataset/update \
  -H "Cookie: $COOKIE_A" \
  -H "Content-Type: application/json" \
  -d "{\"dataset_id\":\"$KB_ID\",\"name\":\"renamed-by-alice\"}" | jq '.code'
```

Expected: `0` (success).

- [ ] **Step 5.10: No commit needed for smoke** — but if you fixed anything during smoke, commit those fixes.

---

## Phase 2 — Frontend

### Task 6: Surface `canEdit` from the API and gate the per-row action menu

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/...` (the list rendering — find the file that maps each Dataset to a row + action menu)

- [ ] **Step 6.1: Confirm `canEdit` is already on the generated TS Dataset type**

```
grep -rn "can_edit\|canEdit" frontend/packages/arch/idl-spec/ | head -5
grep -rn "canEdit" frontend/packages/data/knowledge/common/ | head -10
```

If the field is missing from the TS type, regenerate the IDL bindings — but since the thrift already had `can_edit` since the original schema, the type should already exist. Confirm.

- [ ] **Step 6.2: Locate the row's action menu (delete / rename / settings buttons)**

```
grep -rn "delete\|rename\|settings" frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/ | grep -i "icon\|button\|menu" | head -10
```

Find the JSX component that renders the per-KB action icons.

- [ ] **Step 6.3: Add a `canEdit` guard around write actions**

```tsx
{dataset.canEdit && (
  <>
    <RenameButton ... />
    <DeleteButton ... />
    <SettingsButton ... />
  </>
)}
```

The exact button names depend on the actual JSX; gate every action that mutates KB state.

- [ ] **Step 6.4: Render an author byline + "Mine" badge in the row**

Below the KB name, add:

```tsx
<div className="kb-row-byline">
  <UserBadge userId={dataset.creatorId} />
  {dataset.canEdit && <Tag color="blue">{t('knowledge.list.mine')}</Tag>}
</div>
```

The `UserBadge` / equivalent component already exists in the codebase — grep `creator_id` in this package for usage examples.

- [ ] **Step 6.5: Start the dev server and verify in browser**

```
cd frontend/apps/coze-studio
npm run dev
```

Log in as Bob, open the KB list — confirm Alice's KB appears, the action icons are hidden, the author byline shows Alice. Log in as Alice — confirm her own KBs still show all actions.

- [ ] **Step 6.6: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-modal-base/
git commit -m "feat(knowledge-ui): gate KB row actions by canEdit + show author byline"
```

---

### Task 7: Default the list to `ScopeAll` and add the `全部 / 我创建的` tab

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/use-knowledge-filter/index.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/...` (the tab/dropdown component near the search bar)

- [ ] **Step 7.1: Inspect `use-knowledge-filter`**

```
cat frontend/packages/data/knowledge/knowledge-modal-base/src/knowledge-list-modal/use-knowledge-filter/index.tsx
```

Find where `scope_type` (or the equivalent enum) is currently set in the request payload. Today it likely defaults to `ScopeSelf`.

- [ ] **Step 7.2: Default to `ScopeAll`**

Change the default value of the scope state from `ScopeSelf` to `ScopeAll`.

- [ ] **Step 7.3: Add the tab UI**

In the list-modal header, add a `Tabs` or `Radio.Group` with two values:

```tsx
<Tabs activeKey={scope} onChange={setScope}>
  <TabPane tab={t('knowledge.list.scope_all')} itemKey="all" />
  <TabPane tab={t('knowledge.list.scope_mine')} itemKey="mine" />
</Tabs>
```

Map `'all' → ScopeAll`, `'mine' → ScopeSelf` when sending the request.

- [ ] **Step 7.4: Verify in browser**

Same dev server. Log in as Bob — confirm KB list shows everyone's KBs by default; switching to `我创建的` filters to Bob's own (which may be empty).

- [ ] **Step 7.5: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-modal-base/
git commit -m "feat(knowledge-ui): default KB list to scope=all + add 我创建的 tab"
```

---

### Task 8: KB detail page — read-only mode + banner

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-ide-base/src/features/text-knowledge-workspace/components/text-toolbar.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-ide-base/src/features/text-knowledge-workspace/components/level-content.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-ide-base/src/features/text-knowledge-workspace/components/base-content.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-ide-base/src/features/table-knowledge-workspace/...` (table variant)
- Modify: `frontend/packages/data/knowledge/knowledge-ide-base/src/features/image-knowledge-workspace/index.tsx` (image variant)
- Modify: `frontend/packages/data/knowledge/common/stores/src/knowledge-preview.ts` (or wherever the detail data is stored — surface `canEdit`)

- [ ] **Step 8.1: Surface `canEdit` from the KB detail store**

```
cat frontend/packages/data/knowledge/common/stores/src/knowledge-preview.ts
```

Find where the response from `DatasetDetail` is mapped into the store. Add `canEdit: boolean` to the store shape and map it from the response.

- [ ] **Step 8.2: Add a `useCanEditKnowledge` hook**

Create or extend a hook in `common/hooks/` that returns `canEdit` from the store. Example:

```ts
export const useCanEditKnowledge = (): boolean => {
  const dataset = useKnowledgePreviewStore(s => s.dataset);
  return !!dataset?.canEdit;
};
```

Existing hook files: search `frontend/packages/data/knowledge/common/hooks/src/` for a sibling `use-*.ts` to keep style consistent.

- [ ] **Step 8.3: Render a read-only banner when `!canEdit`**

In `knowledge-ide-base`'s root layout component (or the closest equivalent that wraps all three KB types), add at the top:

```tsx
{!canEdit && (
  <Banner type="info" closeIcon={false}>
    {t('knowledge.detail.readonly_banner', { creator: dataset.creatorName })}
  </Banner>
)}
```

- [ ] **Step 8.4: Hide write controls in `text-toolbar.tsx`**

Find the toolbar buttons (upload doc, batch upload, resegment, etc.) and wrap each with `canEdit &&`:

```tsx
{canEdit && <UploadDocButton ... />}
{canEdit && <ResegmentButton ... />}
```

Apply the same pattern in `level-content.tsx`, `base-content.tsx`, and `doc-selector.tsx` for any delete/edit per-row icons.

- [ ] **Step 8.5: Same in table workspace**

Open `features/table-knowledge-workspace/` and gate all write buttons (insert row, delete row, batch import, schema edit, etc.) the same way.

- [ ] **Step 8.6: Same in image workspace**

Open `features/image-knowledge-workspace/index.tsx` and gate upload / caption edit / delete buttons.

- [ ] **Step 8.7: Verify in browser**

Reload as Bob. Navigate into Alice's KB (text type). Confirm:
- Banner is visible at top
- No upload, no resegment, no delete icons
- Document list still browsable; clicking a doc shows chunks; chunks are readable but not editable

Repeat the check on a table KB and an image KB if available.

- [ ] **Step 8.8: Commit**

```bash
git add frontend/packages/data/knowledge/
git commit -m "feat(knowledge-ui): read-only KB detail mode driven by canEdit"
```

---

### Task 9: Workflow KB picker — global scope + author sort

**Files:**
- Find via grep, modify the picker component:

```
grep -rn "knowledge.*picker\|datasetSelector\|KnowledgeSelector" frontend/packages/workflow/ | head -10
```

- [ ] **Step 9.1: Locate the picker that opens when adding a KB node**

Read the file. Confirm it calls the same `ListDataset` API used by the list modal.

- [ ] **Step 9.2: Force `scope=all` in the picker's request**

The picker should always show all KBs, not just the user's own. If the picker re-uses `useKnowledgeFilter`, make sure scope defaults to `ScopeAll` there too.

- [ ] **Step 9.3: Sort own KBs first**

After the list arrives, sort:

```ts
const sorted = [...kbs].sort((a, b) => {
  if (a.canEdit === b.canEdit) {
    return b.updateTime - a.updateTime;
  }
  return a.canEdit ? -1 : 1;
});
```

- [ ] **Step 9.4: Show author byline per option**

In the picker option row, render `<UserBadge userId={kb.creatorId} />` next to the KB name (consistent with Task 6).

- [ ] **Step 9.5: Verify in browser**

Log in as Bob. Open a workflow, add a knowledge-retrieve node, open the KB picker. Confirm: Bob's own KBs at top, then Alice's, each with byline.

- [ ] **Step 9.6: Commit**

```bash
git add frontend/packages/workflow/
git commit -m "feat(workflow-ui): KB picker shows all KBs sorted with author byline"
```

---

### Task 10: i18n strings

**Files:**
- Modify: `frontend/apps/coze-studio/src/locales/zh-CN.json`
- Modify: `frontend/apps/coze-studio/src/locales/en.json`

(Note: `en.json` not `en-US.json` — see memory `coze-rag-params-i18n-namespace`.)

- [ ] **Step 10.1: Add the new keys to `zh-CN.json`**

```jsonc
"knowledge_list_scope_all": "全部",
"knowledge_list_scope_mine": "我创建的",
"knowledge_list_mine_badge": "我的",
"knowledge_detail_readonly_banner": "你正在查看 @{{creator}} 创建的知识库，无修改权限。",
"knowledge_creator_byline": "由 @{{name}} 创建"
```

- [ ] **Step 10.2: Add the same keys to `en.json`**

```jsonc
"knowledge_list_scope_all": "All",
"knowledge_list_scope_mine": "Created by me",
"knowledge_list_mine_badge": "Mine",
"knowledge_detail_readonly_banner": "You are viewing a knowledge base created by @{{creator}}. Read-only.",
"knowledge_creator_byline": "Created by @{{name}}"
```

- [ ] **Step 10.3: Replace the literal English / Chinese strings in Tasks 6–9 with `t('knowledge_list_scope_all')` etc.**

Grep for the hard-coded strings and swap them.

- [ ] **Step 10.4: Verify both locales render correctly**

Switch the dev server's UI language (settings or via URL param) and confirm both zh-CN and en render the right strings.

- [ ] **Step 10.5: Commit**

```bash
git add frontend/apps/coze-studio/src/locales/ frontend/packages/data/knowledge/ frontend/packages/workflow/
git commit -m "i18n(knowledge): add zh-CN + en strings for cross-user read UI"
```

---

## Phase 3 — End-to-end verification

### Task 11: `copy_wk_template` end-to-end smoke

**Files:** none — this is a verification task.

- [ ] **Step 11.1: As Alice, create a workflow that uses a KB**

Log in as Alice. Build a minimal workflow: Start → Knowledge Retrieve (pointing at Alice's `$KB_ID`) → End. Publish a draft version.

- [ ] **Step 11.2: As Bob, copy Alice's workflow via the template API**

Find Alice's workflow ID (`$WID_A`). With Bob's session cookie, call:

```
curl -s -X POST http://localhost:8888/api/workflow_api/copy_wk_template \
  -H "Cookie: $COOKIE_B" \
  -H "Content-Type: application/json" \
  -d "{\"workflow_ids\":[\"$WID_A\"],\"target_space_id\":\"<bob_space_id>\"}" | jq '.'
```

Expected: response shows a new workflow id `$WID_B` and code 0.

- [ ] **Step 11.3: As Bob, open the copied workflow in the canvas**

Navigate to the workflow editor for `$WID_B` while logged in as Bob.

Expected:
- The canvas opens without 403 errors in the network tab
- The Knowledge Retrieve node shows Alice's KB name (not "knowledge not found")
- The KB info banner inside the node shows the right title / chunk count

- [ ] **Step 11.4: As Bob, run the copied workflow**

Click `试运行` / Test Run. Provide an input query.

Expected: the workflow completes, the KB retrieve node returns chunks from Alice's KB, end node receives them.

- [ ] **Step 11.5: As Bob, attempt to edit Alice's KB from inside the workflow editor**

Click into the KB from the node's settings → land on KB detail page.

Expected: read-only banner shown, no edit/upload/delete buttons.

- [ ] **Step 11.6: Document the smoke result**

Append a short note to the spec file (`docs/superpowers/specs/2026-05-25-knowledge-cross-user-read-design.md`) under a new `## Smoke results` section: pass/fail per step, date, who ran it.

- [ ] **Step 11.7: Commit the smoke note**

```bash
git add docs/superpowers/specs/2026-05-25-knowledge-cross-user-read-design.md
git commit -m "docs(spec): record cross-user KB read e2e smoke result"
```

---

## Done

After Task 11 passes:
- `feat/replace-knowledge-base` branch holds the full change set (11 commits)
- The fork's KB permission model is global-read / creator-write
- `/api/workflow_api/copy_wk_template` results are immediately usable by the copier
- No DB migration, no rag changes, no IDL changes

Roll back by `git revert` of the 11 commits if anything regresses.
