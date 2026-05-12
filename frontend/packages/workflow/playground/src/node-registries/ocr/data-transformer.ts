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

/**
 * Node Backend Data -> Frontend Form Data
 */
export const transformOnInit = (
  value: NodeDataDTO,
  _context: NodeFormContext,
) => {
  const { inputs = {}, outputs } = value || {};
  const rawInputs = inputs as Record<string, unknown>;

  const inputParameters = (rawInputs.inputParameters as any[]) || [
    {
      key: nanoid(),
      name: 'file',
      type: ViewVariableType.File,
      input: undefined,
    },
  ];

  const initValue = {
    nodeMeta: value?.nodeMeta,
    inputs: {
      inputParameters,
      providerType: OCRProviderType.OpenAIVision,
      ocrConfig: {},
      ocrSetting: {
        timeout: 120,
        retryTimes: 3,
      },
      ...rawInputs,
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
  const formattedValue: Record<string, unknown> = {
    ...value,
  };

  return formattedValue as unknown as NodeDataDTO;
};
