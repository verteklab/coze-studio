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
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/bytedance/sonic"

	dataset "github.com/coze-dev/coze-studio/backend/api/model/data/knowledge"
	document "github.com/coze-dev/coze-studio/backend/api/model/data/knowledge"
	modelCommon "github.com/coze-dev/coze-studio/backend/api/model/data/knowledge"
	resource "github.com/coze-dev/coze-studio/backend/api/model/resource/common"
	"github.com/coze-dev/coze-studio/backend/application/base/ctxutil"
	"github.com/coze-dev/coze-studio/backend/application/search"

	model "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service/ragimpl"
	resourceEntity "github.com/coze-dev/coze-studio/backend/domain/search/entity"
	ragcontract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	cd "github.com/coze-dev/coze-studio/backend/infra/document"
	"github.com/coze-dev/coze-studio/backend/infra/document/parser"
	"github.com/coze-dev/coze-studio/backend/infra/storage"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/conv"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/maps"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/slices"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
	"github.com/coze-dev/coze-studio/backend/types/consts"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

type KnowledgeApplicationService struct {
	DomainSVC service.Knowledge
	eventBus  search.ResourceEventBus
	storage   storage.Storage
	// rag is non-nil only when KNOWLEDGE_BACKEND=rag. Endpoints that only make
	// sense against the rag backend (e.g. /model-providers) MUST guard on this
	// before dereferencing.
	rag               ragcontract.Client
	ragTenantResolver ragimpl.TenantResolver
	// mappingRepo is the rag_kb_mapping DAO. Non-nil only when
	// KNOWLEDGE_BACKEND=rag — on legacy deployments no rows can ever exist in
	// rag_kb_mapping so the field is left nil and every Dataset.Backend resolves
	// to "legacy" without paying a DB hit. Used by the convertor to tag
	// outgoing DTOs with their owning backend ("rag" vs "legacy").
	mappingRepo *ragimpl.MappingRepo
}

var KnowledgeSVC = &KnowledgeApplicationService{}

// ragModelProvider is the wire shape consumed by the frontend create-KB modal
// (frontend/.../model-selector.tsx). The selector only reads {id, name}, so
// we strip the rest of rag's ModelProviderDTO at the application boundary;
// the additional fields would force the frontend type to track every rag
// schema bump without buying anything for the current UI.
type ragModelProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListRagModelProvidersResponse is the application-level response shape
// returned to the frontend. We split rag's flat {items: [{type, ...}]} list
// into the {text_models, image_models} buckets the modal expects, so the
// frontend stays unaware of rag's transport schema.
type ListRagModelProvidersResponse struct {
	TextModels  []ragModelProvider `json:"text_models"`
	ImageModels []ragModelProvider `json:"image_models"`
}

// ListRagModelProviders fetches the available embedding model providers from
// rag and translates them into the legacy `{text_models, image_models}` shape
// the create-KB modal consumes.
//
// Returns ErrRagFeaturePendingCode when the active backend is legacy — the
// frontend uses that signal to hide the model selector entirely.
func (k *KnowledgeApplicationService) ListRagModelProviders(ctx context.Context) (*ListRagModelProvidersResponse, error) {
	if k.rag == nil {
		return nil, errorx.New(errno.ErrRagFeaturePendingCode, errorx.KV("msg", "model providers proxy requires KNOWLEDGE_BACKEND=rag"))
	}
	tenant := ""
	if k.ragTenantResolver != nil {
		// Tenant header is sent on every rag call for trace correlation; the
		// model-providers endpoint itself is tenant-agnostic on the server.
		if v, err := k.ragTenantResolver.Resolve(ctx); err == nil {
			tenant = v
		}
	}
	raw, err := k.rag.ListModelProviders(ctx, tenant)
	if err != nil {
		return nil, err
	}
	textItems, imageItems := raw.Split()
	out := &ListRagModelProvidersResponse{
		TextModels:  make([]ragModelProvider, 0, len(textItems)),
		ImageModels: make([]ragModelProvider, 0, len(imageItems)),
	}
	for _, m := range textItems {
		out.TextModels = append(out.TextModels, ragModelProvider{ID: m.ModelID, Name: m.Name})
	}
	for _, m := range imageItems {
		out.ImageModels = append(out.ImageModels, ragModelProvider{ID: m.ModelID, Name: m.Name})
	}
	return out, nil
}

// ragDocumentParameter is the application-layer view of one tunable knob in
// a rag parameter schema. Mirrors ragcontract.DocumentParameter on the wire
// but pinned at the application boundary so rag-internal renames don't leak
// into the frontend contract.
type ragDocumentParameter struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"` // boolean | integer | string | array[string] | ...
	Group         string   `json:"group"`
	Required      bool     `json:"required"`
	Default       any      `json:"default,omitempty"`
	AllowedValues []any    `json:"allowed_values,omitempty"`
	MinValue      *float64 `json:"min_value,omitempty"`
	MaxValue      *float64 `json:"max_value,omitempty"`
	Description   string   `json:"description"`
	UILabel       string   `json:"ui_label"`
	UIComponent   string   `json:"ui_component"`
	Advanced      bool     `json:"advanced"`
}

// ragDocumentParameterSchema is one entry in the per-file-type catalog the
// upload wizard's dynamic form consumes. Selection on the frontend is by
// file_types / source_modalities — the wizard picks the schema whose
// file_types contains the uploaded file's extension.
type ragDocumentParameterSchema struct {
	SchemaID         string                 `json:"schema_id"`
	Description      string                 `json:"description"`
	FileTypes        []string               `json:"file_types"`
	SourceModalities []string               `json:"source_modalities"`
	Parameters       []ragDocumentParameter `json:"parameters"`
}

// ListRagDocumentParameterSchemasResponse is the wire shape the upload UI
// receives. A single-field wrapper leaves room to add per-tenant or
// per-capability filtering later (e.g. only schemas the tenant's enabled
// parsers support) without renaming the top-level shape.
type ListRagDocumentParameterSchemasResponse struct {
	Schemas []ragDocumentParameterSchema `json:"schemas"`
}

// ListRagDocumentParameterSchemas fetches rag's system-wide catalog of
// per-file-type parameter forms so the upload UI can render a dynamic
// "advanced" panel matched to the file being uploaded.
//
// Internal-flagged parameters (rag's "advanced operator knob" set, e.g.
// ocr_render_dpi) are filtered out at this layer — they belong in admin
// tooling, not the user-facing form. The Advanced bool is preserved so the
// UI can still hide its own "advanced toggle" parameters behind a disclosure.
//
// Returns ErrRagFeaturePendingCode when the active backend is legacy — the
// frontend uses that signal to fall back to the static parsing-strategy UI.
func (k *KnowledgeApplicationService) ListRagDocumentParameterSchemas(ctx context.Context) (*ListRagDocumentParameterSchemasResponse, error) {
	if k.rag == nil {
		return nil, errorx.New(errno.ErrRagFeaturePendingCode, errorx.KV("msg", "document parameter schemas proxy requires KNOWLEDGE_BACKEND=rag"))
	}
	tenant := ""
	if k.ragTenantResolver != nil {
		// The schemas endpoint is tenant-agnostic on the server but we still
		// forward the header for trace correlation, matching ListRagModelProviders.
		if v, err := k.ragTenantResolver.Resolve(ctx); err == nil {
			tenant = v
		}
	}
	raw, err := k.rag.ListDocumentParameterSchemas(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := &ListRagDocumentParameterSchemasResponse{
		Schemas: make([]ragDocumentParameterSchema, 0, len(raw)),
	}
	for _, sch := range raw {
		params := make([]ragDocumentParameter, 0, len(sch.Parameters))
		for _, p := range sch.Parameters {
			if p.Internal {
				continue
			}
			params = append(params, ragDocumentParameter{
				Name:          p.Name,
				Type:          p.Type,
				Group:         p.Group,
				Required:      p.Required,
				Default:       p.Default,
				AllowedValues: p.AllowedValues,
				MinValue:      p.MinValue,
				MaxValue:      p.MaxValue,
				Description:   p.Description,
				UILabel:       p.UILabel,
				UIComponent:   p.UIComponent,
				Advanced:      p.Advanced,
			})
		}
		out.Schemas = append(out.Schemas, ragDocumentParameterSchema{
			SchemaID:         sch.SchemaID,
			Description:      sch.Description,
			FileTypes:        sch.FileTypes,
			SourceModalities: sch.SourceModalities,
			Parameters:       params,
		})
	}
	return out, nil
}

func (k *KnowledgeApplicationService) deleteKnowledgeInternal(ctx context.Context, knowledgeID int64) error {
	err := k.DomainSVC.DeleteKnowledge(ctx, &service.DeleteKnowledgeRequest{
		KnowledgeID: knowledgeID,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "delete knowledge failed, err: %v", err)
		return err
	}
	return nil
}

func (k *KnowledgeApplicationService) publishDeleteKnowledgeEvent(ctx context.Context, knowledgeID int64) error {
	err := k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
		OpType: resourceEntity.Deleted,
		Resource: &resourceEntity.ResourceDocument{
			ResID:   knowledgeID,
			ResType: resource.ResType_Knowledge,
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "publish resource event failed, err: %v", err)
		return err
	}
	return nil
}

