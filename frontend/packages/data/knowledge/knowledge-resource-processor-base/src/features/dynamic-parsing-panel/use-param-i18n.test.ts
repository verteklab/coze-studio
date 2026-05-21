/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import { describe, expect, it, vi } from 'vitest';

vi.mock('@coze-arch/i18n', () => ({
  I18n: {
    t: vi.fn((key: string) => {
      const table: Record<string, string> = {
        datasets_createFileModel_rag_param_chunk_size_label: '切片大小',
        datasets_createFileModel_rag_param_chunk_size_desc:
          '文本切片的最大长度。',
        datasets_createFileModel_rag_param_table_text_format_label:
          '表格文本格式',
        datasets_createFileModel_rag_param_table_text_format_desc:
          '提取表格使用的文本格式。',
        datasets_createFileModel_rag_param_table_text_format_enum_csv: 'CSV',
        datasets_createFileModel_rag_param_table_text_format_enum_markdown:
          'Markdown',
        datasets_createFileModel_rag_param_table_text_format_enum_plain:
          '纯文本',
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
