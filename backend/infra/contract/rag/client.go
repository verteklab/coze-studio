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

import "context"

// Client is the abstraction over the rag HTTP service that ragimpl depends on.
// Every business-endpoint method takes an explicit tenantID — the HTTP impl
// forwards it via the X-Tenant-Id request header. The interface intentionally
// surfaces tenant as an argument (not a hidden context value) so the data flow
// is visible at every call site.
//
//go:generate mockgen -destination ../../../internal/mock/infra/rag/client_mock.go -package mock -source client.go Client
type Client interface {
	// Health probes /ready. The tenant header is NOT sent.
	Ready(ctx context.Context) error

	// Model providers (used by the create-KB proxy).
	// rag's /api/v1/model-providers ignores tenant; we still pass the resolver's
	// value so logs and traces correlate cleanly across endpoints.
	ListModelProviders(ctx context.Context, tenantID string) (*ListModelProvidersResponse, error)

	// Knowledge bases.
	CreateKB(ctx context.Context, tenantID string, req *CreateKBRequest) (*KB, error)
	GetKB(ctx context.Context, tenantID, kbID string) (*KB, error)
	UpdateKB(ctx context.Context, tenantID, kbID string, req *UpdateKBRequest) (*KB, error)
	DeleteKB(ctx context.Context, tenantID, kbID string) error
	ListKBs(ctx context.Context, req *ListKBsRequest) (*ListKBsResponse, error)
	// GetCapabilities fetches the KB's capability descriptor (enabled chunk
	// types, modalities, retrievers, defaults). Read-only.
	GetCapabilities(ctx context.Context, tenantID, kbID string) (*KBCapabilities, error)

	// Documents — all nested under their KB on the rag side.
	CreateDocument(ctx context.Context, tenantID, kbID string, req *CreateDocumentRequest) (*CreateDocumentResponse, error)
	GetDocument(ctx context.Context, tenantID, kbID, docID string) (*Document, error)
	ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*ListDocumentsResponse, error)
	DeleteDocument(ctx context.Context, tenantID, kbID, docID string) error
	// RetryDocument re-runs ingestion for a failed task. Rag returns the
	// standard UploadDocumentResponse, identical in shape to CreateDocument,
	// so the response type is reused.
	RetryDocument(ctx context.Context, tenantID, kbID, docID string) (*CreateDocumentResponse, error)
	// UpdateDocument patches document metadata (filename, tags, category,
	// source_type, source_id, extra_metadata). Maps to rag's POST
	// /knowledgebases/{kb_id}/documents/{doc_id}/update; the response is a
	// full DocumentDetail so callers can refresh local state without a
	// follow-up GetDocument.
	UpdateDocument(ctx context.Context, tenantID, kbID, docID string, req *UpdateDocumentRequest) (*Document, error)

	// Tasks.
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)

	// Retrieval.
	Retrieve(ctx context.Context, tenantID string, req *RetrieveRequest) (*RetrieveResponse, error)

	// Document parameter schemas — system-wide (no kb_id); the UI's source
	// of truth for upload-wizard parameter forms.
	ListDocumentParameterSchemas(ctx context.Context, tenantID string) ([]DocumentParameterSchema, error)

	// Manual chunk CRUD. rag uses GET/POST-only verbs (no PUT/DELETE); update
	// and delete are POST with /update and /delete path suffixes respectively.
	// All endpoints take the kb_id; create/update/delete also take a doc_id.
	// MGetChunks is a POST with a body so it can carry large chunk_id lists.
	CreateChunk(ctx context.Context, tenantID, kbID, docID string, req *CreateChunkRequest) (*Chunk, error)
	UpdateChunk(ctx context.Context, tenantID, kbID, docID, chunkID string, req *UpdateChunkRequest) (*Chunk, error)
	DeleteChunk(ctx context.Context, tenantID, kbID, docID, chunkID string) error
	ListChunks(ctx context.Context, tenantID, kbID, docID string, q *ListChunksQuery) (*ListChunksResponse, error)
	GetChunk(ctx context.Context, tenantID, kbID, chunkID string) (*Chunk, error)
	MGetChunks(ctx context.Context, tenantID, kbID string, chunkIDs []string) (*MGetChunksResponse, error)
	ListChunksByKB(ctx context.Context, tenantID, kbID string, q *ListChunksByKBQuery) (*ListChunksResponse, error)
}
