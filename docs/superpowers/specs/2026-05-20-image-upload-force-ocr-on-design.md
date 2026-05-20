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

---

# Follow-up 3: Fix image KB detail page for rag-backed KBs (2026-05-20)

## Problem

In the image-knowledge-workspace detail page (`frontend/.../knowledge-ide-base/.../image-knowledge-workspace/index.tsx`), three symptoms surface for rag-backed image KBs:

1. **Only 1 card shown** even though the KB has multiple uploaded images
2. **No thumbnail preview** — image cards render an empty image area
3. **Misleading "未标注数据将无法被检索召回" warning** even though text retrieval works (proven by Tasks 1-7 OCR pipeline)

Root cause: `listPhoto` (`backend/application/knowledge/knowledge.go:1612`) was built for the legacy non-rag model "1 image = 1 document = 1 slice, slice content = user's manual annotation caption". rag-backed reality is "1 image = 1 document = many text_chunks (OCR) + maybe 1 image_chunk (CLIP)". The mismatch produces three connected failures:

- `ListPhotoSlice` in ragimpl (`ragimpl/slice.go:540-643`) queries rag with hardcoded `ChunkType: "image_chunk"`. After Task 6's `enable_image_embedding=false` force, **new documents have zero image_chunks** → zero slices returned → zero document IDs in the listing → zero cards. Older docs (pre-Task-6) that still have 1 image_chunk surface as 1 card each, but pagination by chunk still produces wrong totals.
- `buildDocumentEntity` (`ragimpl/document.go:195-209`) never populates `entity.Document.URL`. rag's `contract.Document` has no URL field — the image bytes live on rag's internal storage and rag does not expose a presigned URL.
- The "未标注" warning fires when `PhotoInfo.Caption == ""`. `packPhotoInfo` reads `slice.GetSliceContent()` — for docs that surface (those with an image_chunk), rag's image_chunk has `caption: ""`; for docs that don't surface, no caption entry is created at all. Both paths produce empty captions.

Task 6 (forcing `enable_image_embedding=false`) made symptom 1 strictly worse for newly-uploaded image KBs — they become completely invisible to this page. The fix below corrects all three symptoms.

## Goal

Image KB detail page renders correctly for rag-backed KBs: all uploaded images appear as cards, each card shows a thumbnail, and the "未标注" warning no longer fires falsely.

## Non-Goals

- **Existing image documents are not migrated.** Pre-fix uploads have `rag_doc_mapping.image_url=NULL`; their cards will show no thumbnail (filename-only) until re-uploaded. Listing and filename-as-caption work for them.
- **No reintroduction of image_chunk production.** Task 6's force remains in effect; we don't undo it to get URLs from chunk metadata.
- **No new UI annotation/caption editor.** Caption is filename-only for rag-backed KBs.
- **No HasCaption filter rework.** That filter (legacy "annotated vs unannotated") loses semantic meaning under rag — the ragimpl `ListPhotoSlice` will ignore it.

## Design

### Data layer: add `image_url` column to `rag_doc_mapping`

Declarative atlas schema change in `docker/atlas/opencoze_latest_schema.hcl` (rag_doc_mapping table at line 1884):

```hcl
column "image_url" {
  null    = true
  type    = varchar(512)
  comment = "Coze-side MinIO URL for image-source documents (for detail-page thumbnails). NULL for non-image docs and for pre-2026-05-20 image uploads."
}
```

Also add an informational migration file `docker/atlas/migrations/20260520140000_add_image_url_to_rag_doc_mapping.sql` mirroring the change. Per `[[coze-stack-atlas-declarative-deploy]]`, the HCL is the authoritative source; the migration file is informational.

`MappingRepo.InsertDoc` signature gains an `imageURL string` parameter. `KBByCozeID`/`KBsByCozeIDs`/`DocByCozeID` / hydrators read it back.

### Upload side: persist image bytes to coze MinIO during ingestion (Task 8)

In `ragimpl/document.go CreateDocument` (line 242):
- After computing `fileBytes`, check if the schema is image-bearing (`image_document` or `scanned_document` — detected via `req.DocumentEntities[i].ParsingStrategy` or document_options `_source_modality` or by file_type/MIME). If so, call `i.storage.PutObject(ctx, objectKey, fileBytes)` with a deterministic key (e.g. `knowledge/image/{kb_id}/{rag_doc_id}/{filename}`) and then `i.storage.GetObjectUrl(ctx, objectKey)` to obtain the URL.
- Pass the URL through to `i.mapping.InsertDoc(...)`.
- For non-image docs, the URL is empty/null — pass through as such.

