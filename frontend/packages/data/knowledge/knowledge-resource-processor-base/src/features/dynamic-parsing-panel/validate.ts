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

import { FORCED_PARAMS_BY_SCHEMA } from './use-schemas';
import {
  type DocumentOptionsValue,
  type DocumentParameter,
  type DocumentParameterSchema,
} from './types';

/**
 * Returns the parameters whose `required: true` declaration is unsatisfied by
 * `value`. A parameter is "missing" when the user hasn't supplied a value and
 * the schema's own default is also empty -- the form panel only displays the
 * default, it doesn't seed it into `value`, so we treat both untouched and
 * empty-string fields as missing.
 *
 * This exists because rag enforces required fields server-side (pydantic 400)
 * and the only required-with-no-default case in the current catalog
 * (scanned_document.ocr_model_id) renders as a free-text input that users can
 * leave blank. Catching it client-side surfaces a fix-it message instead of
 * an opaque "invalid parameter" error after the upload round-trip.
 */
export function findMissingRequired(
  schema: DocumentParameterSchema,
  value: DocumentOptionsValue,
): DocumentParameter[] {
  return schema.parameters.filter(p => p.required && isEmpty(value[p.name], p));
}

/**
 * Merges schema defaults underneath the user's values so the wire payload
 * reflects every value the form displayed (including unchanged defaults).
 * Without this, `formValue` contains only keys the user explicitly touched
 * and rag silently applies its server-side defaults — the wire payload
 * stops being self-describing, which we hit during the post-fix audit
 * after document_options started actually reaching rag (see
 * auto-generated/knowledge/index.ts:172). Audit: capturing the user's
 * visible state on the wire makes later debugging / replay possible
 * without joining against the rag schema version that was live at upload.
 *
 * Defaults with value `undefined` or `null` are skipped (e.g.
 * scanned_document.ocr_model_id default=null) — they carry no information
 * and rag's pydantic would 422 a literal null on a required string field.
 * User-supplied `value` always wins on key collision.
 */
/**
 * Returns the params that should actually render given the current form
 * `value`. Hides widgets whose values would be inert under rag's ingestion
 * validator — today only the OCR dependency chain: when `enable_ocr` is off,
 * everything in the `ocr` and `ocr_text` groups (model id, languages, OCR-
 * text chunking knobs) becomes a no-op (mergeSchemaDefaults strips
 * ocr_model_id from the wire; the rest would either be ignored or 40001).
 * The `enable_ocr` toggle itself stays visible so the user can turn it back
 * on. Schemas without an `enable_ocr` param (text / markdown / docx) are
 * unaffected.
 *
 * Falls back to the schema's declared default when the user hasn't touched
 * `enable_ocr` yet — image_document declares false, scanned_document true,
 * matching their natural defaults.
 */
export function filterParamsByDependencies(
  params: DocumentParameter[],
  value: DocumentOptionsValue,
  schema: DocumentParameterSchema,
): DocumentParameter[] {
  const enableOcrParam = schema.parameters.find(p => p.name === 'enable_ocr');
  if (!enableOcrParam) {
    return params;
  }
  const enableOcr =
    typeof value.enable_ocr === 'boolean'
      ? value.enable_ocr
      : Boolean(enableOcrParam.default);
  if (enableOcr) {
    return params;
  }
  return params.filter(p => {
    if (p.name === 'enable_ocr') {
      return true;
    }
    return p.group !== 'ocr' && p.group !== 'ocr_text';
  });
}

/**
 * Overwrites keys in `value` whose forced override is declared in
 * `FORCED_PARAMS_BY_SCHEMA[schemaId]`. Returns a new object — does not mutate
 * the input. When `schemaId` is unknown or has no forced params, returns the
 * input map unchanged (same reference is fine; callers treat the return as
 * read-only).
 *
 * Force order vs the inverse-OCR mutex in `mergeSchemaDefaults`: must be
 * called BEFORE the mutex. A stale form value with `enable_ocr=false` would
 * otherwise let the mutex strip `ocr_model_id`, and only then would the force
 * flip `enable_ocr` back to true — sending `{enable_ocr:true, ⌀ ocr_model_id}`
 * trips rag's 40001 "ocr_model_id is required when enable_ocr is true".
 */
export function applyForcedParams<T extends Record<string, unknown>>(
  schemaId: string | undefined,
  value: T,
): T {
  const forced = FORCED_PARAMS_BY_SCHEMA[schemaId ?? ''];
  if (!forced) {
    return value;
  }
  const next = { ...value };
  for (const [k, { value: v }] of Object.entries(forced)) {
    next[k] = v;
  }
  return next as T;
}

export function mergeSchemaDefaults(
  schema: DocumentParameterSchema,
  value: DocumentOptionsValue,
): DocumentOptionsValue {
  const defaults: DocumentOptionsValue = {};
  for (const p of schema.parameters) {
    if (p.default !== undefined && p.default !== null) {
      defaults[p.name] = p.default;
    }
  }
  let merged: DocumentOptionsValue = { ...defaults, ...value };

  // Force-apply locked params (e.g. enable_ocr=true for image_document /
  // scanned_document) BEFORE the inverse-OCR mutex below. Ordering matters:
  // if we forced after, a stale enable_ocr=false in `value` would let the
  // mutex strip ocr_model_id, and only then would the force flip enable_ocr
  // back to true — wire ends up `{enable_ocr:true, ⌀ ocr_model_id}` → rag 40001.
  merged = applyForcedParams(schema.schema_id, merged);

  // Rag's ingestion validator enforces a mutex: ocr_model_id may only travel
  // when enable_ocr is true. Sending both `{enable_ocr: false, ocr_model_id}`
  // fires 40001 "ocr_model_id requires enable_ocr=true". image_document
  // declares enable_ocr default=false with a non-required ocr_model_id, and
  // FRONTEND_PARAM_DEFAULTS synthesises a model-id default — without this
  // prune every image-KB upload that left OCR off would hit 40001. (No-op
  // for forced schemas because applyForcedParams just pinned enable_ocr=true
  // above.)
  if (merged.enable_ocr !== true && 'ocr_model_id' in merged) {
    delete merged.ocr_model_id;
  }

  return merged;
}

function isEmpty(v: unknown, p: DocumentParameter): boolean {
  const candidate = v === undefined ? p.default : v;
  if (candidate === undefined || candidate === null) {
    return true;
  }
  if (typeof candidate === 'string') {
    return candidate.trim() === '';
  }
  if (Array.isArray(candidate)) {
    return candidate.length === 0;
  }
  return false;
}
