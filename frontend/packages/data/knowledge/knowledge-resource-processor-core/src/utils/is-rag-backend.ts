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

import { type Dataset } from '@coze-arch/bot-api/knowledge';

/**
 * Returns true iff the KB is served by the standalone rag service. Any
 * other value — legacy, undefined, null, or an unknown string — falls back
 * to legacy semantics. The frontend uses this to route the upload wizard.
 * See docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md.
 */
export const isRagBackend = (
  kb: Pick<Dataset, 'backend'> | undefined | null,
): boolean => kb?.backend === 'rag';
