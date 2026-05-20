# Image Upload: Force OCR On — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lock `enable_ocr=true` for the `image_document` and `scanned_document` upload schemas so that the natural image-upload UX produces text-chunks the workflow knowledge-retrieve node (text-in/text-out) can find.

**Architecture:** Declarative `FORCED_PARAMS_BY_SCHEMA` map in `use-schemas.ts` is the single source of truth. A new `applyForcedParams` helper in `validate.ts` enforces forced values on the wire payload; `mergeSchemaDefaults` calls it **before** the existing inverse-OCR mutex so `ocr_model_id` stays alongside the forced `enable_ocr=true`. The panel reads the same map to render matching controls as `disabled` with a tooltip.

**Tech Stack:** TypeScript, React, Vitest, `@testing-library/react`, `@coze-arch/coze-design` (`Switch`, `Tooltip`, `Typography`), `@coze-arch/i18n`. Frontend-only — no backend or rag-server changes.

**Files Touched:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts` — add `FORCED_PARAMS_BY_SCHEMA`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts` — add `applyForcedParams`, call it inside `mergeSchemaDefaults`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts` — extend tests
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx` — render forced controls disabled with tooltip
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx` — new component tests
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en.json` — add `datasets_createFileModel_rag_forced_ocr_hint`
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json` — add same key with zh-CN copy

**Package under test:** `@coze-data/knowledge-resource-processor-base`
**Test command (run from package dir):** `npm run test`

---

## Task 1: Add `FORCED_PARAMS_BY_SCHEMA` map and `applyForcedParams` helper

Adds the declarative map and the pure-function helper that consumes it. Tests target `applyForcedParams` directly because the map itself is data — its correctness is exercised through the helper.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts`
- Test: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts`

---

- [ ] **Step 1: Write failing tests for `applyForcedParams`**

Open `validate.test.ts`. At the bottom of the file (after the `describe('filterParamsByDependencies', ...)` block, before the final closing line), append:

```ts
import {
  applyForcedParams,
  filterParamsByDependencies,
  findMissingRequired,
  mergeSchemaDefaults,
} from './validate';
```

(Replace the existing `import` block at the top — the only addition is `applyForcedParams`.)

Then append at the end of the file:

```ts
describe('applyForcedParams', () => {
  it('returns the input map unchanged when schemaId is not in the forced map', () => {
    const value = { enable_ocr: false, foo: 'bar' };
    expect(applyForcedParams('text_document', value)).toEqual(value);
    expect(applyForcedParams('markdown_document', value)).toEqual(value);
    expect(applyForcedParams('totally_unknown', value)).toEqual(value);
    expect(applyForcedParams(undefined, value)).toEqual(value);
  });

  it('overrides value.enable_ocr=false to true for image_document', () => {
    expect(
      applyForcedParams('image_document', { enable_ocr: false }),
    ).toEqual({ enable_ocr: true });
  });

  it('overrides value.enable_ocr=false to true for scanned_document', () => {
    expect(
      applyForcedParams('scanned_document', { enable_ocr: false }),
    ).toEqual({ enable_ocr: true });
  });

  it('preserves other keys when overriding forced params', () => {
    expect(
      applyForcedParams('image_document', {
        enable_ocr: false,
        ocr_model_id: 'paddle',
        enable_image_embedding: true,
      }),
    ).toEqual({
      enable_ocr: true,
      ocr_model_id: 'paddle',
      enable_image_embedding: true,
    });
  });

  it('does not mutate the input map', () => {
    const input = { enable_ocr: false };
    applyForcedParams('image_document', input);
    expect(input).toEqual({ enable_ocr: false });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run (from package dir `frontend/packages/data/knowledge/knowledge-resource-processor-base`):

```bash
npm run test -- validate.test.ts
```

Expected: tests in the new `applyForcedParams` describe block fail with an import error or "applyForcedParams is not a function" (depending on how vitest reports the missing export). Existing `findMissingRequired` / `mergeSchemaDefaults` / `filterParamsByDependencies` tests still pass.

- [ ] **Step 3: Add `FORCED_PARAMS_BY_SCHEMA` to `use-schemas.ts`**

Open `use-schemas.ts`. Find the existing `FRONTEND_PARAM_DEFAULTS` const block (lines 35-49). Immediately AFTER that const declaration (before the `applyFrontendDefaults` function on line 51), add:

```ts
// Params whose value is locked, regardless of user input or schema default.
// Keyed by rag schema_id, then by param.name.
//
// Why: rag's image_document schema declares enable_ocr default=false, but
// coze's workflow knowledge-retrieve node only does text-in/text-out, so an
// OCR-off image upload silently produces a KB the node cannot retrieve from.
// Force OCR on at the frontend so the natural upload UX produces text_chunks.
//
// `reason` is an i18n key used for the disabled-control tooltip in
// dynamic-parsing-panel.tsx.
export const FORCED_PARAMS_BY_SCHEMA: Readonly<
  Record<string, Readonly<Record<string, { value: unknown; reason: string }>>>
> = {
  image_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
  },
  scanned_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
  },
};
```

- [ ] **Step 4: Add `applyForcedParams` to `validate.ts`**

Open `validate.ts`. At the top of the file, expand the imports — the file currently imports only types from `./types`. Add a sibling import from `./use-schemas` directly under the existing import block:

```ts
import { FORCED_PARAMS_BY_SCHEMA } from './use-schemas';
```

Then, immediately AFTER the `filterParamsByDependencies` function (around line 96, before the `mergeSchemaDefaults` function on line 98), insert:

```ts
/**
 * Overwrites keys in `value` whose forced override is declared in
 * `FORCED_PARAMS_BY_SCHEMA[schemaId]`. Returns a new object — does not mutate
 * the input. When `schemaId` is unknown or has no forced params, returns the
 * input map unchanged (same reference is fine; callers treat the return as
 * read-only).
 *
 * Force order vs the inverse-OCR mutex in `mergeSchemaDefaults`: must be
 * called BEFORE the mutex. A stale form value with `enable_ocr=false` would
 * otherwise let the mutex strip `ocr_model_id`, and only then would the force
 * flip `enable_ocr` back to true — sending `{enable_ocr:true, ⌀ ocr_model_id}`
 * trips rag's 40001 "ocr_model_id is required when enable_ocr is true".
 */
export function applyForcedParams<T extends Record<string, unknown>>(
  schemaId: string | undefined,
  value: T,
): T {
  const forced = FORCED_PARAMS_BY_SCHEMA[schemaId ?? ''];
  if (!forced) {
    return value;
  }
  const next = { ...value };
  for (const [k, { value: v }] of Object.entries(forced)) {
    next[k] = v;
  }
  return next as T;
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
npm run test -- validate.test.ts
```

Expected: all tests pass, including the 5 new `applyForcedParams` cases.

- [ ] **Step 6: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): add FORCED_PARAMS_BY_SCHEMA + applyForcedParams

Introduces a declarative map of params whose values are locked regardless
of user input or schema default. Helper `applyForcedParams` enforces it
on the form value map. Used in the next commit to pin enable_ocr=true
for image_document / scanned_document.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Wire `applyForcedParams` into `mergeSchemaDefaults`

Connects the helper to the wire-payload path. Tests verify the **ordering** (force runs before the inverse-OCR mutex) so `ocr_model_id` survives when forced is on.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts:98-121` (the `mergeSchemaDefaults` function)
- Test: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts`

---

- [ ] **Step 1: Write failing tests for the integration**

Open `validate.test.ts`. Inside the existing `describe('mergeSchemaDefaults', ...)` block (which ends around line 177), append these tests as additional `it(...)` cases just before the closing `});` of that describe block:

```ts
  // Force order test (image_document): the forced enable_ocr=true must be
  // applied BEFORE the inverse-OCR mutex would strip ocr_model_id. Without
  // the correct ordering, stale user input with enable_ocr=false would
  // trigger the mutex and the wire would end up with {enable_ocr:true,
  // missing ocr_model_id} → rag 40001.
  const imageSchema = (params: DocumentParameter[]): DocumentParameterSchema => ({
    schema_id: 'image_document',
    description: '',
    file_types: [],
    source_modalities: [],
    parameters: params,
  });
  const scannedSchema = (params: DocumentParameter[]): DocumentParameterSchema => ({
    schema_id: 'scanned_document',
    description: '',
    file_types: [],
    source_modalities: [],
    parameters: params,
  });

  it('forces enable_ocr=true on image_document even with empty user input', () => {
    const s = imageSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'model-ocr-paddle-infer-text',
      }),
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: true,
      }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({
      enable_ocr: true,
      ocr_model_id: 'model-ocr-paddle-infer-text',
      enable_image_embedding: true,
    });
  });

  it('forces enable_ocr=true on scanned_document even with empty user input', () => {
    const s = scannedSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: true }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'model-ocr-paddle-infer-text',
      }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({
      enable_ocr: true,
      ocr_model_id: 'model-ocr-paddle-infer-text',
    });
  });

  it('forces enable_ocr=true when stale user input carries enable_ocr=false', () => {
    const s = imageSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'model-ocr-paddle-infer-text',
      }),
    ]);
    // User somehow has enable_ocr:false in their form state (cached value,
    // devtools-poked, schema-switch artifact). The wire should still ship
    // enable_ocr:true AND keep ocr_model_id.
    expect(
      mergeSchemaDefaults(s, { enable_ocr: false }),
    ).toEqual({
      enable_ocr: true,
      ocr_model_id: 'model-ocr-paddle-infer-text',
    });
  });

  it('keeps ocr_model_id present on image_document because force runs before mutex', () => {
    // Regression test for the ordering bug: if applyForcedParams ran AFTER
    // the mutex, this would strip ocr_model_id and then re-enable ocr → rag
    // 40001 "ocr_model_id is required when enable_ocr is true".
    const s = imageSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({ name: 'ocr_model_id', ui_component: 'text', default: 'paddle' }),
    ]);
    const out = mergeSchemaDefaults(s, { enable_ocr: false });
    expect(out.enable_ocr).toBe(true);
    expect(out.ocr_model_id).toBe('paddle');
  });

  it('does not force on schemas not listed in FORCED_PARAMS_BY_SCHEMA', () => {
    // text_document does not appear in FORCED_PARAMS_BY_SCHEMA; user choice
    // about enable_ocr (if the schema has it) must travel intact.
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({ name: 'chunk_size', ui_component: 'number', default: 512 }),
    ]);
    // schema() helper uses schema_id 'test' — not in the forced map.
    expect(mergeSchemaDefaults(s, { enable_ocr: false })).toEqual({
      enable_ocr: false,
      chunk_size: 512,
    });
  });
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
npm run test -- validate.test.ts
```

Expected: the 5 new mergeSchemaDefaults tests fail (most likely with `expected: {enable_ocr: true, ...}, received: {enable_ocr: false, ...}`). Other tests still pass.

- [ ] **Step 3: Wire `applyForcedParams` into `mergeSchemaDefaults`**

In `validate.ts`, replace the existing `mergeSchemaDefaults` function (currently lines 98-121) with:

```ts
export function mergeSchemaDefaults(
  schema: DocumentParameterSchema,
  value: DocumentOptionsValue,
): DocumentOptionsValue {
  const defaults: DocumentOptionsValue = {};
  for (const p of schema.parameters) {
    if (p.default !== undefined && p.default !== null) {
      defaults[p.name] = p.default;
    }
  }
  let merged: DocumentOptionsValue = { ...defaults, ...value };

  // Force-apply locked params (e.g. enable_ocr=true for image_document /
  // scanned_document) BEFORE the inverse-OCR mutex below. Ordering matters:
  // if we forced after, a stale enable_ocr=false in `value` would let the
  // mutex strip ocr_model_id, then the force would flip enable_ocr back to
  // true — wire ends up `{enable_ocr:true, ⌀ ocr_model_id}` → rag 40001.
  merged = applyForcedParams(schema.schema_id, merged);

  // Rag's ingestion validator enforces a mutex: ocr_model_id may only travel
  // when enable_ocr is true. Sending both `{enable_ocr: false, ocr_model_id}`
  // fires 40001 "ocr_model_id requires enable_ocr=true". image_document
  // declares enable_ocr default=false with a non-required ocr_model_id, and
  // FRONTEND_PARAM_DEFAULTS synthesises a model-id default — without this
  // prune every image-KB upload that left OCR off would hit 40001. (No-op
  // for forced schemas because applyForcedParams just pinned enable_ocr=true
  // above.)
  if (merged.enable_ocr !== true && 'ocr_model_id' in merged) {
    delete merged.ocr_model_id;
  }

  return merged;
}
```

(The diff: `const merged` → `let merged`; new `applyForcedParams` call after the spread; comment updated to call out that the mutex is a no-op for forced schemas.)

- [ ] **Step 4: Run tests to verify they pass**

```bash
npm run test -- validate.test.ts
```

Expected: all tests pass, including the 5 new merge tests and the existing inverse-mutex tests (the existing `drops ocr_model_id when enable_ocr defaults to false` test uses `schema_id: 'test'`, not in the forced map, so its behavior is unchanged).

- [ ] **Step 5: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): wire forced enable_ocr=true into mergeSchemaDefaults

image_document and scanned_document uploads now always send
{enable_ocr:true, ocr_model_id} on the wire, regardless of stale user
state. Force runs before the existing inverse-OCR mutex so ocr_model_id
survives — preventing the rag 40001 that would fire if order reversed.

Workflow knowledge-retrieve node is text-in/text-out by design; image
KBs need text_chunks to be retrievable. This pins the default upload
path to that contract.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add i18n locale keys for the forced-OCR hint

The tooltip copy lives in the studio-i18n-resource package. Both en and zh-CN locales must carry the same key.

**Files:**
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en.json`
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json`

---

- [ ] **Step 1: Add the key to en.json**

Open `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en.json`. Find the line `"datasets_createFileModel_rag_advanced_params": "Advanced parameters",` (around line 5436). Insert a new entry immediately AFTER that line, keeping alphabetical order with the surrounding `datasets_createFileModel_rag_*` keys:

```json
  "datasets_createFileModel_rag_forced_ocr_hint": "OCR is required for image knowledge bases — the workflow knowledge-retrieve node currently supports text input/output only, and only OCR-extracted text chunks can be matched.",
```

Verify the entry sits between `datasets_createFileModel_rag_advanced_params` and `datasets_createFileModel_rag_loading_schemas`. Alphabetical ordering: `advanced_params` < `forced_ocr_hint` < `loading_schemas` ✓.

- [ ] **Step 2: Add the key to zh-CN.json**

Open `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json`. Search for the matching key `datasets_createFileModel_rag_advanced_params` (which will have a Chinese value). Add the same key with Chinese copy immediately after it:

```json
  "datasets_createFileModel_rag_forced_ocr_hint": "图片知识库需要开启 OCR 才能被工作流的知识库检索节点命中（节点目前只支持文本输入/输出）",
```

- [ ] **Step 3: Verify JSON is still parseable**

From the repo root:

```bash
python3 -c "import json; json.load(open('frontend/packages/arch/resources/studio-i18n-resource/src/locales/en.json'))" && \
python3 -c "import json; json.load(open('frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json'))" && \
echo "Both locale files parse OK"
```

Expected output: `Both locale files parse OK`. If either file fails (likely cause: missing/extra comma), fix the JSON syntax inline.

- [ ] **Step 4: Commit**

```bash
git add frontend/packages/arch/resources/studio-i18n-resource/src/locales/en.json \
        frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json
git commit -m "$(cat <<'EOF'
feat(i18n): add datasets_createFileModel_rag_forced_ocr_hint

Tooltip copy for the locked enable_ocr toggle on image upload forms.
Explains why the toggle is non-editable: the workflow knowledge-retrieve
node only supports text-in/text-out, so OCR is required for image KBs
to produce retrievable text_chunks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Render forced controls as disabled with a tooltip

Modifies the panel to read `FORCED_PARAMS_BY_SCHEMA` and apply `disabled`+tooltip to matching controls. Includes new component-rendering tests.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx`

---

- [ ] **Step 1: Write failing component tests**

Create new file `dynamic-parsing-panel.test.tsx` in the same directory. The existing tests in this package (e.g. `image/file/add-rag/__tests__/config.test.tsx`) mock both `@coze-arch/i18n` (so `I18n.t(key)` returns the key) and `@coze-arch/coze-design` (whose CSS imports vitest can't resolve). Follow that pattern:

```tsx
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

import React from 'react';

import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

// i18n: return the key so we can assert against it in DOM queries.
vi.mock('@coze-arch/i18n', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  I18n: { t: (key: string) => key },
}));

// coze-design: stub out the design-system primitives this panel uses. The
// real package pulls CSS that vitest can't resolve in this package's
// environment. Stubs preserve the props the test asserts against.
vi.mock('@coze-arch/coze-design', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Switch: ({
    id,
    checked,
    disabled,
    onChange,
  }: {
    id?: string;
    checked?: boolean;
    disabled?: boolean;
    onChange?: (checked: boolean) => void;
  }) => (
    <input
      id={id}
      type="checkbox"
      role="switch"
      checked={Boolean(checked)}
      disabled={Boolean(disabled)}
      onChange={e => onChange?.(e.target.checked)}
      readOnly
    />
  ),
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Tooltip: ({
    content,
    children,
  }: {
    content?: React.ReactNode;
    children?: React.ReactNode;
  }) => (
    <span data-testid="tooltip" data-content={String(content ?? '')}>
      {children}
    </span>
  ),
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Input: ({
    value,
    disabled,
    onChange,
  }: {
    value?: string;
    disabled?: boolean;
    onChange?: (v: string) => void;
  }) => (
    <input
      type="text"
      value={value ?? ''}
      disabled={Boolean(disabled)}
      onChange={e => onChange?.(e.target.value)}
      readOnly={!onChange}
    />
  ),
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  InputNumber: ({
    value,
    disabled,
  }: {
    value?: number;
    disabled?: boolean;
  }) => (
    <input
      type="number"
      value={value ?? ''}
      disabled={Boolean(disabled)}
      readOnly
    />
  ),
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Select: ({ value, disabled }: { value?: string; disabled?: boolean }) => (
    <select value={value ?? ''} disabled={Boolean(disabled)} readOnly>
      <option value={value ?? ''}>{value ?? ''}</option>
    </select>
  ),
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Typography: {
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    Title: ({ children }: { children?: React.ReactNode }) => (
      <div>{children}</div>
    ),
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    Text: ({
      children,
      type,
    }: {
      children?: React.ReactNode;
      type?: string;
    }) => <span data-type={type}>{children}</span>,
  },
}));

// CollapsePanel: not exercised by the forced-params tests (advanced section
// only appears when an advanced param is present), but it's imported at
// module init.
vi.mock('@/components', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  CollapsePanel: ({ children }: { children?: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

import { DynamicParsingPanel } from './dynamic-parsing-panel';
import {
  type DocumentParameter,
  type DocumentParameterSchema,
} from './types';

const param = (over: Partial<DocumentParameter>): DocumentParameter => ({
  name: 'p',
  type: 'string',
  group: 'g',
  required: false,
  description: '',
  ui_label: '',
  ui_component: 'text',
  advanced: false,
  ...over,
});

const schemaOf = (
  schemaId: string,
  params: DocumentParameter[],
): DocumentParameterSchema => ({
  schema_id: schemaId,
  description: '',
  file_types: [],
  source_modalities: [],
  parameters: params,
});

describe('DynamicParsingPanel forced params', () => {
  it('renders enable_ocr disabled on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        default: false,
        ui_label: 'Enable OCR',
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    const sw = document.getElementById(
      'dpp-enable_ocr',
    ) as HTMLInputElement | null;
    expect(sw).not.toBeNull();
    expect(sw?.disabled).toBe(true);
  });

  it('renders enable_ocr checked on image_document even when value says false', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel
        schema={s}
        value={{ enable_ocr: false }}
        onChange={vi.fn()}
      />,
    );
    const sw = document.getElementById(
      'dpp-enable_ocr',
    ) as HTMLInputElement | null;
    expect(sw).not.toBeNull();
    expect(sw?.checked).toBe(true);
  });

  it('renders enable_ocr disabled on scanned_document', () => {
    const s = schemaOf('scanned_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        default: true,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    const sw = document.getElementById(
      'dpp-enable_ocr',
    ) as HTMLInputElement | null;
    expect(sw?.disabled).toBe(true);
  });

  it('leaves enable_ocr editable on schemas not in the forced map', () => {
    const s = schemaOf('text_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    const sw = document.getElementById(
      'dpp-enable_ocr',
    ) as HTMLInputElement | null;
    expect(sw?.disabled).toBe(false);
  });

  it('renders the forced-OCR hint text on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    // I18n.t is mocked above to return the key. Asserting the key appears in
    // the rendered DOM proves the panel emitted a hint element bound to the
    // correct i18n key. The Tooltip stub also exposes its content via
    // data-content so the hover-tooltip path is exercised.
    expect(
      screen.queryAllByText(
        'datasets_createFileModel_rag_forced_ocr_hint',
        { exact: false },
      ).length,
    ).toBeGreaterThan(0);
    const tooltip = screen.queryByTestId('tooltip');
    expect(tooltip).not.toBeNull();
    expect(tooltip?.getAttribute('data-content')).toBe(
      'datasets_createFileModel_rag_forced_ocr_hint',
    );
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
npm run test -- dynamic-parsing-panel.test.tsx
```

Expected:
- "renders enable_ocr disabled on image_document" → FAIL (switch is currently enabled regardless of schema)
- "renders enable_ocr checked on image_document even when value says false" → FAIL (current code reads from `value` first, so it'll be unchecked)
- "renders enable_ocr disabled on scanned_document" → FAIL
- "leaves enable_ocr editable on schemas not in the forced map" → PASS (already the case today, useful as a regression test)
- "renders the forced-OCR hint text on image_document" → FAIL (no hint rendered today)

- [ ] **Step 3: Modify `dynamic-parsing-panel.tsx` to apply forced state**

Open `dynamic-parsing-panel.tsx`. Apply these three changes:

**Change 1 — imports.** Replace the existing imports block (lines 17-35) so that `Tooltip` and `ReactNode` are pulled in, plus `FORCED_PARAMS_BY_SCHEMA` from use-schemas:

```tsx
import { type FC, type ReactNode, useMemo } from 'react';

import { I18n } from '@coze-arch/i18n';
import {
  InputNumber,
  Input,
  Select,
  Switch,
  Tooltip,
  Typography,
} from '@coze-arch/coze-design';

import { CollapsePanel } from '@/components';

import { FORCED_PARAMS_BY_SCHEMA } from './use-schemas';
import { filterParamsByDependencies } from './validate';
import {
  type DocumentParameter,
  type DocumentParameterSchema,
  type DocumentOptionsValue,
} from './types';
```

**Change 2 — propagate forced info into the controls.** Replace the body of the `DynamicParsingPanel` component (currently lines 61-107) with:

```tsx
export const DynamicParsingPanel: FC<DynamicParsingPanelProps> = ({
  schema,
  value,
  onChange,
}) => {
  const forcedMap = FORCED_PARAMS_BY_SCHEMA[schema.schema_id] ?? {};

  const { visible, advanced } = useMemo(() => {
    // Drop params whose dependencies aren't satisfied (e.g. OCR knobs when
    // enable_ocr is off) before splitting into open/advanced — keeps the
    // form honest about which knobs actually affect the upload.
    const filtered = filterParamsByDependencies(
      schema.parameters,
      value,
      schema,
    );
    const visibleList: DocumentParameter[] = [];
    const advancedList: DocumentParameter[] = [];
    for (const p of filtered) {
      (p.advanced ? advancedList : visibleList).push(p);
    }
    return { visible: visibleList, advanced: advancedList };
  }, [schema, value.enable_ocr]);

  const handleFieldChange = (paramName: string, fieldValue: unknown): void => {
    // Forced params are non-interactive in the UI (disabled control), but
    // defend against programmatic invocations: ignore changes that would
    // overwrite a forced value.
    if (paramName in forcedMap) {
      return;
    }
    onChange({ ...value, [paramName]: fieldValue });
  };

  return (
    <div className="dynamic-parsing-panel">
      <GroupedFields
        params={visible}
        value={value}
        onChange={handleFieldChange}
        forcedMap={forcedMap}
      />
      {advanced.length > 0 ? (
        <CollapsePanel
          header={I18n.t('datasets_createFileModel_rag_advanced_params')}
        >
          <GroupedFields
            params={advanced}
            value={value}
            onChange={handleFieldChange}
            forcedMap={forcedMap}
          />
        </CollapsePanel>
      ) : null}
    </div>
  );
};
```

**Change 3 — render forced controls disabled with tooltip and hint.** Replace the `GroupedFields` and `FieldControl` components (currently lines 109-278) with:

```tsx
/**
 * Renders a flat parameter list with `parameter.group` headers inserted at
 * each group boundary. Order follows the input array — caller is responsible
 * for keeping rag's stable schema order (we don't sort to avoid surprising
 * the user with reshuffles between rag versions).
 *
 * `forcedMap` maps param name → forced value override. When a param is
 * forced, FieldControl renders it disabled with a Tooltip showing the
 * forced.reason i18n key; we also render the same hint as a Typography
 * line under the description so it's visible without hovering.
 */
const GroupedFields: FC<{
  params: DocumentParameter[];
  value: DocumentOptionsValue;
  onChange: (name: string, fieldValue: unknown) => void;
  forcedMap: Readonly<Record<string, { value: unknown; reason: string }>>;
}> = ({ params, value, onChange, forcedMap }) => {
  let lastGroup = '';
  return (
    <>
      {params.map(p => {
        const showHeader = p.group !== lastGroup;
        lastGroup = p.group;
        const forced = forcedMap[p.name];
        return (
          <div key={p.name} style={{ marginBottom: 12 }}>
            {showHeader ? (
              <Typography.Title
                heading={6}
                style={{ marginTop: 8, marginBottom: 4 }}
              >
                {p.group}
              </Typography.Title>
            ) : null}
            <FieldControl
              param={p}
              value={forced ? forced.value : value[p.name]}
              onChange={onChange}
              forced={forced}
            />
            {p.description ? (
              <Typography.Text type="tertiary" size="small">
                {p.description}
              </Typography.Text>
            ) : null}
            {forced ? (
              <Typography.Text
                type="warning"
                size="small"
                style={{ display: 'block' }}
              >
                {I18n.t(forced.reason)}
              </Typography.Text>
            ) : null}
          </div>
        );
      })}
    </>
  );
};

/**
 * Maps `parameter.ui_component` to a concrete control. Recognised:
 *
 *   - "switch"           -> <Switch />
 *   - "number"           -> <InputNumber />
 *   - "select"           -> <Select /> populated from `allowed_values`
 *   - "model-select"     -> editable <Input /> for now; the param value is
 *     a rag model_id (e.g. ocr_model_id="model-ocr-paddle-infer-text").
 *     Long-term this should be a dropdown sourced from
 *     /api/knowledge/rag/model_providers filtered by capability, but the
 *     current ListRagModelProviders endpoint only returns text/image
 *     embedding models — OCR/LLM/rerank entries aren't surfaced yet, so
 *     a free-text fallback unblocks the wizard until that's added.
 *   - "multi-select"     -> editable <Input /> accepting comma-separated
 *     values, parsed to string[] on submit. Same long-term note as above
 *     for a real tag-input control.
 *   - "text" / anything else -> editable <Input />.
 *
 * When `forced` is set, the control is disabled and wrapped in a Tooltip
 * showing the localised reason. The displayed value is the forced override
 * (passed in via `value`), not whatever the form state currently holds.
 */
const FieldControl: FC<{
  param: DocumentParameter;
  value: unknown;
  onChange: (name: string, fieldValue: unknown) => void;
  forced?: { value: unknown; reason: string };
}> = ({ param, value, onChange, forced }) => {
  const label = param.ui_label || param.name;
  const isDisabled = Boolean(forced);
  const wrap = (node: ReactNode): ReactNode =>
    forced ? <Tooltip content={I18n.t(forced.reason)}>{node}</Tooltip> : node;

  switch (param.ui_component) {
    case 'switch': {
      const current =
        typeof value === 'boolean' ? value : Boolean(param.default);
      return wrap(
        <label
          style={{ display: 'flex', alignItems: 'center', gap: 8 }}
          htmlFor={`dpp-${param.name}`}
        >
          <Switch
            id={`dpp-${param.name}`}
            checked={current}
            disabled={isDisabled}
            onChange={(checked: boolean) => onChange(param.name, checked)}
          />
          <span>{label}</span>
        </label>,
      );
    }
    case 'number': {
      const current =
        typeof value === 'number'
          ? value
          : (param.default as number | undefined);
      return wrap(
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <InputNumber
            value={current}
            min={param.min_value}
            max={param.max_value}
            disabled={isDisabled}
            onChange={(next: string | number) =>
              onChange(param.name, Number(next))
            }
          />
        </div>,
      );
    }
    case 'select': {
      const current =
        typeof value === 'string'
          ? value
          : (param.default as string | undefined);
      const options = (param.allowed_values ?? []).map(v => ({
        label: String(v),
        value: String(v),
      }));
      return wrap(
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Select
            value={current}
            optionList={options}
            disabled={isDisabled}
            onChange={next => onChange(param.name, next)}
          />
        </div>,
      );
    }
    case 'multi-select': {
      const arr = Array.isArray(value)
        ? (value as unknown[])
        : ((param.default as unknown[] | undefined) ?? []);
      const display = arr.map(v => String(v)).join(', ');
      return wrap(
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Input
            value={display}
            placeholder="e.g. zh, en"
            disabled={isDisabled}
            onChange={(next: string) => {
              const parts = next
                .split(',')
                .map(s => s.trim())
                .filter(Boolean);
              onChange(param.name, parts);
            }}
          />
        </div>,
      );
    }
    case 'model-select':
    case 'text':
    default: {
      const current =
        typeof value === 'string'
          ? value
          : ((param.default as string | undefined) ?? '');
      return wrap(
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Input
            value={current}
            disabled={isDisabled}
            onChange={(next: string) => onChange(param.name, next)}
          />
        </div>,
      );
    }
  }
};
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
npm run test -- dynamic-parsing-panel.test.tsx
```

Expected: all 5 panel tests pass. Also re-run the validate tests:

```bash
npm run test -- validate.test.ts
```

Expected: all validate tests still pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): render forced params as disabled + tooltip

image_document and scanned_document upload forms now show the
enable_ocr toggle as locked-on with a Tooltip and an inline warning
hint explaining why. Defends against programmatic overrides:
handleFieldChange ignores changes targeting forced params.

Other schemas (text/markdown/etc.) are unaffected — they don't appear
in FORCED_PARAMS_BY_SCHEMA.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: End-to-end smoke against the v2 stack

Rebuild the frontend bundle served by `coze-web-v2`, upload an image through the UI, verify rag persisted `enable_ocr=true`, and confirm the workflow knowledge-retrieve node returns a hit for a text query.

This task does NOT write code or commit anything by itself — it validates the previous tasks work in the running stack.

**Stack:**
- coze-web-v2: http://localhost:8891
- coze-server-v2: http://localhost:8892
- rag-web: http://localhost:8090 (tenant: `coze-v2`)
- mysql-v2: localhost:33061 (root/root, db `opencoze`)

---

- [ ] **Step 1: Rebuild the frontend bundle**

The `coze-web-v2` container serves a baked-in bundle. Check whether the container mounts the source for hot reload:

```bash
docker inspect coze-web-v2 --format '{{json .Mounts}}' | python3 -m json.tool
```

If the source is bind-mounted and HMR is active, no rebuild needed.

Otherwise run a frontend build from the repo root:

```bash
cd frontend/apps/coze-studio && npm run build && cd -
```

Then restart the web container:

```bash
docker compose -p coze-studio-v2 \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.yml \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.override.yml \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.v2.yml \
  restart coze-web-v2
```

Expected: container restarts cleanly. Verify with `docker ps --filter name=coze-web-v2`.

- [ ] **Step 2: Create a fresh image KB through the UI**

Open http://localhost:8891 in a browser. Log in. Create a new knowledge base with format type `Image`.

Record the new `coze_kb_id` (visible in the URL after creation or in the browser devtools network tab).

Verify the mapping was persisted correctly:

```bash
docker exec coze-mysql-v2 mysql -uroot -proot opencoze -e \
  "SELECT coze_kb_id, rag_kb_id, format_type FROM rag_kb_mapping ORDER BY created_at DESC LIMIT 1"
```

Expected: one row with `format_type=2` and a fresh `rag_kb_id` UUID. Save the rag_kb_id for later steps.

- [ ] **Step 3: Upload an image and verify the form lock works**

In the same KB, click "Upload" → choose an image file (any PNG/JPG with text on it, e.g., a screenshot of text). Step through the wizard.

At the "segment" step (where the OCR knobs live), observe:
- the `enable_ocr` toggle is checked AND disabled (greyed out)
- a warning-styled hint line is shown ("图片知识库需要开启 OCR..." or English equivalent depending on locale)
- hovering the toggle shows the same hint via Tooltip

Submit the upload. Wait for processing to reach "ready".

- [ ] **Step 4: Verify rag persisted enable_ocr=true and ocr_model_id**

Using the rag_kb_id from Step 2:

```bash
RAG_KB_ID=<paste-here>
curl -s "http://localhost:8090/api/v1/knowledgebases/${RAG_KB_ID}/documents?limit=5" \
  -H "X-Tenant-Id: coze-v2" \
  | python3 -m json.tool \
  | grep -E "enable_ocr|ocr_model_id|target_chunk_types|status"
```

Expected output lines include:
```
"status": "ready",
"enable_ocr": true,
"ocr_model_id": "model-ocr-paddle-infer-text",
"target_chunk_types": [..., "text_chunk", ...]
```

Critical: `enable_ocr` is `true`, `ocr_model_id` is non-empty, and `target_chunk_types` includes `text_chunk` (this is what makes the retrieve work).

- [ ] **Step 5: Smoke the workflow-style retrieval against the new KB**

Pick some text that should appear in your image (or any reasonable text query). From the repo root:

```bash
RAG_KB_ID=<paste-here>
QUERY="<pick text plausibly in the image>"
curl -s -X POST http://localhost:8090/api/v1/retrieval \
  -H "X-Tenant-Id: coze-v2" \
  -H "Content-Type: application/json" \
  -d "{\"kb_ids\":[\"${RAG_KB_ID}\"],\"query\":\"${QUERY}\",\"query_mode\":\"text_input\",\"top_k\":5}" \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('items:', len(d['data']['items'])); [print('  doc:', i.get('doc_name'), 'score:', round(i.get('score',0),3)) for i in d['data']['items']]"
```

Expected: `items: <1 or more>` with the uploaded document's filename and a score > 0.

If items is 0, check rag-web logs for the actual retrieve plan:

```bash
docker logs --tail 100 rag-web 2>&1 | grep -i "retriev\|kb_plan\|target_chunk"
```

Look for `target_chunk_types: [..., "text_chunk", ...]` in the resolved plan. If text_chunk is absent, the upload didn't actually produce text — re-check Step 4.

- [ ] **Step 6: Final sanity — run all unit tests one more time**

From the package dir:

```bash
cd frontend/packages/data/knowledge/knowledge-resource-processor-base
npm run test
```

Expected: all tests pass (validate.test.ts + dynamic-parsing-panel.test.tsx + any preexisting tests in the package).

- [ ] **Step 7: No commit needed for this task**

Step 5 is verification, not code change. If you noticed any issue in the running stack, go back to whichever earlier task it points at, fix it, re-run the unit tests, and then re-run Step 5.

---

## Definition of Done

- All new unit tests pass (`applyForcedParams` × 5, `mergeSchemaDefaults` forced cases × 5, `DynamicParsingPanel` forced rendering × 5).
- All preexisting tests in the package still pass.
- Locale JSON files still parse.
- End-to-end smoke (Task 5): a freshly-created image KB ingests an image with `enable_ocr=true / ocr_model_id=<default>` and a text-input retrieve against it returns ≥ 1 hit.
- Four commits in this order:
  1. `feat(knowledge-rag): add FORCED_PARAMS_BY_SCHEMA + applyForcedParams`
  2. `feat(knowledge-rag): wire forced enable_ocr=true into mergeSchemaDefaults`
  3. `feat(i18n): add datasets_createFileModel_rag_forced_ocr_hint`
  4. `feat(knowledge-rag): render forced params as disabled + tooltip`

---

## Task 6: Hide image_chunk config (extend forced map with hidden flag)

Extends `FORCED_PARAMS_BY_SCHEMA` entry type from a single shape into a discriminated union: visible entries still get rendered disabled with Tooltip+warning (Task 4 behavior); hidden entries skip rendering entirely while still forcing the wire value via `applyForcedParams`. Adds `enable_image_embedding=false` and `produce_image_chunk=false` to both image-bearing schemas — kills image_chunk production since the workflow knowledge-retrieve node can't consume them.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx`

(No changes to `validate.ts` — `applyForcedParams` already handles hidden entries identically. No new i18n keys — hidden entries don't render text.)

---

- [ ] **Step 1: Write failing tests for `applyForcedParams` with new hidden entries**

In `validate.test.ts`, inside the existing `describe('applyForcedParams', ...)` block, append before the closing `});`:

```ts
  it('forces enable_image_embedding=false on image_document', () => {
    expect(
      applyForcedParams('image_document', { enable_image_embedding: true }),
    ).toEqual({
      enable_image_embedding: false,
      enable_ocr: true,
    });
  });

  it('forces produce_image_chunk=false on image_document', () => {
    expect(
      applyForcedParams('image_document', { produce_image_chunk: true }),
    ).toEqual({
      produce_image_chunk: false,
      enable_ocr: true,
    });
  });

  it('forces enable_image_embedding=false and produce_image_chunk=false on scanned_document', () => {
    expect(
      applyForcedParams('scanned_document', {
        enable_image_embedding: true,
        produce_image_chunk: true,
      }),
    ).toEqual({
      enable_image_embedding: false,
      produce_image_chunk: false,
      enable_ocr: true,
    });
  });
```

Then inside the `describe('mergeSchemaDefaults', ...)` block, append before the closing `});`:

```ts
  it('strips all image_chunk-related defaults on image_document', () => {
    // image_document's rag-side defaults: enable_image_embedding=true,
    // produce_image_chunk=true. Without the hidden forced entries those
    // would propagate to the wire and produce useless image_chunks. With
    // the lock, both end up false.
    const s = imageSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'paddle',
      }),
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: true,
      }),
      param({
        name: 'produce_image_chunk',
        ui_component: 'switch',
        default: true,
      }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({
      enable_ocr: true,
      ocr_model_id: 'paddle',
      enable_image_embedding: false,
      produce_image_chunk: false,
    });
  });
```

- [ ] **Step 2: Run tests to verify they fail**

From the package dir:
```bash
npm run test -- validate.test.ts
```
Expected: 4 new tests fail (the forced map doesn't have these entries yet). Existing tests pass.

- [ ] **Step 3: Extend the forced map type and add the new entries**

In `use-schemas.ts`, replace the existing `FORCED_PARAMS_BY_SCHEMA` declaration with the discriminated union form plus the new entries:

```ts
// Params whose value is locked, regardless of user input or schema default.
// Keyed by rag schema_id, then by param.name.
//
// Why: rag's image_document schema declares enable_ocr default=false, but
// coze's workflow knowledge-retrieve node only does text-in/text-out, so an
// OCR-off image upload silently produces a KB the node cannot retrieve from.
// Force OCR on at the frontend so the natural upload UX produces text_chunks.
//
// Two entry forms:
//   - visible (omits hidden, requires reason): control renders disabled with
//     a Tooltip + inline warning showing I18n.t(reason)
//   - hidden (hidden: true, no reason): control is not rendered at all; the
//     wire value is still pinned via applyForcedParams
//
// image_chunk-related entries (enable_image_embedding, produce_image_chunk)
// are hidden because the workflow knowledge-retrieve node can't consume
// image_chunks — producing them is pure waste in this UX.
type ForcedParamEntry =
  | { value: unknown; reason: string; hidden?: false }
  | { value: unknown; hidden: true };

export const FORCED_PARAMS_BY_SCHEMA: Readonly<
  Record<string, Readonly<Record<string, Readonly<ForcedParamEntry>>>>
> = {
  image_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
  },
  scanned_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
  },
};
```

- [ ] **Step 4: Run tests — wire-level tests should now pass**

```bash
npm run test -- validate.test.ts
```
Expected: all tests pass (the 3 new applyForcedParams tests + the new mergeSchemaDefaults test go green). `applyForcedParams` is generic over entry shape — it ignores the `reason` / `hidden` fields and only uses `value`, so no code change needed there.

If any existing test now fails, stop and report — the type widening may have broken something.

- [ ] **Step 5: Write failing tests for panel hide behavior**

In `dynamic-parsing-panel.test.tsx`, inside the existing `describe('DynamicParsingPanel forced params', ...)` block, append:

```ts
  it('hides enable_image_embedding control on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        group: 'image_chunking',
        default: true,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    // The mock Switch renders an <input type="checkbox" id="dpp-...">.
    // For a hidden forced param, no such input should exist.
    expect(document.getElementById('dpp-enable_image_embedding')).toBeNull();
  });

  it('hides produce_image_chunk control on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'produce_image_chunk',
        ui_component: 'switch',
        group: 'chunk_outputs',
        default: true,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    expect(document.getElementById('dpp-produce_image_chunk')).toBeNull();
  });

  it('hides both image-chunk controls on scanned_document', () => {
    const s = schemaOf('scanned_document', [
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        group: 'image_chunking',
        default: false,
      }),
      param({
        name: 'produce_image_chunk',
        ui_component: 'switch',
        group: 'chunk_outputs',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    expect(document.getElementById('dpp-enable_image_embedding')).toBeNull();
    expect(document.getElementById('dpp-produce_image_chunk')).toBeNull();
  });

  it('does NOT hide enable_image_embedding on unforced schemas', () => {
    // Regression guard: hide policy must be schema-keyed. A future text
    // schema with the same param name should not be affected.
    const s = schemaOf('text_document', [
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    expect(
      document.getElementById('dpp-enable_image_embedding'),
    ).not.toBeNull();
  });
```

- [ ] **Step 6: Run panel tests to verify they fail**

```bash
npm run test -- dynamic-parsing-panel.test.tsx
```
Expected: the 4 new tests fail (panel currently renders all forced entries; doesn't know about `hidden`). Existing panel tests still pass.

- [ ] **Step 7: Modify the panel to skip hidden forced entries**

In `dynamic-parsing-panel.tsx`, find `GroupedFields` (the component that iterates params and renders controls). In the `.map(p => ...)` callback, immediately after the existing `const forced = forcedMap[p.name];` line, add a hidden-check that returns early:

```tsx
const forced = forcedMap[p.name];
// Hidden forced params are skipped entirely — no control, no description,
// no warning Typography. applyForcedParams still pins their wire value.
if (forced && 'hidden' in forced && forced.hidden) {
  return null;
}
```

Then update the `forcedMap` prop's type on `GroupedFields` to match the new union (so the type narrows correctly downstream). Replace the prop type from:

```tsx
forcedMap: Readonly<Record<string, { value: unknown; reason: string }>>;
```

to:

```tsx
forcedMap: Readonly<
  Record<
    string,
    | { value: unknown; reason: string; hidden?: false }
    | { value: unknown; hidden: true }
  >
>;
```

And update `FieldControl`'s `forced?` prop the same way:

```tsx
forced?:
  | { value: unknown; reason: string; hidden?: false }
  | { value: unknown; hidden: true };
```

(Since hidden entries are filtered out in `GroupedFields` before reaching `FieldControl`, the `wrap()` helper will only see the visible shape — but the type allows either.)

Inside `FieldControl`, the `wrap()` function currently reads `forced.reason`. With the union, `forced.reason` is only present on the visible variant. Guard the access:

```tsx
const wrap = (node: ReactNode): ReactNode =>
  forced && !('hidden' in forced && forced.hidden) && 'reason' in forced
    ? <Tooltip content={I18n.t(forced.reason)}>{node}</Tooltip>
    : node;
```

(Simpler: `FieldControl` only ever receives visible forced entries since `GroupedFields` filters; the guard is defensive but correct.)

In `GroupedFields`, the warning Typography line currently reads `I18n.t(forced.reason)`. Add the same guard there — but since hidden entries already returned early, the access is safe:

```tsx
{forced && 'reason' in forced ? (
  <Typography.Text type="warning" size="small" style={{ display: 'block' }}>
    {I18n.t(forced.reason)}
  </Typography.Text>
) : null}
```

- [ ] **Step 8: Run all tests — everything should pass**

```bash
npm run test
```
Expected: all tests pass (was 49, now ~57 = 49 + 4 panel + 4 wire-level — adjust based on actual count). 0 failures.

- [ ] **Step 9: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): hide image_chunk config + force off

image_document and scanned_document uploads no longer expose
enable_image_embedding / produce_image_chunk in the form, and the wire
payload pins both to false. The workflow knowledge-retrieve node is
text-in/text-out and can't consume image_chunks; producing them is
waste (CLIP inference + Milvus vector storage).

Architecture: extends FORCED_PARAMS_BY_SCHEMA entry type into a
discriminated union. Visible entries (existing enable_ocr) render
disabled with Tooltip; hidden entries skip rendering entirely while
applyForcedParams still pins the wire value.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Lock produce_text_chunk symmetrically (defense + UX)

Adds `produce_text_chunk: { value: true, hidden: true }` to both `image_document` and `scanned_document`. Reuses Task 6's hidden-entry architecture — no new types or rendering paths. Wire payload becomes symmetric (text side: enable_ocr=true + produce_text_chunk=true; image side: enable_image_embedding=false + produce_image_chunk=false). Removes a no-op toggle from the UI. Defends against rag dropping its OR-fallback semantics.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx`

(No panel.tsx change — Task 6's hide path already handles any hidden entry.)

---

- [ ] **Step 1: Write failing tests**

In `validate.test.ts`, inside `describe('applyForcedParams', ...)`, append before the closing `});`:

```ts
  it('forces produce_text_chunk=true on image_document', () => {
    expect(
      applyForcedParams('image_document', { produce_text_chunk: false }),
    ).toEqual({
      enable_ocr: true,
      enable_image_embedding: false,
      produce_image_chunk: false,
      produce_text_chunk: true,
    });
  });

  it('forces produce_text_chunk=true on scanned_document', () => {
    expect(
      applyForcedParams('scanned_document', { produce_text_chunk: false }),
    ).toEqual({
      enable_ocr: true,
      enable_image_embedding: false,
      produce_image_chunk: false,
      produce_text_chunk: true,
    });
  });
```

Then inside `describe('mergeSchemaDefaults', ...)`, append before its closing `});`:

```ts
  it('overrides schema-default produce_text_chunk=false to true on image_document', () => {
    // image_document declares produce_text_chunk default=false on the rag side.
    // Without the force, our wire payload would carry false — rag's OR fallback
    // (enable_ocr=true rescues it) is the only reason text_chunk still gets
    // produced today. Pin it explicitly so the wire reflects intent.
    const s = imageSchema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'paddle',
      }),
      param({
        name: 'produce_text_chunk',
        ui_component: 'switch',
        default: false,
      }),
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: true,
      }),
      param({
        name: 'produce_image_chunk',
        ui_component: 'switch',
        default: true,
      }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({
      enable_ocr: true,
      ocr_model_id: 'paddle',
      produce_text_chunk: true,
      enable_image_embedding: false,
      produce_image_chunk: false,
    });
  });
```

In `dynamic-parsing-panel.test.tsx`, inside `describe('DynamicParsingPanel forced params', ...)`, append:

```ts
  it('hides produce_text_chunk control on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'produce_text_chunk',
        ui_component: 'switch',
        group: 'chunk_outputs',
        default: false,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    expect(document.getElementById('dpp-produce_text_chunk')).toBeNull();
  });

  it('hides produce_text_chunk control on scanned_document', () => {
    const s = schemaOf('scanned_document', [
      param({
        name: 'produce_text_chunk',
        ui_component: 'switch',
        group: 'chunk_outputs',
        default: true,
      }),
    ]);
    render(
      <DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />,
    );
    expect(document.getElementById('dpp-produce_text_chunk')).toBeNull();
  });
```

- [ ] **Step 2: Run tests → expect 5 new failures**

```bash
cd /home/xinyuliu/coze-studio/frontend/packages/data/knowledge/knowledge-resource-processor-base
npm run test
```

Expected: 5 new failures (3 in validate.test.ts, 2 in dynamic-parsing-panel.test.tsx). Existing tests still pass.

- [ ] **Step 3: Add the forced entries**

In `use-schemas.ts`, find the existing `FORCED_PARAMS_BY_SCHEMA` const. Inside each of the `image_document` and `scanned_document` entries, append a new line `produce_text_chunk: { value: true, hidden: true },` so they look like:

```ts
  image_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
    produce_text_chunk: { value: true, hidden: true },
  },
  scanned_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
    produce_text_chunk: { value: true, hidden: true },
  },
```

- [ ] **Step 4: Run all tests → expect green**

```bash
npm run test
```

Expected: all tests pass. The panel hide logic is already in place from Task 6 — adding a new hidden entry to the map just plugs in.

- [ ] **Step 5: Commit**

```bash
cd /home/xinyuliu/coze-studio
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): lock produce_text_chunk=true on image schemas

Symmetric counterpart to the image_chunk hide+force (Task 6). The wire
payload now explicitly carries produce_text_chunk=true on image_document
and scanned_document instead of relying on rag's OR-fallback
(wants_text = produce_text_chunk OR enable_ocr) to compensate for the
schema's default=false.

Wire shape is now internally consistent: text side double-locked
(enable_ocr=true + produce_text_chunk=true), image side double-locked
(enable_image_embedding=false + produce_image_chunk=false).

The toggle is also hidden from the upload form — it was a noop under
the forced enable_ocr=true regime.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Persist image URL in rag_doc_mapping during upload

Adds a new column `image_url` to `rag_doc_mapping` and populates it during `CreateDocument` for image-bearing schemas. No behavioral change visible in the UI yet — that lands in Task 9.

**Files:**
- Modify: `docker/atlas/opencoze_latest_schema.hcl` (add image_url column to rag_doc_mapping)
- Create: `docker/atlas/migrations/20260520140000_add_image_url_to_rag_doc_mapping.sql` (informational mirror; not read at deploy per `[[coze-stack-atlas-declarative-deploy]]`)
- Modify: `backend/domain/knowledge/service/ragimpl/mapping.go` (DocMapping struct, InsertDoc signature, hydrators)
- Modify: `backend/domain/knowledge/service/ragimpl/document.go` (CreateDocument forks file bytes to coze MinIO for image schemas, persists URL via InsertDoc)
- Modify: `backend/domain/knowledge/service/ragimpl/mapping_test.go` (test InsertDoc round-trip with image_url)
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` and `integration_test.go` (storage stubs already there)

---

- [ ] **Step 1: Add `image_url` column to declarative atlas schema**

In `docker/atlas/opencoze_latest_schema.hcl`, find the `rag_doc_mapping` table block (starts around line 1884). After the `size` column (line 1916-1922) and before the `created_at` column (line 1923), insert:

```hcl
  column "image_url" {
    null    = true
    type    = varchar(512)
    comment = "Coze-side MinIO URL for image-source documents (for detail-page thumbnails). NULL for non-image docs and for pre-2026-05-20 image uploads."
  }
```

- [ ] **Step 2: Add informational migration file**

Create `docker/atlas/migrations/20260520140000_add_image_url_to_rag_doc_mapping.sql`:

```sql
-- Add image_url column to rag_doc_mapping.
-- Note: per coze-stack-atlas-declarative-deploy, this file is informational
-- only — mysql entrypoint runs atlas schema apply against the HCL, not this
-- migration. Kept here so an operator can reproduce the change manually if
-- needed.

ALTER TABLE `rag_doc_mapping`
  ADD COLUMN `image_url` VARCHAR(512) NULL DEFAULT NULL
    COMMENT 'Coze-side MinIO URL for image-source documents (for detail-page thumbnails). NULL for non-image docs and for pre-2026-05-20 image uploads.'
    AFTER `size`;
```

(Don't update `atlas.sum` — per the same memory it isn't read at deploy.)

- [ ] **Step 3: Apply schema change to running mysql**

The dev stack has a running mysql instance. Apply the change:

```bash
docker compose -p coze-studio-v2 \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.yml \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.override.yml \
  -f /home/xinyuliu/coze-studio/docker/docker-compose.v2.yml \
  restart coze-mysql-v2
```

(Restart triggers the atlas-declarative entrypoint to diff and apply the new column.)

Verify the column exists:

```bash
docker exec coze-mysql-v2 mysql -uroot -proot opencoze -e "SHOW CREATE TABLE rag_doc_mapping\G" | grep image_url
```

Expected output: a line like `\`image_url\` varchar(512) DEFAULT NULL COMMENT 'Coze-side MinIO URL for image-source documents...'`.

- [ ] **Step 4: Update DocMapping struct and InsertDoc signature**

In `backend/domain/knowledge/service/ragimpl/mapping.go`:

Find the `DocMapping` struct (search for "type DocMapping struct"). Add an `ImageURL` field:

```go
ImageURL string  // Coze MinIO URL for image-source docs; empty for others
```

Find `InsertDoc` (currently around line 324):

```go
func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, nowMs int64, size int64) error {
```

Change the signature to accept image_url at the end:

```go
func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, nowMs int64, size int64, imageURL string) error {
```

Inside the function, find the SQL INSERT and append `image_url` to the column list and `imageURL` to the values bind list.

Find any read paths (search for `DocByCozeID`, `DocsByCozeIDs`, `DocByRagID`, or SELECT statements) — add `image_url` to the SELECT list and Scan it into the `ImageURL` field.

- [ ] **Step 5: Update CreateDocument to fork file bytes to coze MinIO for image schemas**

In `backend/domain/knowledge/service/ragimpl/document.go`, find `CreateDocument` (line 242). Around line 309-310 where `ragReq := &contract.CreateDocumentRequest{ FileBytes: fileBytes, ... }` is constructed and before `ragResp, err := i.rag.CreateDocument(...)` at line 321.

Add an image-bytes fork BEFORE the rag call. The image-detection check: schema is image-bearing if the FileExtension matches one of `{png, jpg, jpeg, gif, webp, bmp, tiff}` OR the document_options carry `_source_modality` in `{image_source, scanned_document_source}`. Helper:

```go
func isImageBearing(ent *entity.Document) bool {
    ext := strings.ToLower(string(ent.FileExtension))
    switch ext {
    case "png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff":
        return true
    }
    return false
}
```

(Add this helper at file scope.)

Then in the CreateDocument loop, after `fileBytes` is computed:

```go
var imageURL string
if isImageBearing(req.DocumentEntities[i]) {
    objectKey := fmt.Sprintf("knowledge/image/%d/%s", m.CozeKBID, req.DocumentEntities[i].Name)
    if err := i.storage.PutObject(ctx, objectKey, fileBytes); err != nil {
        // Defense in depth: thumbnail is UX, ingestion is primary. Log and continue.
        logs.CtxWarnf(ctx, "ragimpl.CreateDocument: failed to store image to MinIO for thumbnail, continuing without URL: doc=%s err=%v", req.DocumentEntities[i].Name, err)
    } else {
        url, urlErr := i.storage.GetObjectUrl(ctx, objectKey)
        if urlErr != nil {
            logs.CtxWarnf(ctx, "ragimpl.CreateDocument: stored image but failed to get URL, continuing without URL: doc=%s err=%v", req.DocumentEntities[i].Name, urlErr)
        } else {
            imageURL = url
        }
    }
}
```

Then in the InsertDoc call later in the function, pass `imageURL` as the new last argument.

- [ ] **Step 6: Update existing call sites and tests**

Search the repo for other call sites of `MappingRepo.InsertDoc`:

```bash
grep -rn "mapping\.InsertDoc\|m\.InsertDoc\b\|\.InsertDoc(ctx" backend/ | grep -v _test.go
```

For each call site, pass `""` (empty string) as the new last argument unless it's the CreateDocument path you just updated.

Update mapping_test.go to add a round-trip test:

```go
func TestMappingRepo_InsertDoc_RoundTripsImageURL(t *testing.T) {
    // ... seed and call InsertDoc with imageURL="https://minio.example/x.png"
    // ... assert DocByCozeID returns ImageURL field equal to that string
}
```

(Adapt to the existing test fixture style — look at sibling tests for setup.)

- [ ] **Step 7: Run backend tests**

```bash
cd /home/xinyuliu/coze-studio/backend
go test ./domain/knowledge/service/ragimpl/...
```

Expected: all existing tests pass, new InsertDoc test passes.

- [ ] **Step 8: Commit**

```bash
cd /home/xinyuliu/coze-studio
git add docker/atlas/opencoze_latest_schema.hcl \
        docker/atlas/migrations/20260520140000_add_image_url_to_rag_doc_mapping.sql \
        backend/domain/knowledge/service/ragimpl/mapping.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/mapping_test.go
git commit -m "$(cat <<'EOF'
feat(knowledge-rag): persist image_url in rag_doc_mapping on upload

Adds image_url column to rag_doc_mapping. CreateDocument forks file
bytes to coze MinIO for image-bearing schemas (png/jpg/etc) and stores
the resulting URL in the mapping.

Rag's contract.Document has no URL field — image bytes live on rag's
internal storage. The detail page (Task 9) needs thumbnails, so coze
keeps a parallel copy in MinIO.

Failures in MinIO put/url are logged and don't abort ingestion —
thumbnail is UX, ingestion correctness is primary.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Rewrite ListPhotoSlice + populate Document.URL — make detail page work

Replaces chunk-based pagination with document-based pagination in ragimpl and reads `image_url` from the mapping into `entity.Document.URL`. After this commit the image KB detail page renders all uploaded images with thumbnails (for post-fix uploads) and filename captions.

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/slice.go` (ListPhotoSlice rewrite)
- Modify: `backend/domain/knowledge/service/ragimpl/document.go` (buildDocumentEntity reads ImageURL)
- Modify: `backend/domain/knowledge/service/ragimpl/slice_test.go` or create if not present
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go` (buildDocumentEntity URL test)

---

- [ ] **Step 1: Write failing tests for ListPhotoSlice**

In `backend/domain/knowledge/service/ragimpl/slice_test.go` (create if absent, or append to existing), add:

```go
func TestRagimplListPhotoSlice_ReturnsOneSlicePerDocument(t *testing.T) {
    // Given a KB with 3 documents (rag_doc_id 1, 2, 3), each with 10+
    // chunks of various types:
    //   - doc 1: 13 text_chunks, 0 image_chunks
    //   - doc 2: 60 text_chunks, 0 image_chunks
    //   - doc 3: 6 text_chunks, 1 image_chunk
    // When ListPhotoSlice(KB, limit=20, offset=0) is called,
    // Then the result has exactly 3 slices, one per document.
    // And each slice's RawContent[0].Text equals the document filename.
    // ...

    // (Adapt to existing test fixture style. Use the rag client mock from
    // sibling tests. The key assertion is "3 slices, not 80 chunks".)
}

func TestRagimplListPhotoSlice_HonorsLimitOffset(t *testing.T) {
    // Given 5 documents, when limit=2 offset=0 -> 2 slices; offset=2 -> 2 more; offset=4 -> 1.
}

func TestRagimplListPhotoSlice_IgnoresHasCaptionFilter(t *testing.T) {
    // Given req.HasCaption=ptr.Of(false), still returns documents (filter is a no-op in rag mode).
}
```

- [ ] **Step 2: Write failing test for buildDocumentEntity URL population**

In `document_test.go` (sibling to `mapping_test.go`), add:

```go
func TestBuildDocumentEntity_PopulatesURLFromMappingImageURL(t *testing.T) {
    // Given a DocMapping with ImageURL="https://minio.example/img.png",
    // When buildDocumentEntity(mapping, ragDoc) is called,
    // Then result.URL == "https://minio.example/img.png".
}

func TestBuildDocumentEntity_LeavesURLEmptyWhenMappingImageURLEmpty(t *testing.T) {
    // Given ImageURL="",
    // When buildDocumentEntity is called,
    // Then result.URL == "".
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd /home/xinyuliu/coze-studio/backend
go test ./domain/knowledge/service/ragimpl/... -run "ListPhotoSlice|buildDocumentEntity"
```

Expected: tests fail because the new behavior isn't implemented yet.

- [ ] **Step 4: Rewrite ListPhotoSlice**

In `backend/domain/knowledge/service/ragimpl/slice.go`, find `ListPhotoSlice` (around line 540-643). Replace it with:

```go
// ListPhotoSlice returns one synthetic Slice per document in the given KB,
// not one slice per chunk. This is the data shape the image KB detail page
// expects (1 image = 1 card). The synthetic slice's content is the document
// filename — used by packPhotoInfo as the card caption.
//
// HasCaption filter: ignored in rag mode. The legacy "user-typed annotation"
// concept doesn't map onto rag-backed documents; all rag image docs are
// considered "captioned" via their OCR-extracted text_chunks.
func (i *Impl) ListPhotoSlice(ctx context.Context, req *service.ListPhotoSliceRequest) (*service.ListPhotoSliceResponse, error) {
    m, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
    if err != nil {
        return nil, err
    }
    if m == nil {
        return &service.ListPhotoSliceResponse{}, nil
    }

    tenant, err := i.tenantFor(ctx)
    if err != nil {
        return nil, err
    }

    limit := 20
    if req.Limit != nil && *req.Limit > 0 {
        limit = *req.Limit
    }
    offset := 0
    if req.Offset != nil && *req.Offset > 0 {
        offset = *req.Offset
    }

    listResp, err := i.rag.ListDocuments(ctx, tenant, m.RagKBID, &contract.ListDocumentsRequest{
        Limit:  limit,
        Offset: offset,
    })
    if err != nil {
        return nil, err
    }

    if len(listResp.Items) == 0 {
        return &service.ListPhotoSliceResponse{Total: int64(listResp.Total)}, nil
    }

    // Resolve coze_doc_id for each rag document.
    ragIDs := make([]string, 0, len(listResp.Items))
    for _, d := range listResp.Items {
        ragIDs = append(ragIDs, d.DocID)
    }
    docMappings, err := i.mapping.DocsByRagIDs(ctx, ragIDs)
    if err != nil {
        return nil, err
    }
    cozeIDByRag := make(map[string]int64, len(docMappings))
    for _, dm := range docMappings {
        cozeIDByRag[dm.RagDocID] = dm.CozeDocID
    }

    slices := make([]*entity.Slice, 0, len(listResp.Items))
    for _, d := range listResp.Items {
        cozeID, ok := cozeIDByRag[d.DocID]
        if !ok {
            // Orphan rag doc (no mapping row). Skip — listPhoto would fail to
            // resolve its parent document anyway.
            continue
        }
        filename := d.Filename
        slices = append(slices, &entity.Slice{
            DocumentID: cozeID,
            RawContent: []*model.SliceContent{
                {Type: model.SliceContentTypeText, Text: &filename},
            },
        })
    }
    return &service.ListPhotoSliceResponse{
        Slices: slices,
        Total:  int64(listResp.Total),
    }, nil
}
```

(Note: this assumes `i.rag.ListDocuments(...)` exists with that signature. If the signature differs, adapt to the actual rag client API — search `backend/infra/rag/client.go` for the ListDocuments method.)

(Also: if `i.mapping.DocsByRagIDs` doesn't exist, add it as a batch lookup in `mapping.go` mirroring `KBsByCozeIDs` from prior work.)

- [ ] **Step 5: Populate URL in buildDocumentEntity**

In `backend/domain/knowledge/service/ragimpl/document.go`, find `buildDocumentEntity` (around line 195-209). It currently does NOT set `URL`. Add:

```go
ent.URL = mapping.ImageURL
```

(Inserted after the existing field assignments. If `ent` isn't the variable name in the actual code, use whatever the function builds and returns.)

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd /home/xinyuliu/coze-studio/backend
go test ./domain/knowledge/service/ragimpl/...
```

Expected: all tests pass.

- [ ] **Step 7: End-to-end verification against the running stack**

Restart coze-server-v2 to pick up the Go binary changes:

```bash
cd /home/xinyuliu/coze-studio
docker compose -p coze-studio-v2 \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.override.yml \
  -f docker/docker-compose.v2.yml \
  restart coze-server-v2
```

Wait ~10 seconds for the server to come back up. Open the image KB detail page in the browser at http://localhost:8891 against the new image KB (`2ec6be92-...` with 3 docs from today's testing). Expected:

- All 3 image cards appear (not just 1)
- Each card shows the filename as caption (not "未标注")
- Thumbnails are empty for these 3 docs (uploaded before Task 8 took effect)

To verify thumbnails work on new uploads:

- Upload a fresh image to the same KB via the UI
- Reload the detail page
- The new image's card should have a thumbnail

- [ ] **Step 8: Commit**

```bash
git add backend/domain/knowledge/service/ragimpl/slice.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/slice_test.go \
        backend/domain/knowledge/service/ragimpl/document_test.go \
        backend/domain/knowledge/service/ragimpl/mapping.go
git commit -m "$(cat <<'EOF'
fix(knowledge-rag): make image KB detail page work end-to-end

ListPhotoSlice in ragimpl no longer queries by image_chunk (which our
Task 6 force suppressed) — it now paginates rag documents directly and
returns one synthetic slice per document. The synthetic slice's content
is the document filename, which packPhotoInfo uses as the photo card
caption — replacing the misleading "未标注" warning.

buildDocumentEntity populates Document.URL from rag_doc_mapping.image_url
(persisted by Task 8 for image uploads). Pre-Task-8 uploads keep an
empty URL; their cards render without thumbnails but with filename
captions and correct listing.

Three symptoms fixed:
  1. Only 1 image shown -> all docs now appear
  2. No thumbnail -> populated for new uploads, empty for pre-fix
  3. "未标注" warning -> replaced by filename caption

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```
