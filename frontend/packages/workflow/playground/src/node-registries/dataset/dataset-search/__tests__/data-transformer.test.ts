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

import { describe, expect, it, vi } from 'vitest';

// `@coze-workflow/base` (re-exports of the entire SDK) drags in a lottie-web
// chain that fails under jsdom (`canvas.getContext is not a function`).
// We only use `BlockInput.createArray` + `ViewVariableType` enum surface, so
// stub the minimal contract directly. This keeps the test as a pure
// data-transformer unit test rather than a full SDK integration probe.
vi.mock('@coze-workflow/base', () => ({
  ViewVariableType: {
    ArrayObject: 99,
    String: 1,
    Integer: 2,
    Number: 3,
    Boolean: 4,
  },
  BlockInput: {
    createArray: (name: string, value: unknown, schema: unknown) => ({
      name,
      input: {
        type: 'list',
        schema,
        value: { type: 'literal', content: value },
      },
    }),
    createBoolean: (name: string, value: boolean) => ({
      name,
      input: {
        type: 'boolean',
        value: { type: 'literal', content: value, rawMeta: { type: 4 } },
      },
    }),
  },
}));

import { transformOnInit, transformOnSubmit } from '../data-transformer';

// Minimal nodeMeta/outputs shape so the round-trip doesn't blow up on
// unrelated fields; we only assert on the datasetParam/datasetSetting parts.
const baseValue = (datasetParam: unknown[]) => ({
  nodeMeta: { title: 'test' },
  inputs: {
    inputParameters: [{ name: 'Query', input: { type: 'ref', content: '' } }],
    datasetParam,
  },
  outputs: [],
});

describe('dataset-search data-transformer documentIDs', () => {
  it('transformOnSubmit emits a documentIDs list block when document_ids is set', () => {
    const formData = {
      nodeMeta: { title: 'test' },
      inputs: {
        inputParameters: { Query: { type: 'ref', content: '' } },
        datasetParameters: {
          datasetParam: ['kb-1', 'kb-2'],
          datasetSetting: {
            top_k: 5,
            strategy: 1,
            document_ids: [101, 202, 303],
          },
        },
      },
      outputs: [],
    };

    const out = transformOnSubmit(formData) as {
      inputs: { datasetParam: Array<{ name: string; input: unknown }> };
    };

    const docIdsBlock = out.inputs.datasetParam.find(
      p => p.name === 'documentIDs',
    );
    expect(docIdsBlock).toBeDefined();
    expect(docIdsBlock?.input).toMatchObject({
      type: 'list',
      schema: { type: 'integer' },
      value: { type: 'literal', content: [101, 202, 303] },
    });
  });

  it('transformOnSubmit omits the documentIDs block when document_ids is empty or absent', () => {
    const formDataEmpty = {
      nodeMeta: { title: 'test' },
      inputs: {
        inputParameters: { Query: { type: 'ref', content: '' } },
        datasetParameters: {
          datasetParam: ['kb-1'],
          datasetSetting: {
            top_k: 5,
            strategy: 1,
            document_ids: [],
          },
        },
      },
      outputs: [],
    };
    const formDataAbsent = {
      ...formDataEmpty,
      inputs: {
        ...formDataEmpty.inputs,
        datasetParameters: {
          ...formDataEmpty.inputs.datasetParameters,
          datasetSetting: { top_k: 5, strategy: 1 },
        },
      },
    };

    for (const fd of [formDataEmpty, formDataAbsent]) {
      const out = transformOnSubmit(fd) as {
        inputs: { datasetParam: Array<{ name: string }> };
      };
      expect(
        out.inputs.datasetParam.find(p => p.name === 'documentIDs'),
      ).toBeUndefined();
    }
  });

  it('transformOnInit deserializes documentIDs back into datasetSetting.document_ids', () => {
    const dto = baseValue([
      {
        name: 'datasetList',
        input: {
          type: 'list',
          schema: { type: 'string' },
          value: { type: 'literal', content: ['kb-1'] },
        },
      },
      {
        name: 'topK',
        input: { type: 'integer', value: { type: 'literal', content: 5 } },
      },
      {
        name: 'documentIDs',
        input: {
          type: 'list',
          schema: { type: 'integer' },
          value: { type: 'literal', content: [101, 202] },
        },
      },
    ]);

    const form = transformOnInit(dto) as {
      inputs: {
        datasetParameters: { datasetSetting: { document_ids?: number[] } };
      };
    };

    expect(form.inputs.datasetParameters.datasetSetting.document_ids).toEqual([
      101, 202,
    ]);
  });

  it('transformOnInit leaves document_ids undefined when the param is absent', () => {
    const dto = baseValue([
      {
        name: 'datasetList',
        input: {
          type: 'list',
          schema: { type: 'string' },
          value: { type: 'literal', content: ['kb-1'] },
        },
      },
      {
        name: 'topK',
        input: { type: 'integer', value: { type: 'literal', content: 5 } },
      },
    ]);

    const form = transformOnInit(dto) as {
      inputs: {
        datasetParameters: { datasetSetting: { document_ids?: number[] } };
      };
    };

    expect(
      form.inputs.datasetParameters.datasetSetting.document_ids,
    ).toBeUndefined();
  });

  it('round-trip: submit then init returns the original document_ids', () => {
    const submitted = transformOnSubmit({
      nodeMeta: { title: 'test' },
      inputs: {
        inputParameters: { Query: { type: 'ref', content: '' } },
        datasetParameters: {
          datasetParam: ['kb-1'],
          datasetSetting: {
            top_k: 5,
            strategy: 1,
            document_ids: [42, 84],
          },
        },
      },
      outputs: [],
    }) as { inputs: { datasetParam: unknown[] } };

    const form = transformOnInit({
      nodeMeta: { title: 'test' },
      inputs: {
        inputParameters: [
          { name: 'Query', input: { type: 'ref', content: '' } },
        ],
        datasetParam: submitted.inputs.datasetParam,
      },
      outputs: [],
    }) as {
      inputs: {
        datasetParameters: { datasetSetting: { document_ids?: number[] } };
      };
    };

    expect(form.inputs.datasetParameters.datasetSetting.document_ids).toEqual([
      42, 84,
    ]);
  });
});
