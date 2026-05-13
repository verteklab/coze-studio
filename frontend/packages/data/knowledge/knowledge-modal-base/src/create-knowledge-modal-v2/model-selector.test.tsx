/*
 * Copyright 2025 coze-dev Authors
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

import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';

// Mock @coze-arch/coze-design — its real Select pulls in CSS that vitest can't
// resolve in this package's environment. The contract under test is the
// component's own state/onChange logic, not the design-system Select internals.
vi.mock('@coze-arch/coze-design', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention
  Select: ({
    value,
    onChange,
    optionList,
  }: {
    value: string;
    onChange: (v: string) => void;
    optionList: Array<{ label: string; value: string }>;
  }) => (
    <select
      data-testid={`select-${optionList.map(o => o.value).join('-')}`}
      value={value}
      onChange={e => onChange(e.target.value)}
    >
      {optionList.map(opt => (
        <option key={opt.value} value={opt.value}>
          {opt.label}
        </option>
      ))}
    </select>
  ),
}));

import { ModelSelector } from './model-selector';

describe('ModelSelector', () => {
  it('renders text and image model options from /model_providers', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      text_models: [{ id: 't1', name: 'Text Model 1' }],
      image_models: [{ id: 'i1', name: 'Image Model 1' }],
    });
    const onChange = vi.fn();
    render(<ModelSelector fetchProviders={fetcher} onChange={onChange} />);
    await waitFor(() => expect(fetcher).toHaveBeenCalled());
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({
        textModelId: 't1',
        imageModelId: 'i1',
      }),
    );
    expect(screen.queryByText('Text Model 1')).not.toBeNull();
    expect(screen.queryByText('Image Model 1')).not.toBeNull();
  });

  it('emits onChange with selected ids when user picks a different option', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      text_models: [
        { id: 't1', name: 'T1' },
        { id: 't2', name: 'T2' },
      ],
      image_models: [{ id: 'i1', name: 'I1' }],
    });
    const onChange = vi.fn();
    render(<ModelSelector fetchProviders={fetcher} onChange={onChange} />);
    await waitFor(() => screen.getByText('T1'));
    const textSelect = screen.getByTestId('select-t1-t2') as HTMLSelectElement;
    fireEvent.change(textSelect, { target: { value: 't2' } });
    await waitFor(() =>
      expect(onChange).toHaveBeenLastCalledWith({
        textModelId: 't2',
        imageModelId: 'i1',
      }),
    );
  });

  it('falls back to empty string when no providers returned', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      text_models: [],
      image_models: [],
    });
    const onChange = vi.fn();
    render(<ModelSelector fetchProviders={fetcher} onChange={onChange} />);
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({
        textModelId: '',
        imageModelId: '',
      }),
    );
  });

  it('shows loading state before providers resolve', () => {
    const fetcher = vi.fn().mockReturnValue(new Promise(() => undefined));
    render(<ModelSelector fetchProviders={fetcher} onChange={vi.fn()} />);
    expect(screen.queryByText(/Loading models/)).not.toBeNull();
  });
});
