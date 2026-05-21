# Image KB: collapse image_document / scanned_document modality choice

**Date:** 2026-05-21
**Author:** xinyu.liu@vorteklab.com
**Status:** Design — pending user review

## Problem

When uploading to an image KB, `features/knowledge-type/image/file/add-rag/steps/segment/index.tsx` runs `matchSchemasForFile(schemas, fileType)` to find rag schemas that accept the file's extension. For image file types (jpg / png / webp / ...) both `image_document` and `scanned_document` declare matching `file_types`, so `candidateSchemas.length === 2` and the segment step renders a `<Select>` ("图像" vs "扫描件") at line 151-173.

In rag's own definitions (`document_parameter_catalog.py`) the two schemas have ~10 identically-named user-facing parameters and differ only in default values:

| Param | image default | scanned default |
|---|---|---|
| `enable_ocr` | False | True |
| `enable_image_embedding` | True | False |
| `produce_text_chunk` | False | True |
| `produce_image_chunk` | True | False |

The 2026-05-20 image-upload-force-ocr-on spec then layered an identical `FORCED_PARAMS_BY_SCHEMA` map on both schemas (`use-schemas.ts:75-92`):

```ts
image_document: {
  enable_ocr: { value: true, ... },
  enable_image_embedding: { value: false, hidden: true },
  produce_image_chunk: { value: false, hidden: true },
  produce_text_chunk: { value: true, hidden: true },
},
scanned_document: {  // identical
  enable_ocr: { value: true, ... },
  enable_image_embedding: { value: false, hidden: true },
  produce_image_chunk: { value: false, hidden: true },
  produce_text_chunk: { value: true, hidden: true },
},
```

After the forces, the two schemas produce **byte-identical wire payloads** for the same user input. The `scanned_document`-only `ocr_render_dpi` parameter is `internal=True` (not surfaced in the UI) and ships the same default 150 either way. `_chunk_size_spec("ocr_text")` is shared. The Select is a no-op choice — user picks left or right, downstream behavior is exactly the same — and trivially confusing ("是图像还是扫描件呢?对答案无意义").

## Goal

In the image KB upload step, the modality `<Select>` no longer renders. The flow auto-picks `image_document` and proceeds straight into the dynamic parsing panel. Behavior is identical to the existing `image_document` path post-force.

## Non-Goals

- **No change to `matchSchemasForFile`.** That utility correctly answers "which rag schemas accept this file extension?" — text KB upload of a PDF still legitimately matches `pdf_text_document` AND `scanned_document` (those genuinely differ in OCR cost/quality tradeoffs). The fix is at the call site, not the utility.
- **No change to `FORCED_PARAMS_BY_SCHEMA`.** Future divergence between image_document and scanned_document forces (e.g. a future image-side caption param) remains possible without first un-doing this work; the entries stay.
- **No change to rag's catalog.** Read-only.
- **No change to the text KB segment step.** That has the same code shape and the same `matchSchemasForFile` call but its multi-candidate selector is meaningful (text PDF vs scanned PDF have real-effect differences). Out of scope.
- **No removal of `_source_modality` override plumbing.** The override mechanism stays — we just stop triggering it from the image KB path (the canonical schema's primary modality `image_source` is the only one ever active, so the override would be a self-no-op).

## Design

### Filter to the canonical schema

`features/knowledge-type/image/file/add-rag/steps/segment/index.tsx` — in the `useMemo` that produces `candidateSchemas` (currently lines 80-85):

```tsx
const candidateSchemas = useMemo<DocumentParameterSchema[]>(() => {
  if (!schemas || !fileType) {
    return [];
  }
  // Image KB uploads only ever materially behave as image_document after
  // the FORCED_PARAMS_BY_SCHEMA layer; scanned_document is byte-equivalent
  // post-force. Drop the dead choice to remove a confusing radio without
  // changing any downstream wire shape.
  return matchSchemasForFile(schemas, fileType).filter(
    s => s.schema_id === 'image_document',
  );
}, [schemas, fileType]);
```

With `candidateSchemas.length === 1`, the Select block (currently rendered when `length > 1`) is automatically skipped. `activeSchema` falls through to `candidateSchemas[0]` via the existing chained nullish-coalesce on line 89.

### `_source_modality` injection

The current `handleNext` (lines 113-119) injects `_source_modality` into `document_options` only when the user picked a non-first schema:

```tsx
if (
  activeSchema &&
  candidateSchemas.length > 1 &&
  activeSchema.schema_id !== candidateSchemas[0].schema_id
) {
  payload._source_modality = activeSchema.source_modalities[0];
}
```

With `length > 1` always false post-filter, this branch is dead. Leave it untouched — removing it would be unrelated cleanup and the gate condition is already correctly defensive (it self-disables when the filter narrows). When the text KB segment step replicates this pattern, the same code shape continues to work for it.

### `isScannedSchema` hint

Line 130 + 174-178 currently shows a hint "将使用扫描件解析（含 OCR）" when the user picks scanned. With the schema fixed to `image_document`, `isScannedSchema` is always false and the hint never shows. Leave untouched — same self-disabling story.

### Tests

`features/knowledge-type/image/file/add-rag/steps/segment/segment.test.tsx` (new or extend if exists):

| Scenario | Expected |
|---|---|
| schemas loaded, fileType=jpg | DOM has no mode-label / Select; DynamicParsingPanel renders against `image_document` |
| schemas loaded, fileType=png | same |
| schemas loading | loading text only, no Select |
| schemas failed | error + retry button only, no Select |
| schemas catalog missing `image_document` | renders nothing (empty candidate set) — user sees an empty config area rather than scanned-as-fallback; aligns with "no surprises" |

The last row is intentionally strict: if rag ever drops the `image_document` schema, we'd rather show emptiness than auto-fall-back to scanned (the user would not expect that and the bug would be hidden). Tradeoff: lose graceful degradation on a hypothetical rag breakage. Acceptable because (a) rag dropping `image_document` is a breaking change rag-side that already needs coordinated handling, (b) the empty state is visible and the catalog-fetch error path already exists.

## Risks & open questions

- **A future divergence between image and scanned schemas (e.g. rag adds an image-only `caption` param) could re-introduce real choice.** Mitigation: when that happens, revisit this spec. The single-line filter is easy to unwind. The relevant `caption` work is tracked in memory [coze-photo-extract-caption-not-migrated](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-photo-extract-caption-not-migrated.md); when that lands, the divergence check is part of its plan.
- **The same-pattern text KB segment step is intentionally untouched.** A reader scanning both files will see different filter behavior; the `image_document`-only filter literal pins the difference visibly.

## Predecessor / related

- 2026-05-20-image-upload-force-ocr-on-design.md — established the `FORCED_PARAMS_BY_SCHEMA` map this spec depends on for the equivalence claim.
- 2026-05-21-rag-params-i18n-zh-design.md — sibling spec; the i18n keys it touches are unaffected by this filter (scanned-schema entries can stay in the bundle in case it's used elsewhere or revived).
- [coze-rag-image-chunk-hide-force](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-rag-image-chunk-hide-force.md) — the force-map memory; this spec is a downstream consequence.
