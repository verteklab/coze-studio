# Coze UI ↔ rag flow alignment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation)
**Goal:** Gate the document-upload wizard per-KB so rag-backed KBs use a 2-step `[upload, progress]` flow that never calls `CreateDocumentReview` / `ExtractPhotoCaption` / table-schema stubs.

**Architecture:** New optional `backend` field on the thrift `Dataset` struct, populated by the application layer from `rag_kb_mapping` presence. Frontend reads `kb.backend` in `scenes/base/config.ts` (the central wizard config registry) and routes to `*AddRagConfig` instead of `*AddUpdateConfig`. Legacy wizards untouched; PR-2 collapses by deleting three `add/` directories.

**Tech Stack:** thrift IDL, Go (Hertz handlers + GORM), TypeScript (React + Vitest, Rush monorepo).

---

## Pre-flight: discovery to lock open questions

### Task 0: Confirm codegen mechanics and gating call site

**Why up front:** Three things in the spec depend on local repo mechanics. Resolve before writing tests so later tasks don't churn.

**Files:**
- Read: `idl/data/knowledge/common.thrift:155-170`
- Read: `frontend/packages/arch/idl/src/auto-generated/knowledge/namespaces/dataset.ts`
- Read: `frontend/packages/data/knowledge/knowledge-resource-processor-adapter/src/scenes/base/config.ts`

- [ ] **Step 1: Find the thrift→Go regen command.** Look in `Makefile`, `scripts/`, `tools/`, repo README. Document the exact invocation. Common patterns: `make idl`, `bash scripts/gen-idl.sh`, `make gen-bindings`.

- [ ] **Step 2: Find the thrift→TS regen command.** The auto-generated file exists at `frontend/packages/arch/idl/src/auto-generated/knowledge/namespaces/dataset.ts`, so a pipeline exists. Look in the same places + `frontend/packages/arch/idl/`. Document the invocation.

- [ ] **Step 3: Trace `getConfigV2()` callers in `scenes/base/config.ts`.** Grep `rg "getConfigV2" frontend --type ts --type tsx`. Note each caller's surrounding context: do they have a `kb` (or its backend value) in scope at config-resolution time?

- [ ] **Step 4: Decide gating shape based on Step 3.** Two options:
  - **A:** Pass `kb` (or `backend: string | undefined`) into `getConfigV2(backend)` — every caller threads it through.
  - **B:** Keep `getConfigV2()` signature stable; the 3 affected entries become `(kb) => Config` thunks the caller invokes after lookup.
  - Pick the one that requires fewer call-site changes. Document the choice; subsequent tasks reference it as "the chosen gating shape."

- [ ] **Step 5: Confirm `RetryDocument` is unimplemented in ragimpl.** Run: `rg "func.*RetryDocument" backend/domain/knowledge/service/ragimpl/` — empty result confirms the spec's assumption (retry UI shows the "联系管理员" stub).

- [ ] **Step 6: No commit yet** — discovery only. Findings will be referenced in Task 1's commit message.

---

## Phase A: Backend — `backend` field plumbing

### Task 1: Add `backend` field to thrift `Dataset` struct

**Files:**
- Modify: `idl/data/knowledge/common.thrift:155-170` (add field 13)
- Regenerated (do not hand-edit): `backend/api/model/...` Go binding for `Dataset`
- Regenerated (do not hand-edit): `frontend/packages/arch/idl/src/auto-generated/knowledge/namespaces/dataset.ts`

- [ ] **Step 1: Edit the thrift struct.** Add the field as the 13th member (highest unused tag). Match existing comment style.

```thrift
struct Dataset {
    1:  i64 dataset_id(agw.js_conv="str", api.js_conv="true")
    2:  string        name
    // ... existing fields 3-12 ...
    12: bool          can_edit

    // Backend that owns this KB's knowledge data. "rag" = managed by the
    // standalone rag service via ragimpl; "legacy" = managed by coze's
    // in-tree knowledge module. Derived from rag_kb_mapping presence;
    // absent on responses from older servers (clients treat as "legacy").
    13: optional string backend
}
```

- [ ] **Step 2: Run the Go regen command from Task 0 Step 1.** Confirm `backend/api/model/...` Dataset struct gains a `Backend *string` field (or `Backend string` — depends on Go binding for optional).

- [ ] **Step 3: Run the TS regen command from Task 0 Step 2.** Confirm `dataset.ts` Dataset interface gains `backend?: string`.

- [ ] **Step 4: Compile-check the backend.** Run: `GOTOOLCHAIN=go1.24.0 go build ./...` from `backend/`. Expected: clean build (no consumer yet uses the field; adding an optional field is non-breaking).

- [ ] **Step 5: Commit.**

```bash
git add idl/data/knowledge/common.thrift \
        backend/api/model \
        frontend/packages/arch/idl/src/auto-generated/knowledge/namespaces/dataset.ts
git commit -m "feat(idl): add Dataset.backend field for rag/legacy gating"
```

---

