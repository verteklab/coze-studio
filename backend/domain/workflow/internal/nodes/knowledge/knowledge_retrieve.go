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

package knowledge

import (
	"context"
	"errors"
	"maps"

	"github.com/spf13/cast"

	einoSchema "github.com/cloudwego/eino/schema"

	"github.com/coze-dev/coze-studio/backend/api/model/workflow"
	crossknowledge "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge"
	knowledge "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	crossmessage "github.com/coze-dev/coze-studio/backend/crossdomain/message"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/canvas/convert"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/execute"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/nodes"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/schema"
	"github.com/coze-dev/coze-studio/backend/pkg/ctxcache"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/slices"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)

const outputList = "outputList"

type contextKey string

const chatHistoryKey contextKey = "chatHistory"

type RetrieveConfig struct {
	KnowledgeIDs       []int64
	RetrievalStrategy  *knowledge.RetrievalStrategy
	ChatHistorySetting *vo.ChatHistorySetting
}

func (r *RetrieveConfig) Adapt(_ context.Context, n *vo.Node, _ ...nodes.AdaptOption) (*schema.NodeSchema, error) {
	ns := &schema.NodeSchema{
		Key:     vo.NodeKey(n.ID),
		Type:    entity.NodeTypeKnowledgeRetriever,
		Name:    n.Data.Meta.Title,
		Configs: r,
	}

	inputs := n.Data.Inputs
	datasetListInfoParam := inputs.DatasetParam[0]
	datasetIDs := datasetListInfoParam.Input.Value.Content.([]any)
	knowledgeIDs := make([]int64, 0, len(datasetIDs))
	for _, id := range datasetIDs {
		k, err := cast.ToInt64E(id)
		if err != nil {
			return nil, err
		}
		knowledgeIDs = append(knowledgeIDs, k)
	}
	r.KnowledgeIDs = knowledgeIDs

	if inputs.ChatHistorySetting != nil {
		r.ChatHistorySetting = inputs.ChatHistorySetting
	}

	retrievalStrategy := &knowledge.RetrievalStrategy{}

	var getDesignatedParamContent = func(name string) (any, bool) {
		for _, param := range inputs.DatasetParam {
			if param.Name == name {
				return param.Input.Value.Content, true
			}
		}
		return nil, false
	}

	if content, ok := getDesignatedParamContent("topK"); ok && content != nil {
		topK, err := cast.ToInt64E(content)
		if err != nil {
			return nil, err
		}
		if topK > 0 {
			retrievalStrategy.TopK = &topK
		}
	}

	// 4-boolean query_strategy (rag wire-level keys: rewrite / expansion /
	// multi_query / enable_rerank). Frontend param names use camelCase:
	// rewrite / expansion / multiQuery / enableRerank.
	if content, ok := getDesignatedParamContent("rewrite"); ok {
		v, err := cast.ToBoolE(content)
		if err != nil {
			return nil, err
		}
		retrievalStrategy.Rewrite = v
	}
	if content, ok := getDesignatedParamContent("expansion"); ok {
		v, err := cast.ToBoolE(content)
		if err != nil {
			return nil, err
		}
		retrievalStrategy.Expansion = v
	}
	if content, ok := getDesignatedParamContent("multiQuery"); ok {
		v, err := cast.ToBoolE(content)
		if err != nil {
			return nil, err
		}
		retrievalStrategy.MultiQuery = v
	}
	if content, ok := getDesignatedParamContent("enableRerank"); ok {
		v, err := cast.ToBoolE(content)
		if err != nil {
			return nil, err
		}
		retrievalStrategy.EnableRerank = v
	}

	if content, ok := getDesignatedParamContent("strategy"); ok {
		strategy, err := cast.ToInt64E(content)
		if err != nil {
			return nil, err
		}
		searchType, err := convertRetrievalSearchType(strategy)
		if err != nil {
			return nil, err
		}
		retrievalStrategy.SearchType = searchType
	}

	if content, ok := getDesignatedParamContent("queryMode"); ok && content != nil {
		mode, err := cast.ToStringE(content)
		if err != nil {
			return nil, err
		}
		switch mode {
		case "", "text_input", "image_input", "mixed_input":
			retrievalStrategy.QueryMode = mode
		default:
			return nil, errors.New("queryMode must be one of text_input / image_input / mixed_input")
		}
	}

	if content, ok := getDesignatedParamContent("queryImage"); ok && content != nil {
		m, ok := content.(map[string]any)
		if !ok {
			return nil, errors.New("queryImage param must be an object")
		}
		qi := &knowledge.QueryImage{}
		if v, present := m["image_base64"]; present {
			s, err := cast.ToStringE(v)
			if err != nil {
				return nil, err
			}
			qi.ImageBase64 = s
		}
		if v, present := m["image_ref"]; present {
			s, err := cast.ToStringE(v)
			if err != nil {
				return nil, err
			}
			qi.ImageRef = s
		}
		if qi.ImageBase64 != "" || qi.ImageRef != "" {
			retrievalStrategy.QueryImage = qi
		}
	}

	if content, ok := getDesignatedParamContent("targetChunkTypes"); ok && content != nil {
		raw, ok := content.([]any)
		if !ok {
			return nil, errors.New("targetChunkTypes param must be a list")
		}
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			s, err := cast.ToStringE(v)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		if len(out) > 0 {
			retrievalStrategy.TargetChunkTypes = out
		}
	}

	if content, ok := getDesignatedParamContent("filters"); ok && content != nil {
		m, ok := content.(map[string]any)
		if !ok {
			return nil, errors.New("filters param must be an object")
		}
		if len(m) > 0 {
			retrievalStrategy.Filters = m
		}
	}

	if content, ok := getDesignatedParamContent("retrievers"); ok && content != nil {
		raw, ok := content.([]any)
		if !ok {
			return nil, errors.New("retrievers param must be a list")
		}
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			s, err := cast.ToStringE(v)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		if len(out) > 0 {
			retrievalStrategy.Retrievers = out
		}
	}

	if content, ok := getDesignatedParamContent("fusionPolicy"); ok && content != nil {
		m, ok := content.(map[string]any)
		if !ok {
			return nil, errors.New("fusionPolicy param must be an object")
		}
		if len(m) > 0 {
			retrievalStrategy.FusionPolicy = m
		}
	}

	if content, ok := getDesignatedParamContent("retrieverParams"); ok && content != nil {
		m, ok := content.(map[string]any)
		if !ok {
			return nil, errors.New("retrieverParams param must be an object")
		}
		if len(m) > 0 {
			retrievalStrategy.RetrieverParams = m
		}
	}

	r.RetrievalStrategy = retrievalStrategy

	if err := convert.SetInputsForNodeSchema(n, ns); err != nil {
		return nil, err
	}

	if err := convert.SetOutputTypesForNodeSchema(n, ns); err != nil {
		return nil, err
	}

	return ns, nil
}

func (r *RetrieveConfig) Build(_ context.Context, _ *schema.NodeSchema, _ ...schema.BuildOption) (any, error) {
	if len(r.KnowledgeIDs) == 0 {
		return nil, errors.New("knowledge ids are required")
	}

	if r.RetrievalStrategy == nil {
		return nil, errors.New("retrieval strategy is required")
	}

	return &Retrieve{
		knowledgeIDs:       r.KnowledgeIDs,
		retrievalStrategy:  r.RetrievalStrategy,
		ChatHistorySetting: r.ChatHistorySetting,
	}, nil
}

func (c *RetrieveConfig) ChatHistoryEnabled() bool {
	return c.ChatHistorySetting != nil && c.ChatHistorySetting.EnableChatHistory
}

func (c *RetrieveConfig) ChatHistoryRounds() int64 {
	if c.ChatHistorySetting == nil {
		return 0
	}
	return c.ChatHistorySetting.ChatHistoryRound
}

type Retrieve struct {
	knowledgeIDs       []int64
	retrievalStrategy  *knowledge.RetrievalStrategy
	ChatHistorySetting *vo.ChatHistorySetting
}

func (kr *Retrieve) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	query, ok := input["Query"].(string)
	if !ok {
		return nil, errors.New("capital query key is required")
	}

	req := &knowledge.RetrieveRequest{
		Query:        query,
		KnowledgeIDs: kr.knowledgeIDs,
		ChatHistory:  kr.GetChatHistoryOrNil(ctx, kr.ChatHistorySetting),
		Strategy:     kr.retrievalStrategy,
	}

	response, err := crossknowledge.DefaultSVC().Retrieve(ctx, req)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	result[outputList] = slices.Transform(response.RetrieveSlices, func(m *knowledge.RetrieveSlice) any {
		return map[string]any{
			"documentId": m.Slice.DocumentID,
			"output":     m.Slice.GetSliceContent(),
		}
	})

	return result, nil
}