func (k *KnowledgeApplicationService) updateKnowledgeInternal(ctx context.Context, knowledgeID int64, name, description *string, iconURI *string, status *dataset.DatasetStatus) error {
	updateReq := service.UpdateKnowledgeRequest{
		KnowledgeID: knowledgeID,
	}

	if name != nil && len(*name) != 0 {
		updateReq.Name = name
	}
	if description != nil {
		updateReq.Description = description
	}
	if iconURI != nil {
		updateReq.IconUri = iconURI
	}
	if status != nil {
		updateReq.Status = ptr.Of(convertDatasetStatus2Entity(*status))
	}

	err := k.DomainSVC.UpdateKnowledge(ctx, &updateReq)
	if err != nil {
		logs.CtxErrorf(ctx, "update knowledge failed, err: %v", err)
		return err
	}

	return nil
}

func (k *KnowledgeApplicationService) publishUpdateKnowledgeEvent(ctx context.Context, knowledgeID int64, name *string, updateTimeMs int64) error {
	err := k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
		OpType: resourceEntity.Updated,
		Resource: &resourceEntity.ResourceDocument{
			ResType:      resource.ResType_Knowledge,
			ResID:        knowledgeID,
			Name:         name,
			UpdateTimeMS: ptr.Of(updateTimeMs),
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "publish resource event failed, err: %v", err)
		return err
	}
	return nil
}

func (k *KnowledgeApplicationService) buildListKnowledgeRequest(
	ctx context.Context,
	spaceID int64,
	name *string,
	formatType *dataset.FormatType,
	page, pageSize int,
	projectIDStr string,
	scope dataset.DatasetScopeType,
	uid int64,
) (*service.ListKnowledgeRequest, error) {
	request := service.ListKnowledgeRequest{}

	if page > 0 {
		request.Page = &page
	}
	if pageSize > 0 {
		request.PageSize = &pageSize
	}

	if projectIDStr != "" && projectIDStr != "0" {
		projectID, err := conv.StrToInt64(projectIDStr)
		if err != nil {
			return nil, err
		}
		request.AppID = ptr.Of(projectID)
	}

	orderBy := model.OrderUpdatedAt
	request.Order = &orderBy
	orderType := model.OrderTypeDesc
	request.OrderType = &orderType

	if name != nil && *name != "" {
		request.Query = name
	}
	if formatType != nil {
		request.FormatType = ptr.Of(convertFormatType2Entity(*formatType))
	}

	// Scope logic: ScopeAll (default / zero) returns every KB regardless of
	// space; ScopeSelf restricts to KBs created by the caller. spaceID is
	// intentionally ignored — KB visibility no longer depends on the active
	// space, only on read permission (already checked by the caller).
	_ = spaceID
	switch scope {
	case dataset.DatasetScopeType_ScopeSelf:
		if uid != 0 {
			request.UserID = ptr.Of(uid)
		}
	default:
		// ScopeAll or zero — no space_id and no creator filter.
	}

	return &request, nil
}

func (k *KnowledgeApplicationService) createKnowledgeInternal(ctx context.Context, name, description string, spaceID, creatorID, projectID int64, formatType dataset.FormatType, iconURI string) (*service.CreateKnowledgeResponse, error) {
	documentType := convertDocumentTypeDataset2Entity(formatType)
	if documentType == model.DocumentTypeUnknown {
		return nil, errors.New("unknown document type")
	}

	createReq := service.CreateKnowledgeRequest{
		Name:        name,
		Description: description,
		CreatorID:   creatorID,
		SpaceID:     spaceID,
		AppID:       projectID,
		FormatType:  documentType,
		IconUri:     iconURI,
	}
	if iconURI == "" {
		createReq.IconUri = getIconURI(formatType)
	}

	domainResp, err := k.DomainSVC.CreateKnowledge(ctx, &createReq)
	if err != nil {
		logs.CtxErrorf(ctx, "create knowledge failed, err: %v", err)
		return nil, err
	}

	return domainResp, nil
}

func (k *KnowledgeApplicationService) publishKnowledgeEvent(ctx context.Context, knowledgeID int64, name string, formatType dataset.FormatType, spaceID, projectID, creatorID int64, createdAtMs int64) error {
	var ptrAppID *int64
	if projectID != 0 {
		ptrAppID = ptr.Of(projectID)
	}

	err := k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
		OpType: resourceEntity.Created,
		Resource: &resourceEntity.ResourceDocument{
			ResType:       resource.ResType_Knowledge,
			ResID:         knowledgeID,
			Name:          ptr.Of(name),
			ResSubType:    ptr.Of(int32(formatType)),
			SpaceID:       ptr.Of(spaceID),
			APPID:         ptrAppID,
			OwnerID:       ptr.Of(creatorID),
			PublishStatus: ptr.Of(resource.PublishStatus_Published),
			PublishTimeMS: ptr.Of(createdAtMs),
			CreateTimeMS:  ptr.Of(createdAtMs),
			UpdateTimeMS:  ptr.Of(createdAtMs),
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "publish resource event failed, err: %v", err)
		return err
	}
	return nil
}

func (k *KnowledgeApplicationService) CreateKnowledge(ctx context.Context, req *dataset.CreateDatasetRequest) (*dataset.CreateDatasetResponse, error) {
	documentType := convertDocumentTypeDataset2Entity(req.FormatType)
	if documentType == model.DocumentTypeUnknown {
		return dataset.NewCreateDatasetResponse(), errors.New("unknown document type")
	}
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	// PR-1: forward optional embedding model ids to ragimpl via context.
	// Legacy backend ignores this; rag backend reads it in CreateKnowledge.
	// Helper is a no-op when both fields are empty, so legacy callers see no
	// behaviour change.
	ctx = consts.WithRagModelOverride(ctx, req.GetTextEmbeddingModelID(), req.GetImageEmbeddingModelID())

	domainResp, err := k.createKnowledgeInternal(ctx, req.Name, req.Description, req.SpaceID, ptr.From(uid), req.GetProjectID(), req.FormatType, req.IconURI)
	if err != nil {
		return dataset.NewCreateDatasetResponse(), err
	}

	err = k.publishKnowledgeEvent(ctx, domainResp.KnowledgeID, req.Name, req.FormatType, req.SpaceID, req.GetProjectID(), ptr.From(uid), domainResp.CreatedAtMs)
	if err != nil {
		return dataset.NewCreateDatasetResponse(), err
	}

	return &dataset.CreateDatasetResponse{
		DatasetID: domainResp.KnowledgeID,
	}, nil
}

func (k *KnowledgeApplicationService) DatasetDetail(ctx context.Context, req *dataset.DatasetDetailRequest) (*dataset.DatasetDetailResponse, error) {
	return k.datasetDetail(ctx, req, false)
}

func (k *KnowledgeApplicationService) DatasetDetailOpenAPI(ctx context.Context, req *dataset.DatasetDetailRequest) (*dataset.DatasetDetailResponse, error) {
	return k.datasetDetail(ctx, req, true)
}

func (k *KnowledgeApplicationService) datasetDetail(ctx context.Context, req *dataset.DatasetDetailRequest, fromOpenAPI bool) (*dataset.DatasetDetailResponse, error) {
	var err error
	var datasetIDs []int64

	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}

	if !fromOpenAPI {
		err = k.checkReadAccess(ctx, uid)
		if err != nil {
			return dataset.NewDatasetDetailResponse(), err
		}
	}

	datasetIDs, err = slices.TransformWithErrorCheck(req.GetDatasetIDs(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "convert string ids failed, err: %v", err)
		return dataset.NewDatasetDetailResponse(), err
	}

	listKnowledgeReq := &service.ListKnowledgeRequest{
		IDs: datasetIDs,
	}
	if !fromOpenAPI {
		listKnowledgeReq.SpaceID = &req.SpaceID
		listKnowledgeReq.AppID = &req.ProjectID
	}
	domainResp, err := k.DomainSVC.ListKnowledge(ctx, listKnowledgeReq)
	if err != nil {
		logs.CtxErrorf(ctx, "get knowledge failed, err: %v", err)
		return dataset.NewDatasetDetailResponse(), err
	}
	// getUID returns a non-nil pointer for both flows (it errors otherwise),
	// so this deref is safe. CanEdit is derived from (CreatorID == *uid).
	knowledgeMap, err := batchConvertKnowledgeEntity2Model(ctx, domainResp.KnowledgeList, ptr.From(uid))
	if err != nil {
		logs.CtxErrorf(ctx, "batch convert knowledge entity failed, err: %v", err)
		return dataset.NewDatasetDetailResponse(), err
	}
	response := dataset.NewDatasetDetailResponse()
	response.DatasetDetails = maps.TransformKey(knowledgeMap, func(key int64) string {
		return strconv.FormatInt(key, 10)
	})
	return response, nil
}