### Task 2: Locate and inventory every Dataset producer in the application layer

**Why:** The `backend` field must be populated everywhere a `Dataset` is constructed, or the frontend will see mixed `undefined` (treated as legacy) and `"rag"` for the same KB depending on which endpoint loaded it.

**Files:** Read-only discovery.

- [ ] **Step 1: Grep for Dataset constructions.** Run:

```bash
rg "dataset\.Dataset\{|&dataset\.Dataset\{|common\.Dataset\{" backend --type go
```

(Adjust import alias if your codebase uses a different one — confirm in `application/knowledge/`.)

- [ ] **Step 2: For each hit, record:** file:line, the function it lives in, and whether it has access to the dataset_id. Save to scratch notes; you'll need this list in Task 4.

- [ ] **Step 3: Verify the central producer is `application/knowledge/knowledge.go`.** That's the path the spec assumes. If most hits come from `application/knowledge/` files, fine. If they're scattered across `domain/knowledge/service/` etc., flag in the implementation report — may need a shared helper to avoid copy-paste.

- [ ] **Step 4: No commit** — discovery only.

---

### Task 3: Add `ragKBMappingDAO.Exists(ctx, datasetID) (bool, error)` helper

**Files:**
- Locate: `backend/domain/knowledge/service/ragimpl/mapping.go` (the file that already does `record not found` lookups against `rag_kb_mapping` — seen in 2026-05-13 server logs)
- Create test: `backend/domain/knowledge/service/ragimpl/mapping_test.go`

- [ ] **Step 1: Read `mapping.go`.** Locate the existing mapping repository or DAO type. The smoke logs show queries like:

```sql
SELECT coze_kb_id, rag_kb_id, ... FROM rag_kb_mapping WHERE coze_kb_id = ?
```

So there's already a lookup function. We need an `Exists`-style helper that returns `(bool, error)` without surfacing `record not found` as an error.

- [ ] **Step 2: Write the failing test.** Use the project's existing DB-test pattern (look at any nearby `_test.go` for setup).

```go
func TestRagKBMappingDAO_Exists(t *testing.T) {
    ctx := context.Background()
    dao := newTestRagKBMappingDAO(t)

    // Unmapped KB → false.
    exists, err := dao.Exists(ctx, 9999999)
    require.NoError(t, err)
    require.False(t, exists)

    // Mapped KB → true.
    require.NoError(t, dao.Insert(ctx, &RagKBMapping{CozeKBID: 12345, RagKBID: "rag-abc"}))
    exists, err = dao.Exists(ctx, 12345)
    require.NoError(t, err)
    require.True(t, exists)
}
```

- [ ] **Step 3: Run the test, confirm it fails.** Expected: `Exists` method undefined.

```bash
cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/ -run TestRagKBMappingDAO_Exists -v
```

- [ ] **Step 4: Implement `Exists`.** Use GORM's existing pattern in the file. Roughly:

```go
func (d *ragKBMappingDAO) Exists(ctx context.Context, cozeKBID int64) (bool, error) {
    var count int64
    err := d.db.WithContext(ctx).
        Model(&RagKBMapping{}).
        Where("coze_kb_id = ? AND deleted_at IS NULL", cozeKBID).
        Count(&count).Error
    if err != nil {
        return false, err
    }
    return count > 0, nil
}
```

- [ ] **Step 5: Run the test, confirm it passes.** Same command as Step 3.

- [ ] **Step 6: Commit.**

```bash
git add backend/domain/knowledge/service/ragimpl/mapping.go \
        backend/domain/knowledge/service/ragimpl/mapping_test.go
git commit -m "feat(ragimpl): add RagKBMapping.Exists for fast backend-type checks"
```

---

### Task 4: Populate `Backend` field in every Dataset producer

**Files:**
- Modify: each file from Task 2's inventory
- Modify (likely): `backend/application/knowledge/knowledge.go` (DatasetDetail + DatasetDetailOpenAPI)
- Modify (likely): wherever `ListDataset` builds Dataset entries

- [ ] **Step 1: Write a failing application-layer test.** Pick the most central producer (likely `DatasetDetail`). Pattern:

```go
func TestDatasetDetail_PopulatesBackend(t *testing.T) {
    fakeDAO := &fakeRagKBMappingDAO{
        existsResults: map[int64]bool{12345: true, 67890: false},
    }
    svc := newTestKnowledgeService(t, withRagMappingDAO(fakeDAO))

    // rag-mapped KB.
    resp, err := svc.DatasetDetail(ctx, &DatasetDetailRequest{DatasetIDs: []int64{12345}})
    require.NoError(t, err)
    require.Equal(t, "rag", deref(resp.DatasetDetails["12345"].Backend))

    // Unmapped KB → legacy.
    resp, err = svc.DatasetDetail(ctx, &DatasetDetailRequest{DatasetIDs: []int64{67890}})
    require.NoError(t, err)
    require.Equal(t, "legacy", deref(resp.DatasetDetails["67890"].Backend))
}
```

