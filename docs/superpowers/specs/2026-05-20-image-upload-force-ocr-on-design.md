# Image Upload: Force OCR On

**Date:** 2026-05-20
**Author:** xinyu.liu@vorteklab.com
**Status:** Design — pending user review

## Problem

End-to-end smoke (2026-05-20) of "create image KB → upload image → workflow knowledge-retrieve node" against the v2 stack returned `items: []`:

- Workflow's knowledge-retrieve node is constrained to **text-in / text-out** by design.
- rag's `image_document` schema declares `enable_ocr` default `false`. Frontend faithfully forwards that default to the wire.
- An image uploaded with OCR off produces only `image_chunk`. The text-input retrieve plan defaults `target_chunk_types=["text_chunk"]`, so it never hits the document.
- Forcing `target_chunk_types=["image_chunk"]` from the node would require `query_image`, which the node has no UI path to provide (and is a separate design problem).

Net effect: the default UX (upload image, click through, retrieve) is an empty set. The `enable_ocr` toggle in the upload form is visible (`validate.ts:67`) but its load-bearing implication ("you have to turn this on or text retrieval cannot find this document") is invisible.

Positive control confirms the rest of the pipeline works: a text-input retrieve against an OCR-on doc (`Xinyu Liu.pdf`, 144 text chunks) returned 1 hit in the same smoke.

## Goal

Force `enable_ocr=true` for both image-bearing upload schemas (`image_document` and `scanned_document`) so that the natural image-upload UX produces documents the workflow knowledge-retrieve node can find. Power-user controls (`ocr_model_id`, `ocr_languages`) remain configurable.

## Non-Goals

- **Existing OCR-off documents are not touched.** No migration script, no reprocess button in this design. Users either re-upload or accept that pre-existing documents remain unretrievable from the workflow node.
- **No backend changes.** `document.go` / `client.go` / ragimpl require nothing. `{enable_ocr:true, ocr_model_id}` is a legal wire shape; rag's validator already accepts it.
- **No rag-server schema changes.** rag's `image_document` keeps its `default=false` server-side. Coze's frontend overrides it for coze users.
- **The other half of the contract gap (workflow node image-query input) is out of scope.** That is a separate, larger design (where would the image come from in a workflow context?).
- **No KB-type-aware routing of the upload UX.** Image upload entry is already gated by KB format type elsewhere.

## Design

### Data structure

Add a declarative override map in `dynamic-parsing-panel/use-schemas.ts`, sibling to the existing `FRONTEND_PARAM_DEFAULTS`:

```ts
// Params whose value is locked, regardless of user input or schema default.
// Keyed by rag schema_id, then by param.name.
//
// Why: rag's image_document schema declares enable_ocr default=false, but
// coze's workflow knowledge-retrieve node only does text-in/text-out, so an
// OCR-off image upload silently produces a KB the node cannot retrieve from.
// Force OCR on at the frontend so the natural upload UX produces text_chunks.
//
// `reason` is an i18n key used for the disabled-control tooltip.
export const FORCED_PARAMS_BY_SCHEMA: Record<
  string,
  Record<string, { value: unknown; reason: string }>
> = {
  image_document: {
    enable_ocr: { value: true, reason: 'forced_ocr_for_image_kb' },
  },
  scanned_document: {
    enable_ocr: { value: true, reason: 'forced_ocr_for_image_kb' },
  },
};
```

Priority order when computing a param's effective value:
1. `FORCED_PARAMS_BY_SCHEMA` (highest — overrides everything)
2. User input from the form
3. `FRONTEND_PARAM_DEFAULTS`
4. rag schema's declared `default`

### Wire enforcement: `applyForcedParams`

In `dynamic-parsing-panel/validate.ts`, add:

```ts
export function applyForcedParams<T extends Record<string, unknown>>(
  schemaId: string | undefined,
  value: T,
): T {
  const forced = FORCED_PARAMS_BY_SCHEMA[schemaId ?? ''];
  if (!forced) return value;
  const next = { ...value };
  for (const [k, { value: v }] of Object.entries(forced)) {
    next[k] = v;
  }
  return next as T;
}
```

