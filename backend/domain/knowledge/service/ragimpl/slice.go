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
	"strings"
	"time"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/pkg/logs"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// listPhotoSlicePageCap mirrors rag's MGetChunksRequest.max_length (200) so
// the HasCaption post-filter has a fixed upper bound. When the caller would
// be filtering past this cap, ListPhotoSlice logs a WARN that the filter is
// approximate. See §9 Q1 in 2026-05-15-r2g-manual-slice-design.md for the
// chosen strategy.
const listPhotoSlicePageCap = 200

// rawContentToChunkPayload translates coze's []*SliceContent into the rag
// CreateChunkRequest fields. The rules (per spec §5.4):
//
//   - empty RawContent -> reject
//   - any element with Type=Table -> reject (manual table CRUD is pending)
//   - exactly one element AND that element's Image != nil -> image_chunk
//     (Image's Type field is irrelevant here; SliceContentType has no Image
//     value in entity/knowledge.go, so Image presence is the discriminator)
//   - everything else -> text_chunk, with multiple Text entries joined by
//     newline to match the ingestion pipeline's single-string convention
//
// Returns the (chunk_type, content, image) triple plus an error.
func rawContentToChunkPayload(rc []*knowledgeModel.SliceContent) (string, string, *contract.ChunkImage, error) {
	if len(rc) == 0 {
		return "", "", nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
			errorx.KV("msg", "RawContent must not be empty"))
	}
	for _, item := range rc {
		if item == nil {
			continue
		}
		if item.Type == knowledgeModel.SliceContentTypeTable {
			return "", "", nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
				errorx.KV("msg", "manual table chunk CRUD pending; use bucket-B table ingestion instead"))
		}
	}
	// Image chunks must be sole-element so the chunk has one and only one image.
	if len(rc) == 1 && rc[0] != nil && rc[0].Image != nil {
		img := rc[0].Image
		out := &contract.ChunkImage{
			ImageRef: img.URI,
			OCRUsed:  img.OCR,
		}
		if img.OCRText != nil {
			out.OCRText = *img.OCRText
		}
		return "image_chunk", "", out, nil
	}
	// Text chunk: concatenate any Text entries. Empty after concat -> reject;
	// rag's pydantic validator rejects empty content with 40004 anyway, but
	// pre-rejecting here yields a clearer message.
	parts := make([]string, 0, len(rc))
	for _, item := range rc {
		if item == nil || item.Text == nil {
			continue
		}
		if *item.Text != "" {
			parts = append(parts, *item.Text)
		}
	}
	if len(parts) == 0 {
		return "", "", nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
			errorx.KV("msg", "RawContent has no usable text content"))
	}
	return "text_chunk", strings.Join(parts, "\n"), nil, nil
}

// resolveCozeSliceID looks up (and lazily inserts) the coze int64 id for a
// rag chunk. Used by every read path that returns a Slice. The wrapper exists
// to centralise the (logged) failure mode -- if the mapping write fails, the
// hit is still returned with Slice.Info.ID = 0 rather than dropping it. The
// surface degradation matches the pre-R2-G behaviour for callers that didn't
// rely on a non-zero id; callers that do rely on it (UI re-edit, retrieval
// citation linking) will see a graceful "no id yet" state on the next call.
func (i *Impl) resolveCozeSliceID(ctx context.Context, ragChunkID, ragDocID string, cozeDocID, creatorID int64) int64 {
	id, err := i.mapping.ChunkInsertOrGetCozeID(ctx, ragChunkID, ragDocID, cozeDocID, creatorID, i.idgen.GenID, time.Now().UnixMilli())
	if err != nil {
		logs.CtxWarnf(ctx, "ragimpl: ChunkInsertOrGetCozeID(rag_chunk_id=%s) failed; slice id will be 0: %v", ragChunkID, err)
		return 0
	}
	return id
}

