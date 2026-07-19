/*
 * Copyright 2025 coze-dev Authors
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

package ragimpl

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/storage"
	"github.com/coze-dev/coze-studio/backend/types/consts"
)

// fakeClient implements contract.Client with overridable per-method hooks.
// Methods without a hook return zero/nil values; tests set only the hooks they
// care about and read back captured request fields via the F* fields.
//
// The captured-tenant fields exist so tests can assert that ragimpl forwards
// the tenant from the resolver into every business call.
type fakeClient struct {
	// Capture: most recently received request payload for the relevant method.
	createKBTenant  string
	createKBReq     *contract.CreateKBRequest
	updateKBTenant  string
	updateKBID      string
	updateKBReq     *contract.UpdateKBRequest
	deleteKBTenant  string
	deleteKBID      string
	deleteKBCalls   int
	getKBTenant     string
	listKBsReq      *contract.ListKBsRequest
	createDocTenant string
	createDocKBID   string
	createDocReq    *contract.CreateDocumentRequest
	deleteDocTenant string
	deleteDocKBID   string
	deleteDocID     string
	listDocsTenant  string
	listDocsKBID    string
	getDocTenant    string
	getDocKBID      string
	getTaskTenant   string
	getTaskID       string
	retrieveTenant  string
	retrieveReq     *contract.RetrieveRequest

	// Chunk endpoints -- last seen request fields, plus optional stubs. Tests
	// that only need defaults can ignore both.
	createChunkTenant    string
	createChunkKBID      string
	createChunkDocID     string
	createChunkReq       *contract.CreateChunkRequest
	updateChunkTenant    string
	updateChunkKBID      string
	updateChunkDocID     string
	updateChunkChunkID   string
	updateChunkReq       *contract.UpdateChunkRequest
	deleteChunkTenant    string
	deleteChunkKBID      string
	deleteChunkDocID     string
	deleteChunkChunkID   string
	listChunksTenant     string
	listChunksKBID       string
	listChunksDocID      string
	listChunksQuery      *contract.ListChunksQuery
	getChunkTenant       string
	getChunkKBID         string
	getChunkChunkID      string
	mgetChunksTenant     string
	mgetChunksKBID       string
	mgetChunksIDs        []string
	listChunksByKBTenant string
	listChunksByKBKBID   string
	listChunksByKBQuery  *contract.ListChunksByKBQuery

	// Stubs: override return values from the test.
	createKBFunc                     func(tenantID string, req *contract.CreateKBRequest) (*contract.KB, error)
	deleteKBFunc                     func(tenantID, kbID string) error
	getKBFunc                        func(tenantID, kbID string) (*contract.KB, error)
	updateKBFunc                     func(tenantID, kbID string, req *contract.UpdateKBRequest) (*contract.KB, error)
	listKBsFunc                      func(*contract.ListKBsRequest) (*contract.ListKBsResponse, error)
	getCapabilitiesFunc              func(tenantID, kbID string) (*contract.KBCapabilities, error)
	createDocFunc                    func(tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error)
	retryDocumentFunc                func(tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error)
	updateDocFunc                    func(tenantID, kbID, docID string, req *contract.UpdateDocumentRequest) (*contract.Document, error)
	deleteDocFunc                    func(tenantID, kbID, docID string) error
	listDocsFunc                     func(tenantID, kbID string, page, pageSize int) (*contract.ListDocumentsResponse, error)
	getDocFunc                       func(tenantID, kbID, docID string) (*contract.Document, error)
	getTaskFunc                      func(tenantID, taskID string) (*contract.Task, error)
	retrieveFunc                     func(tenantID string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error)
	listDocumentParameterSchemasFunc func(tenantID string) ([]contract.DocumentParameterSchema, error)

	createChunkFunc    func(tenantID, kbID, docID string, req *contract.CreateChunkRequest) (*contract.Chunk, error)
	updateChunkFunc    func(tenantID, kbID, docID, chunkID string, req *contract.UpdateChunkRequest) (*contract.Chunk, error)
	deleteChunkFunc    func(tenantID, kbID, docID, chunkID string) error
	listChunksFunc     func(tenantID, kbID, docID string, q *contract.ListChunksQuery) (*contract.ListChunksResponse, error)
	getChunkFunc       func(tenantID, kbID, chunkID string) (*contract.Chunk, error)
	mgetChunksFunc     func(tenantID, kbID string, chunkIDs []string) (*contract.MGetChunksResponse, error)
	listChunksByKBFunc func(tenantID, kbID string, q *contract.ListChunksByKBQuery) (*contract.ListChunksResponse, error)
}

func (f *fakeClient) Ready(_ context.Context) error { return nil }

func (f *fakeClient) ListModelProviders(_ context.Context, _ string) (*contract.ListModelProvidersResponse, error) {
	return &contract.ListModelProvidersResponse{}, nil
}

func (f *fakeClient) CreateKB(_ context.Context, tenantID string, req *contract.CreateKBRequest) (*contract.KB, error) {
	f.createKBTenant = tenantID
	f.createKBReq = req
	if f.createKBFunc != nil {
		return f.createKBFunc(tenantID, req)
	}
	return &contract.KB{KBID: "rag-kb-default", Name: req.Name, Status: "active", CreatedAt: contract.RagTime(time.Now()), UpdatedAt: contract.RagTime(time.Now())}, nil
}

func (f *fakeClient) GetKB(_ context.Context, tenantID, kbID string) (*contract.KB, error) {
	f.getKBTenant = tenantID
	if f.getKBFunc != nil {
		return f.getKBFunc(tenantID, kbID)
	}
	return &contract.KB{KBID: kbID, Name: "stub", Status: "active", CreatedAt: contract.RagTime(time.Now()), UpdatedAt: contract.RagTime(time.Now())}, nil
}

func (f *fakeClient) UpdateKB(_ context.Context, tenantID, kbID string, req *contract.UpdateKBRequest) (*contract.KB, error) {
	f.updateKBTenant, f.updateKBID, f.updateKBReq = tenantID, kbID, req
	if f.updateKBFunc != nil {
		return f.updateKBFunc(tenantID, kbID, req)
	}
	return &contract.KB{KBID: kbID, Status: "active"}, nil
}

func (f *fakeClient) DeleteKB(_ context.Context, tenantID, kbID string) error {
	f.deleteKBTenant, f.deleteKBID = tenantID, kbID
	f.deleteKBCalls++
	if f.deleteKBFunc != nil {
		return f.deleteKBFunc(tenantID, kbID)
	}
	return nil
}

func (f *fakeClient) ListKBs(_ context.Context, req *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
	f.listKBsReq = req
	if f.listKBsFunc != nil {
		return f.listKBsFunc(req)
	}
	return &contract.ListKBsResponse{}, nil
}

func (f *fakeClient) GetCapabilities(_ context.Context, tenantID, kbID string) (*contract.KBCapabilities, error) {
	if f.getCapabilitiesFunc != nil {
		return f.getCapabilitiesFunc(tenantID, kbID)
	}
	return &contract.KBCapabilities{}, nil
}

func (f *fakeClient) CreateDocument(_ context.Context, tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
	f.createDocTenant, f.createDocKBID, f.createDocReq = tenantID, kbID, req
	if f.createDocFunc != nil {
		return f.createDocFunc(tenantID, kbID, req)
	}
	return &contract.CreateDocumentResponse{DocID: "rag-doc-default", TaskID: "task-default", Status: "pending"}, nil
}

func (f *fakeClient) GetDocument(_ context.Context, tenantID, kbID, docID string) (*contract.Document, error) {
	f.getDocTenant, f.getDocKBID = tenantID, kbID
	if f.getDocFunc != nil {
		return f.getDocFunc(tenantID, kbID, docID)
	}
	return &contract.Document{DocID: docID, Status: "ready"}, nil
}

func (f *fakeClient) ListDocuments(_ context.Context, tenantID, kbID string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
	f.listDocsTenant, f.listDocsKBID = tenantID, kbID
	if f.listDocsFunc != nil {
		return f.listDocsFunc(tenantID, kbID, page, pageSize)
	}
	return &contract.ListDocumentsResponse{}, nil
}

func (f *fakeClient) DeleteDocument(_ context.Context, tenantID, kbID, docID string) error {
	f.deleteDocTenant, f.deleteDocKBID, f.deleteDocID = tenantID, kbID, docID
	if f.deleteDocFunc != nil {
		return f.deleteDocFunc(tenantID, kbID, docID)
	}
	return nil
}

func (f *fakeClient) RetryDocument(_ context.Context, tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
	if f.retryDocumentFunc != nil {
		return f.retryDocumentFunc(tenantID, kbID, docID)
	}
	return &contract.CreateDocumentResponse{}, nil
}

func (f *fakeClient) UpdateDocument(_ context.Context, tenantID, kbID, docID string, req *contract.UpdateDocumentRequest) (*contract.Document, error) {
	if f.updateDocFunc != nil {
		return f.updateDocFunc(tenantID, kbID, docID, req)
	}
	return &contract.Document{DocID: docID}, nil
}

func (f *fakeClient) GetTask(_ context.Context, tenantID, taskID string) (*contract.Task, error) {
	f.getTaskTenant, f.getTaskID = tenantID, taskID
	if f.getTaskFunc != nil {
		return f.getTaskFunc(tenantID, taskID)
	}
	return &contract.Task{TaskID: taskID, Status: "success"}, nil
}

func (f *fakeClient) Retrieve(_ context.Context, tenantID string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
	f.retrieveTenant, f.retrieveReq = tenantID, req
	if f.retrieveFunc != nil {
		return f.retrieveFunc(tenantID, req)
	}
	return &contract.RetrieveResponse{}, nil
}

func (f *fakeClient) ListDocumentParameterSchemas(_ context.Context, tenantID string) ([]contract.DocumentParameterSchema, error) {
	if f.listDocumentParameterSchemasFunc != nil {
		return f.listDocumentParameterSchemasFunc(tenantID)
	}
	return nil, nil
}

func (f *fakeClient) CreateChunk(_ context.Context, tenantID, kbID, docID string, req *contract.CreateChunkRequest) (*contract.Chunk, error) {
	f.createChunkTenant, f.createChunkKBID, f.createChunkDocID, f.createChunkReq = tenantID, kbID, docID, req
	if f.createChunkFunc != nil {
		return f.createChunkFunc(tenantID, kbID, docID, req)
	}
	return &contract.Chunk{ChunkID: "rag-chunk-default", DocID: docID, KBID: kbID, ChunkType: req.ChunkType, Status: "ready"}, nil
}

func (f *fakeClient) UpdateChunk(_ context.Context, tenantID, kbID, docID, chunkID string, req *contract.UpdateChunkRequest) (*contract.Chunk, error) {
	f.updateChunkTenant, f.updateChunkKBID, f.updateChunkDocID, f.updateChunkChunkID, f.updateChunkReq = tenantID, kbID, docID, chunkID, req
	if f.updateChunkFunc != nil {
		return f.updateChunkFunc(tenantID, kbID, docID, chunkID, req)
	}
	return &contract.Chunk{ChunkID: chunkID, DocID: docID, KBID: kbID, ChunkType: "text_chunk", Status: "ready"}, nil
}

func (f *fakeClient) DeleteChunk(_ context.Context, tenantID, kbID, docID, chunkID string) error {
	f.deleteChunkTenant, f.deleteChunkKBID, f.deleteChunkDocID, f.deleteChunkChunkID = tenantID, kbID, docID, chunkID
	if f.deleteChunkFunc != nil {
		return f.deleteChunkFunc(tenantID, kbID, docID, chunkID)
	}
	return nil
}

func (f *fakeClient) ListChunks(_ context.Context, tenantID, kbID, docID string, q *contract.ListChunksQuery) (*contract.ListChunksResponse, error) {
	f.listChunksTenant, f.listChunksKBID, f.listChunksDocID, f.listChunksQuery = tenantID, kbID, docID, q
	if f.listChunksFunc != nil {
		return f.listChunksFunc(tenantID, kbID, docID, q)
	}
	return &contract.ListChunksResponse{}, nil
}

func (f *fakeClient) GetChunk(_ context.Context, tenantID, kbID, chunkID string) (*contract.Chunk, error) {
	f.getChunkTenant, f.getChunkKBID, f.getChunkChunkID = tenantID, kbID, chunkID
	if f.getChunkFunc != nil {
		return f.getChunkFunc(tenantID, kbID, chunkID)
	}
	return &contract.Chunk{ChunkID: chunkID, KBID: kbID, ChunkType: "text_chunk", Status: "ready"}, nil
}

func (f *fakeClient) MGetChunks(_ context.Context, tenantID, kbID string, chunkIDs []string) (*contract.MGetChunksResponse, error) {
	f.mgetChunksTenant, f.mgetChunksKBID, f.mgetChunksIDs = tenantID, kbID, chunkIDs
	if f.mgetChunksFunc != nil {
		return f.mgetChunksFunc(tenantID, kbID, chunkIDs)
	}
	out := make([]contract.MGetChunksItem, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		out = append(out, contract.MGetChunksItem{Chunk: contract.Chunk{ChunkID: id, KBID: kbID, ChunkType: "text_chunk", Status: "ready"}})
	}
	return &contract.MGetChunksResponse{Items: out}, nil
}

func (f *fakeClient) ListChunksByKB(_ context.Context, tenantID, kbID string, q *contract.ListChunksByKBQuery) (*contract.ListChunksResponse, error) {
	f.listChunksByKBTenant, f.listChunksByKBKBID, f.listChunksByKBQuery = tenantID, kbID, q
	if f.listChunksByKBFunc != nil {
		return f.listChunksByKBFunc(tenantID, kbID, q)
	}
	return &contract.ListChunksResponse{}, nil
}

// stubIDGen returns IDs from a fixed slice, in order. Tests use this to assert
// deterministic mapping rows without pulling in mockgen.
type stubIDGen struct {
	ids []int64
	err error
}

func (s *stubIDGen) GenID(_ context.Context) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if len(s.ids) == 0 {
		return 0, errors.New("stubIDGen: exhausted")
	}
	id := s.ids[0]
	s.ids = s.ids[1:]
	return id, nil
}

func (s *stubIDGen) GenMultiIDs(_ context.Context, n int) ([]int64, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.ids) < n {
		return nil, errors.New("stubIDGen: exhausted")
	}
	out := s.ids[:n]
	s.ids = s.ids[n:]
	return out, nil
}

// newTestImpl wires Impl with an in-memory sqlite DB, a stub idgen seeded with
// `ids`, a configurable fakeClient, and the env tenant resolver pinned to
// "test-tenant". The DB is the same one returned, so tests can inspect mapping
// rows directly after the call.
func newTestImpl(t *testing.T, fc *fakeClient, ids ...int64) *Impl {
	t.Helper()
	db := setupDB(t)
	return &Impl{
		rag:                          fc,
		mapping:                      NewMappingRepo(db),
		idgen:                        &stubIDGen{ids: ids},
		resolver:                     NewEnvTenantResolver("test-tenant"),
		storage:                      &stubStorage{},
		defaultTextEmbeddingModelID:  "text-model-default",
		defaultImageEmbeddingModelID: "image-model-default",
		defaultLLMModelID:            "llm-model-default",
		defaultRerankModelID:         "rerank-model-default",
		defaultOCRModelID:            "ocr-model-default",
	}
}

// stubStorage returns a fixed payload from GetObject; other methods are no-ops.
// Tests don't assert on bytes content, only on mapping/status side effects.
type stubStorage struct{}

var _ storage.Storage = (*stubStorage)(nil)

func (*stubStorage) PutObject(_ context.Context, _ string, _ []byte, _ ...storage.PutOptFn) error {
	return nil
}

func (*stubStorage) PutObjectWithReader(_ context.Context, _ string, _ io.Reader, _ ...storage.PutOptFn) error {
	return nil
}

func (*stubStorage) GetObject(_ context.Context, _ string) ([]byte, error) {
	return []byte("test-payload"), nil
}

func (*stubStorage) DeleteObject(_ context.Context, _ string) error { return nil }

func (*stubStorage) GetObjectUrl(_ context.Context, _ string, _ ...storage.GetOptFn) (string, error) {
	return "", nil
}

func (*stubStorage) HeadObject(_ context.Context, _ string, _ ...storage.GetOptFn) (*storage.FileInfo, error) {
	return nil, nil
}

func (*stubStorage) ListAllObjects(_ context.Context, _ string, _ ...storage.GetOptFn) ([]*storage.FileInfo, error) {
	return nil, nil
}

func (*stubStorage) ListObjectsPaginated(_ context.Context, _ *storage.ListObjectsPaginatedInput, _ ...storage.GetOptFn) (*storage.ListObjectsPaginatedOutput, error) {
	return nil, nil
}

// TestCreateKnowledge_HappyPath asserts that:
//   - tenant_id passed to rag comes from the resolver, NOT from the request
//   - default embedding model IDs are injected into the rag request
//   - a mapping row is inserted with the audit fields and the freshly-generated coze id
func TestCreateKnowledge_HappyPath(t *testing.T) {
	fc := &fakeClient{
		createKBFunc: func(_ string, _ *contract.CreateKBRequest) (*contract.KB, error) {
			return &contract.KB{
				KBID: "rag-kb-7", Name: "n", Status: "active",
				CreatedAt: contract.RagTime(time.Unix(1700000000, 0)), UpdatedAt: contract.RagTime(time.Unix(1700000000, 0)),
			}, nil
		},
	}
	i := newTestImpl(t, fc, 999)
	// Request carries SpaceID 12345 to prove we DO NOT derive tenant from it.
	resp, err := i.CreateKnowledge(context.Background(), &service.CreateKnowledgeRequest{
		Name: "n", Description: "d", CreatorID: 7, SpaceID: 12345, IconUri: "icon-uri",
		FormatType: knowledgeModel.DocumentTypeText, AppID: 42,
	})
	require.NoError(t, err)
	require.Equal(t, int64(999), resp.KnowledgeID)
	require.NotNil(t, fc.createKBReq)
	// Tenant is passed as a header (argument), not as a request-body field.
	require.Equal(t, "test-tenant", fc.createKBTenant, "tenant must come from resolver, not SpaceID")
	require.Equal(t, "text-model-default", fc.createKBReq.TextEmbeddingModelID)
	require.Equal(t, "image-model-default", fc.createKBReq.ImageEmbeddingModelID)
	require.NotEmpty(t, fc.createKBReq.EnabledChunkTypes)

	// Mapping row was inserted with audit fields from the request.
	got, err := i.mapping.KBByCozeID(context.Background(), 999)
	require.NoError(t, err)
	require.Equal(t, "rag-kb-7", got.RagKBID)
	require.Equal(t, "icon-uri", got.IconURI)
	require.Equal(t, int64(42), got.AppID)
	require.Equal(t, int64(7), got.CreatorID)
}

// TestCreateKnowledge_ContextModelOverride asserts that an override attached
// to ctx via consts.WithRagModelOverride wins over the ragimpl defaults.
// This is the contract the application layer (CreateDataset handler) relies
// on for forwarding optional CreateDatasetRequest fields into rag.
func TestCreateKnowledge_ContextModelOverride(t *testing.T) {
	fc := &fakeClient{
		createKBFunc: func(_ string, _ *contract.CreateKBRequest) (*contract.KB, error) {
			return &contract.KB{KBID: "u", Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))}, nil
		},
	}
	i := newTestImpl(t, fc, 1)
	ctx := consts.WithRagModelOverride(context.Background(), "override-t", "override-i")
	_, err := i.CreateKnowledge(ctx, &service.CreateKnowledgeRequest{Name: "k", SpaceID: 1, FormatType: knowledgeModel.DocumentTypeText})
	require.NoError(t, err)
	require.NotNil(t, fc.createKBReq)
	require.Equal(t, "override-t", fc.createKBReq.TextEmbeddingModelID)
	require.Equal(t, "override-i", fc.createKBReq.ImageEmbeddingModelID)
}

// TestCreateKnowledge_ContextModelOverridePartial asserts the per-field
// fallback: an empty override field leaves the configured default in place
// instead of clobbering it to "".
func TestCreateKnowledge_ContextModelOverridePartial(t *testing.T) {
	fc := &fakeClient{
		createKBFunc: func(_ string, _ *contract.CreateKBRequest) (*contract.KB, error) {
			return &contract.KB{KBID: "u", Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))}, nil
		},
	}
	i := newTestImpl(t, fc, 2)
	// Only text override supplied; image must fall back to the configured default.
	ctx := consts.WithRagModelOverride(context.Background(), "override-t", "")
	_, err := i.CreateKnowledge(ctx, &service.CreateKnowledgeRequest{Name: "k", SpaceID: 1, FormatType: knowledgeModel.DocumentTypeText})
	require.NoError(t, err)
	require.Equal(t, "override-t", fc.createKBReq.TextEmbeddingModelID)
	require.Equal(t, "image-model-default", fc.createKBReq.ImageEmbeddingModelID)
}

// TestDeleteKnowledge_RollbackOnRagFailure pre-seeds a mapping row, then makes
// rag.DeleteKB fail. The mapping row must end up un-deleted (restored) so the
// caller can retry without orphaning the coze handle.
func TestDeleteKnowledge_RollbackOnRagFailure(t *testing.T) {
	fc := &fakeClient{
		deleteKBFunc: func(_, _ string) error { return errors.New("rag down") },
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 500, "rag-kb-500", "icon", 1, 2, 0, knowledgeModel.DocumentTypeText))

	err := i.DeleteKnowledge(context.Background(), &service.DeleteKnowledgeRequest{KnowledgeID: 500})
	require.Error(t, err)

	// The mapping row should still be queryable -- restore happened on rag-failure path.
	got, err := i.mapping.KBByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "rag-kb-500", got.RagKBID)
}

// TestRagimpl_GetCapabilities verifies that ragimpl.GetCapabilities resolves
// coze KB id → rag UUID via mapping, then passes through.
func TestRagimpl_GetCapabilities(t *testing.T) {
	var gotTenant, gotKBID string
	fc := &fakeClient{
		getCapabilitiesFunc: func(tenantID, kbID string) (*contract.KBCapabilities, error) {
			gotTenant, gotKBID = tenantID, kbID
			return &contract.KBCapabilities{
				KBID:                "rag-kb-Z",
				EnabledChunkTypes:   []string{"text_chunk"},
				SupportedQueryModes: []string{"text_input"},
			}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 200, "rag-kb-Z", "icon", 0, 0, 1700000000, knowledgeModel.DocumentTypeText))

	got, err := i.GetCapabilities(context.Background(), 200)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "rag-kb-Z", got.KBID)
	require.Equal(t, []string{"text_chunk"}, got.EnabledChunkTypes)

	require.Equal(t, "test-tenant", gotTenant)
	require.Equal(t, "rag-kb-Z", gotKBID)
}

// TestRagimpl_GetCapabilities_MissingMapping verifies that an unknown coze KB
// id surfaces ErrMappingNotFound without calling rag.
func TestRagimpl_GetCapabilities_MissingMapping(t *testing.T) {
	called := false
	fc := &fakeClient{
		getCapabilitiesFunc: func(_, _ string) (*contract.KBCapabilities, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	_, err := i.GetCapabilities(context.Background(), 999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client should NOT be called when mapping is missing")
}

// TestListKnowledge_ByIDs_OwnerCheckIntegrity is the regression test for the
// rag-mode permission-denied bug: when CreateDocument / DatasetDetail call
// ListKnowledge with a specific IDs filter, the response must contain exactly
// those KBs with their correct CreatorID, not the first KB in the tenant.
// Before the fix, ListKnowledge ignored req.IDs and paged the whole tenant,
// causing every owner check to use whichever KB rag returned first.
func TestListKnowledge_ByIDs_OwnerCheckIntegrity(t *testing.T) {
	getKBCalls := 0
	listKBsCalled := false
	fc := &fakeClient{
		getKBFunc: func(_, kbID string) (*contract.KB, error) {
			getKBCalls++
			return &contract.KB{KBID: kbID, Name: "kb-" + kbID, Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))}, nil
		},
		listKBsFunc: func(_ *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
			listKBsCalled = true
			return &contract.ListKBsResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)

	// Seed two mappings owned by different users. The pre-fix code would have
	// surfaced one of them at [0] regardless of which ID was requested.
	require.NoError(t, i.mapping.InsertKB(context.Background(), 1001, "rag-uuid-a", "icon-a", 0, 11, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertKB(context.Background(), 1002, "rag-uuid-b", "icon-b", 0, 22, 0, knowledgeModel.DocumentTypeText))

	resp, err := i.ListKnowledge(context.Background(), &service.ListKnowledgeRequest{IDs: []int64{1001}})
	require.NoError(t, err)
	require.Len(t, resp.KnowledgeList, 1, "by-id query must return exactly the requested KB")
	require.Equal(t, int64(1001), resp.KnowledgeList[0].Info.ID)
	require.Equal(t, int64(11), resp.KnowledgeList[0].Info.CreatorID, "must hydrate from the requested mapping, not the first KB in the tenant")
	require.Equal(t, "kb-rag-uuid-a", resp.KnowledgeList[0].Info.Name, "must fetch the rag UUID derived from the requested coze id")
	require.Equal(t, 1, getKBCalls, "should issue exactly one rag.GetKB per requested id")
	require.False(t, listKBsCalled, "by-id path must not fall through to ListKBs (which has no id filter)")
}

// TestListKnowledge_ByIDs_UnknownIDsSkipped mirrors the list-all branch's
// behaviour: a coze id with no mapping row is "not owned by this deployment"
// and should be skipped silently rather than aborting the whole call. This
// protects callers that pass a mixed batch (e.g. open-api consumers) and
// avoids leaking a hard error for what is really an authorisation no-op.
func TestListKnowledge_ByIDs_UnknownIDsSkipped(t *testing.T) {
	fc := &fakeClient{
		getKBFunc: func(_, kbID string) (*contract.KB, error) {
			return &contract.KB{KBID: kbID, Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 2001, "rag-uuid-x", "icon", 0, 7, 0, knowledgeModel.DocumentTypeText))

	resp, err := i.ListKnowledge(context.Background(), &service.ListKnowledgeRequest{IDs: []int64{2001, 9999}})
	require.NoError(t, err)
	require.Len(t, resp.KnowledgeList, 1, "unknown id should be skipped, not error")
	require.Equal(t, int64(2001), resp.KnowledgeList[0].Info.ID)
}

func TestListKnowledge_ListAllBackfillsUnmappedRagKB(t *testing.T) {
	fc := &fakeClient{
		listKBsFunc: func(req *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
			return &contract.ListKBsResponse{
				Items: []contract.KB{
					{KBID: "rag-mapped", Name: "mapped", Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))},
					{KBID: "rag-external", Name: "external", Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))},
				},
				Total: 2,
			}, nil
		},
	}
	i := newTestImpl(t, fc, 3002)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 3001, "rag-mapped", "icon", 0, 7, 0, knowledgeModel.DocumentTypeText))

	resp, err := i.ListKnowledge(context.Background(), &service.ListKnowledgeRequest{})
	require.NoError(t, err)
	require.Len(t, resp.KnowledgeList, 2)
	require.Equal(t, int64(2), resp.Total)
	require.Equal(t, int64(3001), resp.KnowledgeList[0].Info.ID)
	require.Equal(t, int64(3002), resp.KnowledgeList[1].Info.ID)
	require.Equal(t, "external", resp.KnowledgeList[1].Info.Name)
	require.Equal(t, int64(0), resp.KnowledgeList[1].Info.CreatorID)

	m, err := i.mapping.KBByCozeID(context.Background(), 3002)
	require.NoError(t, err)
	require.Equal(t, "rag-external", m.RagKBID)
}

// TestListKnowledge_ByUserID is the regression test for the ScopeSelf wiring
// gap: ListDataset.Filter.scope_type = ScopeSelf is translated to
// ListKnowledgeRequest.UserID by the application layer (see
// buildListKnowledgeRequest), but the v2 RAG backend used to silently fall
// through to a tenant-wide list because ragimpl.ListKnowledge ignored UserID.
// This test asserts: (a) the response contains only rows whose mapping
// creator_id == UserID, (b) total is the unpaginated filtered count, and (c)
// rag.ListKBs is never called — the filter is satisfied by the mapping table
// alone, which avoids paging the entire tenant just to throw most of it away.
func TestListKnowledge_ByUserID(t *testing.T) {
	getKBCalls := 0
	listKBsCalled := false
	fc := &fakeClient{
		getKBFunc: func(_, kbID string) (*contract.KB, error) {
			getKBCalls++
			return &contract.KB{KBID: kbID, Name: "kb-" + kbID, Status: "active", CreatedAt: contract.RagTime(time.Unix(0, 0)), UpdatedAt: contract.RagTime(time.Unix(0, 0))}, nil
		},
		listKBsFunc: func(_ *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
			listKBsCalled = true
			return &contract.ListKBsResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)

	// Seed two KBs owned by user 11 and one by user 22. ScopeSelf for user 11
	// must surface only the two it owns, with their CreatorID preserved.
	require.NoError(t, i.mapping.InsertKB(context.Background(), 3001, "rag-uuid-3001", "", 0, 11, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertKB(context.Background(), 3002, "rag-uuid-3002", "", 0, 11, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertKB(context.Background(), 3003, "rag-uuid-3003", "", 0, 22, 0, knowledgeModel.DocumentTypeText))

	uid := int64(11)
	page, pageSize := 1, 20
	resp, err := i.ListKnowledge(context.Background(), &service.ListKnowledgeRequest{
		UserID: &uid, Page: &page, PageSize: &pageSize,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), resp.Total, "total must be the unpaginated filtered count")
	require.Len(t, resp.KnowledgeList, 2)
	for _, kb := range resp.KnowledgeList {
		require.Equal(t, uid, kb.Info.CreatorID, "every returned KB must be owned by the requesting user")
	}
	require.Equal(t, 2, getKBCalls, "one rag.GetKB call per owned KB")
	require.False(t, listKBsCalled, "ScopeSelf path must not fall through to tenant-wide ListKBs")
}

// UserID = 0 means "no caller filter" — caller-side bug or ScopeAll request
// reaching ragimpl through an unusual path. The contract is to treat it as
// ScopeAll, NOT to return every user's KBs by accident. The KBsByCreator
// zero-creator guard enforces this on the mapping side; this test pins the
// behaviour end-to-end by asserting we go through ListKBs.
func TestListKnowledge_ByUserID_ZeroUIDFallsThroughToListKBs(t *testing.T) {
	listKBsCalled := false
	fc := &fakeClient{
		listKBsFunc: func(_ *contract.ListKBsRequest) (*contract.ListKBsResponse, error) {
			listKBsCalled = true
			return &contract.ListKBsResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)

	zero := int64(0)
	resp, err := i.ListKnowledge(context.Background(), &service.ListKnowledgeRequest{UserID: &zero})
	require.NoError(t, err)
	require.Empty(t, resp.KnowledgeList)
	require.True(t, listKBsCalled, "UserID=0 must NOT trigger the creator-filtered branch")
}

// TestRagimpl_ListDocumentParameterSchemas verifies the pass-through
// behavior: tenant resolver runs, no mapping lookup happens (rag's
// endpoint is system-wide), and the rag client's return value
// propagates unchanged.
func TestRagimpl_ListDocumentParameterSchemas(t *testing.T) {
	var gotTenant string
	canned := []contract.DocumentParameterSchema{
		{
			SchemaID:    "text_document",
			Description: "Plain text",
			FileTypes:   []string{"txt"},
			Parameters: []contract.DocumentParameter{
				{Name: "chunk_size", Type: "integer", Default: 512.0},
			},
		},
	}
	fc := &fakeClient{
		listDocumentParameterSchemasFunc: func(tenantID string) ([]contract.DocumentParameterSchema, error) {
			gotTenant = tenantID
			return canned, nil
		},
	}
	i := newTestImpl(t, fc)

	got, err := i.ListDocumentParameterSchemas(context.Background())
	require.NoError(t, err)
	require.Equal(t, "test-tenant", gotTenant)
	require.Len(t, got, 1)
	require.Equal(t, "text_document", got[0].SchemaID)
	require.Len(t, got[0].Parameters, 1)
	require.Equal(t, "chunk_size", got[0].Parameters[0].Name)
}

// seedKBMapping inserts a minimal KB mapping row keyed by cozeID → ragKBID for
// tests that need a pre-existing mapping. The format_type is hardcoded to
// knowledgeModel.DocumentTypeText — fine for current callers (CreateDocument
// doesn't filter mapping rows by type), but if a future test needs a different
// KB type, extend this helper with an explicit parameter rather than hardcoding
// the new value here.
//
// The icon/creator/space/app fields are intentionally zeroed — tests that care
// about those values should call InsertKB directly.
func seedKBMapping(t *testing.T, m *MappingRepo, cozeID int64, ragKBID string) {
	t.Helper()
	require.NoError(t, m.InsertKB(context.Background(), cozeID, ragKBID, "", 0, 0, 0, knowledgeModel.DocumentTypeText))
}
