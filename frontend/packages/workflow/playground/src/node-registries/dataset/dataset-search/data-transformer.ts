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
import { isNil, set } from 'lodash-es';
import { BlockInput, ViewVariableType } from '@coze-workflow/base';

export function transformOnInit(value) {
  // New drag-in node initialization
  if (!value) {
    return {
      nodeMeta: undefined,
      inputs: {
        inputParameters: {
          Query: { type: 'ref', content: '' },
        },
        datasetParameters: {
          datasetParam: [],
          datasetSetting: {},
        },
      },
      outputs: [
        {
          key: nanoid(),
          name: 'outputList',
          type: ViewVariableType.ArrayObject,
          children: [
            {
              key: nanoid(),
              name: 'output',
              type: ViewVariableType.String,
            },
          ],
        },
      ],
    };
  }

  const { inputParameters, datasetParam } = value.inputs;
  const formData = {
    ...value,
    inputs: {
      datasetParameters: {},
    },
  };

  formData.inputs.inputParameters = inputParameters.reduce(
    (map, obj: { name: string | number; input: unknown }) => {
      map[obj.name] = obj.input;
      return map;
    },
    {},
  );
  formData.inputs.datasetParameters.datasetParam = datasetParam[0]?.input.value
    .content as string[];
  // In the case of initial creation/stock data, the top_k and other rag params
  // may be empty; defaults are handled by the dataset-settings component.
  // Legacy wire-keys (useRewrite/useRerank/useNl2sql/isPersonalOnly/minScore/
  // documentIDs) are silently dropped per the no-migration decision — old
  // workflow JSON with those keys simply does not populate any DataSetInfo field.
  formData.inputs.datasetParameters.datasetSetting = {
    top_k: datasetParam.find(item => item.name === 'topK')?.input.value
      .content as number | undefined,
    strategy: datasetParam.find(item => item.name === 'strategy')?.input.value
      .content as number | undefined,
    rewrite: datasetParam.find(item => item.name === 'rewrite')?.input.value
      .content as boolean | undefined,
    expansion: datasetParam.find(item => item.name === 'expansion')?.input.value
      .content as boolean | undefined,
    multi_query: datasetParam.find(item => item.name === 'multiQuery')?.input
      .value.content as boolean | undefined,
    enable_rerank: datasetParam.find(item => item.name === 'enableRerank')
      ?.input.value.content as boolean | undefined,
    query_mode: datasetParam.find(item => item.name === 'queryMode')?.input
      .value.content as
      | 'text_input'
      | 'image_input'
      | 'mixed_input'
      | undefined,
    query_image: datasetParam.find(item => item.name === 'queryImage')?.input
      .value.content as
      | { image_base64?: string; image_ref?: string }
      | undefined,
    target_chunk_types: datasetParam.find(
      item => item.name === 'targetChunkTypes',
    )?.input.value.content as Array<'text_chunk' | 'image_chunk'> | undefined,
    filters: datasetParam.find(item => item.name === 'filters')?.input.value
      .content as Record<string, unknown> | undefined,
    retrievers: datasetParam.find(item => item.name === 'retrievers')?.input
      .value.content as Array<'dense' | 'bm25' | 'image_vector'> | undefined,
    fusion_policy: datasetParam.find(item => item.name === 'fusionPolicy')
      ?.input.value.content as Record<string, unknown> | undefined,
    retriever_params: datasetParam.find(item => item.name === 'retrieverParams')
      ?.input.value.content as Record<string, unknown> | undefined,
  };

  return formData;
}

