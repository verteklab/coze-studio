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

// Package rag is the HTTP client implementation of the rag contract
// (backend/infra/contract/rag). Domain code depends on the contract; this
// package is wired in at composition time.
package rag

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

// apiPrefix is the rag API base path. /health and /ready sit at the service
// root, all business endpoints sit under /api/v1.
const apiPrefix = "/api/v1"

// tenantHeader is the HTTP header rag reads for tenant isolation. Every
// business-endpoint request MUST set this; the impl never derives a tenant
// from anywhere else.
const tenantHeader = "X-Tenant-Id"

// Compile-time check that *Client satisfies the rag contract.
var _ contract.Client = (*Client)(nil)

// Client is the HTTP client used to talk to the rag service. It is safe for
// concurrent use; the underlying *http.Client is shared across all requests.
type Client struct {
	cfg  ragconf.Config
	http *http.Client
}

// New constructs a Client from a parsed rag config. The caller owns the
// lifecycle: there is nothing to Close.
func New(cfg ragconf.Config) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Ready probes the rag /ready endpoint. Returns nil when the upstream reports
// 2xx; otherwise a mapped error (see errors.go). The tenant header is omitted
// — /ready is not tenant-scoped.
func (c *Client) Ready(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/ready", "", nil, nil, c.cfg.Timeout)
}

// envelope mirrors rag's ResponseEnvelope[T] wrapper. data is held as raw JSON
// so the caller can unmarshal into the typed business DTO after envelope-level
// validation (code, message). request_id is preserved on the wire but not
// surfaced here — logging it would require threading correlation into every
// call site, which we'd rather do in a single middleware pass than per-method.
type envelope struct {
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

// doJSON executes a JSON-in/JSON-out request against the rag service.
//
//   - tenantID == "" means "do not send X-Tenant-Id". Currently only /ready
//     uses this; every other endpoint requires it.
//   - body == nil means "no request body".
//   - out  == nil means "discard response data". The envelope is still parsed
//     so a non-zero envelope.code surfaces as an error.
//   - Retries are applied only to idempotent methods (GET/DELETE). POST/PATCH
//     callers must implement their own retry strategy if they want one,
//     because we cannot safely re-send a non-idempotent request.
func (c *Client) doJSON(ctx context.Context, method, path, tenantID string, body, out any, timeout time.Duration) error {
	var lastErr error
	attempts := 1 + c.cfg.MaxRetries
	for attempt := 0; attempt < attempts; attempt++ {
		err := c.doOnce(ctx, method, path, tenantID, body, out, timeout)
		if err == nil {
			return nil
		}
		lastErr = err
		if !idempotent(method) {
			return err
		}
		if attempt == attempts-1 {
			break
		}
		// Linear backoff: cheap, predictable, and bounded by MaxRetries.
		// Caller's ctx still controls overall cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration((attempt+1)*c.cfg.RetryBackoffMs) * time.Millisecond):
		}
	}
	return lastErr
}

func (c *Client) doOnce(ctx context.Context, method, path, tenantID string, body, out any, timeout time.Duration) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, c.cfg.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}
	// No Authorization header: rag has no service-to-service auth in current
	// scope (spec §11 risk #1). If rag ever adds a token-check middleware,
	// inject it here.

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
		// DecodeErrorEnvelope tolerates rag's three real envelope shapes (flat,
		// FastAPI HTTPException, pydantic 422). A non-JSON or unknown body
		// returns (0, ""), letting MapRagError fall through to the
		// upstream-unavailable bucket — same outcome as the previous decoder
		// for unknown bodies, but with correct classification for the shapes
		// rag actually emits.
		code, msg := contract.DecodeErrorEnvelope(raw)
		return MapRagError(resp.StatusCode, code, msg)
	}

	// Endpoints that return JSON ALL wrap their payload in ResponseEnvelope.
	// /ready is the only non-enveloped endpoint and uses out==nil + a separate
	// branch below.
	if out == nil {
		// Still validate the envelope when present so that envelope.code != 0
		// surfaces. A truly empty 2xx (e.g. /ready) is also accepted.
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			// /ready returns {"checks": ...}, not an envelope — accept silently.
			return nil
		}
		if env.Code != 0 {
			return MapRagError(resp.StatusCode, env.Code, env.Message)
		}
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
		// rag returned code=0 but no data; treat as success-with-empty.
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

