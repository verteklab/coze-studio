# RAG dynamic-parsing-panel + image KB UI Chinese i18n

**Date:** 2026-05-21
**Author:** xinyu.liu@vorteklab.com
**Status:** Design — pending user review

## Problem

The post-upload "Parsing config" page (`dynamic-parsing-panel/dynamic-parsing-panel.tsx`) renders rag's `DocumentParameterSchema` directly:

- **Field label** = `parameter.name` (e.g. `chunk_size`, `enable_ocr`, `ocr_render_dpi`, `merge_blank_line_paragraphs`) — raw English snake_case.
- **Field description** = `parameter.description` (English sentence, e.g. `"Scanned documents require OCR to produce text chunks."`).
- **Group header** = `parameter.group` (e.g. `chunking`, `ocr`, `pdf_text`).
- **Enum option label** = the raw `allowed_values[i]` string (e.g. `markdown`, `latex`, `plain`).

Schema selector chrome was localized in 2026-05-15 (`datasets_createFileModel_rag_schema_*` keys 6014-6019), but the per-parameter content was not. Users on zh see English sentences mid-form.

Image KB UI (`features/knowledge-type/image/file/**`) is largely already routed through `I18n.t(...)`, with **one stray literal** at `add-rag/steps/progress/index.tsx:152` (`请刷新页面或联系管理员。`) that should move to the bundle for consistency, plus an audit pass to confirm no others crept in.

## Goal

Every user-facing string surfaced by the upload-config flow — schema-derived parameter labels, descriptions, group headers, enum option labels, and any incidental Chinese/English literals in image KB UI — flows through the existing `@coze-arch/i18n` `I18n.t(...)` channel. `zh-CN` shows Chinese, `en-US` keeps the existing English. Missing-key fall-back surfaces the raw rag string (existing English) so a future rag-schema addition doesn't produce blanks before translators catch up.

## Non-Goals

- **No new locale beyond zh / en.** Existing infrastructure ships only zh-CN.json + en-US.json (and one or two other locales already present); this spec adds keys to whatever locale files exist, no new files.
- **No i18n infra change.** `@coze-arch/i18n`, the bundle loader, the `I18n.t` signature all stay. We add keys, not machinery.
- **No backend change.** Rag continues to emit English strings; coze does not proxy/rewrite. The translation table lives entirely in the frontend bundle.
- **No restructure of rag's `document_parameter_catalog.py`.** Read-only on the rag side.
- **No translation of rag schema_id values** beyond what 2026-05-15 already shipped (`pdf_text_document` etc.); those keys exist and are reused.
- **No image-KB UX redesign.** Strictly translation; the layout, control set, step flow do not change.

## Design

### Source-of-truth inventory

The rag `document_parameter_catalog.py` exposes ~45 `ParameterSpec` entries across 6 schemas:

| Schema | Param count (incl. shared) |
|---|---|
| `text_document` | 5 |
| `markdown_document` | 8 |
| `pdf_text_document` | 9 |
| `docx_document` | 7 |
| `image_document` | 7 |
| `scanned_document` | 9 |

Shared params (`chunk_size`, `chunk_overlap`, `enable_ocr`, `enable_image_embedding`, `ocr_model_id`, `ocr_languages`, `ocr_render_dpi`, `produce_text_chunk`, `produce_image_chunk`) reuse a single translation key — translation lives at the **parameter name** level, not schema-scoped, to avoid duplication.

The inventory list (param-name → zh label / zh description / enum labels) lives inside this spec's accompanying plan document, not in code comments. The plan will produce one PR adding the bundle entries + the panel wiring; the inventory is the PR's review surface.

### Key namespace

All new keys live under prefix **`datasets_createFileModel_rag_param_`**, sibling to the existing `datasets_createFileModel_rag_schema_*` keys (zh-CN.json lines 6014-6019).

| Key form | Example | Purpose |
|---|---|---|
| `<prefix><param_name>_label` | `..._param_chunk_size_label` → "切片大小" | Field label (replaces raw `parameter.name`) |
| `<prefix><param_name>_desc` | `..._param_chunk_size_desc` → "用于切片的最大长度。" | Field description (replaces raw `parameter.description`) |
| `<prefix><param_name>_enum_<value>` | `..._param_table_format_enum_markdown` → "Markdown" | Enum option label (when `allowed_values` is present) |
| `<prefix>group_<group_name>` | `..._param_group_ocr` → "OCR" | Section header (replaces raw `parameter.group`) |

Rules:

- Key segments are snake_case identifiers stripped of any non-`[a-z0-9_]`. Spec includes a small allowlist of expected param names so a typo'd new rag param surfaces as "missing key" (logged via `I18n.t` fallback), not silently mis-mapped.
- Group-key namespace is flat (`group_*`) — groups are few and shared across schemas.
- The bundle entries land in both `zh-CN.json` and `en-US.json`. en-US values come directly from the rag-side English (description verbatim, label from a snake_case → Title Case rule), so the EN UX is identical pre- and post-change.

### Frontend hook

New file: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.ts`.

```ts
import { I18n } from '@coze-arch/i18n';
import { type DocumentParameter } from './types';

/**
 * Resolves localised label / description / enum option labels for a rag
 * DocumentParameter. Falls back to the raw schema value when a key is missing
 * so a future rag-side param doesn't render blank before translators catch up.
 *
 * Key namespace: `datasets_createFileModel_rag_param_<param_name>_{label,desc}`
 * and `datasets_createFileModel_rag_param_<param_name>_enum_<value>`.
 */
