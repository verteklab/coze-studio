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

import { nanoid } from 'nanoid';
import { isEmpty } from 'lodash-es';
import { type NodeFormContext } from '@flowgram-adapter/free-layout-editor';
import { type NodeDataDTO, ViewVariableType } from '@coze-workflow/base';

import { OCRProviderType } from './types';

interface FormData {
  inputs: Record<string, unknown>;
}

const DEFAULT_OCR_PROVIDER_ID = 'paddle-ocr';
const legacyProviderIDs = new Set([
  'deepseek_ocr2_gpu7',
  'paddleocr-openai-pdf-api',
]);

const normalizeProviderID = (providerID: unknown) => {
  if (typeof providerID !== 'string' || providerID.trim() === '') {
    return DEFAULT_OCR_PROVIDER_ID;
  }
  if (legacyProviderIDs.has(providerID)) {
    return DEFAULT_OCR_PROVIDER_ID;
  }
  return providerID;
};

/**
 * Node Backend Data -> Frontend Form Data
 */
export const transformOnInit = (
  value: NodeDataDTO,
  _context: NodeFormContext,
) => {
  const { inputs = {}, outputs } = value || {};
  const rawInputs = inputs as Record<string, unknown>;

  const existingParams = rawInputs.inputParameters;
  const inputParameters =
    Array.isArray(existingParams) && existingParams.length > 0
      ? existingParams
      : [
          {
            key: nanoid(),
            name: 'file',
            type: ViewVariableType.File,
            input: undefined,
          },
        ];

  const defaultOcrConfig = {
    provider_id: DEFAULT_OCR_PROVIDER_ID,
    max_tokens: 8192,
  };

  const existingOcrConfig =
    typeof rawInputs.ocrConfig === 'object' && rawInputs.ocrConfig !== null
      ? (rawInputs.ocrConfig as Record<string, unknown>)
      : {};
  const normalizedOcrConfig = {
    ...existingOcrConfig,
    provider_id: normalizeProviderID(existingOcrConfig.provider_id),
  };

  const initValue = {
    nodeMeta: value?.nodeMeta,
    inputs: {
      inputParameters,
      providerType: OCRProviderType.OpenAIVision,
      ocrConfig: { ...defaultOcrConfig, ...normalizedOcrConfig },
      ocrSetting: {
        timeout: 120,
        retryTimes: 3,
      },
      ...rawInputs,
      providerType: rawInputs.providerType ?? OCRProviderType.OpenAIVision,
      ocrConfig: { ...defaultOcrConfig, ...normalizedOcrConfig },
      inputParameters,
    },
    outputs: isEmpty(outputs)
      ? [
          {
            key: nanoid(),
            type: ViewVariableType.String,
            name: 'text',
          },
          {
            key: nanoid(),
            type: ViewVariableType.Object,
            name: 'raw_response',
          },
        ]
      : outputs,
  };

  return initValue;
};

/**
 * Frontend Form Data -> Node Backend Data
 */
export const transformOnSubmit = (
  value: FormData,
  _context: NodeFormContext,
): NodeDataDTO => {
  const inputs = (value.inputs || {}) as Record<string, unknown>;
  const ocrConfig =
    typeof inputs.ocrConfig === 'object' && inputs.ocrConfig !== null
      ? { ...(inputs.ocrConfig as Record<string, unknown>) }
      : {};

  [
    'endpoint',
    'api_key',
    'model',
    'message_format',
    'paddleocr_output_format',
    'url',
    'auth_token',
    'auth_value',
    'auth_header',
    'body_template',
    'json_path',
  ].forEach(key => {
    delete ocrConfig[key];
  });
  ocrConfig.provider_id = normalizeProviderID(ocrConfig.provider_id);

  const formattedValue: Record<string, unknown> = {
    ...value,
    inputs: {
      ...inputs,
      providerType: OCRProviderType.OpenAIVision,
      ocrConfig,
    },
  };

  return formattedValue as unknown as NodeDataDTO;
};
