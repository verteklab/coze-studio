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
