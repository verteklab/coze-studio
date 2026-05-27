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
		// UpdateDocument was wired to rag in R2-H (document.go).
		// The seven manual-chunk methods (Create/Update/Delete/List/Get/MGet/
		// ListPhoto Slice) were wired to rag in R2-G (slice.go) -- they
		// are no longer bucket-B stubs.
		{"ResegmentDocument", func() error { _, e := i.ResegmentDocument(ctx, &service.ResegmentDocumentRequest{}); return e }},
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

	// Bucket-B count drops as stubs are replaced. R2-H wired UpdateDocument
	// (18 remaining). R2-G wired the 7 manual-chunk methods; 11 stubs remain
	// pending (re-segmentation, table ingestion, photo caption, document
	// review, KB copy/move).
	if want, got := 11, len(cases); want != got {
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
