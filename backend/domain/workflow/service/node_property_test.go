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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	"github.com/coze-dev/coze-studio/backend/crossdomain/plugin/pluginmock"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/pkg/sonic"
)

func TestInvalidPluginReferenceReasonMarksDeletedPlugin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPlugin := pluginmock.NewMockPluginService(ctrl)
	crossplugin.SetDefaultSVC(mockPlugin)
	t.Cleanup(func() { crossplugin.SetDefaultSVC(nil) })

	mockPlugin.EXPECT().
		MGetDraftPlugins(gomock.Any(), []int64{101}).
		Return(nil, nil)

	var canvas vo.Canvas
	require.NoError(t, sonic.UnmarshalString(pluginNodeOnlyCanvasJSON(), &canvas))
	reason, invalid, err := (&impl{}).invalidPluginReferenceReason(context.Background(), canvas.Nodes[0])

	require.NoError(t, err)
	assert.True(t, invalid)
	assert.Equal(t, "plugin_deleted", reason)
}

func pluginNodeOnlyCanvasJSON() string {
	return `{
		"nodes": [
			{
				"id": "plugin-node",
				"type": "4",
				"data": {
					"nodeMeta": {"title": "Plugin Node"},
					"inputs": {
						"apiParam": [
							{"name": "pluginID", "input": {"value": {"type": "literal", "content": "101"}}},
							{"name": "apiID", "input": {"value": {"type": "literal", "content": "201"}}},
							{"name": "pluginVersion", "input": {"value": {"type": "literal", "content": "0"}}}
						]
					}
				}
			}
		]
	}`
}
