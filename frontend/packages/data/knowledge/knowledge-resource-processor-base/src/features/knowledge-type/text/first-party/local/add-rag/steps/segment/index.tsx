/*
 * Copyright 2026 coze-dev Authors
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

import { type FC, useMemo, useState } from 'react';

import { useShallow } from 'zustand/react/shallow';
import { type ContentProps } from '@coze-data/knowledge-resource-processor-core';
import { I18n } from '@coze-arch/i18n';
import { Select, Typography } from '@coze-arch/coze-design';

import {
  DynamicParsingPanel,
  matchSchemasForFile,
  useRagDocumentParameterSchemas,
  type DocumentOptionsValue,
  type DocumentParameterSchema,
} from '@/features/dynamic-parsing-panel';

import { TextLocalAddRagStep } from '../../constants';
import type { UploadTextLocalAddUpdateStore } from '../../../add/store';

/**
 * Rag-mode SEGMENT_CLEANER step (Phase 3b). Replaces the legacy
 * `<TextSegment />` for the rag wizard with a schema-driven dynamic form:
 *
 *   - Looks at the first uploaded file's extension and picks the matching
 *     rag parameter schemas from the catalog (Phase 2 endpoint).
 *   - When more than one schema matches (PDF: text vs. scanned; image:
 *     image vs. scanned), renders a selector so the user owns the modality
 *     decision instead of relying on backend auto-routing.
 *   - When the user picks the "scanned" schema, surfaces a one-line hint
 *     so the modality change is transparent (per the Phase 3b UX decision
 *     to make the routing visible).
 *   - Renders <DynamicParsingPanel /> for the chosen schema and writes
 *     user choices into local state.
 *   - On Next, serialises the form values to JSON, inlines a reserved
 *     `_source_modality` key only when a non-default schema was chosen,
 *     and stores the result in `store.documentOptions` for the progress
 *     step to forward to `KnowledgeApi.CreateDocument`.
 *
 * Loading / error states fall back to "skip the form" — the wizard still
 * advances to PROGRESS with an empty documentOptions so the upload uses
 * rag defaults rather than blocking the user behind a transient outage.
 */
export const TextRagSegment: FC<
  ContentProps<UploadTextLocalAddUpdateStore>
> = props => {
  const { useStore, footer } = props;
  const { unitList, setCurrentStep, setDocumentOptions } = useStore(
    useShallow(state => ({
      unitList: state.unitList,
      setCurrentStep: state.setCurrentStep,
      setDocumentOptions: state.setDocumentOptions,
    })),
  );

  const { schemas, loading, error } = useRagDocumentParameterSchemas();

  // File type drives schema selection. Multi-file uploads currently share
  // strategy (matches legacy behaviour) — we use the first file as the
  // representative. If the batch contains heterogeneous types the user can
  // still set per-schema knobs but they'll apply uniformly; that's a known
  // limitation rag's per-document API can't address from a single form.
  const fileType = useMemo(
    () => unitList[0]?.type?.toLowerCase() ?? '',
    [unitList],
  );

  const candidateSchemas = useMemo<DocumentParameterSchema[]>(() => {
    if (!schemas || !fileType) {
      return [];
    }
    return matchSchemasForFile(schemas, fileType);
  }, [schemas, fileType]);

  // Default to the FIRST candidate. For PDFs that's pdf_text_document,
  // which matches rag's own default routing — picking "scanned" is an
  // explicit user opt-in.
  const [selectedSchemaId, setSelectedSchemaId] = useState<string>('');
  const activeSchema =
    candidateSchemas.find(s => s.schema_id === selectedSchemaId) ??
    candidateSchemas[0] ??
    null;

  const [formValue, setFormValue] = useState<DocumentOptionsValue>({});

  const handleNext = (): void => {
    // Compose the wire shape. `_source_modality` only travels when the user
    // explicitly chose a non-first schema — letting backend auto-routing
    // own the common case keeps existing PDF/image uploads identical.
    const payload: DocumentOptionsValue = { ...formValue };
    if (
      activeSchema &&
      candidateSchemas.length > 1 &&
      activeSchema.schema_id !== candidateSchemas[0].schema_id
    ) {
      payload._source_modality = activeSchema.source_modalities[0];
    }
    setDocumentOptions(
      Object.keys(payload).length > 0 ? JSON.stringify(payload) : '',
    );
    setCurrentStep(TextLocalAddRagStep.PROGRESS);
  };

  const handleBack = (): void => {
    setCurrentStep(TextLocalAddRagStep.UPLOAD);
  };

  const isScannedSchema = (activeSchema?.schema_id ?? '').includes('scanned');

  return (
    <div style={{ padding: 16 }}>
      {loading ? (
        <Typography.Text type="tertiary">
          {/* TODO i18n */}加载解析参数中…
        </Typography.Text>
      ) : null}
      {error ? (
        <Typography.Text type="warning">
          {/* TODO i18n */}解析参数加载失败，将使用默认配置上传：{error.message}
        </Typography.Text>
      ) : null}
      {candidateSchemas.length > 1 ? (
        <div style={{ marginBottom: 12 }}>
          <Typography.Title heading={6} style={{ marginBottom: 4 }}>
            {/* TODO i18n */}解析模式
          </Typography.Title>
          <Select
            style={{ minWidth: 240 }}
            value={activeSchema?.schema_id ?? candidateSchemas[0].schema_id}
            optionList={candidateSchemas.map(s => ({
              label: s.schema_id,
              value: s.schema_id,
            }))}
            onChange={(v: unknown) => {
              setSelectedSchemaId(String(v));
              // Reset form values when switching schema: the new schema may
              // not declare the same parameter names, and carrying stale
              // values through document_options would 422 rag's pydantic
              // validation under extra=forbid.
              setFormValue({});
            }}
          />
        </div>
      ) : null}
      {isScannedSchema ? (
        <Typography.Text type="tertiary" size="small">
          {/* TODO i18n */}将使用扫描件解析（含 OCR）
        </Typography.Text>
      ) : null}
      {activeSchema ? (
        <DynamicParsingPanel
          schema={activeSchema}
          value={formValue}
          onChange={setFormValue}
        />
      ) : null}
      {footer?.([
        {
          type: 'primary',
          theme: 'light',
          onClick: handleBack,
          text: I18n.t('datasets_createFileModel_previousBtn'),
        },
        {
          type: 'hgltplus',
          theme: 'solid',
          onClick: handleNext,
          text: I18n.t('datasets_createFileModel_NextBtn'),
        },
      ])}
    </div>
  );
};
