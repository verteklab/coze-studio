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

import { useKnowledgeParams } from '@coze-data/knowledge-stores';
import {
  OptType,
  UnitType,
} from '@coze-data/knowledge-resource-processor-core';
import {
  KnowledgeResourceProcessorLayout,
  type KnowledgeResourceProcessorLayoutProps,
} from '@coze-data/knowledge-resource-processor-base/layout/base';

import { getUploadConfig } from './config';

export type KnowledgeResourceProcessorProps =
  KnowledgeResourceProcessorLayoutProps & {
    /**
     * kb.backend — gates rag vs legacy upload wizard. Optional; absent → legacy
     * (safe fallback). Callers that have fetched the KB should pass it.
     * See docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md §4.3.
     */
    backend?: string;
  };

export const KnowledgeResourceProcessor = ({
  backend,
  ...props
}: KnowledgeResourceProcessorProps) => {
  const { type, opt } = useKnowledgeParams();
  const uploadConfig = getUploadConfig(
    type ?? UnitType.TEXT,
    opt ?? OptType.ADD,
    backend,
  );
  if (!uploadConfig) {
    return <></>;
  }
  return (
    <KnowledgeResourceProcessorLayout {...props} uploadConfig={uploadConfig} />
  );
};
