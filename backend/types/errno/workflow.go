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

package errno

import (
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx/code"
)

const (
	ErrWorkflowNotPublished                        = 720702011
	ErrMissingRequiredParam                        = 720702002
	ErrInterruptNotSupported                       = 720702078
	ErrInvalidParameter                            = 720702001
	ErrArrIndexOutOfRange                          = 720712014
	ErrWorkflowExecuteFail                         = 720701013
	ErrCodeExecuteFail                             = 305000002
	ErrQuestionOptionsEmpty                        = 720712049
	ErrNodeOutputParseFail                         = 720712023
	ErrWorkflowTimeout                             = 720702085
	ErrWorkflowNotFound                            = 720702004
	ErrSerializationDeserializationFail            = 720701011
	ErrInternalBadRequest                          = 720701007
	ErrSchemaConversionFail                        = 720702089
	ErrWorkflowCompileFail                         = 720701003
	ErrPluginAPIErr                                = 720701004
	ErrConversationNameIsDuplicated                = 720702200
	ErrConversationOfAppNotFound                   = 720702201
	ErrWorkflowNameIsDuplicated                    = 720702202
	ErrWorkflowVersionNotIncremental               = 720702203
	ErrConversationNodeInvalidOperation            = 720702250
	ErrOnlyDefaultConversationAllowInAgentScenario = 720712033
	ErrConversationNodesNotAvailable               = 702093204
)

const (
	ErrConversationNodeOperationFail    = 777777782
	ErrMessageNodeOperationFail         = 777777781
	ErrChatFlowRoleOperationFail        = 777777780
	ErrConversationOfAppOperationFail   = 777777779
	ErrWorkflowSpecifiedVersionNotFound = 777777778
	ErrWorkflowCanceledByUser           = 777777777
	ErrNodeTimeout                      = 777777776
	ErrWorkflowOperationFail            = 777777775
	ErrIndexingNilArray                 = 777777774
	ErrLLMStructuredOutputParseFail     = 777777773
	ErrCreateNodeFail                   = 777777772
	ErrWorkflowSnapshotNotFound         = 777777771
	ErrNotifyWorkflowResourceChangeErr  = 777777770
	ErrInvalidVersionName               = 777777769
	ErrPluginIDNotFound                 = 777777768
	ErrTOSError                         = 777777767
	ErrToolIDNotFound                   = 777777766
	ErrAuthorizationRequired            = 777777765
	ErrVariablesAPIFail                 = 777777764
	ErrInputFieldMissing                = 777777763
	ErrConversationNotFoundForOperation = 777777762
)

// stability problems
const (
	ErrDatabaseError = 720700801
	ErrRedisError    = 720700803
	ErrIDGenError    = 720700808
)

const (
	ErrOpenAPIWorkflowNotPublished  = 6031
	ErrOpenAPIBadRequest            = 4000
	ErrOpenAPIInterruptNotSupported = 6039
	ErrOpenAPIWorkflowTimeout       = 6023
)

