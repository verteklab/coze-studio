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
	"encoding/json"
	"time"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/document/parser"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
)

// sourceModalityFor selects the rag source_modality string from a coze Document.
// Phase 1: text + table docs are "text_source"; image docs are "image_source".
// "scanned_document_source" is not yet emitted (pending Task 18/OCR work).
func sourceModalityFor(d *entity.Document) string {
	if d != nil && d.Type == knowledgeModel.DocumentTypeImage {
		return "image_source"
	}
	return "text_source"
}

// buildDocMetadata injects the coze-side identifiers we want rag to round-trip
// back to us in retrieval hits (rag stores this as opaque JSON on the doc).
// Keep keys snake_case to match rag's convention.
func buildDocMetadata(d *entity.Document) map[string]any {
	md := map[string]any{}
	if d == nil {
		return md
	}
	if d.CreatorID != 0 {
		md["creator_id"] = d.CreatorID
	}
	if d.Name != "" {
		md["coze_document_name"] = d.Name
	}
	return md
}

// buildDocumentEntity constructs an entity.Document from a rag-side Document
// response plus the coze-side mapping row. Shared between ListDocument and
// MGetDocument so that future field additions land in one place.
//
// TODO(coze-rag): rag's file_type is unconstrained on the wire; the cast
// to parser.FileExtension accepts anything but parser dispatch downstream
// only knows coze's enum. Validate via parser.ValidateFileExtension (or
// filter rag's supported set via R2-D's /capabilities) when the enum
// stabilizes.
func buildDocumentEntity(dm *DocMapping, rd *contract.Document) *entity.Document {
	return &entity.Document{
		Info: knowledgeModel.Info{
			ID:          dm.CozeID,
			Name:        rd.Filename,
			CreatorID:   dm.CreatorID,
			CreatedAtMs: rd.CreatedAt.UnixMilli(),
			UpdatedAtMs: rd.UpdatedAt.UnixMilli(),
		},
		KnowledgeID:   dm.KBID,
		Status:        RagStatusToEntity(rd.Status),
		FileExtension: parser.FileExtension(rd.FileType),
		Size:          dm.Size,
	}
}

// taskStatusToDoc maps a rag Task.Status string to coze's DocumentStatus enum.
//
// rag task FSM:  pending -> running [-> retrying] -> success | failed
// coze doc FSM:  Init    -> Chunking              -> Enable  | Failed
//
// "pending" lands in Init (queued, not started). "running" and "retrying" both
// collapse to Chunking (the user-visible "processing" phase). Any unknown
// value fails closed to Failed — a stuck task should be visible, not "ready".
func taskStatusToDoc(s string) entity.DocumentStatus {
	switch s {
	case "pending":
		return entity.DocumentStatusInit
	case "running", "retrying":
		return entity.DocumentStatusChunking
	case "success":
		return entity.DocumentStatusEnable
	case "failed":
		return entity.DocumentStatusFailed
	default:
		return entity.DocumentStatusFailed
	}
}

// CreateDocument creates one rag document per input entity. Each rag doc gets
// an int64 coze id from idgen, and a mapping row is written before we return.
//
// Since the 2026-05-14 rag contract change, rag's POST .../documents is
// multipart-with-bytes. We fetch the file bytes from MinIO (the URI is the
// coze-side object key) and forward them inline. Sequential per-doc loop is
// preserved — the upload UI typical batch is 1-20 and rag's CreateDocument is
// already async (it returns a task_id immediately).
func (i *Impl) CreateDocument(ctx context.Context, req *service.CreateDocumentRequest) (*service.CreateDocumentResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Document, 0, len(req.Documents))
	for _, d := range req.Documents {
		m, err := i.mapping.KBByCozeID(ctx, d.KnowledgeID)
		if err != nil {
			return nil, err
		}

		fileBytes, err := i.storage.GetObject(ctx, d.URI)
		if err != nil {
			return nil, err
		}

		var chunkSize, chunkOverlap *int
		if d.ChunkingStrategy != nil {
			if d.ChunkingStrategy.ChunkSize > 0 {
				s := int(d.ChunkingStrategy.ChunkSize)
				chunkSize = &s
			}
			if d.ChunkingStrategy.Overlap > 0 {
				o := int(d.ChunkingStrategy.Overlap)
				chunkOverlap = &o
			}
		}

		// buildDocMetadata already produces snake_case keys rag expects.
		// Marshal errors here are not surfaced: the map only ever holds
		// primitives that always marshal cleanly. An empty map serialises to
		// "{}" which we then drop to "" so rag sees the optional field absent.
		mdJSON, _ := json.Marshal(buildDocMetadata(d))
		extraMetadata := string(mdJSON)
		if extraMetadata == "{}" {
			extraMetadata = ""
		}

		ragReq := &contract.CreateDocumentRequest{
			FileBytes:      fileBytes,
			Filename:       d.Name,
			FileType:       string(d.FileExtension),
			SourceModality: sourceModalityFor(d),
			ChunkSize:      chunkSize,
			ChunkOverlap:   chunkOverlap,
			ExtraMetadata:  extraMetadata,
		}
		ragResp, err := i.rag.CreateDocument(ctx, tenant, m.RagKBID, ragReq)
		if err != nil {
			return nil, err
		}
		cozeID, err := i.idgen.GenID(ctx)
		if err != nil {
			// Best-effort cleanup: rag has accepted the doc but we can't track it.
			if delErr := i.rag.DeleteDocument(ctx, tenant, m.RagKBID, ragResp.DocID); delErr != nil {
				logs.CtxWarnf(ctx, "ragimpl: rollback DeleteDocument after idgen failure: %v", delErr)
			}
			return nil, err
		}
		nowMs := time.Now().UnixMilli()
		if err := i.mapping.InsertDoc(ctx, cozeID, ragResp.DocID, d.KnowledgeID, d.CreatorID, ragResp.TaskID, nowMs, int64(len(fileBytes))); err != nil {
			if delErr := i.rag.DeleteDocument(ctx, tenant, m.RagKBID, ragResp.DocID); delErr != nil {
				logs.CtxWarnf(ctx, "ragimpl: rollback DeleteDocument after InsertDoc failure: %v", delErr)
			}
			return nil, err
		}
		// Translate the rag status string back to coze's enum so the caller
		// sees the same shape it would have under the legacy implementation.
		copied := *d
		copied.ID = cozeID
		copied.Status = RagStatusToEntity(ragResp.Status)
		copied.CreatedAtMs = nowMs
		copied.UpdatedAtMs = nowMs
		out = append(out, &copied)
	}
	return &service.CreateDocumentResponse{Documents: out}, nil
}

