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

package pluginref

import (
	"fmt"
	"strconv"

	"github.com/coze-dev/coze-studio/backend/api/model/app/bot_common"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/slices"
	"github.com/coze-dev/coze-studio/backend/pkg/sonic"
)

type Reference struct {
	NodeID        string
	NodeName      string
	PluginID      int64
	ToolID        int64
	PluginVersion string
	IsDraft       bool
	PluginFrom    *bot_common.PluginFrom
}

func CollectFromCanvasString(canvasSchema string) ([]Reference, error) {
	if canvasSchema == "" {
		return nil, nil
	}
	var canvas vo.Canvas
	if err := sonic.UnmarshalString(canvasSchema, &canvas); err != nil {
		return nil, err
	}
	return CollectFromCanvas(&canvas)
}

func CollectFromCanvas(canvas *vo.Canvas) ([]Reference, error) {
	if canvas == nil {
		return nil, nil
	}
	refs := make([]Reference, 0)
	if err := collectFromNodes(canvas.Nodes, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func ContainsPluginID(refs []Reference, pluginID int64) bool {
	for _, ref := range refs {
		if ref.PluginID == pluginID {
			return true
		}
	}
	return false
}

func collectFromNodes(nodes []*vo.Node, refs *[]Reference) error {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		nType := entity.IDStrToNodeType(node.Type)
		meta := entity.NodeMetaByNodeType(nType)
		if meta != nil && meta.UsePlugin {
			ref, ok, err := collectPluginNodeReference(node)
			if err != nil {
				return err
			}
			if ok {
				*refs = append(*refs, ref)
			}
		}

		if nType == entity.NodeTypeLLM && node.Data != nil && node.Data.Inputs != nil && node.Data.Inputs.LLM != nil &&
			node.Data.Inputs.FCParam != nil && node.Data.Inputs.FCParam.PluginFCParam != nil {
			for _, pluginInfo := range node.Data.Inputs.FCParam.PluginFCParam.PluginList {
				pluginID, err := strconv.ParseInt(pluginInfo.PluginID, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid plugin id %q in node %s: %w", pluginInfo.PluginID, node.ID, err)
				}
				toolID, err := strconv.ParseInt(pluginInfo.ApiId, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid api id %q in node %s: %w", pluginInfo.ApiId, node.ID, err)
				}
				*refs = append(*refs, Reference{
					NodeID:        node.ID,
					NodeName:      nodeName(node),
					PluginID:      pluginID,
					ToolID:        toolID,
					PluginVersion: pluginInfo.PluginVersion,
					IsDraft:       pluginInfo.IsDraft,
					PluginFrom:    pluginInfo.PluginFrom,
				})
			}
		}

		if len(node.Blocks) > 0 {
			if err := collectFromNodes(node.Blocks, refs); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectPluginNodeReference(node *vo.Node) (Reference, bool, error) {
	if node.Data == nil || node.Data.Inputs == nil || len(node.Data.Inputs.APIParams) == 0 {
		return Reference{}, false, nil
	}
	apiParams := slices.ToMap(node.Data.Inputs.APIParams, func(e *vo.Param) (string, *vo.Param) {
		return e.Name, e
	})
	pluginID, err := parseAPIParamInt64(apiParams, "pluginID")
	if err != nil {
		return Reference{}, false, fmt.Errorf("node %s pluginID invalid: %w", node.ID, err)
	}
	toolID, err := parseAPIParamInt64(apiParams, "apiID")
	if err != nil {
		return Reference{}, false, fmt.Errorf("node %s apiID invalid: %w", node.ID, err)
	}
	pluginVersion, err := parseAPIParamString(apiParams, "pluginVersion")
	if err != nil {
		return Reference{}, false, fmt.Errorf("node %s pluginVersion invalid: %w", node.ID, err)
	}
	return Reference{
		NodeID:        node.ID,
		NodeName:      nodeName(node),
		PluginID:      pluginID,
		ToolID:        toolID,
		PluginVersion: pluginVersion,
		IsDraft:       pluginVersion == "0",
		PluginFrom:    node.Data.Inputs.PluginFrom,
	}, true, nil
}

func parseAPIParamInt64(params map[string]*vo.Param, name string) (int64, error) {
	value, err := parseAPIParamString(params, name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseAPIParamString(params map[string]*vo.Param, name string) (string, error) {
	param, ok := params[name]
	if !ok || param == nil || param.Input == nil || param.Input.Value == nil {
		return "", fmt.Errorf("%s param is not found", name)
	}
	value, ok := param.Input.Value.Content.(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%s param is not a string", name)
	}
	return value, nil
}

func nodeName(node *vo.Node) string {
	if node != nil && node.Data != nil && node.Data.Meta != nil {
		return node.Data.Meta.Title
	}
	return ""
}
