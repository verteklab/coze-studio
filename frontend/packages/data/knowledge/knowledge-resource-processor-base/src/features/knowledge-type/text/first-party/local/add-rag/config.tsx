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
import { TextUpload, TextRagSegment, TextProgress } from './steps';
import { TextLocalAddRagStep } from './constants';

/**
 * Rag-mode wizard for text local upload. 3 steps:
 * `[UPLOAD, SEGMENT_CLEANER, PROGRESS]`.
 *
 * Structural twin of {@link TextLocalAddUpdateConfig} (legacy), minus the
 * SEGMENT_PREVIEW step — rag has no document-review workflow, so on Next
 * we go straight from the segment-config step to the progress poll.
 *
 * Phase 3a brought SEGMENT_CLEANER back as the legacy `<TextSegment />`.
 * Phase 3b swaps that for `<TextRagSegment />`, a schema-driven dynamic
 * form: it pulls the per-file-type parameter catalog from
 * `GET /api/knowledge/rag/document_parameter_schemas` and renders rag's
 * actual knobs (chunk_size, OCR/image bools, parser-specific options) per
 * `ui_component` hint. The form's output is serialised to JSON and stored
 * on `documentOptions`; the progress step forwards it as
 * `CreateDocumentRequest.document_options` so backend Phase 3b-1
 * (commit 6cdf670f) can pass it verbatim to rag's POST /documents.
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
        <TextRagSegment
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