`mergeSchemaDefaults` calls `applyForcedParams` **before** the existing inverse-OCR mutex (which deletes `ocr_model_id` when `enable_ocr !== true`). Ordering matters:

1. Seed `FRONTEND_PARAM_DEFAULTS` (incl. `ocr_model_id` default) and schema defaults.
2. **`applyForcedParams`**: if schema is in the map, overwrite `enable_ocr=true`. This must come **before** step 3 — otherwise a stale `enable_ocr=false` in user input would trigger the mutex to strip `ocr_model_id`, and step 2 would then re-enable OCR with no model → rag 40001.
3. Run inverse OCR mutex: if `enable_ocr !== true`, drop `ocr_model_id` (existing behavior, unchanged). For forced schemas this is a no-op since step 2 just pinned `enable_ocr=true`. For non-forced schemas the original strip still fires correctly.

Net result for `image_document` / `scanned_document`: `{enable_ocr:true, ocr_model_id:<default-or-user-chosen>}` always reaches the wire. rag's ingestion validator accepts this shape per `coze-rag-ocr-validator-mutex`.

### UI rendering: locked control + tooltip

In `dynamic-parsing-panel.tsx`, the param render pass checks the forced map:

```ts
const forcedMap = FORCED_PARAMS_BY_SCHEMA[schema.schema_id] ?? {};

// For each param:
const forced = forcedMap[param.name];
<ParamControl
  ...
  value={forced ? forced.value : value[param.name]}
  disabled={Boolean(forced) || existingDisabled}
  hint={forced ? I18n.t(forced.reason) : undefined}
/>
```

- `enable_ocr` toggle: shows **checked, greyed-out, with a question-mark tooltip** under `image_document` and `scanned_document`.
- `ocr_model_id` / `ocr_languages`: unchanged — render normally in the advanced area, user-editable, defaulted via `FRONTEND_PARAM_DEFAULTS`.
- Other schemas (`text_document`, `markdown_document`, `docx_document`, etc.): zero impact — they don't appear in `FORCED_PARAMS_BY_SCHEMA`, no `enable_ocr` param to forced-render.

### Tooltip copy

i18n key: `forced_ocr_for_image_kb`

zh-CN: 图片知识库需要开启 OCR 才能被工作流的知识库检索节点命中（节点目前只支持文本输入/输出）

en: OCR is required for image knowledge bases — the workflow knowledge-retrieve node currently supports text input/output only, and only OCR-extracted text chunks can be matched.

### Edge cases

- **Schema switch (image_document ↔ scanned_document)**: `applyForcedParams` keyed by current `schema_id`, both have `enable_ocr=true`, switching doesn't change behavior.
- **Stale cached form state with `enable_ocr=false`**: UI control overrides to true on render; `applyForcedParams` overrides on submit. Defense in depth.
- **DevTools-poked form state**: same — submit-time override catches it.
- **Schemas with no `enable_ocr` param** (text/markdown/etc.): `FORCED_PARAMS_BY_SCHEMA` doesn't list them, no override applied, no render-side disabled control.
- **Future schema additions**: simple — add the schema_id and the locked params to the map.

## Tests

In `validate.test.ts` (sibling to existing `mergeSchemaDefaults` tests):

1. `applyForcedParams returns input unchanged when schemaId not in map` — text/markdown/docx/unknown id, value passes through verbatim
2. `applyForcedParams overrides value.enable_ocr=false to true for image_document`
3. `applyForcedParams overrides value.enable_ocr=false to true for scanned_document`
4. `mergeSchemaDefaults sends {enable_ocr:true, ocr_model_id:<default>} for image_document with empty user input` — end-to-end first submit
5. `mergeSchemaDefaults sends enable_ocr=true even when stale form value carries false` — defensive
6. `mergeSchemaDefaults keeps ocr_model_id present when forced=true (inverse-mutex strip does not fire)`

Panel rendering tests (if the existing component test setup covers this):

