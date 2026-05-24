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

package search

import (
	"context"
	"fmt"

	"github.com/coze-dev/coze-studio/backend/api/model/data/database/table"
	"github.com/coze-dev/coze-studio/backend/api/model/resource/common"
	"github.com/coze-dev/coze-studio/backend/api/model/workflow"
	"github.com/coze-dev/coze-studio/backend/application/base/ctxutil"
	database "github.com/coze-dev/coze-studio/backend/crossdomain/database/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	dbservice "github.com/coze-dev/coze-studio/backend/domain/memory/database/service"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ternary"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)

var defaultAction = []*common.ResourceAction{
	{
		Key:    common.ActionKey_Edit,
		Enable: true,
	},
	{
		Key:    common.ActionKey_Delete,
		Enable: true,
	},
	{
		Key:    common.ActionKey_Copy,
		Enable: true,
	},
}

type ResourcePacker interface {
	GetDataInfo(ctx context.Context) (*dataInfo, error)
	GetActions(ctx context.Context) []*common.ResourceAction
	GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction
}

func NewResourcePacker(resID int64, t common.ResType, appContext *ServiceComponents) (ResourcePacker, error) {
	base := resourceBasePacker{appContext: appContext, resID: resID}

	switch t {
	case common.ResType_Plugin:
		return &pluginPacker{resourceBasePacker: base}, nil
	case common.ResType_Workflow:
		return &workflowPacker{resourceBasePacker: base}, nil
	case common.ResType_Knowledge:
		return &knowledgePacker{resourceBasePacker: base}, nil
	case common.ResType_Prompt:
		return &promptPacker{resourceBasePacker: base}, nil
	case common.ResType_Database:
		return &databasePacker{resourceBasePacker: base}, nil
	}

	return nil, fmt.Errorf("unsupported resource type: %s , resID: %d", t, resID)
}

type resourceBasePacker struct {
	resID      int64
	appContext *ServiceComponents
}

type dataInfo struct {
	iconURI *string
	iconURL string
	desc    *string
	status  *int32
}

func (b *resourceBasePacker) GetActions(ctx context.Context) []*common.ResourceAction {
	return defaultAction
}

func (b *resourceBasePacker) GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction {
	return []*common.ProjectResourceAction{}
}

type pluginPacker struct {
	resourceBasePacker
}

func (p *pluginPacker) GetDataInfo(ctx context.Context) (*dataInfo, error) {
	plugin, err := p.appContext.PluginDomainSVC.GetDraftPlugin(ctx, p.resID)
	if err != nil {
		return nil, err
	}

	iconURL, err := p.appContext.TOS.GetObjectUrl(ctx, plugin.GetIconURI())
	if err != nil {
		logs.CtxWarnf(ctx, "get icon url failed with '%s', err=%v", plugin.GetIconURI(), err)
	}

	return &dataInfo{
		iconURI: ptr.Of(plugin.GetIconURI()),
		iconURL: iconURL,
		desc:    ptr.Of(plugin.GetDesc()),
	}, nil
}

func (p *pluginPacker) GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction {
	return []*common.ProjectResourceAction{
		{
			Key:    common.ProjectResourceActionKey_Rename,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_Copy,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_Delete,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_CopyToLibrary,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_MoveToLibrary,
			Enable: true,
		},
	}
}

type workflowPacker struct {
	resourceBasePacker
}

func (w *workflowPacker) GetDataInfo(ctx context.Context) (*dataInfo, error) {
	info, err := w.appContext.WorkflowDomainSVC.Get(ctx, &vo.GetPolicy{
		ID:       w.resID,
		MetaOnly: true,
	})
	if err != nil {
		return nil, err
	}

	return &dataInfo{
		iconURI: &info.IconURI,
		iconURL: info.IconURL,
		desc:    &info.Desc,
	}, nil
}

