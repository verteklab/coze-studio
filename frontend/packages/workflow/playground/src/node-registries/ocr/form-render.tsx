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

import { I18n } from '@coze-arch/i18n';

import { NodeConfigForm } from '@/node-registries/common/components';
import {
  Section,
  InputField,
  InputNumberField,
  SelectField,
  TextareaField,
  useWatch,
} from '@/form';
import { OutputsField } from '../common/fields';

import { OCRProviderType } from './types';

const providerOptions = [
  {
    value: OCRProviderType.OpenAIVision,
    label: 'OpenAI Vision',
  },
  {
    value: OCRProviderType.MinerU,
    label: 'MinerU',
  },
  {
    value: OCRProviderType.PaddleOCR,
    label: 'PaddleOCR',
  },
  {
    value: OCRProviderType.Custom,
    label: I18n.t('node_ocr_custom_http', {}, 'Custom HTTP'),
  },
];

const authTypeOptions = [
  { value: '', label: I18n.t('node_ocr_auth_none', {}, 'None') },
  { value: 'bearer', label: 'Bearer Token' },
  { value: 'custom', label: I18n.t('node_ocr_auth_custom', {}, 'Custom Header') },
];

const contentTypeOptions = [
  { value: 'application/json', label: 'application/json' },
  { value: 'multipart/form-data', label: 'multipart/form-data' },
];

const OpenAIVisionFields = () => (
  <>
    <Section title="Endpoint URL">
      <InputField
        name="inputs.ocrConfig.endpoint"
        placeholder="https://api.example.com"
        className="w-full"
      />
    </Section>
    <Section title="API Key">
      <InputField
        name="inputs.ocrConfig.api_key"
        placeholder={I18n.t('node_ocr_api_key_placeholder', {}, 'Optional for self-hosted vLLM')}
        className="w-full"
      />
    </Section>
    <Section title="Model">
      <InputField
        name="inputs.ocrConfig.model"
        placeholder="deepseek-ai/DeepSeek-OCR"
        className="w-full"
      />
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
          borderColor: 'var(--Stroke-COZ-stroke-plus, rgba(84, 97, 156, 0.27))',
        }}
      />
    </Section>
  </>
);

const MinerUFields = () => (
  <Section title="Endpoint URL">
    <InputField
      name="inputs.ocrConfig.endpoint"
      placeholder="http://localhost:8888"
      className="w-full"
    />
  </Section>
);

const PaddleOCRFields = () => (
  <Section title="Endpoint URL">
    <InputField
      name="inputs.ocrConfig.endpoint"
      placeholder="http://localhost:8080/ocr"
      className="w-full"
    />
  </Section>
);

const CustomHTTPFields = () => {
  const authType: string = useWatch('inputs.ocrConfig.auth_type') || '';

  return (
    <>
      <Section title="URL">
        <InputField
          name="inputs.ocrConfig.url"
          placeholder="https://api.example.com/ocr"
          className="w-full"
        />
      </Section>
      <Section title="Method">
        <InputField
          name="inputs.ocrConfig.method"
          placeholder="POST"
          className="w-full"
        />
      </Section>
      <Section title="Content-Type">
        <SelectField
          name="inputs.ocrConfig.content_type"
          options={contentTypeOptions}
          defaultValue="application/json"
          className="w-full"
        />
      </Section>
      <Section title={I18n.t('node_ocr_auth', {}, 'Authentication')}>
        <SelectField
          name="inputs.ocrConfig.auth_type"
          options={authTypeOptions}
          defaultValue=""
          className="w-full"
        />
      </Section>
      {authType === 'bearer' && (
        <Section title="Bearer Token">
          <InputField
            name="inputs.ocrConfig.auth_token"
            placeholder="Token"
            className="w-full"
          />
        </Section>
      )}
      {authType === 'custom' && (
        <>
          <Section title="Header Name">
            <InputField
              name="inputs.ocrConfig.auth_header"
              placeholder="X-API-Key"
              className="w-full"
            />
          </Section>
          <Section title="Header Value">
            <InputField
              name="inputs.ocrConfig.auth_value"
              placeholder="your-api-key"
              className="w-full"
            />
          </Section>
        </>
      )}
      <Section title="Body Template">
        <TextareaField
          name="inputs.ocrConfig.body_template"
          placeholder={'{"file": "{{file_base64}}", "type": "ocr"}'}
          className="w-full"
          rows={4}
        />
      </Section>
      <Section title={I18n.t('node_ocr_json_path', {}, 'Response JSONPath')}>
        <InputField
          name="inputs.ocrConfig.json_path"
          placeholder="result.text"
          className="w-full"
        />
      </Section>
    </>
  );
};

const ProviderFields = () => {
  const providerType: string = useWatch('inputs.providerType');

  switch (providerType) {
    case OCRProviderType.OpenAIVision:
      return <OpenAIVisionFields />;
    case OCRProviderType.MinerU:
      return <MinerUFields />;
    case OCRProviderType.PaddleOCR:
      return <PaddleOCRFields />;
    case OCRProviderType.Custom:
      return <CustomHTTPFields />;
    default:
      return <OpenAIVisionFields />;
  }
};

const Render = () => (
  <NodeConfigForm>
    <Section title={I18n.t('node_ocr_provider', {}, 'OCR Provider')}>
      <SelectField
        name="inputs.providerType"
        options={providerOptions}
        defaultValue={OCRProviderType.OpenAIVision}
        className="w-full"
      />
    </Section>

    <ProviderFields />

    <Section title={I18n.t('node_http_timeout_setting', {}, 'Timeout (seconds)')}>
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