func (k *KnowledgeApplicationService) ListKnowledge(ctx context.Context, req *dataset.ListDatasetRequest) (*dataset.ListDatasetResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	var name *string
	var formatType *dataset.FormatType
	var scope dataset.DatasetScopeType
	if req.Filter != nil {
		if req.GetFilter().GetName() != "" {
			nameValue := req.GetFilter().GetName()
			name = &nameValue
		}
		if req.GetFilter().GetFormatType() != 0 {
			formatTypeValue := req.GetFilter().GetFormatType()
			formatType = &formatTypeValue
		}
		scope = req.GetFilter().GetScopeType()
	}

	var page, pageSize int
	if req.Page != nil && *req.Page > 0 {
		page = int(*req.Page)
	}
	if req.Size != nil && *req.Size > 0 {
		pageSize = int(*req.Size)
	}

	request, err := k.buildListKnowledgeRequest(ctx, req.GetSpaceID(), name, formatType, page, pageSize, req.GetProjectID(), scope, *uid)
	if err != nil {
		logs.CtxErrorf(ctx, "build list knowledge request failed, err: %v", err)
		return dataset.NewListDatasetResponse(), err
	}

	err = k.checkReadAccess(ctx, uid)
	if err != nil {
		return dataset.NewListDatasetResponse(), err
	}

	if req.Filter != nil && len(req.GetFilter().DatasetIds) > 0 {
		request.IDs, err = slices.TransformWithErrorCheck(req.GetFilter().GetDatasetIds(), func(s string) (int64, error) {
			id, err := strconv.ParseInt(s, 10, 64)
			return id, err
		})
		if err != nil {
			logs.CtxErrorf(ctx, "convert string ids failed, err: %v", err)
			return dataset.NewListDatasetResponse(), err
		}
	}

	if req.GetOrderField() == dataset.OrderField_CreateTime {
		request.Order = ptr.Of(model.OrderCreatedAt)
	}
	if req.GetOrderType() == dataset.OrderType_Asc {
		request.OrderType = ptr.Of(model.OrderTypeAsc)
	}

	domainResp, err := k.DomainSVC.ListKnowledge(ctx, request)
	if err != nil {
		logs.CtxErrorf(ctx, "mget knowledge failed, err: %v", err)
		return dataset.NewListDatasetResponse(), err
	}

	resp := dataset.ListDatasetResponse{}
	resp.Total = int32(domainResp.Total)
	// uid is guaranteed non-nil at this point (checked at function entry).
	knowledgeMap, err := batchConvertKnowledgeEntity2Model(ctx, domainResp.KnowledgeList, *uid)
	if err != nil {
		logs.CtxErrorf(ctx, "batch convert knowledge entity failed, err: %v", err)
		return dataset.NewListDatasetResponse(), err
	}
	resp.DatasetList = make([]*dataset.Dataset, 0)
	for i := range domainResp.KnowledgeList {
		resp.DatasetList = append(resp.DatasetList, knowledgeMap[domainResp.KnowledgeList[i].ID])
	}
	return &resp, nil
}

func (k *KnowledgeApplicationService) DeleteKnowledge(ctx context.Context, req *dataset.DeleteDatasetRequest) (*dataset.DeleteDatasetResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	err := k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return nil, err
	}

	err = k.deleteKnowledgeInternal(ctx, req.GetDatasetID())
	if err != nil {
		return dataset.NewDeleteDatasetResponse(), err
	}

	err = k.publishDeleteKnowledgeEvent(ctx, req.GetDatasetID())
	if err != nil {
		return dataset.NewDeleteDatasetResponse(), err
	}

	return &dataset.DeleteDatasetResponse{}, nil
}

func (k *KnowledgeApplicationService) UpdateKnowledge(ctx context.Context, req *dataset.UpdateDatasetRequest) (*dataset.UpdateDatasetResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	err := k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	err = k.updateKnowledgeInternal(ctx, req.GetDatasetID(), &req.Name, &req.Description, &req.IconURI, req.Status)
	if err != nil {
		return dataset.NewUpdateDatasetResponse(), err
	}

	err = k.publishUpdateKnowledgeEvent(ctx, req.GetDatasetID(), &req.Name, now)
	if err != nil {
		return dataset.NewUpdateDatasetResponse(), err
	}

	return &dataset.UpdateDatasetResponse{}, nil
}

func (k *KnowledgeApplicationService) CreateDocument(ctx context.Context, req *dataset.CreateDocumentRequest, fromOpenAPI bool) (*dataset.CreateDocumentResponse, error) {

	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}
	err = k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return dataset.NewCreateDocumentResponse(), err
	}
	listResp, err := k.DomainSVC.ListKnowledge(ctx, &service.ListKnowledgeRequest{IDs: []int64{req.GetDatasetID()}})
	if err != nil {
		logs.CtxErrorf(ctx, "mget knowledge failed, err: %v", err)
		return dataset.NewCreateDocumentResponse(), err
	}
	if len(listResp.KnowledgeList) == 0 {
		return dataset.NewCreateDocumentResponse(), errors.New("knowledge not found")
	}
	knowledgeInfo := listResp.KnowledgeList[0]

	documents := []*entity.Document{}
	if len(req.GetDocumentBases()) == 0 {
		return dataset.NewCreateDocumentResponse(), errors.New("document base is empty")
	}
	if req.FormatType == dataset.FormatType_Table && req.DocumentBases[0].GetName() == "" {
		req.DocumentBases[0].Name = knowledgeInfo.Name
	}
	for i := range req.GetDocumentBases() {
		if req.GetDocumentBases()[i] == nil {
			continue
		}
		docSource := entity.DocumentSourceCustom
		if req.GetDocumentBases()[i].GetSourceInfo().GetTosURI() != "" {
			docSource = entity.DocumentSourceLocal
		}
		var captionType *dataset.CaptionType
		if req.GetChunkStrategy() != nil {
			captionType = req.GetChunkStrategy().CaptionType
		}
		document := entity.Document{
			Info: model.Info{
				Name:      req.GetDocumentBases()[i].GetName(),
				CreatorID: *uid,
				SpaceID:   knowledgeInfo.SpaceID,
				AppID:     knowledgeInfo.AppID,
			},
			KnowledgeID:      req.GetDatasetID(),
			Type:             convertDocumentTypeDataset2Entity(req.GetFormatType()),
			RawContent:       req.GetDocumentBases()[i].GetSourceInfo().GetCustomContent(),
			URI:              req.GetDocumentBases()[i].GetSourceInfo().GetTosURI(),
			FileExtension:    parser.FileExtension(GetExtension(req.GetDocumentBases()[i].GetSourceInfo().GetTosURI())),
			Source:           docSource,
			IsAppend:         req.GetIsAppend(),
			ParsingStrategy:  convertParsingStrategy2Entity(req.GetParsingStrategy(), req.GetDocumentBases()[i].TableSheet, captionType, req.GetDocumentBases()[i].FilterStrategy),
			ChunkingStrategy: convertChunkingStrategy2Entity(req.GetChunkStrategy()),
			TableInfo: entity.TableInfo{
				Columns: convertTableColumns2Entity(req.GetDocumentBases()[i].GetTableMeta()),
			},
		}
		documents = append(documents, &document)
	}
	resp := dataset.NewCreateDocumentResponse()
	createResp, err := k.DomainSVC.CreateDocument(ctx, &service.CreateDocumentRequest{
		Documents:       documents,
		DocumentOptions: req.GetDocumentOptions(),
	})
	if err != nil {
		logs.CtxErrorf(ctx, "create document failed, err: %v", err)
		return resp, err
	}
	resp.DocumentInfos = make([]*dataset.DocumentInfo, 0)
	for i := range createResp.Documents {
		resp.DocumentInfos = append(resp.DocumentInfos, convertDocument2Model(createResp.Documents[i]))
	}
	return resp, nil
}

func getUID(ctx context.Context, fromOpenAPI bool) (*int64, error) {
	if fromOpenAPI {
		return ptr.Of(ctxutil.MustGetUIDFromApiAuthCtx(ctx)), nil
	}
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	return uid, nil
}

func (k *KnowledgeApplicationService) ListDocument(ctx context.Context, req *dataset.ListDocumentRequest, fromOpenAPI bool) (*dataset.ListDocumentResponse, error) {

	var err error
	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}
	err = k.checkReadAccess(ctx, uid)
	if err != nil {
		return dataset.NewListDocumentResponse(), err
	}
	var limit int = int(req.GetSize())
	// var offset int = int(req.GetPage() * req.GetSize())
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	var offset int = (page - 1) * limit

	docIDs := make([]int64, 0)
	if len(req.GetDocumentIds()) != 0 {
		docIDs, err = slices.TransformWithErrorCheck(req.GetDocumentIds(), func(s string) (int64, error) {
			id, err := strconv.ParseInt(s, 10, 64)
			return id, err
		})
		if err != nil {
			logs.CtxErrorf(ctx, "convert string ids failed, err: %v", err)
			return dataset.NewListDocumentResponse(), err
		}
	}
	//keyword := req.GetKeyword()
	listResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{
		KnowledgeID: req.GetDatasetID(),
		//Keyword:     &keyword,
		DocumentIDs: docIDs,
		Limit:       &limit,
		Offset:      &offset,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "list document failed, err: %v", err)
		return dataset.NewListDocumentResponse(), err
	}
	documents := listResp.Documents
	resp := dataset.NewListDocumentResponse()
	resp.Total = int32(listResp.Total)
	resp.DocumentInfos = make([]*dataset.DocumentInfo, 0)
	for i := range documents {
		resp.DocumentInfos = append(resp.DocumentInfos, convertDocument2Model(documents[i]))
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) DeleteDocument(ctx context.Context, req *dataset.DeleteDocumentRequest, fromOpenAPI bool) (*dataset.DeleteDocumentResponse, error) {

	if len(req.GetDocumentIds()) == 0 {
		return dataset.NewDeleteDocumentResponse(), errors.New("document ids is empty")
	}

	uid, err := getUID(ctx, fromOpenAPI)

	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	transformedIDs, err := slices.TransformWithErrorCheck(req.GetDocumentIds(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "convert string ids failed, err: %v", err)
		return dataset.NewDeleteDocumentResponse(), err
	}

	err = k.checkWriteAccess(ctx, uid, nil, transformedIDs, nil)
	if err != nil {
		return dataset.NewDeleteDocumentResponse(), err
	}

	for i := range req.GetDocumentIds() {
		docID, err := strconv.ParseInt(req.GetDocumentIds()[i], 10, 64)
		if err != nil {
			logs.CtxErrorf(ctx, "parse int failed, err: %v", err)
			return dataset.NewDeleteDocumentResponse(), err
		}
		err = k.DomainSVC.DeleteDocument(ctx, &service.DeleteDocumentRequest{
			DocumentID: docID,
		})
		if err != nil {
			logs.CtxErrorf(ctx, "delete document failed, err: %v", err)
			return dataset.NewDeleteDocumentResponse(), err
		}
	}
	return &dataset.DeleteDocumentResponse{}, nil
}