func (w *workflowPacker) GetActions(ctx context.Context) []*common.ResourceAction {
	actions := []*common.ResourceAction{
		{
			Key:    common.ActionKey_Edit,
			Enable: true,
		},
		{
			Key:    common.ActionKey_Delete,
			Enable: true,
		},
		{
			Key:    common.ActionKey_Copy,
			Enable: true,
		},
	}
	meta, err := w.appContext.WorkflowDomainSVC.Get(ctx, &vo.GetPolicy{
		ID:       w.resID,
		MetaOnly: true,
	})
	if err != nil {
		logs.CtxWarnf(ctx, "get policy failed with '%s', err=%v", w.resID, err)
		return actions
	}
	key := ternary.IFElse(meta.Mode == workflow.WorkflowMode_Workflow, common.ActionKey_SwitchToChatflow, common.ActionKey_SwitchToFuncflow)
	action := &common.ResourceAction{
		Key:    key,
		Enable: true,
	}
	return append(actions, action)
}

func (w *workflowPacker) GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction {
	actions := []*common.ProjectResourceAction{
		{
			Key:    common.ProjectResourceActionKey_Rename,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_Copy,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_CopyToLibrary,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_MoveToLibrary,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_Delete,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_UpdateDesc,
			Enable: true,
		},
	}
	meta, err := w.appContext.WorkflowDomainSVC.Get(ctx, &vo.GetPolicy{
		ID:       w.resID,
		MetaOnly: true,
	})
	if err != nil {
		logs.CtxWarnf(ctx, "get policy failed with '%s', err=%v", w.resID, err)
		return actions
	}
	key := ternary.IFElse(meta.Mode == workflow.WorkflowMode_Workflow, common.ProjectResourceActionKey_SwitchToChatflow, common.ProjectResourceActionKey_SwitchToFuncflow)
	action := &common.ProjectResourceAction{
		Enable: true,
		Key:    key,
	}

	return append(actions, action)
}

type knowledgePacker struct {
	resourceBasePacker
	// cachedCanEdit memoizes the owner check so GetActions /
	// GetProjectDefaultActions / GetDataInfo don't each issue a fresh
	// GetKnowledgeByID for the same row. Packers are per-row + single-threaded
	// so a plain pointer is fine (no mutex required).
	cachedCanEdit *bool
}

func (k *knowledgePacker) GetDataInfo(ctx context.Context) (*dataInfo, error) {
	res, err := k.appContext.KnowledgeDomainSVC.GetKnowledgeByID(ctx, &service.GetKnowledgeByIDRequest{
		KnowledgeID: k.resID,
	})
	if err != nil {
		return nil, err
	}

	kn := res.Knowledge

	// Opportunistically warm the canEdit cache so the subsequent
	// GetActions / GetProjectDefaultActions call doesn't refetch.
	if k.cachedCanEdit == nil {
		k.cachedCanEdit = ptr.Of(k.computeCanEditFromCreator(ctx, kn.CreatorID))
	}

	return &dataInfo{
		iconURI: ptr.Of(kn.IconURI),
		iconURL: kn.IconURL,
		desc:    ptr.Of(kn.Description),
		status:  ptr.Of(int32(kn.Status)),
	}, nil
}

func (k *knowledgePacker) GetActions(ctx context.Context) []*common.ResourceAction {
	canEdit := k.canCurrentUserEdit(ctx)
	return []*common.ResourceAction{
		{
			Key:    common.ActionKey_Delete,
			Enable: canEdit,
		},
		{
			Key:    common.ActionKey_EnableSwitch,
			Enable: canEdit,
		},
		{
			Key:    common.ActionKey_Edit,
			Enable: canEdit,
		},
	}
}
func (k *knowledgePacker) GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction {
	canEdit := k.canCurrentUserEdit(ctx)
	return []*common.ProjectResourceAction{
		{
			Key:    common.ProjectResourceActionKey_Rename,
			Enable: canEdit,
		},
		{
			// Copy is read-only on the source; the copy is owned by the copier.
			Key:    common.ProjectResourceActionKey_Copy,
			Enable: true,
		},
		{
			// CopyToLibrary likewise produces a new resource owned by the copier.
			Key:    common.ProjectResourceActionKey_CopyToLibrary,
			Enable: true,
		},
		{
			// MoveToLibrary mutates the original — gate it.
			Key:    common.ProjectResourceActionKey_MoveToLibrary,
			Enable: canEdit,
		},
		{
			Key:    common.ProjectResourceActionKey_Delete,
			Enable: canEdit,
		},
		{
			Key:    common.ProjectResourceActionKey_Disable,
			Enable: canEdit,
		},
	}
}

