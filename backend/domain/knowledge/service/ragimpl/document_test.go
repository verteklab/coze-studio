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
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// TestCreateDocument_InsertsMapping asserts that a successful rag CreateDocument
// is followed by a mapping insert carrying rag_doc_id, last_task_id, and the
// caller's KB / creator info. The returned Document has its Status translated
// via RagStatusToEntity (rag "pending" -> coze DocumentStatusInit).
func TestCreateDocument_InsertsMapping(t *testing.T) {
	fc := &fakeClient{
		createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
			return &contract.CreateDocumentResponse{DocID: "rag-doc-A", TaskID: "task-A", Status: "pending"}, nil
		},
	}
	i := newTestImpl(t, fc, 7777)
	// Seed a KB mapping so KBByCozeID succeeds.
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-100", "icon", 0, 5, 0))

	resp, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
		Documents: []*entity.Document{{
			Info:        knowledgeModel.Info{Name: "doc.txt", CreatorID: 5},
			KnowledgeID: 100,
			Type:        knowledgeModel.DocumentTypeText,
			URI:         "s3://x/y",
		}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Documents, 1)
	require.Equal(t, int64(7777), resp.Documents[0].ID)
	require.Equal(t, entity.DocumentStatusInit, resp.Documents[0].Status)

	// rag.CreateDocument was called with tenant from resolver (header arg) +
	// correct modality. Tenant is no longer in the request body.
	require.Equal(t, "rag-kb-100", fc.createDocKBID)
	require.Equal(t, "test-tenant", fc.createDocTenant)
	require.Equal(t, "text_source", fc.createDocReq.SourceModality)

	// Mapping row inserted with rag_doc_id and last_task_id.
	got, err := i.mapping.DocByCozeID(context.Background(), 7777)
	require.NoError(t, err)
	require.Equal(t, "rag-doc-A", got.RagDocID)
	require.Equal(t, "task-A", got.LastTaskID)
	require.Equal(t, int64(100), got.KBID)
	require.Equal(t, int64(5), got.CreatorID)
}

// TestMGetDocumentProgress_NoMirror verifies two invariants:
//   - rag's task status is translated correctly ("success" -> DocumentStatusEnable)
//   - the mapping row is NOT touched (last_task_id unchanged) -- the mapping
//     table has no status column, and rag is the system of record.
func TestMGetDocumentProgress_NoMirror(t *testing.T) {
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-Z", Status: "success"}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4242, "rag-doc-Z", 100, 7, "task-Z", 1700000000, 0))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4242},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, entity.DocumentStatusEnable, resp.ProgressList[0].Status)
	require.Equal(t, 100, resp.ProgressList[0].Progress)

	// Mapping row was NOT modified -- last_task_id stays exactly what we seeded.
	got, err := i.mapping.DocByCozeID(context.Background(), 4242)
	require.NoError(t, err)
	require.Equal(t, "task-Z", got.LastTaskID, "MGetDocumentProgress must not mirror status to mapping table")
}

// TestMGetDocumentProgress_FilenameSet asserts that when rag's GetTask returns
// a non-nil `filename`, MGetDocumentProgress copies it onto DocumentProgress.Name.
// Without this, the upload-progress UI falls back to rendering raw doc IDs.
func TestMGetDocumentProgress_FilenameSet(t *testing.T) {
	fn := "report-q3.pdf"
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-fn-1", Status: "running", Filename: &fn}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4301, "rag-doc-fn-1", 100, 7, "task-fn-1", 1700000000, 0))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4301},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, "report-q3.pdf", resp.ProgressList[0].Name)
}

// TestMGetDocumentProgress_FilenameNil asserts that when rag returns a nil
// filename pointer (Optional[str] = null on the wire), Name stays empty.
// The frontend's `name || id` fallback covers the rendering.
func TestMGetDocumentProgress_FilenameNil(t *testing.T) {
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-fn-nil", Status: "pending", Filename: nil}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4302, "rag-doc-fn-nil", 100, 7, "task-fn-nil", 1700000000, 0))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4302},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, "", resp.ProgressList[0].Name)
}

// TestMGetDocumentProgress_MixedFilenames covers the realistic upload-batch
// case: a per-doc fakeClient returns a filename for some tasks and a nil for
// others. Each progress entry must carry the right name (or "" for the nil).
func TestMGetDocumentProgress_MixedFilenames(t *testing.T) {
	a := "a.pdf"
	b := "b.pdf"
	filenames := map[string]*string{
		"task-A": &a,
		"task-B": &b,
		"task-C": nil,
	}
	fc := &fakeClient{
		getTaskFunc: func(_, taskID string) (*contract.Task, error) {
			return &contract.Task{TaskID: taskID, Status: "success", Filename: filenames[taskID]}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5001, "rag-doc-A", 100, 7, "task-A", 1700000000, 0))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5002, "rag-doc-B", 100, 7, "task-B", 1700000000, 0))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5003, "rag-doc-C", 100, 7, "task-C", 1700000000, 0))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{5001, 5002, 5003},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 3)
	byID := map[int64]string{}
	for _, dp := range resp.ProgressList {
		byID[dp.ID] = dp.Name
	}
	require.Equal(t, "a.pdf", byID[5001])
	require.Equal(t, "b.pdf", byID[5002])
	require.Equal(t, "", byID[5003])
}

// TestRagimpl_RetryDocument verifies that ragimpl.RetryDocument resolves the
// coze doc id to its rag UUID and the owning KB's rag UUID via the mapping
// table, forwards the call to the rag client, and bumps the mapping's
// last_task_id so MGetDocumentProgress follows the retry's new task.
func TestRagimpl_RetryDocument(t *testing.T) {
	var gotTenant, gotKBID, gotDocID string
	fc := &fakeClient{
		retryDocumentFunc: func(tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
			gotTenant, gotKBID, gotDocID = tenantID, kbID, docID
			return &contract.CreateDocumentResponse{
				DocID: docID, TaskID: "task-retry-9", Status: "pending",
			}, nil
		},
	}
	i := newTestImpl(t, fc)

	// Wire mapping rows: coze KB 100 → rag UUID "rag-kb-X";
	// coze doc 500 → rag UUID "rag-doc-Y" in KB 100 with last_task_id="task-old-1".
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-old-1", 1700000000, 0))

	resp, err := i.RetryDocument(context.Background(), &service.RetryDocumentRequest{DocumentID: 500})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Document)
	require.Equal(t, int64(500), resp.Document.ID)
	require.Equal(t, entity.DocumentStatusInit, resp.Document.Status) // rag "pending" → Init
	require.Equal(t, int64(100), resp.Document.KnowledgeID)

	// Rag client received the rag-side UUIDs:
	require.Equal(t, "test-tenant", gotTenant)
	require.Equal(t, "rag-kb-X", gotKBID)
	require.Equal(t, "rag-doc-Y", gotDocID)

	// CRITICAL: mapping's last_task_id was bumped to the new task.
	dm, err := i.mapping.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "task-retry-9", dm.LastTaskID, "mapping must be updated so MGetDocumentProgress polls the new task")
}

// TestRagimpl_RetryDocument_MissingDocMapping verifies that a missing doc
// mapping row surfaces ErrMappingNotFound without calling the rag client.
func TestRagimpl_RetryDocument_MissingDocMapping(t *testing.T) {
	called := false
	fc := &fakeClient{
		retryDocumentFunc: func(_, _, _ string) (*contract.CreateDocumentResponse, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	_, err := i.RetryDocument(context.Background(), &service.RetryDocumentRequest{DocumentID: 999})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client should NOT be called when mapping is missing")
}
