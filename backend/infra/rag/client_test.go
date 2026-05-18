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
	"errors"
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
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{"code": 40401, "message": "kb not found"},
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

// TestCreateDocument_Multipart_ErrorEnvelope covers rag's FastAPI
// HTTPException shape on 4xx. We assert the error surfaces; classification
// is pinned by TestClient_DecodesPydantic422AsInvalidParam (the broader
// decoder coverage in R2-C) so this test focuses on the multipart-error
// integration path only.
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

// TestDeleteDocument locks rag's POST .../documents/{doc_id}/delete wire shape.
// Asserts the HTTP method, nested path, and tenant header. Rag's v1 contract
// is uniformly POST-action style (no REST DELETE), so this endpoint is a POST
// even though the operation is semantically a delete.
func TestDeleteDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/uuid-1/documents/doc-1/delete"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "42" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "42")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok"})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err := c.DeleteDocument(context.Background(), "42", "uuid-1", "doc-1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
}

// TestDeleteDocument_NoRetryOn5xx pins the behaviour shift from the rag
// contract migration: deletion is now a POST, and doJSON only retries
// idempotent methods (GET/DELETE). MaxRetries=1 must therefore produce a
// single attempt — not two — so this test guards against silently regaining
// retry semantics if anyone broadens idempotent() to include POST later.
func TestDeleteDocument_NoRetryOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{"code": 50001, "message": "boom"},
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
	if calls != 1 {
		t.Fatalf("expected 1 call (POST is not retried), got %d", calls)
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

// TestGetTask_FilenameField verifies the new optional `filename` field on
// rag's TaskDetail decodes to a non-nil *string when present on the wire.
// MGetDocumentProgress consumes this to populate DocumentProgress.Name so
// the upload-progress UI no longer falls back to rendering raw doc IDs.
func TestGetTask_FilenameField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-fn",
				"type":        "ingestion",
				"status":      "success",
				"retry_count": 0,
				"filename":    "doc.pdf",
				"created_at":  "2026-05-15T08:00:00.000000",
			},
			"request_id": "req-fn",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetTask(context.Background(), "t1", "task-fn")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Filename == nil {
		t.Fatalf("Filename = nil, want non-nil pointer to %q", "doc.pdf")
	}
	if *got.Filename != "doc.pdf" {
		t.Errorf("*Filename = %q, want %q", *got.Filename, "doc.pdf")
	}
}

// TestGetTask_FilenameNull verifies that `filename: null` decodes to a nil
// pointer (rag's TaskDetail.filename is Optional[str], emitted as JSON null
// during pre-ingest phases before the ingestion worker stamps it).
func TestGetTask_FilenameNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"task_id":     "task-fnnull",
				"type":        "ingestion",
				"status":      "pending",
				"retry_count": 0,
				"filename":    nil,
				"created_at":  "2026-05-15T08:00:00.000000",
			},
			"request_id": "req-fnnull",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetTask(context.Background(), "t1", "task-fnnull")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Filename != nil {
		t.Errorf("Filename = %v, want nil", got.Filename)
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
	// Lock the time decoding too — Phase A learned that nil/value checks alone
	// won't catch a RagTime.UnmarshalJSON regression that drops microseconds or
	// applies the wrong timezone. The wire values below decode to UTC milliseconds.
	if got.CreatedAt.UnixMilli() != 1778765157009 {
		t.Errorf("CreatedAt.UnixMilli() = %d, want 1778765157009", got.CreatedAt.UnixMilli())
	}
	if got.UpdatedAt.UnixMilli() != 1778765164484 {
		t.Errorf("UpdatedAt.UnixMilli() = %d, want 1778765164484", got.UpdatedAt.UnixMilli())
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
	if item.CreatedAt.UnixMilli() != 1778765157009 {
		t.Errorf("Items[0].CreatedAt.UnixMilli() = %d, want 1778765157009", item.CreatedAt.UnixMilli())
	}
	if item.UpdatedAt.UnixMilli() != 1778765164484 {
		t.Errorf("Items[0].UpdatedAt.UnixMilli() = %d, want 1778765164484", item.UpdatedAt.UnixMilli())
	}
}

// TestClient_DecodesFlatEnvelopeError verifies that rag's flat error envelope
// {code, message, data, request_id} flows through the new DecodeErrorEnvelope
// path and reaches MapRagError with the correct rag code and message. A 5xx
// classifies as ErrRagUpstreamUnavailableCode (default branch) — the new
// decoder doesn't change that, only ensures the rag code and message are
// preserved in the error's rendered Msg() for debugging.
func TestClient_DecodesFlatEnvelopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":       50001,
			"message":    "model not found",
			"data":       nil,
			"request_id": "r1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	_, err := c.ListModelProviders(context.Background(), "t1")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var se errorx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected errorx.StatusError, got %T: %v", err, err)
	}
	if se.Code() != errno.ErrRagUpstreamUnavailableCode {
		t.Errorf("Code() = %d, want %d (ErrRagUpstreamUnavailableCode)", se.Code(), errno.ErrRagUpstreamUnavailableCode)
	}
	// errorx.KV("msg", ...) substitutes into the registered template's {msg}
	// placeholder; the rendered text lives on Msg(), not Extra().
	msg := se.Msg()
	if !strings.Contains(msg, "50001") {
		t.Errorf("Msg() = %q, want it to contain rag code 50001", msg)
	}
	if !strings.Contains(msg, "model not found") {
		t.Errorf("Msg() = %q, want it to contain rag message", msg)
	}
}

