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
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// TestCreateChunk locks the wire shape of CreateChunk's request and the
// decode of the rag ChunkDetail response.
func TestCreateChunk(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/knowledgebases/kb-1/documents/doc-1/chunks"
		if r.URL.Path != wantPath || r.Method != http.MethodPost {
			t.Fatalf("got %s %s, want POST %s", r.Method, r.URL.Path, wantPath)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want t1", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write(envelopeBody(t, contract.Chunk{
			ChunkID:   "chunk-9",
			DocID:     "doc-1",
			KBID:      "kb-1",
			DocName:   "design.pdf",
			ChunkType: "text_chunk",
			Content:   "hello",
			CharCount: 5,
			ByteCount: 5,
			Status:    "ready",
		}))
	}))
	seq := 12
	got, err := c.CreateChunk(context.Background(), "t1", "kb-1", "doc-1", &contract.CreateChunkRequest{
		ChunkType: "text_chunk",
		Content:   "hello",
		Position:  &contract.ChunkPosition{SequenceIndex: &seq},
		Metadata:  map[string]any{"source": "manual"},
	})
	if err != nil {
		t.Fatalf("CreateChunk: %v", err)
	}
	if got.ChunkID != "chunk-9" || got.DocID != "doc-1" || got.ChunkType != "text_chunk" || got.Status != "ready" {
		t.Errorf("decoded = %+v", got)
	}
	if gotBody["chunk_type"] != "text_chunk" {
		t.Errorf("body.chunk_type = %v", gotBody["chunk_type"])
	}
	if gotBody["content"] != "hello" {
		t.Errorf("body.content = %v", gotBody["content"])
	}
	// position.sequence_index is a nested object on the wire.
	pos, _ := gotBody["position"].(map[string]any)
	if pos == nil || pos["sequence_index"] != float64(12) {
		t.Errorf("body.position = %v", gotBody["position"])
	}
}

// TestCreateChunk_RagError surfaces rag's typed envelope error so callers
// can branch on code (here 40901, doc-not-ready) without parsing strings.
func TestCreateChunk_RagError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{"code": 40901, "message": "document is not ready"},
		})
	}))
	_, err := c.CreateChunk(context.Background(), "t1", "kb-1", "doc-1", &contract.CreateChunkRequest{ChunkType: "text_chunk", Content: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("error didn't propagate rag message: %v", err)
	}
}

func TestUpdateChunk(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/knowledgebases/kb-1/documents/doc-1/chunks/chunk-9/update"
		if r.URL.Path != wantPath || r.Method != http.MethodPost {
			t.Fatalf("got %s %s, want POST %s", r.Method, r.URL.Path, wantPath)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write(envelopeBody(t, contract.Chunk{
			ChunkID: "chunk-9", DocID: "doc-1", KBID: "kb-1",
			ChunkType: "text_chunk", Content: "edited", CharCount: 6, ByteCount: 6, Status: "ready",
		}))
	}))
	newContent := "edited"
	got, err := c.UpdateChunk(context.Background(), "t1", "kb-1", "doc-1", "chunk-9", &contract.UpdateChunkRequest{
		Content: &newContent,
	})
	if err != nil {
		t.Fatalf("UpdateChunk: %v", err)
	}
	if got.Content != "edited" {
		t.Errorf("got.Content = %q", got.Content)
	}
	if gotBody["content"] != "edited" {
		t.Errorf("body.content = %v", gotBody["content"])
	}
	// Pointer-nil fields must be absent so rag's exclude_unset distinguishes
	// "leave alone" from "explicit clear".
	for _, k := range []string{"metadata", "image"} {
		if _, present := gotBody[k]; present {
			t.Errorf("body.%s should be absent, got %v", k, gotBody[k])
		}
	}
}

func TestDeleteChunk(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/knowledgebases/kb-1/documents/doc-1/chunks/chunk-9/delete"
		if r.URL.Path != wantPath || r.Method != http.MethodPost {
			t.Fatalf("got %s %s, want POST %s", r.Method, r.URL.Path, wantPath)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q", got)
		}
		_, _ = w.Write(envelopeBody(t, contract.DeleteChunkResponse{Deleted: true}))
	}))
	if err := c.DeleteChunk(context.Background(), "t1", "kb-1", "doc-1", "chunk-9"); err != nil {
		t.Fatalf("DeleteChunk: %v", err)
	}
}

