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

import { describe, expect, it } from 'vitest';

import { findMissingRequired } from './validate';
import { type DocumentParameter, type DocumentParameterSchema } from './types';

const param = (over: Partial<DocumentParameter>): DocumentParameter => ({
  name: 'p',
  type: 'string',
  group: 'g',
  required: false,
  description: '',
  ui_label: '',
  ui_component: 'text',
  advanced: false,
  ...over,
});

const schema = (params: DocumentParameter[]): DocumentParameterSchema => ({
  schema_id: 'test',
  description: '',
  file_types: [],
  source_modalities: [],
  parameters: params,
});

describe('findMissingRequired', () => {
  it('flags a required string with no value and no default', () => {
    const s = schema([
      param({ name: 'ocr_model_id', required: true, default: null }),
    ]);
    expect(findMissingRequired(s, {}).map(p => p.name)).toEqual([
      'ocr_model_id',
    ]);
  });

  it('flags an empty-string value as missing', () => {
    const s = schema([
      param({ name: 'ocr_model_id', required: true, default: null }),
    ]);
    expect(
      findMissingRequired(s, { ocr_model_id: '   ' }).map(p => p.name),
    ).toEqual(['ocr_model_id']);
  });

  it('passes when the value is set', () => {
    const s = schema([
      param({ name: 'ocr_model_id', required: true, default: null }),
    ]);
    expect(findMissingRequired(s, { ocr_model_id: 'paddle' })).toEqual([]);
  });

  it('passes when the schema supplies a non-empty default', () => {
    const s = schema([
      param({
        name: 'languages',
        required: true,
        default: 'auto',
        type: 'string',
      }),
    ]);
    expect(findMissingRequired(s, {})).toEqual([]);
  });

  it('flags an empty array value', () => {
    const s = schema([
      param({ name: 'tags', required: true, type: 'array', default: null }),
    ]);
    expect(findMissingRequired(s, { tags: [] }).map(p => p.name)).toEqual([
      'tags',
    ]);
  });

  it('ignores non-required params even if empty', () => {
    const s = schema([param({ name: 'note', required: false })]);
    expect(findMissingRequired(s, {})).toEqual([]);
  });
});
