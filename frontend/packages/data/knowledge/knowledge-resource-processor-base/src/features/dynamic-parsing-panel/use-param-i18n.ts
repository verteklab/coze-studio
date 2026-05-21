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
 */
export function useRagParameterI18n(p: DocumentParameter): {
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
 */
export function useRagGroupI18n(groupName: string): string {
  if (!groupName) {
    return '';
  }
  return i18nWithFallback(`${PREFIX}group_${groupName}`, groupName);
}

/**
 * I18n.t returns the key itself when missing (a known loader quirk). Detect
 * that and substitute the provided fallback so we never show a raw
 * `datasets_createFileModel_...` key to the user.
 */
function i18nWithFallback(key: string, fallback: string): string {
  const v = I18n.t(key);
  return v && v !== key ? v : fallback;
}
