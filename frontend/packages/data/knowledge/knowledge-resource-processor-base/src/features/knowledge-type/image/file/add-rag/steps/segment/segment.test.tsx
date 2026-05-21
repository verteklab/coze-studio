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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock the schemas hook so we can drive the segment step's input directly.
const useRagDocumentParameterSchemasMock = vi.fn();
vi.mock('@/features/dynamic-parsing-panel', async () => {
  const actual = await vi.importActual('@/features/dynamic-parsing-panel');
  return {
    ...actual,
    useRagDocumentParameterSchemas: () => useRagDocumentParameterSchemasMock(),
  };
});

vi.mock('@coze-arch/i18n', () => ({
  I18n: { t: (k: string) => k },
}));

// Stub design-system primitives; the real package pulls CSS vitest can't
// resolve in this env. Stubs expose enough structure for the assertions.
// NOTE: DynamicParsingPanel is the real component (importActual above), so we
// must stub ALL coze-design exports it also uses (Switch, Tooltip, Input, etc).
vi.mock('@coze-arch/coze-design', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Button: ({
    children,
    onClick,
  }: {
    children?: React.ReactNode;
    onClick?: () => void;
  }) => <button onClick={onClick}>{children}</button>,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Select: ({
    value,
    optionList,
  }: {
    value?: string;
    optionList?: { label: string; value: string }[];
    onChange?: (v: unknown) => void;
  }) => (
    <select defaultValue={value}>
      {(optionList ?? []).map(o => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  ),
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

  Toast: { error: vi.fn() },
  Typography: {
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    Title: ({ children }: { children?: React.ReactNode }) => (
      <div>{children}</div>
    ),
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    Text: ({ children }: { children?: React.ReactNode }) => (
      <span>{children}</span>
    ),
  },
}));

// CollapsePanel is imported transitively by DynamicParsingPanel.
vi.mock('@/components', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  CollapsePanel: ({ children }: { children?: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

// Minimal zustand-shape store mock matching ImageFileAddStore's shape used by
// ImageRagSegment. Only the keys the component reads are present.
const storeState = {
  unitList: [{ type: 'jpg' }] as { type: string }[],
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
      required: false,
      advanced: false,
      default: false,
      description: 'Whether to extract text via OCR.',
      ui_label: 'Enable OCR',
      ui_component: 'switch' as const,
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
      required: false,
      advanced: false,
      default: true,
      description: 'Scanned documents require OCR to produce text chunks.',
      ui_label: 'Enable OCR',
      ui_component: 'switch' as const,
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

    // The image schema's enable_ocr toggle's label (rag's raw `ui_label`).
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