// TestClient_DecodesPydantic422AsInvalidParam verifies that rag's pydantic
// validation 422 envelope (detail-as-array) is now correctly classified as
// ErrKnowledgeInvalidParamCode instead of being silently treated as upstream-
// unavailable. This is the leverage fix R2-C delivers — every endpoint's
// validation errors become diagnosable.
func TestClient_DecodesPydantic422AsInvalidParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": []map[string]any{
				{
					"loc":  []any{"body", "kb_ids"},
					"msg":  "field required",
					"type": "value_error.missing",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	_, err := c.CreateKB(context.Background(), "t1", &contract.CreateKBRequest{Name: "x"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var se errorx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected errorx.StatusError, got %T: %v", err, err)
	}
	if se.Code() != errno.ErrKnowledgeInvalidParamCode {
		t.Errorf("Code() = %d, want %d (ErrKnowledgeInvalidParamCode)", se.Code(), errno.ErrKnowledgeInvalidParamCode)
	}
	// errorx.KV("msg", ...) substitutes into the registered template's {msg}
	// placeholder; the rendered text lives on Msg(), not Extra().
	msg := se.Msg()
	if !strings.Contains(msg, "body.kb_ids") {
		t.Errorf("Msg() = %q, want it to contain formatted loc path 'body.kb_ids'", msg)
	}
	if !strings.Contains(msg, "field required") {
		t.Errorf("Msg() = %q, want it to contain pydantic msg 'field required'", msg)
	}
}

// TestRetrieve_QueryImageObject locks rag's RetrievalRequest.query_image wire
// shape after the 0e1f49b audit. Before R2-C, coze sent a bare base64 string
// here and rag's StrictBaseModel(extra="forbid") rejected it with HTTP 422.
// This test posts a QueryImage{ImageBase64: ...} and asserts the wire body
// has the nested object form `"query_image":{"image_base64":"..."}`.
func TestRetrieve_QueryImageObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/retrieval") {
			t.Errorf("path = %s, want suffix /api/v1/retrieval", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}

		// Decode the request body into a structurally-flexible map so we can
		// assert on the nested query_image shape exactly. Decoding into
		// contract.RetrieveRequest would tautologically succeed because the
		// type itself is what we are locking; map-decode keeps the assertion
		// honest at the wire level.
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, body)
		}
		qi, ok := got["query_image"].(map[string]any)
		if !ok {
			t.Fatalf("query_image is not an object, body=%s", body)
		}
		if qi["image_base64"] != "abc" {
			t.Errorf("query_image.image_base64 = %v, want \"abc\"", qi["image_base64"])
		}
		// image_ref omitted on the wire (omitempty); the map should not have it.
		if _, present := qi["image_ref"]; present {
			t.Errorf("query_image.image_ref should be omitted when empty, got it present")
		}

		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", Score: 0.9}},
		}))
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second, RetrievalTimeoutMs: 5000})
	out, err := c.Retrieve(context.Background(), "t1", &contract.RetrieveRequest{
		KBIDs:      []string{"kb-1"},
		QueryImage: &contract.QueryImage{ImageBase64: "abc"},
		QueryMode:  "image_input",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ChunkID != "c1" {
		t.Errorf("decoded response = %+v, want one hit chunk_id=c1", out)
	}
}

// TestRetrieve_DocumentIDsWire locks rag's RetrievalRequest.document_ids wire
// shape (R2-I). The handler decodes the body into a structurally-flexible map
// so the assertion exercises the JSON tag at the wire level rather than the Go
// field name.
func TestRetrieve_DocumentIDsWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/retrieval") {
			t.Errorf("path = %s, want suffix /api/v1/retrieval", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, body)
		}
		raw, ok := got["document_ids"].([]any)
		if !ok {
			t.Fatalf("document_ids is not an array, body=%s", body)
		}
		if len(raw) != 1 || raw[0] != "uuid-1" {
			t.Errorf("document_ids = %v, want [\"uuid-1\"]", raw)
		}

		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", Score: 0.9}},
		}))
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second, RetrievalTimeoutMs: 5000})
	q := "hi"
	_, err := c.Retrieve(context.Background(), "t1", &contract.RetrieveRequest{
		KBIDs:       []string{"kb-1"},
		Query:       &q,
		QueryMode:   "text_input",
		DocumentIDs: []string{"uuid-1"},
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
}

// TestRetrieve_MinScoreWire locks rag's RetrievalRequest.min_score wire shape
// (R2-J). MinScore is a *float64 in Go so omitempty kicks in when nil and the
// field is omitted from the body entirely.
func TestRetrieve_MinScoreWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, body)
		}
		raw, ok := got["min_score"].(float64)
		if !ok {
			t.Fatalf("min_score is not a number, body=%s", body)
		}
		if raw != 0.7 {
			t.Errorf("min_score = %v, want 0.7", raw)
		}
		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", Score: 0.9}},
		}))
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second, RetrievalTimeoutMs: 5000})
	q := "hi"
	ms := 0.7
	_, err := c.Retrieve(context.Background(), "t1", &contract.RetrieveRequest{
		KBIDs:     []string{"kb-1"},
		Query:     &q,
		QueryMode: "text_input",
		MinScore:  &ms,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
}

// TestRetrieve_MaxTokensWire locks rag's RetrievalRequest.max_tokens wire shape
// (R2-K). JSON numbers decode into any-typed maps as float64; the assertion
// converts back when comparing.
func TestRetrieve_MaxTokensWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, body)
		}
		raw, ok := got["max_tokens"].(float64)
		if !ok {
			t.Fatalf("max_tokens is not a number, body=%s", body)
		}
		if int(raw) != 2048 {
			t.Errorf("max_tokens = %v, want 2048", raw)
		}
		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", Score: 0.9}},
		}))
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second, RetrievalTimeoutMs: 5000})
	q := "hi"
	mt := 2048
	_, err := c.Retrieve(context.Background(), "t1", &contract.RetrieveRequest{
		KBIDs:     []string{"kb-1"},
		Query:     &q,
		QueryMode: "text_input",
		MaxTokens: &mt,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
}

// TestRetryDocument locks rag's POST .../documents/{doc_id}/retry wire shape.
// Rag emits the standard UploadDocumentResponse envelope (same as CreateDocument);
// the test asserts the wire path + headers and that the response decodes into
// the existing CreateDocumentResponse type.
func TestRetryDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/documents/doc-1/retry"
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
				"doc_id":  "doc-1",
				"task_id": "task-retry-1",
				"status":  "pending",
			},
			"request_id": "req-r1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.RetryDocument(context.Background(), "t1", "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("RetryDocument: %v", err)
	}
	if got.DocID != "doc-1" || got.TaskID != "task-retry-1" || got.Status != "pending" {
		t.Errorf("decoded = %+v, want {doc-1, task-retry-1, pending}", got)
	}
}