// buildSliceFromChunk constructs an entity.Slice from a rag Chunk plus the
// already-resolved coze doc id. The caller is responsible for resolving the
// chunk's int64 id (via resolveCozeSliceID) so that test paths and concrete
// callers share the same backfill semantics.
func buildSliceFromChunk(c *contract.Chunk, cozeSliceID, cozeDocID, cozeKBID, creatorID int64) *entity.Slice {
	s := &entity.Slice{
		Info: knowledgeModel.Info{
			ID:        cozeSliceID,
			Name:      c.DocName,
			CreatorID: creatorID,
		},
		KnowledgeID:  cozeKBID,
		DocumentID:   cozeDocID,
		DocumentName: c.DocName,
		ByteCount:    int64(c.ByteCount),
		CharCount:    int64(c.CharCount),
	}
	if c.SequenceIndex != nil {
		s.Sequence = int64(*c.SequenceIndex)
	}
	switch c.ChunkType {
	case "image_chunk":
		var ocrText *string
		if c.Image != nil {
			if c.Image.OCRText != "" {
				v := c.Image.OCRText
				ocrText = &v
			}
			s.RawContent = []*knowledgeModel.SliceContent{
				{
					// The cross-domain model has no SliceContentTypeImage (commented out
					// in knowledge.go), so we use the default Text type and surface the
					// image fields via the Image pointer + an OCR-derived Text fallback.
					Type:  knowledgeModel.SliceContentTypeText,
					Image: &knowledgeModel.SliceImage{URI: c.Image.ImageRef, OCR: c.Image.OCRUsed, OCRText: ocrText},
					Text:  nil,
				},
			}
			if c.Image.Caption != "" {
				caption := c.Image.Caption
				s.RawContent[0].Text = &caption
			}
		}
	default:
		text := c.Content
		s.RawContent = []*knowledgeModel.SliceContent{
			{Type: knowledgeModel.SliceContentTypeText, Text: &text},
		}
	}
	return s
}

// resolveSliceKBAndDoc looks up the rag kb_id and rag doc_id from a coze
// slice mapping. The lookup chain (slice -> doc -> kb) is fixed by the
// mapping invariants. This helper exists to keep the per-method bodies
// short and self-evident in CreateSlice/UpdateSlice/etc.
func (i *Impl) resolveSliceKBAndDoc(ctx context.Context, cm *ChunkMapping) (kbMapping *KBMapping, docMapping *DocMapping, err error) {
	docMapping, err = i.mapping.DocByCozeID(ctx, cm.CozeDocID)
	if err != nil {
		return nil, nil, err
	}
	kbMapping, err = i.mapping.KBByCozeID(ctx, docMapping.KBID)
	if err != nil {
		return nil, nil, err
	}
	return kbMapping, docMapping, nil
}