// doMultipart executes a multipart-in/JSON-out POST. The caller owns the body
// reader and Content-Type (typically from multipart.Writer.FormDataContentType()).
// Response handling mirrors doJSON: ResponseEnvelope is decoded, code != 0 is
// mapped via MapRagError, and the data payload is unmarshalled into out.
//
// No retries — multipart payloads are bound to a non-idempotent POST and we
// cannot safely re-send. Matches doJSON's POST behavior.
func (c *Client) doMultipart(ctx context.Context, path, tenantID string, body io.Reader, contentType string, out any, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.cfg.BaseURL+path, body)
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
		return fmt.Errorf("rag http POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read response body: %w", readErr)
	}

	if resp.StatusCode >= 400 {
		// See doOnce for the decoder rationale; multipart's response path is
		// identical once we have the raw body bytes.
		code, msg := contract.DecodeErrorEnvelope(raw)
		return MapRagError(resp.StatusCode, code, msg)
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

// idempotent reports whether method is safe to retry without side effects.
// Intentionally narrow: only GET and DELETE. PUT is idempotent by HTTP spec
// but in practice the rag contract does not use it, and we'd rather force a
// per-call decision than retry blindly.
func idempotent(method string) bool {
	return method == http.MethodGet || method == http.MethodDelete
}

func (c *Client) ListModelProviders(ctx context.Context, tenantID string) (*contract.ListModelProvidersResponse, error) {
	out := &contract.ListModelProvidersResponse{}
	if err := c.doJSON(ctx, http.MethodGet, apiPrefix+"/model-providers", tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateKB(ctx context.Context, tenantID string, req *contract.CreateKBRequest) (*contract.KB, error) {
	out := &contract.KB{}
	if err := c.doJSON(ctx, http.MethodPost, apiPrefix+"/knowledgebases", tenantID, req, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetKB(ctx context.Context, tenantID, kbID string) (*contract.KB, error) {
	out := &contract.KB{}
	path := apiPrefix + "/knowledgebases/" + kbID
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) UpdateKB(ctx context.Context, tenantID, kbID string, req *contract.UpdateKBRequest) (*contract.KB, error) {
	out := &contract.KB{}
	path := apiPrefix + "/knowledgebases/" + kbID
	if err := c.doJSON(ctx, http.MethodPatch, path, tenantID, req, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteKB(ctx context.Context, tenantID, kbID string) error {
	path := apiPrefix + "/knowledgebases/" + kbID
	return c.doJSON(ctx, http.MethodDelete, path, tenantID, nil, nil, c.cfg.Timeout)
}

func (c *Client) ListKBs(ctx context.Context, req *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
	out := &contract.ListKBsResponse{}
	path := fmt.Sprintf("%s/knowledgebases?page=%d&page_size=%d", apiPrefix, req.Page, req.PageSize)
	if err := c.doJSON(ctx, http.MethodGet, path, req.TenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCapabilities fetches the rag-side capability descriptor for a KB. Used
// to drive wizard config and feature gating in the UI layer. Read-only; safe
// to retry on transient failures (doJSON handles GET retry idempotently).
func (c *Client) GetCapabilities(ctx context.Context, tenantID, kbID string) (*contract.KBCapabilities, error) {
	out := &contract.KBCapabilities{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/capabilities"
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

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
	if err := c.doMultipart(ctx, path, tenantID, &buf, w.FormDataContentType(), out, timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDocument(ctx context.Context, tenantID, kbID, docID string) (*contract.Document, error) {
	out := &contract.Document{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
	out := &contract.ListDocumentsResponse{}
	path := fmt.Sprintf("%s/knowledgebases/%s/documents?page=%d&page_size=%d", apiPrefix, kbID, page, pageSize)
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteDocument(ctx context.Context, tenantID, kbID, docID string) error {
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID
	return c.doJSON(ctx, http.MethodDelete, path, tenantID, nil, nil, c.cfg.Timeout)
}

// RetryDocument re-runs ingestion for a failed document task. Rag's response
// is the standard UploadDocumentResponse shape — same as CreateDocument — so
// we reuse contract.CreateDocumentResponse rather than introducing an alias.
func (c *Client) RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
	out := &contract.CreateDocumentResponse{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID + "/retry"
	if err := c.doJSON(ctx, http.MethodPost, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateDocument patches document metadata via rag's POST
// /knowledgebases/{kb_id}/documents/{doc_id}/update endpoint. Rag responds
// with the full DocumentDetail; we hand it back to the caller so domain code
// doesn't have to make a follow-up GetDocument round trip when it needs the
// post-update state.
//
// No retries: rag's update endpoint is POST and not idempotent — repeating it
// with the same body is safe in principle, but doJSON's retry policy is
// intentionally narrow (GET/DELETE only) and we don't extend it here.
func (c *Client) UpdateDocument(ctx context.Context, tenantID, kbID, docID string, req *contract.UpdateDocumentRequest) (*contract.Document, error) {
	out := &contract.Document{}
	path := apiPrefix + "/knowledgebases/" + kbID + "/documents/" + docID + "/update"
	if err := c.doJSON(ctx, http.MethodPost, path, tenantID, req, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetTask(ctx context.Context, tenantID, taskID string) (*contract.Task, error) {
	out := &contract.Task{}
	path := apiPrefix + "/tasks/" + taskID
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Retrieve(ctx context.Context, tenantID string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
	out := &contract.RetrieveResponse{}
	if err := c.doJSON(ctx, http.MethodPost, apiPrefix+"/retrieval", tenantID, req, out, time.Duration(c.cfg.RetrievalTimeoutMs)*time.Millisecond); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDocumentParameterSchemas returns rag's system-wide catalog of per-
// schema_id parameter forms. The rag endpoint is NOT KB-scoped (no kb_id
// in the path), but the tenant header still travels per rag's request-
// context invariant.
func (c *Client) ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]contract.DocumentParameterSchema, error) {
	var out []contract.DocumentParameterSchema
	path := apiPrefix + "/document-parameter-schemas"
	if err := c.doJSON(ctx, http.MethodGet, path, tenantID, nil, &out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}
