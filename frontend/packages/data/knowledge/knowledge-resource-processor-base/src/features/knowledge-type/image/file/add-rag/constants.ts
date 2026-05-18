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
 * Rag-mode wizard steps for image file upload. Mirrors
 * {@link ImageFileAddStep} but drops the legacy ANNOTATION step (which
 * depended on the pending `ExtractPhotoCaption` stub) in favour of a
 * schema-driven SEGMENT_CLEANER (Phase 3b) that lets the user pick between
 * the `image_document` and `scanned_document` rag schemas and configure
 * their parameters.
 *
 * Numeric values are intentional and load-bearing:
 *   - The wizard engine (`UploadConfig<T extends number, R>` in
 *     knowledge-resource-processor-core) compares `currentStep === step.step`
 *     so the step type must be a `number`.
 *   - We reuse the legacy `<ImageUpload />` step, which hardcodes
 *     `setCurrentStep(ImageFileAddStep.Annotation)` = 1 on Next (see
 *     image/file/steps/upload/index.tsx). Aligning `SEGMENT_CLEANER = 1`
 *     here lands that handoff on our new segment step without forking
 *     <ImageUpload />.
 *   - The new <ImageRagSegment /> step's Next button sets step to PROGRESS
 *     directly, so PROGRESS just needs to be the next free number after
 *     SEGMENT_CLEANER.
 */
export enum ImageFileAddRagStep {
  UPLOAD = 0,
  SEGMENT_CLEANER = 1,
  PROGRESS = 2,
}
