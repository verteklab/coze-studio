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

package ragimpl

import (
	"context"
	"errors"
	"time"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
	"github.com/coze-dev/coze-studio/backend/types/consts"
)

// modelOverride lets a caller (the CreateDataset application handler) supply a
// non-default embedding model for a specific CreateKB call. Wiring lives in
// the application layer, which attaches a consts.RagModelOverride to ctx
// before calling into the domain.
type modelOverride struct {
	TextModelID  string
	ImageModelID string
}

func getModelOverride(ctx context.Context) (modelOverride, bool) {
	v, ok := consts.RagModelOverrideFromContext(ctx)
	if !ok {
		return modelOverride{}, false
	}
	return modelOverride{TextModelID: v.TextModelID, ImageModelID: v.ImageModelID}, true
}

// defaultChunkTypesFor maps a coze DocumentType to the rag-side chunk types we
// enable on the KB at creation. The rag service uses these to validate the
// chunking strategies it later accepts on CreateDocument.
func defaultChunkTypesFor(t knowledgeModel.DocumentType) []string {
	switch t {
	case knowledgeModel.DocumentTypeTable:
		return []string{"row"}
	case knowledgeModel.DocumentTypeImage:
		return []string{"image"}
	default:
		// text / unknown
		return []string{"paragraph", "fixed_size", "custom"}
	}
}

// defaultSourceModalitiesFor mirrors defaultChunkTypesFor but for the
// `supported_source_modalities` field on the KB.
func defaultSourceModalitiesFor(t knowledgeModel.DocumentType) []string {
	switch t {
	case knowledgeModel.DocumentTypeImage:
		return []string{"image_source"}
	case knowledgeModel.DocumentTypeTable:
		// Tables come in as text (csv/xlsx parsed to text rows) at the rag layer.
		return []string{"text_source"}
	default:
		return []string{"text_source"}
	}
}

// statusToRag converts coze's KnowledgeStatus to the rag string enum.
// Rag supports two states: "active" and "disabled".
func statusToRag(s knowledgeModel.KnowledgeStatus) string {
	if s == knowledgeModel.KnowledgeStatusDisable {
		return "disabled"
	}
	return "active"
}

// statusFromRag is the inverse mapping. Unknown values fail-closed to Disable
// so a drifted rag enum doesn't silently appear "Enabled" in the UI.
func statusFromRag(s string) knowledgeModel.KnowledgeStatus {
	switch s {
	case "active":
		return knowledgeModel.KnowledgeStatusEnable
	case "disabled":
		return knowledgeModel.KnowledgeStatusDisable
	default:
		return knowledgeModel.KnowledgeStatusDisable
	}
}

// hydrateKnowledge fuses authoritative rag data (name, description, status,
// timestamps) with coze-only audit fields stored in the mapping row (icon,
// app_id, creator_id). The returned entity has SpaceID==0 because Phase 1
// drops space-scoped isolation — rag enforces tenancy.
func hydrateKnowledge(kb *contract.KB, m *KBMapping) *knowledgeModel.Knowledge {
	if kb == nil {
		return nil
	}
	out := &knowledgeModel.Knowledge{
		Info: knowledgeModel.Info{
			ID:          m.CozeID,
			Name:        kb.Name,
			Description: kb.Description,
			IconURI:     m.IconURI,
			CreatorID:   m.CreatorID,
			SpaceID:     0,
			AppID:       m.AppID,
			CreatedAtMs: kb.CreatedAt.UnixMilli(),
			UpdatedAtMs: kb.UpdatedAt.UnixMilli(),
		},
		Status: statusFromRag(kb.Status),
	}
	return out
}