// TestUpdateDocument locks rag's POST .../documents/{doc_id}/update wire
// shape. Asserts the HTTP method, nested path, tenant header, and that the
// pointer-typed UpdateDocumentRequest serialises with `omitempty`: unset
// fields must NOT appear on the wire (rag uses model_dump(exclude_unset=True)
// to distinguish "leave alone" from "explicit empty").
func TestUpdateDocument(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/documents/doc-1/update"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"doc_id":          "doc-1",
				"filename":        "new.pdf",
				"file_type":       "pdf",
				"status":          "ready",
				"chunk_count":     4,
				"source_modality": "text_source",
				"created_at":      "2026-05-15T08:00:00.000000",
				"updated_at":      "2026-05-15T08:00:05.000000",
			},
			"request_id": "req-upd-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	newName := "new.pdf"
	got, err := c.UpdateDocument(context.Background(), "t1", "kb-1", "doc-1", &contract.UpdateDocumentRequest{
		Filename: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	if got.DocID != "doc-1" || got.Filename != "new.pdf" {
		t.Errorf("decoded = %+v, want {doc-1, new.pdf, ...}", got)
	}

	// Body must carry only the set field; pointer-nil fields must be absent
	// so rag's exclude_unset distinguishes "rename" from "clear all metadata."
	if gotBody["filename"] != "new.pdf" {
		t.Errorf("body.filename = %v, want %q", gotBody["filename"], "new.pdf")
	}
	for _, k := range []string{"tags", "category", "source_type", "source_id", "extra_metadata"} {
		if _, present := gotBody[k]; present {
			t.Errorf("body.%s present (= %v), expected omitted via omitempty", k, gotBody[k])
		}
	}
}

// TestGetCapabilities_FieldShape locks rag's GET .../capabilities wire shape.
// Asserts every top-level scalar/slice/map field; covers the "all defaults
// present as numbers/strings" path (non-nil pointers with correct values).
func TestGetCapabilities_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/capabilities"
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
				"kb_id":                       "kb-1",
				"enabled_chunk_types":         []string{"text_chunk", "image_chunk"},
				"supported_source_modalities": []string{"text_source", "image_source", "scanned_document_source"},
				"enabled_retrievers":          []string{"dense", "bm25"},
				"supported_query_modes":       []string{"text_input", "image_input"},
				"supported_search_types":      []string{"dense", "hybrid"},
				"metadata_schema":             map[string]any{},
				"filterable_fields":           []string{},
				"retrievable_fields":          []string{},
				"default_chunk_size":          512,
				"default_chunk_overlap":       64,
				"default_search_type":         "hybrid",
				"default_candidate_k":         100,
				"default_top_k":               10,
				"default_fusion_policy": map[string]any{
					"mode":    "weighted_rrf",
					"rrf_k":   60,
					"weights": map[string]float64{"text": 0.6, "image": 0.4},
				},
				"retriever_defaults":         map[string]any{},
				"supported_query_strategies": []string{"rewrite", "expansion"},
				"request_overrideable_fields": []string{
					"query_mode", "search_type", "top_k", "candidate_k", "filters",
					"target_chunk_types", "retrievers", "fusion_policy",
					"retriever_params", "query_strategy",
				},
			},
			"request_id": "req-cap-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetCapabilities(context.Background(), "t1", "kb-1")
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}

	if got.KBID != "kb-1" {
		t.Errorf("KBID = %q, want kb-1", got.KBID)
	}
	if len(got.EnabledChunkTypes) != 2 || got.EnabledChunkTypes[0] != "text_chunk" {
		t.Errorf("EnabledChunkTypes = %v", got.EnabledChunkTypes)
	}
	if len(got.SupportedSourceModalities) != 3 {
		t.Errorf("SupportedSourceModalities len = %d, want 3", len(got.SupportedSourceModalities))
	}
	if got.DefaultChunkSize == nil || *got.DefaultChunkSize != 512 {
		t.Errorf("DefaultChunkSize = %v, want *int(512)", got.DefaultChunkSize)
	}
	if got.DefaultChunkOverlap == nil || *got.DefaultChunkOverlap != 64 {
		t.Errorf("DefaultChunkOverlap = %v, want *int(64)", got.DefaultChunkOverlap)
	}
	if got.DefaultSearchType == nil || *got.DefaultSearchType != "hybrid" {
		t.Errorf("DefaultSearchType = %v, want *string(hybrid)", got.DefaultSearchType)
	}
	if got.DefaultFusionPolicy.Mode != "weighted_rrf" || got.DefaultFusionPolicy.RrfK != 60 {
		t.Errorf("DefaultFusionPolicy = %+v", got.DefaultFusionPolicy)
	}
	if len(got.SupportedQueryStrategies) != 2 {
		t.Errorf("SupportedQueryStrategies len = %d, want 2", len(got.SupportedQueryStrategies))
	}
	if len(got.RequestOverrideableFields) != 10 {
		t.Errorf("RequestOverrideableFields len = %d, want 10", len(got.RequestOverrideableFields))
	}
}

