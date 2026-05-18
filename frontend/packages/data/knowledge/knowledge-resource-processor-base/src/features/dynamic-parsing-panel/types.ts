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

/**
 * Wire shape for the response of
 * `GET /api/knowledge/rag/document_parameter_schemas`.
 *
 * Mirrors the backend's `ragDocumentParameterSchema` / `ragDocumentParameter`
 * (see backend/application/knowledge/knowledge.go). Pinned at the frontend
 * boundary so a rag schema rename does not silently slip an `any` through
 * the form renderer.
 */
export interface DocumentParameter {
  name: string;
  /** boolean | integer | string | array[string] | ... — drives form-control mapping. */
  type: string;
  /** Free-form grouping label (e.g. "chunking", "ocr"). Used to render headers. */
  group: string;
  required: boolean;
  /** JSON type depends on `type`. Form renderer narrows at use time. */
  default?: unknown;
  /** When non-empty, the renderer switches to a Select / Radio. */
  allowed_values?: unknown[];
  min_value?: number;
  max_value?: number;
  description: string;
  ui_label: string;
  /** "switch" | "number" | "text" | "select" | ... — drives form-control mapping. */
  ui_component: string;
  /** True means hide behind a disclosure until the user opens the panel. */
  advanced: boolean;
}

export interface DocumentParameterSchema {
  schema_id: string;
  description: string;
  /** File extensions this schema accepts (e.g. ["pdf"]). Used for auto-routing. */
  file_types: string[];
  /** Source modalities this schema covers (e.g. ["text_source"]). */
  source_modalities: string[];
  parameters: DocumentParameter[];
}

export interface ListRagDocumentParameterSchemasResponse {
  schemas: DocumentParameterSchema[];
}

/**
 * Form-value bag the dynamic panel emits via onChange. Keys are
 * `parameter.name`, values match `parameter.type`. Reserved key
 * `_source_modality` (added by the schema selector) is consumed by the
 * backend to override rag's auto-routing — frontend must serialise this
 * map to JSON and send it as `CreateDocumentRequest.document_options`.
 */
export type DocumentOptionsValue = Record<string, unknown>;
