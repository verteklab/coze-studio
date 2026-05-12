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

import { get } from 'lodash-es';
import {
  ValidateTrigger,
  type FormMetaV2,
} from '@flowgram-adapter/free-layout-editor';
import { I18n } from '@coze-arch/i18n';

import { provideNodeOutputVariablesEffect } from '@/nodes-v2/materials/provide-node-output-variables';
import { nodeMetaValidate } from '@/nodes-v2/materials/node-meta-validate';
import { fireNodeTitleChange } from '@/nodes-v2/materials/fire-node-title-change';
import { createValueExpressionInputValidate } from '@/node-registries/common/validators';

import { OCRProviderType } from './types';
import FormRender from './form-render';
import { transformOnInit, transformOnSubmit } from './data-transformer';

export const OCR_FORM_META: FormMetaV2<FormData> = {
  render: () => <FormRender />,

  validateTrigger: ValidateTrigger.onBlur,

  validate: {
    nodeMeta: nodeMetaValidate,
    'inputs.inputParameters.0.input': createValueExpressionInputValidate({
      emptyMessage: I18n.t('node_ocr_file_required', {}, 'Please select a file'),
    }),
    'inputs.providerType': ({ value }) => {
      if (!value) {
        return I18n.t(
          'node_ocr_provider_required',
          {},
          'Please select an OCR provider',
        );
      }
    },
    'inputs.ocrConfig.endpoint': ({ value, formValues }) => {
      const provider = get(formValues, 'inputs.providerType');
      if (
        provider !== OCRProviderType.Custom &&
        provider !== undefined &&
        (!value || (typeof value === 'string' && value.trim() === ''))
      ) {
        return I18n.t(
          'node_ocr_endpoint_required',
          {},
          'Endpoint URL is required',
        );
      }
    },
    'inputs.ocrConfig.model': ({ value, formValues }) => {
      const provider = get(formValues, 'inputs.providerType');
      if (
        provider === OCRProviderType.OpenAIVision &&
        (!value || (typeof value === 'string' && value.trim() === ''))
      ) {
        return I18n.t('node_ocr_model_required', {}, 'Model name is required');
      }
    },
    'inputs.ocrConfig.url': ({ value, formValues }) => {
      const provider = get(formValues, 'inputs.providerType');
      if (
        provider === OCRProviderType.Custom &&
        (!value || (typeof value === 'string' && value.trim() === ''))
      ) {
        return I18n.t('node_ocr_url_required', {}, 'URL is required');
      }
    },
  },

  effect: {
    nodeMeta: fireNodeTitleChange,
    outputs: provideNodeOutputVariablesEffect,
  },

  formatOnInit: transformOnInit,
  formatOnSubmit: transformOnSubmit,
};
