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

import { get } from 'lodash-es';
import {
  UnitType,
  OptType,
  type UploadBaseState,
  type UploadBaseAction,
  type UploadConfig,
} from '@coze-data/knowledge-resource-processor-core';
import { TextResegmentConfig } from '@coze-data/knowledge-resource-processor-base/features/resegment/text';
import { TableResegmentConfig } from '@coze-data/knowledge-resource-processor-base/features/resegment/table';
import { TextLocalResegmentConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/resegment';
import { TextLocalAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/add-rag';
import { TextLocalAddUpdateConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/add';
import { TextCustomAddUpdateConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/custom/add';
import { TableLocalIncrementalConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/incremental';
import { TableLocalAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/add-rag';
import { TableLocalAddConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/add';
import { TableCustomIncrementalConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/custom/incremental';
import { TableCustomAddConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/custom/add';
import { ImageFileAddRagConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/image/file/add-rag';
import { ImageFileAddConfig } from '@coze-data/knowledge-resource-processor-base/features/knowledge-type/image/file';

/**
 * Which backend serves a given KB. See
 * docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md §4.3.
 *
 * 'rag'        — managed by the standalone rag service; route to rag-mode wizard
 * 'legacy'     — in-tree legacy module; route to existing wizard
 * undefined    — KB not yet fetched / older server response; safe-fallback to legacy
 */
type Backend = 'rag' | 'legacy' | string | undefined;

const getConfigV2 = (backend?: Backend) => {
  const isRag = backend === 'rag';
  return {
    [UnitType.TEXT]: {
      [OptType.RESEGMENT]: TextResegmentConfig,
    },
    [UnitType.TABLE]: {
      [OptType.RESEGMENT]: TableResegmentConfig,
    },
    [UnitType.TEXT_DOC]: {
      [OptType.ADD]: isRag ? TextLocalAddRagConfig : TextLocalAddUpdateConfig,
      [OptType.RESEGMENT]: TextLocalResegmentConfig,
    },
    [UnitType.TEXT_CUSTOM]: {
      [OptType.ADD]: TextCustomAddUpdateConfig,
    },
    [UnitType.TABLE_DOC]: {
      [OptType.ADD]: isRag ? TableLocalAddRagConfig : TableLocalAddConfig,
      [OptType.INCREMENTAL]: TableLocalIncrementalConfig,
    },
    [UnitType.TABLE_CUSTOM]: {
      [OptType.ADD]: TableCustomAddConfig,
      [OptType.INCREMENTAL]: TableCustomIncrementalConfig,
    },
    [UnitType.IMAGE_FILE]: {
      [OptType.ADD]: isRag ? ImageFileAddRagConfig : ImageFileAddConfig,
    },
  };
};

/**
 * Knowledge information architecture reconstruction changes are not small, so split into two configs. Change points:
 * 1. Remove the update operation from all links
 * 2. The resegment of the text shares one, because the interaction is already the same
 * 3. There are some detailed UI changes
 * 4. All interfaces have been migrated to KnowledgeAPI.
 *
 * `backend` is the kb.backend value (see Backend type above). It gates which
 * ADD wizard a KB type renders. Existing callers that do not yet pass it
 * default to 'legacy' semantics — safe migration.
 */
export const getUploadConfig = (
  type: UnitType,
  opt: OptType,
  backend?: Backend,
): UploadConfig<
  number,
  UploadBaseState<number> & UploadBaseAction<number>
> | null => {
  const optKey = opt || OptType.ADD; // When opt === '' , the default is ADD.
  const config = getConfigV2(backend);

  return get(config, `${type}.${optKey}`, null);
};
