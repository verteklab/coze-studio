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
	"fmt"

	"gorm.io/gorm"
)

var (
	ErrMappingNotFound = errors.New("rag mapping not found")
)

// KBMapping carries the int64 <-> UUID pair plus coze-only display/audit fields.
// Authoritative business data (name, description, status) lives in rag and is
// fetched live; do NOT add those fields here.
type KBMapping struct {
	CozeID    int64
	RagKBID   string
	IconURI   string
	AppID     int64 // informational only -- never used for isolation
	CreatorID int64 // informational only
}

type DocMapping struct {
	CozeID     int64
	RagDocID   string
	KBID       int64
	CreatorID  int64
	LastTaskID string
	Size       int64 // file size in bytes; populated at upload, read on display
}

type MappingRepo struct {
	db *gorm.DB
}

func NewMappingRepo(db *gorm.DB) *MappingRepo {
	return &MappingRepo{db: db}
}

func (m *MappingRepo) KBByCozeID(ctx context.Context, cozeID int64) (*KBMapping, error) {
	var row struct {
		CozeKBID  int64  `gorm:"column:coze_kb_id"`
		RagKBID   string `gorm:"column:rag_kb_id"`
		IconURI   string `gorm:"column:icon_uri"`
		AppID     int64  `gorm:"column:app_id"`
		CreatorID int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id").
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
		CozeKBID  int64  `gorm:"column:coze_kb_id"`
		RagKBID   string `gorm:"column:rag_kb_id"`
		IconURI   string `gorm:"column:icon_uri"`
		AppID     int64  `gorm:"column:app_id"`
		CreatorID int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id").
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
		})
	}
	return out, nil
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
		CozeKBID  int64  `gorm:"column:coze_kb_id"`
		RagKBID   string `gorm:"column:rag_kb_id"`
		IconURI   string `gorm:"column:icon_uri"`
		AppID     int64  `gorm:"column:app_id"`
		CreatorID int64  `gorm:"column:creator_id"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_kb_mapping").
		Select("coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id").
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
	}, nil
}

func (m *MappingRepo) DocByCozeID(ctx context.Context, cozeID int64) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("coze_doc_id = ? AND (deleted_at IS NULL)", cozeID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: doc id=%d", ErrMappingNotFound, cozeID)
		}
		return nil, err
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
	}, nil
}

func (m *MappingRepo) DocsByCozeIDs(ctx context.Context, ids []int64) ([]*DocMapping, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("coze_doc_id IN ? AND (deleted_at IS NULL)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*DocMapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, &DocMapping{
			CozeID: r.CozeDocID, RagDocID: r.RagDocID, KBID: r.CozeKBID,
			CreatorID: r.CreatorID, LastTaskID: r.LastTaskID, Size: r.Size,
		})
	}
	return out, nil
}

// docByRagID is the reverse lookup, used by retrieval result translation.
func (m *MappingRepo) docByRagID(ctx context.Context, ragDocID string) (*DocMapping, error) {
	var row struct {
		CozeDocID  int64  `gorm:"column:coze_doc_id"`
		RagDocID   string `gorm:"column:rag_doc_id"`
		CozeKBID   int64  `gorm:"column:coze_kb_id"`
		CreatorID  int64  `gorm:"column:creator_id"`
		LastTaskID string `gorm:"column:last_task_id"`
		Size       int64  `gorm:"column:size"`
	}
	err := m.db.WithContext(ctx).
		Table("rag_doc_mapping").
		Select("coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size").
		Where("rag_doc_id = ? AND (deleted_at IS NULL)", ragDocID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: rag_doc_id=%s", ErrMappingNotFound, ragDocID)
		}
		return nil, err
	}
	return &DocMapping{
		CozeID: row.CozeDocID, RagDocID: row.RagDocID, KBID: row.CozeKBID,
		CreatorID: row.CreatorID, LastTaskID: row.LastTaskID, Size: row.Size,
	}, nil
}

// Write helpers (used by Create flows). Timestamps are bigint unsigned milliseconds
// to match coze's project-wide convention. `deleted_at` is a datetime(3) -- soft delete
// is signaled by NOW(3); restore is signaled by NULL.
//
// Note the slim signatures: name / description / status / format_type / space_id
// are deliberately absent -- those live in rag, not in the mapping table.

func (m *MappingRepo) InsertKB(ctx context.Context, cozeID int64, ragKBID, iconURI string, appID, creatorID, nowMs int64) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_kb_mapping
		 (coze_kb_id, rag_kb_id, icon_uri, app_id, creator_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cozeID, ragKBID, iconURI, appID, creatorID, nowMs,
	).Error
}

func (m *MappingRepo) InsertDoc(ctx context.Context, cozeID int64, ragDocID string, kbID, creatorID int64, lastTaskID string, size int64, nowMs int64) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO rag_doc_mapping
		 (coze_doc_id, rag_doc_id, coze_kb_id, creator_id, last_task_id, size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cozeID, ragDocID, kbID, creatorID, lastTaskID, size, nowMs,
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

// Note: there is no UpdateDocStatus -- document status is rag's data, not coze's.
// Status is read live from rag via GetTask / GetDocument; nothing is mirrored.
