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

import { describe, it, expect } from 'vitest';

import { isRagBackend } from '../../src/utils/is-rag-backend';

describe('isRagBackend', () => {
  it('returns true for backend="rag"', () => {
    expect(isRagBackend({ backend: 'rag' } as any)).toBe(true);
  });
  it('returns false for backend="legacy"', () => {
    expect(isRagBackend({ backend: 'legacy' } as any)).toBe(false);
  });
  it('returns false for backend undefined (safe legacy fallback)', () => {
    expect(isRagBackend({} as any)).toBe(false);
  });
  it('returns false for backend null', () => {
    expect(isRagBackend({ backend: null } as any)).toBe(false);
  });
  it('returns false for an unknown backend string', () => {
    expect(isRagBackend({ backend: 'mystery' } as any)).toBe(false);
  });
});
