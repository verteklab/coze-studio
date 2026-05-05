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

package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pluginModel "github.com/coze-dev/coze-studio/backend/crossdomain/plugin/model"
	workflowModel "github.com/coze-dev/coze-studio/backend/crossdomain/workflow/model"
	pluginEntity "github.com/coze-dev/coze-studio/backend/domain/plugin/entity"
	workflowDomain "github.com/coze-dev/coze-studio/backend/domain/workflow"
	workflowEntity "github.com/coze-dev/coze-studio/backend/domain/workflow/entity"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	mockWorkflow "github.com/coze-dev/coze-studio/backend/internal/mock/domain/workflow"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
)

func TestFindWorkflowsReferencingPluginFindsReferencesInSameApp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mockWorkflow.NewMockRepository(ctrl)
	workflowDomain.SetRepository(repo)
	t.Cleanup(func() { workflowDomain.SetRepository(nil) })

	appID := int64(10)
	repo.EXPECT().
		MGetDrafts(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, policy *vo.MGetPolicy) ([]*workflowEntity.Workflow, int64, error) {
			assert.Equal(t, workflowModel.FromDraft, policy.QType)
			require.NotNil(t, policy.AppID)
			assert.Equal(t, appID, *policy.AppID)
			return []*workflowEntity.Workflow{
				{ID: 99, CanvasInfo: &vo.CanvasInfo{Canvas: referencedPluginCanvasJSON()}},
			}, int64(0), nil
		})
	repo.EXPECT().
		MGetLatestVersion(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, policy *vo.MGetPolicy) ([]*workflowEntity.Workflow, int64, error) {
			assert.Equal(t, workflowModel.FromLatestVersion, policy.QType)
			require.NotNil(t, policy.AppID)
			assert.Equal(t, appID, *policy.AppID)
			return nil, int64(0), nil
		})

	service := &PluginApplicationService{}
	refs, err := service.findWorkflowsReferencingPlugin(context.Background(), pluginEntity.NewPluginInfo(&pluginModel.PluginInfo{
		ID:    101,
		APPID: ptr.Of(appID),
	}))

	require.NoError(t, err)
	assert.Equal(t, []int64{99}, refs)
}

func referencedPluginCanvasJSON() string {
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