// CreateSlice handles the rag-side chunk create, then writes the coze
// mapping. If the mapping insert fails after rag has accepted the chunk we
// surface the error but do NOT roll back the rag-side chunk -- the lazy
// backfill on the next read path will reattach a coze id, so the chunk is
// not lost (just unreachable via SliceID until then). Spec §4.1 (step 5):
// "rag chunk exists but coze has no handle yet; lazy backfill recovers on
// next read."
func (i *Impl) CreateSlice(ctx context.Context, req *service.CreateSliceRequest) (*service.CreateSliceResponse, error) {
	chunkType, content, image, err := rawContentToChunkPayload(req.RawContent)
	if err != nil {
		return nil, err
	}
	docMapping, err := i.mapping.DocByCozeID(ctx, req.DocumentID)
	if err != nil {
		return nil, err
	}
	kbMapping, err := i.mapping.KBByCozeID(ctx, docMapping.KBID)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}

	ragReq := &contract.CreateChunkRequest{
		ChunkType: chunkType,
		Content:   content,
		Image:     image,
	}
	// Frontend sends 0-based sequence_index where 0 means "insert at the top
	// (shift existing chunks down)". The earlier `> 0` guard treated 0 as
	// "unset" and dropped it, which broke the "insert above first chunk" path
	// -- the new chunk silently appended to the end instead. Forward
	// unconditionally; rag's pydantic validator (ge=0) covers negative values.
	seq := int(req.Position)
	if seq < 0 {
		seq = 0
	}
	ragReq.Position = &contract.ChunkPosition{SequenceIndex: &seq}
	// Creator tracking lives in coze-side rag_chunk_mapping.creator_id (Bug 1
	// fix). Rag's `source` is a reserved metadata key, and rag has its own
	// notion of authorship; sending either would 40001. Leave Metadata nil.

	ragChunk, err := i.rag.CreateChunk(ctx, tenant, kbMapping.RagKBID, docMapping.RagDocID, ragReq)
	if err != nil {
		return nil, err
	}
	cozeSliceID, err := i.idgen.GenID(ctx)
	if err != nil {
		logs.CtxWarnf(ctx, "ragimpl.CreateSlice: idgen.GenID failed after rag CreateChunk(rag_chunk_id=%s); chunk exists in rag but coze mapping not written: %v", ragChunk.ChunkID, err)
		return nil, err
	}
	if err := i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: cozeSliceID,
		RagChunkID:  ragChunk.ChunkID,
		RagDocID:    docMapping.RagDocID,
		CozeDocID:   req.DocumentID,
		CreatorID:   req.CreatorID,
	}, time.Now().UnixMilli()); err != nil {
		logs.CtxWarnf(ctx, "ragimpl.CreateSlice: ChunkInsert(rag_chunk_id=%s) failed after rag accepted chunk; lazy backfill on next read will recover: %v", ragChunk.ChunkID, err)
		return nil, err
	}
	return &service.CreateSliceResponse{SliceID: cozeSliceID}, nil
}

// UpdateSlice mutates content / metadata on an existing chunk. Image-ref
// changes are out of scope (delete-and-recreate is the documented path);
// table chunks are rejected via rawContentToChunkPayload.
func (i *Impl) UpdateSlice(ctx context.Context, req *service.UpdateSliceRequest) error {
	chunkType, content, image, err := rawContentToChunkPayload(req.RawContent)
	if err != nil {
		return err
	}
	cm, err := i.mapping.ChunkByCozeID(ctx, req.SliceID)
	if err != nil {
		return err
	}
	kbMapping, docMapping, err := i.resolveSliceKBAndDoc(ctx, cm)
	if err != nil {
		return err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return err
	}

	payload := &contract.UpdateChunkRequest{}
	switch chunkType {
	case "text_chunk":
		payload.Content = &content
	case "image_chunk":
		// Per rag's UpdateChunkRequest: image_ref is NOT supported as a
		// change here (rag's update endpoint accepts an `image` object but
		// drops image_ref edits server-side). Only metadata-ish image
		// fields (caption, ocr_text) are mutable. The translation layer
		// above already populates only image_ref / ocr_used / ocr_text
		// from the SliceImage; rag silently ignores image_ref when present
		// so we forward the whole object.
		payload.Image = image
	}

	if _, err := i.rag.UpdateChunk(ctx, tenant, kbMapping.RagKBID, docMapping.RagDocID, cm.RagChunkID, payload); err != nil {
		return err
	}
	return nil
}