// DeleteDocument soft-deletes the mapping row first, then rag, then restores
// the mapping on rag failure — mirrors DeleteKnowledge's invariant.
//
// rag's document endpoints are nested under the KB
// (DELETE /api/v1/knowledgebases/{kb_id}/documents/{doc_id}), so we resolve
// the coze KB id from the doc mapping to its rag UUID before issuing the call.
func (i *Impl) DeleteDocument(ctx context.Context, req *service.DeleteDocumentRequest) error {
	m, err := i.mapping.DocByCozeID(ctx, req.DocumentID)
	if err != nil {
		return err
	}
	kb, err := i.mapping.KBByCozeID(ctx, m.KBID)
	if err != nil {
		return err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return err
	}
	if err := i.mapping.SoftDeleteDoc(ctx, req.DocumentID); err != nil {
		return err
	}
	if err := i.rag.DeleteDocument(ctx, tenant, kb.RagKBID, m.RagDocID); err != nil {
		if restoreErr := i.mapping.RestoreDoc(ctx, req.DocumentID); restoreErr != nil {
			logs.CtxErrorf(ctx, "ragimpl: RestoreDoc after rag DeleteDocument failure also failed: %v (original: %v)", restoreErr, err)
		}
		return err
	}
	return nil
}

// ListDocument fetches docs for the KB from rag and translates each rag doc
// back to a coze entity by reverse-lookup on rag_doc_id. Docs that have no
// mapping row are skipped (drift between rag and the mapping table — should
// never happen in steady state).
//
// Phase 1: page/page-size come from req.Limit/Offset if present; otherwise we
// ask rag for a generous first page. Cursor-based pagination is not wired.
func (i *Impl) ListDocument(ctx context.Context, req *service.ListDocumentRequest) (*service.ListDocumentResponse, error) {
	kbMapping, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	page := 1
	pageSize := 50
	if req.Limit != nil && *req.Limit > 0 {
		pageSize = *req.Limit
	}
	if req.Offset != nil && *req.Offset > 0 && pageSize > 0 {
		// rag's contract is 1-indexed page numbers; translate offset/limit best-effort.
		page = (*req.Offset / pageSize) + 1
	}
	resp, err := i.rag.ListDocuments(ctx, tenant, kbMapping.RagKBID, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Document, 0, len(resp.Items))
	for idx := range resp.Items {
		rd := &resp.Items[idx]
		dm, err := i.mapping.docByRagID(ctx, rd.DocID)
		if err != nil {
			// Doc exists in rag but we have no coze handle — drift; skip silently.
			continue
		}
		out = append(out, buildDocumentEntity(dm, rd))
	}
	return &service.ListDocumentResponse{
		Documents: out,
		Total:     int64(resp.Total),
	}, nil
}

