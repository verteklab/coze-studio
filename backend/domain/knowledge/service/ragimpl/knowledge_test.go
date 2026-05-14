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

	// Stubs: override return values from the test.
	createKBFunc  func(tenantID string, req *contract.CreateKBRequest) (*contract.KB, error)
	deleteKBFunc  func(tenantID, kbID string) error
	getKBFunc     func(tenantID, kbID string) (*contract.KB, error)
	updateKBFunc  func(tenantID, kbID string, req *contract.UpdateKBRequest) (*contract.KB, error)
	listKBsFunc   func(*contract.ListKBsRequest) (*contract.ListKBsResponse, error)
	createDocFunc func(tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error)
	deleteDocFunc func(tenantID, kbID, docID string) error
	listDocsFunc  func(tenantID, kbID string, page, pageSize int) (*contract.ListDocumentsResponse, error)
	getDocFunc    func(tenantID, kbID, docID string) (*contract.Document, error)
	getTaskFunc   func(tenantID, taskID string) (*contract.Task, error)
	retrieveFunc  func(tenantID string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error)
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
	require.NoError(t, i.mapping.InsertKB(context.Background(), 500, "rag-kb-500", "icon", 1, 2, 0))

	err := i.DeleteKnowledge(context.Background(), &service.DeleteKnowledgeRequest{KnowledgeID: 500})
	require.Error(t, err)

	// The mapping row should still be queryable -- restore happened on rag-failure path.
	got, err := i.mapping.KBByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "rag-kb-500", got.RagKBID)
}
