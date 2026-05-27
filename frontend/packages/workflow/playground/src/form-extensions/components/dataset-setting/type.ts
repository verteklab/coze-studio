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

export enum Strategy {
  Semantic = 0,
  Hybird = 1,
  FullText = 20,
}

export interface QueryImage {
  image_base64?: string;
  image_ref?: string;
}

export interface DataSetInfo {
  top_k?: number;
  strategy?: Strategy;

  // query_strategy 4 booleans (wire-level rag keys)
  rewrite?: boolean;
  expansion?: boolean;
  multi_query?: boolean;
  enable_rerank?: boolean;

  // new top-level rag fields
  query_image?: QueryImage;
  query_mode?: 'text_input' | 'image_input' | 'mixed_input';
  target_chunk_types?: Array<'text_chunk' | 'image_chunk'>;
  filters?: Record<string, unknown>;
  retrievers?: Array<'dense' | 'bm25' | 'image_vector'>;
  fusion_policy?: Record<string, unknown>;
  retriever_params?: Record<string, unknown>;
}