// TestGetCapabilities_NullableDefaults verifies that JSON null in default_*
// fields decodes to nil pointers, not zero-valued integers/strings.
func TestGetCapabilities_NullableDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"kb_id":                       "kb-empty",
				"enabled_chunk_types":         []string{"text_chunk"},
				"supported_source_modalities": []string{"text_source"},
				"enabled_retrievers":          []string{"dense"},
				"supported_query_modes":       []string{"text_input"},
				"supported_search_types":      []string{"dense"},
				"metadata_schema":             map[string]any{},
				"filterable_fields":           []string{},
				"retrievable_fields":          []string{},
				"default_chunk_size":          nil,
				"default_chunk_overlap":       nil,
				"default_search_type":         nil,
				"default_candidate_k":         nil,
				"default_top_k":               nil,
				"default_fusion_policy": map[string]any{
					"mode":  "weighted_rrf",
					"rrf_k": 60,
				},
				"retriever_defaults":          map[string]any{},
				"supported_query_strategies":  []string{},
				"request_overrideable_fields": []string{},
			},
			"request_id": "req-cap-2",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.GetCapabilities(context.Background(), "t1", "kb-empty")
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if got.DefaultChunkSize != nil {
		t.Errorf("DefaultChunkSize = %v, want nil", got.DefaultChunkSize)
	}
	if got.DefaultChunkOverlap != nil {
		t.Errorf("DefaultChunkOverlap = %v, want nil", got.DefaultChunkOverlap)
	}
	if got.DefaultSearchType != nil {
		t.Errorf("DefaultSearchType = %v, want nil", got.DefaultSearchType)
	}
	if got.DefaultCandidateK != nil {
		t.Errorf("DefaultCandidateK = %v, want nil", got.DefaultCandidateK)
	}
	if got.DefaultTopK != nil {
		t.Errorf("DefaultTopK = %v, want nil", got.DefaultTopK)
	}
}