func (k *KnowledgeApplicationService) UpdateDocument(ctx context.Context, req *dataset.UpdateDocumentRequest, fromOpenAPI bool) (*dataset.UpdateDocumentResponse, error) {

	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}

	err = k.checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)
	if err != nil {
		return dataset.NewUpdateDocumentResponse(), err
	}

	err = k.DomainSVC.UpdateDocument(ctx, &service.UpdateDocumentRequest{
		DocumentID:   req.GetDocumentID(),
		DocumentName: req.DocumentName,
		TableInfo: &entity.TableInfo{
			Columns: convertTableColumns2Entity(req.GetTableMeta()),
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "update document failed, err: %v", err)
		return dataset.NewUpdateDocumentResponse(), err
	}
	return &dataset.UpdateDocumentResponse{}, nil
}

// checkReadAccess allows any authenticated user. Use for read endpoints
// (list, detail, list docs, list slices, retrieve). Knowledge data is
// globally readable by design; only writes are owner-gated.
func (k *KnowledgeApplicationService) checkReadAccess(ctx context.Context, uid *int64) error {
	if uid == nil {
		return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	return nil
}

// checkWriteAccess resolves the target identifier(s) back to the owning KB(s)
// and passes only when every owning KB.creator_id == *uid.
//
// Pass any combination of: knowledgeID, documentIDs, sliceIDs. (The union of
// owning KBs is checked.)
func (k *KnowledgeApplicationService) checkWriteAccess(
	ctx context.Context,
	uid *int64,
	knowledgeID *int64,
	documentIDs []int64,
	sliceIDs []int64,
) error {
	if uid == nil {
		return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	if ctxutil.IsAdminFromCtx(ctx) {
		return nil
	}

	// Resolve target → KB set. Batched queries; no N+1.
	kbIDSet := map[int64]struct{}{}
	if knowledgeID != nil {
		kbIDSet[*knowledgeID] = struct{}{}
	}
	if len(documentIDs) > 0 {
		docResp, err := k.DomainSVC.MGetDocument(ctx, &service.MGetDocumentRequest{DocumentIDs: documentIDs})
		if err != nil {
			logs.CtxErrorf(ctx, "mget document failed, err: %v", err)
			return err
		}
		if docResp == nil || len(docResp.Documents) != len(documentIDs) {
			return errorx.New(errno.ErrKnowledgeDocumentNotExistCode, errorx.KV("msg", "document not found"))
		}
		for _, d := range docResp.Documents {
			kbIDSet[d.KnowledgeID] = struct{}{}
		}
	}
	if len(sliceIDs) > 0 {
		sliceResp, err := k.DomainSVC.MGetSlice(ctx, &service.MGetSliceRequest{SliceIDs: sliceIDs})
		if err != nil {
			logs.CtxErrorf(ctx, "mget slice failed, err: %v", err)
			return err
		}
		if sliceResp == nil || len(sliceResp.Slices) != len(sliceIDs) {
			return errorx.New(errno.ErrKnowledgeSliceNotExistCode, errorx.KV("msg", "slice not found"))
		}
		for _, s := range sliceResp.Slices {
			kbIDSet[s.KnowledgeID] = struct{}{}
		}
	}

	if len(kbIDSet) == 0 {
		return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "no write target"))
	}

	kbIDs := make([]int64, 0, len(kbIDSet))
	for id := range kbIDSet {
		kbIDs = append(kbIDs, id)
	}

	kbResp, err := k.DomainSVC.ListKnowledge(ctx, &service.ListKnowledgeRequest{IDs: kbIDs})
	if err != nil {
		logs.CtxErrorf(ctx, "list knowledge failed, err: %v", err)
		return err
	}
	if kbResp == nil || len(kbResp.KnowledgeList) != len(kbIDs) {
		return errorx.New(errno.ErrKnowledgeNotExistCode, errorx.KV("msg", "knowledge not found"))
	}
	for _, kb := range kbResp.KnowledgeList {
		if kb.CreatorID != *uid {
			return errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "not knowledge owner"))
		}
	}
	return nil
}

func (k *KnowledgeApplicationService) GetDocumentProgress(ctx context.Context, req *dataset.GetDocumentProgressRequest) (*dataset.GetDocumentProgressResponse, error) {

	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	docIDs, err := slices.TransformWithErrorCheck(req.GetDocumentIds(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "convert string ids failed, err: %v", err)
		return dataset.NewGetDocumentProgressResponse(), err
	}
	err = k.checkReadAccess(ctx, uid)
	if err != nil {
		return dataset.NewGetDocumentProgressResponse(), err
	}

	domainResp, err := k.DomainSVC.MGetDocumentProgress(ctx, &service.MGetDocumentProgressRequest{
		DocumentIDs: docIDs,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "mget document progress failed, err: %v", err)
		return dataset.NewGetDocumentProgressResponse(), err
	}
	resp := dataset.NewGetDocumentProgressResponse()
	resp.Data = make([]*dataset.DocumentProgress, 0)
	for i := range domainResp.ProgressList {
		resp.Data = append(resp.Data, &dataset.DocumentProgress{
			DocumentID:     domainResp.ProgressList[i].ID,
			Progress:       int32(domainResp.ProgressList[i].Progress),
			Status:         convertDocumentStatus2Model(domainResp.ProgressList[i].Status),
			StatusDescript: &domainResp.ProgressList[i].StatusMsg,
			DocumentName:   domainResp.ProgressList[i].Name,
			RemainingTime:  &domainResp.ProgressList[i].RemainingSec,
			Size:           &domainResp.ProgressList[i].Size,
			Type:           &domainResp.ProgressList[i].FileExtension,
			URL:            ptr.Of(domainResp.ProgressList[i].URL),
		})
	}
	return resp, nil
}

// RetryDocument retries a previously-failed document ingestion. Forwards to
// the domain service which dispatches to ragimpl (which calls rag's
// POST /documents/{id}/retry and bumps mapping.last_task_id so subsequent
// MGetDocumentProgress polls follow the retry's new task) or to the legacy
// stub. Response carries a refreshed DocumentInfo so the UI can re-render
// the row without an immediate second round-trip.
func (k *KnowledgeApplicationService) RetryDocument(ctx context.Context, req *dataset.RetryDocumentRequest) (*dataset.RetryDocumentResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	if err := k.checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil); err != nil {
		return dataset.NewRetryDocumentResponse(), err
	}
	svcResp, err := k.DomainSVC.RetryDocument(ctx, &service.RetryDocumentRequest{DocumentID: req.GetDocumentID()})
	if err != nil {
		logs.CtxErrorf(ctx, "retry document failed, err: %v", err)
		return dataset.NewRetryDocumentResponse(), err
	}
	resp := dataset.NewRetryDocumentResponse()
	resp.DocumentInfo = convertDocument2Model(svcResp.Document)
	return resp, nil
}

func (k *KnowledgeApplicationService) Resegment(ctx context.Context, req *dataset.ResegmentRequest) (*dataset.ResegmentResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	docIDs, err := slices.TransformWithErrorCheck(req.GetDocumentIds(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "parse int failed, err: %v", err)
		return dataset.NewResegmentResponse(), err
	}
	err = k.checkWriteAccess(ctx, uid, nil, docIDs, nil)
	if err != nil {
		return dataset.NewResegmentResponse(), err
	}
	resp := dataset.NewResegmentResponse()
	resp.DocumentInfos = make([]*dataset.DocumentInfo, 0)
	for _, docID := range docIDs {
		var captionType *dataset.CaptionType
		if req.GetChunkStrategy() != nil {
			captionType = req.GetChunkStrategy().CaptionType
		}
		resegmentResp, err := k.DomainSVC.ResegmentDocument(ctx, &service.ResegmentDocumentRequest{
			DocumentID:       docID,
			ChunkingStrategy: convertChunkingStrategy2Entity(req.GetChunkStrategy()),
			ParsingStrategy:  convertParsingStrategy2Entity(req.GetParsingStrategy(), nil, captionType, req.FilterStrategy),
		})
		if err != nil {
			logs.CtxErrorf(ctx, "resegment document failed, err: %v", err)
			return dataset.NewResegmentResponse(), err
		}

		resp.DocumentInfos = append(resp.DocumentInfos, &dataset.DocumentInfo{
			Name:       resegmentResp.Document.Name,
			DocumentID: resegmentResp.Document.ID,
		})
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) CreateSlice(ctx context.Context, req *dataset.CreateSliceRequest) (*dataset.CreateSliceResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	err := k.checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)
	if err != nil {
		return dataset.NewCreateSliceResponse(), err
	}
	listResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{
		DocumentIDs: []int64{req.GetDocumentID()},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "list document failed, err: %v", err)
		return dataset.NewCreateSliceResponse(), err
	}
	if len(listResp.Documents) != 1 {
		return dataset.NewCreateSliceResponse(), errors.New("document not found")
	}
	sliceEntity := &model.Slice{
		Info: model.Info{
			CreatorID: *uid,
		},
		DocumentID: req.GetDocumentID(),
		Sequence:   req.GetSequence(),
	}
	if listResp.Documents[0].Type == model.DocumentTypeTable {
		err = packTableSliceColumnData(ctx, sliceEntity, req.GetRawText(), listResp.Documents[0])
		if err != nil {
			logs.CtxErrorf(ctx, "pack table slice column data failed, err: %v", err)
			return dataset.NewCreateSliceResponse(), errorx.New(errno.ErrKnowledgeCheckTableSliceValidCode, errorx.KV("msg", err.Error()))
		}
	} else {
		sliceEntity.RawContent = []*model.SliceContent{
			{
				Type: model.SliceContentTypeText,
				Text: req.RawText,
			},
		}
	}
	createResp, err := k.DomainSVC.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: req.GetDocumentID(),
		CreatorID:  ptr.From(uid),
		Position:   req.GetSequence(),
		RawContent: sliceEntity.RawContent,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "create slice failed, err: %v", err)
		return dataset.NewCreateSliceResponse(), err
	}
	resp := dataset.NewCreateSliceResponse()
	resp.SliceID = createResp.SliceID
	return resp, nil
}

