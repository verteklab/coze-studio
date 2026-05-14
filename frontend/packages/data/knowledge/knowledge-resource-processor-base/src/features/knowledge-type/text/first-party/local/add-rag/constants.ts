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
 * {@link TextLocalAddUpdateStep} but drops SEGMENT_CLEANER + SEGMENT_PREVIEW
 * since rag has no document-review workflow and chunking is locked at
 * KB-creation time.
 *
 * Numeric values are intentional and load-bearing:
 *   - The wizard engine (`UploadConfig<T extends number, R>` in
 *     knowledge-resource-processor-core) compares `currentStep === step.step`
 *     so the step type must be a `number`.
 *   - We reuse the legacy `<TextUpload />` step, which hardcodes
 *     `setCurrentStep(TextLocalAddUpdateStep.SEGMENT_CLEANER)` = 1 when the
 *     user clicks Next. Aligning `PROGRESS = 1` here means that handoff lands
 *     on the rag progress step without needing to fork the upload component.
 */
export enum TextLocalAddRagStep {
  UPLOAD = 0,
  PROGRESS = 1,
}
