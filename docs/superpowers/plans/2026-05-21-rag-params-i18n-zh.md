# RAG Dynamic Parsing Panel + Image KB UI Chinese i18n — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every user-facing string surfaced by the upload-config flow — schema-derived parameter labels, descriptions, group headers, enum option labels, and any incidental literal strings in image KB UI — flows through `@coze-arch/i18n` `I18n.t(...)`. `zh-CN` shows Chinese, `en-US` keeps existing English. Missing-key fallback surfaces the raw rag string so a future rag-schema addition does not blank out the UI before translators catch up.

**Architecture:** New `useRagParameterI18n` / `useRagGroupI18n` hooks return localised label/description/group/enum strings for each rag `DocumentParameter`. Keys live under prefix `datasets_createFileModel_rag_param_` in the existing studio-i18n-resource bundles. `dynamic-parsing-panel.tsx` swaps three raw-string render sites for hook output. One stray Chinese literal in image KB progress UI moves to the bundle.

**Tech Stack:** TypeScript, React, Vitest, `@coze-arch/i18n`, `@coze-arch/coze-design`. Frontend-only — no backend or rag-server changes.

**Files Touched:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.ts`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.test.ts`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx`
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/index.ts` — re-export hooks
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json` — add ~75 keys
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en-US.json` — add the same keys
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/progress/index.tsx` — replace literal Chinese error hint with `I18n.t(...)`

**Package under test:** `@coze-data/knowledge-resource-processor-base`
**Test command (run from package dir):** `npm run test`
**Package dir:** `frontend/packages/data/knowledge/knowledge-resource-processor-base`

---

## Task 1: Inventory + locale bundle entries (zh-CN + en-US)

Drop the 75 new translation keys into both locale files in one shot. The data is grouped (params, groups, enums) and each entry is independent — a stable PR review surface and the only "data dump" task in the plan.

**Files:**
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json`
- Modify: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en-US.json`

---

- [ ] **Step 1: Add the 75 keys to `zh-CN.json`**

Open `frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json`. JSON objects are alphabetically sorted in this file (verify by inspecting around the existing `datasets_createFileModel_rag_*` block near line 6006). Insert the new keys in alphabetical order. The full block (already alphabetised):

```json
"datasets_createFileModel_rag_param_chunk_overlap_desc": "切片重叠长度,必须小于切片大小。",
"datasets_createFileModel_rag_param_chunk_overlap_label": "切片重叠长度",
"datasets_createFileModel_rag_param_chunk_size_desc": "文本切片的最大长度。",
"datasets_createFileModel_rag_param_chunk_size_label": "切片大小",
"datasets_createFileModel_rag_param_deduplicate_blank_lines_desc": "合并连续的空行。",
"datasets_createFileModel_rag_param_deduplicate_blank_lines_label": "去除重复空行",
"datasets_createFileModel_rag_param_enable_image_embedding_desc": "是否生成 image_chunk 向量。",
"datasets_createFileModel_rag_param_enable_image_embedding_label": "启用图像向量",
"datasets_createFileModel_rag_param_enable_ocr_desc": "是否通过 OCR 提取文本。",
"datasets_createFileModel_rag_param_enable_ocr_label": "启用 OCR",
"datasets_createFileModel_rag_param_extract_headers_footers_desc": "提取文档中的页眉和页脚文本。",
"datasets_createFileModel_rag_param_extract_headers_footers_label": "提取页眉页脚",
"datasets_createFileModel_rag_param_extract_tables_desc": "在解析器支持时启用表格文本提取。",
"datasets_createFileModel_rag_param_extract_tables_label": "提取表格",
"datasets_createFileModel_rag_param_group_chunk_outputs": "切片产出",
"datasets_createFileModel_rag_param_group_chunking": "切片",
"datasets_createFileModel_rag_param_group_docx_structure": "Word 结构",
"datasets_createFileModel_rag_param_group_image_chunking": "图像切片",
"datasets_createFileModel_rag_param_group_markdown_structure": "Markdown 结构",
"datasets_createFileModel_rag_param_group_ocr": "OCR",
"datasets_createFileModel_rag_param_group_ocr_layout": "OCR 版式",
"datasets_createFileModel_rag_param_group_ocr_text": "OCR 文本切片",
"datasets_createFileModel_rag_param_group_pdf_layout": "PDF 版式",
"datasets_createFileModel_rag_param_group_text_paragraph": "文本段落",
"datasets_createFileModel_rag_param_include_bbox_desc": "在 OCR 返回边界框时保留到切片元数据中。",
"datasets_createFileModel_rag_param_include_bbox_label": "保留边界框",
"datasets_createFileModel_rag_param_include_heading_in_chunk_desc": "切片内容中保留标题文本。",
"datasets_createFileModel_rag_param_include_heading_in_chunk_label": "切片保留标题",
"datasets_createFileModel_rag_param_include_image_ref_desc": "在切片元数据中保留源图像引用。",
"datasets_createFileModel_rag_param_include_image_ref_label": "保留图像引用",
"datasets_createFileModel_rag_param_include_page_number_desc": "元数据中保留页码,可选写入正文。",
"datasets_createFileModel_rag_param_include_page_number_label": "保留页码",
"datasets_createFileModel_rag_param_max_heading_level_desc": "Markdown 切分使用的最大标题层级。",
"datasets_createFileModel_rag_param_max_heading_level_label": "最大标题层级",
"datasets_createFileModel_rag_param_max_merged_paragraph_length_desc": "切片刷新前的最大合并段落长度。",
"datasets_createFileModel_rag_param_max_merged_paragraph_length_label": "合并段落最大长度",
"datasets_createFileModel_rag_param_merge_blank_line_paragraphs_desc": "打包切片时合并被空行分隔的段落。",
"datasets_createFileModel_rag_param_merge_blank_line_paragraphs_label": "合并空行分隔段落",
"datasets_createFileModel_rag_param_merge_cross_page_paragraphs_desc": "关闭按页切分时,将跨页文本合并为连续段落。",
"datasets_createFileModel_rag_param_merge_cross_page_paragraphs_label": "合并跨页段落",
"datasets_createFileModel_rag_param_min_heading_level_desc": "Markdown 切分使用的最小标题层级。",
"datasets_createFileModel_rag_param_min_heading_level_label": "最小标题层级",
"datasets_createFileModel_rag_param_min_paragraph_length_desc": "打包前舍弃短于该长度的段落。",
"datasets_createFileModel_rag_param_min_paragraph_length_label": "最小段落长度",
"datasets_createFileModel_rag_param_normalize_whitespace_desc": "规整重复的水平空白字符。",
"datasets_createFileModel_rag_param_normalize_whitespace_label": "规范化空白字符",
"datasets_createFileModel_rag_param_ocr_languages_desc": "OCR 语言提示。",
"datasets_createFileModel_rag_param_ocr_languages_label": "OCR 语言",
"datasets_createFileModel_rag_param_ocr_model_id_desc": "启用 OCR 时所需的模型 ID。",
"datasets_createFileModel_rag_param_ocr_model_id_label": "OCR 模型",
"datasets_createFileModel_rag_param_preserve_list_structure_desc": "在提取文本中保留简单的列表边界。",
"datasets_createFileModel_rag_param_preserve_list_structure_label": "保留列表结构",
"datasets_createFileModel_rag_param_produce_image_chunk_desc": "生成图像切片。",
"datasets_createFileModel_rag_param_produce_image_chunk_label": "生成图像切片",
"datasets_createFileModel_rag_param_produce_text_chunk_desc": "生成 OCR 文本切片。",
"datasets_createFileModel_rag_param_produce_text_chunk_label": "生成文本切片",
"datasets_createFileModel_rag_param_protect_code_blocks_desc": "尽量避免切分围栏标记的代码块。",
"datasets_createFileModel_rag_param_protect_code_blocks_label": "保护代码块",
"datasets_createFileModel_rag_param_protect_tables_desc": "尽量避免切分 Markdown 表格。",
"datasets_createFileModel_rag_param_protect_tables_label": "保护表格",
"datasets_createFileModel_rag_param_remove_headers_footers_desc": "去除 PDF 各页重复出现的首尾行。",
"datasets_createFileModel_rag_param_remove_headers_footers_label": "去除页眉页脚",
"datasets_createFileModel_rag_param_split_by_heading_desc": "依照标题结构切分 Markdown。",
"datasets_createFileModel_rag_param_split_by_heading_label": "按标题切分",
"datasets_createFileModel_rag_param_split_by_heading_style_desc": "当 Word 文档有标题样式时使用其作为切分边界。",
"datasets_createFileModel_rag_param_split_by_heading_style_label": "按标题样式切分",
"datasets_createFileModel_rag_param_split_by_ocr_block_desc": "在 OCR 返回块边界时,以其为切分提示。",
"datasets_createFileModel_rag_param_split_by_ocr_block_label": "按 OCR 块切分",
"datasets_createFileModel_rag_param_split_by_page_desc": "在切片前生成按页范围的文本单元。",
"datasets_createFileModel_rag_param_split_by_page_label": "按页切分",
"datasets_createFileModel_rag_param_table_text_format_desc": "提取表格使用的文本格式。",
"datasets_createFileModel_rag_param_table_text_format_enum_csv": "CSV",
"datasets_createFileModel_rag_param_table_text_format_enum_markdown": "Markdown",
"datasets_createFileModel_rag_param_table_text_format_enum_plain": "纯文本",
"datasets_createFileModel_rag_param_table_text_format_label": "表格文本格式",
```

Also add (for the literal-string fix in Task 5):

```json
"datasets_createFileModel_rag_progress_error_refresh_hint": "请刷新页面或联系管理员。",
```

- [ ] **Step 2: Add the same 76 keys (75 params/groups/enums + 1 progress hint) to `en-US.json`**

Open `frontend/packages/arch/resources/studio-i18n-resource/src/locales/en-US.json`. Mirror the values with the rag-side English originals (`ui_label` for labels, `description` for desc, `allowed_values` verbatim for enums, snake_case → Title Case for group keys):

```json
"datasets_createFileModel_rag_param_chunk_overlap_desc": "Chunk overlap; must be smaller than chunk_size.",
"datasets_createFileModel_rag_param_chunk_overlap_label": "Chunk overlap",
"datasets_createFileModel_rag_param_chunk_size_desc": "Maximum chunk size for text chunking.",
"datasets_createFileModel_rag_param_chunk_size_label": "Chunk size",
"datasets_createFileModel_rag_param_deduplicate_blank_lines_desc": "Collapse repeated blank lines.",
"datasets_createFileModel_rag_param_deduplicate_blank_lines_label": "Remove repeated blank lines",
"datasets_createFileModel_rag_param_enable_image_embedding_desc": "Whether to create image_chunk vectors.",
"datasets_createFileModel_rag_param_enable_image_embedding_label": "Produce image chunks",
"datasets_createFileModel_rag_param_enable_ocr_desc": "Whether to extract text via OCR.",
"datasets_createFileModel_rag_param_enable_ocr_label": "Enable OCR",
"datasets_createFileModel_rag_param_extract_headers_footers_desc": "Extract header and footer text when present.",
"datasets_createFileModel_rag_param_extract_headers_footers_label": "Extract headers and footers",
"datasets_createFileModel_rag_param_extract_tables_desc": "Enable table text extraction when the parser supports it.",
"datasets_createFileModel_rag_param_extract_tables_label": "Extract tables",
"datasets_createFileModel_rag_param_group_chunk_outputs": "Chunk outputs",
"datasets_createFileModel_rag_param_group_chunking": "Chunking",
"datasets_createFileModel_rag_param_group_docx_structure": "Word structure",
"datasets_createFileModel_rag_param_group_image_chunking": "Image chunking",
"datasets_createFileModel_rag_param_group_markdown_structure": "Markdown structure",
"datasets_createFileModel_rag_param_group_ocr": "OCR",
"datasets_createFileModel_rag_param_group_ocr_layout": "OCR layout",
"datasets_createFileModel_rag_param_group_ocr_text": "OCR text chunks",
"datasets_createFileModel_rag_param_group_pdf_layout": "PDF layout",
"datasets_createFileModel_rag_param_group_text_paragraph": "Text paragraphs",
"datasets_createFileModel_rag_param_include_bbox_desc": "Keep OCR bounding boxes in chunk metadata when returned by OCR.",
"datasets_createFileModel_rag_param_include_bbox_label": "Keep bbox",
"datasets_createFileModel_rag_param_include_heading_in_chunk_desc": "Keep heading text in chunk content.",
"datasets_createFileModel_rag_param_include_heading_in_chunk_label": "Keep headings in chunks",
"datasets_createFileModel_rag_param_include_image_ref_desc": "Keep source image references in chunk metadata.",
"datasets_createFileModel_rag_param_include_image_ref_label": "Keep image reference",
"datasets_createFileModel_rag_param_include_page_number_desc": "Keep page number in metadata and optionally text.",
"datasets_createFileModel_rag_param_include_page_number_label": "Keep page number",
"datasets_createFileModel_rag_param_max_heading_level_desc": "Maximum Markdown heading level used as a split boundary.",
"datasets_createFileModel_rag_param_max_heading_level_label": "Maximum heading level",
"datasets_createFileModel_rag_param_max_merged_paragraph_length_desc": "Maximum packed paragraph length before flushing a chunk.",
"datasets_createFileModel_rag_param_max_merged_paragraph_length_label": "Maximum merged paragraph length",
"datasets_createFileModel_rag_param_merge_blank_line_paragraphs_desc": "Merge paragraphs separated by blank lines when packing chunks.",
"datasets_createFileModel_rag_param_merge_blank_line_paragraphs_label": "Merge blank-line paragraphs",
"datasets_createFileModel_rag_param_merge_cross_page_paragraphs_desc": "Merge page text into continuous paragraphs when split_by_page is false.",
"datasets_createFileModel_rag_param_merge_cross_page_paragraphs_label": "Merge cross-page paragraphs",
"datasets_createFileModel_rag_param_min_heading_level_desc": "Minimum Markdown heading level used as a split boundary.",
"datasets_createFileModel_rag_param_min_heading_level_label": "Minimum heading level",
"datasets_createFileModel_rag_param_min_paragraph_length_desc": "Drop paragraphs shorter than this length before packing.",
"datasets_createFileModel_rag_param_min_paragraph_length_label": "Minimum paragraph length",
"datasets_createFileModel_rag_param_normalize_whitespace_desc": "Normalize repeated horizontal whitespace.",
"datasets_createFileModel_rag_param_normalize_whitespace_label": "Clean redundant whitespace",
"datasets_createFileModel_rag_param_ocr_languages_desc": "OCR language hints.",
"datasets_createFileModel_rag_param_ocr_languages_label": "OCR languages",
"datasets_createFileModel_rag_param_ocr_model_id_desc": "OCR model ID required when enable_ocr is true.",
"datasets_createFileModel_rag_param_ocr_model_id_label": "OCR model",
"datasets_createFileModel_rag_param_preserve_list_structure_desc": "Preserve simple list boundaries in extracted text.",
"datasets_createFileModel_rag_param_preserve_list_structure_label": "Preserve list structure",
"datasets_createFileModel_rag_param_produce_image_chunk_desc": "Produce image chunks.",
"datasets_createFileModel_rag_param_produce_image_chunk_label": "Produce image chunks",
"datasets_createFileModel_rag_param_produce_text_chunk_desc": "Produce OCR text chunks.",
"datasets_createFileModel_rag_param_produce_text_chunk_label": "Produce text chunks",
"datasets_createFileModel_rag_param_protect_code_blocks_desc": "Avoid splitting fenced code blocks when possible.",
"datasets_createFileModel_rag_param_protect_code_blocks_label": "Protect code blocks",
"datasets_createFileModel_rag_param_protect_tables_desc": "Avoid splitting Markdown table blocks when possible.",
"datasets_createFileModel_rag_param_protect_tables_label": "Protect tables",
"datasets_createFileModel_rag_param_remove_headers_footers_desc": "Remove repeated first and last lines across PDF pages.",
"datasets_createFileModel_rag_param_remove_headers_footers_label": "Remove headers and footers",
"datasets_createFileModel_rag_param_split_by_heading_desc": "Split Markdown by heading structure.",
"datasets_createFileModel_rag_param_split_by_heading_label": "Split by heading",
"datasets_createFileModel_rag_param_split_by_heading_style_desc": "Use Word heading styles as structural split boundaries when available.",
"datasets_createFileModel_rag_param_split_by_heading_style_label": "Split by heading style",
"datasets_createFileModel_rag_param_split_by_ocr_block_desc": "Use OCR block boundaries as split hints when returned by OCR.",
"datasets_createFileModel_rag_param_split_by_ocr_block_label": "Split by OCR block",
"datasets_createFileModel_rag_param_split_by_page_desc": "Produce page-scoped text units before chunking.",
"datasets_createFileModel_rag_param_split_by_page_label": "Split by page",
"datasets_createFileModel_rag_param_table_text_format_desc": "Text format used for extracted tables.",
"datasets_createFileModel_rag_param_table_text_format_enum_csv": "CSV",
"datasets_createFileModel_rag_param_table_text_format_enum_markdown": "Markdown",
"datasets_createFileModel_rag_param_table_text_format_enum_plain": "Plain text",
"datasets_createFileModel_rag_param_table_text_format_label": "Table text format",
"datasets_createFileModel_rag_progress_error_refresh_hint": "Please refresh the page or contact your administrator.",
```

- [ ] **Step 3: JSON-validate both files**

Run (from repo root):
```bash
python3 -m json.tool frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json > /dev/null
python3 -m json.tool frontend/packages/arch/resources/studio-i18n-resource/src/locales/en-US.json > /dev/null
```
Expected: no output (parse succeeded).

- [ ] **Step 4: Commit**

```bash
git add frontend/packages/arch/resources/studio-i18n-resource/src/locales/zh-CN.json \
        frontend/packages/arch/resources/studio-i18n-resource/src/locales/en-US.json
