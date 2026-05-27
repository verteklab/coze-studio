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

// `useImageDisplayAnnotationStepCheck` and `UploadFooter` are referenced in
// the config closure but never invoked at import time; still, their module
// init reaches design-system CSS that vitest can't resolve here. The config-
// level assertions don't render, so a no-op stub is sufficient.
vi.mock('@/hooks/common', () => ({
  useImageDisplayAnnotationStepCheck: () => [undefined, undefined],
}));
vi.mock('@/components', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  UploadFooter: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  UploadProgressPoll: () => null,
}));

// Step components are wrapped in step `content` factories that vitest
// never actually executes during these assertions; we stub them to keep
// the module graph small. Phase 3b adds <ImageRagSegment /> as the new
// SEGMENT_CLEANER step between <ImageUpload /> and <ImageProgress />.
vi.mock('../steps', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  ImageUpload: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  ImageRagSegment: () => null,
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  ImageProgress: () => null,
}));

// The store factory is referenced as `createStore` on the config but not
// invoked here; stub to a no-op to avoid pulling in the full slice graph.
vi.mock('../../store', () => ({
  createImageFileAddStore: () => () => undefined,
}));

import { ImageFileAddRagStep } from '../constants';
import { ImageFileAddRagConfig } from '../config';

describe('ImageFileAddRagConfig', () => {
  it('exposes three steps in upload→segment→progress order', () => {
    expect(ImageFileAddRagConfig.steps).toHaveLength(3);
    expect(ImageFileAddRagConfig.steps[0].step).toBe(
      ImageFileAddRagStep.UPLOAD,
    );
    expect(ImageFileAddRagConfig.steps[1].step).toBe(
      ImageFileAddRagStep.SEGMENT_CLEANER,
    );
    expect(ImageFileAddRagConfig.steps[2].step).toBe(
      ImageFileAddRagStep.PROGRESS,
    );
  });

  it('wires a content renderer for every step', () => {
    for (const step of ImageFileAddRagConfig.steps) {
      expect(typeof step.content).toBe('function');
    }
  });

  it('aligns step numeric values with the legacy components reused inside', () => {
    // Legacy `<ImageUpload />` step calls
    // `setCurrentStep(ImageFileAddStep.Annotation)` = 1 on Next (see
    // image/file/types.ts: `Upload = 0, Annotation, Process`). We reuse
    // that component, so SEGMENT_CLEANER must equal 1 for the handoff to
    // land on the right step. PROGRESS is the next free value (2).
    expect(ImageFileAddRagStep.UPLOAD).toBe(0);
    expect(ImageFileAddRagStep.SEGMENT_CLEANER).toBe(1);
    expect(ImageFileAddRagStep.PROGRESS).toBe(2);
  });
});
