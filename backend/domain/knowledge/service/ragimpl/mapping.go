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
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
)

var (
	ErrMappingNotFound = errors.New("rag mapping not found")
)

// KBMapping carries the int64 <-> UUID pair plus coze-only display/audit fields.
// Authoritative business data (name, description, status) lives in rag and is
// fetched live; do NOT add those fields here.
//
// FormatType is the one piece of "what kind of KB is this" state coze must
// remember locally: rag's KnowledgeBaseDetail returns the union of every
// modality its embedding-model bindings support (always includes image_source),
// so we can't reconstruct text/table/image from rag's response. Persist what
// the caller asked for at create time and trust this column on read.
type KBMapping struct {
	CozeID     int64
	RagKBID    string
	IconURI    string
	AppID      int64 // informational only -- never used for isolation
	CreatorID  int64 // informational only
	FormatType knowledgeModel.DocumentType
}

type DocMapping struct {
	CozeID     int64
	RagDocID   string
	KBID       int64
	CreatorID  int64
	LastTaskID string
	Size       int64  // file size in bytes; populated at upload, read on display
	ImageURL   string // coze-side MinIO URL for image-source documents; "" for non-image docs
}

// ChunkMapping bridges coze's int64 slice id and rag's string chunk UUID. No
// authoritative content lives here — caption / content / sequence_index are
// returned live from rag. The mapping table exists solely so retrieval hits
// can be re-keyed with a stable int64, matching the rest of coze's data model.
//
// Concurrency: rag_chunk_id is INDEXED but NOT UNIQUE (see
// docker/atlas/migrations/...rag_chunk_mapping.sql). On lazy backfill, two
// concurrent retrievals on the same unmapped chunk may insert two rows with
// the same rag_chunk_id. Read paths resolve this by selecting the earliest
// created_at; once a coze_slice_id is assigned it is stable. Strict
// dedup (if ever required) belongs to a follow-up R2-G2 dedup task.
type ChunkMapping struct {
	CozeSliceID int64
	RagChunkID  string
	RagDocID    string
	CozeDocID   int64
	CreatorID   int64
}

type MappingRepo struct {
	db *gorm.DB
}

func NewMappingRepo(db *gorm.DB) *MappingRepo {
	return &MappingRepo{db: db}
}

func (m *MappingRepo) KBByCozeID(ctx context.Context, cozeID int64) (*KBMapping, error) {
	var row struct {
		CozeKBID   int64 `gorm:"column:coze_kb_id"`
		RagKBID    string `gorm:"column:rag_kb_id"`
		IconURI    string `gorm:"column:icon_uri"`
		AppID      int64  `gorm:"column:app_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		FormatType int64  `gorm:"column:format_type"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, format_type").
		Where("coze_kb_id = ? AND (deleted_at IS NULL)", cozeID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: kb id=%d", ErrMappingNotFound, cozeID)
		}
		return nil, err
	}
	return &KBMapping{
		CozeID: row.CozeKBID, RagKBID: row.RagKBID, IconURI: row.IconURI,
		AppID: row.AppID, CreatorID: row.CreatorID,
		FormatType: knowledgeModel.DocumentType(row.FormatType),
	}, nil
}

// KBsByCozeIDs returns mappings for the requested ids. It does NOT enforce any
// tenant invariant -- tenant is resolved by TenantResolver at the call site and
// rag itself rejects cross-tenant access by filtering on tenant_id.
func (m *MappingRepo) KBsByCozeIDs(ctx context.Context, ids []int64) ([]*KBMapping, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		RagKBID    string `gorm:"column:rag_kb_id"`
		IconURI    string `gorm:"column:icon_uri"`
		AppID      int64  `gorm:"column:app_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		FormatType int64  `gorm:"column:format_type"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, format_type").
		Where("coze_kb_id IN ? AND (deleted_at IS NULL)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	// Batch fetcher semantics: return what was found, let the caller diff against
	// input. Mirrors DocsByCozeIDs. Single-id callers should use KBByCozeID.
	out := make([]*KBMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &KBMapping{
			CozeID: r.CozeKBID, RagKBID: r.RagKBID, IconURI: r.IconURI,
			AppID: r.AppID, CreatorID: r.CreatorID,
			FormatType: knowledgeModel.DocumentType(r.FormatType),
		})
	}
	return out, nil
}