// DeleteSlice removes the rag chunk first, then soft-deletes the coze
// mapping. Order matters: if we soft-deleted the mapping first and the rag
// call failed, the caller would see the mapping gone but the chunk still
// queryable -- worse than the inverse. The rag delete is the source-of-
// truth action.
func (i *Impl) DeleteSlice(ctx context.Context, req *service.DeleteSliceRequest) error {
	cm, err := i.mapping.ChunkByCozeID(ctx, req.SliceID)
	if err != nil {
		return err
	}
	kbMapping, docMapping, err := i.resolveSliceKBAndDoc(ctx, cm)
	if err != nil {
		return err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return err
	}
	if err := i.rag.DeleteChunk(ctx, tenant, kbMapping.RagKBID, docMapping.RagDocID, cm.RagChunkID); err != nil {
		return err
	}
	if err := i.mapping.ChunkSoftDelete(ctx, req.SliceID); err != nil {
		// rag has already deleted the chunk; the mapping cleanup failure is
		// non-fatal (the row is orphaned but lookups via ChunkByCozeID would
		// continue to "succeed" and point at a nonexistent rag chunk). Log
		// and surface, but acceptance criteria for the caller is the rag
		// delete -- which succeeded.
		logs.CtxWarnf(ctx, "ragimpl.DeleteSlice: mapping ChunkSoftDelete(%d) failed after rag delete: %v", req.SliceID, err)
		return err
	}
	return nil
}

// GetSlice fetches a single chunk by coze slice id. Lazy backfill is N/A
// here -- by definition the slice id came from a coze caller, so there's
// always a mapping. If the mapping is missing we return the wrapped
// ErrMappingNotFound (callers map this to ErrKnowledgeNotFound via the
// pkg/errorx envelope).
func (i *Impl) GetSlice(ctx context.Context, req *service.GetSliceRequest) (*service.GetSliceResponse, error) {
	cm, err := i.mapping.ChunkByCozeID(ctx, req.SliceID)
	if err != nil {
		return nil, err
	}
	kbMapping, docMapping, err := i.resolveSliceKBAndDoc(ctx, cm)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	ragChunk, err := i.rag.GetChunk(ctx, tenant, kbMapping.RagKBID, cm.RagChunkID)
	if err != nil {
		return nil, err
	}
	s := buildSliceFromChunk(ragChunk, cm.CozeSliceID, cm.CozeDocID, docMapping.KBID, cm.CreatorID)
	return &service.GetSliceResponse{Slice: s}, nil
}

