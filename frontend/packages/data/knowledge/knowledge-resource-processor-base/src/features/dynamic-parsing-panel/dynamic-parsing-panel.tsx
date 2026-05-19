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

import { type FC, useMemo } from 'react';

import { I18n } from '@coze-arch/i18n';
import {
  InputNumber,
  Input,
  Select,
  Switch,
  Typography,
} from '@coze-arch/coze-design';

import { CollapsePanel } from '@/components';

import {
  type DocumentParameter,
  type DocumentParameterSchema,
  type DocumentOptionsValue,
} from './types';

interface DynamicParsingPanelProps {
  /**
   * The schema whose parameters drive the form. The caller picks this from
   * the catalog (see useRagDocumentParameterSchemas + matchSchemasForFile)
   * by file_type and the user's modality choice.
   */
  schema: DocumentParameterSchema;
  /** Current form values; flat map keyed by `parameter.name`. */
  value: DocumentOptionsValue;
  /** Fired on every individual field change with the full updated map. */
  onChange: (next: DocumentOptionsValue) => void;
}

/**
 * Renders rag's per-schema parameter form. Field controls are picked from
 * `parameter.ui_component`; values flow back via onChange as a flat map keyed
 * by `parameter.name`. The map is later serialised to JSON and sent as
 * `CreateDocumentRequest.document_options`.
 *
 * Layout: non-advanced parameters in the open section, advanced parameters
 * collapsed behind a single `<CollapsePanel>`. Both sections are grouped
 * sub-headers by `parameter.group`, preserving the order rag returned (rag's
 * schema definitions already group related knobs and order is stable).
 */
export const DynamicParsingPanel: FC<DynamicParsingPanelProps> = ({
  schema,
  value,
  onChange,
}) => {
  const { visible, advanced } = useMemo(() => {
    const visibleList: DocumentParameter[] = [];
    const advancedList: DocumentParameter[] = [];
    for (const p of schema.parameters) {
      (p.advanced ? advancedList : visibleList).push(p);
    }
    return { visible: visibleList, advanced: advancedList };
  }, [schema.parameters]);

  const handleFieldChange = (paramName: string, fieldValue: unknown): void => {
    onChange({ ...value, [paramName]: fieldValue });
  };

  return (
    <div className="dynamic-parsing-panel">
      <GroupedFields
        params={visible}
        value={value}
        onChange={handleFieldChange}
      />
      {advanced.length > 0 ? (
        <CollapsePanel
          header={I18n.t('datasets_createFileModel_rag_advanced_params')}
        >
          <GroupedFields
            params={advanced}
            value={value}
            onChange={handleFieldChange}
          />
        </CollapsePanel>
      ) : null}
    </div>
  );
};

/**
 * Renders a flat parameter list with `parameter.group` headers inserted at
 * each group boundary. Order follows the input array — caller is responsible
 * for keeping rag's stable schema order (we don't sort to avoid surprising
 * the user with reshuffles between rag versions).
 */
const GroupedFields: FC<{
  params: DocumentParameter[];
  value: DocumentOptionsValue;
  onChange: (name: string, fieldValue: unknown) => void;
}> = ({ params, value, onChange }) => {
  let lastGroup = '';
  return (
    <>
      {params.map(p => {
        const showHeader = p.group !== lastGroup;
        lastGroup = p.group;
        return (
          <div key={p.name} style={{ marginBottom: 12 }}>
            {showHeader ? (
              <Typography.Title
                heading={6}
                style={{ marginTop: 8, marginBottom: 4 }}
              >
                {p.group}
              </Typography.Title>
            ) : null}
            <FieldControl param={p} value={value[p.name]} onChange={onChange} />
            {p.description ? (
              <Typography.Text type="tertiary" size="small">
                {p.description}
              </Typography.Text>
            ) : null}
          </div>
        );
      })}
    </>
  );
};

/**
 * Maps `parameter.ui_component` to a concrete control. Recognised:
 *
 *   - "switch"           -> <Switch />
 *   - "number"           -> <InputNumber />
 *   - "select"           -> <Select /> populated from `allowed_values`
 *   - "model-select"     -> editable <Input /> for now; the param value is
 *     a rag model_id (e.g. ocr_model_id="model-ocr-paddle-infer-text").
 *     Long-term this should be a dropdown sourced from
 *     /api/knowledge/rag/model_providers filtered by capability, but the
 *     current ListRagModelProviders endpoint only returns text/image
 *     embedding models — OCR/LLM/rerank entries aren't surfaced yet, so
 *     a free-text fallback unblocks the wizard until that's added.
 *   - "multi-select"     -> editable <Input /> accepting comma-separated
 *     values, parsed to string[] on submit. Same long-term note as above
 *     for a real tag-input control.
 *   - "text" / anything else -> editable <Input />.
 *
 * The unknown-component fallback is intentionally editable (it used to be
 * disabled): a rag-side ui_component rename or a required param like
 * ocr_model_id would otherwise leave the user with no way to satisfy
 * pydantic's required-field check on submit.
 */
const FieldControl: FC<{
  param: DocumentParameter;
  value: unknown;
  onChange: (name: string, fieldValue: unknown) => void;
}> = ({ param, value, onChange }) => {
  const label = param.ui_label || param.name;
  switch (param.ui_component) {
    case 'switch': {
      const current =
        typeof value === 'boolean' ? value : Boolean(param.default);
      return (
        <label
          style={{ display: 'flex', alignItems: 'center', gap: 8 }}
          htmlFor={`dpp-${param.name}`}
        >
          <Switch
            id={`dpp-${param.name}`}
            checked={current}
            onChange={(checked: boolean) => onChange(param.name, checked)}
          />
          <span>{label}</span>
        </label>
      );
    }
    case 'number': {
      const current =
        typeof value === 'number'
          ? value
          : (param.default as number | undefined);
      return (
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <InputNumber
            value={current}
            min={param.min_value}
            max={param.max_value}
            onChange={(next: string | number) =>
              onChange(param.name, Number(next))
            }
          />
        </div>
      );
    }
    case 'select': {
      const current =
        typeof value === 'string'
          ? value
          : (param.default as string | undefined);
      const options = (param.allowed_values ?? []).map(v => ({
        label: String(v),
        value: String(v),
      }));
      return (
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Select
            value={current}
            optionList={options}
            onChange={next => onChange(param.name, next)}
          />
        </div>
      );
    }
    case 'multi-select': {
      // Parameter value is array[string] on the wire; UI is a comma-separated
      // text input that splits/joins on the fly. Trailing-empty splits are
      // dropped so a user typing "zh, " mid-edit doesn't emit ["zh", ""].
      const arr = Array.isArray(value)
        ? (value as unknown[])
        : ((param.default as unknown[] | undefined) ?? []);
      const display = arr.map(v => String(v)).join(', ');
      return (
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Input
            value={display}
            placeholder="e.g. zh, en"
            onChange={(next: string) => {
              const parts = next
                .split(',')
                .map(s => s.trim())
                .filter(Boolean);
              onChange(param.name, parts);
            }}
          />
        </div>
      );
    }
    case 'model-select':
    case 'text':
    default: {
      const current =
        typeof value === 'string'
          ? value
          : ((param.default as string | undefined) ?? '');
      return (
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ marginBottom: 4 }}>{label}</span>
          <Input
            value={current}
            onChange={(next: string) => onChange(param.name, next)}
          />
        </div>
      );
    }
  }
};
