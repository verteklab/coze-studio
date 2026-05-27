# R2-A: CreateDocument multipart + MinIO fetch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-14-r2a-createdocument-multipart-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation, base `67b73042`)
**Goal:** Switch coze's `Client.CreateDocument` to rag's multipart-with-bytes contract; ragimpl fetches the file from MinIO before calling rag. Unblocks end-to-end smoke for `KNOWLEDGE_BACKEND=rag`.

**Architecture:** Two-layer change. (1) `infra/contract/rag` redefines `CreateDocumentRequest` as a multipart-friendly struct (`FileBytes`, `Filename`, `FileType`, `SourceModality`, `ChunkSize`, `ChunkOverlap`, `ExtraMetadata`). (2) `infra/rag/client.go` adds a `doMultipart` sibling to `doJSON` and rewrites `Client.CreateDocument` to build a `multipart/form-data` body via `mime/multipart`. (3) `ragimpl.Impl` gains a `storage.Storage` dependency; its `CreateDocument` per-doc loop reads bytes via `storage.GetObject(d.URI)` and populates the new request. (4) An httptest-based contract test in `infra/rag/client_test.go` locks the wire shape so future drift fails a unit test, not a smoke. (5) `application/knowledge/init.go` and `integration_test.go` pass the existing `c.Storage` handle into the constructor.

**Tech Stack:** Go 1.24 (pinned via `GOTOOLCHAIN`), `mime/multipart`, `net/http/httptest`, gorm.io/gorm, MinIO via existing `backend/infra/storage.Storage` interface.

---

## Pre-flight: facts the plan depends on

These were resolved during plan-writing; capture them here so executing tasks don't need to re-discover them.

- `entity.ChunkingStrategy` (`backend/domain/knowledge/entity/strategy.go:52-64`): `ChunkSize int64`, `Overlap int64`. The rag multipart fields are `int` (not int64); cast explicitly via `int(d.ChunkingStrategy.ChunkSize)`.
- `ragimpl.Impl` struct + constructor live in `backend/domain/knowledge/service/ragimpl/factory.go:35` and `:45`. Adding a field there is the right place; positional param extension keeps the diff minimal.
- The composition root that calls `ragimpl.New` is `backend/application/knowledge/init.go:98`. It already has `c.Storage` (used at `:112` to set `KnowledgeSVC.storage`). Pass the same handle into `ragimpl.New`.
- `backend/domain/knowledge/service/ragimpl/integration_test.go:101` also calls `New(...)` directly with the old positional signature. The test must be updated alongside the constructor.
- `storage.Storage` is `backend/infra/storage/storage.go:31` — `GetObject(ctx, objectKey) ([]byte, error)`.
- Rag's multipart contract is frozen: see `app/api/routes/documents.py:22-42` in the rag repo. Required form fields: `file`, `file_type`, `source_modality`. All others optional. Header `X-Tenant-Id` still required (rag rejects with 40001 otherwise).

---

## Phase A — Contract type rewrite

### Task 1: Rewrite `CreateDocumentRequest`

**Files:**
- Modify: `backend/infra/contract/rag/types.go:118-127` (the existing struct)

- [ ] **Step 1: Replace the struct definition.**

Find the existing block at `types.go:118-127`:

```go
// CreateDocumentRequest is the JSON body for POST
// /api/v1/knowledgebases/{kb_id}/documents. Tenant comes from the
// X-Tenant-Id header.
type CreateDocumentRequest struct {
	SourceURI        string         `json:"source_uri"`
	SourceModality   string         `json:"source_modality"` // text_source | image_source | scanned_document_source
	ParsingStrategy  map[string]any `json:"parsing_strategy,omitempty"`
	ChunkingStrategy map[string]any `json:"chunking_strategy,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}