// CreateKnowledge proxies to rag.CreateKB and then inserts the int64<->UUID
// mapping. On mapping-insert failure we attempt to clean up the rag KB so we
// don't leak orphan state.
func (i *Impl) CreateKnowledge(ctx context.Context, req *service.CreateKnowledgeRequest) (*service.CreateKnowledgeResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}

	textModel := i.defaultTextEmbeddingModelID
	imageModel := i.defaultImageEmbeddingModelID
	if ov, ok := getModelOverride(ctx); ok {
		if ov.TextModelID != "" {
			textModel = ov.TextModelID
		}
		if ov.ImageModelID != "" {
			imageModel = ov.ImageModelID
		}
	}

	ragReq := &contract.CreateKBRequest{
		Name:                      req.Name,
		Description:               req.Description,
		TextEmbeddingModelID:      textModel,
		ImageEmbeddingModelID:     imageModel,
		EnabledChunkTypes:         defaultChunkTypesFor(req.FormatType),
		SupportedSourceModalities: defaultSourceModalitiesFor(req.FormatType),
		DefaultFusionPolicy:       contract.FusionPolicy{Mode: "weighted_rrf", RrfK: 60},
	}
	kb, err := i.rag.CreateKB(ctx, tenant, ragReq)
	if err != nil {
		return nil, err
	}

	cozeID, err := i.idgen.GenID(ctx)
	if err != nil {
		// Best-effort cleanup so the rag KB doesn't become an orphan.
		if delErr := i.rag.DeleteKB(ctx, tenant, kb.KBID); delErr != nil {
			logs.CtxWarnf(ctx, "ragimpl: rollback DeleteKB after idgen failure: %v", delErr)
		}
		return nil, err
	}

	nowMs := time.Now().UnixMilli()
	if err := i.mapping.InsertKB(ctx, cozeID, kb.KBID, req.IconUri, req.AppID, req.CreatorID, nowMs); err != nil {
		// Roll back the rag KB so we don't leak a tenant-side KB with no coze handle.
		if delErr := i.rag.DeleteKB(ctx, tenant, kb.KBID); delErr != nil {
			logs.CtxWarnf(ctx, "ragimpl: rollback DeleteKB after InsertKB failure: %v", delErr)
		}
		return nil, err
	}

	return &service.CreateKnowledgeResponse{
		KnowledgeID: cozeID,
		CreatedAtMs: nowMs,
	}, nil
}

// UpdateKnowledge forwards a partial update to rag. The coze mapping table has
// no name/description/status columns, so nothing is mirrored locally.
func (i *Impl) UpdateKnowledge(ctx context.Context, req *service.UpdateKnowledgeRequest) error {
	m, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
	if err != nil {
		return err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return err
	}
	ragReq := &contract.UpdateKBRequest{
		Name:        req.Name,
		Description: req.Description,
	}
	if req.Status != nil {
		s := statusToRag(*req.Status)
		ragReq.Status = &s
	}
	_, err = i.rag.UpdateKB(ctx, tenant, m.RagKBID, ragReq)
	return err
}

// DeleteKnowledge soft-deletes the coze mapping row first, then asks rag to
// delete the KB. If rag fails the mapping is restored so the caller can retry
// without ending up with a "deleted" coze row pointing at a still-alive rag KB.
func (i *Impl) DeleteKnowledge(ctx context.Context, req *service.DeleteKnowledgeRequest) error {
	m, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
	if err != nil {
		return err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return err
	}
	if err := i.mapping.SoftDeleteKB(ctx, req.KnowledgeID); err != nil {
		return err
	}
	if err := i.rag.DeleteKB(ctx, tenant, m.RagKBID); err != nil {
		if restoreErr := i.mapping.RestoreKB(ctx, req.KnowledgeID); restoreErr != nil {
			logs.CtxErrorf(ctx, "ragimpl: RestoreKB after rag DeleteKB failure also failed: %v (original: %v)", restoreErr, err)
		}
		return err
	}
	return nil
}

// GetKnowledgeByID fetches the rag KB live and hydrates a coze entity from it.
func (i *Impl) GetKnowledgeByID(ctx context.Context, req *service.GetKnowledgeByIDRequest) (*service.GetKnowledgeByIDResponse, error) {
	m, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	kb, err := i.rag.GetKB(ctx, tenant, m.RagKBID)
	if err != nil {
		return nil, err
	}
	return &service.GetKnowledgeByIDResponse{Knowledge: hydrateKnowledge(kb, m)}, nil
}