func (k *KnowledgeApplicationService) DeleteSlice(ctx context.Context, req *dataset.DeleteSliceRequest) (*dataset.DeleteSliceResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	sliceIDs := make([]int64, 0, len(req.GetSliceIds()))
	for i := range req.GetSliceIds() {
		sliceID, err := strconv.ParseInt(req.GetSliceIds()[i], 10, 64)
		if err != nil {
			logs.CtxErrorf(ctx, "parse int failed, err: %v", err)
			return dataset.NewDeleteSliceResponse(), err
		}
		sliceIDs = append(sliceIDs, sliceID)
	}

	err := k.checkWriteAccess(ctx, uid, nil, nil, sliceIDs)
	if err != nil {
		logs.CtxErrorf(ctx, "check permission failed, err: %v", err)
		return dataset.NewDeleteSliceResponse(), err
	}
	for i := range sliceIDs {
		sliceID := sliceIDs[i]
		err = k.DomainSVC.DeleteSlice(ctx, &service.DeleteSliceRequest{
			SliceID: sliceID,
		})
		if err != nil {
			logs.CtxErrorf(ctx, "delete slice failed, err: %v", err)
			return dataset.NewDeleteSliceResponse(), err
		}
	}
	return &dataset.DeleteSliceResponse{}, nil
}

func (k *KnowledgeApplicationService) UpdateSlice(ctx context.Context, req *dataset.UpdateSliceRequest) (*dataset.UpdateSliceResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	err := k.checkWriteAccess(ctx, uid, nil, nil, []int64{req.GetSliceID()})
	if err != nil {
		return nil, err
	}
	getSliceResp, err := k.DomainSVC.GetSlice(ctx, &service.GetSliceRequest{
		SliceID: req.GetSliceID(),
	})
	if err != nil {
		return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode, errorx.KV("msg", "slice not found"))
	}
	if getSliceResp.Slice == nil {
		return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode, errorx.KV("msg", "slice not found"))
	}
	docID := getSliceResp.Slice.DocumentID

	listResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{
		DocumentIDs: []int64{docID},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "list document failed, err: %v", err)
		return dataset.NewUpdateSliceResponse(), err
	}
	if len(listResp.Documents) != 1 {
		return dataset.NewUpdateSliceResponse(), errors.New("document not found")
	}
	sliceEntity := &model.Slice{
		Info: model.Info{
			ID:        req.GetSliceID(),
			CreatorID: *uid,
		},
		DocumentID: docID,
	}
	if listResp.Documents[0].Type == model.DocumentTypeTable {
		err = packTableSliceColumnData(ctx, sliceEntity, req.GetRawText(), listResp.Documents[0])
		if err != nil {
			logs.CtxErrorf(ctx, "pack table slice column data failed, err: %v", err)
			return dataset.NewUpdateSliceResponse(), errorx.New(errno.ErrKnowledgeCheckTableSliceValidCode, errorx.KV("msg", err.Error()))
		}
	} else {
		sliceEntity.RawContent = []*model.SliceContent{
			{
				Type: model.SliceContentTypeText,
				Text: req.RawText,
			},
		}
	}
	err = k.DomainSVC.UpdateSlice(ctx, &service.UpdateSliceRequest{
		SliceID:    req.GetSliceID(),
		DocumentID: docID,
		CreatorID:  ptr.From(uid),
		RawContent: sliceEntity.RawContent,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "update slice failed, err: %v", err)
		return dataset.NewUpdateSliceResponse(), err
	}
	return &dataset.UpdateSliceResponse{}, nil
}

func packTableSliceColumnData(ctx context.Context, slice *model.Slice, text string, doc *entity.Document) error {
	columnMap := map[int64]string{}
	columnTypeMap := map[int64]cd.TableColumnType{}
	for i := range doc.TableInfo.Columns {
		columnMap[doc.TableInfo.Columns[i].ID] = doc.TableInfo.Columns[i].Name
		columnTypeMap[doc.TableInfo.Columns[i].ID] = doc.TableInfo.Columns[i].Type
	}
	dataMap := map[string]string{}
	err := sonic.Unmarshal([]byte(text), &dataMap)
	if err != nil {
		logs.CtxErrorf(ctx, "unmarshal raw text failed, err: %v", err)
		return err
	}
	slice.RawContent = []*model.SliceContent{
		{
			Type: model.SliceContentTypeTable,
			Table: &model.SliceTable{
				Columns: make([]*cd.ColumnData, 0, len(dataMap)),
			},
		},
	}
	for columnID, val := range dataMap {
		cid, err := strconv.ParseInt(columnID, 10, 64)
		if err != nil {
			logs.CtxErrorf(ctx, "parse column id failed, err: %v", err)
			return err
		}
		value := val
		column, err := assertValAs(columnTypeMap[cid], value)
		if err != nil {
			logs.CtxErrorf(ctx, "assert val as failed, err: %v", err)
			return err
		}
		column.ColumnID = cid
		column.ColumnName = columnMap[cid]
		slice.RawContent[0].Table.Columns = append(slice.RawContent[0].Table.Columns, column)
	}
	return nil
}

func (k *KnowledgeApplicationService) ListSlice(ctx context.Context, req *dataset.ListSliceRequest) (*dataset.ListSliceResponse, error) {
	return k.listSlice(ctx, req, false)
}

func (k *KnowledgeApplicationService) ListSliceOpenAPI(ctx context.Context, req *dataset.ListSliceRequest) (*dataset.ListSliceResponse, error) {
	return k.listSlice(ctx, req, true)
}