// TestListDocumentParameterSchemas_FieldShape locks rag's
// GET /document-parameter-schemas wire shape. The response is a list; each
// entry has nested Parameters. The test asserts both the outer envelope
// (no kb_id in path; tenant header still required) and the nested
// parameter shape across multiple parameter Type values (boolean, integer)
// to cover the `any`-typed Default and pointer-typed Min/Max fields.
func TestListDocumentParameterSchemas_FieldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/document-parameter-schemas") {
			t.Errorf("path = %s, want suffix /api/v1/document-parameter-schemas", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": []map[string]any{
				{
					"schema_id":         "text_document",
					"description":       "Plain text paragraph processing parameters.",
					"file_types":        []string{"txt", "text"},
					"source_modalities": []string{"text_source"},
					"parameters": []map[string]any{
						{
							"name":           "merge_blank_line_paragraphs",
							"type":           "boolean",
							"group":          "text_paragraph",
							"required":       false,
							"default":        true,
							"allowed_values": []any{},
							"min_value":      nil,
							"max_value":      nil,
							"description":    "Merge paragraphs separated by blank lines when packing chunks.",
							"ui_label":       "Merge blank-line paragraphs",
							"ui_component":   "switch",
							"advanced":       false,
							"internal":       false,
						},
						{
							"name":           "chunk_size",
							"type":           "integer",
							"group":          "chunking",
							"required":       false,
							"default":        512,
							"allowed_values": []any{},
							"min_value":      64,
							"max_value":      8192,
							"description":    "Maximum chunk size for text chunking.",
							"ui_label":       "Maximum merged paragraph length",
							"ui_component":   "number",
							"advanced":       false,
							"internal":       false,
						},
					},
				},
				{
					"schema_id":         "image_document",
					"description":       "Image processing parameters.",
					"file_types":        []string{"jpg", "png"},
					"source_modalities": []string{"image_source"},
					"parameters":        []map[string]any{},
				},
			},
			"request_id": "req-ps-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := c.ListDocumentParameterSchemas(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ListDocumentParameterSchemas: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d schemas, want 2", len(got))
	}
	text := got[0]
	if text.SchemaID != "text_document" {
		t.Errorf("schemas[0].SchemaID = %q, want text_document", text.SchemaID)
	}
	if len(text.FileTypes) != 2 || text.FileTypes[0] != "txt" {
		t.Errorf("schemas[0].FileTypes = %v", text.FileTypes)
	}
	if len(text.Parameters) != 2 {
		t.Fatalf("schemas[0].Parameters len = %d, want 2", len(text.Parameters))
	}

	merge := text.Parameters[0]
	if merge.Name != "merge_blank_line_paragraphs" || merge.Type != "boolean" {
		t.Errorf("Parameters[0] name/type = %q/%q", merge.Name, merge.Type)
	}
	if merge.Default != true {
		t.Errorf("Parameters[0].Default = %v, want true (bool)", merge.Default)
	}
	if merge.MinValue != nil || merge.MaxValue != nil {
		t.Errorf("Parameters[0] min/max = %v/%v, want nil/nil for boolean", merge.MinValue, merge.MaxValue)
	}

	chunk := text.Parameters[1]
	if chunk.Name != "chunk_size" || chunk.Type != "integer" {
		t.Errorf("Parameters[1] name/type = %q/%q", chunk.Name, chunk.Type)
	}
	// JSON-decoded numbers arrive as float64 in `any`; coerce when asserting.
	if v, ok := chunk.Default.(float64); !ok || v != 512 {
		t.Errorf("Parameters[1].Default = %v (%T), want float64(512)", chunk.Default, chunk.Default)
	}
	if chunk.MinValue == nil || *chunk.MinValue != 64 {
		t.Errorf("Parameters[1].MinValue = %v, want *float64(64)", chunk.MinValue)
	}
	if chunk.MaxValue == nil || *chunk.MaxValue != 8192 {
		t.Errorf("Parameters[1].MaxValue = %v, want *float64(8192)", chunk.MaxValue)
	}

	image := got[1]
	if image.SchemaID != "image_document" {
		t.Errorf("schemas[1].SchemaID = %q", image.SchemaID)
	}
	if len(image.Parameters) != 0 {
		t.Errorf("schemas[1].Parameters should be empty, got %d", len(image.Parameters))
	}
}

