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

/**
 * Rag-mode wizard steps for text local upload. Mirrors
 * {@link TextLocalAddUpdateStep} but drops SEGMENT_PREVIEW because rag has
 * no document-review workflow.
 *
 * Phase 3 amendment: SEGMENT_CLEANER is back. The original RAG wizard
 * stripped it on the assumption that "chunking is locked at KB-creation
 * time," but rag's `POST /documents` actually accepts per-document
 * chunk_size / chunk_overlap / document_options (verified against
 * rag's openapi schemas). Without this step the user could not set OCR,
 * table extraction, or chunk size per document.
 *
 * Numeric values are intentional and load-bearing:
 *   - The wizard engine (`UploadConfig<T extends number, R>` in
 *     knowledge-resource-processor-core) compares `currentStep === step.step`
 *     so the step type must be a `number`.
 *   - We reuse the legacy `<TextUpload />` step, which hardcodes
 *     `setCurrentStep(TextLocalAddUpdateStep.SEGMENT_CLEANER)` = 1 on Next;
 *     aligning `SEGMENT_CLEANER = 1` here lands that handoff on our
 *     segment step without forking <TextUpload />.
 *   - We also reuse the legacy `<TextSegment />` step, whose Next button
 *     hardcodes `setCurrentStep(TextLocalAddUpdateStep.SEGMENT_PREVIEW)` = 2;
 *     aligning `PROGRESS = 2` here means that handoff lands directly on
 *     PROGRESS (skipping the missing review step) without forking
 *     <TextSegment />. This is the Phase-4-decision "skip review" wiring.
 */
export enum TextLocalAddRagStep {
  UPLOAD = 0,
  SEGMENT_CLEANER = 1,
  PROGRESS = 2,
}