// MGetSlice groups requested slice ids by their owning KB (chunks from
// different KBs cannot be batched into one rag mget call) and dispatches
// one MGetChunks per group. Missing mappings and per-KB failures are
// logged and skipped rather than failing the whole batch -- the spec
// (§7 row "MGetSlice 跨 KB") explicitly forbids partial-success only for
// the rag-side call; coze-side mapping drift is just drift.
func (i *Impl) MGetSlice(ctx context.Context, req *service.MGetSliceRequest) (*service.MGetSliceResponse, error) {
	if len(req.SliceIDs) == 0 {
		return &service.MGetSliceResponse{}, nil
	}
	cms, err := i.mapping.ChunksByCozeIDs(ctx, req.SliceIDs)
	if err != nil {
		return nil, err
	}
	if len(cms) == 0 {
		return &service.MGetSliceResponse{}, nil
	}

	// Build coze_doc_id -> doc_mapping cache so we don't issue a SELECT per
	// chunk. Multiple chunks usually share docs.
	docMappingByCozeDocID := map[int64]*DocMapping{}
	for _, cm := range cms {
		if _, ok := docMappingByCozeDocID[cm.CozeDocID]; ok {
			continue
		}
		dm, derr := i.mapping.DocByCozeID(ctx, cm.CozeDocID)
		if derr != nil {
			logs.CtxWarnf(ctx, "ragimpl.MGetSlice: DocByCozeID(%d) failed; chunks under this doc will be skipped: %v", cm.CozeDocID, derr)
			continue
		}
		docMappingByCozeDocID[cm.CozeDocID] = dm
	}
	// kb_id (coze) -> kb_mapping cache.
	kbMappingByCozeKBID := map[int64]*KBMapping{}
	// Group chunks by coze_kb_id.
	type grouping struct {
		kbMapping *KBMapping
		ragChunks []string
		// Map from rag chunk id -> resolved chunk mapping + doc mapping for
		// efficient response assembly.
		bySliceID map[string]*ChunkMapping
		docByRag  map[string]*DocMapping
	}
	groups := map[int64]*grouping{}
	for _, cm := range cms {
		dm := docMappingByCozeDocID[cm.CozeDocID]
		if dm == nil {
			continue
		}
		if _, ok := kbMappingByCozeKBID[dm.KBID]; !ok {
			kb, kerr := i.mapping.KBByCozeID(ctx, dm.KBID)
			if kerr != nil {
				logs.CtxWarnf(ctx, "ragimpl.MGetSlice: KBByCozeID(%d) failed; chunks under this KB will be skipped: %v", dm.KBID, kerr)
				continue
			}
			kbMappingByCozeKBID[dm.KBID] = kb
		}
		g, ok := groups[dm.KBID]
		if !ok {
			g = &grouping{
				kbMapping: kbMappingByCozeKBID[dm.KBID],
				bySliceID: map[string]*ChunkMapping{},
				docByRag:  map[string]*DocMapping{},
			}
			groups[dm.KBID] = g
		}
		g.ragChunks = append(g.ragChunks, cm.RagChunkID)
		g.bySliceID[cm.RagChunkID] = cm
		g.docByRag[cm.RagChunkID] = dm
	}

	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*entity.Slice, 0, len(cms))
	for cozeKBID, g := range groups {
		resp, mErr := i.rag.MGetChunks(ctx, tenant, g.kbMapping.RagKBID, g.ragChunks)
		if mErr != nil {
			// Per spec failure-modes table: "any failure then all failure".
			// We surface the first error rather than degrading silently.
			return nil, mErr
		}
		for idx := range resp.Items {
			item := resp.Items[idx]
			if item.Deleted {
				continue
			}
			cm := g.bySliceID[item.ChunkID]
			dm := g.docByRag[item.ChunkID]
			if cm == nil || dm == nil {
				// rag returned a chunk we did not ask for; defensive.
				continue
			}
			out = append(out, buildSliceFromChunk(&item.Chunk, cm.CozeSliceID, dm.CozeID, cozeKBID, cm.CreatorID))
		}
	}
	return &service.MGetSliceResponse{Slices: out}, nil
}

// ListSlice returns all chunks under a coze document, paginated. Lazy
// backfill is applied to every rag chunk in the response so the returned
// Slice.Info.ID is always non-zero (subject to mapping-insert success;
// failure logs WARN and leaves the id at zero).
//
// Phase 1: KnowledgeID-only (no DocumentID) is not supported -- the
// service-interface signature allows it but the legacy implementation
// pre-R2-G only ever listed by document. Pre-R2-G callers always set
// DocumentID. We pre-reject the KB-only case rather than silently doing
// the wrong thing.
func (i *Impl) ListSlice(ctx context.Context, req *service.ListSliceRequest) (*service.ListSliceResponse, error) {
	if req.DocumentID == nil {
		return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
			errorx.KV("msg", "ListSlice without DocumentID is not supported by the rag backend"))
	}
	docMapping, err := i.mapping.DocByCozeID(ctx, *req.DocumentID)
	if err != nil {
		return nil, err
	}
	kbMapping, err := i.mapping.KBByCozeID(ctx, docMapping.KBID)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	q := &contract.ListChunksQuery{
		Page:     1,
		PageSize: 50,
	}
	if req.Limit > 0 {
		q.PageSize = int(req.Limit)
	}
	if req.Offset > 0 && q.PageSize > 0 {
		q.Page = int(req.Offset/req.Limit) + 1
	}
	if req.Keyword != nil && *req.Keyword != "" {
		q.Keyword = *req.Keyword
	}

	resp, err := i.rag.ListChunks(ctx, tenant, kbMapping.RagKBID, docMapping.RagDocID, q)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Slice, 0, len(resp.Items))
	for idx := range resp.Items {
		c := &resp.Items[idx]
		cozeSliceID := i.resolveCozeSliceID(ctx, c.ChunkID, docMapping.RagDocID, *req.DocumentID, docMapping.CreatorID)
		out = append(out, buildSliceFromChunk(c, cozeSliceID, *req.DocumentID, docMapping.KBID, docMapping.CreatorID))
	}
	hasMore := resp.Total > q.Page*q.PageSize
	return &service.ListSliceResponse{
		Slices:  out,
		Total:   resp.Total,
		HasMore: hasMore,
	}, nil
}

