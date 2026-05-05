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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/pkg/sonic"
)

func TestCollectFromCanvasFindsPluginAndLLMToolReferences(t *testing.T) {
	var canvas vo.Canvas
	err := sonic.UnmarshalString(`{
		"nodes": [
			{
				"id": "plugin-node",
				"type": "`+entity.NodeTypePlugin.IDStr()+`",
				"data": {
					"nodeMeta": {"title": "plugin node"},
					"inputs": {
						"apiParam": [
							{"name": "pluginID", "input": {"value": {"type": "literal", "content": "101"}}},
							{"name": "apiID", "input": {"value": {"type": "literal", "content": "201"}}},
							{"name": "pluginVersion", "input": {"value": {"type": "literal", "content": "0"}}}
						]
					}
				}
			},
			{
				"id": "llm-node",
				"type": "`+entity.NodeTypeLLM.IDStr()+`",
				"data": {
					"nodeMeta": {"title": "llm node"},
					"inputs": {
						"fcParam": {
							"pluginFCParam": {
								"pluginList": [
									{"plugin_id": "102", "api_id": "202", "plugin_version": "v1", "is_draft": false}
								]
							}
						}
					}
				}
			}
		]
	}`, &canvas)
	require.NoError(t, err)

	refs, err := CollectFromCanvas(&canvas)

	require.NoError(t, err)
	assert.ElementsMatch(t, []Reference{
		{NodeID: "plugin-node", NodeName: "plugin node", PluginID: 101, ToolID: 201, PluginVersion: "0", IsDraft: true},
		{NodeID: "llm-node", NodeName: "llm node", PluginID: 102, ToolID: 202, PluginVersion: "v1", IsDraft: false},
	}, refs)
}

func TestCollectFromCanvasStringReportsReferencedPlugin(t *testing.T) {
	canvas := &vo.Canvas{
		Nodes: []*vo.Node{
			{
				ID:   "plugin-node",
				Type: entity.NodeTypePlugin.IDStr(),
				Data: &vo.Data{
					Meta: &vo.NodeMetaFE{Title: "plugin node"},
					Inputs: &vo.Inputs{
						PluginAPIParam: &vo.PluginAPIParam{
							APIParams: []*vo.Param{
								literalParam("pluginID", "101"),
								literalParam("apiID", "201"),
								literalParam("pluginVersion", "0"),
							},
						},
					},
				},
			},
		},
	}
	canvasBytes, err := sonic.Marshal(canvas)
	require.NoError(t, err)

	refs, err := CollectFromCanvasString(string(canvasBytes))

	require.NoError(t, err)
	assert.True(t, ContainsPluginID(refs, 101))
	assert.False(t, ContainsPluginID(refs, 999))
}

func literalParam(name string, content any) *vo.Param {
	return &vo.Param{
		Name: name,
		Input: &vo.BlockInput{
			Value: &vo.BlockInputValue{
				Type:    vo.BlockInputValueTypeLiteral,
				Content: content,
			},
		},
	}
}