- [ ] **Step 2: Run test, confirm it fails.** Expected: Backend field is nil/empty.

- [ ] **Step 3: Add a helper at `application/knowledge/backend_field.go`** to avoid copy-paste across producers:

```go
// resolveBackend returns "rag" if the dataset has a row in rag_kb_mapping,
// "legacy" otherwise. Used to populate Dataset.Backend on outgoing DTOs so
// the frontend can pick the right upload wizard per-KB.
func resolveBackend(ctx context.Context, dao RagKBMappingDAO, datasetID int64) (*string, error) {
    isRag, err := dao.Exists(ctx, datasetID)
    if err != nil {
        return nil, err
    }
    v := "legacy"
    if isRag {
        v = "rag"
    }
    return &v, nil
}

// resolveBackendBatch returns a map[datasetID]backend in one round-trip,
// suitable for list endpoints. Implementation: single query that returns
// all rag-mapped IDs in the input set, then derive the rest as "legacy".
func resolveBackendBatch(ctx context.Context, dao RagKBMappingDAO, datasetIDs []int64) (map[int64]*string, error) {
    ragIDs, err := dao.ExistsBatch(ctx, datasetIDs)  // returns set of mapped IDs
    if err != nil {
        return nil, err
    }
    out := make(map[int64]*string, len(datasetIDs))
    for _, id := range datasetIDs {
        v := "legacy"
        if _, ok := ragIDs[id]; ok {
            v = "rag"
        }
        out[id] = &v
    }
    return out, nil
}
```

- [ ] **Step 4: Add `ExistsBatch` to the DAO.** Extend the test from Task 3 to cover it.

```go
func (d *ragKBMappingDAO) ExistsBatch(ctx context.Context, cozeKBIDs []int64) (map[int64]struct{}, error) {
    if len(cozeKBIDs) == 0 {
        return map[int64]struct{}{}, nil
    }
    var rows []struct{ CozeKBID int64 }
    err := d.db.WithContext(ctx).
        Model(&RagKBMapping{}).
        Select("coze_kb_id").
        Where("coze_kb_id IN ? AND deleted_at IS NULL", cozeKBIDs).
        Find(&rows).Error
    if err != nil {
        return nil, err
    }
    out := make(map[int64]struct{}, len(rows))
    for _, r := range rows {
        out[r.CozeKBID] = struct{}{}
    }
    return out, nil
}
```

- [ ] **Step 5: Wire each Dataset producer.** For each file in Task 2's inventory, find the spot where `Dataset` is constructed and add `Backend: backendFor(datasetID)` (call the helper). Single-KB producers use `resolveBackend`; multi-KB producers use `resolveBackendBatch` once and look up per-ID.

- [ ] **Step 6: Run the application test from Step 1, confirm passes.**

- [ ] **Step 7: Run the full backend test suite to catch unintended breakage.**

```bash
GOTOOLCHAIN=go1.24.0 go test ./application/knowledge/... ./domain/knowledge/service/ragimpl/... ./infra/contract/rag/... ./infra/rag/... -v
```

Expected: all pass.

- [ ] **Step 8: Commit.**

```bash
git add backend/application/knowledge/ \
        backend/domain/knowledge/service/ragimpl/
git commit -m "feat(knowledge): populate Dataset.backend on all outgoing DTOs"
```

---

## Phase B: Frontend — shared components

### Task 5: Add `isRagBackend(kb)` helper

**Files:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-core/src/utils/is-rag-backend.ts`
- Create test: `frontend/packages/data/knowledge/knowledge-resource-processor-core/src/utils/__tests__/is-rag-backend.test.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-core/src/utils/index.ts` (re-export)

- [ ] **Step 1: Write the failing test.**

```typescript
import { describe, it, expect } from 'vitest';
import { isRagBackend } from '../is-rag-backend';

describe('isRagBackend', () => {
  it('returns true for backend="rag"', () => {
    expect(isRagBackend({ backend: 'rag' } as any)).toBe(true);
  });
  it('returns false for backend="legacy"', () => {
    expect(isRagBackend({ backend: 'legacy' } as any)).toBe(false);
  });
  it('returns false for backend undefined (safe legacy fallback)', () => {
    expect(isRagBackend({} as any)).toBe(false);
  });
  it('returns false for backend null', () => {
    expect(isRagBackend({ backend: null } as any)).toBe(false);
  });
  it('returns false for unknown backend string', () => {
    expect(isRagBackend({ backend: 'mystery' } as any)).toBe(false);
  });
});
```

- [ ] **Step 2: Run test, confirm fails.**

```bash
cd frontend && rush test --to @coze-data/knowledge-resource-processor-core
```

(Or the package-local `npm run test` if Rush isn't set up — check the package.json.)

Expected: module-not-found.

- [ ] **Step 3: Implement the helper.**

```typescript
import type { Dataset } from '@coze-arch/idl/knowledge';

