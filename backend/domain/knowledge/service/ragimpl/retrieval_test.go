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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// TestRetrieve_RejectsNL2SQL asserts that the NL2SQL sub-feature returns 501
// (ErrRagFeaturePendingCode) with a recognizable message. NL2SQL is bucket-B
// even though Retrieve itself is bucket-A.
func TestRetrieve_RejectsNL2SQL(t *testing.T) {
	i := newTestImpl(t, &fakeClient{})
	_, err := i.Retrieve(context.Background(), &knowledgeModel.RetrieveRequest{
		Query:        "anything",
		KnowledgeIDs: []int64{1},
		Strategy:     &knowledgeModel.RetrievalStrategy{EnableNL2SQL: true},
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "NL2SQL"), "expected NL2SQL in error, got: %v", err)
}

// TestRetrieve_HappyPath verifies the end-to-end translation:
//   - tenant_id passed to rag.Retrieve comes from the resolver, not the request
//   - coze KB ids are translated to rag UUIDs via the mapping
//   - returned hits are re-keyed via docByRagID to coze int64 DocumentIDs
//   - chunk content lands in Slice.RawContent as a Text entry
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
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))
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

// TestRetrieve_EnableQueryRewrite_WithLLMModelID verifies that when the caller
// requests EnableQueryRewrite AND defaultLLMModelID is configured, ragimpl
// sends both rewrite=true AND llm_model_id in the rag query_strategy. This is
// the post-R2-F happy path; before R2-F, only rewrite was sent and rag 40004'd.
func TestRetrieve_EnableQueryRewrite_WithLLMModelID(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	i.defaultLLMModelID = "model-openai-gpt-4o-mini"
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			EnableQueryRewrite: true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.QueryStrategy, "rewrite enhancement should be sent when LLM id is configured")
	require.Equal(t, true, capturedReq.QueryStrategy["rewrite"])
	require.Equal(t, "model-openai-gpt-4o-mini", capturedReq.QueryStrategy["llm_model_id"])
}

// TestRetrieve_EnableQueryRewrite_NoLLMModelID_DropsEnhancement verifies that
// when EnableQueryRewrite is true but defaultLLMModelID is empty, ragimpl drops
// the enhancement entirely (no query_strategy sent) rather than triggering rag
// 40004. Basic retrieval still completes.
func TestRetrieve_EnableQueryRewrite_NoLLMModelID_DropsEnhancement(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	i.defaultLLMModelID = ""
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy: &knowledgeModel.RetrievalStrategy{
			EnableQueryRewrite: true,
		},
	})
	require.NoError(t, err, "basic retrieval should still succeed; enhancement is dropped silently")
	require.NotNil(t, capturedReq)
	require.Nil(t, capturedReq.QueryStrategy, "query_strategy must be nil when LLM id is empty, even with EnableQueryRewrite=true")
}

// TestRetrieve_DocumentIDs_Translated verifies that when the caller supplies
// req.DocumentIDs as coze int64s, ragimpl translates them through the mapping
// repo and the resulting rag UUIDs land on ragReq.DocumentIDs.
func TestRetrieve_DocumentIDs_Translated(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))
	require.NoError(t, i.mapping.InsertDoc(ctx, 1, "uuid-1", 100, 0, "", 0, 0))
	require.NoError(t, i.mapping.InsertDoc(ctx, 2, "uuid-2", 100, 0, "", 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		DocumentIDs:  []int64{1, 2},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.ElementsMatch(t, []string{"uuid-1", "uuid-2"}, capturedReq.DocumentIDs)
}

// TestRetrieve_DocumentIDs_AllUnmapped verifies that if every coze doc id the
// caller supplied has no mapping row, ragimpl short-circuits with an empty
// response and does NOT call rag (avoids accidentally widening the scope to
// "whole KB" when the user asked to narrow it).
func TestRetrieve_DocumentIDs_AllUnmapped(t *testing.T) {
	called := false
	fc := &fakeClient{
		retrieveFunc: func(_ string, _ *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			called = true
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))
	// No InsertDoc for ids 1 and 2.

	resp, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		DocumentIDs:  []int64{1, 2},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.RetrieveSlices)
	require.False(t, called, "rag must NOT be called when every requested DocumentID is unmapped")
}