```

Replace with:

```go
// CreateDocumentRequest is the in-memory representation of the multipart body
// for POST /api/v1/knowledgebases/{kb_id}/documents. The JSON tags are
// intentionally absent — this struct is NEVER marshalled; the Client builds a
// multipart/form-data body field-by-field. The tenant comes from the
// X-Tenant-Id header, not the form. See rag's app/api/routes/documents.py
// upload_document for the authoritative contract.
type CreateDocumentRequest struct {
	// Required: the file bytes (loaded into memory; storage is []byte-based).
	FileBytes []byte
	// Required: file's display name; becomes the multipart filename attribute.
	Filename string
	// Required: rag's file_type form field (e.g. "pdf", "txt", "docx").
	FileType string
	// Required: rag's source_modality enum — text_source | image_source | scanned_document_source.
	SourceModality string
	// Optional: rag's chunk_size form field; nil means "rag's default".
	ChunkSize *int
	// Optional: rag's chunk_overlap form field; nil means "rag's default".
	ChunkOverlap *int
	// Optional: rag's extra_metadata form field. JSON-stringified by the
	// caller; empty string means "omit the field".
	ExtraMetadata string
}
```

- [ ] **Step 2: Compile-check (will fail intentionally — by design).**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/...`
Expected: build errors in `infra/rag/client.go::CreateDocument` (uses old fields) and `domain/knowledge/service/ragimpl/document.go::CreateDocument` (sets old fields). Both are fixed in later tasks; do NOT patch them yet to keep the diff legible.

- [ ] **Step 3: Do not commit yet.** Task 3 will commit Tasks 1–3 together once the build is green again.

---

## Phase B — Multipart client + contract test

### Task 2: Write the failing httptest contract test

**Files:**
- Create: `backend/infra/rag/client_test.go`

- [ ] **Step 1: Create the new test file.**

```go
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
```

- [ ] **Step 2: Run the test to verify it fails to compile or fails to pass.**

Run: `GOTOOLCHAIN=go1.24.0 go test ./backend/infra/rag/... -run TestCreateDocument_Multipart -v`
Expected: compile error (uses fields from Task 1 that already exist) OR test failure because `Client.CreateDocument` still uses `doJSON` (Content-Type is `application/json`, not `multipart/form-data`).

- [ ] **Step 3: Do not commit yet.** Continues into Task 3.

---

### Task 3: Add `doMultipart`; rewrite `Client.CreateDocument`

**Files:**
- Modify: `backend/infra/rag/client.go` (add `doMultipart`; rewrite `CreateDocument` at line 259-266)

- [ ] **Step 1: Add `doMultipart` to `client.go`.**