func (k *KnowledgeApplicationService) listSlice(ctx context.Context, req *dataset.ListSliceRequest, fromOpenAPI bool) (*dataset.ListSliceResponse, error) {
	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}

	if !fromOpenAPI {
		err := k.checkReadAccess(ctx, uid)
		if err != nil {
			return nil, err
		}
	}

	listResp, err := k.DomainSVC.ListSlice(ctx, &service.ListSliceRequest{
		KnowledgeID: req.DatasetID,
		DocumentID:  req.DocumentID,
		Keyword:     req.Keyword,
		Sequence:    req.GetSequence(),
		Limit:       req.GetPageSize(),
	})
	if err != nil {
		logs.CtxErrorf(ctx, "list slice failed, err: %v", err)
		return dataset.NewListSliceResponse(), err
	}
	resp := dataset.NewListSliceResponse()
	resp.Total = int64(listResp.Total)
	resp.Hasmore = listResp.HasMore
	resp.Slices = make([]*dataset.SliceInfo, 0)
	for i := range listResp.Slices {
		resp.Slices = append(resp.Slices, convertSlice2Model(listResp.Slices[i]))
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) GetTableSchema(ctx context.Context, req *dataset.GetTableSchemaRequest) (*dataset.GetTableSchemaResponse, error) {
	resp := dataset.NewGetTableSchemaResponse()
	if req.TableSheet == nil {
		req.TableSheet = &dataset.TableSheet{
			SheetID:       0,
			HeaderLineIdx: 0,
			StartLineIdx:  1,
		}
	}
	if req.TableDataType == nil {
		req.TableDataType = dataset.TableDataTypePtr(dataset.TableDataType(service.AllData))
	}

	var (
		domainResp *service.TableSchemaResponse
		err        error
	)
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	err = k.checkReadAccess(ctx, uid)
	if err != nil {
		return nil, err
	}

	if req.SourceFile == nil { // alter table
		domainResp, err = k.DomainSVC.GetAlterTableSchema(ctx, &service.AlterTableSchemaRequest{
			DocumentID:       req.GetDocumentID(),
			TableDataType:    convertTableDataType2Entity(req.GetTableDataType()),
			OriginTableMeta:  convertTableColumns2Entity(req.GetOriginTableMeta()),
			PreviewTableMeta: convertTableColumns2Entity(req.GetPreviewTableMeta()),
		})
	} else {
		var srcInfo *service.TableSourceInfo
		srcInfo, err = convertSourceInfo(req.SourceFile)
		if err != nil {
			return resp, err
		}

		domainResp, err = k.DomainSVC.GetImportDataTableSchema(ctx, &service.ImportDataTableSchemaRequest{
			SourceInfo:       *srcInfo,
			TableSheet:       convertTableSheet2Entity(req.TableSheet),
			TableDataType:    convertTableDataType2Entity(req.GetTableDataType()),
			DocumentID:       req.DocumentID,
			OriginTableMeta:  convertTableColumns2Entity(req.GetOriginTableMeta()),
			PreviewTableMeta: convertTableColumns2Entity(req.GetPreviewTableMeta()),
		})
	}
	if err != nil {
		logs.CtxErrorf(ctx, "get table schema failed, err: %v", err)
		return resp, err
	}

	prevData := make([]map[string]string, 0, len(domainResp.PreviewData))
	for _, data := range domainResp.PreviewData {
		if len(data) == 0 {
			continue
		}
		if len(data) != len(domainResp.TableMeta) {
			data = append(data, make([]*cd.ColumnData, len(domainResp.TableMeta)-len(data))...)
		}
		prev, err := convertTableColumnDataSlice(domainResp.TableMeta, data)
		if err != nil {
			return resp, err
		}
		prevData = append(prevData, prev)
	}

	resp.PreviewData = prevData

	resp.TableMeta = convertTableColumns2Model(domainResp.TableMeta)

	resp.SheetList = make([]*dataset.DocTableSheet, 0)
	for i := range domainResp.AllTableSheets {
		if domainResp.AllTableSheets[i] == nil {
			continue
		}
		resp.SheetList = append(resp.SheetList, convertDocTableSheet2Model(*domainResp.AllTableSheets[i]))
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) ValidateTableSchema(ctx context.Context, req *dataset.ValidateTableSchemaRequest) (*dataset.ValidateTableSchemaResponse, error) {
	resp := dataset.NewValidateTableSchemaResponse()
	srcInfo, err := convertSourceInfo(req.SourceInfo)
	if err != nil {
		return resp, err
	}
	if srcInfo == nil {
		return nil, fmt.Errorf("source info not provided")
	}

	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	if req.GetDocumentID() > 0 {
		err = k.checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)
		if err != nil {
			return nil, err
		}
	}

	var tableSheet *entity.TableSheet
	if req.TableSheet != nil {
		tableSheet = &entity.TableSheet{
			SheetId:       req.TableSheet.SheetID,
			HeaderLineIdx: req.TableSheet.HeaderLineIdx,
			StartLineIdx:  req.TableSheet.StartLineIdx,
		}
	}
	domainResp, err := k.DomainSVC.ValidateTableSchema(ctx, &service.ValidateTableSchemaRequest{
		DocumentID: req.GetDocumentID(),
		SourceInfo: *srcInfo,
		TableSheet: tableSheet,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "validate table schema failed, err: %v", err)
		return resp, err
	}
	resp.ColumnValidResult = domainResp.ColumnValidResult
	return resp, nil
}

func (k *KnowledgeApplicationService) GetDocumentTableInfo(ctx context.Context, req *document.GetDocumentTableInfoRequest) (*document.GetDocumentTableInfoResponse, error) {

	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	if err := k.checkReadAccess(ctx, uid); err != nil {
		return nil, err
	}

	domainResp, err := k.DomainSVC.GetDocumentTableInfo(ctx, &service.GetDocumentTableInfoRequest{
		DocumentID: req.DocumentID,
		SourceInfo: &service.TableSourceInfo{
			Uri: req.TosURI,
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "get document table info failed, err: %v", err)
		return document.NewGetDocumentTableInfoResponse(), err
	}
	resp := document.NewGetDocumentTableInfoResponse()
	resp.PreviewData = domainResp.PreviewData
	resp.SheetList = make([]*modelCommon.DocTableSheet, 0)
	for i := range domainResp.TableSheet {
		if domainResp.TableSheet[i] == nil {
			continue
		}
		resp.SheetList = append(resp.SheetList, convertDocTableSheet(domainResp.TableSheet[i]))
	}
	resp.TableMeta = map[string][]*modelCommon.DocTableColumn{}
	for index, rows := range domainResp.TableMeta {
		resp.TableMeta[index] = convertTableMeta(rows)
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) CreateDocumentReview(ctx context.Context, req *dataset.CreateDocumentReviewRequest) (*dataset.CreateDocumentReviewResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	if req.GetDatasetID() > 0 {
		err := k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
		if err != nil {
			return nil, err
		}
	}

	createResp, err := k.DomainSVC.CreateDocumentReview(ctx, convertCreateDocReviewReq(req))
	if err != nil {
		logs.CtxErrorf(ctx, "create document review failed, err: %v", err)
		return dataset.NewCreateDocumentReviewResponse(), err
	}
	resp := dataset.NewCreateDocumentReviewResponse()
	resp.DatasetID = req.GetDatasetID()
	resp.Reviews = slices.Transform(createResp.Reviews, func(item *entity.Review) *dataset.Review {
		return &dataset.Review{
			ReviewID:      item.ReviewID,
			DocumentName:  item.DocumentName,
			DocumentType:  item.DocumentType,
			TosURL:        item.Url,
			Status:        convertReviewStatus2Model(item.Status),
			DocTreeTosURL: item.DocTreeTosUrl,
			PreviewTosURL: item.PreviewTosUrl,
		}
	})
	return resp, nil
}

func (k *KnowledgeApplicationService) MGetDocumentReview(ctx context.Context, req *dataset.MGetDocumentReviewRequest) (*dataset.MGetDocumentReviewResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	reviewIDs, err := slices.TransformWithErrorCheck(req.GetReviewIds(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "parse int failed, err: %v", err)
		return dataset.NewMGetDocumentReviewResponse(), err
	}

	err = k.checkReadAccess(ctx, uid)
	if err != nil {
		return nil, err
	}

	mGetResp, err := k.DomainSVC.MGetDocumentReview(ctx, &service.MGetDocumentReviewRequest{
		KnowledgeID: req.GetDatasetID(),
		ReviewIDs:   reviewIDs,
	})
	if err != nil {
		logs.CtxErrorf(ctx, "mget document review failed, err: %v", err)
		return dataset.NewMGetDocumentReviewResponse(), err
	}
	resp := dataset.NewMGetDocumentReviewResponse()
	resp.Reviews = slices.Transform(mGetResp.Reviews, func(item *entity.Review) *dataset.Review {
		return &dataset.Review{
			ReviewID:      item.ReviewID,
			DocumentName:  item.DocumentName,
			DocumentType:  item.DocumentType,
			TosURL:        item.Url,
			Status:        convertReviewStatus2Model(item.Status),
			DocTreeTosURL: item.DocTreeTosUrl,
			PreviewTosURL: item.PreviewTosUrl,
		}
	})
	resp.DatasetID = req.GetDatasetID()
	return resp, nil
}

func (k *KnowledgeApplicationService) SaveDocumentReview(ctx context.Context, req *dataset.SaveDocumentReviewRequest) (*dataset.SaveDocumentReviewResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}
	err := k.checkWriteAccess(ctx, uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return nil, err
	}

	err = k.DomainSVC.SaveDocumentReview(ctx, &service.SaveDocumentReviewRequest{
		KnowledgeID: req.GetDatasetID(),
		DocTreeJson: req.GetDocTreeJSON(),
		ReviewID:    req.GetReviewID(),
	})
	if err != nil {
		logs.CtxErrorf(ctx, "save document review failed, err: %v", err)
		return dataset.NewSaveDocumentReviewResponse(), err
	}
	return &dataset.SaveDocumentReviewResponse{}, nil
}

func (k *KnowledgeApplicationService) DeleteAppKnowledge(ctx context.Context, req *DeleteAppKnowledgeRequest) error {
	listResp, err := k.DomainSVC.ListKnowledge(ctx, &model.ListKnowledgeRequest{
		AppID: &req.AppID,
	})
	if err != nil {
		return err
	}
	if len(listResp.KnowledgeList) == 0 {
		return nil
	}
	for i := range listResp.KnowledgeList {
		err := k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
			OpType: resourceEntity.Deleted,
			Resource: &resourceEntity.ResourceDocument{
				ResID:   listResp.KnowledgeList[i].ID,
				ResType: resource.ResType_Knowledge,
			},
		})
		if err != nil {
			logs.CtxErrorf(ctx, "publish resources failed, err: %v", err)
			return err
		}
		err = k.DomainSVC.DeleteKnowledge(ctx, &model.DeleteKnowledgeRequest{
			KnowledgeID: listResp.KnowledgeList[i].ID,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *KnowledgeApplicationService) CopyKnowledge(ctx context.Context, req *model.CopyKnowledgeRequest) (*model.CopyKnowledgeResponse, error) {
	resp, err := k.DomainSVC.CopyKnowledge(ctx, req)
	if err != nil {
		return nil, err
	}
	getResp, err := k.DomainSVC.GetKnowledgeByID(ctx, &model.GetKnowledgeByIDRequest{
		KnowledgeID: resp.TargetKnowledgeID,
	})
	if err != nil {
		return nil, err
	}
	var appIDPtr *int64
	if req.TargetAppID != 0 {
		appIDPtr = &req.TargetAppID
	}
	if resp.CopyStatus == model.CopyStatus_Successful {
		err = k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
			OpType: resourceEntity.Created,
			Resource: &resourceEntity.ResourceDocument{
				ResID:         resp.TargetKnowledgeID,
				ResType:       resource.ResType_Knowledge,
				ResSubType:    ptr.Of(int32(getResp.Knowledge.Type)),
				Name:          ptr.Of(getResp.Knowledge.Name),
				OwnerID:       ptr.Of(getResp.Knowledge.CreatorID),
				SpaceID:       ptr.Of(getResp.Knowledge.SpaceID),
				APPID:         appIDPtr,
				PublishStatus: ptr.Of(resource.PublishStatus_Published),
				CreateTimeMS:  ptr.Of(getResp.Knowledge.CreatedAtMs),
				UpdateTimeMS:  ptr.Of(getResp.Knowledge.CreatedAtMs),
			},
		})
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}
func (k *KnowledgeApplicationService) UpdatePhotoCaption(ctx context.Context, req *dataset.UpdatePhotoCaptionRequest) (*dataset.UpdatePhotoCaptionResponse, error) {
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	err := k.checkWriteAccess(ctx, uid, nil, []int64{req.DocumentID}, nil)
	if err != nil {
		return nil, err
	}

	resp := dataset.NewUpdatePhotoCaptionResponse()
	listResp, err := k.DomainSVC.ListSlice(ctx, &service.ListSliceRequest{DocumentID: ptr.Of(req.DocumentID)})
	if err != nil {
		logs.CtxErrorf(ctx, "list slice failed, err: %v", err)
		return resp, err
	}
	if len(listResp.Slices) == 0 {
		return resp, nil
	}
	err = k.DomainSVC.UpdateSlice(ctx, &service.UpdateSliceRequest{
		SliceID:    listResp.Slices[0].ID,
		DocumentID: req.DocumentID,
		CreatorID:  ptr.From(uid),
		RawContent: []*model.SliceContent{{
			Type: model.SliceContentTypeText,
			Text: ptr.Of(req.Caption),
		}},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "update slice failed, err: %v", err)
		return resp, err
	}
	return resp, nil
}

func (k *KnowledgeApplicationService) MoveKnowledgeToLibrary(ctx context.Context, req *model.MoveKnowledgeToLibraryRequest) error {
	err := k.DomainSVC.MoveKnowledgeToLibrary(ctx, req)
	if err != nil {
		return err
	}
	err = k.eventBus.PublishResources(ctx, &resourceEntity.ResourceDomainEvent{
		OpType: resourceEntity.Updated,
		Resource: &resourceEntity.ResourceDocument{
			ResID:        req.KnowledgeID,
			ResType:      resource.ResType_Knowledge,
			APPID:        ptr.Of(int64(0)),
			UpdateTimeMS: ptr.Of(time.Now().UnixMilli()),
		},
	})
	if err != nil {
		logs.CtxErrorf(ctx, "publish resources failed, err: %v", err)
		return err
	}
	return nil
}
func (k *KnowledgeApplicationService) ListPhoto(ctx context.Context, req *dataset.ListPhotoRequest) (*dataset.ListPhotoResponse, error) {
	return k.listPhoto(ctx, req, false)
}

func (k *KnowledgeApplicationService) ListPhotoOpenAPI(ctx context.Context, req *dataset.ListPhotoRequest) (*dataset.ListPhotoResponse, error) {
	return k.listPhoto(ctx, req, true)
}

func (k *KnowledgeApplicationService) listPhoto(ctx context.Context, req *dataset.ListPhotoRequest, fromOpenAPI bool) (*dataset.ListPhotoResponse, error) {
	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}

	if !fromOpenAPI {
		err = k.checkReadAccess(ctx, uid)
		if err != nil {
			return nil, err
		}
	}

	resp := dataset.NewListPhotoResponse()
	var offset int
	if req.GetPage() >= 1 {
		offset = int(req.GetSize() * (req.GetPage() - 1))
	}
	listPhotoSliceReq := service.ListPhotoSliceRequest{
		KnowledgeID: req.GetDatasetID(),
		Limit:       ptr.Of(int(req.GetSize())),
		Offset:      &offset,
	}
	if req.Filter != nil {
		listPhotoSliceReq.HasCaption = req.Filter.HasCaption
	}
	listResp, err := k.DomainSVC.ListPhotoSlice(ctx, &listPhotoSliceReq)
	if err != nil {
		logs.CtxErrorf(ctx, "list document failed, err: %v", err)
		return resp, err
	}
	if len(listResp.Slices) == 0 {
		resp.Total = int32(listResp.Total)
		return resp, nil
	}
	docIDs := slices.Transform(listResp.Slices, func(item *entity.Slice) int64 {
		return item.DocumentID
	})
	listDocResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{DocumentIDs: docIDs, SelectAll: true})
	if err != nil {
		logs.CtxErrorf(ctx, "get documents by slice ids failed, err: %v", err)
		return resp, err
	}
	photos := k.packPhotoInfo(listResp.Slices, listDocResp.Documents)
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i].UpdateTime > photos[j].UpdateTime
	})
	resp.PhotoInfos = photos
	resp.Total = int32(listResp.Total)
	return resp, nil
}