func (kr *Retrieve) GetChatHistoryOrNil(ctx context.Context, ChatHistorySetting *vo.ChatHistorySetting) []*einoSchema.Message {
	if ChatHistorySetting == nil || !ChatHistorySetting.EnableChatHistory {
		return nil
	}

	exeCtx := execute.GetExeCtx(ctx)
	if exeCtx == nil {
		logs.CtxWarnf(ctx, "execute context is nil, skipping chat history")
		return nil
	}
	if exeCtx.ExeCfg.WorkflowMode != workflow.WorkflowMode_ChatFlow {
		return nil
	}

	historyMessages, ok := ctxcache.Get[[]*einoSchema.Message](ctx, chatHistoryKey)

	if !ok || len(historyMessages) == 0 {
		logs.CtxWarnf(ctx, "conversation history is empty")
		return nil
	}
	return historyMessages
}

func (kr *Retrieve) ToCallbackInput(ctx context.Context, in map[string]any) (
	*nodes.StructuredCallbackInput, error) {
	if kr.ChatHistorySetting == nil || !kr.ChatHistorySetting.EnableChatHistory {
		return &nodes.StructuredCallbackInput{Input: in}, nil
	}

	var messages []*crossmessage.WfMessage
	var scMessages []*einoSchema.Message
	var sectionID *int64
	execCtx := execute.GetExeCtx(ctx)
	if execCtx != nil {
		messages = execCtx.ExeCfg.ConversationHistory
		scMessages = execCtx.ExeCfg.ConversationHistorySchemaMessages
		sectionID = execCtx.ExeCfg.SectionID
	}

	ret := map[string]any{
		"chatHistory": []any{},
	}
	maps.Copy(ret, in)

	if len(messages) == 0 {
		return &nodes.StructuredCallbackInput{Input: ret}, nil
	}

	if sectionID != nil && messages[0].SectionID != *sectionID {
		return &nodes.StructuredCallbackInput{Input: ret}, nil
	}

	maxRounds := int(kr.ChatHistorySetting.ChatHistoryRound)
	if execCtx != nil && execCtx.ExeCfg.MaxHistoryRounds != nil {
		maxRounds = min(int(*execCtx.ExeCfg.MaxHistoryRounds), maxRounds)
	}

	count := 0
	startIdx := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == einoSchema.User {
			count++
		}
		if count >= maxRounds {
			startIdx = i
			break
		}
	}

	var historyMessages []any
	for _, msg := range messages[startIdx:] {
		content, err := nodes.ConvertMessageToString(ctx, msg)
		if err != nil {
			logs.CtxWarnf(ctx, "failed to convert message to string: %v", err)
			continue
		}
		historyMessages = append(historyMessages, map[string]any{
			"role":    string(msg.Role),
			"content": content,
		})
	}
	ctxcache.Store(ctx, chatHistoryKey, scMessages[startIdx:])

	ret["chatHistory"] = historyMessages
	return &nodes.StructuredCallbackInput{Input: ret}, nil
}
