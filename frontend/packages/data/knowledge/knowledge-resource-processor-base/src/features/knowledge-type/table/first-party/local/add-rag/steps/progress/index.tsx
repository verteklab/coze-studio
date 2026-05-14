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

import { DataNamespace, dataReporter } from '@coze-data/reporter';
import {
  useDataNavigate,
  useKnowledgeParams,
} from '@coze-data/knowledge-stores';
import { type ContentProps } from '@coze-data/knowledge-resource-processor-core';
import { getKnowledgeIDEQuery } from '@coze-data/knowledge-common-services';
import { REPORT_EVENTS } from '@coze-arch/report-events';
import { KnowledgeApi } from '@coze-arch/bot-api';

import {
  getConfigurationMeta,
  getCreateDocumentParams,
} from '@/features/knowledge-type/table/utils';
import type {
  UploadTableAction,
  UploadTableState,
} from '@/features/knowledge-type/table/interface';
import { UploadProgressPoll } from '@/components';

import { type TableLocalAddRagStep } from '../../constants';

import styles from './index.module.less';

type TableLocalAddRagStore = UploadTableState<TableLocalAddRagStep> &
  UploadTableAction<TableLocalAddRagStep>;

/**
 * Rag-mode progress step for table local upload.
 *
 * Differences from the legacy `<TableProcessing />`:
 *   - Calls `KnowledgeApi.CreateDocument` directly (rather than via
 *     `useCreateDocument`) so we can capture `document_infos` and hand
 *     them to `<UploadProgressPoll />`, which owns its own polling loop.
 *     The legacy hook bundles polling + progressList state mutation, which
 *     the shared poll component already encapsulates more cleanly for rag.
 *   - On poll completion, navigates to the knowledge detail page via
 *     `useDataNavigate().toResource('knowledge', datasetID, query)` — same
 *     navigation primitive the legacy processing footer's "Done" button
 *     uses, so behaviour stays consistent across modes.
 *   - The legacy CONFIGURATION / PREVIEW / PROCESSING steps are dropped:
 *     they depend on `GetTableSchema`, `ValidateTableSchema`, and
 *     `CreateDocumentReview` — all pending stubs in ragimpl (see Task 0 of
 *     the rag-flow alignment plan).
 *
 * CreateDocument payload provenance:
 *   - `unitList`      → snapshotted from the store (populated by `<TableUpload />`).
 *   - `tableSettings` → snapshotted from the store; defaults to
 *                       `DEFAULT_TABLE_SETTINGS_FROM_ONE` because the legacy
 *                       CONFIGURATION step (which lets the user pick sheet /
 *                       header row) is dropped. Rag accepts the default
 *                       sheet/header-row mapping for single-sheet uploads.
 *   - `metaData`      → derived from `tableData` via `getConfigurationMeta`.
 *                       For rag this is typically `[]` because the legacy
 *                       upload step's fire-and-forget `GetTableSchema` request
 *                       fails against the rag backend (the endpoint is a
 *                       pending stub), so `tableData` stays at its initial
 *                       `{}`. Server-side rag derives column definitions from
 *                       the uploaded file when `table_meta` is empty.
 *   - `isAppend`      → `false` — rag-mode add-flow is always a fresh create.
 *   - `sourceType`    → omitted → defaults to `DocumentSource.Document`,
 *                       matching legacy table-local-add behaviour.
 *
 * The store shape is the same `UploadTableState & UploadTableAction` used by
 * the legacy wizard (the upload step is reused as-is, so we must read from
 * the same store). Doc IDs are NOT pulled from the store; they come from the
 * `CreateDocument` response captured locally below — the legacy wizard never
 * persists doc IDs to the store as a top-level array either, it persists them
 * indirectly via `progressList[].documentId`.
 */
export const TableProgress: FC<ContentProps<TableLocalAddRagStore>> = props => {
  const { useStore } = props;

  const resourceNavigate = useDataNavigate();
  const params = useKnowledgeParams();

  const [docIds, setDocIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  // Guards against StrictMode-driven double-invocation of the create effect.
  // We intentionally do NOT include this in any deps array — it's a "fire
  // exactly once per mount" latch.
  const createdRef = useRef(false);

  // The store API (`useStore`) is used only to read a one-shot snapshot
  // via `getState()` inside the effect below. We deliberately do NOT
  // subscribe via `useShallow` — the effect runs exactly once on mount
  // (gated by createdRef), so subscribing to store mutations would only
  // trigger wasted re-renders without changing behaviour.

  useEffect(() => {
    if (createdRef.current) {
      return;
    }
    createdRef.current = true;

    const run = async () => {
      try {
        const { unitList, tableData, tableSettings } = useStore.getState();
        const metaData = getConfigurationMeta(tableData, tableSettings);
        const reqParams = getCreateDocumentParams({
          isAppend: false,
          unitList,
          metaData,
          tableSettings,
        });
        const res = await KnowledgeApi.CreateDocument({
          dataset_id: params.datasetID,
          ...reqParams,
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
        // TODO: i18n — wait for spec to register a key.
        setError(e instanceof Error ? e.message : '创建文档失败');
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

  // CreateDocument rejected — surface the error and DO NOT mount the poll
  // component. Mounting it with an empty docIds is also defended against
  // upstream in <UploadProgressPoll />, but bailing here keeps the user-
  // facing semantics explicit: failed create == no progress UI, no nav.
  if (error) {
    return (
      <div className={styles.error}>
        {/* TODO: i18n — wait for spec to register a key. */}
        <p className={styles['error-message']}>{error}</p>
        {/* TODO: i18n — wait for spec to register a key. */}
        <p className={styles['error-hint']}>请刷新页面或联系管理员。</p>
      </div>
    );
  }

  // CreateDocument is still in flight — render a loading state rather than
  // mounting <UploadProgressPoll /> with empty docIds. The poll component
  // itself also guards against this, but keeping the loading state local
  // here avoids the 0/0 summary flash before the response lands.
  if (docIds.length === 0) {
    return (
      <div className={styles.loading}>
        {/* TODO: i18n — wait for spec to register a key. */}
        正在创建文档…
      </div>
    );
  }

  return <UploadProgressPoll docIds={docIds} onComplete={handleComplete} />;
};