func (k *KnowledgeApplicationService) packPhotoInfo(slices []*entity.Slice, documents []*entity.Document) []*dataset.PhotoInfo {
	captions := map[int64]string{}
	for i := range slices {
		captions[slices[i].DocumentID] = slices[i].GetSliceContent()
	}
	photoInfo := make([]*dataset.PhotoInfo, 0, len(documents))
	for _, document := range documents {
		photoStatus := convertDocumentStatus2Model(document.Status)
		photoInfo = append(photoInfo, &dataset.PhotoInfo{
			Name:       document.Name,
			DocumentID: document.ID,
			URL:        document.URL,
			Caption:    captions[document.ID],
			CreateTime: int32(document.CreatedAtMs / 1000),
			UpdateTime: int32(document.UpdatedAtMs / 1000),
			CreatorID:  document.CreatorID,
			Type:       string(document.FileExtension),
			Size:       int32(document.Size),
			Status:     photoStatus,
			SourceType: dataset.DocumentSource_Document,
		})
	}
	return photoInfo
}

func (k *KnowledgeApplicationService) PhotoDetail(ctx context.Context, req *dataset.PhotoDetailRequest) (*dataset.PhotoDetailResponse, error) {
	return k.photoDetail(ctx, req, false)
}

func (k *KnowledgeApplicationService) PhotoDetailOpenAPI(ctx context.Context, req *dataset.PhotoDetailRequest) (*dataset.PhotoDetailResponse, error) {
	return k.photoDetail(ctx, req, true)
}

func (k *KnowledgeApplicationService) photoDetail(ctx context.Context, req *dataset.PhotoDetailRequest, fromOpenAPI bool) (*dataset.PhotoDetailResponse, error) {
	resp := dataset.NewPhotoDetailResponse()
	if len(req.GetDocumentIds()) == 0 {
		resp.Code = 400
		resp.Msg = "document ids is empty"
		return resp, nil
	}

	uid, err := getUID(ctx, fromOpenAPI)
	if err != nil {
		return nil, err
	}

	if !fromOpenAPI {
		err = k.checkReadAccess(ctx, uid)
		if err != nil {
			return nil, err
		}
	}

	docIDs, err := slices.TransformWithErrorCheck(req.GetDocumentIds(), func(s string) (int64, error) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err
	})
	if err != nil {
		logs.CtxErrorf(ctx, "parse int failed, err: %v", err)
		return resp, err
	}
	listResp, err := k.DomainSVC.ListPhotoSlice(ctx, &service.ListPhotoSliceRequest{DocumentIDs: docIDs})
	if err != nil {
		logs.CtxErrorf(ctx, "list photo slice failed, err: %v", err)
		return resp, err
	}
	listDocResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{DocumentIDs: docIDs, SelectAll: true, KnowledgeID: req.GetDatasetID()})
	if err != nil {
		logs.CtxErrorf(ctx, "get documents by slice ids failed, err: %v", err)
		return resp, err
	}

	photos := k.packPhotoInfo(listResp.Slices, listDocResp.Documents)
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i].UpdateTime > photos[j].UpdateTime
	})
	resp.PhotoInfos = slices.ToMap(photos, func(item *dataset.PhotoInfo) (string, *dataset.PhotoInfo) {
		return strconv.FormatInt(item.DocumentID, 10), item
	})
	return resp, nil
}

func (k *KnowledgeApplicationService) ExtractPhotoCaption(ctx context.Context, req *dataset.ExtractPhotoCaptionRequest) (*dataset.ExtractPhotoCaptionResponse, error) {
	resp := dataset.NewExtractPhotoCaptionResponse()
	if req.GetDocumentID() == 0 {
		resp.Code = 400
		resp.Msg = "document id is empty"
		return resp, nil
	}
	uid := ctxutil.GetUIDFromCtx(ctx)
	if uid == nil {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", "session required"))
	}

	err := k.checkWriteAccess(ctx, uid, nil, []int64{req.GetDocumentID()}, nil)
	if err != nil {
		return nil, err
	}

	extractResp, err := k.DomainSVC.ExtractPhotoCaption(ctx, &service.ExtractPhotoCaptionRequest{DocumentID: req.GetDocumentID()})
	if err != nil {
		return resp, err
	}
	resp.Caption = extractResp.Caption
	return resp, nil
}

