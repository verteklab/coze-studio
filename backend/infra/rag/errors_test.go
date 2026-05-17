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

package rag

import (
	"errors"
	"strings"
	"testing"

	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// codeOf extracts the coze status code from a MapRagError-returned error so
// tests can assert the precise mapping (not just message preservation).
func codeOf(t *testing.T, err error) int32 {
	t.Helper()
	var se errorx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected errorx.StatusError, got %T: %v", err, err)
	}
	return se.Code()
}

func TestMapRagError_InvalidParam(t *testing.T) {
	err := MapRagError(400, 40001, "missing field")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing field") {
		t.Fatalf("err missing message: %v", err)
	}
}

func TestMapRagError_KBNotFound(t *testing.T) {
	err := MapRagError(404, 40401, "kb not found")
	if !strings.Contains(err.Error(), "kb not found") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_DocNotFound(t *testing.T) {
	err := MapRagError(404, 40402, "doc not found")
	if !strings.Contains(err.Error(), "doc not found") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_Conflict(t *testing.T) {
	err := MapRagError(409, 40901, "duplicate name")
	if !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_Fallback(t *testing.T) {
	err := MapRagError(500, 50001, "internal")
	if !strings.Contains(err.Error(), "rag=50001") {
		t.Fatalf("got %v", err)
	}
}

// TestMapRagError_DocumentStatusConflict locks the 40902 mapping. Before this
// fix, "document status doesn't allow this operation" surfaced as the coze
// code 105000010 "knowledge name duplicate", which was nonsense for the
// caller. The fix routes 40902 to ErrKnowledgeDocNotReadyCode.
func TestMapRagError_DocumentStatusConflict(t *testing.T) {
	err := MapRagError(409, 40902, "doc not ready")
	if got, want := codeOf(t, err), int32(errno.ErrKnowledgeDocNotReadyCode); got != want {
		t.Fatalf("40902 mapping: got code %d, want %d", got, want)
	}
	if !strings.Contains(err.Error(), "rag 40902") {
		t.Fatalf("err must preserve rag code in message: %v", err)
	}
}

// TestMapRagError_KBNameConflict_StaysDuplicate ensures the 40901 case still
// uses ErrKnowledgeDuplicateCode (it was previously the only 409 we mapped,
// and the explicit switch must preserve that behavior).
func TestMapRagError_KBNameConflict_StaysDuplicate(t *testing.T) {
	err := MapRagError(409, 40901, "name taken")
	if got, want := codeOf(t, err), int32(errno.ErrKnowledgeDuplicateCode); got != want {
		t.Fatalf("40901 mapping: got code %d, want %d", got, want)
	}
}

// TestMapRagError_ContentTypeKBConflict checks 40904 is a param error, not a
// duplicate. Rag's CONTENT_TYPE_KB_CONFLICT means the request asked for a
// chunk_type / source_modality the KB wasn't configured for -- a capability
// mismatch.
func TestMapRagError_ContentTypeKBConflict(t *testing.T) {
	err := MapRagError(409, 40904, "image_chunk not enabled")
	if got, want := codeOf(t, err), int32(errno.ErrKnowledgeInvalidParamCode); got != want {
		t.Fatalf("40904 mapping: got code %d, want %d", got, want)
	}
}