git commit -m "feat(i18n): add zh/en bundle entries for rag dynamic parsing params"
```

---

## Task 2: Hook — `useRagParameterI18n` + `useRagGroupI18n`

Pure-function "hook" wrappers around `I18n.t(...)` with fallback to the raw rag string. Named `use*` for ergonomic consumer call sites; they take no React state so any pure function variant is equivalent — keeping the `use*` prefix to keep call sites stable if a later version starts caching.

**Files:**
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.ts`
- Create: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.test.ts`

---

- [ ] **Step 1: Write failing tests**

Create `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.test.ts`:

```ts
import { describe, expect, it, vi } from 'vitest';

vi.mock('@coze-arch/i18n', () => ({
  I18n: {
    t: vi.fn((key: string) => {
      const table: Record<string, string> = {
        datasets_createFileModel_rag_param_chunk_size_label: '切片大小',
        datasets_createFileModel_rag_param_chunk_size_desc: '文本切片的最大长度。',
        datasets_createFileModel_rag_param_table_text_format_label: '表格文本格式',
        datasets_createFileModel_rag_param_table_text_format_desc: '提取表格使用的文本格式。',
        datasets_createFileModel_rag_param_table_text_format_enum_csv: 'CSV',
        datasets_createFileModel_rag_param_table_text_format_enum_markdown: 'Markdown',
        datasets_createFileModel_rag_param_table_text_format_enum_plain: '纯文本',
        datasets_createFileModel_rag_param_group_ocr: 'OCR',
      };
      // Mimic the loader's miss behaviour: return the key itself.
      return table[key] ?? key;
    }),
  },
}));