/**
 * Returns true iff the KB is served by the standalone rag service. Any other
 * value (legacy, undefined, unknown string) falls back to legacy semantics.
 * The frontend uses this to route to the rag-mode upload wizard. See
 * docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md.
 */
export const isRagBackend = (kb: Pick<Dataset, 'backend'> | undefined | null): boolean =>
  kb?.backend === 'rag';
```

- [ ] **Step 4: Re-export from package barrel.** Add to `src/utils/index.ts`:

```typescript
export { isRagBackend } from './is-rag-backend';
```

- [ ] **Step 5: Run tests, confirm pass.** Same command as Step 2.

- [ ] **Step 6: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-core/
git commit -m "feat(knowledge): add isRagBackend helper"
```

---

### Task 6: Build `<UploadProgressPoll />` shared component

**Files:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/components/upload-progress-poll/index.tsx`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/components/upload-progress-poll/index.module.less`
- Create test: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/components/upload-progress-poll/__tests__/index.test.tsx`

- [ ] **Step 1: Write the failing test for single-doc happy path.**

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { UploadProgressPoll } from '../index';

const mockGetProgress = vi.fn();
vi.mock('@coze-arch/bot-api', () => ({
  KnowledgeApi: { GetDocumentProgress: (...args: any[]) => mockGetProgress(...args) },
}));

describe('<UploadProgressPoll />', () => {
  it('polls until ready then calls onComplete', async () => {
    mockGetProgress
      .mockResolvedValueOnce({ data: [{ document_id: 'd1', status: 1, progress: 30 }] })
      .mockResolvedValueOnce({ data: [{ document_id: 'd1', status: 1, progress: 70 }] })
      .mockResolvedValueOnce({ data: [{ document_id: 'd1', status: 9, progress: 100 }] });

    const onComplete = vi.fn();
    render(<UploadProgressPoll docIds={['d1']} onComplete={onComplete} pollIntervalMs={10} />);

    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce(), { timeout: 1000 });
    expect(screen.getByText(/1\/1/)).toBeInTheDocument();
  });

  it('shows error + retry CTA when a doc fails', async () => {
    mockGetProgress.mockResolvedValueOnce({
      data: [{ document_id: 'd1', status: 0, progress: 0, status_descript: 'rate limit' }],
    });

    render(<UploadProgressPoll docIds={['d1']} onComplete={vi.fn()} pollIntervalMs={10} />);

    await waitFor(() => expect(screen.getByText(/rate limit/)).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /联系管理员|重试/ })).toBeInTheDocument();
  });

  it('aggregates "N/M ready" across multiple docs', async () => {
    mockGetProgress.mockResolvedValue({
      data: [
        { document_id: 'd1', status: 9, progress: 100 },
        { document_id: 'd2', status: 1, progress: 50 },
        { document_id: 'd3', status: 1, progress: 20 },
      ],
    });

    render(<UploadProgressPoll docIds={['d1', 'd2', 'd3']} onComplete={vi.fn()} pollIntervalMs={10} />);

    await waitFor(() => expect(screen.getByText(/1\/3/)).toBeInTheDocument());
  });
});
```

Note on status codes: `status=9` (ready), `status=0` (failed), `status=1` (processing) — confirm against the existing `KnowledgeApi.GetDocumentProgress` response type in `@coze-arch/idl/knowledge`. The numbers in the tests are illustrative; update them to match the real enum if different.

- [ ] **Step 2: Run tests, confirm fail.** Module-not-found.

- [ ] **Step 3: Implement the component.**

```tsx
import { useEffect, useRef, useState } from 'react';
import { KnowledgeApi } from '@coze-arch/bot-api';
import { Button, Progress } from '@coze-arch/coze-design';
import { I18n } from '@coze-arch/i18n';
import styles from './index.module.less';

const DEFAULT_POLL_MS = 2000;

// Mirror the rag status codes coze normalizes to. Verify these match the
// generated enum at the call site before merging.
const READY = 9;
const FAILED = 0;

interface DocProgress {
  document_id: string;
  status: number;
  progress: number;
  status_descript?: string;
}

export interface UploadProgressPollProps {
  docIds: string[];
  onComplete: () => void;
  pollIntervalMs?: number;
}

export const UploadProgressPoll = ({
  docIds,
  onComplete,
  pollIntervalMs = DEFAULT_POLL_MS,
}: UploadProgressPollProps) => {
  const [progress, setProgress] = useState<Record<string, DocProgress>>({});
  const completedRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      try {
        const resp = await KnowledgeApi.GetDocumentProgress({ document_ids: docIds });
        if (cancelled) return;
        const next: Record<string, DocProgress> = {};
        for (const row of resp.data ?? []) {
          next[row.document_id] = row;
        }
        setProgress(next);

        const allReady = docIds.every(id => next[id]?.status === READY);
        if (allReady && !completedRef.current) {
          completedRef.current = true;
          onComplete();
          return;  // stop polling
        }
      } catch {
        // transient failure — next tick will retry
      }
      if (!cancelled && !completedRef.current) {
        setTimeout(tick, pollIntervalMs);
      }
    };
    tick();
    return () => { cancelled = true; };
  }, [docIds, onComplete, pollIntervalMs]);

  const readyCount = docIds.filter(id => progress[id]?.status === READY).length;
  const failedDocs = docIds.filter(id => progress[id]?.status === FAILED);

  return (
    <div className={styles['upload-progress-poll']}>
      <div className={styles['summary']}>
        {I18n.t('rag_upload_progress_summary', { ready: readyCount, total: docIds.length })}
        {` `}{readyCount}/{docIds.length}
      </div>
      <ul className={styles['list']}>
        {docIds.map(id => {
          const p = progress[id];
          return (
            <li key={id}>
              <Progress percent={p?.progress ?? 0} />
              {p?.status === FAILED && (
                <>
                  <span className={styles['error']}>{p.status_descript ?? I18n.t('rag_upload_failed_generic')}</span>
                  <Button disabled>{I18n.t('rag_upload_contact_admin')}</Button>
                </>
              )}
            </li>
          );
        })}
      </ul>
      {failedDocs.length > 0 && (
        <div className={styles['hint']}>{I18n.t('rag_upload_retry_pending_support')}</div>
      )}
    </div>
  );
};
```

