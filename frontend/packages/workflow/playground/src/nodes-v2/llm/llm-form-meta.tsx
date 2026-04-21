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

import { get, set, omit, isEmpty } from 'lodash-es';
import {
  ValidateTrigger,
  type FormMetaV2,
  nanoid,
  type Validate,
} from '@flowgram-adapter/free-layout-editor';
import { nodeUtils, ViewVariableType } from '@coze-workflow/nodes';
import {
  BlockInput,
  type InputValueDTO,
  type ViewVariableTreeNode,
  ValueExpressionType,
} from '@coze-workflow/base';
import { I18n } from '@coze-arch/i18n';

import { WorkflowModelsService } from '@/services';
import { provideNodeOutputVariablesEffect } from '@/nodes-v2/materials/provide-node-output-variables';
import { createProvideNodeBatchVariables } from '@/nodes-v2/materials/provide-node-batch-variable';
import { fireNodeTitleChange } from '@/nodes-v2/materials/fire-node-title-change';
import { createValueExpressionInputValidate } from '@/nodes-v2/materials/create-value-expression-input-validate';
import { createNodeInputNameValidate } from '@/nodes-v2/components/node-input-name/validate';

import { nodeMetaValidate } from '../materials/node-meta-validate';
import {
  llmOutputTreeMetaValidator,
  llmInputNameValidator,
} from './validators';
import {
  getDefaultLLMParams,
  modelItemToBlockInput,
  reviseLLMParamPair,
} from './utils';
import { type FormData } from './types';
import {
  formatFcParamOnInit,
  formatFcParamOnSubmit,
} from './skills/data-transformer';
import { Render } from './llm-form-render';
import { isCustomHTTPModel } from './custom-http-utils';
import {
  formatReasoningContentOnInit,
  formatReasoningContentOnSubmit,
  provideReasoningContentEffect,
} from './cot';

/** Default session rounds */
const DEFAULT_CHAT_ROUND = 3;

const NEW_NODE_DEFAULT_VERSION = '3';

const userPromptFieldKey = '$$prompt_decorator$$.prompt';

const getInitialLLMParams = (value, modelsService: WorkflowModelsService) => {
  const llmParam = get(value, 'inputs.llmParam');

  if (llmParam) {
    return llmParam;
  }

  return getDefaultLLMParams(modelsService.getModels());
};

const getModelStateFromLLMParams = (llmParam: InputValueDTO[]) => {
  const model: Record<string, unknown> = {};

  llmParam.forEach((item: InputValueDTO) => {
    const [key, paramValue] = reviseLLMParamPair(item);
    model[key] = paramValue;
  });

  const { prompt } = model;

  return {
    prompt,
    model: omit(model, ['prompt', 'systemPrompt', 'enableChatHistory']),
  };
};

const getChatHistorySetting = (llmParam: InputValueDTO[]) => ({
  enableChatHistory:
    get(
      llmParam.find(item => item.name === 'enableChatHistory'),
      'input.value.content',
    ) || false,
  chatHistoryRound: Number(
    get(
      llmParam.find(item => item.name === 'chatHistoryRound'),
      'input.value.content',
      DEFAULT_CHAT_ROUND,
    ),
  ),
});

const getInitOutputs = ({
  outputs,
  modelsService,
  isBatch,
  modelType,
}: {
  outputs: ViewVariableTreeNode[] | undefined;
  modelsService: WorkflowModelsService;
  isBatch: boolean;
  modelType?: number;
}) =>
  isEmpty(outputs)
    ? [{ name: 'output', type: ViewVariableType.String, key: nanoid() }]
    : formatReasoningContentOnInit({
        modelsService,
        isBatch,
        outputs,
        modelType,
      });

const getVersionFromBackend = (
  schemaJSON: string | undefined,
  nodeId: string,
) => {
  const schema = JSON.parse(schemaJSON || '{}');
  const curNode = schema?.nodes?.find(_node => _node.id === nodeId);

  return parseInt(curNode?.data?.version) >= parseInt(NEW_NODE_DEFAULT_VERSION)
    ? curNode?.data?.version
    : NEW_NODE_DEFAULT_VERSION;
};

