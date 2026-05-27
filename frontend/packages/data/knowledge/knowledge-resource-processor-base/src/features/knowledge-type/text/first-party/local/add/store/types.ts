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

import { type ILevelSegmentsSlice } from '@coze-data/knowledge-stores';

import {
  type UploadTextStore,
  type UploadTextState,
  type LocalTextCustomResegmentState,
  type LocalTextCustomResegmentAction,
} from '@/features/knowledge-type/text/interface';

import { type TextLocalAddUpdateStep } from '../constants';
import { type IDocReviewSlice, type IDocReviewState } from './doc-review-slice';

/**
 * Phase 3b dynamic upload form state. Written by the rag-mode segment step
 * (add-rag/steps/segment) and read by the rag-mode progress step
 * (add-rag/steps/progress) when firing `KnowledgeApi.CreateDocument`. The
 * value is the final JSON string the backend forwards verbatim to rag's
 * POST /documents document_options field (with the reserved
 * `_source_modality` key inlined when the user picked a non-default schema).
 *
 * Empty string is the "use defaults" signal — the legacy wizard never writes
 * this field, and the rag-mode wizard writes it only on Next from the
 * segment step.
 */
export interface IDocumentOptionsState {
  documentOptions: string;
}
export interface IDocumentOptionsSlice {
  setDocumentOptions: (value: string) => void;
}

export type UploadTextLocalAddUpdateState =
  UploadTextState<TextLocalAddUpdateStep> &
    LocalTextCustomResegmentState &
    IDocReviewState &
    IDocumentOptionsState;

export type UploadTextLocalAddUpdateStore =
  UploadTextStore<TextLocalAddUpdateStep> &
    LocalTextCustomResegmentState &
    LocalTextCustomResegmentAction &
    IDocReviewSlice &
    ILevelSegmentsSlice &
    IDocumentOptionsState &
    IDocumentOptionsSlice;