// canCurrentUserEdit reports whether the requester is the KB's creator (or an
// admin-context caller, e.g. admin-bearer OpenAPI). The result is memoized on
// the packer instance so multiple action/data lookups for the same row share a
// single GetKnowledgeByID call.
func (k *knowledgePacker) canCurrentUserEdit(ctx context.Context) bool {
	if k.cachedCanEdit != nil {
		return *k.cachedCanEdit
	}
	canEdit := k.resolveCanEdit(ctx)
	k.cachedCanEdit = ptr.Of(canEdit)
	return canEdit
}

func (k *knowledgePacker) resolveCanEdit(ctx context.Context) bool {
	if ctxutil.IsAdminFromCtx(ctx) {
		return true
	}
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return false
	}
	res, err := k.appContext.KnowledgeDomainSVC.GetKnowledgeByID(ctx, &service.GetKnowledgeByIDRequest{
		KnowledgeID: k.resID,
	})
	if err != nil || res == nil || res.Knowledge == nil {
		// If we can't determine ownership, fall back to read-only — safer than
		// enabling writes for a stranger.
		logs.CtxWarnf(ctx, "[knowledgePacker.resolveCanEdit] GetKnowledgeByID failed, resID=%d, err=%v", k.resID, err)
		return false
	}
	return res.Knowledge.CreatorID == *uid
}

// computeCanEditFromCreator is the cheap path used when GetDataInfo has
// already loaded the knowledge — it skips the duplicate GetKnowledgeByID.
func (k *knowledgePacker) computeCanEditFromCreator(ctx context.Context, creatorID int64) bool {
	if ctxutil.IsAdminFromCtx(ctx) {
		return true
	}
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return false
	}
	return creatorID == *uid
}

type promptPacker struct {
	resourceBasePacker
}

func (p *promptPacker) GetDataInfo(ctx context.Context) (*dataInfo, error) {
	pInfo, err := p.appContext.PromptDomainSVC.GetPromptResource(ctx, p.resID)
	if err != nil {
		return nil, err
	}
	return &dataInfo{
		iconURI: nil, // prompt don't have custom icon
		iconURL: "",
		desc:    &pInfo.Description,
	}, nil
}

type databasePacker struct {
	resourceBasePacker
}

func (d *databasePacker) GetDataInfo(ctx context.Context) (*dataInfo, error) {
	listResp, err := d.appContext.DatabaseDomainSVC.MGetDatabase(ctx, &dbservice.MGetDatabaseRequest{Basics: []*database.DatabaseBasic{
		{
			ID:        d.resID,
			TableType: table.TableType_OnlineTable,
		},
	}})
	if err != nil {
		return nil, err
	}
	if len(listResp.Databases) == 0 {
		return nil, fmt.Errorf("online database not found, id: %d", d.resID)
	}

	return &dataInfo{
		iconURI: ptr.Of(listResp.Databases[0].IconURI),
		iconURL: listResp.Databases[0].IconURL,
		desc:    ptr.Of(listResp.Databases[0].TableDesc),
	}, nil
}

func (d *databasePacker) GetActions(ctx context.Context) []*common.ResourceAction {
	return []*common.ResourceAction{
		{
			Key:    common.ActionKey_Delete,
			Enable: true,
		},
	}
}

func (d *databasePacker) GetProjectDefaultActions(ctx context.Context) []*common.ProjectResourceAction {
	return []*common.ProjectResourceAction{
		{
			Key:    common.ProjectResourceActionKey_Copy,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_CopyToLibrary,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_MoveToLibrary,
			Enable: true,
		},
		{
			Key:    common.ProjectResourceActionKey_Delete,
			Enable: true,
		},
	}
}