export function useRagParameterI18n(p: DocumentParameter): {
  label: string;
  description: string;
  options: Array<{ value: string; label: string }>;
} {
  const labelKey = `datasets_createFileModel_rag_param_${p.name}_label`;
  const descKey = `datasets_createFileModel_rag_param_${p.name}_desc`;
  return {
    label: i18nWithFallback(labelKey, p.name),
    description: i18nWithFallback(descKey, p.description ?? ''),
    options: (p.allowed_values ?? []).map(v => ({
      value: String(v),
      label: i18nWithFallback(
        `datasets_createFileModel_rag_param_${p.name}_enum_${v}`,
        String(v),
      ),
    })),
  };
}

export function useRagGroupI18n(groupName: string): string {
  return i18nWithFallback(
    `datasets_createFileModel_rag_param_group_${groupName}`,
    groupName,
  );
}

/**
 * I18n.t returns the key itself when missing (a known quirk of the loader).
 * Detect that and substitute the provided fallback so we never show raw
 * `datasets_createFileModel_...` strings to the user.
 */
function i18nWithFallback(key: string, fallback: string): string {
  const v = I18n.t(key);
  return v && v !== key ? v : fallback;
}
```

The `i18nWithFallback` helper sidesteps `@coze-arch/i18n`'s default of returning the key on miss — a behaviour confirmed in the existing codebase by the `datasets_createFileModel_rag_required_missing` key pattern (which would otherwise show its own key on miss).

### Wiring `dynamic-parsing-panel.tsx`

Three render sites change. All replace raw schema strings with hook results — no logic change.

Site 1 (group header, line ~169):

```tsx
<Typography.Title heading={6} style={...}>
  {useRagGroupI18n(p.group)}
</Typography.Title>
```

Site 2 (param description, line ~180):

```tsx
{description ? (
  <Typography.Text type="tertiary" size="small">{description}</Typography.Text>
) : null}
```

where `description` comes from `useRagParameterI18n(p)` higher in the function body.

Site 3 (field control's label + Select options) — `FieldControl` (further down `dynamic-parsing-panel.tsx`). The control passes `param.name` to the input element's label/placeholder and `allowed_values` to `<Select>`; both swap to `useRagParameterI18n(p).label` and `.options`.

Hook is called once per param at the top of the component that needs it (one of `GroupedFields` or `FieldControl`); React's rules-of-hooks are satisfied because hook calls happen unconditionally inside a render function — the underlying `I18n.t` is a pure read, not a state read.

### Image KB UI audit + cleanup

`features/knowledge-type/image/file/**`:

- `add-rag/steps/progress/index.tsx:152` — literal `请刷新页面或联系管理员。` becomes a new key:
  - `datasets_createFileModel_rag_progress_error_refresh_hint` → zh: "请刷新页面或联系管理员。" / en: "Please refresh the page or contact your administrator."
- Audit pass: `grep -rE '>[一-鿿]|>[A-Za-z][A-Za-z ]+<' features/knowledge-type/image/file/` — any other naked literals get the same treatment. Spec does not enumerate beyond the one found in initial recon; the plan's first task is the full audit.
- The existing image KB `caption` / `extract_caption` schema fields (paused per memory [coze-photo-extract-caption-not-migrated](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-photo-extract-caption-not-migrated.md)) — when they land, they ride the same `*_param_caption_label` / `*_param_extract_caption_label` keys this spec defines. Pre-add their entries so the future enablement is config-only.

### Tests

`frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.test.ts` (new):

- `useRagParameterI18n` with a known param (`chunk_size`) returns the zh label/description.
- With an unknown param (`__not_a_real_param__`) returns the raw `parameter.name` / `parameter.description` as fallback.
- Enum option fallback: a `Select` option whose `allowed_values` entry has no `_enum_<value>` key shows the raw value.

`dynamic-parsing-panel.test.tsx` (extend existing): assert that for a schema containing `chunk_size`, the rendered DOM has the zh label, not the raw `chunk_size` string.

Bundle integrity: an `npm run` script (or Vitest unit) reads both `zh-CN.json` and `en-US.json` and asserts the set of `datasets_createFileModel_rag_param_*` keys is identical between them. Prevents drift where one locale forgets an entry.

## Risks & open questions

- **`I18n.t` missing-key behavior depends on bundle loader implementation.** If a future loader version changes from "return-key-on-miss" to "return-empty-on-miss" (or vice versa), `i18nWithFallback`'s detection breaks. Mitigation: ship a tiny unit that asserts `I18n.t('__definitely_missing_key__')` !== ''; if that ever flips, this test alerts before users do.
- **Rag may add new params silently.** A rag upgrade that adds a new `ParameterSpec` will surface raw English in zh until translations land. Acceptable degradation; the audit task in the plan covers existing surface and the fallback covers future drift.
- **Translation churn vs rag-side wording changes.** If rag changes a `description=` value to clarify wording, our key stays put but content drifts. Long-term mitigation (out of scope here) would be a pin script that diffs rag's catalog against our key set on each rag bump.

## Predecessor / related memory

- 2026-05-15 batch (`datasets_createFileModel_rag_schema_*` keys 6014-6019) — same namespace, this spec extends it.
- 2026-05-20-image-upload-force-ocr-on-design.md — earlier image-KB-touching spec; introduced `datasets_createFileModel_rag_forced_ocr_hint` which already uses the namespace.
- [coze-photo-extract-caption-not-migrated](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-photo-extract-caption-not-migrated.md) — future caption/extract_caption params will need keys pre-allocated here.
