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

import { I18n } from '@coze-arch/i18n';

import { type DocumentParameter } from './types';

const PREFIX = 'datasets_createFileModel_rag_param_';

/**
 * Resolves localised label / description / enum option labels for a rag
 * DocumentParameter. Falls back to the raw schema value when a bundle key is
 * missing so a future rag-side param does not render blank before
 * translators catch up.
 *
 * Key namespace: `${PREFIX}<param_name>_label`, `..._desc`, and
 * `..._enum_<value>` for each allowed_values entry.
 *
 * Named `get*` (not `use*`) so it is safe to call inside array `.map()`
 * callbacks without triggering react-hooks/rules-of-hooks lint errors —
 * it does not call any React state or effect hooks internally.
 */
export function getRagParameterI18n(p: DocumentParameter): {
  label: string;
  description: string;
  options: Array<{ value: string; label: string }>;
} {
  return {
    label: i18nWithFallback(`${PREFIX}${p.name}_label`, p.name),
    description: i18nWithFallback(
      `${PREFIX}${p.name}_desc`,
      p.description ?? '',
    ),
    options: (p.allowed_values ?? []).map(v => ({
      value: String(v),
      label: i18nWithFallback(`${PREFIX}${p.name}_enum_${v}`, String(v)),
    })),
  };
}

/**
 * Localised group header label. Empty input passes through (no key lookup).
 *
 * Named `get*` (not `use*`) so it is safe to call inside array `.map()`
 * callbacks without triggering react-hooks/rules-of-hooks lint errors.
 */
export function getRagGroupI18n(groupName: string): string {
  if (!groupName) {
    return '';
  }
  return i18nWithFallback(`${PREFIX}group_${groupName}`, groupName);
}

// Re-export under the canonical `use*` names for any callers that import
// the hook-style names (e.g. top-level component bodies where hooks are
// legal, or the unit tests which were written against the original names).
export {
  getRagParameterI18n as useRagParameterI18n,
  getRagGroupI18n as useRagGroupI18n,
};

/**
 * I18n.t returns the key itself when missing (a known loader quirk). Detect
 * that and substitute the provided fallback so we never show a raw
 * `datasets_createFileModel_...` key to the user.
 */
function i18nWithFallback(key: string, fallback: string): string {
  const v = I18n.t(key);
  return v && v !== key ? v : fallback;
}
