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

// Rag wizard reuses the legacy upload step as-is — no rag-specific behaviour
// is needed during file selection; the only divergence is the post-upload
// flow, handled by the new `<TextProgress />`.
export { TextUpload } from '../../add/steps';
export { TextProgress } from './progress';
