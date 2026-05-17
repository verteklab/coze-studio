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

import { type FC, useEffect, useMemo, useState } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Select } from '@coze-arch/coze-design';
import { type DocumentInfo } from '@coze-arch/bot-api/knowledge';
import { KnowledgeApi } from '@coze-arch/bot-api';

interface DocumentOption {
  label: string;
  /**
   * doc_id as STRING. Coze's int64 ids exceed JS Number's safe range (2^53),
   * so storing as Number truncates the trailing 3-4 digits and breaks the
   * coze→rag mapping lookup. Stay in string-land all the way through to the
   * datasetParam payload; the backend `cast.ToInt64E` handles either form.
   */
  value: string;
  /** dataset_id the document belongs to -- shown as secondary text. */
  datasetId: string;
}

interface DocumentIDsSelectProps {
  /**
   * dataset_ids (string) of the currently-selected KBs. Documents from all
   * listed KBs are merged into one option list.
   */
  datasetIDs: string[];
  /** Currently-selected document IDs (string). Empty / undefined means "all". */
  value?: string[];
  onChange: (next: string[] | undefined) => void;
  readonly?: boolean;
  disabled?: boolean;
}

/**
 * DocumentIDsSelect renders a multi-select of all documents in the selected
 * KBs. Empty selection means "no filter -- all documents".
 *
 * Backend wiring (R2-I):
 *   selected ids -> datasetParam[name=documentIDs].value.content
 *     -> RetrieveConfig.DocumentIDs
 *     -> RetrieveRequest.DocumentIDs (top-level, NOT on RetrievalStrategy)
 *     -> rag /retrieval document_ids
 */
export const DocumentIDsSelect: FC<DocumentIDsSelectProps> = ({
  datasetIDs,
  value,
  onChange,
  readonly,
  disabled,
}) => {
  const [documents, setDocuments] = useState<DocumentOption[]>([]);
  const [loading, setLoading] = useState(false);

  // Stable JSON key so deeply equal arrays don't refetch.
  const datasetIDsKey = useMemo(
    () => [...datasetIDs].sort().join(','),
    [datasetIDs],
  );

  useEffect(() => {
    let cancelled = false;
    if (!datasetIDs.length) {
      setDocuments([]);
      // Wipe selection when the KB list is empty -- a stale doc id from a
      // previously-selected KB would just silently filter out every result.
      if (value?.length) {
        onChange(undefined);
      }
      return;
    }

    setLoading(true);
    Promise.all(
      datasetIDs.map(datasetID =>
        KnowledgeApi.ListDocument({ dataset_id: datasetID, page: 1, size: 200 })
          .then(res => ({ datasetID, infos: res.document_infos ?? [] }))
          .catch(() => ({ datasetID, infos: [] as DocumentInfo[] })),
      ),
    )
      .then(results => {
        if (cancelled) {
          return;
        }
        const merged: DocumentOption[] = [];
        for (const { datasetID, infos } of results) {
          for (const info of infos) {
            const idStr = info.document_id;
            if (!idStr) {
              continue;
            }
            // Keep as string -- Number(idStr) would truncate int64 ids above 2^53.
            merged.push({
              label: info.name || idStr,
              value: idStr,
              datasetId: datasetID,
            });
          }
        }
        setDocuments(merged);
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- datasetIDsKey is the stable key for datasetIDs; including datasetIDs/value/onChange triggers refetch loops.
  }, [datasetIDsKey]);

  const optionList = useMemo(
    () =>
      documents.map(d => ({
        label: d.label,
        value: d.value,
      })),
    [documents],
  );

  return (
    <Select
      multiple
      filter
      size="small"
      style={{ width: '100%' }}
      value={value ?? []}
      optionList={optionList}
      loading={loading}
      disabled={readonly || disabled}
      placeholder={I18n.t(
        'workflow_detail_knowledge_document_filter_placeholder',
        {},
        'Filter by document (leave empty for all)',
      )}
      emptyContent={I18n.t(
        'workflow_detail_node_nodata',
        {},
        'No documents available',
      )}
      onChange={next => {
        const arr = (next as string[] | undefined) ?? [];
        onChange(arr.length === 0 ? undefined : arr);
      }}
    />
  );
};
