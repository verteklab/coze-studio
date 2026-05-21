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

import { useEffect, useState } from 'react';

import { ViewVariableType } from '@coze-workflow/base';
import { I18n } from '@coze-arch/i18n';

import { NodeConfigForm } from '@/node-registries/common/components';
import {
  Field,
  Section,
  InputNumberField,
  Select,
  TextareaField,
  useField,
} from '@/form';

import { OutputsField, ValueExpressionInputField } from '../common/fields';

const DEFAULT_OCR_PROVIDER_ID = 'paddle-ocr';

interface SafeOCRProvider {
  id: string;
  name: string;
  enabled?: boolean;
}

const fallbackOCRProviderOptions = [
  {
    value: DEFAULT_OCR_PROVIDER_ID,
    label: 'paddle-ocr',
  },
  {
    value: 'mineru',
    label: 'mineru',
  },
  {
    value: 'easyocr',
    label: 'easyocr',
  },
  {
    value: 'deepseek-ocr2',
    label: 'deepseek-ocr2',
  },
  {
    value: 'deepseek-ocr2-pdf',
    label: 'deepseek-ocr2-pdf',
  },
];

type ProviderOption = (typeof fallbackOCRProviderOptions)[number];

const mergeProviderOptions = (
  apiProviders: SafeOCRProvider[],
): ProviderOption[] => {
  const options = [...fallbackOCRProviderOptions];
  const seen = new Set(options.map(option => option.value));

  apiProviders
    .filter(provider => provider.enabled !== false)
    .forEach(provider => {
      if (seen.has(provider.id)) {
        return;
      }
      seen.add(provider.id);
      options.push({
        value: provider.id,
        label: provider.name || provider.id,
      });
    });

  return options;
};

const OCRProviderSelect = ({ options }: { options: ProviderOption[] }) => {
  const { value, onChange, onBlur, errors, readonly } = useField<string>();

  useEffect(() => {
    if (!value || !options.some(option => option.value === value)) {
      onChange(DEFAULT_OCR_PROVIDER_ID);
    }
  }, [value, options, onChange]);

  return (
    <Select
      disabled={readonly}
      value={
        options.some(option => option.value === value)
          ? value
          : DEFAULT_OCR_PROVIDER_ID
      }
      optionList={options}
      onChange={v => onChange(v as string)}
      onBlur={onBlur}
      hasError={errors && errors.length > 0}
      className="w-full"
    />
  );
};

const OpenAIVisionFields = () => {
  const [providerOptionsForSelect, setProviderOptionsForSelect] = useState(
    fallbackOCRProviderOptions,
  );

  useEffect(() => {
    let cancelled = false;

    void fetch('/api/workflow_api/ocr_providers')
      .then(resp => resp.json())
      .then((resp: { data?: SafeOCRProvider[] }) => {
        if (cancelled || !Array.isArray(resp.data) || resp.data.length === 0) {
          return;
        }
        setProviderOptionsForSelect(mergeProviderOptions(resp.data));
      })
      .catch(() => {
        // Keep the static fallback so local testing is not blocked by API load order.
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <>
      <Section title={I18n.t('node_ocr_provider', {}, 'OCR Provider')}>
        <Field
          name="inputs.ocrConfig.provider_id"
          defaultValue={DEFAULT_OCR_PROVIDER_ID}
        >
          <OCRProviderSelect options={providerOptionsForSelect} />
        </Field>
      </Section>
      <Section title="OCR Prompt">
        <TextareaField
          name="inputs.ocrConfig.prompt"
          placeholder={I18n.t(
            'node_ocr_prompt_placeholder',
            {},
            'Default: Extract all text from the image, preserving original layout.',
          )}
          className="w-full"
          rows={3}
        />
      </Section>
      <Section title="Max Tokens">
        <InputNumberField
          name="inputs.ocrConfig.max_tokens"
          defaultValue={8192}
          min={1}
          max={65536}
          className="w-full"
          style={{
            height: '24px',
            borderColor:
              'var(--Stroke-COZ-stroke-plus, rgba(84, 97, 156, 0.27))',
          }}
        />
      </Section>
    </>
  );
};

const OCR_FILE_INPUT_TYPES = [
  ViewVariableType.File,
  ViewVariableType.Image,
  ViewVariableType.Doc,
];

const FileInputField = () => (
  <Section title={I18n.t('node_ocr_file_input', {}, 'File Input')}>
    <ValueExpressionInputField
      name="inputs.inputParameters.0.input"
      inputType={ViewVariableType.File}
      availableFileTypes={OCR_FILE_INPUT_TYPES}
    />
  </Section>
);

const Render = () => (
  <NodeConfigForm>
    <FileInputField />

    <OpenAIVisionFields />

    <Section
      title={I18n.t('node_http_timeout_setting', {}, 'Timeout (seconds)')}
    >
      <InputNumberField
        name="inputs.ocrSetting.timeout"
        defaultValue={120}
        max={600}
        min={0}
        className="w-full"
        style={{
          height: '24px',
          borderColor: 'var(--Stroke-COZ-stroke-plus, rgba(84, 97, 156, 0.27))',
        }}
      />
    </Section>
    <Section title={I18n.t('node_http_retry_count', {}, 'Retry Count')}>
      <InputNumberField
        name="inputs.ocrSetting.retryTimes"
        defaultValue={3}
        max={10}
        min={0}
        className="w-full"
        style={{
          height: '24px',
          borderColor: 'var(--Stroke-COZ-stroke-plus, rgba(84, 97, 156, 0.27))',
        }}
      />
    </Section>

    <OutputsField
      title={I18n.t('workflow_detail_node_output', {}, 'Output')}
      tooltip={I18n.t('node_ocr_output_desc', {}, 'OCR recognition results')}
      id="ocr-node-outputs"
      name="outputs"
      topLevelReadonly={true}
      customReadonly
    />
  </NodeConfigForm>
);

export default Render;
