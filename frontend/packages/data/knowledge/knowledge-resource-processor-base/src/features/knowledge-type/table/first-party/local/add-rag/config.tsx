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
  type ContentProps,
} from '@coze-data/knowledge-resource-processor-core';
import { I18n } from '@coze-arch/i18n';

import { UploadFooter } from '@/components';

import { createTableLocalAddStore } from '../add/store';
import { useTableCheck } from '../../hooks';
import {
  type UploadTableAction,
  type UploadTableState,
} from '../../../interface';
import { TableUpload, TableProgress } from './steps';
import { TableLocalAddRagStep } from './constants';

type TableLocalAddRagContentProps = ContentProps<
  UploadTableAction<TableLocalAddRagStep> &
    UploadTableState<TableLocalAddRagStep>
>;

/**
 * Rag-mode wizard for table local upload. 2 steps: `[UPLOAD, PROGRESS]`.
 *
 * Structural twin of {@link TableLocalAddConfig} (legacy) — same store
 * factory, same upload step, same `useUploadMount`, same footer wrapper — but
 * with the CONFIGURATION, PREVIEW, and PROCESSING steps removed because rag
 * lacks the supporting endpoints (`GetTableSchema`, `ValidateTableSchema`,
 * `CreateDocumentReview` are all pending stubs in ragimpl). The progress
 * step replaces all three: it calls CreateDocument directly with the unit
 * list snapshotted from the store and delegates polling + navigation to the
 * shared `<UploadProgressPoll />`.
 *
 * Selection between this config and the legacy one is handled in
 * `scenes/base/config.ts` (see Task 10) by gating on `kb.backend === 'rag'`.
 */
export const TableLocalAddRagConfig: UploadConfig<
  TableLocalAddRagStep,
  UploadTableAction<TableLocalAddRagStep> &
    UploadTableState<TableLocalAddRagStep>
> = {
  steps: [
    {
      content: (props: TableLocalAddRagContentProps) => (
        <TableUpload
          useStore={props.useStore}
          footer={(controls: FooterControlsProps) => (
            <UploadFooter controls={controls} />
          )}
          checkStatus={undefined}
        />
      ),
      title: I18n.t('datasets_createFileModel_step2'),
      step: TableLocalAddRagStep.UPLOAD,
    },
    {
      content: (props: TableLocalAddRagContentProps) => (
        <TableProgress useStore={props.useStore} />
      ),
      title: I18n.t('datasets_createFileModel_step4'),
      step: TableLocalAddRagStep.PROGRESS,
    },
  ],
  createStore: createTableLocalAddStore,
  className: 'table-local-wrapper',
  useUploadMount: store => useTableCheck(store),
};