func init() {
	code.Register(
		ErrWorkflowNotPublished,
		"工作流未发布，无法对未发布的工作流执行该操作，请先发布工作流后再试。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrMissingRequiredParam,
		"缺少必填参数：『{param}』，请检查 API 文档并确认所有必填字段都已填写。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrInterruptNotSupported,
		"同步请求不支持中断，如需可中断操作，请改用异步请求。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrInvalidParameter,
		"请求参数无效，请检查输入，确保所有必填字段格式正确并在允许范围内。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrArrIndexOutOfRange,
		"数组 {arr_name} 下标越界：请求的下标 {req_index} 超出数组长度 {arr_len}，请确认下标在数组的有效范围内，更多详情可参考 debug_url。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrIndexingNilArray,
		"数组 {arr_name} 为空：无法读取下标 {req_index} 对应的元素。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowExecuteFail,
		"工作流执行失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowOperationFail,
		"工作流操作失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrChatFlowRoleOperationFail,
		"ChatFlowRole 操作失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrMessageNodeOperationFail,
		"消息节点操作失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationNodeOperationFail,
		"会话节点操作失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrCodeExecuteFail,
		"函数执行失败，请检查函数代码。详情：{detail}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrQuestionOptionsEmpty,
		"问题选项为空",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrNodeOutputParseFail,
		"节点输出解析失败：{warnings}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowCanceledByUser,
		"工作流已被用户取消",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationOfAppOperationFail,
		"会话管理操作失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrNodeTimeout,
		"节点执行超时",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowTimeout,
		"工作流执行超时，请检查是否存在长耗时操作，尝试优化后重试。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrLLMStructuredOutputParseFail,
		"大模型结构化输出解析失败，详情请参考大模型的原始输出。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrCreateNodeFail,
		"创建节点 {node_name} 失败：{cause}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrDatabaseError,
		"数据库操作失败",
		code.WithAffectStability(true),
	)
	code.Register(
		ErrRedisError,
		"Redis 操作失败",
		code.WithAffectStability(true),
	)
	code.Register(
		ErrIDGenError,
		"ID 生成器调用失败",
		code.WithAffectStability(true),
	)

	code.Register(
		ErrWorkflowNotFound,
		"工作流 {id} 不存在，请确认该工作流是否存在且未被删除。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationOfAppNotFound,
		"会话不存在，请确认对应的应用会话是否存在。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrSerializationDeserializationFail,
		"数据序列化/反序列化失败，请联系技术支持。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowSnapshotNotFound,
		"工作流 {id} 的快照 {commit_id} 不存在，请联系技术支持。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrInternalBadRequest,
		"{scene} 的请求参数存在非法字段，请联系技术支持。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrSchemaConversionFail,
		"Schema 转换失败，请联系技术支持。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowCompileFail,
		"工作流编译失败，请联系技术支持。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrNotifyWorkflowResourceChangeErr,
		"通知工作流资源变更失败，请稍后重试。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrInvalidVersionName,
		"工作流版本名称不合法",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrPluginAPIErr,
		"插件 API 调用异常",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationNameIsDuplicated,
		"会话名称 {name} 已存在",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowNameIsDuplicated,
		"工作流名称『{name}』已存在，请换一个名称",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrWorkflowVersionNotIncremental,
		"版本号必须递增，线上版本 {old_version}，当前版本 {new_version}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrPluginIDNotFound,
		"插件 {id} 不存在",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrTOSError,
		"对象存储操作失败",
		code.WithAffectStability(true),
	)

	code.Register(
		ErrToolIDNotFound,
		"工具 {id} 不存在",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrAuthorizationRequired,
		"授权失败：{extra}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrVariablesAPIFail,
		"变量 API 调用失败",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrInputFieldMissing,
		"输入参数 {name} 不存在",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationNotFoundForOperation,
		"会话不存在，请先创建会话后再执行相关操作。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationNodesNotAvailable,
		"会话节点在智能体场景下不可用，需要绑定应用。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrConversationNodeInvalidOperation,
		"仅允许修改或删除通过节点创建的会话。",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrOnlyDefaultConversationAllowInAgentScenario,
		"智能体场景下仅允许使用默认会话",
		code.WithAffectStability(false),
	)

}

var errnoMap = map[int]int{
	ErrWorkflowNotPublished:  ErrOpenAPIWorkflowNotPublished,
	ErrMissingRequiredParam:  ErrOpenAPIBadRequest,
	ErrInterruptNotSupported: ErrOpenAPIInterruptNotSupported,
	ErrInvalidParameter:      ErrOpenAPIBadRequest,
	ErrArrIndexOutOfRange:    ErrOpenAPIBadRequest,
	ErrWorkflowTimeout:       ErrOpenAPIWorkflowTimeout,
}

func CodeForOpenAPI(err errorx.StatusError) int {
	if err == nil {
		return 0
	}

	if c, ok := errnoMap[int(err.Code())]; ok {
		return c
	}

	return int(err.Code())
}
