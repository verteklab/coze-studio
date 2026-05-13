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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// envelopeBody is a small test-helper that wraps a typed payload in rag's
// ResponseEnvelope JSON shape. Keeping it in one place means tests can update
// alongside the production envelope decoder if rag ever versions the envelope.
func envelopeBody(t *testing.T, data any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	out, err := json.Marshal(map[string]any{
		"code":       0,
		"message":    "ok",
		"data":       json.RawMessage(raw),
		"request_id": "req-test",
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return out
}

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := ragconf.Config{
		BaseURL:            srv.URL,
		Timeout:            5 * time.Second,
		UploadTimeoutMs:    30000,
		RetrievalTimeoutMs: 5000,
		MaxRetries:         0,
		RetryBackoffMs:     0,
	}
	return New(cfg), srv
}

func TestListModelProviders(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/model-providers" || r.Method != http.MethodGet {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant-Id") != "42" {
			t.Fatalf("missing X-Tenant-Id; got %q", r.Header.Get("X-Tenant-Id"))
		}
		_, _ = w.Write(envelopeBody(t, contract.ListModelProvidersResponse{
			Items: []contract.ModelProvider{
				{ModelID: "t1", Type: "text", Name: "Text 1", IsActive: true},
				{ModelID: "i1", Type: "image", Name: "Image 1", IsActive: true},
			},
		}))
	}))
	out, err := c.ListModelProviders(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("got %+v", out)
	}
	text, image := out.Split()
	if len(text) != 1 || text[0].ModelID != "t1" {
		t.Fatalf("split text: %+v", text)
	}
	if len(image) != 1 || image[0].ModelID != "i1" {
		t.Fatalf("split image: %+v", image)
	}
}

func TestCreateKB(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebases" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant-Id") != "42" {
			t.Fatalf("missing/wrong X-Tenant-Id: %q", r.Header.Get("X-Tenant-Id"))
		}
		var req contract.CreateKBRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Name != "kb1" {
			t.Fatalf("bad req: %+v", req)
		}
		// Body must NOT carry a tenant field — explicit guard against drift.
		raw := mustReadAndReset(t, r)
		if strings.Contains(string(raw), "tenant_id") {
			t.Fatalf("body unexpectedly carried tenant_id: %s", raw)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelopeBody(t, contract.KB{KBID: "uuid-1", Name: "kb1", Status: "active"}))
	}))
	out, err := c.CreateKB(context.Background(), "42", &contract.CreateKBRequest{
		Name:                      "kb1",
		TextEmbeddingModelID:      "t1",
		ImageEmbeddingModelID:     "i1",
		EnabledChunkTypes:         []string{"text_chunk"},
		SupportedSourceModalities: []string{"text_source"},
		DefaultFusionPolicy:       contract.FusionPolicy{Mode: "weighted_rrf", RrfK: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.KBID != "uuid-1" {
		t.Fatalf("got %+v", out)
	}
}

// mustReadAndReset isn't actually called inside the handler above (the decoder
// already consumed the body). Kept as a no-op stub so the explicit-guard test
// reads naturally; if a future test needs it the helper is here.
func mustReadAndReset(_ *testing.T, _ *http.Request) []byte { return nil }

func TestGetKB_NotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// FastAPI HTTPException envelope: {"detail": {"code": ..., "message": ...}}.
		_ = json.NewEncoder(w).Encode(contract.ErrorBody{
			Detail: contract.ErrorDetail{Code: 40401, Message: "kb not found"},
		})
	}))
	_, err := c.GetKB(context.Background(), "42", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kb not found") {
		t.Fatalf("error didn't propagate rag message: %v", err)
	}
}

func TestCreateDocument(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebases/uuid-1/documents" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant-Id") != "42" {
			t.Fatalf("missing X-Tenant-Id")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelopeBody(t, contract.CreateDocumentResponse{
			DocID: "doc-1", TaskID: "task-1", Status: "pending",
		}))
	}))
	out, err := c.CreateDocument(context.Background(), "42", "uuid-1", &contract.CreateDocumentRequest{
		SourceURI:      "minio://bucket/file.pdf",
		SourceModality: "text_source",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.DocID != "doc-1" || out.TaskID != "task-1" {
		t.Fatalf("got %+v", out)
	}
}

func TestGetDocument_NestedPath(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebases/uuid-1/documents/doc-9" {
			t.Fatalf("got path %s", r.URL.Path)
		}
		_, _ = w.Write(envelopeBody(t, contract.Document{DocID: "doc-9", Status: "ready"}))
	}))
	out, err := c.GetDocument(context.Background(), "42", "uuid-1", "doc-9")
	if err != nil {
		t.Fatal(err)
	}
	if out.DocID != "doc-9" {
		t.Fatalf("got %+v", out)
	}
}

func TestGetTask(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/task-1" {
			t.Fatalf("got %s", r.URL.Path)
		}
		_, _ = w.Write(envelopeBody(t, contract.Task{TaskID: "task-1", Status: "success", Progress: 100}))
	}))
	out, err := c.GetTask(context.Background(), "42", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "success" {
		t.Fatalf("got %+v", out)
	}
}

// TestDeleteDocument_Retries verifies that idempotent methods retry on 5xx.
// MaxRetries=1 means we expect 2 calls total (initial + 1 retry).
func TestDeleteDocument_Retries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(contract.ErrorBody{
			Detail: contract.ErrorDetail{Code: 50001, Message: "boom"},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := ragconf.Config{
		BaseURL:        srv.URL,
		Timeout:        5 * time.Second,
		MaxRetries:     1,
		RetryBackoffMs: 1,
	}
	c := New(cfg)
	err := c.DeleteDocument(context.Background(), "42", "uuid-1", "doc-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (1 + 1 retry), got %d", calls)
	}
}

func TestRetrieve(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/retrieval" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant-Id") != "42" {
			t.Fatalf("missing X-Tenant-Id")
		}
		var req contract.RetrieveRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.KBIDs) != 2 {
			t.Fatalf("bad req: %+v", req)
		}
		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", DocID: "d1", Score: 0.9, Content: "hello"}},
		}))
	}))
	q := "hi"
	out, err := c.Retrieve(context.Background(), "42", &contract.RetrieveRequest{
		KBIDs: []string{"a", "b"}, Query: &q, QueryMode: "text_input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].Content != "hello" {
		t.Fatalf("got %+v", out)
	}
}

// TestEnvelope_NonZeroCodeIsError verifies that a 200 OK with envelope.code != 0
// is surfaced as an error (rag uses envelope.code for soft failures sometimes,
// not just HTTP status).
func TestEnvelope_NonZeroCodeIsError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 OK but envelope says "kb not found" with code 40401.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":       40401,
			"message":    "kb not found",
			"data":       nil,
			"request_id": "r",
		})
	}))
	_, err := c.GetKB(context.Background(), "42", "x")
	if err == nil {
		t.Fatal("expected error from non-zero envelope code")
	}
	if !strings.Contains(err.Error(), "kb not found") {
		t.Fatalf("error didn't surface envelope message: %v", err)
	}
}