// KBsByCreator returns active KB mappings created by the given user, paginated.
// total is the unpaginated count of matching rows. Used by ListKnowledge when
// the caller asks for ScopeSelf (filter.scope_type = ScopeSelf), which maps to
// ListKnowledgeRequest.UserID. Empty creator returns (nil, 0, nil) so the caller
// keeps the gate explicit and we don't accidentally degrade to a tenant-wide
// scan when the application layer forgot to set UserID.
func (m *MappingRepo) KBsByCreator(ctx context.Context, creatorID int64, page, pageSize int) ([]*KBMapping, int64, error) {
	if creatorID == 0 {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var total int64
	if err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Where("creator_id = ? AND (deleted_at IS NULL)", creatorID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	var rows []struct {
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		RagKBID    string `gorm:"column:rag_kb_id"`
		IconURI    string `gorm:"column:icon_uri"`
		AppID      int64  `gorm:"column:app_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		FormatType int64  `gorm:"column:format_type"`
	}
	if err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, format_type").
		Where("creator_id = ? AND (deleted_at IS NULL)", creatorID).
		Order("coze_kb_id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*KBMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &KBMapping{
			CozeID: r.CozeKBID, RagKBID: r.RagKBID, IconURI: r.IconURI,
			AppID: r.AppID, CreatorID: r.CreatorID,
			FormatType: knowledgeModel.DocumentType(r.FormatType),
		})
	}
	return out, total, nil
}

// Exists is a yes/no lookup: does an active mapping row exist for this coze KB id?
// "No such mapping" is (false, nil); only true DB failures return an error. Used by
// the application layer to tag Dataset.Backend on outgoing DTOs without paying the
// full hydration cost.
func (m *MappingRepo) Exists(ctx context.Context, cozeKBID int64) (bool, error) {
	var count int64
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Where("coze_kb_id = ? AND (deleted_at IS NULL)", cozeKBID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ExistsBatch returns the subset of input ids that have an active mapping, shaped
// as a set (map[int64]struct{}) for O(1) membership tests in the caller's per-record
// loop. Empty input is not a DB hit. Like KBsByCozeIDs, missing ids are simply
// absent from the returned map -- the caller diffs against its input.
func (m *MappingRepo) ExistsBatch(ctx context.Context, cozeKBIDs []int64) (map[int64]struct{}, error) {
	if len(cozeKBIDs) == 0 {
		return map[int64]struct{}{}, nil
	}
	var rows []struct {
		CozeKBID int64 `gorm:"column:coze_kb_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id").
		Where("coze_kb_id IN ? AND (deleted_at IS NULL)", cozeKBIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64]struct{}, len(rows))
	for _, r := range rows {
		out[r.CozeKBID] = struct{}{}
	}
	return out, nil
}

// kbByRagID is the reverse lookup -- given a rag UUID, find the coze mapping.
// Lowercase: internal to the package, used by KB List/MGet hydration paths.
func (m *MappingRepo) kbByRagID(ctx context.Context, ragKBID string) (*KBMapping, error) {
	var row struct {
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		RagKBID    string `gorm:"column:rag_kb_id"`
		IconURI    string `gorm:"column:icon_uri"`
		AppID      int64  `gorm:"column:app_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		FormatType int64  `gorm:"column:format_type"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, format_type").
		Where("rag_kb_id = ? AND (deleted_at IS NULL)", ragKBID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: rag_kb_id=%s", ErrMappingNotFound, ragKBID)
		}
		return nil, err
	}
	return &KBMapping{
		CozeID: row.CozeKBID, RagKBID: row.RagKBID, IconURI: row.IconURI,
		AppID: row.AppID, CreatorID: row.CreatorID,
		FormatType: knowledgeModel.DocumentType(row.FormatType),
	}, nil
}

func (m *MappingRepo) DocByCozeID(ctx context.Context, cozeID int64) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64          `gorm:"column:coze_doc_id"`
		RagDocID   string         `gorm:"column:rag_doc_id"`
		CozeKBID   int64          `gorm:"column:coze_kb_id"`
		CreatorID  int64          `gorm:"column:creator_id"`
		LastTaskID string         `gorm:"column:last_task_id"`
		Size       int64          `gorm:"column:size"`
		ImageURL   sql.NullString `gorm:"column:image_url"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, image_url").
		Where("coze_doc_id = ? AND (deleted_at IS NULL)", cozeID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: doc id=%d", ErrMappingNotFound, cozeID)
		}
		return nil, err
	}
	imageURL := ""
	if row.ImageURL.Valid {
		imageURL = row.ImageURL.String
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
		ImageURL: imageURL,
	}, nil
}

func (m *MappingRepo) DocsByCozeIDs(ctx context.Context, ids []int64) ([]*DocMapping, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeDocID  int64          `gorm:"column:coze_doc_id"`
		RagDocID   string         `gorm:"column:rag_doc_id"`
		CozeKBID   int64          `gorm:"column:coze_kb_id"`
		CreatorID  int64          `gorm:"column:creator_id"`
		LastTaskID string         `gorm:"column:last_task_id"`
		Size       int64          `gorm:"column:size"`
		ImageURL   sql.NullString `gorm:"column:image_url"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, image_url").
		Where("coze_doc_id IN ? AND (deleted_at IS NULL)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*DocMapping, 0, len(rows))
	for _, r := range rows {
		imageURL := ""
		if r.ImageURL.Valid {
			imageURL = r.ImageURL.String
		}
		out = append(out, &DocMapping{
			CozeID: r.CozeDocID, RagDocID: r.RagDocID, KBID: r.CozeKBID,
			CreatorID: r.CreatorID, LastTaskID: r.LastTaskID, Size: r.Size,
			ImageURL: imageURL,
		})
	}
	return out, nil
}

// DocsByRagIDs is the batch reverse lookup by rag_doc_id. It returns at most one
// mapping per requested rag_doc_id (the first row found — same dedup contract as
// docByRagID). Missing ids are simply absent from the returned slice; the caller
// diffs against its input. Empty input short-circuits with no DB hit.
func (m *MappingRepo) DocsByRagIDs(ctx context.Context, ragDocIDs []string) ([]*DocMapping, error) {
	if len(ragDocIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeDocID  int64          `gorm:"column:coze_doc_id"`
		RagDocID   string         `gorm:"column:rag_doc_id"`
		CozeKBID   int64          `gorm:"column:coze_kb_id"`
		CreatorID  int64          `gorm:"column:creator_id"`
		LastTaskID string         `gorm:"column:last_task_id"`
		Size       int64          `gorm:"column:size"`
		ImageURL   sql.NullString `gorm:"column:image_url"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, image_url").
		Where("rag_doc_id IN ? AND (deleted_at IS NULL)", ragDocIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	// Dedup: when multiple active rows share a rag_doc_id (shouldn't happen in
	// practice but mirrors the defensive contract of docByRagID), keep the first
	// occurrence in result order.
	seen := make(map[string]struct{}, len(rows))
	out := make([]*DocMapping, 0, len(rows))
	for _, r := range rows {
		if _, dup := seen[r.RagDocID]; dup {
			continue
		}
		seen[r.RagDocID] = struct{}{}
		imageURL := ""
		if r.ImageURL.Valid {
			imageURL = r.ImageURL.String
		}
		out = append(out, &DocMapping{
			CozeID: r.CozeDocID, RagDocID: r.RagDocID, KBID: r.CozeKBID,
			CreatorID: r.CreatorID, LastTaskID: r.LastTaskID, Size: r.Size,
			ImageURL: imageURL,
		})
	}
	return out, nil
}

// docByRagID is the reverse lookup, used by retrieval result translation.
func (m *MappingRepo) docByRagID(ctx context.Context, ragDocID string) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64          `gorm:"column:coze_doc_id"`
		RagDocID   string         `gorm:"column:rag_doc_id"`
		CozeKBID   int64          `gorm:"column:coze_kb_id"`
		CreatorID  int64          `gorm:"column:creator_id"`
		LastTaskID string         `gorm:"column:last_task_id"`
		Size       int64          `gorm:"column:size"`
		ImageURL   sql.NullString `gorm:"column:image_url"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, image_url").
		Where("rag_doc_id = ? AND (deleted_at IS NULL)", ragDocID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: rag_doc_id=%s", ErrMappingNotFound, ragDocID)
		}
		return nil, err
	}
	imageURL := ""
	if row.ImageURL.Valid {
		imageURL = row.ImageURL.String
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
		ImageURL: imageURL,
	}, nil
}

// Write helpers (used by Create flows). Timestamps are bigint unsigned milliseconds
// to match coze's project-wide convention. `deleted_at` is a datetime(3) -- soft delete
// is signaled by NOW(3); restore is signaled by NULL.
//
// Slim signatures: name / description / status / space_id are deliberately
// absent -- those live in rag, not in the mapping table. format_type is the
// exception: rag's KB metadata can't distinguish text/table/image (every KB
// reports the union of capabilities its embedding bindings support), so coze
// must remember what the caller asked for at create time.

func (m *MappingRepo) InsertKB(ctx context.Context, cozeID int64, ragKBID, iconURI string, appID, creatorID, nowMs int64, formatType knowledgeModel.DocumentType) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_kb_mapping
		 (coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, created_at, format_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cozeID, ragKBID, iconURI, appID, creatorID, nowMs, int64(formatType),
	).Error
}

