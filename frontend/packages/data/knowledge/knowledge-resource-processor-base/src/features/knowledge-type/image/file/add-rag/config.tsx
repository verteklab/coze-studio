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

import {
  type FooterControlsProps,
  type UploadConfig,
} from '@coze-data/knowledge-resource-processor-core';
import { I18n } from '@coze-arch/i18n';

import { useImageDisplayAnnotationStepCheck } from '@/hooks/common';
import { UploadFooter } from '@/components';

import { createImageFileAddStore, type ImageFileAddStore } from '../store';
import { ImageUpload, ImageProgress } from './steps';
import { ImageFileAddRagStep } from './constants';

/**
 * Rag-mode wizard for image file upload. 2 steps: `[UPLOAD, PROGRESS]`.
 *
 * Structural twin of {@link ImageFileAddConfig} (legacy) — same store
 * factory, same upload step, same `useUploadMount` shape — but with the
 * ANNOTATION step removed because the rag service's `ExtractPhotoCaption`
 * stub is not yet wired (see Task 0 of the rag-flow alignment plan). The
 * progress step replaces the legacy PROCESS step; it calls CreateDocument
 * directly and adapts `<UploadProgressPoll />` for polling + navigation.
 *
 * Selection between this config and the legacy one is handled in
 * `scenes/base/config.ts` (see Task 10) by gating on `kb.backend === 'rag'`.
 */
export const ImageFileAddRagConfig: UploadConfig<
  ImageFileAddRagStep,
  ImageFileAddStore
> = {
  steps: [
    {
      content: props => (
        <ImageUpload
          {...props}
          footer={(controls: FooterControlsProps) => (
            <UploadFooter controls={controls} />
          )}
        />
      ),
      title: I18n.t('knowledge_photo_006'),
      step: ImageFileAddRagStep.UPLOAD,
    },
    {
      content: props => <ImageProgress {...props} />,
      title: I18n.t('db_table_0126_015'),
      step: ImageFileAddRagStep.PROGRESS,
    },
  ],
  createStore: createImageFileAddStore,
  useUploadMount: () => useImageDisplayAnnotationStepCheck(),
};