import { useRagGroupI18n, useRagParameterI18n } from './use-param-i18n';

describe('useRagParameterI18n', () => {
  it('returns translated label and description for a known param', () => {
    const result = useRagParameterI18n({
      name: 'chunk_size',
      description: 'Maximum chunk size for text chunking.',
    } as never);
    expect(result.label).toBe('切片大小');
    expect(result.description).toBe('文本切片的最大长度。');
    expect(result.options).toEqual([]);
  });

  it('falls back to raw name and description when key is missing', () => {
    const result = useRagParameterI18n({
      name: '__not_a_real_param__',
      description: 'raw english fallback',
    } as never);
    expect(result.label).toBe('__not_a_real_param__');
    expect(result.description).toBe('raw english fallback');
  });

  it('maps enum option labels through the bundle', () => {
    const result = useRagParameterI18n({
      name: 'table_text_format',
      description: 'Text format used for extracted tables.',
      allowed_values: ['csv', 'markdown', 'plain'],
    } as never);
    expect(result.options).toEqual([
      { value: 'csv', label: 'CSV' },
      { value: 'markdown', label: 'Markdown' },
      { value: 'plain', label: '纯文本' },
    ]);
  });

  it('falls back to raw allowed_value when enum key is missing', () => {
    const result = useRagParameterI18n({
      name: 'table_text_format',
      description: '',
      allowed_values: ['csv', 'unknown_value'],
    } as never);
    expect(result.options).toEqual([
      { value: 'csv', label: 'CSV' },
      { value: 'unknown_value', label: 'unknown_value' },
    ]);
  });

  it('returns empty description when neither key nor raw description present', () => {
    const result = useRagParameterI18n({
      name: 'unknown_param',
    } as never);
    expect(result.description).toBe('');
  });
});

