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

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	"github.com/coze-dev/coze-studio/backend/crossdomain/plugin/model"
	"github.com/coze-dev/coze-studio/backend/crossdomain/plugin/pluginmock"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/pluginref"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

func TestCheckReferencesReturnsMissingPluginReference(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPlugin := pluginmock.NewMockPluginService(ctrl)
	crossplugin.SetDefaultSVC(mockPlugin)
	t.Cleanup(func() { crossplugin.SetDefaultSVC(nil) })

	mockPlugin.EXPECT().
		MGetDraftPlugins(gomock.Any(), []int64{101}).
		Return(nil, nil)

	result, err := CheckReferences(context.Background(), []pluginref.Reference{
		{NodeID: "plugin-node", NodeName: "Plugin Node", PluginID: 101, ToolID: 201, PluginVersion: "0", IsDraft: true},
	})

	require.NoError(t, err)
	require.Len(t, result.InvalidReferences, 1)
	assert.Equal(t, "plugin-node", result.InvalidReferences[0].NodeID)
	assert.Equal(t, int64(101), result.InvalidReferences[0].PluginID)
	assert.Equal(t, int32(errno.ErrPluginIDNotFound), result.InvalidReferences[0].Code)
}

func TestCheckReferencesReturnsMissingToolReference(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPlugin := pluginmock.NewMockPluginService(ctrl)
	crossplugin.SetDefaultSVC(mockPlugin)
	t.Cleanup(func() { crossplugin.SetDefaultSVC(nil) })

	mockPlugin.EXPECT().
		MGetDraftPlugins(gomock.Any(), []int64{101}).
		Return([]*model.PluginInfo{{ID: 101}}, nil)
	mockPlugin.EXPECT().
		MGetDraftTools(gomock.Any(), []int64{201}).
		Return(nil, nil)

	result, err := CheckReferences(context.Background(), []pluginref.Reference{
		{NodeID: "plugin-node", NodeName: "Plugin Node", PluginID: 101, ToolID: 201, PluginVersion: "0", IsDraft: true},
	})

	require.NoError(t, err)
	require.Len(t, result.InvalidReferences, 1)
	assert.Equal(t, int32(errno.ErrToolIDNotFound), result.InvalidReferences[0].Code)
}

func TestCheckReferencesPassesWhenPluginAndToolExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPlugin := pluginmock.NewMockPluginService(ctrl)
	crossplugin.SetDefaultSVC(mockPlugin)
	t.Cleanup(func() { crossplugin.SetDefaultSVC(nil) })

	mockPlugin.EXPECT().
		MGetDraftPlugins(gomock.Any(), []int64{101}).
		Return([]*model.PluginInfo{{ID: 101}}, nil)
	mockPlugin.EXPECT().
		MGetDraftTools(gomock.Any(), []int64{201}).
		Return([]*model.ToolInfo{{ID: 201, PluginID: 101}}, nil)

	result, err := CheckReferences(context.Background(), []pluginref.Reference{
		{NodeID: "plugin-node", NodeName: "Plugin Node", PluginID: 101, ToolID: 201, PluginVersion: "0", IsDraft: true},
	})

	require.NoError(t, err)
	assert.Empty(t, result.InvalidReferences)
}