// MGetDocument resolves each int64 doc id and queries rag per doc. Missing
// docs are skipped (consistent with MGetKnowledgeByID).
func (i *Impl) MGetDocument(ctx context.Context, req *service.MGetDocumentRequest) (*service.MGetDocumentResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Document, 0, len(req.DocumentIDs))
	for _, id := range req.DocumentIDs {
		m, err := i.mapping.DocByCozeID(ctx, id)
		if err != nil {
			continue
		}
		kb, err := i.mapping.KBByCozeID(ctx, m.KBID)
		if err != nil {
			// Doc mapping references a KB we no longer track; skip rather than
			// fail the batch — consistent with the docByRagID drift handling
			// elsewhere in this file.
			logs.CtxWarnf(ctx, "ragimpl: MGetDocument: KBByCozeID(%d) failed: %v", m.KBID, err)
			continue
		}
		rd, err := i.rag.GetDocument(ctx, tenant, kb.RagKBID, m.RagDocID)
		if err != nil {
			logs.CtxWarnf(ctx, "ragimpl: MGetDocument: GetDocument(%s) failed: %v", m.RagDocID, err)
			continue
		}
		out = append(out, buildDocumentEntity(m, rd))
	}
	return &service.MGetDocumentResponse{Documents: out}, nil
}

// MGetDocumentProgress reads task status live from rag. We deliberately do NOT
// mirror the status back into the mapping table — there is no status column,
// and rag is the system of record. If the mapping row has no last_task_id,
// the doc finished ingest before this method was added (or didn't go through
// a task at all) — treat as Enable.
func (i *Impl) MGetDocumentProgress(ctx context.Context, req *service.MGetDocumentProgressRequest) (*service.MGetDocumentProgressResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]*service.DocumentProgress, 0, len(req.DocumentIDs))
	for _, id := range req.DocumentIDs {
		m, err := i.mapping.DocByCozeID(ctx, id)
		if err != nil {
			// Soft-deleted or missing — surface nothing rather than fail the batch.
			continue
		}
		dp := &service.DocumentProgress{ID: m.CozeID}
		if m.LastTaskID == "" {
			dp.Status = entity.DocumentStatusEnable
			dp.Progress = 100
			list = append(list, dp)
			continue
		}
		task, err := i.rag.GetTask(ctx, tenant, m.LastTaskID)
		if err != nil {
			logs.CtxWarnf(ctx, "ragimpl: MGetDocumentProgress: GetTask(%s) failed: %v", m.LastTaskID, err)
			dp.Status = entity.DocumentStatusFailed
			dp.StatusMsg = err.Error()
			list = append(list, dp)
			continue
		}
		dp.Status = taskStatusToDoc(task.Status)
		dp.Progress = progressForStatus(task.Status)
		dp.StatusMsg = task.ErrorMsg
		list = append(list, dp)
	}
	return &service.MGetDocumentProgressResponse{ProgressList: list}, nil
}

// RetryDocument re-runs ingestion for a previously-failed coze document.
// Resolves coze→rag IDs via the mapping table, forwards to the rag client,
// bumps rag_doc_mapping.last_task_id so MGetDocumentProgress follows the
// retry's new task, and returns a refreshed entity.Document with the
// post-retry status. The mapping update is best-effort on failure: rag
// has already accepted the retry, so a logged warning is preferable to
// returning an error that suggests the retry didn't trigger.
func (i *Impl) RetryDocument(ctx context.Context, req *service.RetryDocumentRequest) (*service.RetryDocumentResponse, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	dm, err := i.mapping.DocByCozeID(ctx, req.DocumentID)
	if err != nil {
		return nil, err
	}
	kb, err := i.mapping.KBByCozeID(ctx, dm.KBID)
	if err != nil {
		return nil, err
	}
	ragResp, err := i.rag.RetryDocument(ctx, tenant, kb.RagKBID, dm.RagDocID)
	if err != nil {
		return nil, err
	}
	if err := i.mapping.UpdateLastTaskID(ctx, req.DocumentID, ragResp.TaskID); err != nil {
		logs.CtxWarnf(ctx, "ragimpl: RetryDocument: UpdateLastTaskID(%d, %s) failed: %v", req.DocumentID, ragResp.TaskID, err)
	}
	nowMs := time.Now().UnixMilli()
	// The returned entity is intentionally sparse: only fields callable
	// without an additional rag round-trip are populated. The frontend
	// discards this body and re-polls MGetDocumentProgress (which reads
	// the freshly-bumped mapping.last_task_id), so richer fields would be
	// dead weight on the wire. If a future caller needs Name/Size/FileType
	// post-retry, fetch via MGetDocument or enrich here with a GetDocument
	// call.
	refreshed := &entity.Document{
		Info: knowledgeModel.Info{
			ID:          dm.CozeID,
			CreatorID:   dm.CreatorID,
			UpdatedAtMs: nowMs,
		},
		KnowledgeID: dm.KBID,
		Status:      RagStatusToEntity(ragResp.Status),
	}
	return &service.RetryDocumentResponse{Document: refreshed}, nil
}