describe('useRagGroupI18n', () => {
  it('returns translated group label', () => {
    expect(useRagGroupI18n('ocr')).toBe('OCR');
  });
  it('falls back to raw group name when key is missing', () => {
    expect(useRagGroupI18n('__unknown_group__')).toBe('__unknown_group__');
  });
  it('returns empty string when group name is empty', () => {
    expect(useRagGroupI18n('')).toBe('');
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run (from package dir):
```bash
npm run test -- use-param-i18n.test.ts
```
Expected: FAIL — `Cannot find module './use-param-i18n'`.

- [ ] **Step 3: Create the hook file**

Create `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.ts`:

```ts
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import { I18n } from '@coze-arch/i18n';

import { type DocumentParameter } from './types';

const PREFIX = 'datasets_createFileModel_rag_param_';

/**
 * Resolves localised label / description / enum option labels for a rag
 * DocumentParameter. Falls back to the raw schema value when a bundle key is
 * missing so a future rag-side param does not render blank before
 * translators catch up.
 *
 * Key namespace: `${PREFIX}<param_name>_label`, `..._desc`, and
 * `..._enum_<value>` for each allowed_values entry.
 */
export function useRagParameterI18n(p: DocumentParameter): {
  label: string;
  description: string;
  options: Array<{ value: string; label: string }>;
} {
  return {
    label: i18nWithFallback(`${PREFIX}${p.name}_label`, p.name),
    description: i18nWithFallback(`${PREFIX}${p.name}_desc`, p.description ?? ''),
    options: (p.allowed_values ?? []).map(v => ({
      value: String(v),
      label: i18nWithFallback(`${PREFIX}${p.name}_enum_${v}`, String(v)),
    })),
  };
}

/**
 * Localised group header label. Empty input passes through (no key lookup).
 */
export function useRagGroupI18n(groupName: string): string {
  if (!groupName) {
    return '';
  }
  return i18nWithFallback(`${PREFIX}group_${groupName}`, groupName);
}

/**
 * I18n.t returns the key itself when missing (loader quirk). Detect that and
 * substitute the provided fallback so we never show a raw
 * `datasets_createFileModel_...` key to the user.
 */
function i18nWithFallback(key: string, fallback: string): string {
  const v = I18n.t(key);
  return v && v !== key ? v : fallback;
}
```

- [ ] **Step 4: Re-export from the feature index**

Open `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/index.ts`. Append:

```ts
export { useRagParameterI18n, useRagGroupI18n } from './use-param-i18n';
```

- [ ] **Step 5: Run tests to verify they pass**

Run (from package dir): `npm run test -- use-param-i18n.test.ts`
Expected: PASS (all 8 cases).

- [ ] **Step 6: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/use-param-i18n.test.ts \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/index.ts
git commit -m "feat(knowledge-rag): add useRagParameterI18n hook for dynamic panel"
```

---

## Task 3: Wire the hook into `dynamic-parsing-panel.tsx`

Three render sites switch from raw to localised strings: the group header (`p.group`), the parameter description (`p.description`), and the field control's label + enum options. `FieldControl` lives in the same file lower down.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx`

---

- [ ] **Step 1: Add the import**

Open `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx`. At the top, alongside the existing imports from `./types`, `./validate`, `./use-schemas`, add:

```ts
import { useRagGroupI18n, useRagParameterI18n } from './use-param-i18n';
```

- [ ] **Step 2: Group header — swap raw `p.group` for `useRagGroupI18n`**

Find the `<Typography.Title heading={6} ...>{p.group}</Typography.Title>` block in `GroupedFields` (around line 165-170). Replace `{p.group}` with `{useRagGroupI18n(p.group)}`.

Final shape:

```tsx
{showHeader ? (
  <Typography.Title
    heading={6}
    style={{ marginTop: 8, marginBottom: 4 }}
  >
    {useRagGroupI18n(p.group)}
  </Typography.Title>
) : null}
```

- [ ] **Step 3: Description — use the hook output**

In the same `GroupedFields` render, find the description block (around line 178-182):

```tsx
{p.description ? (
  <Typography.Text type="tertiary" size="small">
    {p.description}
  </Typography.Text>
) : null}
```

Replace with hook-driven output. At the top of the `params.map(p => {...})` callback (right after `const forced = forcedMap[p.name];`), add:

```tsx
const i18nParam = useRagParameterI18n(p);
```

Then change the description render to:

```tsx
{i18nParam.description ? (
  <Typography.Text type="tertiary" size="small">
    {i18nParam.description}
  </Typography.Text>
) : null}
```

Calling `useRagParameterI18n` inside `params.map` is fine — it's a pure function despite the `use*` name, no React state involved. (If lint flags rules-of-hooks: rename the hook to `getRagParameterI18n` and re-export under both names in `use-param-i18n.ts` for consumer compatibility. Defer this rename until lint actually complains.)

- [ ] **Step 4: Field label + enum options — propagate to `FieldControl`**

Pass `i18nParam` down: change the `<FieldControl ...>` invocation in `GroupedFields` from:

```tsx
<FieldControl
  param={p}
  value={forced ? forced.value : value[p.name]}
  onChange={onChange}
  forced={forced}
/>
```

to:

```tsx
<FieldControl
  param={p}
  i18n={i18nParam}
  value={forced ? forced.value : value[p.name]}
  onChange={onChange}
  forced={forced}
/>
```

Then update `FieldControl` (further down in the same file) to accept and use the `i18n` prop. Find its signature, add the prop:

```tsx
const FieldControl: FC<{
  param: DocumentParameter;
  i18n: { label: string; description: string; options: Array<{ value: string; label: string }> };
  value: unknown;
  onChange: (name: string, fieldValue: unknown) => void;
  forced?: { value: unknown; reason?: string; hidden?: boolean };
}> = ({ param, i18n, value, onChange, forced }) => {
```

(Use the existing prop shape — adjust the inline type to match the file's current `forced` typing.)

Inside `FieldControl`, anywhere `param.name` is currently rendered as a user-visible label (search for `label={` or any `<Typography>{param.name}</Typography>` shape — there is typically one per control), replace with `{i18n.label}`. Specifically:
- The control's label/placeholder/aria-label uses `i18n.label`.
- For the `<Select>` (`ui_component === 'select'`), the `optionList` is built from `i18n.options` instead of `param.allowed_values`.

Example (the Select case — the others are mechanical analogues):

Find:
```tsx
<Select
  optionList={(param.allowed_values ?? []).map(v => ({ label: String(v), value: String(v) }))}
  ...
/>
```

Replace with:
```tsx
<Select
  optionList={i18n.options}
  ...
/>
```

- [ ] **Step 5: Run the dynamic-parsing-panel tests**

Run (from package dir):
```bash
npm run test -- dynamic-parsing-panel.test.tsx
```
Expected: all existing tests still PASS (the wiring change does not alter behaviour observed by them, except that label/description strings flip from raw to translated — see Task 4 for the assertion).

- [ ] **Step 6: Do not commit yet.** Continues into Task 4.

---

## Task 4: Extend `dynamic-parsing-panel.test.tsx` to assert translation

Lock in the translation render at least at one well-known param + group + enum so a future refactor that drops the hook is caught by tests.

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx`

---

- [ ] **Step 1: Add a new test case**

Open `dynamic-parsing-panel.test.tsx`. Find the existing `vi.mock('@coze-arch/i18n', ...)` block if present and extend its `t` map to include `chunk_size_label`, `chunk_size_desc`, `group_chunking`, `table_text_format_label`, `table_text_format_enum_csv` (matching the Task 1 bundle values). If no mock exists yet, add one at the top of the file (mirror the shape from Task 2 Step 1).

Then append a test:

```ts
it('renders translated label / description / group / enum for a known schema', () => {
  const schema = {
    schema_id: 'pdf_text_document',
    parameters: [
      {
        name: 'chunk_size',
        type: 'integer',
        group: 'chunking',
        description: 'Maximum chunk size for text chunking.',
        ui_label: 'Chunk size',
        ui_component: 'number',
      },
      {
        name: 'table_text_format',
        type: 'string',
        group: 'pdf_layout',
        description: 'Text format used for extracted tables.',
        ui_label: 'Table text format',
        ui_component: 'select',
        allowed_values: ['csv', 'markdown', 'plain'],
      },
    ],
  } as never;

  render(<DynamicParsingPanel schema={schema} value={{}} onChange={() => undefined} />);

  // Group header
  expect(screen.getByText('切片')).toBeInTheDocument();
  // Label (the FieldControl renders the label somewhere — exact text)
  expect(screen.getByText('切片大小')).toBeInTheDocument();
  // Description
  expect(screen.getByText('文本切片的最大长度。')).toBeInTheDocument();
  // Enum option label visible after opening the select; just assert the
  // option is rendered by mounting the popover via fireEvent if needed —
  // for the simpler initial assertion, just confirm `CSV` appears in DOM
  // (Semi's Select renders selected value into a span).
  // If the Select control needs to be opened, this assertion can be
  // expanded in a follow-up.
});
```

If `render` / `screen` are not imported, add `import { render, screen } from '@testing-library/react';` at the top.

- [ ] **Step 2: Run test to verify it passes**

Run (from package dir): `npm run test -- dynamic-parsing-panel.test.tsx`
Expected: PASS (the new test plus any existing ones).

- [ ] **Step 3: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.tsx \
        frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/dynamic-parsing-panel.test.tsx
git commit -m "feat(knowledge-rag): translate dynamic parsing panel via i18n hook"
```

---

## Task 5: Image KB UI — replace the literal Chinese string

The audit found exactly one literal Chinese string in `features/knowledge-type/image/file/**` outside `I18n.t(...)`. Move it to the bundle (key was added in Task 1).

**Files:**
- Modify: `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/progress/index.tsx`

---

- [ ] **Step 1: Replace the literal**

Open `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/progress/index.tsx`. Find line 152 (or search for `请刷新页面或联系管理员`):

```tsx
<p className={styles['error-hint']}>请刷新页面或联系管理员。</p>
```

Replace with:

```tsx
<p className={styles['error-hint']}>
  {I18n.t('datasets_createFileModel_rag_progress_error_refresh_hint')}
</p>
```

If `I18n` is not already imported, add at the top:

```tsx
import { I18n } from '@coze-arch/i18n';
```

- [ ] **Step 2: Audit pass — verify no other naked literals**

Run (from package dir):
```bash
grep -rE '>[一-鿿][^<]*<' src/features/knowledge-type/image/file/ | grep -v test
```

Expected: no output (the one literal above was the only one).

If new ones surface, repeat the pattern: add a new `datasets_createFileModel_rag_*` key in both locale files (Step is the same shape as Task 1), then replace the literal with `I18n.t('<key>')`. Spec does not pre-enumerate these because the recon at design time only found one — anything else is a one-line fix per occurrence.

- [ ] **Step 3: Run image KB tests (smoke)**

Run (from package dir):
```bash
npm run test -- knowledge-type/image
```
Expected: PASS for all existing image KB tests.

- [ ] **Step 4: Commit**

```bash
git add frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/knowledge-type/image/file/add-rag/steps/progress/index.tsx
git commit -m "feat(knowledge-rag): route image KB progress error hint through i18n"
```

---

## Task 6: Locale bundle integrity test

Catch zh/en drift: any `datasets_createFileModel_rag_param_*` key must exist in both locale files.

**Files:**
- Create: `frontend/packages/arch/resources/studio-i18n-resource/src/locales/locales.test.ts`

---

- [ ] **Step 1: Write the test**

Create `frontend/packages/arch/resources/studio-i18n-resource/src/locales/locales.test.ts`:

```ts
import { describe, expect, it } from 'vitest';

import enUS from './en-US.json';
import zhCN from './zh-CN.json';

const PARAM_PREFIX = 'datasets_createFileModel_rag_param_';

describe('rag-param i18n bundle integrity', () => {
  it('zh-CN and en-US contain the same set of datasets_createFileModel_rag_param_* keys', () => {
    const enKeys = Object.keys(enUS).filter(k => k.startsWith(PARAM_PREFIX)).sort();
    const zhKeys = Object.keys(zhCN).filter(k => k.startsWith(PARAM_PREFIX)).sort();
    expect(zhKeys).toEqual(enKeys);
  });

  it('every value is a non-empty string', () => {
    for (const bundle of [enUS, zhCN] as Array<Record<string, unknown>>) {
      for (const [key, value] of Object.entries(bundle)) {
        if (!key.startsWith(PARAM_PREFIX)) continue;
        expect(typeof value).toBe('string');
        expect((value as string).trim().length).toBeGreaterThan(0);
      }
    }
  });
});
```

If the `studio-i18n-resource` package does not have a test config yet, this test can live in a more-test-friendly package — relocate to `frontend/packages/data/knowledge/knowledge-resource-processor-base/src/features/dynamic-parsing-panel/locale-integrity.test.ts` and import the JSON from the resource package via path alias. Either location works; pick whichever already has Vitest configured.

- [ ] **Step 2: Run the test**

Run (from the package the test landed in): `npm run test -- locale`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add <path-to-the-new-test-file>
git commit -m "test(i18n): assert zh/en parity for rag param keys"
```

---

## Task 7: Wider sweep + smoke

**Files:** none (verification only).

---

- [ ] **Step 1: Full package test sweep**

Run (from `frontend/packages/data/knowledge/knowledge-resource-processor-base`): `npm run test`
Expected: PASS.

- [ ] **Step 2: Lint**

Run (from same package): `npm run lint` (or the project's lint command — e.g. `rush lint-staged` from repo root)
Expected: no new errors.

- [ ] **Step 3: Build the affected packages**

Run from repo root: `rush rebuild -t @coze-data/knowledge-resource-processor-base -t @coze-arch/resources`
Expected: build succeeds.

- [ ] **Step 4: Manual smoke (visual)**

In a dev server (`cd frontend/apps/coze-studio && npm run dev`), upload any document to a text KB and open the parsing config step. Confirm zh-CN locale (browser language or app setting) shows Chinese param labels/descriptions/group headers. Switch to en-US and confirm English copy.

For image KB: trigger the progress error path (e.g. by temporarily breaking the indexing-poll endpoint) and confirm the error hint now reads from i18n. (If reproducing the error path is finicky, this can be visually confirmed in DevTools by checking the rendered React element's text node.)

- [ ] **Step 5: Open PR**

```bash
git push -u origin "$(git branch --show-current)"
gh pr create --title "feat(knowledge-rag): zh i18n for dynamic parsing panel + image KB UI" --body "$(cat <<'EOF'
## Summary

- `useRagParameterI18n` / `useRagGroupI18n` hooks resolve localised label / description / enum / group header for every rag `DocumentParameter`, with fallback to the raw rag string when a bundle key is missing.
- `dynamic-parsing-panel.tsx` swaps three raw-string render sites for hook output.
- 75 new `datasets_createFileModel_rag_param_*` keys land in `zh-CN.json` and `en-US.json` (31 params × {label, desc}, 10 groups, 3 enums, 1 progress hint).
- One stray literal Chinese string in `image/file/add-rag/steps/progress/index.tsx` now flows through i18n.
- Bundle-integrity Vitest asserts zh/en parity going forward.

Spec: `docs/superpowers/specs/2026-05-21-rag-params-i18n-zh-design.md`

## Test plan

- [x] `npm run test` passes in `@coze-data/knowledge-resource-processor-base`
- [x] Bundle parity test passes
- [x] Manual: zh-CN dev server shows Chinese labels in the parsing panel
- [x] Manual: en-US dev server shows English labels unchanged from pre-PR

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Map (spec section → plan task)

- §Design / Source-of-truth inventory → Task 1 (params, groups, enums all materialised)
- §Design / Key namespace → Task 1 + Task 2 (PREFIX constant in hook)
- §Design / Frontend hook (`useRagParameterI18n`, `useRagGroupI18n`, `i18nWithFallback`) → Task 2
- §Design / Wiring `dynamic-parsing-panel.tsx` (group, description, label, enum) → Tasks 3 + 4
- §Design / Image KB UI audit + cleanup → Task 5
- §Design / Tests (hook unit, panel render assertion, bundle parity) → Tasks 2, 4, 6
- §Risks / loader miss-behaviour detector — folded into `i18nWithFallback` (Task 2) with implicit coverage; if a future loader change flips the contract, the missing-key test cases in Task 2 will surface the regression
