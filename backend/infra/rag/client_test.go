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
	"testing"
	"time"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

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
		if r.URL.Path != "/model_providers" || r.Method != http.MethodGet {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(contract.ListModelProvidersResponse{
			TextModels:  []contract.ModelProvider{{ID: "t1", Kind: "text"}},
			ImageModels: []contract.ModelProvider{{ID: "i1", Kind: "image"}},
		})
	}))
	out, err := c.ListModelProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TextModels) != 1 || out.TextModels[0].ID != "t1" {
		t.Fatalf("got %+v", out)
	}
}

func TestCreateKB(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledgebases" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var req contract.CreateKBRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TenantID != "42" || req.Name != "kb1" {
			t.Fatalf("bad req: %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.KB{KBID: "uuid-1", Name: "kb1", Status: "active"})
	}))
	out, err := c.CreateKB(context.Background(), &contract.CreateKBRequest{
		TenantID: "42", Name: "kb1",
		TextEmbeddingModelID: "t1", ImageEmbeddingModelID: "i1",
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

func TestGetKB_NotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(contract.ErrorBody{Code: 40401, Message: "kb not found"})
	}))
	_, err := c.GetKB(context.Background(), "42", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateDocument(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledgebases/uuid-1/documents" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(contract.CreateDocumentResponse{DocID: "doc-1", TaskID: "task-1", Status: "pending"})
	}))
	out, err := c.CreateDocument(context.Background(), "uuid-1", &contract.CreateDocumentRequest{
		TenantID:       "42",
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

func TestGetTask(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contract.Task{TaskID: "task-1", Status: "success", Progress: 100})
	}))
	out, err := c.GetTask(context.Background(), "42", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "success" {
		t.Fatalf("got %+v", out)
	}
}

func TestDeleteDocument_NotRetried(t *testing.T) {
	// Note: DeleteDocument IS idempotent (DELETE method), so it WILL retry.
	// This test verifies the retry happens 1+MaxRetries times. We use MaxRetries=1.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(contract.ErrorBody{Code: 50001, Message: "boom"})
	}))
	t.Cleanup(srv.Close)
	cfg := ragconf.Config{
		BaseURL:        srv.URL,
		Timeout:        5 * time.Second,
		MaxRetries:     1,
		RetryBackoffMs: 1,
	}
	c := New(cfg)
	err := c.DeleteDocument(context.Background(), "42", "doc-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (1 + 1 retry), got %d", calls)
	}
}

func TestRetrieve(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var req contract.RetrieveRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TenantID != "42" || len(req.KBIDs) != 2 {
			t.Fatalf("bad req: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(contract.RetrieveResponse{
			Hits: []contract.RetrieveHit{{ChunkID: "c1", DocID: "d1", Score: 0.9, Content: "hello"}},
		})
	}))
	out, err := c.Retrieve(context.Background(), &contract.RetrieveRequest{
		TenantID: "42", KBIDs: []string{"a", "b"}, Query: "hi", QueryMode: "text_input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 || out.Hits[0].Content != "hello" {
		t.Fatalf("got %+v", out)
	}
}