func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, nowMs int64, size int64, imageURL string) error {
	var imageURLVal sql.NullString
	if imageURL != "" {
		imageURLVal = sql.NullString{String: imageURL, Valid: true}
	}
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_doc_mapping
		 (coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, created_at, size, image_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cozeID, ragDocID, kbID, creatorID, lastTaskID, nowMs, size, imageURLVal,
	).Error
}

func (m *MappingRepo) SoftDeleteKB(ctx context.Context, cozeID int64) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_kb_mapping SET deleted_at = NOW(3) WHERE coze_kb_id = ?`, cozeID,
	).Error
}

func (m *MappingRepo) RestoreKB(ctx context.Context, cozeID int64) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_kb_mapping SET deleted_at = NULL WHERE coze_kb_id = ?`, cozeID,
	).Error
}

func (m *MappingRepo) SoftDeleteDoc(ctx context.Context, cozeID int64) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_doc_mapping SET deleted_at = NOW(3) WHERE coze_doc_id = ?`, cozeID,
	).Error
}

func (m *MappingRepo) RestoreDoc(ctx context.Context, cozeID int64) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_doc_mapping SET deleted_at = NULL WHERE coze_doc_id = ?`, cozeID,
	).Error
}

// UpdateLastTaskID bumps rag_doc_mapping.last_task_id for a coze doc, called
// after RetryDocument so MGetDocumentProgress polls the new rag task. Soft-
// deleted rows are excluded; the caller has already verified the row via
// DocByCozeID upstream so a "no row matched" result is treated as a no-op
// rather than an error (mirrors SoftDeleteDoc / RestoreDoc).
func (m *MappingRepo) UpdateLastTaskID(ctx context.Context, cozeDocID int64, taskID string) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_doc_mapping SET last_task_id = ? WHERE coze_doc_id = ? AND deleted_at IS NULL`,
		taskID, cozeDocID,
	).Error
}

// Note: there is no UpdateDocStatus -- document status is rag's data, not coze's.
// Status is read live from rag via GetTask / GetDocument; nothing is mirrored.

// --- Chunk mapping helpers ------------------------------------------------
//
// Shape parallels DocMapping: lookup-by-coze-id, batch-by-coze-ids, list-by-doc,
// insert, soft-delete. Reverse lookup by rag id (ChunkByRagID) plus the
// concurrency-safe insert-or-get (ChunkInsertOrGetCozeID) are unique to chunks
// because the read paths must materialise mappings lazily for chunks that
// originated in rag (via document ingestion) and were never seen by coze before.

func (m *MappingRepo) ChunkByCozeID(ctx context.Context, cozeSliceID int64) (*ChunkMapping, error) {
	var row struct {
		CozeSliceID int64  `gorm:"column:coze_slice_id"`
		RagChunkID  string `gorm:"column:rag_chunk_id"`
		RagDocID    string `gorm:"column:rag_doc_id"`
		CozeDocID   int64  `gorm:"column:coze_doc_id"`
		CreatorID   int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_chunk_mapping").
		Select("coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id").
		Where("coze_slice_id = ? AND (deleted_at IS NULL)", cozeSliceID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: slice id=%d", ErrMappingNotFound, cozeSliceID)
		}
		return nil, err
	}
	return &ChunkMapping{
		CozeSliceID: row.CozeSliceID, RagChunkID: row.RagChunkID,
		RagDocID: row.RagDocID, CozeDocID: row.CozeDocID,
		CreatorID: row.CreatorID,
	}, nil
}

// ChunkByRagID resolves a rag chunk UUID to the earliest active coze mapping.
// "Earliest" is by created_at -- when concurrent lazy backfills race they may
// produce two rows with the same rag_chunk_id; the earliest one wins so the
// surfaced coze_slice_id is stable for the lifetime of the chunk.
func (m *MappingRepo) ChunkByRagID(ctx context.Context, ragChunkID string) (*ChunkMapping, error) {
	var row struct {
		CozeSliceID int64  `gorm:"column:coze_slice_id"`
		RagChunkID  string `gorm:"column:rag_chunk_id"`
		RagDocID    string `gorm:"column:rag_doc_id"`
		CozeDocID   int64  `gorm:"column:coze_doc_id"`
		CreatorID   int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_chunk_mapping").
		Select("coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id").
		Where("rag_chunk_id = ? AND (deleted_at IS NULL)", ragChunkID).
		Order("created_at ASC, coze_slice_id ASC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: rag_chunk_id=%s", ErrMappingNotFound, ragChunkID)
		}
		return nil, err
	}
	return &ChunkMapping{
		CozeSliceID: row.CozeSliceID, RagChunkID: row.RagChunkID,
		RagDocID: row.RagDocID, CozeDocID: row.CozeDocID,
		CreatorID: row.CreatorID,
	}, nil
}

func (m *MappingRepo) ChunksByCozeIDs(ctx context.Context, ids []int64) ([]*ChunkMapping, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeSliceID int64  `gorm:"column:coze_slice_id"`
		RagChunkID  string `gorm:"column:rag_chunk_id"`
		RagDocID    string `gorm:"column:rag_doc_id"`
		CozeDocID   int64  `gorm:"column:coze_doc_id"`
		CreatorID   int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_chunk_mapping").
		Select("coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id").
		Where("coze_slice_id IN ? AND (deleted_at IS NULL)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*ChunkMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ChunkMapping{
			CozeSliceID: r.CozeSliceID, RagChunkID: r.RagChunkID,
			RagDocID: r.RagDocID, CozeDocID: r.CozeDocID,
			CreatorID: r.CreatorID,
		})
	}
	return out, nil
}

// ChunksByCozeDocID returns active mappings for a coze doc. Useful for
// invalidation after a document delete (cleanup is a follow-up; for now we
// rely on rag-side cascade behavior and leave chunk rows in place -- they
// become orphaned but harmless because lookups always go through ChunkByRagID).
func (m *MappingRepo) ChunksByCozeDocID(ctx context.Context, cozeDocID int64) ([]*ChunkMapping, error) {
	var rows []struct {
		CozeSliceID int64  `gorm:"column:coze_slice_id"`
		RagChunkID  string `gorm:"column:rag_chunk_id"`
		RagDocID    string `gorm:"column:rag_doc_id"`
		CozeDocID   int64  `gorm:"column:coze_doc_id"`
		CreatorID   int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_chunk_mapping").
		Select("coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id").
		Where("coze_doc_id = ? AND (deleted_at IS NULL)", cozeDocID).
		Order("created_at ASC, coze_slice_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*ChunkMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ChunkMapping{
			CozeSliceID: r.CozeSliceID, RagChunkID: r.RagChunkID,
			RagDocID: r.RagDocID, CozeDocID: r.CozeDocID,
			CreatorID: r.CreatorID,
		})
	}
	return out, nil
}

func (m *MappingRepo) ChunkInsert(ctx context.Context, mp *ChunkMapping, nowMs int64) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_chunk_mapping
		 (coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		mp.CozeSliceID, mp.RagChunkID, mp.RagDocID, mp.CozeDocID, mp.CreatorID, nowMs,
	).Error
}

func (m *MappingRepo) ChunkSoftDelete(ctx context.Context, cozeSliceID int64) error {
	return m.db.WithContext(ctx).Exec(
		`UPDATE rag_chunk_mapping SET deleted_at = NOW(3) WHERE coze_slice_id = ?`, cozeSliceID,
	).Error
}

// ChunkInsertOrGetCozeID materialises a coze_slice_id for a rag chunk_id seen
// on a read path. The flow is:
//
//  1. Try an existing-row lookup first (ChunkByRagID). Most read traffic hits
//     this branch -- chunks are seen many times once mapped.
//  2. If not found, allocate a coze_slice_id from the supplied generator and
//     insert. Concurrent inserts may both succeed (no UNIQUE on rag_chunk_id);
//     after insert we re-lookup so the earliest-created row's id is the one
//     returned to every caller. This keeps the returned id stable in the face
//     of races without requiring a UNIQUE constraint that would serialise hot
//     retrieval paths.
//
// The generator callback exists so the repo doesn't depend on idgen directly
// -- keeps mapping_test.go usable without wiring an idgen stub.
func (m *MappingRepo) ChunkInsertOrGetCozeID(
	ctx context.Context,
	ragChunkID, ragDocID string,
	cozeDocID, creatorID int64,
	allocID func(context.Context) (int64, error),
	nowMs int64,
) (int64, error) {
	if existing, err := m.ChunkByRagID(ctx, ragChunkID); err == nil {
		return existing.CozeSliceID, nil
	} else if !errors.Is(err, ErrMappingNotFound) {
		return 0, err
	}
	cozeSliceID, err := allocID(ctx)
	if err != nil {
		return 0, err
	}
	if err := m.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: cozeSliceID, RagChunkID: ragChunkID,
		RagDocID: ragDocID, CozeDocID: cozeDocID,
		CreatorID: creatorID,
	}, nowMs); err != nil {
		return 0, err
	}
	// Re-resolve: under race, our row may not be the earliest. Reading earliest
	// here means every concurrent caller converges on the same id.
	resolved, err := m.ChunkByRagID(ctx, ragChunkID)
	if err != nil {
		return 0, err
	}
	return resolved.CozeSliceID, nil
}

// ChunksByRagIDs is the batch variant of ChunkByRagID. It returns at most one
// mapping per requested rag_chunk_id -- when multiple rows exist for the same
// id (from a lazy-backfill race) the earliest-created wins, matching
// ChunkByRagID's single-row semantics. Missing chunks are simply absent from
// the result; the caller distinguishes "no mapping" by the returned map's
// keyset, not by an error.
//
// Empty input short-circuits with no database hit.
func (m *MappingRepo) ChunksByRagIDs(ctx context.Context, ragChunkIDs []string) ([]*ChunkMapping, error) {
	if len(ragChunkIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeSliceID int64  `gorm:"column:coze_slice_id"`
		RagChunkID  string `gorm:"column:rag_chunk_id"`
		RagDocID    string `gorm:"column:rag_doc_id"`
		CozeDocID   int64  `gorm:"column:coze_doc_id"`
		CreatorID   int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_chunk_mapping").
		Select("coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id").
		Where("rag_chunk_id IN ? AND (deleted_at IS NULL)", ragChunkIDs).
		Order("rag_chunk_id ASC, created_at ASC, coze_slice_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	// Dedup: keep only the first occurrence of each rag_chunk_id (the
	// earliest-created thanks to the ORDER BY). This mirrors ChunkByRagID's
	// race-stability contract -- every reader converges on the same row.
	seen := make(map[string]struct{}, len(rows))
	out := make([]*ChunkMapping, 0, len(rows))
	for _, r := range rows {
		if _, dup := seen[r.RagChunkID]; dup {
			continue
		}
		seen[r.RagChunkID] = struct{}{}
		out = append(out, &ChunkMapping{
			CozeSliceID: r.CozeSliceID, RagChunkID: r.RagChunkID,
			RagDocID: r.RagDocID, CozeDocID: r.CozeDocID,
			CreatorID: r.CreatorID,
		})
	}
	return out, nil
}

// ChunksBulkInsert writes a batch of chunk mappings in a single multi-row
// INSERT. Empty input is a no-op. Mirrors ChunkInsert's column set; the
// non-UNIQUE rag_chunk_id invariant still applies -- concurrent batches may
// produce duplicate rows that the read path resolves via earliest-created.
func (m *MappingRepo) ChunksBulkInsert(ctx context.Context, mps []*ChunkMapping, nowMs int64) error {
	if len(mps) == 0 {
		return nil
	}
	var (
		placeholders = make([]string, 0, len(mps))
		args         = make([]any, 0, len(mps)*6)
	)
	for _, mp := range mps {
		placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?)")
		args = append(args, mp.CozeSliceID, mp.RagChunkID, mp.RagDocID, mp.CozeDocID, mp.CreatorID, nowMs)
	}
	stmt := `INSERT INTO rag_chunk_mapping
		 (coze_slice_id, rag_chunk_id, rag_doc_id, coze_doc_id, creator_id, created_at)
		 VALUES ` + strings.Join(placeholders, ", ")
	return m.db.WithContext(ctx).Exec(stmt, args...).Error
}

// ChunkInsertOrGetItem is the per-chunk descriptor for ChunksInsertOrGetCozeIDs.
// It exists because ListPhotoSlice's chunks span multiple docs (each with a
// different RagDocID / CozeDocID / CreatorID), so a single per-call doc tuple
// is not enough. ListSlice's call site fills every item with the same trio.
type ChunkInsertOrGetItem struct {
	RagChunkID string
	RagDocID   string
	CozeDocID  int64
	CreatorID  int64
}

// ChunksInsertOrGetCozeIDs is the batch variant of ChunkInsertOrGetCozeID.
// The flow collapses the per-chunk SELECT + GenID + INSERT + re-SELECT loop
// into at most four database / idgen round-trips for the entire batch:
//
//  1. One SELECT (ChunksByRagIDs) covers all existing mappings.
//  2. One GenMultiIDs call allocates ids for the missing N.
//  3. One multi-row INSERT (ChunksBulkInsert) materialises the new mappings.
//  4. One re-SELECT (ChunksByRagIDs again) covers race convergence -- only
//     the freshly-inserted rag_chunk_ids are re-resolved so every caller still
//     ends up on the earliest-created row when two batches race.
//
// Duplicate rag_chunk_ids in `items` collapse to a single map entry (no
// duplicate id allocation). Empty input returns an empty (non-nil) map.
//
// The allocMulti callback exists so the repo does not depend on the idgen
// package directly -- keeps the test stub simple and the unit boundary clean.
func (m *MappingRepo) ChunksInsertOrGetCozeIDs(
	ctx context.Context,
	items []ChunkInsertOrGetItem,
	allocMulti func(ctx context.Context, n int) ([]int64, error),
	nowMs int64,
) (map[string]int64, error) {
	out := make(map[string]int64, len(items))
	if len(items) == 0 {
		return out, nil
	}

	// Dedup the inputs by rag_chunk_id. Two chunks with the same id share an
	// allocation; we remember the first descriptor we saw for the insert path.
	uniq := make(map[string]ChunkInsertOrGetItem, len(items))
	order := make([]string, 0, len(items))
	for _, it := range items {
		if it.RagChunkID == "" {
			continue
		}
		if _, ok := uniq[it.RagChunkID]; ok {
			continue
		}
		uniq[it.RagChunkID] = it
		order = append(order, it.RagChunkID)
	}
	if len(order) == 0 {
		return out, nil
	}

	existing, err := m.ChunksByRagIDs(ctx, order)
	if err != nil {
		return nil, err
	}
	for _, cm := range existing {
		out[cm.RagChunkID] = cm.CozeSliceID
	}

	missing := make([]string, 0, len(order)-len(out))
	for _, id := range order {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}

	ids, err := allocMulti(ctx, len(missing))
	if err != nil {
		return nil, err
	}
	if len(ids) != len(missing) {
		return nil, fmt.Errorf("idgen returned %d ids, expected %d", len(ids), len(missing))
	}

	newRows := make([]*ChunkMapping, 0, len(missing))
	for i, id := range missing {
		it := uniq[id]
		newRows = append(newRows, &ChunkMapping{
			CozeSliceID: ids[i],
			RagChunkID:  it.RagChunkID,
			RagDocID:    it.RagDocID,
			CozeDocID:   it.CozeDocID,
			CreatorID:   it.CreatorID,
		})
	}
	if err := m.ChunksBulkInsert(ctx, newRows, nowMs); err != nil {
		return nil, err
	}

	// Re-resolve only the newly inserted ids so racing batches converge on the
	// earliest-created row (same contract as ChunkInsertOrGetCozeID).
	reresolved, err := m.ChunksByRagIDs(ctx, missing)
	if err != nil {
		return nil, err
	}
	for _, cm := range reresolved {
		out[cm.RagChunkID] = cm.CozeSliceID
	}
	return out, nil
}
