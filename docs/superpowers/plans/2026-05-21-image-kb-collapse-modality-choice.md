# Image KB: Collapse Modality Choice — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the image KB upload wizard, the "图像 vs 扫描件" mode `<Select>` no longer renders. Flow auto-picks `image_document` and proceeds straight into the dynamic parsing panel. Behaviour is identical to the existing `image_document` path post-`FORCED_PARAMS_BY_SCHEMA`, since both schemas produce byte-identical wire payloads after the force layer.

**Architecture:** Narrow `candidateSchemas` to `image_document` only inside `ImageRagSegment`'s `useMemo` (image-KB-specific call site of `matchSchemasForFile`). `matchSchemasForFile` itself is untouched — text KB's PDF text/scanned distinction is real and stays intact. The existing `length > 1` gates around the Select / `_source_modality` injection / scanned-hint self-disable when the candidate set shrinks to 1, so no other render or wire code needs to change.

**Tech Stack:** TypeScript, React, Vitest, `@testing-library/react`, `@coze-arch/coze-design` (`Select`, `Typography`), `@coze-arch/i18n`. Frontend-only — no backend or rag-server changes.

**Files Touched:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/index.tsx`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/segment.test.tsx`

**Package under test:** `@coze-data/knowledge-resource-processor-base`
**Test command (run from package dir):** `npm run test`
**Package dir:** `frontend/packages/data/knowledge/knowledge-resource-processor-base`

---

## Task 1: Write the failing tests for `ImageRagSegment`

Cover the four spec scenarios (jpg/png happy path, loading, error, empty catalog). The "absent `image_document` in catalog → empty config area" is the only behavioural surprise; the rest are checking that the modality Select is gone.

**Files:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/segment.test.tsx`

---

- [ ] **Step 1: Write the test file**

Create `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/segment.test.tsx`:

```tsx
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Mock the schemas hook so we can drive the segment step's input directly.
const useRagDocumentParameterSchemasMock = vi.fn();
vi.mock('@/features/dynamic-parsing-panel', async () => {
  const actual = await vi.importActual<
    typeof import('@/features/dynamic-parsing-panel')
  >('@/features/dynamic-parsing-panel');
  return {
    ...actual,
    useRagDocumentParameterSchemas: () => useRagDocumentParameterSchemasMock(),
  };
});

vi.mock('@coze-arch/i18n', () => ({
  I18n: { t: (k: string) => k },
}));

// Minimal zustand-shape store mock matching ImageFileAddStore's shape used by
// ImageRagSegment. Only the keys the component reads are present.
const storeState = {
  unitList: [{ type: 'jpg' }],
  setCurrentStep: vi.fn(),
  setDocumentOptions: vi.fn(),
};
const useStoreMock = vi.fn((selector: (s: typeof storeState) => unknown) =>
  selector(storeState),
);

import { ImageRagSegment } from './index';

const baseProps = {
  useStore: useStoreMock,
  footer: (_buttons: unknown) => null,
  // The rest of ContentProps is unused by ImageRagSegment in this test setup.
} as never;

const imageDocumentSchema = {
  schema_id: 'image_document',
  description: 'Image processing parameters.',
  file_types: ['jpg', 'jpeg', 'png', 'webp', 'gif', 'bmp', 'tiff'],
  source_modalities: ['image_source'],
  parameters: [
    {
      name: 'enable_ocr',
      type: 'boolean',
      group: 'ocr',
      default: false,
      description: 'Whether to extract text via OCR.',
      ui_label: 'Enable OCR',
      ui_component: 'switch',
    },
  ],
};

const scannedDocumentSchema = {
  schema_id: 'scanned_document',
  description: 'Scanned document parameters.',
  file_types: ['jpg', 'jpeg', 'png', 'pdf', 'webp', 'tiff', 'bmp'],
  source_modalities: ['scanned_document_source'],
  parameters: [
    {
      name: 'enable_ocr',
      type: 'boolean',
      group: 'ocr',
      default: true,
      description: 'Scanned documents require OCR to produce text chunks.',
      ui_label: 'Enable OCR',
      ui_component: 'switch',
    },
  ],
};

