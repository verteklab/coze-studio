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

import { describe, expect, it, vi } from 'vitest';
import { renderHook } from '@testing-library/react-hooks';

const mockToResource = vi.fn();

vi.mock('@coze-data/knowledge-stores', () => ({
  useDataNavigate: () => ({
    toResource: mockToResource,
  }),
  useKnowledgeParams: () => ({
    datasetID: 'dataset-123',
  }),
}));

vi.mock('@coze-arch/i18n', () => ({
  I18n: {
    t: (key: string) => key,
  },
}));

import { useUploadCancelButton } from './use-upload-cancel-button';

describe('useUploadCancelButton', () => {
  it('returns an enabled cancel footer button that navigates to the document list', () => {
    const { result } = renderHook(() => useUploadCancelButton());

    expect(result.current.text).toBe('Cancel');
    expect(result.current.status).toBeUndefined();

    result.current.onClick();

    expect(mockToResource).toHaveBeenCalledWith('knowledge', 'dataset-123');
  });
});
