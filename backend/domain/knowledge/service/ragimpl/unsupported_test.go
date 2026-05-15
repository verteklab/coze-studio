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

package ragimpl

import (
	"context"
	"strings"
	"testing"

	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
)

// TestUnsupported_AllReturnFeaturePending verifies every bucket-B stub
// returns a non-nil error whose message contains a roadmap pointer. The
// guarantee here is intentionally narrow: contract-level wiring (501 code,
// caller-visible roadmap anchor) — not the body of each future feature.
func TestUnsupported_AllReturnFeaturePending(t *testing.T) {
	i := &Impl{}
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		// UpdateDocument moved to document.go (R2-H wired it to rag's
		// /documents/{doc_id}/update); it is no longer a bucket-B stub.
		{"ResegmentDocument", func() error { _, e := i.ResegmentDocument(ctx, &service.ResegmentDocumentRequest{}); return e }},
		{"CreateSlice", func() error { _, e := i.CreateSlice(ctx, &service.CreateSliceRequest{}); return e }},
		{"UpdateSlice", func() error { return i.UpdateSlice(ctx, &service.UpdateSliceRequest{}) }},
		{"DeleteSlice", func() error { return i.DeleteSlice(ctx, &service.DeleteSliceRequest{}) }},
		{"ListSlice", func() error { _, e := i.ListSlice(ctx, &service.ListSliceRequest{}); return e }},
		{"GetSlice", func() error { _, e := i.GetSlice(ctx, &service.GetSliceRequest{}); return e }},
		{"MGetSlice", func() error { _, e := i.MGetSlice(ctx, &service.MGetSliceRequest{}); return e }},
		{"ListPhotoSlice", func() error { _, e := i.ListPhotoSlice(ctx, &service.ListPhotoSliceRequest{}); return e }},
		{"GetAlterTableSchema", func() error { _, e := i.GetAlterTableSchema(ctx, &service.AlterTableSchemaRequest{}); return e }},
		{"ValidateTableSchema", func() error { _, e := i.ValidateTableSchema(ctx, &service.ValidateTableSchemaRequest{}); return e }},
		{"GetDocumentTableInfo", func() error { _, e := i.GetDocumentTableInfo(ctx, &service.GetDocumentTableInfoRequest{}); return e }},
		{"GetImportDataTableSchema", func() error {
			_, e := i.GetImportDataTableSchema(ctx, &service.ImportDataTableSchemaRequest{})
			return e
		}},
		{"ExtractPhotoCaption", func() error { _, e := i.ExtractPhotoCaption(ctx, &service.ExtractPhotoCaptionRequest{}); return e }},
		{"CreateDocumentReview", func() error { _, e := i.CreateDocumentReview(ctx, &service.CreateDocumentReviewRequest{}); return e }},
		{"MGetDocumentReview", func() error { _, e := i.MGetDocumentReview(ctx, &service.MGetDocumentReviewRequest{}); return e }},
		{"SaveDocumentReview", func() error { return i.SaveDocumentReview(ctx, &service.SaveDocumentReviewRequest{}) }},
		{"CopyKnowledge", func() error { _, e := i.CopyKnowledge(ctx, &service.CopyKnowledgeRequest{}); return e }},
		{"MoveKnowledgeToLibrary", func() error { return i.MoveKnowledgeToLibrary(ctx, &service.MoveKnowledgeToLibraryRequest{}) }},
	}

	// Bucket-B count drops by one each time a stub is replaced with a real
	// implementation. R2-H wired UpdateDocument; the remaining 18 are still
	// pending (re-segmentation, manual-chunk CRUD, table ingestion, photo
	// caption, document review, KB copy/move).
	if want, got := 18, len(cases); want != got {
		t.Fatalf("expected %d bucket-B methods covered, got %d", want, got)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s: expected error, got nil", c.name)
			}
			if !strings.Contains(err.Error(), "roadmap") {
				t.Fatalf("%s: error missing roadmap pointer: %v", c.name, err)
			}
			if !strings.Contains(err.Error(), c.name) {
				t.Fatalf("%s: error missing method name: %v", c.name, err)
			}
		})
	}
}
