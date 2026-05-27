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
import '@testing-library/jest-dom';

// i18n: use a lookup table that returns the key itself on miss so:
//  (a) existing tests that assert against the key string continue to pass, and
//  (b) the translation render test can assert the Chinese text for known keys.
const i18nTable: Record<string, string> = {
  datasets_createFileModel_rag_param_chunk_size_label: '切片大小',
  datasets_createFileModel_rag_param_chunk_size_desc: '文本切片的最大长度。',
  datasets_createFileModel_rag_param_group_chunking: '切片',
  datasets_createFileModel_rag_param_table_text_format_label: '表格文本格式',
  datasets_createFileModel_rag_param_table_text_format_enum_csv: 'CSV',
};
vi.mock('@coze-arch/i18n', () => ({
  I18n: { t: (key: string) => i18nTable[key] ?? key },
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

import { type DocumentParameter, type DocumentParameterSchema } from './types';
import { DynamicParsingPanel } from './dynamic-parsing-panel';

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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
    // I18n.t is mocked above to return the key. Asserting the key appears in
    // the rendered DOM proves the panel emitted a hint element bound to the
    // correct i18n key. The Tooltip stub also exposes its content via
    // data-content so the hover-tooltip path is exercised.
    expect(
      screen.queryAllByText('datasets_createFileModel_rag_forced_ocr_hint', {
        exact: false,
      }).length,
    ).toBeGreaterThan(0);
    const tooltip = screen.queryByTestId('tooltip');
    expect(tooltip).not.toBeNull();
    expect(tooltip?.getAttribute('data-content')).toBe(
      'datasets_createFileModel_rag_forced_ocr_hint',
    );
  });

  it('hides ocr_model_id control on image_document (forced hidden)', () => {
    // As of 2026-05-22, ocr_model_id is in the forced map with hidden:true.
    // The panel must not render it even though filterParamsByDependencies
    // would show it when enable_ocr is on — the hidden flag takes precedence.
    const s = schemaOf('image_document', [
      param({
        name: 'enable_ocr',
        ui_component: 'switch',
        group: 'ocr',
        default: false,
      }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        group: 'ocr',
        default: 'paddle',
      }),
    ]);
    render(
      <DynamicParsingPanel
        schema={s}
        value={{ enable_ocr: false }}
        onChange={vi.fn()}
      />,
    );
    // ocr_model_id is hidden — no text input should be rendered for it.
    expect(document.getElementById('dpp-ocr_model_id')).toBeNull();
  });

  it('hides enable_image_embedding control on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        group: 'image_chunking',
        default: true,
      }),
    ]);
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
    expect(document.getElementById('dpp-enable_image_embedding')).toBeNull();
    expect(document.getElementById('dpp-produce_image_chunk')).toBeNull();
  });

  it('does NOT hide enable_image_embedding on unforced schemas', () => {
    const s = schemaOf('text_document', [
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: false,
      }),
    ]);
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
    expect(
      document.getElementById('dpp-enable_image_embedding'),
    ).not.toBeNull();
  });

  it('hides produce_text_chunk control on image_document', () => {
    const s = schemaOf('image_document', [
      param({
        name: 'produce_text_chunk',
        ui_component: 'switch',
        group: 'chunk_outputs',
        default: false,
      }),
    ]);
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
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
    render(<DynamicParsingPanel schema={s} value={{}} onChange={vi.fn()} />);
    expect(document.getElementById('dpp-produce_text_chunk')).toBeNull();
  });
});

describe('DynamicParsingPanel translation rendering', () => {
  it('renders translated label / description / group for a known schema', () => {
    const schema = schemaOf('pdf_text_document', [
      {
        name: 'chunk_size',
        type: 'integer',
        group: 'chunking',
        required: false,
        advanced: false,
        description: 'Maximum chunk size for text chunking.',
        ui_label: 'Chunk size',
        ui_component: 'number',
      },
      {
        name: 'table_text_format',
        type: 'string',
        group: 'pdf_layout',
        required: false,
        advanced: false,
        description: 'Text format used for extracted tables.',
        ui_label: 'Table text format',
        ui_component: 'select',
        allowed_values: ['csv', 'markdown', 'plain'],
      },
    ]);

    render(
      <DynamicParsingPanel
        schema={schema}
        value={{}}
        onChange={() => undefined}
      />,
    );

    // Group header is translated via getRagGroupI18n
    expect(screen.getByText('切片')).toBeInTheDocument();
    // Param label is translated via getRagParameterI18n
    expect(screen.getByText('切片大小')).toBeInTheDocument();
    // Param description is translated via getRagParameterI18n
    expect(screen.getByText('文本切片的最大长度。')).toBeInTheDocument();
  });
});
