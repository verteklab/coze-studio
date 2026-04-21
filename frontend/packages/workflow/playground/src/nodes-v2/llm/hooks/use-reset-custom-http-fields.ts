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

import { useCallback } from 'react';

import { type FormRenderProps } from '@flowgram-adapter/free-layout-editor';

import { type WorkflowModelsService } from '@/services';

import { type FormData } from '../types';
import { isCustomHTTPModel } from '../custom-http-utils';

export const useResetCustomHTTPFields = (
  form: FormRenderProps<FormData>['form'],
  modelsService: WorkflowModelsService,
) =>
  useCallback(
    (nextValue: unknown) => {
      const nextModelType =
        nextValue && typeof nextValue === 'object' && 'modelType' in nextValue
          ? (nextValue.modelType as number | undefined)
          : undefined;
      const nextModel = modelsService.getModelByType(nextModelType);
      if (!isCustomHTTPModel(nextModel)) {
        return;
      }
      form.setValueIn('$$prompt_decorator$$.prompt', '');
      form.setValueIn('$$prompt_decorator$$.systemPrompt', '');
      form.setValueIn('fcParam', undefined);
    },
    [form, modelsService],
  );