export function transformOnSubmit(value) {
  const { nodeMeta, inputs, outputs } = value;
  const { inputParameters = { Query: { type: 'ref' } }, datasetParameters } =
    inputs ?? {};
  const { datasetParam, datasetSetting } = datasetParameters ?? {};
  const actualData = {
    nodeMeta,
    outputs,
    inputs: {
      datasetParam: [] as unknown[],
    },
  };

  set(
    actualData.inputs,
    'inputParameters',
    Object.entries(inputParameters).map(([key, mapValue]) => ({
      name: key,
      input: mapValue,
    })) || [],
  );

  set(actualData.inputs, 'datasetParam', [
    {
      name: 'datasetList',
      input: {
        type: 'list',
        schema: {
          type: 'string',
        },
        value: {
          type: 'literal',
          content: datasetParam || [],
        },
      },
    },
    {
      name: 'topK',
      input: {
        type: 'integer',
        value: {
          type: 'literal',
          content: datasetSetting?.top_k,
        },
      },
    },
  ]);

  // 4-boolean query_strategy. Always emitted so the field is present in JSON
  // even when false (consistency with the surviving topK entry above).
  actualData.inputs.datasetParam.push(
    BlockInput.createBoolean('rewrite', datasetSetting?.rewrite),
    BlockInput.createBoolean('expansion', datasetSetting?.expansion),
    BlockInput.createBoolean('multiQuery', datasetSetting?.multi_query),
    BlockInput.createBoolean('enableRerank', datasetSetting?.enable_rerank),
  );

  // New top-level rag fields. Only emitted when non-empty so old workflow
  // JSON without these keys doesn't grow on save.
  if (datasetSetting?.query_mode) {
    actualData.inputs.datasetParam.push({
      name: 'queryMode',
      input: {
        type: 'string',
        value: {
          type: 'literal',
          content: datasetSetting.query_mode,
        },
      },
    });
  }

  if (
    datasetSetting?.query_image &&
    (datasetSetting.query_image.image_base64 ||
      datasetSetting.query_image.image_ref)
  ) {
    actualData.inputs.datasetParam.push({
      name: 'queryImage',
      input: {
        type: 'object',
        value: {
          type: 'literal',
          content: datasetSetting.query_image,
        },
      },
    });
  }

  if (
    Array.isArray(datasetSetting?.target_chunk_types) &&
    datasetSetting.target_chunk_types.length > 0
  ) {
    actualData.inputs.datasetParam.push(
      BlockInput.createArray(
        'targetChunkTypes',
        datasetSetting.target_chunk_types,
        {
          type: 'string',
        },
      ),
    );
  }

  if (
    datasetSetting?.filters &&
    Object.keys(datasetSetting.filters).length > 0
  ) {
    actualData.inputs.datasetParam.push({
      name: 'filters',
      input: {
        type: 'object',
        value: {
          type: 'literal',
          content: datasetSetting.filters,
        },
      },
    });
  }

  if (
    Array.isArray(datasetSetting?.retrievers) &&
    datasetSetting.retrievers.length > 0
  ) {
    actualData.inputs.datasetParam.push(
      BlockInput.createArray('retrievers', datasetSetting.retrievers, {
        type: 'string',
      }),
    );
  }

  if (
    datasetSetting?.fusion_policy &&
    Object.keys(datasetSetting.fusion_policy).length > 0
  ) {
    actualData.inputs.datasetParam.push({
      name: 'fusionPolicy',
      input: {
        type: 'object',
        value: {
          type: 'literal',
          content: datasetSetting.fusion_policy,
        },
      },
    });
  }

  if (
    datasetSetting?.retriever_params &&
    Object.keys(datasetSetting.retriever_params).length > 0
  ) {
    actualData.inputs.datasetParam.push({
      name: 'retrieverParams',
      input: {
        type: 'object',
        value: {
          type: 'literal',
          content: datasetSetting.retriever_params,
        },
      },
    });
  }

  // Added search policy configuration, there may be no strategy data not in grey release
  // Strategy may be 0, hence the explicit isNil check.
  if (!isNil(datasetSetting?.strategy)) {
    actualData.inputs.datasetParam.push({
      name: 'strategy',
      input: {
        type: 'integer',
        value: {
          type: 'literal',
          content: datasetSetting?.strategy,
        },
      },
    });
  }

  return actualData;
}
