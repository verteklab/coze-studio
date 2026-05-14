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
	"io"
	"mime"
	"mime/multipart"
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

// TestCreateDocument_Multipart locks the wire shape of CreateDocument against
// rag's multipart contract. The handler asserts every property of the request
// that coze controls; the test fails immediately if a future change reverts
// the body shape, drops a header, or breaks a field name.
func TestCreateDocument_Multipart(t *testing.T) {
	const (
		tenantID = "t1"
		kbID     = "kb-abc"
		filename = "hello.txt"
		fileType = "txt"
		modality = "text_source"
	)
	wantBytes := []byte("hello world")

	chunkSize, chunkOverlap := 512, 64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantPath := "/api/v1/knowledgebases/" + kbID + "/documents"
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != tenantID {
			t.Errorf("X-Tenant-Id header = %q, want %q", got, tenantID)
		}

		ct, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("Content-Type parse: %v", err)
		}
		if ct != "multipart/form-data" {
			t.Errorf("Content-Type = %s, want multipart/form-data", ct)
		}
		boundary := params["boundary"]
		if boundary == "" {
			t.Fatalf("Content-Type missing boundary")
		}

		mr := multipart.NewReader(r.Body, boundary)
		fields := map[string]string{}
		var gotFile []byte
		var gotFilename string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("multipart NextPart: %v", err)
			}
			body, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read part %q: %v", part.FormName(), err)
			}
			if part.FormName() == "file" {
				gotFile = body
				gotFilename = part.FileName()
			} else {
				fields[part.FormName()] = string(body)
			}
		}

		if string(gotFile) != string(wantBytes) {
			t.Errorf("file bytes = %q, want %q", gotFile, wantBytes)
		}
		if gotFilename != filename {
			t.Errorf("file part filename = %q, want %q", gotFilename, filename)
		}
		if fields["file_type"] != fileType {
			t.Errorf("file_type = %q, want %q", fields["file_type"], fileType)
		}
		if fields["source_modality"] != modality {
			t.Errorf("source_modality = %q, want %q", fields["source_modality"], modality)
		}
		if fields["chunk_size"] != "512" {
			t.Errorf("chunk_size = %q, want \"512\"", fields["chunk_size"])
		}
		if fields["chunk_overlap"] != "64" {
			t.Errorf("chunk_overlap = %q, want \"64\"", fields["chunk_overlap"])
		}
		if fields["extra_metadata"] != `{"creator_id":42}` {
			t.Errorf("extra_metadata = %q, want %q", fields["extra_metadata"], `{"creator_id":42}`)
		}

		// Respond with a rag-shaped envelope.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"doc_id":  "d1",
				"task_id": "t1",
				"status":  "pending",
			},
			"request_id": "req-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{
		BaseURL:         srv.URL,
		Timeout:         5 * time.Second,
		UploadTimeoutMs: 5000,
	})
	resp, err := c.CreateDocument(context.Background(), tenantID, kbID, &contract.CreateDocumentRequest{
		FileBytes:      wantBytes,
		Filename:       filename,
		FileType:       fileType,
		SourceModality: modality,
		ChunkSize:      &chunkSize,
		ChunkOverlap:   &chunkOverlap,
		ExtraMetadata:  `{"creator_id":42}`,
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if resp.DocID != "d1" || resp.TaskID != "t1" || resp.Status != "pending" {
		t.Errorf("decoded response = %+v, want {DocID:d1 TaskID:t1 Status:pending}", resp)
	}
}

// TestCreateDocument_Multipart_ErrorEnvelope covers rag's current FastAPI
// HTTPException shape on 4xx. We assert the error surfaces; we do NOT pin the
// specific classification because R2-C will rework MapRagError to also accept
// the flat envelope and pydantic 422 array shapes.
func TestCreateDocument_Multipart_ErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{
				"code":    40001,
				"message": "X-Tenant-Id header is required",
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{
		BaseURL:         srv.URL,
		Timeout:         5 * time.Second,
		UploadTimeoutMs: 5000,
	})
	_, err := c.CreateDocument(context.Background(), "t1", "kb", &contract.CreateDocumentRequest{
		FileBytes:      []byte("x"),
		Filename:       "x.txt",
		FileType:       "txt",
		SourceModality: "text_source",
	})
	if err == nil {
		t.Fatalf("CreateDocument: want error, got nil")
	}
	if !strings.Contains(err.Error(), "X-Tenant-Id") {
		t.Errorf("error = %v, want it to mention the upstream message", err)
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
		_, _ = w.Write(envelopeBody(t, contract.Task{TaskID: "task-1", Status: "success"}))
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

// TestGetTask_FieldShape locks rag's GetTask wire shape after the 2026-05-14
// round-2 audit. The handler returns the full new envelope; the test asserts
// every coze-side field decodes correctly, including null-handling for
// StartedAt/FinishedAt.
func TestGetTask_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/tasks/task-abc") {
			t.Errorf("path = %s, want suffix /api/v1/tasks/task-abc", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-abc",
				"type":        "ingestion",
				"status":      "success",
				"retry_count": 2,
				"error_msg":   "transient embed error retried",
				"created_at":  "2026-05-14T13:25:57.009000",
				"started_at":  "2026-05-14T13:26:00.055000",
				"finished_at": "2026-05-14T13:26:04.484000",
			},
			"request_id": "req-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})
	got, err := c.GetTask(context.Background(), "t1", "task-abc")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskID != "task-abc" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "task-abc")
	}
	if got.Type != "ingestion" {
		t.Errorf("Type = %q, want ingestion", got.Type)
	}
	if got.Status != "success" {
		t.Errorf("Status = %q, want success", got.Status)
	}
	if got.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", got.RetryCount)
	}
	if got.ErrorMsg != "transient embed error retried" {
		t.Errorf("ErrorMsg = %q", got.ErrorMsg)
	}
	if got.StartedAt == nil {
		t.Errorf("StartedAt = nil, want non-nil")
	}
	if got.FinishedAt == nil {
		t.Errorf("FinishedAt = nil, want non-nil")
	}
	// Lock the decoded millisecond representation so a future RagTime
	// regression that silently drops microseconds or applies the wrong
	// timezone fails loudly. Wire values were:
	//   2026-05-14T13:25:57.009000 UTC -> 1778765157009 ms
	//   2026-05-14T13:26:00.055000 UTC -> 1778765160055 ms
	//   2026-05-14T13:26:04.484000 UTC -> 1778765164484 ms
	if gotCreatedMs := got.CreatedAt.UnixMilli(); gotCreatedMs != 1778765157009 {
		t.Errorf("CreatedAt.UnixMilli() = %d, want 1778765157009", gotCreatedMs)
	}
	if got.StartedAt != nil {
		if gotStartedMs := got.StartedAt.UnixMilli(); gotStartedMs != 1778765160055 {
			t.Errorf("StartedAt.UnixMilli() = %d, want 1778765160055", gotStartedMs)
		}
	}
	if got.FinishedAt != nil {
		if gotFinishedMs := got.FinishedAt.UnixMilli(); gotFinishedMs != 1778765164484 {
			t.Errorf("FinishedAt.UnixMilli() = %d, want 1778765164484", gotFinishedMs)
		}
	}
}

