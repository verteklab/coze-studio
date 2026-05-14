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

import { type FC, useEffect, useRef, useState } from 'react';

import { useShallow } from 'zustand/react/shallow';
import { DataNamespace, dataReporter } from '@coze-data/reporter';
import {
  useDataNavigate,
  useKnowledgeParams,
} from '@coze-data/knowledge-stores';
import { type ContentProps } from '@coze-data/knowledge-resource-processor-core';
import { getKnowledgeIDEQuery } from '@coze-data/knowledge-common-services';
import { REPORT_EVENTS } from '@coze-arch/report-events';
import { KnowledgeApi } from '@coze-arch/bot-api';

import { UploadProgressPoll } from '@/components';

import type { UploadTextLocalAddUpdateStore } from '../../../add/store';
import { getCreateDocumentParams } from '../../../add/steps/processing/utils';

/**
 * Rag-mode progress step for text local upload.
 *
 * Differences from the legacy `<TextProcessing />`:
 *   - Calls `KnowledgeApi.CreateDocument` directly (rather than via
 *     `useCreateDocument`) so we can capture `document_infos` and hand
 *     them to `<UploadProgressPoll />`, which owns its own polling loop.
 *     The legacy hook bundles polling + progressList state mutation, which
 *     the shared poll component already encapsulates more cleanly for rag.
 *   - On poll completion, navigates to the knowledge detail page via
 *     `useDataNavigate().toResource('knowledge', datasetID, query)` —
 *     same navigation primitive the legacy processing footer's "Done"
 *     button uses, so behaviour stays consistent across modes.
 *
 * The store shape is the same `UploadTextLocalAddUpdateStore` used by the
 * legacy wizard (the upload step is reused as-is, so we must read from the
 * same store). Doc IDs are NOT pulled from the store; they come from the
 * `CreateDocument` response captured locally below — the legacy wizard
 * never persists doc IDs to the store as a top-level array either, it
 * persists them indirectly via `progressList[].documentId`.
 */
export const TextProgress: FC<
  ContentProps<UploadTextLocalAddUpdateStore>
> = props => {
  const { useStore } = props;

  const resourceNavigate = useDataNavigate();
  const params = useKnowledgeParams();

  const [docIds, setDocIds] = useState<string[]>([]);
  // Guards against StrictMode-driven double-invocation of the create effect.
  // We intentionally do NOT include this in any deps array — it's a "fire
  // exactly once per mount" latch.
  const createdRef = useRef(false);

  const {
    unitList,
    segmentMode,
    segmentRule,
    enableStorageStrategy,
    storageLocation,
    openSearchConfig,
    docReviewList,
  } = useStore(
    useShallow(state => ({
      unitList: state.unitList,
      segmentMode: state.segmentMode,
      segmentRule: state.segmentRule,
      enableStorageStrategy: state.enableStorageStrategy,
      storageLocation: state.storageLocation,
      openSearchConfig: state.openSearchConfig,
      docReviewList: state.docReviewList,
    })),
  );

  useEffect(() => {
    if (createdRef.current) {
      return;
    }
    createdRef.current = true;

    const run = async () => {
      try {
        const { parsingStrategy, filterStrategy, levelChunkStrategy } =
          useStore.getState();
        const reqParams = getCreateDocumentParams({
          unitList,
          segmentMode,
          segmentRule,
          pdfFilterValueList: filterStrategy,
          levelChunkStrategy,
          docReviewList,
          enableStorageStrategy,
          storageLocation,
          openSearchConfig,
        });
        const res = await KnowledgeApi.CreateDocument({
          dataset_id: params.datasetID,
          ...reqParams,
          parsing_strategy: parsingStrategy,
        });
        const ids =
          res.document_infos
            ?.map(info => info.document_id)
            .filter((id): id is string => typeof id === 'string') ?? [];
        setDocIds(ids);
      } catch (e) {
        dataReporter.errorEvent(DataNamespace.KNOWLEDGE, {
          eventName: REPORT_EVENTS.KnowledgeCreateDocument,
          error: e as Error,
        });
      }
    };
    void run();
    // Intentionally empty — fire-once on mount. Inputs are snapshotted
    // from the store via `getState()` to avoid stale-closure traps and to
    // prevent re-firing the network call on any store mutation.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- see comment
  }, []);

  const handleComplete = () => {
    const query = getKnowledgeIDEQuery() as Record<string, string>;
    resourceNavigate.toResource?.('knowledge', params.datasetID, query);
  };

  // Before CreateDocument resolves, docIds is empty — the poll component
  // gracefully renders an empty list with a 0/0 summary in that window.
  // Once the response lands, ids populate and polling kicks off.
  return <UploadProgressPoll docIds={docIds} onComplete={handleComplete} />;
};