If `i.storage.PutObject` or `GetObjectUrl` fails: don't abort the upload — log the failure and proceed with empty URL. Thumbnails are a UX nicety; ingestion correctness is the primary concern.

### Read side: rewrite `ListPhotoSlice` and populate `Document.URL` (Task 9)

**`ragimpl/slice.go ListPhotoSlice`** (currently lines 540-643): replace chunk-based pagination with document-based pagination.

- Drop the `ChunkType: "image_chunk"` query.
- Call rag's `GET /api/v1/knowledgebases/{kb_id}/documents` (already exposed via the rag client) with `limit/offset` honoring the input request.
- For each rag document returned, look up the matching `rag_doc_mapping` row, get `coze_doc_id`.
- Construct a synthetic `entity.Slice` per document:
  - `DocumentID = coze_doc_id`
  - `RawContent = [{Type: SliceContentTypeText, Text: &doc.Filename}]` (so `GetSliceContent()` returns the filename)
- Ignore `HasCaption` filter (no semantic equivalent in rag-backed mode).
- Return `entity.SliceResult{Slices, Total}` where Total = rag's documents `total` field.

**`ragimpl/document.go buildDocumentEntity`** (line 195-209): populate `entity.Document.URL` from `DocMapping.ImageURL`. When NULL, leave `URL` as zero-value `""` (existing behavior unchanged for non-image docs and pre-fix image docs).

### Backward compatibility

- Pre-fix image documents: `image_url=NULL` in mapping → URL=`""` → frontend renders empty image area but shows filename as caption (no false warning). Acceptable degradation.
- Post-fix image documents: URL populated → thumbnail renders.
- Listing all 3 symptoms resolved for both old and new docs (modulo missing thumbnails on old docs).

## Tests

### Task 8 (upload side)

- `CreateDocument stores image_url in mapping for image_document uploads` (ragimpl integration test)
- `CreateDocument stores image_url for scanned_document uploads`
- `CreateDocument leaves image_url empty for text/markdown/docx uploads`
- `CreateDocument tolerates storage failure: image_url empty, doc still created` (defensive)
- `MappingRepo.InsertDoc round-trips image_url`

### Task 9 (read side)

- `ListPhotoSlice returns one synthetic slice per rag document (not per chunk)`
- `ListPhotoSlice honors limit/offset for document-level pagination`
- `ListPhotoSlice's synthetic slice content equals document filename`
- `buildDocumentEntity populates URL from mapping.image_url when present`
- `buildDocumentEntity leaves URL empty when mapping.image_url is NULL`

## Out of Scope

- Migrating pre-fix image documents to populate image_url retroactively. User confirmed: only new uploads.
- Frontend changes. The legacy "annotation editor" modal (`usePhotoDetailModal`) may still surface but will save no-ops on rag-backed docs; leaving it as-is for this fix.
- HasCaption filter; legacy semantic doesn't map; ignored in rag.
- The image_chunk caption (rag-side) — we no longer produce image_chunks, so this is moot.

## Files Touched (across Tasks 8 + 9)

- `docker/atlas/opencoze_latest_schema.hcl` — add image_url column to rag_doc_mapping
- `docker/atlas/migrations/20260520140000_add_image_url_to_rag_doc_mapping.sql` — informational migration
- `backend/domain/knowledge/service/ragimpl/mapping.go` — InsertDoc signature, hydrators read image_url
- `backend/domain/knowledge/service/ragimpl/document.go` — CreateDocument forks fileBytes to storage; buildDocumentEntity populates URL
- `backend/domain/knowledge/service/ragimpl/slice.go` — ListPhotoSlice rewrite
- Test files: `mapping_test.go`, `document_test.go`, `slice_test.go`, possibly `integration_test.go`

No frontend changes. No locale changes.

## Related

- `[[coze-rag-kb-type-persisted-locally]]` — same architectural pattern (rag exposes a capability flag but coze needs identity → persist locally in the mapping table). image_url follows the same precedent.
- `[[coze-stack-atlas-declarative-deploy]]` — schema changes must go in the HCL, migrations dir is informational.