export const LLM_FORM_META: FormMetaV2<FormData> = {
  render: props => <Render {...props} />,
  validateTrigger: ValidateTrigger.onChange,
  validate: {
    nodeMeta: nodeMetaValidate,
    outputs: llmOutputTreeMetaValidator,
    '$$input_decorator$$.inputParameters.*.name': llmInputNameValidator,
    '$$input_decorator$$.inputParameters.*.input':
      createValueExpressionInputValidate({ required: true }),
    'batch.inputLists.*.name': createNodeInputNameValidate({
      getNames: ({ formValues }) =>
        (get(formValues, 'batch.inputLists') || []).map(item => item.name),
      skipValidate: ({ formValues }) => formValues.batchMode === 'single',
    }),
    [userPromptFieldKey]: (({ value, formValues, context }) => {
      const { playgroundContext } = context;
      const modelType = get(formValues, 'model.modelType');
      const curModel = playgroundContext?.models?.find(
        model => model.model_type === modelType,
      );
      if (isCustomHTTPModel(curModel)) {
        return undefined;
      }
      const isUserPromptRequired = curModel?.is_up_required ?? false;
      if (!isUserPromptRequired) {
        return undefined;
      }
      return value?.length
        ? undefined
        : I18n.t('workflow_detail_llm_prompt_error_empty');
    }) as Validate,
  },
  effect: {
    nodeMeta: fireNodeTitleChange,
    batchMode: createProvideNodeBatchVariables('batchMode', 'batch.inputLists'),
    'batch.inputLists': createProvideNodeBatchVariables(
      'batchMode',
      'batch.inputLists',
    ),
    outputs: provideNodeOutputVariablesEffect,
    model: provideReasoningContentEffect,
  },
  formatOnInit(value, context) {
    const { node, playgroundContext } = context;
    const modelsService = node.getService<WorkflowModelsService>(
      WorkflowModelsService,
    );
    const llmParam = getInitialLLMParams(value, modelsService);
    const { prompt, model } = getModelStateFromLLMParams(llmParam);

    const inputParameters = get(value, 'inputs.inputParameters');
    const outputs = get(value, 'outputs');
    const isBatch = Boolean(get(value, 'inputs.batch.batchEnable'));

    const initValue = {
      nodeMeta: value?.nodeMeta,
      $$input_decorator$$: {
        inputParameters: !inputParameters
          ? [{ name: 'input', input: { type: ValueExpressionType.REF } }]
          : inputParameters,
        chatHistorySetting: getChatHistorySetting(llmParam),
      },
      outputs: getInitOutputs({
        outputs,
        modelsService,
        isBatch,
        modelType: model.modelType as number | undefined,
      }),

      // The model will re-fill the value according to llmParam, and the
      // previous chatHistoryRound will also be filled at this time.
      // Since a chatHistoryRound will be re-added when submitting, ignore it
      // here to avoid problems.
      model: omit(model, ['chatHistoryRound']),
      $$prompt_decorator$$: {
        prompt,
        systemPrompt: get(
          llmParam.find(item => item.name === 'systemPrompt'),
          'input.value.content',
        ),
      },
      batchMode: isBatch ? 'batch' : 'single',
      batch: nodeUtils.batchToVO(get(value, 'inputs.batch'), context),
      fcParam: formatFcParamOnInit(get(value, 'inputs.fcParam')),
    };

    const versionFromBackend = getVersionFromBackend(
      playgroundContext.globalState.info?.schema_json,
      node.id,
    );

    // [LLM node revised requirements, new node defaults to 3]
    set(initValue, 'version', versionFromBackend);

    return initValue;
  },
  formatOnSubmit(value, context) {
    const { node, playgroundContext } = context;
    const { globalState } = playgroundContext;

    const models = node
      .getService<WorkflowModelsService>(WorkflowModelsService)
      .getModels();
    const { model } = value;
    const modelMeta = models.find(m => m.model_type === model.modelType);

    const isCurrentCustomHTTPModel = isCustomHTTPModel(modelMeta);
    const llmParam = modelItemToBlockInput(model, modelMeta);
    const { batchMode } = value;
    const batchDTO = nodeUtils.batchToDTO(value.batch, context);

    const prompt = BlockInput.createString(
      'prompt',
      isCurrentCustomHTTPModel ? '' : value.$$prompt_decorator$$.prompt,
    );

    const enableChatHistory = BlockInput.createBoolean(
      'enableChatHistory',
      // The workflow has no session history, so set false unless chatflow
      // explicitly enables it.
      globalState.isChatflow
        ? Boolean(
            get(
              value,
              '$$input_decorator$$.chatHistorySetting.enableChatHistory',
            ),
          )
        : false,
    );
    const chatHistoryRound = BlockInput.createInteger(
      'chatHistoryRound',
      get(value, '$$input_decorator$$.chatHistorySetting.chatHistoryRound'),
    );
    const systemPrompt = BlockInput.createString(
      'systemPrompt',
      isCurrentCustomHTTPModel
        ? ''
        : get(value, '$$prompt_decorator$$.systemPrompt'),
    );
    llmParam.push(prompt, enableChatHistory, chatHistoryRound, systemPrompt);
    const isBatch = batchMode === 'batch';
    const formattedValue: Record<string, unknown> = {
      nodeMeta: value.nodeMeta,
      inputs: {
        inputParameters: get(value, '$$input_decorator$$.inputParameters'),
        llmParam,
        fcParam: isCurrentCustomHTTPModel
          ? undefined
          : formatFcParamOnSubmit(value.fcParam),
        batch: isBatch
          ? {
              batchEnable: batchMode === 'batch',
              ...batchDTO,
            }
          : undefined,
      },
      outputs: formatReasoningContentOnSubmit(value.outputs, isBatch),
      /**
       * - "LLM node format optimization" requirement, integrate the output
       *   content into the prompt to limit the output format, the backend
       *   needs flag distinction logic, version 2
       * - "LLM node revised requirements fallback logic", version 3
       */

      version: NEW_NODE_DEFAULT_VERSION,
    };

    return formattedValue;
  },
};