- [ ] **Step 4: Add minimal styles.** Create `index.module.less` with simple flex layout — exact CSS not critical for the test to pass.

- [ ] **Step 5: Run tests, confirm all 3 pass.**

- [ ] **Step 6: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/components/upload-progress-poll/
git commit -m "feat(knowledge): add UploadProgressPoll for rag-mode wizards"
```

---

## Phase C: Frontend — per-type rag-mode wizards

### Task 7: Build `TextLocalAddRagConfig` and its `<TextProgress />` step

**Files:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/index.ts`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/config.tsx`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/constants.ts`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/steps/index.ts`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/steps/progress/index.tsx`
- (Reuse without modification) `.../local/add/steps/upload/upload.tsx`
- (Reuse without modification) `.../local/add/store/`

- [ ] **Step 1: Read the legacy `config.tsx` and `constants.ts` files** to learn the wizard config shape. Path: `.../local/add/config.tsx` and `.../local/add/constants.ts`. Note the exported keys (`TextLocalAddUpdateConfig`, `TextLocalAddUpdateStep`) and the structure of the step list.

- [ ] **Step 2: Create `constants.ts` with just two enum values.**

```typescript
// Rag-mode wizard steps. Mirrors TextLocalAddUpdateStep but drops SEGMENT_CLEANER and SEGMENT_PREVIEW.
export enum TextLocalAddRagStep {
  UPLOAD = 'upload',
  PROGRESS = 'progress',
}
```

- [ ] **Step 3: Create the progress step component** at `steps/progress/index.tsx`. It's a thin adapter over `<UploadProgressPoll />` that reads doc IDs from the store and navigates on completion.

```tsx
import { useNavigate } from 'react-router-dom';
import { UploadProgressPoll } from '../../../../../../../components/upload-progress-poll';
import { useTextLocalAddUpdateStore } from '../../store';

export const TextProgress = () => {
  const navigate = useNavigate();
  const docIds = useTextLocalAddUpdateStore(s => s.docIds);  // exact selector TBC during Step 7
  const datasetId = useTextLocalAddUpdateStore(s => s.datasetId);

  return (
    <UploadProgressPoll
      docIds={docIds}
      onComplete={() => navigate(`/knowledge/${datasetId}`)}
    />
  );
};
```

(The exact store selector keys must match the legacy `store/` shape — read the file first and align.)

- [ ] **Step 4: Create `steps/index.ts` re-exporting both steps.**

```typescript
export { TextUpload } from '../../add/steps/upload';  // reuse legacy upload step
export { TextProgress } from './progress';
```

- [ ] **Step 5: Create `config.tsx`** that wires the two steps in order. Use the legacy `TextLocalAddUpdateConfig` as a structural template — copy the file then trim steps to `[UPLOAD, PROGRESS]`.

```tsx
import { TextLocalAddRagStep } from './constants';
import { TextUpload, TextProgress } from './steps';
// import any shared types from -core needed by the config shape

export const TextLocalAddRagConfig = {
  steps: [
    { key: TextLocalAddRagStep.UPLOAD, Component: TextUpload },
    { key: TextLocalAddRagStep.PROGRESS, Component: TextProgress },
  ],
  initialStep: TextLocalAddRagStep.UPLOAD,
  // ... matching the legacy config shape (header config, footer config, etc.)
};
```

Exact shape: match what the legacy config exports. The legacy version has a fixed contract with the wizard engine; mismatching breaks the engine.

- [ ] **Step 6: Create the package-local barrel** `add-rag/index.ts`:

```typescript
export { TextLocalAddRagConfig } from './config';
export { TextLocalAddRagStep } from './constants';
```

- [ ] **Step 7: Add the export to the parent barrel** `text/first-party/local/index.tsx`:

```typescript
export { TextLocalAddRagConfig } from './add-rag';
```

