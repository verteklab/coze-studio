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

		switch s.SearchType {
		case knowledgeModel.SearchTypeFullText:
			ragReq.SearchType = "bm25"
		case knowledgeModel.SearchTypeHybrid:
			ragReq.SearchType = "hybrid"
		default:
			ragReq.SearchType = "dense"
		}

		// query_strategy 4-boolean. Omit the dict entirely when all four are
		// false (matches the "no enhancement requested" wire shape).
		qs := map[string]any{}
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