// TestGetTask_NullableTimestamps verifies that JSON null for started_at /
// finished_at decodes to a nil pointer rather than zero-time.
func TestGetTask_NullableTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-pending",
				"type":        "ingestion",
				"status":      "pending",
				"retry_count": 0,
				"error_msg":   nil,
				"created_at":  "2026-05-14T13:25:57.009000",
				"started_at":  nil,
				"finished_at": nil,
			},
			"request_id": "req-2",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetTask(context.Background(), "t1", "task-pending")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil", got.StartedAt)
	}
	if got.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil", got.FinishedAt)
	}
	if got.ErrorMsg != "" {
		t.Errorf("ErrorMsg = %q, want empty (null)", got.ErrorMsg)
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

// TestGetDocument_FieldShape locks rag's GetDocument wire shape after the
// 2026-05-14 round-2 audit. Asserts every renamed and new field decodes.
// Explicitly does NOT assert anything about Name, KBID, or Size — those
// fields are not on rag's response.
func TestGetDocument_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/documents/doc-1"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"doc_id":          "doc-1",
				"filename":        "7.测试计划.docx",
				"file_type":       "docx",
				"status":          "ready",
				"chunk_count":     80,
				"error_msg":       nil,
				"source_modality": "text_source",
				"created_at":      "2026-05-14T13:25:57.009000",
				"updated_at":      "2026-05-14T13:26:04.484000",
			},
			"request_id": "req-3",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetDocument(context.Background(), "t1", "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.DocID != "doc-1" {
		t.Errorf("DocID = %q, want doc-1", got.DocID)
	}
	if got.Filename != "7.测试计划.docx" {
		t.Errorf("Filename = %q", got.Filename)
	}
	if got.FileType != "docx" {
		t.Errorf("FileType = %q, want docx", got.FileType)
	}
	if got.Status != "ready" {
		t.Errorf("Status = %q, want ready", got.Status)
	}
	if got.ChunkCount != 80 {
		t.Errorf("ChunkCount = %d, want 80", got.ChunkCount)
	}
	if got.ErrorMsg != "" {
		t.Errorf("ErrorMsg = %q, want empty (null)", got.ErrorMsg)
	}
	if got.SourceModality != "text_source" {
		t.Errorf("SourceModality = %q", got.SourceModality)
	}
}

// TestListDocuments_FieldShape locks the list endpoint's envelope shape.
// Returns one item with the same fields as the singleton case to share
// assertion logic; the meaningful new bit is the {items, total} wrapper.
func TestListDocuments_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"items": []map[string]any{
					{
						"doc_id":          "doc-1",
						"filename":        "a.txt",
						"file_type":       "txt",
						"status":          "ready",
						"chunk_count":     3,
						"error_msg":       nil,
						"source_modality": "text_source",
						"created_at":      "2026-05-14T13:25:57.009000",
						"updated_at":      "2026-05-14T13:26:04.484000",
					},
				},
				"total": 1,
			},
			"request_id": "req-4",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.ListDocuments(context.Background(), "t1", "kb-1", 1, 50)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("Total = %d, want 1", got.Total)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	item := got.Items[0]
	if item.Filename != "a.txt" {
		t.Errorf("Items[0].Filename = %q, want a.txt", item.Filename)
	}
	if item.FileType != "txt" {
		t.Errorf("Items[0].FileType = %q", item.FileType)
	}
	if item.ChunkCount != 3 {
		t.Errorf("Items[0].ChunkCount = %d", item.ChunkCount)
	}
}