// MGetKnowledgeByID resolves mappings in one shot then calls rag per KB. We
// keep it sequential for now — rag doesn't expose a batched GetKB, and the
// expected fan-out is small (UI lists ~10-50 KBs per page).
func (i *Impl) MGetKnowledgeByID(ctx context.Context, req *service.MGetKnowledgeByIDRequest) (*service.MGetKnowledgeByIDResponse, error) {
	mappings, err := i.mapping.KBsByCozeIDs(ctx, req.KnowledgeIDs)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*knowledgeModel.Knowledge, 0, len(mappings))
	for _, m := range mappings {
		kb, err := i.rag.GetKB(ctx, tenant, m.RagKBID)
		if err != nil {
			// Skip individual failures; full failure on first error would mask N-1 valid rows.
			logs.CtxWarnf(ctx, "ragimpl: MGetKnowledgeByID: GetKB(%s) failed: %v", m.RagKBID, err)
			continue
		}
		out = append(out, hydrateKnowledge(kb, m))
	}
	return &service.MGetKnowledgeByIDResponse{Knowledge: out}, nil
}

// ListKnowledge calls rag.ListKBs and resolves each rag KB back to its coze
// mapping for hydration. KBs that have no mapping row are skipped — they
// belong to the rag tenant but aren't owned by this coze deployment.
func (i *Impl) ListKnowledge(ctx context.Context, req *service.ListKnowledgeRequest) (*service.ListKnowledgeResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}

	// By-id path. Application-layer callers (CreateDocument's owner check,
	// DatasetDetail, etc.) pass a specific ID set and then index [0] of the
	// response, so the result must be the exact KB(s) requested. rag's
	// /knowledgebases endpoint has no by-id filter, so we resolve each coze
	// id to its rag UUID via the mapping table and fetch one at a time.
	//
	// Without this path the function fell through to ListKBs and silently
	// returned tenant-wide pages, causing every ownership check to see the
	// first KB in the tenant — wrong the moment a tenant contained more
	// than one KB.
	if len(req.IDs) > 0 {
		out := make([]*knowledgeModel.Knowledge, 0, len(req.IDs))
		for _, cozeID := range req.IDs {
			m, err := i.mapping.KBByCozeID(ctx, cozeID)
			if err != nil {
				if errors.Is(err, ErrMappingNotFound) {
					// Mirror the list-all branch: unknown mapping is "not
					// owned by this deployment", not a hard error.
					continue
				}
				return nil, err
			}
			kb, err := i.rag.GetKB(ctx, tenant, m.RagKBID)
			if err != nil {
				return nil, err
			}
			out = append(out, hydrateKnowledge(kb, m))
		}
		return &service.ListKnowledgeResponse{
			KnowledgeList: out,
			Total:         int64(len(out)),
		}, nil
	}

	// NOTE: other req fields (SpaceID, AppID, Name, Status, UserID, Query,
	// Order*, FormatType) are not yet honoured by ragimpl — they were
	// unimplemented before this patch too. Filed as known gap; current
	// callers either rely on IDs or accept the unfiltered tenant page.

	page, pageSize := 1, 20
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}
	if req.PageSize != nil && *req.PageSize > 0 {
		pageSize = *req.PageSize
	}
	resp, err := i.rag.ListKBs(ctx, &contract.ListKBsRequest{
		TenantID: tenant,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*knowledgeModel.Knowledge, 0, len(resp.Items))
	for idx := range resp.Items {
		kb := resp.Items[idx]
		m, err := i.mapping.kbByRagID(ctx, kb.KBID)
		if err != nil {
			// Not owned by this coze deployment (or mapping race) — skip silently.
			continue
		}
		out = append(out, hydrateKnowledge(&kb, m))
	}
	return &service.ListKnowledgeResponse{
		KnowledgeList: out,
		Total:         int64(resp.Total),
	}, nil
}

// GetCapabilities fetches rag-side capabilities for a coze KB. Resolves the
// coze KB id to its rag UUID via the mapping table and passes through. The
// response is the rag-side typed shape; coze does not translate to an entity
// type — R2-D-frontend will introduce that translation when the UI's needs
// are concrete.
func (i *Impl) GetCapabilities(ctx context.Context, cozeKBID int64) (*contract.KBCapabilities, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	m, err := i.mapping.KBByCozeID(ctx, cozeKBID)
	if err != nil {
		return nil, err
	}
	return i.rag.GetCapabilities(ctx, tenant, m.RagKBID)
}
