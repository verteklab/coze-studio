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

import { describe, it, expect, vi } from 'vitest';

// config.tsx pulls in step components that transitively touch i18n,
// design-system CSS, zustand stores, and the wider knowledge-stores context.
// We're asserting on the steps array shape — not rendering anything — so
// stub out the leaf-level deps that crash at module-init time.
vi.mock('@coze-arch/i18n', () => ({
  I18n: { t: (key: string) => key },
}));

// `useTableCheck` and `UploadFooter` are referenced in the config closure but
// never invoked at import time; still, their module init reaches design-system
// CSS that vitest can't resolve here. The config-level assertions don't render,
// so a no-op stub is sufficient.
vi.mock('../../../hooks', () => ({
  useTableCheck: () => null,
}));
vi.mock('@/components', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  UploadFooter: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  UploadProgressPoll: () => null,
}));

// The legacy upload step and the new progress step are wrapped in step
// `content` factories that vitest never actually executes during these
// assertions; we stub them to keep the module graph small.
vi.mock('../steps', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  TableUpload: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  TableProgress: () => null,
}));

// The store factory is referenced as `createStore` on the config but not
// invoked here; stub to a no-op to avoid pulling in the full slice graph.
vi.mock('../../add/store', () => ({
  createTableLocalAddStore: () => () => undefined,
}));

import { TableLocalAddRagStep } from '../constants';
import { TableLocalAddRagConfig } from '../config';

describe('TableLocalAddRagConfig', () => {
  it('exposes exactly two steps in upload-then-progress order', () => {
    expect(TableLocalAddRagConfig.steps).toHaveLength(2);
    expect(TableLocalAddRagConfig.steps[0].step).toBe(
      TableLocalAddRagStep.UPLOAD,
    );
    expect(TableLocalAddRagConfig.steps[1].step).toBe(
      TableLocalAddRagStep.PROGRESS,
    );
  });

  it('wires a content renderer for every step', () => {
    for (const step of TableLocalAddRagConfig.steps) {
      expect(typeof step.content).toBe('function');
    }
  });

  it('aligns PROGRESS with the value legacy TableUpload pushes (1)', () => {
    // Legacy `TableUpload` step calls
    // `setCurrentStep(TableLocalStep.CONFIGURATION)` which is numeric 1
    // (see table/first-party/local/constants.ts:
    // `UPLOAD = 0, CONFIGURATION, PREVIEW, PROCESSING`). Rag wizard reuses
    // that component as-is, so PROGRESS must equal 1 for the handoff to
    // land on the right step.
    expect(TableLocalAddRagStep.PROGRESS).toBe(1);
    expect(TableLocalAddRagStep.UPLOAD).toBe(0);
  });
});