- [ ] **Step 8: Write a smoke test** at `add-rag/__tests__/config.test.tsx`:

```typescript
import { describe, it, expect } from 'vitest';
import { TextLocalAddRagConfig } from '../config';
import { TextLocalAddRagStep } from '../constants';

describe('TextLocalAddRagConfig', () => {
  it('exposes exactly two steps in upload-then-progress order', () => {
    expect(TextLocalAddRagConfig.steps).toHaveLength(2);
    expect(TextLocalAddRagConfig.steps[0].key).toBe(TextLocalAddRagStep.UPLOAD);
    expect(TextLocalAddRagConfig.steps[1].key).toBe(TextLocalAddRagStep.PROGRESS);
  });
  it('starts on upload', () => {
    expect(TextLocalAddRagConfig.initialStep).toBe(TextLocalAddRagStep.UPLOAD);
  });
});
```

- [ ] **Step 9: Run tests, confirm pass.**

```bash
rush test --to @coze-data/knowledge-resource-processor-base
```

- [ ] **Step 10: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/add-rag/ \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/text/first-party/local/index.tsx
git commit -m "feat(knowledge): add rag-mode wizard config for text local upload"
```

---

### Task 8: Build `ImageFileAddRagConfig` and its progress step

**Files:** Mirror Task 7's structure under `image/file/add-rag/`.

- Create: `.../image/file/add-rag/index.ts`
- Create: `.../image/file/add-rag/config.tsx`
- Create: `.../image/file/add-rag/constants.ts`
- Create: `.../image/file/add-rag/steps/index.ts`
- Create: `.../image/file/add-rag/steps/progress/index.tsx`
- Modify (barrel export): `.../image/file/index.tsx`

- [ ] **Step 1: Read legacy** `.../image/file/config.tsx` and `.../image/file/steps/annotation/index.tsx`. Note that the legacy image flow has an `annotation` step (caption preview/edit) that calls `ExtractPhotoCaption`. The rag wizard drops it.

- [ ] **Step 2: Create `constants.ts`.**

```typescript
export enum ImageFileAddRagStep {
  UPLOAD = 'upload',
  PROGRESS = 'progress',
}
```

- [ ] **Step 3: Create `steps/progress/index.tsx`.** Same pattern as the text version — `<UploadProgressPoll />` driven by the existing image-flow store's docIds + datasetId.

```tsx
import { useNavigate } from 'react-router-dom';
import { UploadProgressPoll } from '../../../../../../components/upload-progress-poll';
import { useImageFileStore } from '../../store';  // path TBC; match legacy import

export const ImageProgress = () => {
  const navigate = useNavigate();
  const docIds = useImageFileStore(s => s.docIds);
  const datasetId = useImageFileStore(s => s.datasetId);
  return <UploadProgressPoll docIds={docIds} onComplete={() => navigate(`/knowledge/${datasetId}`)} />;
};
```

- [ ] **Step 4: Create `steps/index.ts`.**

```typescript
export { ImageUpload } from '../../steps/upload';  // reuse legacy
export { ImageProgress } from './progress';
```

- [ ] **Step 5: Create `config.tsx`** mirroring the legacy `ImageFileAddConfig` but with steps `[UPLOAD, PROGRESS]`.

- [ ] **Step 6: Create barrel** `add-rag/index.ts`.

```typescript
export { ImageFileAddRagConfig } from './config';
export { ImageFileAddRagStep } from './constants';
```

- [ ] **Step 7: Add the export to** `image/file/index.tsx`.

- [ ] **Step 8: Smoke test** at `add-rag/__tests__/config.test.tsx`. Same pattern as Task 7 Step 8.

- [ ] **Step 9: Run tests, confirm pass.**

- [ ] **Step 10: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/ \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/index.tsx
git commit -m "feat(knowledge): add rag-mode wizard config for image file upload"
```

---

### Task 9: Build `TableLocalAddRagConfig` and its progress step

**Files:** Mirror Task 7's structure under `table/first-party/local/add-rag/`.

- Create: `.../table/first-party/local/add-rag/index.ts`
- Create: `.../table/first-party/local/add-rag/config.tsx`
- Create: `.../table/first-party/local/add-rag/constants.ts`
- Create: `.../table/first-party/local/add-rag/steps/index.ts`
- Create: `.../table/first-party/local/add-rag/steps/progress/index.tsx`
- Modify (barrel export): `.../table/first-party/local/index.tsx` (or whichever barrel exports `TableLocalAddConfig`)

- [ ] **Step 1: Read legacy** `.../table/first-party/local/add/config.tsx`. Note legacy table flow's schema/validate/preview steps — all calling `Get*TableSchema*` / `ValidateTableSchema` / `CreateDocumentReview`. The rag wizard drops them.

- [ ] **Step 2-10: Follow Task 8's pattern verbatim**, substituting `Table` for `Image` and the correct store path.