// TestUpdateKB locks rag's POST .../knowledgebases/{kb_id}/update wire shape.
// Asserts the HTTP method, nested path, tenant header, and pointer-typed
// UpdateKBRequest omitempty behaviour (unset fields must not appear on the
// wire so rag's exclude_unset distinguishes "leave alone" from "explicit
// empty"). Mirrors TestUpdateDocument.
func TestUpdateKB(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/update"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"kb_id":                    "kb-1",
				"name":                     "renamed",
				"description":              "",
				"text_embedding_model_id":  "m-text",
				"image_embedding_model_id": "",
				"status":                   "ready",
				"created_at":               "2026-05-15T08:00:00.000000",
				"updated_at":               "2026-05-15T08:00:05.000000",
			},
			"request_id": "req-updkb-1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	newName := "renamed"
	got, err := c.UpdateKB(context.Background(), "t1", "kb-1", &contract.UpdateKBRequest{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateKB: %v", err)
	}
	if got.KBID != "kb-1" || got.Name != "renamed" {
		t.Errorf("decoded = %+v, want {kb-1, renamed, ...}", got)
	}

	if gotBody["name"] != "renamed" {
		t.Errorf("body.name = %v, want %q", gotBody["name"], "renamed")
	}
	for _, k := range []string{"description", "status"} {
		if _, present := gotBody[k]; present {
			t.Errorf("body.%s present (= %v), expected omitted via omitempty", k, gotBody[k])
		}
	}
}

// TestDeleteKB locks rag's POST .../knowledgebases/{kb_id}/delete wire shape.
// Like DeleteDocument, this is a POST in rag's POST-action contract — not a
// REST DELETE.
func TestDeleteKB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantSuffix := "/api/v1/knowledgebases/kb-1/delete"
		if !strings.HasSuffix(r.URL.Path, wantSuffix) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantSuffix)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok"})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err := c.DeleteKB(context.Background(), "t1", "kb-1"); err != nil {
		t.Fatalf("DeleteKB: %v", err)
	}
}
