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

import { useMemo } from 'react';

import {
  useDataNavigate,
  useKnowledgeParams,
} from '@coze-data/knowledge-stores';
import { type FooterBtnProps } from '@coze-data/knowledge-resource-processor-core/types';
import { I18n } from '@coze-arch/i18n';

export const useUploadCancelButton = (): FooterBtnProps => {
  const { datasetID } = useKnowledgeParams();
  const resourceNavigate = useDataNavigate();

  return useMemo(
    () => ({
      type: 'secondary',
      theme: 'solid',
      text: I18n.t('Cancel'),
      onClick: () => {
        resourceNavigate.toResource?.('knowledge', datasetID);
      },
    }),
    [datasetID, resourceNavigate],
  );
};
