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

  it('keeps OCR-dependent params visible on image_document even when value.enable_ocr=false', () => {
    // Regression: filterParamsByDependencies sees the patched effective
    // value, so ocr_model_id (group="ocr") stays visible alongside the
    // forced-on enable_ocr toggle. Without the effectiveValue fix, this
    // would have filtered ocr_model_id out and made the panel internally
    // inconsistent.
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
    // The text input for ocr_model_id is a mocked <input type="text">.
    // There should be exactly one such input rendered (for ocr_model_id).
    const textInputs = document.querySelectorAll('input[type="text"]');
    expect(textInputs.length).toBeGreaterThan(0);
  });
});