- [ ] **Step 11: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/table/first-party/local/add-rag/ \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/table/first-party/local/index.tsx
git commit -m "feat(knowledge): add rag-mode wizard config for table local upload"
```

---

## Phase D: Frontend — gating

### Task 10: Wire the gating in `scenes/base/config.ts`

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-adapter/src/scenes/base/config.ts`
- Create test: `frontend/packages/data/knowledge/knowledge-resource-processor-adapter/src/scenes/base/__tests__/config.test.ts`
- Modify (per Task 0 Step 4 choice): every `getConfigV2()` call site

- [ ] **Step 1: Import the rag configs** at the top of `scenes/base/config.ts`:

```typescript
import { TextLocalAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local';
import { ImageFileAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/image/file';
import { TableLocalAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local';
import { isRagBackend } from '@coze-data/knowledge-resource-processor-core';
```

- [ ] **Step 2: Modify `getConfigV2` per the gating shape chosen in Task 0 Step 4.** Two reference implementations — use the one that matches the call-site survey.

**Shape A — pass `backend` through:**

```typescript
type Backend = 'rag' | 'legacy' | undefined;

const getConfigV2 = (backend: Backend = 'legacy') => {
  const isRag = backend === 'rag';
  return {
    [UnitType.TEXT_DOC]: {
      [OptType.ADD]: isRag ? TextLocalAddRagConfig : TextLocalAddUpdateConfig,
      [OptType.RESEGMENT]: TextLocalResegmentConfig,
    },
    [UnitType.TABLE_DOC]: {
      [OptType.ADD]: isRag ? TableLocalAddRagConfig : TableLocalAddConfig,
      [OptType.INCREMENTAL]: TableLocalIncrementalConfig,
    },
    [UnitType.IMAGE_FILE]: {
      [OptType.ADD]: isRag ? ImageFileAddRagConfig : ImageFileAddConfig,
    },
    // ... unchanged entries
  };
};
```

**Shape B — thunks the caller invokes:**

```typescript
const getConfigV2 = () => ({
  [UnitType.TEXT_DOC]: {
    [OptType.ADD]: (kb: Pick<Dataset, 'backend'>) =>
      isRagBackend(kb) ? TextLocalAddRagConfig : TextLocalAddUpdateConfig,
    [OptType.RESEGMENT]: TextLocalResegmentConfig,
  },
  // ...similar for TABLE_DOC and IMAGE_FILE
});
```

- [ ] **Step 3: Write tests** covering all 6 cells (3 KB types × 2 backends).

```typescript
import { describe, it, expect } from 'vitest';
import { getConfigV2 } from '../config';
import { UnitType, OptType } from '@coze-data/knowledge-resource-processor-core';
import {
  TextLocalAddUpdateConfig, TextLocalAddRagConfig,
  TableLocalAddConfig, TableLocalAddRagConfig,
  ImageFileAddConfig, ImageFileAddRagConfig,
} from '...';  // adjust imports

describe('getConfigV2', () => {
  it('returns legacy text config when backend=legacy or undefined', () => {
    expect(getConfigV2('legacy')[UnitType.TEXT_DOC][OptType.ADD]).toBe(TextLocalAddUpdateConfig);
    expect(getConfigV2()[UnitType.TEXT_DOC][OptType.ADD]).toBe(TextLocalAddUpdateConfig);
  });
  it('returns rag text config when backend=rag', () => {
    expect(getConfigV2('rag')[UnitType.TEXT_DOC][OptType.ADD]).toBe(TextLocalAddRagConfig);
  });
  it('routes table add by backend', () => {
    expect(getConfigV2('legacy')[UnitType.TABLE_DOC][OptType.ADD]).toBe(TableLocalAddConfig);
    expect(getConfigV2('rag')[UnitType.TABLE_DOC][OptType.ADD]).toBe(TableLocalAddRagConfig);
  });
  it('routes image add by backend', () => {
    expect(getConfigV2('legacy')[UnitType.IMAGE_FILE][OptType.ADD]).toBe(ImageFileAddConfig);
    expect(getConfigV2('rag')[UnitType.IMAGE_FILE][OptType.ADD]).toBe(ImageFileAddRagConfig);
  });
  it('does not affect non-ADD entries', () => {
    // RESEGMENT/INCREMENTAL stay legacy in both modes — they're out of scope.
    expect(getConfigV2('rag')[UnitType.TEXT_DOC][OptType.RESEGMENT]).toBe(TextLocalResegmentConfig);
  });
});
```

Adapt the test for Shape B (call the thunk with a `{backend}` arg).

- [ ] **Step 4: Update every `getConfigV2()` call site.** Use the grep result from Task 0 Step 3. Pass the kb's backend (read from the kb being uploaded into — must already be in scope since the caller knew which UnitType to ask for).

- [ ] **Step 5: Run all `knowledge-resource-processor-*` tests.**

```bash
rush test --to-version-policy knowledge-resource-processor
```

Expected: all pass.

