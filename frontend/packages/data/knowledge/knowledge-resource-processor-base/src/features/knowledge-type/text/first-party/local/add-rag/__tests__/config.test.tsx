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

// `useTextDisplaySegmentStepCheck` and `UploadFooter` are referenced in the
// config closure but never invoked at import time; still, their module init
// reaches design-system CSS that vitest can't resolve here. The config-level
// assertions don't render, so a no-op stub is sufficient.
vi.mock('@/hooks/common', () => ({
  useTextDisplaySegmentStepCheck: () => [undefined, undefined],
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
  TextUpload: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  TextProgress: () => null,
}));

// The legacy `<TextSegment />` is reused as the SEGMENT_CLEANER step from
// Phase 3 onward. Same stub strategy as the other step components above.
vi.mock('../../add/steps/segment', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  TextSegment: () => null,
}));

// The store factory is referenced as `createStore` on the config but not
// invoked here; stub to a no-op to avoid pulling in the full slice graph.
vi.mock('../../add/store', () => ({
  createTextLocalAddUpdateStore: () => () => undefined,
}));

import { TextLocalAddRagStep } from '../constants';
import { TextLocalAddRagConfig } from '../config';

describe('TextLocalAddRagConfig', () => {
  it('exposes three steps in upload→segment→progress order', () => {
    expect(TextLocalAddRagConfig.steps).toHaveLength(3);
    expect(TextLocalAddRagConfig.steps[0].step).toBe(
      TextLocalAddRagStep.UPLOAD,
    );
    expect(TextLocalAddRagConfig.steps[1].step).toBe(
      TextLocalAddRagStep.SEGMENT_CLEANER,
    );
    expect(TextLocalAddRagConfig.steps[2].step).toBe(
      TextLocalAddRagStep.PROGRESS,
    );
  });

  it('wires a content renderer for every step', () => {
    for (const step of TextLocalAddRagConfig.steps) {
      expect(typeof step.content).toBe('function');
    }
  });

  it('aligns step numeric values with the legacy components reused inside', () => {
    // Legacy `<TextUpload />` step calls
    // `setCurrentStep(TextLocalAddUpdateStep.SEGMENT_CLEANER)` = 1 on Next;
    // legacy `<TextSegment />` calls
    // `setCurrentStep(TextLocalAddUpdateStep.SEGMENT_PREVIEW)` = 2 on Next.
    // Both components are reused as-is here, so the rag enum values must
    // align: SEGMENT_CLEANER=1 catches the upload handoff and PROGRESS=2
    // catches the segment handoff (skipping the absent review step).
    expect(TextLocalAddRagStep.UPLOAD).toBe(0);
    expect(TextLocalAddRagStep.SEGMENT_CLEANER).toBe(1);
    expect(TextLocalAddRagStep.PROGRESS).toBe(2);
  });
});
