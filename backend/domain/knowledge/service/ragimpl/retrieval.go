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
	"fmt"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// Retrieve queries rag for chunks matching the request and translates each
// hit back to a coze entity. Tenant isolation is enforced by rag (which
// filters its KB index on tenant_id) — we don't re-check on the coze side
// because Phase 1 has exactly one global tenant.
//
// NL2SQL is a separately-tracked sub-feature (the rag service doesn't expose
// SQL generation yet), so a request that opts into it returns 501 early.
func (i *Impl) Retrieve(ctx context.Context, req *service.RetrieveRequest) (*knowledgeModel.RetrieveResponse, error) {
	// NL2SQL guard. Retrieve itself is bucket-A, but this sub-feature isn't.
	if req.Strategy != nil && req.Strategy.EnableNL2SQL {
		return nil, errorx.New(errno.ErrRagFeaturePendingCode, errorx.KV("msg",
			"NL2SQL retrieval is pending rag support (roadmap: rag/docs/notes/roadmap.md#nl2sql)"))
	}

	if len(req.KnowledgeIDs) == 0 {
		return nil, errors.New("ragimpl.Retrieve: at least one knowledge_id required")
	}

	// Resolve KB mappings. We tolerate partial resolution (caller asked for ids we
	// don't know about) but fail if NOTHING resolves -- that's almost certainly
	// a wiring bug worth surfacing.
	kbs, err := i.mapping.KBsByCozeIDs(ctx, req.KnowledgeIDs)
	if err != nil {
		return nil, err
	}
	if len(kbs) == 0 {
		return nil, errors.New("ragimpl.Retrieve: no knowledge bases resolved from mapping")
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	ragKBIDs := make([]string, 0, len(kbs))
	for _, k := range kbs {
		ragKBIDs = append(ragKBIDs, k.RagKBID)
	}

	ragReq := &contract.RetrieveRequest{
		KBIDs:     ragKBIDs,
		QueryMode: "text_input",
	}

	// Translate coze int64 doc ids -> rag UUIDs via the mapping repo. Rag's
	// pydantic validator caps document_ids at 200; reject earlier on the coze
	// side so the error surfaces with a clearer message than rag's 422.
	if len(req.DocumentIDs) > 0 {
		if len(req.DocumentIDs) > 200 {
			return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
				errorx.KV("msg", fmt.Sprintf("DocumentIDs exceeds 200 (got %d)", len(req.DocumentIDs))))
		}
		docs, err := i.mapping.DocsByCozeIDs(ctx, req.DocumentIDs)
		if err != nil {
			return nil, err
		}
		ragDocIDs := make([]string, 0, len(docs))
		for _, d := range docs {
			ragDocIDs = append(ragDocIDs, d.RagDocID)
		}
		if len(ragDocIDs) > 0 {
			ragReq.DocumentIDs = ragDocIDs
		} else {
			// All ids unmapped (soft-deleted or drift). The user asked to scope
			// retrieval; falling through to whole-KB search would be worse than
			// returning nothing, so short-circuit with an empty response.
			logs.CtxWarnf(ctx, "ragimpl.Retrieve: all %d DocumentIDs had no mapping; returning empty hits", len(req.DocumentIDs))
			return &knowledgeModel.RetrieveResponse{}, nil
		}
	}

	if req.Query != "" {
		q := req.Query
		ragReq.Query = &q
	}
	if req.Strategy != nil {
		if req.Strategy.TopK != nil {
			topK := int(*req.Strategy.TopK)
			ragReq.TopK = &topK
		}
		if req.Strategy.MinScore != nil {
			ms := *req.Strategy.MinScore
			ragReq.MinScore = &ms
		}
		if req.Strategy.MaxTokens != nil {
			mt := int(*req.Strategy.MaxTokens)
			if mt < 1 {
				// Rag's pydantic schema requires ge=1; pre-rejecting here gives a
				// clearer error than rag's 422.
				return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
					errorx.KV("msg", fmt.Sprintf("MaxTokens must be >= 1 (got %d)", mt)))
			}
			// MaxTokens is enforced by rag at chunk boundary granularity (approximate;
			// not exact token-count cutoff). Callers needing a strict budget should
			// post-process the returned slices.
			ragReq.MaxTokens = &mt
		}
		switch req.Strategy.SearchType {
		case knowledgeModel.SearchTypeFullText:
			ragReq.SearchType = "fulltext"
		case knowledgeModel.SearchTypeHybrid:
			ragReq.SearchType = "hybrid"
		default:
			ragReq.SearchType = "semantic"
		}
		if req.Strategy.EnableQueryRewrite {
			if i.defaultLLMModelID != "" {
				if ragReq.QueryStrategy == nil {
					ragReq.QueryStrategy = map[string]any{}
				}
				ragReq.QueryStrategy["rewrite"] = true
				ragReq.QueryStrategy["llm_model_id"] = i.defaultLLMModelID
			} else {
				// EnableQueryRewrite was requested but RAG_DEFAULT_LLM_MODEL_ID
				// is unset. Rag's validator rejects {rewrite:true} without an
				// llm_model_id (40004), so dropping the enhancement is
				// preferable to failing the whole retrieval. Basic retrieval
				// still completes.
				logs.CtxWarnf(ctx, "ragimpl.Retrieve: EnableQueryRewrite=true but RAG_DEFAULT_LLM_MODEL_ID is empty; dropping rewrite to avoid rag 40004")
			}
		}
		if req.Strategy.EnableRerank {
			if i.defaultRerankModelID != "" {
				// Map-merge, not overwrite: query rewrite above may have
				// already populated QueryStrategy with rewrite/llm_model_id.
				// Rag's validator (retrieval_validator.py:294) requires
				// rerank_model_id whenever enable_rerank is true.
				if ragReq.QueryStrategy == nil {
					ragReq.QueryStrategy = map[string]any{}
				}
				ragReq.QueryStrategy["enable_rerank"] = true
				ragReq.QueryStrategy["rerank_model_id"] = i.defaultRerankModelID
			} else {
				// EnableRerank was requested but RAG_DEFAULT_RERANK_MODEL_ID
				// is unset. Mirror the rewrite-drop pattern: dropping rerank
				// keeps basic retrieval working instead of failing with rag
				// 40004 ("rerank_model_id is required when enable_rerank is
				// true").
				logs.CtxWarnf(ctx, "ragimpl.Retrieve: EnableRerank=true but RAG_DEFAULT_RERANK_MODEL_ID is empty; dropping rerank to avoid rag 40004")
			}
		}
	}

	resp, err := i.rag.Retrieve(ctx, tenant, ragReq)
	if err != nil {
		return nil, err
	}

	// Translate hits. Chunk-level int64 ids are not stable across rag yet
	// (rag returns string chunk uuids), so Slice.Info.ID is left as 0 in v1.
	slices := make([]*knowledgeModel.RetrieveSlice, 0, len(resp.Items))
	for idx := range resp.Items {
		h := resp.Items[idx]
		m, err := i.mapping.docByRagID(ctx, h.DocID)
		if err != nil {
			// Hit from a doc we don't have a coze handle for -- drift or another
			// tenant of the same rag KB; skip rather than fabricate an id.
			logs.CtxWarnf(ctx, "ragimpl.Retrieve: docByRagID(%s) failed, skipping hit: %v", h.DocID, err)
			continue
		}
		text := h.Content
		s := &knowledgeModel.Slice{
			Info:        knowledgeModel.Info{ID: 0},
			KnowledgeID: m.KBID,
			DocumentID:  m.CozeID,
			RawContent:  []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: &text}},
		}
		slices = append(slices, &knowledgeModel.RetrieveSlice{Slice: s, Score: h.Score})
	}
	return &knowledgeModel.RetrieveResponse{RetrieveSlices: slices}, nil
}
