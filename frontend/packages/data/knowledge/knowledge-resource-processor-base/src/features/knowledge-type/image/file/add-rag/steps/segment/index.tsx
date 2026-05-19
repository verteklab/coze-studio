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
import { Button, Select, Toast, Typography } from '@coze-arch/coze-design';

import {
  DynamicParsingPanel,
  findMissingRequired,
  matchSchemasForFile,
  schemaLabel,
  useRagDocumentParameterSchemas,
  type DocumentOptionsValue,
  type DocumentParameterSchema,
} from '@/features/dynamic-parsing-panel';

import { ImageFileAddRagStep } from '../../constants';
import { type ImageFileAddStore } from '../../../store';

/**
 * Rag-mode SEGMENT_CLEANER step for the image upload wizard (Phase 3b).
 * Twin of the text wizard's <TextRagSegment /> — same schema-driven form
 * UX, but scoped to image schemas (`image_document` + `scanned_document`)
 * and writing into the image store.
 *
 * Behaviour:
 *   - For typical image uploads (jpg/png/webp/...), two schemas match:
 *     `image_document` (image_source) and `scanned_document`
 *     (scanned_document_source). The selector lets the user pick; default
 *     is `image_document` (matches rag's auto-routing). Picking
 *     `scanned_document` inlines a reserved `_source_modality` key so the
 *     backend (commit 6cdf670f) honors the explicit choice.
 *   - When `scanned_document` is chosen, surfaces a one-line hint that
 *     the OCR-heavy parser will be used.
 *   - On Next, serialises the form values into the wire blob (with
 *     `_source_modality` only when the user picked the non-default schema)
 *     and advances to PROGRESS.
 *   - Catalog load failure falls back to "advance with empty options" so
 *     a transient outage on the schemas endpoint does not block uploads.
 *
 * Differs from <TextRagSegment /> only in the store type and the step
 * enum it transitions to; the renderer + selector logic is shared via
 * <DynamicParsingPanel /> + matchSchemasForFile.
 */
export const ImageRagSegment: FC<ContentProps<ImageFileAddStore>> = props => {
  const { useStore, footer } = props;
  const { unitList, setCurrentStep, setDocumentOptions } = useStore(
    useShallow(state => ({
      unitList: state.unitList,
      setCurrentStep: state.setCurrentStep,
      setDocumentOptions: state.setDocumentOptions,
    })),
  );

  const { schemas, loading, error, retry } = useRagDocumentParameterSchemas();

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

  const [selectedSchemaId, setSelectedSchemaId] = useState<string>('');
  const activeSchema =
    candidateSchemas.find(s => s.schema_id === selectedSchemaId) ??
    candidateSchemas[0] ??
    null;

  const [formValue, setFormValue] = useState<DocumentOptionsValue>({});

  const handleNext = (): void => {
    if (activeSchema) {
      const missing = findMissingRequired(activeSchema, formValue);
      if (missing.length > 0) {
        Toast.error(
          I18n.t('datasets_createFileModel_rag_required_missing', {
            fields: missing.map(p => p.ui_label || p.name).join(', '),
          }),
        );
        return;
      }
    }
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
    setCurrentStep(ImageFileAddRagStep.PROGRESS);
  };

  const handleBack = (): void => {
    setCurrentStep(ImageFileAddRagStep.UPLOAD);
  };

  const isScannedSchema = (activeSchema?.schema_id ?? '').includes('scanned');

  return (
    <div style={{ padding: 16 }}>
      {loading ? (
        <Typography.Text type="tertiary">
          {I18n.t('datasets_createFileModel_rag_loading_schemas')}
        </Typography.Text>
      ) : null}
      {error ? (
        <div style={{ marginBottom: 12 }}>
          <Typography.Text type="warning">
            {I18n.t('datasets_createFileModel_rag_schemas_failed', {
              message: error.message,
            })}
          </Typography.Text>{' '}
          <Button size="small" onClick={retry}>
            {I18n.t('datasets_createFileModel_rag_schemas_retry')}
          </Button>
        </div>
      ) : null}
      {candidateSchemas.length > 1 ? (
        <div style={{ marginBottom: 12 }}>
          <Typography.Title heading={6} style={{ marginBottom: 4 }}>
            {I18n.t('datasets_createFileModel_rag_mode_label')}
          </Typography.Title>
          <Select
            style={{ minWidth: 240 }}
            value={activeSchema?.schema_id ?? candidateSchemas[0].schema_id}
            optionList={candidateSchemas.map(s => ({
              label: schemaLabel(s.schema_id),
              value: s.schema_id,
            }))}
            onChange={(v: unknown) => {
              setSelectedSchemaId(String(v));
              // Reset form values when switching schema: rag's
              // image_document and scanned_document share many parameter
              // names but their valid value ranges differ; carrying stale
              // values across the switch would 422 pydantic on submit.
              setFormValue({});
            }}
          />
        </div>
      ) : null}
      {isScannedSchema ? (
        <Typography.Text type="tertiary" size="small">
          {I18n.t('datasets_createFileModel_rag_scanned_hint')}
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
