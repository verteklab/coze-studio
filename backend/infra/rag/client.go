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
	"net/http"
	"time"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

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
// 2xx; otherwise a mapped error (see errors.go).
func (c *Client) Ready(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/ready", nil, nil, c.cfg.Timeout)
}

// doJSON executes a JSON-in/JSON-out request.
//
//   - If body == nil no request body is sent.
//   - If out  == nil the response body is discarded.
//   - Retries are applied only to idempotent methods (GET/DELETE). POST/PATCH
//     callers must implement their own retry strategy if they want one,
//     because we cannot safely re-send a non-idempotent request.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any, timeout time.Duration) error {
	var lastErr error
	attempts := 1 + c.cfg.MaxRetries
	for attempt := 0; attempt < attempts; attempt++ {
		err := c.doOnce(ctx, method, path, body, out, timeout)
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

func (c *Client) doOnce(ctx context.Context, method, path string, body, out any, timeout time.Duration) error {
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
	// No Authorization header: rag has no service-to-service auth in current
	// scope (spec §11 risk #1). If rag ever adds a token-check middleware,
	// inject it here.

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rag http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		var errBody contract.ErrorBody
		// Best-effort decode: if the upstream returns non-JSON the error body
		// will be zero-valued and MapRagError will fall through to the
		// upstream-unavailable bucket, which is what we want.
		_ = json.Unmarshal(raw, &errBody)
		return MapRagError(resp.StatusCode, errBody.Code, errBody.Message)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
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

func (c *Client) ListModelProviders(ctx context.Context) (*contract.ListModelProvidersResponse, error) {
	out := &contract.ListModelProvidersResponse{}
	if err := c.doJSON(ctx, http.MethodGet, "/model_providers", nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateKB(ctx context.Context, req *contract.CreateKBRequest) (*contract.KB, error) {
	out := &contract.KB{}
	if err := c.doJSON(ctx, http.MethodPost, "/knowledgebases", req, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetKB(ctx context.Context, tenantID, kbID string) (*contract.KB, error) {
	out := &contract.KB{}
	path := "/knowledgebases/" + kbID + "?tenant_id=" + tenantID
	if err := c.doJSON(ctx, http.MethodGet, path, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) UpdateKB(ctx context.Context, tenantID, kbID string, req *contract.UpdateKBRequest) (*contract.KB, error) {
	out := &contract.KB{}
	path := "/knowledgebases/" + kbID + "?tenant_id=" + tenantID
	if err := c.doJSON(ctx, http.MethodPatch, path, req, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteKB(ctx context.Context, tenantID, kbID string) error {
	path := "/knowledgebases/" + kbID + "?tenant_id=" + tenantID
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil, c.cfg.Timeout)
}

func (c *Client) ListKBs(ctx context.Context, req *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
	out := &contract.ListKBsResponse{}
	path := fmt.Sprintf("/knowledgebases?tenant_id=%s&page=%d&page_size=%d", req.TenantID, req.Page, req.PageSize)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateDocument(ctx context.Context, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
	out := &contract.CreateDocumentResponse{}
	path := "/knowledgebases/" + kbID + "/documents"
	if err := c.doJSON(ctx, http.MethodPost, path, req, out, time.Duration(c.cfg.UploadTimeoutMs)*time.Millisecond); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDocument(ctx context.Context, tenantID, docID string) (*contract.Document, error) {
	out := &contract.Document{}
	path := "/documents/" + docID + "?tenant_id=" + tenantID
	if err := c.doJSON(ctx, http.MethodGet, path, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
	out := &contract.ListDocumentsResponse{}
	path := fmt.Sprintf("/knowledgebases/%s/documents?tenant_id=%s&page=%d&page_size=%d", kbID, tenantID, page, pageSize)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteDocument(ctx context.Context, tenantID, docID string) error {
	path := "/documents/" + docID + "?tenant_id=" + tenantID
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil, c.cfg.Timeout)
}

func (c *Client) GetTask(ctx context.Context, tenantID, taskID string) (*contract.Task, error) {
	out := &contract.Task{}
	path := "/tasks/" + taskID + "?tenant_id=" + tenantID
	if err := c.doJSON(ctx, http.MethodGet, path, nil, out, c.cfg.Timeout); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Retrieve(ctx context.Context, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
	out := &contract.RetrieveResponse{}
	if err := c.doJSON(ctx, http.MethodPost, "/retrieval", req, out, time.Duration(c.cfg.RetrievalTimeoutMs)*time.Millisecond); err != nil {
		return nil, err
	}
	return out, nil
}