// ListPhotoSlice covers the image-only chunk listing. Rag does not implement
// has_caption filtering (see brief §7 note), so coze post-filters on
// SliceImage.OCRText / caption presence. The post-filter is approximate when
// the rag page returns more than listPhotoSlicePageCap items: we log a WARN
// and filter only within that page (per spec §9 Q1 "option a + cap").
func (i *Impl) ListPhotoSlice(ctx context.Context, req *service.ListPhotoSliceRequest) (*service.ListPhotoSliceResponse, error) {
	kbMapping, err := i.mapping.KBByCozeID(ctx, req.KnowledgeID)
	if err != nil {
		return nil, err
	}
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	q := &contract.ListChunksByKBQuery{
		ChunkType: "image_chunk",
		Page:      1,
		PageSize:  listPhotoSlicePageCap,
	}
	if req.Limit != nil && *req.Limit > 0 {
		if *req.Limit > listPhotoSlicePageCap {
			q.PageSize = listPhotoSlicePageCap
		} else {
			q.PageSize = *req.Limit
		}
	}
	if req.Offset != nil && *req.Offset > 0 && q.PageSize > 0 {
		q.Page = (*req.Offset / q.PageSize) + 1
	}
	if len(req.DocumentIDs) > 0 {
		docs, err := i.mapping.DocsByCozeIDs(ctx, req.DocumentIDs)
		if err != nil {
			return nil, err
		}
		ragDocIDs := make([]string, 0, len(docs))
		for _, d := range docs {
			ragDocIDs = append(ragDocIDs, d.RagDocID)
		}
		q.DocIDs = ragDocIDs
	}
	resp, err := i.rag.ListChunksByKB(ctx, tenant, kbMapping.RagKBID, q)
	if err != nil {
		return nil, err
	}
	if req.HasCaption != nil && len(resp.Items) >= listPhotoSlicePageCap {
		logs.CtxWarnf(ctx, "ragimpl.ListPhotoSlice: HasCaption post-filter is approximate (filter applied only within first %d items; rag has not yet implemented has_caption)", listPhotoSlicePageCap)
	}

	// Build a coze_doc_id cache via reverse lookup on rag_doc_id -- we don't
	// have the int64 ids directly from rag.
	docByRagID := map[string]*DocMapping{}
	out := make([]*entity.Slice, 0, len(resp.Items))
	for idx := range resp.Items {
		c := &resp.Items[idx]
		dm := docByRagID[c.DocID]
		if dm == nil {
			rev, rerr := i.mapping.docByRagID(ctx, c.DocID)
			if rerr != nil {
				logs.CtxWarnf(ctx, "ragimpl.ListPhotoSlice: docByRagID(%s) failed; skipping chunk: %v", c.DocID, rerr)
				continue
			}
			docByRagID[c.DocID] = rev
			dm = rev
		}
		if req.HasCaption != nil {
			hasCaption := c.Image != nil && strings.TrimSpace(c.Image.Caption) != ""
			if *req.HasCaption != hasCaption {
				continue
			}
		}
		cozeSliceID := i.resolveCozeSliceID(ctx, c.ChunkID, c.DocID, dm.CozeID, dm.CreatorID)
		out = append(out, buildSliceFromChunk(c, cozeSliceID, dm.CozeID, dm.KBID, dm.CreatorID))
	}
	return &service.ListPhotoSliceResponse{
		Slices: out,
		Total:  resp.Total,
	}, nil
}