// TestRetrieve_DocumentIDs_PartiallyMapped verifies that mixed mapping (some
// mapped, some not) results in only the mapped ones being forwarded; rag is
// still called.
func TestRetrieve_DocumentIDs_PartiallyMapped(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))
	require.NoError(t, i.mapping.InsertDoc(ctx, 1, "uuid-1", 100, 0, "", 0, 0))
	// id=2 intentionally not inserted.

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		DocumentIDs:  []int64{1, 2},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.Equal(t, []string{"uuid-1"}, capturedReq.DocumentIDs)
}

// TestRetrieve_DocumentIDs_Over200 verifies that ragimpl pre-rejects oversized
// DocumentIDs (rag's pydantic validator caps at 200) before calling either
// the mapping repo or the rag client.
func TestRetrieve_DocumentIDs_Over200(t *testing.T) {
	called := false
	fc := &fakeClient{
		retrieveFunc: func(_ string, _ *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			called = true
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	ids := make([]int64, 201)
	for k := range ids {
		ids[k] = int64(k + 1)
	}
	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		DocumentIDs:  ids,
	})
	require.Error(t, err)
	require.False(t, called, "rag must not be called when DocumentIDs exceeds 200")
}

// TestRetrieve_DocumentIDs_Empty_FallsThrough verifies that an empty
// DocumentIDs slice leaves ragReq.DocumentIDs unset (nil), so rag-side
// filtering is not invoked.
func TestRetrieve_DocumentIDs_Empty_FallsThrough(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		DocumentIDs:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.Nil(t, capturedReq.DocumentIDs, "ragReq.DocumentIDs should remain nil when caller passes no doc ids")
}

// TestRetrieve_MinScore_Set verifies that when the caller supplies
// Strategy.MinScore, ragimpl forwards it on ragReq.MinScore so rag is the
// single authoritative filtering point (no coze-side post-trim in this path).
func TestRetrieve_MinScore_Set(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	ms := 0.7
	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{MinScore: &ms},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.MinScore)
	require.InDelta(t, 0.7, *capturedReq.MinScore, 1e-9)
}

// TestRetrieve_MinScore_Nil verifies that an unset Strategy.MinScore is not
// forwarded — ragReq.MinScore stays nil and the field is omitted from the
// JSON body (omitempty).
func TestRetrieve_MinScore_Nil(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.Nil(t, capturedReq.MinScore)
}

// TestRetrieve_MaxTokens_Set verifies that Strategy.MaxTokens is forwarded as
// rag's max_tokens (with int64 -> int conversion). Rag's cut is chunk-boundary
// approximate, not strict; the wire contract is what this test locks.
func TestRetrieve_MaxTokens_Set(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	mt := int64(2048)
	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{MaxTokens: &mt},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.MaxTokens)
	require.Equal(t, 2048, *capturedReq.MaxTokens)
}

// TestRetrieve_MaxTokens_Nil verifies that an unset Strategy.MaxTokens leaves
// ragReq.MaxTokens nil (omitted from the wire).
func TestRetrieve_MaxTokens_Nil(t *testing.T) {
	var capturedReq *contract.RetrieveRequest
	fc := &fakeClient{
		retrieveFunc: func(_ string, req *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			capturedReq = req
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{},
	})
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.Nil(t, capturedReq.MaxTokens)
}

// TestRetrieve_MaxTokens_Zero_Rejected verifies that *MaxTokens == 0 is
// rejected on the coze side (rag's pydantic schema requires ge=1; pre-rejecting
// surfaces a clearer ErrKnowledgeInvalidParam than rag's 422).
func TestRetrieve_MaxTokens_Zero_Rejected(t *testing.T) {
	called := false
	fc := &fakeClient{
		retrieveFunc: func(_ string, _ *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			called = true
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	mt := int64(0)
	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{MaxTokens: &mt},
	})
	require.Error(t, err)
	require.False(t, called, "rag must not be called when MaxTokens is zero")
}

// TestRetrieve_MaxTokens_Negative_Rejected mirrors the zero case for negative
// values. Both fall under "< 1" per rag's ge=1 constraint.
func TestRetrieve_MaxTokens_Negative_Rejected(t *testing.T) {
	called := false
	fc := &fakeClient{
		retrieveFunc: func(_ string, _ *contract.RetrieveRequest) (*contract.RetrieveResponse, error) {
			called = true
			return &contract.RetrieveResponse{}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-100", "", 0, 0, 0))

	mt := int64(-1)
	_, err := i.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "hi",
		KnowledgeIDs: []int64{100},
		Strategy:     &knowledgeModel.RetrievalStrategy{MaxTokens: &mt},
	})
	require.Error(t, err)
	require.False(t, called, "rag must not be called when MaxTokens is negative")
}
