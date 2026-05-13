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

interface SpanTypeConfig {
  label?: string;
}

/** key: SpanType */
export type SpanTypeConfigMap = Record<number, SpanTypeConfig | undefined>;

interface SpanCategoryConfig {
  label: string;
}

/** key: SpanCategory */
export type SpanCategoryConfigMap = Record<
  number,
  SpanCategoryConfig | undefined
>;

interface SpanStatusConfig {
  /**
   * Lazy getter. Returning a thunk (rather than a precomputed string) avoids
   * the i18n early-evaluation bug: when this module is imported before the
   * zh-CN resource pack is loaded, `I18n.t(...)` would fall back to the
   * English value and freeze that string into the config object permanently.
   * Calling it at render time always sees the up-to-date locale.
   */
  label: () => string;
}

export interface SpanStatusConfigMap {
  [x: number]: SpanStatusConfig | undefined;
}