7. Rendering `image_document` schema → `enable_ocr` control has `disabled=true` and a non-empty `hint`
8. Rendering `scanned_document` schema → same
9. Rendering `text_document` schema (or any schema without `enable_ocr` listed) → unaffected

## Out of Scope (explicit)

- Existing OCR-off documents in the live stack (e.g., `img3` / `fe1c0ba2`). They remain unretrievable from the workflow node until manually re-uploaded.
- Workflow node image input (`query_image`). Separate design.
- Backend (`document.go`, `client.go`, `ragimpl/*`). Unchanged — frontend is the policy layer.
- rag-server `image_document` schema default. Unchanged — coze's frontend overrides at coze's contract surface.

## Files Touched

- `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-schemas.ts` — add `FORCED_PARAMS_BY_SCHEMA` map
- `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.ts` — add `applyForcedParams`, call it at the end of `mergeSchemaDefaults`
- `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/validate.test.ts` — new tests (6 wire-level + maybe 3 panel-level)
- `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx` — read forced map, apply `disabled` + `hint` to matching param controls
- i18n locale file(s) for the `forced_ocr_for_image_kb` key (zh-CN, en)

## Related

- `coze-rag-ocr-validator-mutex` (memory): the original 40001 fix that established the inverse-OCR mutex this design extends
- `coze-rag-kb-type-persisted-locally` (memory): why `format_type=2` lives in `rag_kb_mapping` (so coze knows which KBs are "image KBs" — relevant if a future iteration wants to gate this UX by KB type)
- `coze-rag-retrieval-param-mismatch` (memory): same contract-drift family — frontend takes responsibility for a policy that rag's per-schema default doesn't express

---

# Follow-up: Hide image_chunk config (2026-05-20)

## Problem

After locking `enable_ocr=true`, the upload form still exposes two image-chunk-related toggles for `image_document` and `scanned_document`:
- `enable_image_embedding` (group: `image_chunking`) — runs the image through a CLIP-style embedding model
- `produce_image_chunk` (group: `chunk_outputs`) — outputs the resulting vector as an `image_chunk`

The workflow knowledge-retrieve node is text-in/text-out — it cannot consume `image_chunk` (would need `query_image` + `target_chunk_types=["image_chunk"]` override, neither exposed by the node UI). Currently `image_document` defaults `enable_image_embedding=true`, so every uploaded image gets a CLIP vector produced and stored in Milvus — pure waste in this UX.

## Goal

Hide both image-chunk toggles from the upload UI and force them off on the wire for both `image_document` and `scanned_document`. No `image_chunk` produced; CLIP inference and vector storage saved.

## Non-Goals

- Existing image_chunks in the live stack — not touched.
- Multi-modal retrieval future work — when/if that lands, the hide can be reverted; the architecture supports it.
- Other groups (`ocr_layout.include_image_ref`) — keeps OCR text-chunk image references, unrelated to image_chunks.

## Design

### Extend `FORCED_PARAMS_BY_SCHEMA` entry shape

Change the entry type from single-form to a discriminated union:

```ts
type ForcedParamEntry =
  | { value: unknown; reason: string; hidden?: false }  // visible: disabled+Tooltip+warning
  | { value: unknown; hidden: true };                    // hidden: skip rendering, still force wire value
```

Semantics:
- **Visible (`hidden: false` or omitted)**: control renders disabled, with Tooltip and inline warning Typography — current behavior for `enable_ocr`.
- **Hidden (`hidden: true`)**: param skipped entirely in `GroupedFields`; `applyForcedParams` still pins the wire value.

### New forced entries

`image_document` and `scanned_document` each gain two hidden forced entries:

```ts
enable_image_embedding: { value: false, hidden: true },
produce_image_chunk:    { value: false, hidden: true },
```

For `scanned_document` these align with rag's schema defaults (false), but the lock is kept symmetric + defensive (stale state, future drift).

### Panel rendering

`GroupedFields` filters out hidden entries before mapping params to controls. Hidden entries don't render: no control, no description, no warning Typography line.

`applyForcedParams` doesn't change — it processes all entries identically. The `hidden` flag is a render-time concern only.

