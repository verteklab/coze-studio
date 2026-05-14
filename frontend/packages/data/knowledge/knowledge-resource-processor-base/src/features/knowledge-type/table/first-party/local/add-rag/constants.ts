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
 * Rag-mode wizard steps for table local upload. Mirrors {@link TableLocalStep}
 * but collapses CONFIGURATION + PREVIEW + PROCESSING into a single PROGRESS
 * step. The dropped steps depend on `GetTableSchema`, `ValidateTableSchema`,
 * and `CreateDocumentReview` — all of which are pending stubs in ragimpl
 * (see Task 0 of the rag-flow alignment plan). The progress step owns its
 * own CreateDocument call and delegates polling to `<UploadProgressPoll />`.
 *
 * Numeric values are intentional and load-bearing:
 *   - The wizard engine (`UploadConfig<T extends number, R>` in
 *     knowledge-resource-processor-core) compares `currentStep === step.step`
 *     so the step type must be a `number`.
 *   - We reuse the legacy `<TableUpload />` step, which hardcodes
 *     `setCurrentStep(TableLocalStep.CONFIGURATION)` = 1 when the user clicks
 *     Next (see table/first-party/local/add/steps/upload/upload.tsx). Aligning
 *     `PROGRESS = 1` here means that handoff lands on the rag progress step
 *     without needing to fork the upload component.
 *
 *   Note: the legacy upload `onClick` also fires `fetchTableInfo(...)` as a
 *   side-effect; that request hits `GetTableSchema` which is currently a stub
 *   on the rag backend. The request fails async and is logged via
 *   `dataReporter.errorEvent`, but does NOT block navigation — `setCurrentStep`
 *   ran synchronously beforehand, so the rag progress step still mounts.
 */
export enum TableLocalAddRagStep {
  UPLOAD = 0,
  PROGRESS = 1,
}