- [ ] **Step 6: Commit.**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-adapter/
git commit -m "feat(knowledge): gate upload wizard config by kb.backend"
```

---

## Phase E: Integration validation

### Task 11: End-to-end manual smoke

**Setup:** Follow the recipe in the project memory (`project-coze-rag-replacement-paused.md` §2 queued recipe). Specifically:

- Recreate `rag/docker-compose.local.yml` with the `27018/9201/6380/9100/9101` port remaps.
- Ensure `rag/config/model_providers.json` exists with a tenant_id that matches what's in `coze-studio/docker/.env.debug` (RAG_TENANT_ID).
- `docker compose -f docker-compose.yml -f docker-compose.local.yml up -d` (in rag).
- `make middleware` (in coze-studio).
- `GOTOOLCHAIN=go1.24.0 make server` (in coze-studio).

- [ ] **Step 1: Verify text upload happy path.** Open `http://localhost:8888`, log in (or register), create a fresh text KB with both embedding model IDs from your catalog, upload a small `.txt`, observe `<UploadProgressPoll />` rendering, wait for ready, confirm auto-navigation to KB detail page.

- [ ] **Step 2: Verify rag-side log evidence.** Tail rag-web logs:

```bash
docker compose -f /Users/liuxinyu/workspace/rag/docker-compose.yml \
                -f /Users/liuxinyu/workspace/rag/docker-compose.local.yml \
                logs --tail=200 web | grep rag.request
```

Confirm presence of `POST /api/v1/knowledgebases/{id}/documents` and `GET /api/v1/knowledgebases/{id}/documents/{id}` polling.
Confirm absence of any `CreateDocumentReview` 5xx errors in `/tmp/coze-server.log`.

- [ ] **Step 3: Verify legacy path still works.** Switch `KNOWLEDGE_BACKEND=legacy` in `.env.debug`, restart coze server (`pkill -f opencoze && make server`), upload to a legacy KB, confirm the 4-step wizard renders unchanged. (If no legacy KBs exist, skip this step and note it; legacy regression risk is low because nothing in `add/` changed.)

- [ ] **Step 4: Document findings.** Append a short note to the project memory (`What's done in latest smoke`) capturing the test result and any deviation from the plan.

- [ ] **Step 5: No commit** — this is verification, not code.

---

## Phase F: Follow-up tasks (file, don't implement)

### Task 12: File follow-up issues / memory updates

- [ ] **Step 1: Add ragimpl follow-up task** for `RetryDocument` wiring (rag's `POST /documents/{doc_id}/retry` endpoint exists per queued bonus #4; ragimpl's `RetryDocument` is currently unimplemented). New TaskCreate item: `"Wire ragimpl.RetryDocument to rag's /documents/{doc_id}/retry"`.

- [ ] **Step 2: Add UI cleanup follow-up task** for hiding other bucket-B pending-stub entry points on the KB detail page (manual chunk editor, document re-segmentation menu, KB copy/move) when `kb.backend === "rag"`. Per spec §8.

- [ ] **Step 3: Update the project memory `project-coze-rag-replacement-paused.md`:**
  - In §2 queued item "Verify end-to-end with `KNOWLEDGE_BACKEND=rag`", mark **DONE ✓** and add a HEAD commit reference for the merged plan commits.
  - Remove the "Still blocked: Document upload UI hits `CreateDocumentReview`..." paragraph.
  - In §8 queued item "Decide review-workflow strategy", update status from "pending" to "implemented (UI-route)".

- [ ] **Step 4: Commit memory + follow-up notes if any are in the repo** (memory is in `~/.claude` — that's separate from the repo and updated via Edit tool, not committed here).

---

## Self-review notes

**Spec coverage check** (run after writing): each section of the design doc has at least one task:
- §3.1 gating signal (`backend` field on Dataset) → Tasks 1, 2, 3, 4
- §3.2 frontend layout (`add-rag/` siblings) → Tasks 7, 8, 9
- §3.3 step mapping → Tasks 7, 8, 9 (config files trim steps per the table)
- §4.1 `<UploadProgressPoll />` → Task 6
- §4.2 per-type wizards → Tasks 7, 8, 9
- §4.3 shell router via `scenes/base/config.ts` → Task 10
- §5 data flow → manual verification in Task 11
- §6 error handling (retry stub) → Task 6 Step 3 + Task 12 follow-up
- §7 testing → unit tests in each task + Task 11 manual
- §8 out-of-scope (hide pending stub entries) → Task 12 follow-up
- §9 open questions → Task 0

**Placeholder scan:** No "TBD" / "implement later" / "add validation" / etc. — every concrete code edit has actual code. The `// path TBC; match legacy import` comments in Task 8/9 progress steps are explicit pointers, not placeholders — the implementer needs to read the sibling file to align, and the test framework will catch mismatches.

**Type consistency:** Backend field is named `Backend` in Go (after thrift regen) and `backend` in TS (lowercase per thrift→TS convention). `isRagBackend(kb)` is consistent across Task 5 + 10. Step enum naming (`TextLocalAddRagStep` etc.) is consistent across tasks 7-9.
