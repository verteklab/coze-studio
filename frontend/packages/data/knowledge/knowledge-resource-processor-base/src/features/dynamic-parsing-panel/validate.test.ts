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

import {
  applyForcedParams,
  filterParamsByDependencies,
  findMissingRequired,
  mergeSchemaDefaults,
} from './validate';
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

describe('mergeSchemaDefaults', () => {
  it('seeds defaults for untouched fields and preserves user values', () => {
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: true }),
      param({ name: 'chunk_size', ui_component: 'number', default: 512 }),
      param({ name: 'split_by_page', ui_component: 'switch', default: true }),
    ]);
    // User toggled chunk_size only.
    expect(mergeSchemaDefaults(s, { chunk_size: 256 })).toEqual({
      enable_ocr: true,
      chunk_size: 256,
      split_by_page: true,
    });
  });

  it('skips params whose default is undefined or null', () => {
    const s = schema([
      param({ name: 'ocr_model_id', default: null }),
      param({ name: 'note' }), // no default key at all
      param({ name: 'enable_ocr', default: true }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({ enable_ocr: true });
  });

  it('lets the user explicitly override a default with falsy value', () => {
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: true }),
    ]);
    expect(mergeSchemaDefaults(s, { enable_ocr: false })).toEqual({
      enable_ocr: false,
    });
  });

  // Rag's ingestion validator rejects {ocr_model_id, enable_ocr:false} with
  // 40001 ("ocr_model_id requires enable_ocr=true"). image_document declares
  // ocr_model_id with default=null + required=false and enable_ocr default=false,
  // so without this mutex the synthesised FRONTEND_PARAM_DEFAULTS leak onto
  // the wire on every image-KB upload that leaves OCR off.
  it('drops ocr_model_id when enable_ocr defaults to false', () => {
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({
        name: 'ocr_model_id',
        ui_component: 'text',
        default: 'model-ocr-paddle-infer-text',
      }),
      param({
        name: 'enable_image_embedding',
        ui_component: 'switch',
        default: true,
      }),
    ]);
    expect(mergeSchemaDefaults(s, {})).toEqual({
      enable_ocr: false,
      enable_image_embedding: true,
    });
  });

  it('drops user-supplied ocr_model_id when enable_ocr is toggled off', () => {
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: true }),
      param({ name: 'ocr_model_id', ui_component: 'text', default: 'paddle' }),
    ]);
    expect(
      mergeSchemaDefaults(s, { enable_ocr: false, ocr_model_id: 'custom' }),
    ).toEqual({ enable_ocr: false });
  });

  it('keeps ocr_model_id when enable_ocr is true', () => {
    const s = schema([
      param({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      param({ name: 'ocr_model_id', ui_component: 'text', default: 'paddle' }),
    ]);
    expect(mergeSchemaDefaults(s, { enable_ocr: true })).toEqual({
      enable_ocr: true,
      ocr_model_id: 'paddle',
    });
  });
});

describe('filterParamsByDependencies', () => {
  // Mirrors rag's ingestion-validator dependency chain: with enable_ocr off,
  // the entire ocr / ocr_text param groups are inert (mergeSchemaDefaults
  // already strips ocr_model_id from the wire). Hiding the widgets keeps the
  // form honest — the user can't tweak knobs that have no effect.
  const ocr = (over: Partial<DocumentParameter>): DocumentParameter =>
    param({ group: 'ocr', ...over });
  const ocrText = (over: Partial<DocumentParameter>): DocumentParameter =>
    param({ group: 'ocr_text', ...over });

  it('hides ocr / ocr_text params when enable_ocr is false (schema default)', () => {
    const s = schema([
      ocr({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      ocr({ name: 'ocr_model_id', default: 'paddle' }),
      ocr({ name: 'ocr_languages' }),
      ocrText({ name: 'chunk_size_ocr_text' }),
      param({
        name: 'enable_image_embedding',
        group: 'image_chunking',
        default: true,
      }),
    ]);
    const names = filterParamsByDependencies(s.parameters, {}, s).map(
      p => p.name,
    );
    expect(names).toEqual(['enable_ocr', 'enable_image_embedding']);
  });

  it('hides ocr params when enable_ocr is explicitly toggled off', () => {
    const s = schema([
      ocr({ name: 'enable_ocr', ui_component: 'switch', default: true }),
      ocr({ name: 'ocr_model_id', default: 'paddle' }),
      ocrText({ name: 'chunk_size_ocr_text' }),
    ]);
    const names = filterParamsByDependencies(
      s.parameters,
      { enable_ocr: false },
      s,
    ).map(p => p.name);
    expect(names).toEqual(['enable_ocr']);
  });

  it('shows all params when enable_ocr is true', () => {
    const s = schema([
      ocr({ name: 'enable_ocr', ui_component: 'switch', default: false }),
      ocr({ name: 'ocr_model_id', default: 'paddle' }),
      ocrText({ name: 'chunk_size_ocr_text' }),
    ]);
    const names = filterParamsByDependencies(
      s.parameters,
      { enable_ocr: true },
      s,
    ).map(p => p.name);
    expect(names).toEqual([
      'enable_ocr',
      'ocr_model_id',
      'chunk_size_ocr_text',
    ]);
  });

  it('shows all params on schemas without an enable_ocr param (text/markdown)', () => {
    const s = schema([
      param({ name: 'chunk_size', group: 'chunking' }),
      param({ name: 'chunk_overlap', group: 'chunking' }),
    ]);
    const names = filterParamsByDependencies(s.parameters, {}, s).map(
      p => p.name,
    );
    expect(names).toEqual(['chunk_size', 'chunk_overlap']);
  });
});

describe('applyForcedParams', () => {
  it('returns the input map unchanged when schemaId is not in the forced map', () => {
    const value = { enable_ocr: false, foo: 'bar' };
    expect(applyForcedParams('text_document', value)).toEqual(value);
    expect(applyForcedParams('markdown_document', value)).toEqual(value);
    expect(applyForcedParams('totally_unknown', value)).toEqual(value);
    expect(applyForcedParams(undefined, value)).toEqual(value);
  });

  it('overrides value.enable_ocr=false to true for image_document', () => {
    expect(applyForcedParams('image_document', { enable_ocr: false })).toEqual({
      enable_ocr: true,
    });
  });

  it('overrides value.enable_ocr=false to true for scanned_document', () => {
    expect(
      applyForcedParams('scanned_document', { enable_ocr: false }),
    ).toEqual({ enable_ocr: true });
  });

  it('preserves other keys when overriding forced params', () => {
    expect(
      applyForcedParams('image_document', {
        enable_ocr: false,
        ocr_model_id: 'paddle',
        enable_image_embedding: true,
      }),
    ).toEqual({
      enable_ocr: true,
      ocr_model_id: 'paddle',
      enable_image_embedding: true,
    });
  });

  it('does not mutate the input map', () => {
    const input = { enable_ocr: false };
    applyForcedParams('image_document', input);
    expect(input).toEqual({ enable_ocr: false });
  });

  it('returns enable_ocr=true unchanged when value already has it true (idempotent)', () => {
    expect(applyForcedParams('image_document', { enable_ocr: true })).toEqual({
      enable_ocr: true,
    });
    expect(applyForcedParams('scanned_document', { enable_ocr: true })).toEqual(
      { enable_ocr: true },
    );
  });
});