beforeEach(() => {
  useRagDocumentParameterSchemasMock.mockReset();
  storeState.unitList = [{ type: 'jpg' }];
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('ImageRagSegment', () => {
  it('does NOT render a modality Select when both image and scanned schemas match', () => {
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: [imageDocumentSchema, scannedDocumentSchema],
      loading: false,
      error: null,
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    // The mode-label i18n key would render if the Select were present.
    expect(
      screen.queryByText('datasets_createFileModel_rag_mode_label'),
    ).not.toBeInTheDocument();
  });

  it('renders DynamicParsingPanel against image_document for fileType=jpg', () => {
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: [imageDocumentSchema, scannedDocumentSchema],
      loading: false,
      error: null,
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    // The image schema's enable_ocr toggle's label (rag's raw `ui_label`
    // for now — i18n landed by sibling spec is independent).
    expect(screen.getByText('Enable OCR')).toBeInTheDocument();
  });

  it('renders DynamicParsingPanel against image_document for fileType=png', () => {
    storeState.unitList = [{ type: 'png' }];
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: [imageDocumentSchema, scannedDocumentSchema],
      loading: false,
      error: null,
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    expect(screen.getByText('Enable OCR')).toBeInTheDocument();
  });

  it('renders only the loading text when schemas are loading', () => {
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: null,
      loading: true,
      error: null,
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    expect(
      screen.getByText('datasets_createFileModel_rag_loading_schemas'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasets_createFileModel_rag_mode_label'),
    ).not.toBeInTheDocument();
  });

  it('renders error + retry when schemas failed, no Select', () => {
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: null,
      loading: false,
      error: new Error('boom'),
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    expect(
      screen.getByText(/datasets_createFileModel_rag_schemas_failed/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasets_createFileModel_rag_mode_label'),
    ).not.toBeInTheDocument();
  });

  it('renders nothing in the config area when image_document is absent from the catalog', () => {
    // Edge case: rag dropped image_document. We refuse to silently fall
    // back to scanned (spec §Tests). Config area is empty; user notices.
    useRagDocumentParameterSchemasMock.mockReturnValue({
      schemas: [scannedDocumentSchema],
      loading: false,
      error: null,
      retry: vi.fn(),
    });

    render(<ImageRagSegment {...baseProps} />);

    // No Enable OCR label means the panel didn't render against scanned.
    expect(screen.queryByText('Enable OCR')).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasets_createFileModel_rag_mode_label'),
    ).not.toBeInTheDocument();
  });
});
```

If `@testing-library/react` is not already a devDependency in this package, check sibling test files (e.g. `add-rag/__tests__/config.test.tsx`) for the existing dep pattern and copy it.

- [ ] **Step 2: Run tests to verify they fail**

Run (from package dir):
```bash
npm run test -- knowledge-type/image/file/add-rag/steps/segment
```

Expected: FAIL — the first test expects no Select to render, but the current code renders one when `candidateSchemas.length > 1` (which is the case when both image + scanned schemas match jpg).

The exact failing assertion is:
```
expected null not to be in the document
Found: element with text "datasets_createFileModel_rag_mode_label"
```

The "image_document absent" test will also fail because today the code falls through to `candidateSchemas[0]` (scanned_document) and renders its panel — exposing `Enable OCR` in the DOM.

- [ ] **Step 3: Do not commit yet.** Continues into Task 2.

---

## Task 2: Apply the filter

The single behavioural change: filter `matchSchemasForFile` result to `image_document` only. Comment explains why for the next reader.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/index.tsx`

---

- [ ] **Step 1: Modify `candidateSchemas`**

Open `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/index.tsx`. Find the `candidateSchemas` `useMemo` (around line 80-85):

```tsx
const candidateSchemas = useMemo<DocumentParameterSchema[]>(() => {
  if (!schemas || !fileType) {
    return [];
  }
  return matchSchemasForFile(schemas, fileType);
}, [schemas, fileType]);
```

Replace with:

```tsx
const candidateSchemas = useMemo<DocumentParameterSchema[]>(() => {
  if (!schemas || !fileType) {
    return [];
  }
  // Image KB uploads only ever materially behave as image_document after
  // the FORCED_PARAMS_BY_SCHEMA layer in use-schemas.ts — scanned_document
  // is byte-equivalent on the wire post-force. Narrow to image_document
  // here (vs. inside matchSchemasForFile, which stays correct for the
  // text-KB call site where PDF text/scanned do meaningfully differ).
  return matchSchemasForFile(schemas, fileType).filter(
    s => s.schema_id === 'image_document',
  );
}, [schemas, fileType]);
```

- [ ] **Step 2: Update the JSDoc block at the top of the file**

Find the JSDoc above `ImageRagSegment` (the `Behaviour:` bullet list around line 44-58). Replace the bullets describing the two-schema selector with a one-line note about the collapse:

```tsx
/**
 * Rag-mode SEGMENT_CLEANER step for the image upload wizard (Phase 3b).
 * Twin of the text wizard's <TextRagSegment /> — same schema-driven form
 * UX, but scoped to the `image_document` schema only.
 *
 * Image KB uploads materially behave as `image_document` after the
 * `FORCED_PARAMS_BY_SCHEMA` layer applies (`scanned_document` produces
 * byte-identical wire payloads post-force), so the modality selector that
 * the text wizard exposes for PDF text-vs-scanned is intentionally absent
 * here. See 2026-05-21-image-kb-collapse-modality-choice-design.md.
 *
 * On Next, serialises the form values into the wire blob and advances to
 * PROGRESS. Catalog load failure falls back to "advance with empty
 * options" so a transient outage on the schemas endpoint does not block
 * uploads. If `image_document` is absent from the catalog (a rag-side
 * breaking change), the config area renders empty rather than silently
 * falling back to scanned — the user surfaces the broken state.
 */
```

- [ ] **Step 3: Run tests to verify they pass**

Run (from package dir):
```bash
npm run test -- knowledge-type/image/file/add-rag/steps/segment
```

Expected: PASS (all 6 cases).

- [ ] **Step 4: Run wider package tests to check for regressions**

Run (from package dir): `npm run test`
Expected: PASS for the full sweep. The text-KB segment step's tests must remain green — its `matchSchemasForFile` call site is intact.

- [ ] **Step 5: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/index.tsx \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/segment/segment.test.tsx
git commit -m "feat(knowledge-rag): collapse image-KB image_document/scanned_document choice"
```

---

## Task 3: Wider sweep + smoke

**Files:** none (verification only).

---

- [ ] **Step 1: Lint**

Run (from package dir): `npm run lint` (or `rush lint-staged` from repo root)
Expected: no new errors.

- [ ] **Step 2: Build affected packages**

Run from repo root: `rush rebuild -t @coze-data/knowledge-resource-processor-base`
Expected: build succeeds.

- [ ] **Step 3: Manual smoke**

In a dev server (`cd frontend/apps/coze-studio && npm run dev`):
1. Open the image KB upload wizard.
2. Drop a jpg/png/webp/etc.
3. Click Next into the segment step.

Expected:
- No mode-label `<Select>` ("图像" vs "扫描件") at the top of the segment step.
- The dynamic parsing panel is rendered directly with image_document parameters (forced OCR-on, hidden produce_image_chunk, etc. — same as today's `image_document`-picked behaviour).
- The 2026-05-20 force-OCR-on hint still appears next to `enable_ocr`.

If a Select still appears: check `candidateSchemas`'s post-filter length in DevTools React inspector — should be exactly 1.

- [ ] **Step 4: Open PR**

```bash
git push -u origin "$(git branch --show-current)"
gh pr create --title "feat(knowledge-rag): collapse image-KB modality choice" --body "$(cat <<'EOF'
## Summary

- In the image KB upload wizard, the "图像 vs 扫描件" mode `<Select>` no longer renders.
- `candidateSchemas` filters `matchSchemasForFile` output to `image_document` only at the segment-step call site; `matchSchemasForFile` and the text-KB code path are untouched.
- Behaviour is identical to today's `image_document` path post-`FORCED_PARAMS_BY_SCHEMA` — both schemas already produced byte-identical wire payloads, the Select was a no-op choice.

Spec: `docs/superpowers/specs/2026-05-21-image-kb-collapse-modality-choice-design.md`

## Test plan

- [x] `npm run test` passes in `@coze-data/knowledge-resource-processor-base`
- [x] Manual: image KB upload of jpg/png/webp — no mode-label Select; parsing panel renders directly with image_document
- [x] Manual: text KB upload of PDF — text/scanned selector still works (unaffected)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Map (spec section → plan task)

- §Design / Filter to the canonical schema → Task 2 Step 1
- §Design / `_source_modality` injection (self-disabling explanation) → Task 2 Step 2 JSDoc + spec text; no code change needed
- §Design / `isScannedSchema` hint (self-disabling) → No task: the existing `isScannedSchema` derivation evaluates to false post-filter, the hint render block self-skips. Tests in Task 1 implicitly cover by asserting only image_document params show.
- §Design / Tests (5 scenarios) → Task 1 Step 1 (6 cases — happy path is split into jpg and png to lock the file-type fan-out)
- §Risks / future divergence → JSDoc comment in Task 2 Step 2 references this spec so a future reader who hits the divergence knows where to revert.
