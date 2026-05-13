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
		createDocFunc: func(_ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
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

	// rag.CreateDocument was called with tenant from resolver + correct modality.
	require.Equal(t, "rag-kb-100", fc.createDocKBID)
	require.Equal(t, "test-tenant", fc.createDocReq.TenantID)
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
			return &contract.Task{TaskID: "task-Z", Status: "success", Progress: 100}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4242, "rag-doc-Z", 100, 7, "task-Z", 1700000000))

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
