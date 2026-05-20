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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// TestRetrieve_HappyPath verifies the end-to-end translation:
//   - tenant_id passed to rag.Retrieve comes from the resolver, not the request
//   - coze KB ids are translated to rag UUIDs via the mapping
//   - returned hits are re-keyed via docByRagID to coze int64 DocumentIDs
//   - chunk content lands in Slice.RawContent as a Text entry
//   - chunk-level int64 ids are populated via the rag_chunk_mapping table
//     (R2-G: this used to be 0 in the pre-mapping-table implementation)
func TestRetrieve_HappyPath(t *testing.T) {
	var capturedTenant string
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(tenantID string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedTenant, capturedReq = tenantID, req
			return &contract.RetrieveResponse{
				Items: []contract.RetrieveHit{
					{ChunkID: "c1", DocID: "rag-doc-X", Score: 0.87, Content: "hello world"},
				},
			}, nil
		},
	}
	i := newTestImpl(t, fc, 8800) // single id for the lazy backfill
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(ctx, 555, "rag-doc-X", 100, 7, "task-1", 0, 0))

	resp, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
	})
	require.NoError(t, err)
	require.Len(t, resp.RetrieveSlices, 1)
	rs := resp.RetrieveSlices[0]
	require.InDelta(t, 0.87, rs.Score, 1e-9)
	require.Equal(t, int64(555), rs.Slice.DocumentID)
	require.Equal(t, int64(100), rs.Slice.KnowledgeID)
	require.Equal(t, int64(8800), rs.Slice.Info.ID, "chunk int64 id must be backfilled via rag_chunk_mapping (fixes gap doc §C row 3)")
	require.Len(t, rs.Slice.RawContent, 1)
	require.NotNil(t, rs.Slice.RawContent[0].Text)
	require.Equal(t, "hello world", *rs.Slice.RawContent[0].Text)

	// Tenant came from the resolver, passed as a header (argument), not body.
	require.Equal(t, "test-tenant", capturedTenant)
	require.NotNil(t, capturedReq)
	require.Equal(t, []string{"rag-kb-100"}, capturedReq.KBIDs)
	require.NotNil(t, capturedReq.Query)
	require.Equal(t, "hi", *capturedReq.Query)
}

// TestRetrieve_ChunkIDBackfill_StableAcrossCalls verifies that the second
// retrieve call hitting the same rag chunk returns the same Slice.Info.ID
// (idgen is only called for genuinely fresh chunks).
func TestRetrieve_ChunkIDBackfill_StableAcrossCalls(t *testing.T) {
	fc := &fakeClient{
		retrieveFunc: func(_ string, _ *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			return &contract.RetrieveResponse{
				Items: []contract.RetrieveHit{{ChunkID: "c-stable", DocID: "rag-doc-X", Content: "x"}},
			}, nil
		},
	}
	i := newTestImpl(t, fc, 9001) // only ONE id; second call must NOT need a fresh allocation
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(ctx, 555, "rag-doc-X", 100, 7, "task-1", 0, 0))

	resp1, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{Query: "x", KnowledgeIDs: []int64{100}})
	require.NoError(t, err)
	require.Equal(t, int64(9001), resp1.RetrieveSlices[0].Slice.Info.ID)

	resp2, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{Query: "x", KnowledgeIDs: []int64{100}})
	require.NoError(t, err)
	require.Equal(t, int64(9001), resp2.RetrieveSlices[0].Slice.Info.ID, "same chunk_id must resolve to same coze id; idgen was already exhausted")
}

// TestRetrieve_QueryStrategy_FourBooleanSubset_NoModelIDs verifies that
// when Strategy sets some subset of the 4 booleans (Rewrite / Expansion /
// MultiQuery / EnableRerank), ragimpl emits exactly those keys in
// query_strategy — no llm_model_id / rerank_model_id, no min_score /
// document_ids / max_tokens anywhere on the wire.
func TestRetrieve_QueryStrategy_FourBooleanSubset_NoModelIDs(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeText))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hello",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			Rewrite:      true,
			EnableRerank: true,
		},
	})
	require.NoError(t, err)

	require.NotNil(t, capturedReq.QueryStrategy)
	require.Equal(t, map[string]any{
		"rewrite":       true,
		"enable_rerank": true,
	}, capturedReq.QueryStrategy)

	body, err := json.Marshal(capturedReq)
	require.NoError(t, err)
	require.NotContains(t, string(body), "llm_model_id")
	require.NotContains(t, string(body), "rerank_model_id")
	require.NotContains(t, string(body), "min_score")
	require.NotContains(t, string(body), "document_ids")
	require.NotContains(t, string(body), "max_tokens")
}

// TestRetrieve_QueryStrategy_AllFalse_Omitted verifies that when the
// caller sets no query_strategy booleans, the wire payload omits the
// query_strategy key entirely.
func TestRetrieve_QueryStrategy_AllFalse_Omitted(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeText))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hello",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{},
	})
	require.NoError(t, err)

	require.Nil(t, capturedReq.QueryStrategy)

	body, err := json.Marshal(capturedReq)
	require.NoError(t, err)
	require.NotContains(t, string(body), "query_strategy")
}

// TestRetrieve_NewTopLevelFields_Forwarded verifies that the new
// top-level rag fields (filters / target_chunk_types / retrievers /
// fusion_policy / retriever_params / query_image / query_mode) are
// transparently forwarded.
func TestRetrieve_NewTopLevelFields_Forwarded(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeText))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hello",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			QueryMode:        "mixed_input",
			QueryImage:       &knowledgeModel.QueryImage{ImageRef: "ref-1"},
			TargetChunkTypes: []string{"text_chunk"},
			Filters:          map[string]any{"tag": "guides"},
			Retrievers:       []string{"dense", "bm25"},
			FusionPolicy:     map[string]any{"rrf_k": 60},
			RetrieverParams:  map[string]any{"dense": map[string]any{"candidate_k": 75}},
		},
	})
	require.NoError(t, err)

	require.Equal(t, "mixed_input", capturedReq.QueryMode)
	require.NotNil(t, capturedReq.QueryImage)
	require.Equal(t, "ref-1", capturedReq.QueryImage.ImageRef)
	require.Equal(t, []string{"text_chunk"}, capturedReq.TargetChunkTypes)
	require.Equal(t, map[string]any{"tag": "guides"}, capturedReq.Filters)
	require.Equal(t, []string{"dense", "bm25"}, capturedReq.Retrievers)
	require.Equal(t, map[string]any{"rrf_k": 60}, capturedReq.FusionPolicy)
	require.Equal(t, map[string]any{"dense": map[string]any{"candidate_k": 75}}, capturedReq.RetrieverParams)
}
