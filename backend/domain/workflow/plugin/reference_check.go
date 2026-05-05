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
	"errors"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/pluginref"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

type ReferenceCheckResult struct {
	InvalidReferences []InvalidReference
}

type InvalidReference struct {
	NodeID   string
	NodeName string
	PluginID int64
	ToolID   int64
	Code     int32
	Message  string
}

func CheckReferences(ctx context.Context, refs []pluginref.Reference) (*ReferenceCheckResult, error) {
	result := &ReferenceCheckResult{InvalidReferences: make([]InvalidReference, 0)}
	for _, ref := range refs {
		_, _, err := getPluginsWithTools(ctx, &vo.PluginEntity{
			PluginID:      ref.PluginID,
			PluginVersion: &ref.PluginVersion,
			PluginFrom:    ref.PluginFrom,
		}, []int64{ref.ToolID}, ref.IsDraft)
		if err == nil {
			continue
		}
		code, ok := workflowErrorCode(err)
		if !ok {
			return nil, err
		}
		if code != int32(errno.ErrPluginIDNotFound) && code != int32(errno.ErrToolIDNotFound) {
			return nil, err
		}
		result.InvalidReferences = append(result.InvalidReferences, InvalidReference{
			NodeID:   ref.NodeID,
			NodeName: ref.NodeName,
			PluginID: ref.PluginID,
			ToolID:   ref.ToolID,
			Code:     code,
			Message:  err.Error(),
		})
	}
	return result, nil
}

func workflowErrorCode(err error) (int32, bool) {
	for err != nil {
		var statusErr errorx.StatusError
		if errors.As(err, &statusErr) {
			return statusErr.Code(), true
		}
		err = errors.Unwrap(err)
	}
	return 0, false
}