Add the following method directly below `doOnce` (currently ends at line 201). The method is one-shot — POST is non-idempotent and no retry path applies (matches `doJSON`'s rule).

```go
// doMultipart executes a multipart-in/JSON-out POST. The caller owns the body
// reader and Content-Type (typically from multipart.Writer.FormDataContentType()).
// Response handling mirrors doJSON: ResponseEnvelope is decoded, code != 0 is
// mapped via MapRagError, and the data payload is unmarshalled into out.
//
// No retries — multipart payloads are bound to a non-idempotent POST and we
// cannot safely re-send. Matches doJSON's POST behavior.
func (c *Client) doMultipart(ctx context.Context, method, path, tenantID string, body io.Reader, contentType string, out any, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, c.cfg.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rag http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read response body: %w", readErr)
	}

	if resp.StatusCode >= 400 {
		var errBody contract.ErrorBody
		_ = json.Unmarshal(raw, &errBody)
		return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)
	}

	if out == nil {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if env.Code != 0 {
		return MapRagError(resp.StatusCode, env.Code, env.Message)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Add `mime/multipart` and `strconv` imports.**

At the top of `client.go`, the import block currently reads:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)
```

Replace with:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)
```

- [ ] **Step 3: Rewrite `Client.CreateDocument`.**

Find the existing block at `client.go:259-266`:

```go
func (c *Client) CreateDocument(ctx context.Context, tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
	out := &contract.CreateDocumentResponse{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents"
	if err := c.doJSON(ctx, http.MethodPost, path, tenantID, req, out, time.Duration(c.cfg.UploadTimeoutMs)*time.Millisecond); err != nil {
		return nil, err
	}
	return out, nil
}
```

Replace with:

```go
func (c *Client) CreateDocument(ctx context.Context, tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// file (required) — use CreateFormFile so the part carries the filename attribute.
	fw, err := w.CreateFormFile("file", req.Filename)
	if err != nil {
		return nil, fmt.Errorf("multipart create file part: %w", err)
	}
	if _, err := fw.Write(req.FileBytes); err != nil {
		return nil, fmt.Errorf("multipart write file bytes: %w", err)
	}

	// Required form fields.
	if err := w.WriteField("file_type", req.FileType); err != nil {
		return nil, fmt.Errorf("multipart write file_type: %w", err)
	}
	if err := w.WriteField("source_modality", req.SourceModality); err != nil {
		return nil, fmt.Errorf("multipart write source_modality: %w", err)
	}

	// Optional form fields. We omit on nil/empty so rag applies its defaults
	// instead of seeing empty strings (which pydantic would reject under extra="forbid").
	if req.ChunkSize != nil {
		if err := w.WriteField("chunk_size", strconv.Itoa(*req.ChunkSize)); err != nil {
			return nil, fmt.Errorf("multipart write chunk_size: %w", err)
		}
	}
	if req.ChunkOverlap != nil {
		if err := w.WriteField("chunk_overlap", strconv.Itoa(*req.ChunkOverlap)); err != nil {
			return nil, fmt.Errorf("multipart write chunk_overlap: %w", err)
		}
	}
	if req.ExtraMetadata != "" {
		if err := w.WriteField("extra_metadata", req.ExtraMetadata); err != nil {
			return nil, fmt.Errorf("multipart write extra_metadata: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("multipart close: %w", err)
	}

	out := &contract.CreateDocumentResponse{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents"
	timeout := time.Duration(c.cfg.UploadTimeoutMs) * time.Millisecond
	if err := c.doMultipart(ctx, http.MethodPost, path, tenantID, &buf, w.FormDataContentType(), out, timeout); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run the contract test; expect it to pass.**

Run: `GOTOOLCHAIN=go1.24.0 go test ./backend/infra/rag/... -run TestCreateDocument_Multipart -v`
Expected: both `TestCreateDocument_Multipart` and `TestCreateDocument_Multipart_ErrorEnvelope` PASS.

- [ ] **Step 5: Verify the surrounding rag package still compiles.**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/infra/rag/... ./backend/infra/contract/rag/...`
Expected: clean build.

(The full repo build still fails because `domain/knowledge/service/ragimpl/document.go` references old `CreateDocumentRequest` fields. That is fixed in Task 6.)

- [ ] **Step 6: Commit Tasks 1–3 together.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/rag/client.go \
        backend/infra/rag/client_test.go
git commit -m "$(cat <<'EOF'
refactor(rag): switch CreateDocument to rag's multipart contract

Rag changed POST /api/v1/knowledgebases/{kb_id}/documents from JSON-with-
source_uri to multipart-with-bytes since the 2026-05-12 audit. Coze was
still sending the old JSON shape and getting 422 on every upload.

Rewrites CreateDocumentRequest as a multipart-friendly in-memory type
(FileBytes, Filename, FileType, SourceModality, ChunkSize, ChunkOverlap,
ExtraMetadata), adds doMultipart sibling to doJSON, and rewires
Client.CreateDocument to build a multipart/form-data body via
mime/multipart. The httptest-based client_test locks the wire shape so
the next drift fails a unit test instead of a smoke. Ragimpl side comes
in the next commit.
EOF
)"
```

---

## Phase C — Ragimpl + composition wiring

### Task 4: Extend `ragimpl.Impl` to carry a Storage dependency

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go:35-60` (Impl struct + New signature)

- [ ] **Step 1: Add `storage.Storage` import.**

Find the import block at `factory.go:19-28`:

```go
import (
	"context"

	"gorm.io/gorm"

	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/idgen"
)
```

Replace with:

```go
import (
	"context"

	"gorm.io/gorm"

	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/idgen"
	"github.com/coze-dev/coze-studio/backend/infra/storage"
)
```

- [ ] **Step 2: Add `storage` field to `Impl`.**

Find the struct at `factory.go:35-43`:

```go
type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
}
```

Replace with:

```go
type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver
	// storage is used by CreateDocument to fetch file bytes from MinIO and
	// forward them to rag as a multipart body. Required since the 2026-05-14
	// rag contract change; previously rag fetched by source_uri itself.
	storage storage.Storage

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
}
```

- [ ] **Step 3: Extend `New` signature.**

Find at `factory.go:45-60`:

```go
func New(
	rag contract.Client,
	db *gorm.DB,
	idgen idgen.IDGenerator,
	resolver TenantResolver,
	defaultTextModel, defaultImageModel string,
) *Impl {
	return &Impl{
		rag:                          rag,
		mapping:                      NewMappingRepo(db),
		idgen:                        idgen,
		resolver:                     resolver,
		defaultTextEmbeddingModelID:  defaultTextModel,
		defaultImageEmbeddingModelID: defaultImageModel,
	}
}
```

Replace with:

```go
func New(
	rag contract.Client,
	db *gorm.DB,
	idgen idgen.IDGenerator,
	resolver TenantResolver,
	storage storage.Storage,
	defaultTextModel, defaultImageModel string,
) *Impl {
	return &Impl{
		rag:                          rag,
		mapping:                      NewMappingRepo(db),
		idgen:                        idgen,
		resolver:                     resolver,
		storage:                      storage,
		defaultTextEmbeddingModelID:  defaultTextModel,
		defaultImageEmbeddingModelID: defaultImageModel,
	}
}
```

(`storage` slots between `resolver` and the model id strings — keeps wired deps grouped, model defaults grouped.)

- [ ] **Step 4: Build the package alone — expect failures.**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/domain/knowledge/service/ragimpl/...`
Expected: build errors in `document.go::CreateDocument` (still uses old request fields) and in test/init.go call sites because the signature changed. Fixed in Tasks 5–7.

- [ ] **Step 5: Do not commit yet.** Continues into Task 5.

---

### Task 5: Rewrite ragimpl `CreateDocument` to fetch bytes from MinIO

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go:86-131` (the entire CreateDocument method)

- [ ] **Step 1: Add `encoding/json` to imports.**

Find the import block at `document.go:19-28`:

```go
import (
	"context"
	"time"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)
```

Replace with:

```go
import (
	"context"
	"encoding/json"
	"time"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)
```

- [ ] **Step 2: Replace `CreateDocument` body.**

Find the method at `document.go:80-131`. The full block to replace begins at `// CreateDocument creates one rag document per input entity.` and ends at `return &service.CreateDocumentResponse{Documents: out}, nil` followed by `}`.

Replace with:

```go
// CreateDocument creates one rag document per input entity. Each rag doc gets
// an int64 coze id from idgen, and a mapping row is written before we return.
//
// Since the 2026-05-14 rag contract change, rag's POST .../documents is
// multipart-with-bytes. We fetch the file bytes from MinIO (the URI is the
// coze-side object key) and forward them inline. Sequential per-doc loop is
// preserved — the upload UI typical batch is 1-20 and rag's CreateDocument is
// already async (it returns a task_id immediately).
func (i *Impl) CreateDocument(ctx context.Context, req *service.CreateDocumentRequest) (*service.CreateDocumentResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Document, 0, len(req.Documents))
	for _, d := range req.Documents {
		m, err := i.mapping.KBByCozeID(ctx, d.KnowledgeID)
		if err != nil {
			return nil, err
		}

		fileBytes, err := i.storage.GetObject(ctx, d.URI)
		if err != nil {
			return nil, err
		}

		var chunkSize, chunkOverlap *int
		if d.ChunkingStrategy != nil {
			if d.ChunkingStrategy.ChunkSize > 0 {
				s := int(d.ChunkingStrategy.ChunkSize)
				chunkSize = &s
			}
			if d.ChunkingStrategy.Overlap > 0 {
				o := int(d.ChunkingStrategy.Overlap)
				chunkOverlap = &o
			}
		}

		// buildDocMetadata already produces snake_case keys rag expects.
		// Marshal errors here are not surfaced: the map only ever holds
		// primitives that always marshal cleanly. An empty map serialises to
		// "{}" which we then drop to "" so rag sees the optional field absent.
		mdJSON, _ := json.Marshal(buildDocMetadata(d))
		extraMetadata := string(mdJSON)
		if extraMetadata == "{}" {
			extraMetadata = ""
		}

		ragReq := &contract.CreateDocumentRequest{
			FileBytes:      fileBytes,
			Filename:       d.Name,
			FileType:       string(d.FileExtension),
			SourceModality: sourceModalityFor(d),
			ChunkSize:      chunkSize,
			ChunkOverlap:   chunkOverlap,
			ExtraMetadata:  extraMetadata,
		}
		ragResp, err := i.rag.CreateDocument(ctx, tenant, m.RagKBID, ragReq)
		if err != nil {
			return nil, err
		}
		cozeID, err := i.idgen.GenID(ctx)
		if err != nil {
			// Best-effort cleanup: rag has accepted the doc but we can't track it.
			if delErr := i.rag.DeleteDocument(ctx, tenant, m.RagKBID, ragResp.DocID); delErr != nil {
				logs.CtxWarnf(ctx, "ragimpl: rollback DeleteDocument after idgen failure: %v", delErr)
			}
			return nil, err
		}
		nowMs := time.Now().UnixMilli()
		if err := i.mapping.InsertDoc(ctx, cozeID, ragResp.DocID, d.KnowledgeID, d.CreatorID, ragResp.TaskID, nowMs); err != nil {
			if delErr := i.rag.DeleteDocument(ctx, tenant, m.RagKBID, ragResp.DocID); delErr != nil {
				logs.CtxWarnf(ctx, "ragimpl: rollback DeleteDocument after InsertDoc failure: %v", delErr)
			}
			return nil, err
		}
		// Translate the rag status string back to coze's enum so the caller
		// sees the same shape it would have under the legacy implementation.
		copied := *d
		copied.ID = cozeID
		copied.Status = RagStatusToEntity(ragResp.Status)
		copied.CreatedAtMs = nowMs
		copied.UpdatedAtMs = nowMs
		out = append(out, &copied)
	}
	return &service.CreateDocumentResponse{Documents: out}, nil
}
```

- [ ] **Step 3: Build the package alone.**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/domain/knowledge/service/ragimpl/...`
Expected: still fails — but now only because external call sites (init.go, integration_test.go) haven't been updated to the new `New` signature. The package's own contents compile.

- [ ] **Step 4: Do not commit yet.** Continues into Task 6.

---

### Task 6: Update the composition root and integration test

**Files:**
- Modify: `backend/application/knowledge/init.go:98-105` (the `ragimpl.New` call)
- Modify: `backend/domain/knowledge/service/ragimpl/integration_test.go:101-108` (the `New` call)

- [ ] **Step 1: Update `init.go`.**

Find at `init.go:98-105`:

```go
	domainSVC := ragimpl.New(
		client,
		c.DB,
		c.IDGen,
		resolver,
		cfg.Rag.DefaultTextEmbeddingModelID,
		cfg.Rag.DefaultImageEmbeddingModelID,
	)
```

Replace with:

```go
	domainSVC := ragimpl.New(
		client,
		c.DB,
		c.IDGen,
		resolver,
		c.Storage,
		cfg.Rag.DefaultTextEmbeddingModelID,
		cfg.Rag.DefaultImageEmbeddingModelID,
	)
```

- [ ] **Step 2: Update `integration_test.go`.**

Find at `integration_test.go:101-108`:

```go
	impl := New(
		client,
		db,
		integrationIDGen{},
		resolver,
		os.Getenv("RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID"),
		os.Getenv("RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID"),
	)
```

This needs a real `storage.Storage`. The integration test currently relied on rag's old behavior — coze passed a URI, rag fetched from rag's MinIO. R2-A inverts that: coze fetches via `i.storage.GetObject(d.URI)` from coze's MinIO. Two options:

- **(a)** Provide a real storage handle (the test would need full MinIO config + a real object at SMOKE_DOC_URI). Heavy.
- **(b)** Mark the test build-tag-skipped if the storage handle is nil and document that R2-A's broader integration test belongs in `application/knowledge` where `c.Storage` is fully assembled. Light.

Go with **(b)**: pass a small `noopStorage` stub that returns a fixed byte payload for SMOKE_DOC_URI, with a clear comment that this is a knowingly thin replacement for a proper E2E test which belongs upstream. This keeps the unit-level integration test compiling and the manual smoke (Task 8 Step 4) is the real verifier.

Replace the block with:

```go
	impl := New(
		client,
		db,
		integrationIDGen{},
		resolver,
		newSmokeStorage(t),
		os.Getenv("RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID"),
		os.Getenv("RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID"),
	)
```

- [ ] **Step 3: Add `newSmokeStorage` helper at the bottom of `integration_test.go`.**

Append after the last test function (use Read to locate the last line of the file first if uncertain):

```go
// smokeStorage is a deliberately-thin storage.Storage substitute for the
// build-tag-gated integration test. After R2-A, ragimpl.CreateDocument fetches
// bytes via i.storage.GetObject before calling rag, so the test needs SOME
// Storage to construct Impl. Providing a real MinIO handle here would duplicate
// the configuration that the application layer already does (init.go wires
// c.Storage from the same MinIO config). The "true" end-to-end test for the
// rag-backed upload path lives at the application layer; here we only verify
// the per-doc flow with a constant payload.
type smokeStorage struct{ payload []byte }

func newSmokeStorage(t *testing.T) *smokeStorage {
	t.Helper()
	return &smokeStorage{payload: []byte("rag-integration-test payload")}
}

func (s *smokeStorage) PutObject(ctx context.Context, objectKey string, content []byte, opts ...storage.PutOptFn) error {
	return nil
}
func (s *smokeStorage) GetObject(ctx context.Context, objectKey string) ([]byte, error) {
	return s.payload, nil
}
func (s *smokeStorage) DeleteObject(ctx context.Context, objectKey string) error {
	return nil
}
func (s *smokeStorage) GetObjectUrl(ctx context.Context, objectKey string, opts ...storage.GetOptFn) (string, error) {
	return "https://example.invalid/" + objectKey, nil
}
```

Add the storage import to `integration_test.go`'s import block if not already present:

```go
"github.com/coze-dev/coze-studio/backend/infra/storage"
```

(Verify the exact `storage.Storage` interface methods by re-reading `backend/infra/storage/storage.go`. If the interface has additional methods, add no-op implementations for those too — the compile-time `var _ storage.Storage = (*smokeStorage)(nil)` check below is the safety net.)

Add this compile-time assertion at the top of the helper section to catch missing methods:

```go
var _ storage.Storage = (*smokeStorage)(nil)
```

- [ ] **Step 4: Full repo build.**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/...`
Expected: clean build.

- [ ] **Step 5: Run all affected package tests.**

Run: `GOTOOLCHAIN=go1.24.0 go test ./backend/infra/contract/rag/... ./backend/infra/rag/... ./backend/domain/knowledge/service/ragimpl/... ./backend/application/knowledge/... -v`

Expected: every test passes. The httptest contract test from Task 2 is the new coverage; existing tests in `application/knowledge` and `ragimpl` continue to pass.

If the integration test under `ragimpl` is skipped because of a build tag (`//go:build integration`), it won't run by default. That is fine — its only job is to compile.

- [ ] **Step 6: Commit Tasks 4–6 together.**

```bash
git add backend/domain/knowledge/service/ragimpl/factory.go \
        backend/domain/knowledge/service/ragimpl/document.go \
        backend/domain/knowledge/service/ragimpl/integration_test.go \
        backend/application/knowledge/init.go
git commit -m "$(cat <<'EOF'
feat(ragimpl): fetch bytes from MinIO before rag CreateDocument

Threads infra/storage.Storage through ragimpl.Impl and uses it in
CreateDocument to fetch the file body for each input entity. ChunkSize /
ChunkOverlap from entity.ChunkingStrategy are forwarded as rag form
fields; buildDocMetadata round-trips into rag's extra_metadata as
JSON-stringified text. Composition root passes c.Storage; integration_test
substitutes a thin stub since the real E2E verification belongs upstream.

End-to-end smoke against KNOWLEDGE_BACKEND=rag should now reach a 200 on
POST /knowledgebases/{kb_id}/documents instead of the 422 wall that
blocked Phase 1.5.
EOF
)"
```

---

## Phase D — Verification

### Task 7: Full backend test sweep

**Files:** (no edits; verification only)

- [ ] **Step 1: Run the full test sweep that the project memory pins as the green bar.**

Run:

```bash
GOTOOLCHAIN=go1.24.0 go test \
  ./backend/infra/contract/rag/... \
  ./backend/infra/rag/... \
  ./backend/domain/knowledge/service/ragimpl/... \
  ./backend/application/knowledge/...
```

Expected: PASS for all packages.

- [ ] **Step 2: Run the full backend build to make sure nothing else regressed.**

Run: `GOTOOLCHAIN=go1.24.0 go build ./backend/...`
Expected: clean build, no warnings.

- [ ] **Step 3: Stage-but-don't-commit any lint fixes.** If a linter (golangci-lint, go vet) flags anything in the touched files, fix it and amend the relevant commit:

Run: `GOTOOLCHAIN=go1.24.0 go vet ./backend/infra/rag/... ./backend/domain/knowledge/service/ragimpl/... ./backend/application/knowledge/...`
Expected: clean.

If issues found, fix in place. Commit any fixes as a new commit (don't amend a published commit):

```bash
git add <fixed files>
git commit -m "chore(rag): vet/lint cleanup after R2-A"
```

---

### Task 8: Manual end-to-end smoke

This is the real verification. The httptest contract test only proves the wire shape; only a real upload through the UI proves the change works against live rag.

- [ ] **Step 1: Bring up rag + coze middleware** per the recipe in the project memory's queued item #2.

Key checks before starting:
- `rag/config/model_providers.json` exists.
- `rag/docker-compose.local.yml` has `!override` ports.
- `docker/.env.debug` has `KNOWLEDGE_BACKEND="rag"` and matching `RAG_TENANT_ID`.

Run:
```bash
# rag
cd /Users/liuxinyu/workspace/rag
docker compose -f docker-compose.yml -f docker-compose.local.yml up -d

# coze middleware
cd /Users/liuxinyu/workspace/coze-studio
make middleware

# coze server (native)
GOTOOLCHAIN=go1.24.0 make server
```

- [ ] **Step 2: Frontend rebuild check.** The Phase 1.5 wizard is already in `bin/resources/static/` from `67b73042` ancestors. If `bin/resources/static/` is empty or you just pulled, run `make fe` first (per project memory operational note #12). For R2-A specifically no frontend code changed, so `make fe` is only needed if running from a clean build.

- [ ] **Step 3: Execute the smoke.**

1. Log in (lxy907360 / local mysql account).
2. Create a new rag-backed KB (text mode), using the configured text/image embedding model ids.
3. Click upload; pick a small `.txt` or `.pdf` (<1 MB).
4. Watch the network tab in browser devtools or `docker compose logs -f web` on the rag side.

Expected behavior:
- `POST /api/v1/knowledgebases/{kb_id}/documents` returns 200 with a `data: {doc_id, task_id, status:"pending"}` envelope.
- The UI's progress poller starts ticking. (NOTE: progress field rename is R2-B, so progress likely shows as 0 or indeterminate — that is expected here; the unblock is that status transitions out of `pending`.)
- After 10–30 seconds rag completes ingest; KB detail eventually shows the document.

- [ ] **Step 4: Capture artifacts.**

If anything fails, capture:
- Browser network panel for the failing request (full URL, response body).
- `docker compose logs --since=5m web worker` from rag.
- Coze server stdout where the call originated.

A 4xx with a recognizable rag message in coze logs means our multipart was malformed — common culprit is missing or empty required form fields.

A 4xx without our message in logs but rag receiving the request means the wire shape works and the failure is on rag's side (model not registered, missing file_type mapping, etc.).

- [ ] **Step 5: Document the outcome.** If smoke passes, append a one-line entry to the project memory at `/Users/liuxinyu/.claude/projects/-Users-liuxinyu-workspace/memory/project-coze-rag-replacement-paused.md` under `## What's done`:

```markdown
- R2-A landed YYYY-MM-DD: CreateDocument now multipart; smoke green through `pending → processing`.
```

If smoke fails, do NOT amend the spec or plan inline. File the finding in the project memory's queued-items section and stop — diagnosis is a separate task from R2-A's "make the wire shape right" scope.

- [ ] **Step 6: No commit.** Manual smoke does not change tracked files.

---

## Out of scope (do not address in this plan)

These will be picked up in dedicated slices later:

- **R2-B:** `GetTask` / `GetDocument` / `ListDocuments` field renames and frontend progress derivation.
- **R2-C:** `Retrieve.query_image` shape; union-friendly `ErrorBody` decoder.
- **R2-D:** New endpoints (`capabilities`, `retry`, `document-parameter-schemas`) + wizard rework using `/document-parameter-schemas`.
- **R2-E:** Broader httptest scaffolding for the other rag endpoints; extending `rag-contract-check` to body schemas.
- Wiring richer `ParsingStrategy` / `document_options` / `target_chunk_types` / `enable_ocr` / `enable_image_embedding` form fields — those wait for R2-D where `/document-parameter-schemas` becomes the source of truth.
- Streaming the file body via `io.Pipe`. Storage interface returns `[]byte`; rag's handler materializes via `await file.read()`. No memory win for the current upload sizes; revisit if Storage gains a streaming variant naturally.

---

## Self-review checklist (filled in)

1. **Spec coverage** — every section in the spec has a corresponding task:
   - §3.2 contract change → Task 1
   - §5.1 doMultipart → Task 3 Step 1
   - §5.2 multipart builder in CreateDocument → Task 3 Step 3
   - §5.3 ragimpl CreateDocument → Task 5
   - §5.4 storage dependency wiring → Tasks 4 + 6
   - §6 invariants → preserved by Task 5's preserved rollback path
   - §7 error handling → covered by Task 5 (no new path; same MapRagError flow)
   - §8.1 contract test → Task 2
   - §8.2 existing test sweep → Task 7 Step 1
   - §8.3 smoke → Task 8

2. **Placeholders** — none. The "exact field names on ChunkingStrategy" deferred in the spec are resolved in the Pre-flight section above.

3. **Type consistency** — `CreateDocumentRequest` field names match between Task 1 (definition), Task 3 (multipart builder), and Task 5 (ragimpl population). The `New` constructor signature in Task 4 matches what Task 6 calls in both `init.go` and `integration_test.go`.
