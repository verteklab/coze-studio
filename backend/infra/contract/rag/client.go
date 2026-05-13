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
// All methods are tenant-scoped via the request bodies; the underlying HTTP
// client never invents a tenant_id.
//
//go:generate mockgen -destination ../../../internal/mock/infra/rag/client_mock.go -package mock -source client.go Client
type Client interface {
	// Health
	Ready(ctx context.Context) error

	// Model providers (used by the create-KB proxy)
	ListModelProviders(ctx context.Context) (*ListModelProvidersResponse, error)

	// Knowledge bases
	CreateKB(ctx context.Context, req *CreateKBRequest) (*KB, error)
	GetKB(ctx context.Context, tenantID, kbID string) (*KB, error)
	UpdateKB(ctx context.Context, tenantID, kbID string, req *UpdateKBRequest) (*KB, error)
	DeleteKB(ctx context.Context, tenantID, kbID string) error
	ListKBs(ctx context.Context, req *ListKBsRequest) (*ListKBsResponse, error)

	// Documents
	CreateDocument(ctx context.Context, kbID string, req *CreateDocumentRequest) (*CreateDocumentResponse, error)
	GetDocument(ctx context.Context, tenantID, docID string) (*Document, error)
	ListDocuments(ctx context.Context, tenantID, kbID string, page, pageSize int) (*ListDocumentsResponse, error)
	DeleteDocument(ctx context.Context, tenantID, docID string) error

	// Tasks
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)

	// Retrieval
	Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResponse, error)
}
