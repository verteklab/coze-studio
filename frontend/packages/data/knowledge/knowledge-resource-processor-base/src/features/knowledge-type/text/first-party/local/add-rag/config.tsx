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
  type UploadConfig,
  type FooterControlsProps,
} from '@coze-data/knowledge-resource-processor-core';
import { I18n } from '@coze-arch/i18n';

import { useTextDisplaySegmentStepCheck } from '@/hooks/common';
import { UploadFooter } from '@/components';

import {
  createTextLocalAddUpdateStore,
  type UploadTextLocalAddUpdateStore,
} from '../add/store';
import { TextSegment } from '../add/steps/segment';
import { TextUpload, TextProgress } from './steps';
import { TextLocalAddRagStep } from './constants';

/**
 * Rag-mode wizard for text local upload. 3 steps:
 * `[UPLOAD, SEGMENT_CLEANER, PROGRESS]`.
 *
 * Structural twin of {@link TextLocalAddUpdateConfig} (legacy), minus the
 * SEGMENT_PREVIEW step — rag has no document-review workflow, so on Next
 * we go straight from the segment-config step to the progress poll.
 *
 * Phase 3 brought SEGMENT_CLEANER back: the original RAG wizard dropped
 * it on the (incorrect) assumption that rag locks chunking at KB-creation
 * time, but rag's `POST /documents` actually takes per-document chunk_size,
 * chunk_overlap, enable_ocr, enable_image_embedding, and a schema-scoped
 * `document_options` JSON. Without this step the user couldn't influence
 * any of those — the upload silently defaulted everything.
 *
 * The segment step itself is the legacy `<TextSegment />` reused verbatim:
 * it writes parsingStrategy / segmentMode / segmentRule into the shared
 * store, and the rag progress step (`./steps/progress`) already reads those
 * fields and forwards them to `KnowledgeApi.CreateDocument`. Backend
 * Phase 1 (commit a5f32092) maps the parsing fields to rag's form params.
 *
 * Selection between this config and the legacy one is handled in
 * `scenes/base/config.ts` (see Task 10) by gating on `kb.backend === 'rag'`.
 */
export const TextLocalAddRagConfig: UploadConfig<
  TextLocalAddRagStep,
  UploadTextLocalAddUpdateStore
> = {
  steps: [
    {
      content: props => (
        <TextUpload
          useStore={props.useStore}
          footer={(controls: FooterControlsProps) => (
            <UploadFooter controls={controls} />
          )}
          checkStatus={props.checkStatus}
        />
      ),
      title: I18n.t('datasets_createFileModel_step2'),
      step: TextLocalAddRagStep.UPLOAD,
    },
    {
      content: props => (
        <TextSegment
          useStore={props.useStore}
          footer={(controls: FooterControlsProps) => (
            <UploadFooter controls={controls} />
          )}
          checkStatus={undefined}
        />
      ),
      title: I18n.t('kl_write_107'),
      step: TextLocalAddRagStep.SEGMENT_CLEANER,
    },
    {
      content: props => <TextProgress useStore={props.useStore} />,
      title: I18n.t('datasets_createFileModel_step4'),
      step: TextLocalAddRagStep.PROGRESS,
    },
  ],
  createStore: createTextLocalAddUpdateStore,
  useUploadMount: () => useTextDisplaySegmentStepCheck(),
};
