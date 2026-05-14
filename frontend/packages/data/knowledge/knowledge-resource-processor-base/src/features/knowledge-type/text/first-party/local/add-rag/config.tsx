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
import { TextUpload, TextProgress } from './steps';
import { TextLocalAddRagStep } from './constants';

/**
 * Rag-mode wizard for text local upload. 2 steps: `[UPLOAD, PROGRESS]`.
 *
 * Structural twin of {@link TextLocalAddUpdateConfig} (legacy) — same store
 * factory, same upload step, same `useUploadMount`, same footer wrapper — but
 * with the SEGMENT_CLEANER and SEGMENT_PREVIEW steps removed because rag
 * locks chunking at KB-creation time and has no document-review workflow.
 * The progress step is a thin adapter over the shared `<UploadProgressPoll />`
 * which polls `GetDocumentProgress` and navigates to KB detail on completion.
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
      content: props => <TextProgress useStore={props.useStore} />,
      title: I18n.t('datasets_createFileModel_step4'),
      step: TextLocalAddRagStep.PROGRESS,
    },
  ],
  createStore: createTextLocalAddUpdateStore,
  useUploadMount: () => useTextDisplaySegmentStepCheck(),
};