func (k *KnowledgeApplicationService) ListKnowledgeAPI(ctx context.Context, req *dataset.ListDatasetOpenApiRequest) (*dataset.ListDatasetOpenApiResponse, error) {
	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	var page, pageSize int
	if req.PageNum != nil && *req.PageNum > 0 {
		page = int(*req.PageNum)
	}
	if req.PageSize != nil && *req.PageSize > 0 {
		pageSize = int(*req.PageSize)
	}

	// OpenAPI request shape doesn't carry a scope_type; default behaviour is
	// ScopeAll (zero value), matching the UI default.
	request, err := k.buildListKnowledgeRequest(ctx, req.GetSpaceID(), req.Name, req.FormatType, page, pageSize, req.GetProjectID(), dataset.DatasetScopeType_ScopeAll, uid)
	if err != nil {
		logs.CtxErrorf(ctx, "build list knowledge request failed, err: %v", err)
		return dataset.NewListDatasetOpenApiResponse(), err
	}

	err = k.checkReadAccess(ctx, &uid)
	if err != nil {
		return dataset.NewListDatasetOpenApiResponse(), err
	}

	domainResp, err := k.DomainSVC.ListKnowledge(ctx, request)
	if err != nil {
		logs.CtxErrorf(ctx, "mget knowledge failed, err: %v", err)
		return dataset.NewListDatasetOpenApiResponse(), err
	}

	// OpenAPI flow: uid is the API-key owner (already checked non-zero above);
	// CanEdit will be true only for KBs that user actually owns.
	knowledgeMap, err := batchConvertKnowledgeEntity2Model(ctx, domainResp.KnowledgeList, uid)
	if err != nil {
		logs.CtxErrorf(ctx, "batch convert knowledge entity failed, err: %v", err)
		return dataset.NewListDatasetOpenApiResponse(), err
	}

	datasetList := make([]*dataset.Dataset, 0)
	for i := range domainResp.KnowledgeList {
		datasetList = append(datasetList, knowledgeMap[domainResp.KnowledgeList[i].ID])
	}

	code := int64(0)
	msg := "success"
	return &dataset.ListDatasetOpenApiResponse{
		Data: &dataset.ListDatasetOpenApiData{
			DatasetList: datasetList,
			TotalCount:  int32(domainResp.Total),
		},
		Code: &code,
		Msg:  &msg,
	}, nil
}

func (k *KnowledgeApplicationService) UpdateKnowledgeAPI(ctx context.Context, req *dataset.UpdateDatasetOpenApiRequest) (*dataset.UpdateDatasetOpenApiResponse, error) {
	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	err := k.checkWriteAccess(ctx, &uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	err = k.updateKnowledgeInternal(ctx, req.GetDatasetID(), &req.Name, &req.Description, nil, nil)
	if err != nil {
		return dataset.NewUpdateDatasetOpenApiResponse(), err
	}

	err = k.publishUpdateKnowledgeEvent(ctx, req.GetDatasetID(), &req.Name, now)
	if err != nil {
		return dataset.NewUpdateDatasetOpenApiResponse(), err
	}

	code := int64(0)
	msg := "success"
	return &dataset.UpdateDatasetOpenApiResponse{
		Code: &code,
		Msg:  &msg,
	}, nil
}

func (k *KnowledgeApplicationService) DeleteKnowledgeAPI(ctx context.Context, req *dataset.DeleteDatasetOpenApiRequest) (*dataset.DeleteDatasetOpenApiResponse, error) {
	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	err := k.checkWriteAccess(ctx, &uid, ptr.Of(req.GetDatasetID()), nil, nil)
	if err != nil {
		return nil, err
	}

	err = k.deleteKnowledgeInternal(ctx, req.GetDatasetID())
	if err != nil {
		return dataset.NewDeleteDatasetOpenApiResponse(), err
	}

	err = k.publishDeleteKnowledgeEvent(ctx, req.GetDatasetID())
	if err != nil {
		return dataset.NewDeleteDatasetOpenApiResponse(), err
	}

	code := int64(0)
	msg := "success"
	return &dataset.DeleteDatasetOpenApiResponse{
		Code: &code,
		Msg:  &msg,
	}, nil
}

func (k *KnowledgeApplicationService) GetDocumentProgressAPI(ctx context.Context, req *dataset.GetDocumentProgressOpenApiRequest) (*dataset.GetDocumentProgressOpenApiResponse, error) {
	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	err := k.checkReadAccess(ctx, &uid)
	if err != nil {
		return dataset.NewGetDocumentProgressOpenApiResponse(), err
	}

	domainResp, err := k.DomainSVC.MGetDocumentProgress(ctx, &service.MGetDocumentProgressRequest{
		DocumentIDs: req.GetDocumentIds(),
	})
	if err != nil {
		logs.CtxErrorf(ctx, "mget document progress failed, err: %v", err)
		return dataset.NewGetDocumentProgressOpenApiResponse(), err
	}

	progressList := make([]*dataset.DocumentProgress, 0)
	for i := range domainResp.ProgressList {
		progressList = append(progressList, &dataset.DocumentProgress{
			DocumentID:     domainResp.ProgressList[i].ID,
			Progress:       int32(domainResp.ProgressList[i].Progress),
			Status:         convertDocumentStatus2Model(domainResp.ProgressList[i].Status),
			StatusDescript: &domainResp.ProgressList[i].StatusMsg,
			DocumentName:   domainResp.ProgressList[i].Name,
			RemainingTime:  &domainResp.ProgressList[i].RemainingSec,
			Size:           &domainResp.ProgressList[i].Size,
			Type:           &domainResp.ProgressList[i].FileExtension,
			URL:            ptr.Of(domainResp.ProgressList[i].URL),
		})
	}

	return &dataset.GetDocumentProgressOpenApiResponse{
		Data: &dataset.GetDocumentProgressOpenApiData{
			Data: progressList,
		},
		Code: 0,
		Msg:  "success",
	}, nil
}

func (k *KnowledgeApplicationService) ListPhotoAPI(ctx context.Context, req *dataset.ListPhotoOpenApiRequest) (*dataset.ListPhotoOpenApiResponse, error) {
	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	err := k.checkReadAccess(ctx, &uid)
	if err != nil {
		return nil, err
	}

	resp := dataset.NewListPhotoOpenApiResponse()
	var offset int
	if req.GetPageNum() >= 1 {
		offset = int(req.GetPageSize() * (req.GetPageNum() - 1))
	}
	listPhotoSliceReq := service.ListPhotoSliceRequest{
		KnowledgeID: req.GetDatasetID(),
		Limit:       ptr.Of(int(req.GetPageSize())),
		Offset:      &offset,
	}
	if req.HasCaption != nil {
		listPhotoSliceReq.HasCaption = req.HasCaption
	}
	listResp, err := k.DomainSVC.ListPhotoSlice(ctx, &listPhotoSliceReq)
	if err != nil {
		logs.CtxErrorf(ctx, "list document failed, err: %v", err)
		return resp, err
	}
	if len(listResp.Slices) == 0 {
		resp.Data = &dataset.ListPhotoOpenApiData{
			PhotoInfos: []*dataset.PhotoInfo{},
			TotalCount: int32(listResp.Total),
		}
		code := int64(0)
		msg := "success"
		resp.Code = &code
		resp.Msg = &msg
		return resp, nil
	}
	docIDs := slices.Transform(listResp.Slices, func(item *entity.Slice) int64 {
		return item.DocumentID
	})
	listDocResp, err := k.DomainSVC.ListDocument(ctx, &service.ListDocumentRequest{DocumentIDs: docIDs, SelectAll: true})
	if err != nil {
		logs.CtxErrorf(ctx, "get documents by slice ids failed, err: %v", err)
		return resp, err
	}
	photos := k.packPhotoInfo(listResp.Slices, listDocResp.Documents)
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i].UpdateTime > photos[j].UpdateTime
	})

	code := int64(0)
	msg := "success"
	resp.Data = &dataset.ListPhotoOpenApiData{
		PhotoInfos: photos,
		TotalCount: int32(listResp.Total),
	}
	resp.Code = &code
	resp.Msg = &msg
	return resp, nil
}

func (k *KnowledgeApplicationService) CreateKnowledgeAPI(ctx context.Context, req *dataset.CreateDatasetOpenApiRequest) (*dataset.CreateDatasetOpenApiResponse, error) {
	documentType := convertDocumentTypeDataset2Entity(req.GetFormatType())
	if documentType == model.DocumentTypeUnknown {
		return dataset.NewCreateDatasetOpenApiResponse(), errors.New("unknown document type")
	}

	uid := ctxutil.MustGetUIDFromApiAuthCtx(ctx)
	if uid == 0 {
		return nil, errorx.New(errno.ErrKnowledgePermissionCode, errorx.KV("msg", " apiKeyInfo is nil"))
	}

	domainResp, err := k.createKnowledgeInternal(ctx, req.GetName(), req.GetDescription(), req.GetSpaceID(), uid, req.GetProjectID(), req.GetFormatType(), "")
	if err != nil {
		return dataset.NewCreateDatasetOpenApiResponse(), err
	}

	err = k.publishKnowledgeEvent(ctx, domainResp.KnowledgeID, req.GetName(), req.GetFormatType(), req.GetSpaceID(), req.GetProjectID(), uid, domainResp.CreatedAtMs)
	if err != nil {
		return dataset.NewCreateDatasetOpenApiResponse(), err
	}

	code := int64(0)
	msg := "success"
	return &dataset.CreateDatasetOpenApiResponse{
		Data: &dataset.CreateDatasetOpenApiData{
			DatasetID: domainResp.KnowledgeID,
		},
		Code: &code,
		Msg:  &msg,
	}, nil
}

type DeleteAppKnowledgeRequest struct {
	AppID int64 `json:"app_id"`
}
