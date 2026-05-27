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
	"errors"
	"fmt"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)

// Retrieve queries rag for chunks matching the request and translates each
// hit back to a coze entity. Tenant isolation is enforced by rag (which
// filters its KB index on tenant_id) — we don't re-check on the coze side
// because Phase 1 has exactly one global tenant.
func (i *Impl) Retrieve(ctx context.Context, req *service.RetrieveRequest) (*knowledgeModel.RetrieveResponse, error) {
	if len(req.KnowledgeIDs) == 0 {
		return nil, errors.New("ragimpl.Retrieve: at least one knowledge_id required")
	}

	// Empty-query guard. The deployed rag-web container's pydantic schema
	// rejects requests where query_mode=text_input but no query is present,
	// AND rejects requests where neither query nor query_image is provided.
	// Catch this early with a clear coze-side message instead of letting it
	// surface as a generic 40004 "either query or query_image must be
	// provided" from rag.
	hasQuery := req.Query != ""
	hasImage := req.Strategy != nil && req.Strategy.QueryImage != nil &&
		(req.Strategy.QueryImage.ImageBase64 != "" || req.Strategy.QueryImage.ImageRef != "")
	if !hasQuery && !hasImage {
		return nil, errors.New("ragimpl.Retrieve: query is empty (and no query_image provided); cannot retrieve without a query")
	}

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

	if req.Query != "" {
		q := req.Query
		ragReq.Query = &q
	}

	if req.Strategy != nil {
		s := req.Strategy

		if s.TopK != nil && *s.TopK > 0 {
			tk := int(*s.TopK)
			ragReq.TopK = &tk
		}

		// search_type also implicitly pins the retrievers list. Without this,
		// rag's _resolve_retrievers auto-derives BOTH dense + bm25 from
		// target_chunk_types=[text_chunk] regardless of the search_type
		// string, so a "dense" search_type still triggers a BM25 fanout that
		// can blow up ES's maxClauseCount=1024 limit when multi_query /
		// expansion is enabled (the LLM produces N query variants and BM25
		// turns each term × variant into a bool clause).
		//
		// If the caller explicitly populated s.Retrievers (Advanced section
		// in the workflow node UI), we honor that and don't override.
		switch s.SearchType {
		case knowledgeModel.SearchTypeFullText:
			ragReq.SearchType = "bm25"
			if len(s.Retrievers) == 0 {
				ragReq.Retrievers = []string{"bm25"}
			}
		case knowledgeModel.SearchTypeHybrid:
			ragReq.SearchType = "hybrid"
			// leave retrievers unset; rag derives dense+bm25 (the point of hybrid)
		default:
			ragReq.SearchType = "dense"
			if len(s.Retrievers) == 0 {
				ragReq.Retrievers = []string{"dense"}
			}
		}

		// query_strategy. The deployed rag-web container's validator accepts
		// 6 keys total: rewrite / expansion / multi_query / enable_rerank
		// (booleans) + llm_model_id / rerank_model_id (non-empty strings).
		// Any of the first 3 booleans true requires llm_model_id; rerank
		// requires rerank_model_id. We inject the env-configured defaults
		// and fail-fast if env is empty so the caller gets a clearer error
		// than rag's generic 40004.
		qs := map[string]any{}
		needsLLM := s.Rewrite || s.Expansion || s.MultiQuery
		if s.Rewrite {
			qs["rewrite"] = true
		}
		if s.Expansion {
			qs["expansion"] = true
		}
		if s.MultiQuery {
			qs["multi_query"] = true
		}
		if s.EnableRerank {
			qs["enable_rerank"] = true
		}
		if needsLLM {
			if i.defaultLLMModelID == "" {
				return nil, fmt.Errorf("ragimpl.Retrieve: query enhancement requested (rewrite/expansion/multi_query) but RAG_DEFAULT_LLM_MODEL_ID is unset; either configure the env or disable enhancement on the node")
			}
			qs["llm_model_id"] = i.defaultLLMModelID
		}
		if s.EnableRerank {
			if i.defaultRerankModelID == "" {
				return nil, fmt.Errorf("ragimpl.Retrieve: rerank requested but RAG_DEFAULT_RERANK_MODEL_ID is unset; either configure the env or disable rerank on the node")
			}
			qs["rerank_model_id"] = i.defaultRerankModelID
		}
		if len(qs) > 0 {
			ragReq.QueryStrategy = qs
		}

		// New top-level rag fields. Each is forwarded only when the caller
		// explicitly set a non-zero value; zero values let rag use its
		// own defaults.
		if s.QueryMode != "" {
			ragReq.QueryMode = s.QueryMode
		}
		if s.QueryImage != nil {
			ragReq.QueryImage = &contract.QueryImage{
				ImageBase64: s.QueryImage.ImageBase64,
				ImageRef:    s.QueryImage.ImageRef,
			}
		}
		if len(s.TargetChunkTypes) > 0 {
			ragReq.TargetChunkTypes = s.TargetChunkTypes
		}
		if len(s.Filters) > 0 {
			ragReq.Filters = s.Filters
		}
		if len(s.Retrievers) > 0 {
			ragReq.Retrievers = s.Retrievers
		}
		if len(s.FusionPolicy) > 0 {
			ragReq.FusionPolicy = s.FusionPolicy
		}
		if len(s.RetrieverParams) > 0 {
			ragReq.RetrieverParams = s.RetrieverParams
		}
	}

	// Debug log of the outgoing rag retrieval body. Marshals defensively —
	// json.Marshal on a request struct only fails on cyclic / unsupported
	// types, which can't happen here. Cheap (one alloc per call) and the
	// log is the fastest path to debugging "what did coze send to rag?"
	// without tcpdump or rag-side logging tricks.
	if body, mErr := json.Marshal(ragReq); mErr == nil {
		logs.CtxInfof(ctx, "ragimpl.Retrieve: tenant=%s body=%s", tenant, string(body))
	}

	resp, err := i.rag.Retrieve(ctx, tenant, ragReq)
	if err != nil {
		return nil, err
	}

	slices := make([]*knowledgeModel.RetrieveSlice, 0, len(resp.Items))
	for idx := range resp.Items {
		h := resp.Items[idx]
		m, err := i.mapping.docByRagID(ctx, h.DocID)
		if err != nil {
			logs.CtxWarnf(ctx, "ragimpl.Retrieve: docByRagID(%s) failed, skipping hit: %v", h.DocID, err)
			continue
		}
		cozeSliceID := i.resolveCozeSliceID(ctx, h.ChunkID, h.DocID, m.CozeID, m.CreatorID)
		text := h.Content
		s := &knowledgeModel.Slice{
			Info:        knowledgeModel.Info{ID: cozeSliceID, CreatorID: m.CreatorID},
			KnowledgeID: m.KBID,
			DocumentID:  m.CozeID,
			RawContent:  []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: &text}},
		}
		slices = append(slices, &knowledgeModel.RetrieveSlice{Slice: s, Score: h.Score})
	}
	return &knowledgeModel.RetrieveResponse{RetrieveSlices: slices}, nil
}