// TestDeleteChunk_NotFound asserts that rag's 40404 surfaces as a Go error
// so callers can map it to ErrKnowledgeNotFound.
func TestDeleteChunk_NotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{"code": 40404, "message": "chunk not found"},
		})
	}))
	err := c.DeleteChunk(context.Background(), "t1", "kb-1", "doc-1", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "chunk not found") {
		t.Fatalf("error didn't propagate rag message: %v", err)
	}
}

func TestListChunks_QueryString(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/knowledgebases/kb-1/documents/doc-1/chunks") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("page") != "2" || q.Get("page_size") != "20" || q.Get("keyword") != "hello" || q.Get("chunk_type") != "text_chunk" {
			t.Fatalf("query = %v", q)
		}
		_, _ = w.Write(envelopeBody(t, contract.ListChunksResponse{
			Items:    []contract.Chunk{{ChunkID: "c-1", KBID: "kb-1", DocID: "doc-1", ChunkType: "text_chunk"}},
			Total:    1,
			Page:     2,
			PageSize: 20,
		}))
	}))
	got, err := c.ListChunks(context.Background(), "t1", "kb-1", "doc-1", &contract.ListChunksQuery{
		Page: 2, PageSize: 20, Keyword: "hello", ChunkType: "text_chunk",
	})
	if err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ChunkID != "c-1" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestGetChunk(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/knowledgebases/kb-1/chunks/chunk-9"
		if r.URL.Path != wantPath || r.Method != http.MethodGet {
			t.Fatalf("got %s %s, want GET %s", r.Method, r.URL.Path, wantPath)
		}
		_, _ = w.Write(envelopeBody(t, contract.Chunk{
			ChunkID: "chunk-9", DocID: "doc-1", KBID: "kb-1", ChunkType: "text_chunk", Content: "hi", Status: "ready",
		}))
	}))
	got, err := c.GetChunk(context.Background(), "t1", "kb-1", "chunk-9")
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if got.ChunkID != "chunk-9" || got.DocID != "doc-1" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestMGetChunks(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/knowledgebases/kb-1/chunks:mget"
		if r.URL.Path != wantPath || r.Method != http.MethodPost {
			t.Fatalf("got %s %s, want POST %s", r.Method, r.URL.Path, wantPath)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write(envelopeBody(t, contract.MGetChunksResponse{
			Items: []contract.MGetChunksItem{
				{Chunk: contract.Chunk{ChunkID: "c-1", KBID: "kb-1", ChunkType: "text_chunk", Status: "ready"}},
				{Chunk: contract.Chunk{ChunkID: "c-2"}, Deleted: true},
			},
		}))
	}))
	got, err := c.MGetChunks(context.Background(), "t1", "kb-1", []string{"c-1", "c-2"})
	if err != nil {
		t.Fatalf("MGetChunks: %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].ChunkID != "c-1" || !got.Items[1].Deleted {
		t.Errorf("decoded = %+v", got)
	}
	// chunk_ids body is a JSON array; encoding/json decodes into []any with strings.
	ids, _ := gotBody["chunk_ids"].([]any)
	if len(ids) != 2 || ids[0] != "c-1" || ids[1] != "c-2" {
		t.Errorf("body.chunk_ids = %v", ids)
	}
}

func TestListChunksByKB_DocIDsCommaJoined(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/knowledgebases/kb-1/chunks") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("chunk_type"); got != "image_chunk" {
			t.Errorf("query.chunk_type = %q", got)
		}
		if got := q.Get("doc_ids"); got != "a,b,c" {
			t.Errorf("query.doc_ids = %q, want comma-joined", got)
		}
		_, _ = w.Write(envelopeBody(t, contract.ListChunksResponse{Total: 0}))
	}))
	if _, err := c.ListChunksByKB(context.Background(), "t1", "kb-1", &contract.ListChunksByKBQuery{
		ChunkType: "image_chunk",
		DocIDs:    []string{"a", "b", "c"},
	}); err != nil {
		t.Fatalf("ListChunksByKB: %v", err)
	}
}
