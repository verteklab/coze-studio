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

// Bucket-B stubs: methods on the wider service.Knowledge interface that the
// rag backend does not yet support. Each returns ErrRagFeaturePendingCode with
// a roadmap pointer so callers (and operators reading logs) know where the
// feature is tracked. Once rag implements a capability, the stub here is
// replaced with a real implementation in the corresponding file.

package ragimpl

import (
	"context"
	"fmt"

	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// pending builds the 501-style error returned by every bucket-B stub. The
// message embeds the calling method and a stable roadmap anchor so log
// readers can jump straight to the design doc that owns the feature.
func pending(method, roadmapAnchor string) error {
	return errorx.New(errno.ErrRagFeaturePendingCode, errorx.KV("msg",
		fmt.Sprintf("%s is pending rag support (roadmap: rag/docs/notes/roadmap.md#%s)", method, roadmapAnchor)))
}

// --- Document metadata / lifecycle ---------------------------------------
//
// UpdateDocument now ships in document.go (R2-H: wire UpdateDocument to rag's
// /documents/{doc_id}/update). The stub previously here returned
// ErrRagFeaturePendingCode; the real implementation lives alongside the rest
// of the document lifecycle methods.

func (i *Impl) ResegmentDocument(_ context.Context, _ *service.ResegmentDocumentRequest) (*service.ResegmentDocumentResponse, error) {
	return nil, pending("ResegmentDocument", "re-segmentation")
}

// --- Manual slice (chunk) CRUD -------------------------------------------
//
// R2-G wired the seven manual-chunk methods (CreateSlice / UpdateSlice /
// DeleteSlice / ListSlice / GetSlice / MGetSlice / ListPhotoSlice) to rag's
// /chunks endpoints; real implementations live in slice.go. The stubs that
// previously sat here have been removed.

// --- Table / sheet ingestion ---------------------------------------------

func (i *Impl) GetAlterTableSchema(_ context.Context, _ *service.AlterTableSchemaRequest) (*service.TableSchemaResponse, error) {
	return nil, pending("GetAlterTableSchema", "table-sheet-ingestion")
}

func (i *Impl) ValidateTableSchema(_ context.Context, _ *service.ValidateTableSchemaRequest) (*service.ValidateTableSchemaResponse, error) {
	return nil, pending("ValidateTableSchema", "table-sheet-ingestion")
}

func (i *Impl) GetDocumentTableInfo(_ context.Context, _ *service.GetDocumentTableInfoRequest) (*service.GetDocumentTableInfoResponse, error) {
	return nil, pending("GetDocumentTableInfo", "table-sheet-ingestion")
}

func (i *Impl) GetImportDataTableSchema(_ context.Context, _ *service.ImportDataTableSchemaRequest) (*service.TableSchemaResponse, error) {
	return nil, pending("GetImportDataTableSchema", "table-sheet-ingestion")
}

// --- Photo caption extraction --------------------------------------------

func (i *Impl) ExtractPhotoCaption(_ context.Context, _ *service.ExtractPhotoCaptionRequest) (*service.ExtractPhotoCaptionResponse, error) {
	return nil, pending("ExtractPhotoCaption", "photo-caption-extraction")
}

// --- Document review workflow --------------------------------------------

func (i *Impl) CreateDocumentReview(_ context.Context, _ *service.CreateDocumentReviewRequest) (*service.CreateDocumentReviewResponse, error) {
	return nil, pending("CreateDocumentReview", "document-review-workflow")
}

func (i *Impl) MGetDocumentReview(_ context.Context, _ *service.MGetDocumentReviewRequest) (*service.MGetDocumentReviewResponse, error) {
	return nil, pending("MGetDocumentReview", "document-review-workflow")
}

func (i *Impl) SaveDocumentReview(_ context.Context, _ *service.SaveDocumentReviewRequest) error {
	return pending("SaveDocumentReview", "document-review-workflow")
}

// --- Knowledge base copy / move ------------------------------------------

func (i *Impl) CopyKnowledge(_ context.Context, _ *service.CopyKnowledgeRequest) (*service.CopyKnowledgeResponse, error) {
	return nil, pending("CopyKnowledge", "kb-copy-move")
}

func (i *Impl) MoveKnowledgeToLibrary(_ context.Context, _ *service.MoveKnowledgeToLibraryRequest) error {
	return pending("MoveKnowledgeToLibrary", "kb-copy-move")
}
