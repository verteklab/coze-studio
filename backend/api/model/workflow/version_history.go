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

package workflow

import "github.com/coze-dev/coze-studio/backend/api/model/base"

type VersionHistoryListRequest struct {
	SpaceID    string      `json:"space_id" form:"space_id" query:"space_id" vd:"len($)>0"`
	WorkflowID string      `json:"workflow_id" form:"workflow_id" query:"workflow_id" vd:"len($)>0"`
	Type       OperateType `json:"type" form:"type" query:"type"`
	Limit      *int        `json:"limit,omitempty" form:"limit" query:"limit"`
	CommitIDs  []string    `json:"commit_ids,omitempty" form:"commit_ids" query:"commit_ids"`
	Cursor     *string     `json:"cursor,omitempty" form:"cursor" query:"cursor"`
	OrderBy    *int        `json:"order_by,omitempty" form:"order_by" query:"order_by"`
	Base       *base.Base  `json:"Base,omitempty" form:"Base" query:"Base"`
}

type VersionHistoryListResponse struct {
	Data     *VersionHistoryListData `json:"data"`
	Code     int64                   `json:"code"`
	Msg      string                  `json:"msg"`
	BaseResp *base.BaseResp          `json:"BaseResp,omitempty"`
}

type VersionHistoryListData struct {
	VersionList []*VersionMetaInfo `json:"version_list"`
	Cursor      string             `json:"cursor,omitempty"`
	HasMore     bool               `json:"has_more"`
}

type VersionMetaInfo struct {
	WorkflowID     string       `json:"workflow_id,omitempty"`
	SpaceID        string       `json:"space_id,omitempty"`
	CommitID       string       `json:"commit_id,omitempty"`
	SubmitCommitID string       `json:"submit_commit_id,omitempty"`
	CreateTime     int64        `json:"create_time,omitempty"`
	UpdateTime     int64        `json:"update_time,omitempty"`
	Env            string       `json:"env,omitempty"`
	Desc           string       `json:"desc,omitempty"`
	User           *VersionUser `json:"user,omitempty"`
	Type           OperateType  `json:"type,omitempty"`
	Offline        bool         `json:"offline,omitempty"`
	IsDelete       bool         `json:"is_delete,omitempty"`
	Version        string       `json:"version,omitempty"`
	VersionType    int          `json:"version_type,omitempty"`
}

type VersionUser struct {
	UserID     int64  `json:"user_id,string,omitempty"`
	UserName   string `json:"user_name,omitempty"`
	UserAvatar string `json:"user_avatar,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
}

type RevertDraftRequest struct {
	SpaceID    string      `json:"space_id" form:"space_id" query:"space_id" vd:"len($)>0"`
	WorkflowID string      `json:"workflow_id" form:"workflow_id" query:"workflow_id" vd:"len($)>0"`
	CommitID   string      `json:"commit_id" form:"commit_id" query:"commit_id" vd:"len($)>0"`
	Type       OperateType `json:"type" form:"type" query:"type"`
	Env        *string     `json:"env,omitempty" form:"env" query:"env"`
	Base       *base.Base  `json:"Base,omitempty" form:"Base" query:"Base"`
}

type RevertDraftResponse struct {
	Data     *RevertDraftData `json:"data"`
	Code     int64            `json:"code"`
	Msg      string           `json:"msg"`
	BaseResp *base.BaseResp   `json:"BaseResp,omitempty"`
}

type RevertDraftData struct {
	SubmitCommitID string `json:"submit_commit_id,omitempty"`
}
