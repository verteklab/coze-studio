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
import { ImageUpload, ImageRagSegment, ImageProgress } from './steps';
import { ImageFileAddRagStep } from './constants';

/**
 * Rag-mode wizard for image file upload. 3 steps:
 * `[UPLOAD, SEGMENT_CLEANER, PROGRESS]`.
 *
 * Structural twin of {@link ImageFileAddConfig} (legacy) — same store
 * factory, same upload step, same `useUploadMount` shape — but with the
 * ANNOTATION step replaced by a schema-driven SEGMENT_CLEANER and the
 * legacy PROCESS step replaced by a `<UploadProgressPoll />`-backed
 * `<ImageProgress />`.
 *
 * Phase 3b expanded this wizard from 2 to 3 steps: the original RAG wizard
 * dropped the legacy ANNOTATION step because `ExtractPhotoCaption` was
 * still stubbed on the rag side, but rag's `POST /documents` actually
 * accepts per-image enable_ocr, enable_image_embedding, ocr_model_id,
 * ocr_languages, and a schema-scoped `document_options` JSON for the
 * `image_document` / `scanned_document` schemas. Without a configuration
 * step the user could not influence any of those — every image upload
 * silently defaulted everything.
 *
 * `<ImageRagSegment />` is the image analog of `<TextRagSegment />`: it
 * pulls the per-file-type parameter catalog from Phase 2 endpoint, picks
 * between `image_document` and `scanned_document` via a selector, renders
 * the matched schema's parameters, and writes the serialised payload
 * into `state.documentOptions`. `<ImageProgress />` forwards it as
 * `CreateDocumentRequest.document_options` so backend Phase 3b-1
 * (commit 6cdf670f) can pass it verbatim to rag's POST /documents.
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
      content: props => (
        <ImageRagSegment
          useStore={props.useStore}
          footer={(controls: FooterControlsProps) => (
            <UploadFooter controls={controls} />
          )}
          checkStatus={undefined}
        />
      ),
      title: I18n.t('kl_write_107'),
      step: ImageFileAddRagStep.SEGMENT_CLEANER,
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
