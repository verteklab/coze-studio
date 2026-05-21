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
  mergeSchemaDefaults,
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
 * UX, but scoped to the `image_document` schema only.
 *
 * Image KB uploads materially behave as `image_document` after the
 * `FORCED_PARAMS_BY_SCHEMA` layer applies (`scanned_document` produces
 * byte-identical wire payloads post-force), so the modality selector that
 * the text wizard exposes for PDF text-vs-scanned is intentionally absent
 * here. See 2026-05-21-image-kb-collapse-modality-choice-design.md.
 *
 * On Next, serialises the form values into the wire blob and advances to
 * PROGRESS. Catalog load failure falls back to "advance with empty
 * options" so a transient outage on the schemas endpoint does not block
 * uploads. If `image_document` is absent from the catalog (a rag-side
 * breaking change), the config area renders empty rather than silently
 * falling back to scanned — the user surfaces the broken state.
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
    // Image KB uploads only ever materially behave as image_document after
    // the FORCED_PARAMS_BY_SCHEMA layer in use-schemas.ts — scanned_document
    // is byte-equivalent on the wire post-force. Narrow to image_document
    // here (vs. inside matchSchemasForFile, which stays correct for the
    // text-KB call site where PDF text/scanned do meaningfully differ).
    return matchSchemasForFile(schemas, fileType).filter(
      s => s.schema_id === 'image_document',
    );
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
    // Seed with schema defaults so unchanged toggles travel on the wire
    // (otherwise formValue only contains user-touched keys and the wire stops
    // describing what the user actually saw — see mergeSchemaDefaults JSDoc).
    const payload: DocumentOptionsValue = activeSchema
      ? mergeSchemaDefaults(activeSchema, formValue)
      : { ...formValue };
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