### Why no i18n key for hidden entries

Hidden entries don't surface text to the user, so no `reason` field is needed. The discriminated union enforces this at the type level.

## Tests

In `validate.test.ts`:
- `applyForcedParams forces enable_image_embedding=false on image_document`
- `applyForcedParams forces produce_image_chunk=false on image_document`
- Same two for `scanned_document`
- `mergeSchemaDefaults for image_document outputs neither true image-chunk flag` (integration)

In `dynamic-parsing-panel.test.tsx`:
- `does not render enable_image_embedding control on image_document`
- `does not render produce_image_chunk control on image_document`
- Same two for `scanned_document`
- `still renders these params on schemas not in the forced map` (negative — these schemas don't actually have these params, so trivially satisfied; skip if redundant)

## Out of Scope

- The `include_image_ref` param (ocr_layout group) — unrelated to image_chunks.
- Existing `image_chunk`s in rag-side Milvus — they remain but become orphan data with no consumer in the workflow path.

---

# Follow-up 2: Lock produce_text_chunk symmetrically (2026-05-20)

## Problem

After the image_chunk hide+force, the wire payload for image_document looks asymmetric:

```
enable_ocr: true              ← forced visible
enable_image_embedding: false ← forced hidden
produce_image_chunk: false    ← forced hidden
produce_text_chunk: false     ← schema default, NOT forced
```

`produce_text_chunk` defaults to false on image_document. rag's ingestion policy resolver (`/app/app/policy/ingestion_policy_resolver.py:255-256`) saves us via an OR fallback:
```python
wants_text = bool(produce_text_chunk) or enable_ocr
wants_image = bool(produce_image_chunk) or enable_image_embedding
```
So `enable_ocr=true` overrides `produce_text_chunk=false` → text_chunk is still produced. The current behavior is correct **but only because of rag's tolerance**.

Two reasons to fix:
1. **Symmetry**: image side is double-locked (embedding off + produce_image_chunk off). Text side should mirror (enable_ocr on + produce_text_chunk on).
2. **Defense in depth**: if rag ever removes the OR fallback (e.g., refactors to AND semantics — "user must explicitly opt in to each output type"), our wire becomes wrong and OCR work goes nowhere.

Also a minor UX win: `produce_text_chunk` shows up in the form as an editable toggle today, but it's effectively a no-op because of the OR. Hiding it removes the confusion.

## Goal

Add `produce_text_chunk: { value: true, hidden: true }` to both `image_document` and `scanned_document` in `FORCED_PARAMS_BY_SCHEMA`. Toggle disappears from the UI; wire payload explicitly carries `produce_text_chunk=true`.

## Scope confirmed via rag schema dump

`produce_text_chunk` only exists on `image_document` and `scanned_document` — the other schemas (`text_document`, `markdown_document`, `pdf_text_document`, `docx_document`) are pure text sources where text_chunk production is implicit. No extra schema needs updating.

## Non-Goals

- No new architecture changes — Task 6's hidden variant of `ForcedParamEntry` already covers it.
- No new i18n keys — hidden entries don't render text.
- No backend, no rag-server changes.

## Design

Two new entries in `FORCED_PARAMS_BY_SCHEMA`:

```ts
image_document: {
  // ...existing enable_ocr, enable_image_embedding, produce_image_chunk
  produce_text_chunk: { value: true, hidden: true },
},
scanned_document: {
  // ...existing enable_ocr, enable_image_embedding, produce_image_chunk
  produce_text_chunk: { value: true, hidden: true },
},
```

For `scanned_document` this aligns with the rag-side default (true), but the explicit lock + hide is for symmetry and defense.

## Tests

Append:
- `applyForcedParams forces produce_text_chunk=true on image_document`
- `applyForcedParams forces produce_text_chunk=true on scanned_document`
- `mergeSchemaDefaults sends produce_text_chunk=true on image_document even when schema default is false`
- `hides produce_text_chunk control on image_document` (panel)
- `hides produce_text_chunk control on scanned_document` (panel)
