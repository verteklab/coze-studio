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
