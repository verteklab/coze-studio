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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// kbRow / docRow mirror the slim mapping tables. SQLite is permissive about
// column types, so we model `deleted_at` as a nullable string for simplicity
// (production schema is datetime(3)).
type kbRow struct {
	CozeKBID  int64   `gorm:"column:coze_kb_id;primaryKey"`
	RagKBID   string  `gorm:"column:rag_kb_id"`
	IconURI   string  `gorm:"column:icon_uri"`
	AppID     int64   `gorm:"column:app_id"`
	CreatorID int64   `gorm:"column:creator_id"`
	CreatedAt int64   `gorm:"column:created_at"`
	DeletedAt *string `gorm:"column:deleted_at"`
}

func (kbRow) TableName() string { return "rag_kb_mapping" }

type docRow struct {
	CozeDocID  int64   `gorm:"column:coze_doc_id;primaryKey"`
	RagDocID   string  `gorm:"column:rag_doc_id"`
	CozeKBID   int64   `gorm:"column:coze_kb_id"`
	CreatorID  int64   `gorm:"column:creator_id"`
	LastTaskID string  `gorm:"column:last_task_id"`
	Size       int64   `gorm:"column:size"`
	CreatedAt  int64   `gorm:"column:created_at"`
	DeletedAt  *string `gorm:"column:deleted_at"`
}

func (docRow) TableName() string { return "rag_doc_mapping" }

type chunkRow struct {
	CozeSliceID int64   `gorm:"column:coze_slice_id;primaryKey"`
	RagChunkID  string  `gorm:"column:rag_chunk_id"`
	RagDocID    string  `gorm:"column:rag_doc_id"`
	CozeDocID   int64   `gorm:"column:coze_doc_id"`
	CreatedAt   int64   `gorm:"column:created_at"`
	DeletedAt   *string `gorm:"column:deleted_at"`
}

func (chunkRow) TableName() string { return "rag_chunk_mapping" }

// sqliteDriverOnce ensures we register the custom NOW(n) function exactly once.
// Production uses MySQL where NOW(3) is built-in; SQLite has no such function,
// so we shim it here so the unchanged production SQL is exercised in tests.
var sqliteDriverRegistered int32

func registerSQLiteShim() string {
	driverName := "sqlite3_with_now"
	if !atomic.CompareAndSwapInt32(&sqliteDriverRegistered, 0, 1) {
		return driverName
	}
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// NOW(precision) -> current UTC timestamp string. SQLite ignores the
			// precision argument; this is good enough to flip `deleted_at` to a
			// non-NULL value for soft-delete tests.
			return conn.RegisterFunc("now", func(_ int64) string {
				return time.Now().UTC().Format("2006-01-02 15:04:05.000")
			}, true)
		},
	})
	return driverName
}

func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	driverName := registerSQLiteShim()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	raw, err := sql.Open(driverName, dsn)
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Dialector{Conn: raw}, &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kbRow{}, &docRow{}, &chunkRow{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestMapping_KBByCozeID(t *testing.T) {
	db := setupDB(t)
	db.Create(&kbRow{CozeKBID: 100, RagKBID: "uuid-a", IconURI: "icon", AppID: 0, CreatorID: 9})
	m := NewMappingRepo(db)
	got, err := m.KBByCozeID(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, "uuid-a", got.RagKBID)
	require.Equal(t, "icon", got.IconURI)
}

func TestMapping_KBByCozeID_NotFound(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	_, err := m.KBByCozeID(context.Background(), 999)
	require.Error(t, err)
}

func TestMapping_KBsByCozeIDs(t *testing.T) {
	db := setupDB(t)
	db.Create(&kbRow{CozeKBID: 1, RagKBID: "a"})
	db.Create(&kbRow{CozeKBID: 2, RagKBID: "b"})
	m := NewMappingRepo(db)
	res, err := m.KBsByCozeIDs(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, res, 2)
}

// Batch fetcher contract: zero matches is not an error -- the caller diffs
// the returned slice against the input ids to decide. Mirrors DocsByCozeIDs.
func TestMapping_KBsByCozeIDs_NoneFound(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	res, err := m.KBsByCozeIDs(context.Background(), []int64{999, 1000})
	require.NoError(t, err)
	require.Len(t, res, 0)
}

func TestMapping_KBsByCozeIDs_PartialFound(t *testing.T) {
	db := setupDB(t)
	db.Create(&kbRow{CozeKBID: 1, RagKBID: "a"})
	m := NewMappingRepo(db)
	res, err := m.KBsByCozeIDs(context.Background(), []int64{1, 999})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, int64(1), res[0].CozeID)
}

func TestMapping_KBByRagID(t *testing.T) {
	db := setupDB(t)
	db.Create(&kbRow{CozeKBID: 200, RagKBID: "uuid-z"})
	m := NewMappingRepo(db)
	got, err := m.kbByRagID(context.Background(), "uuid-z")
	require.NoError(t, err)
	require.Equal(t, int64(200), got.CozeID)
}

func TestMapping_DocByCozeID(t *testing.T) {
	db := setupDB(t)
	db.Create(&docRow{CozeDocID: 50, RagDocID: "doc-uuid", CozeKBID: 100, LastTaskID: "task-1"})
	m := NewMappingRepo(db)
	got, err := m.DocByCozeID(context.Background(), 50)
	require.NoError(t, err)
	require.Equal(t, "doc-uuid", got.RagDocID)
	require.Equal(t, "task-1", got.LastTaskID)
}

func TestMapping_DocByRagID(t *testing.T) {
	db := setupDB(t)
	db.Create(&docRow{CozeDocID: 60, RagDocID: "doc-z", CozeKBID: 100})
	m := NewMappingRepo(db)
	got, err := m.docByRagID(context.Background(), "doc-z")
	require.NoError(t, err)
	require.Equal(t, int64(60), got.CozeID)
}

func TestMapping_InsertKB(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.InsertKB(context.Background(), 300, "uuid-300", "icon-uri", 42, 7, 1234567890))
	got, err := m.KBByCozeID(context.Background(), 300)
	require.NoError(t, err)
	require.Equal(t, "uuid-300", got.RagKBID)
	require.Equal(t, "icon-uri", got.IconURI)
	require.Equal(t, int64(42), got.AppID)
}

func TestMapping_SoftDeleteAndRestore(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.InsertKB(context.Background(), 400, "uuid-400", "", 0, 0, 0))
	require.NoError(t, m.SoftDeleteKB(context.Background(), 400))
	_, err := m.KBByCozeID(context.Background(), 400)
	require.Error(t, err) // deleted -> not found
	require.NoError(t, m.RestoreKB(context.Background(), 400))
	_, err = m.KBByCozeID(context.Background(), 400)
	require.NoError(t, err) // restored -> found again
}

func TestMapping_DocsByCozeIDs(t *testing.T) {
	db := setupDB(t)
	db.Create(&docRow{CozeDocID: 10, RagDocID: "d-a", CozeKBID: 1})
	db.Create(&docRow{CozeDocID: 11, RagDocID: "d-b", CozeKBID: 1})
	m := NewMappingRepo(db)
	res, err := m.DocsByCozeIDs(context.Background(), []int64{10, 11})
	require.NoError(t, err)
	require.Len(t, res, 2)
}

func TestMapping_InsertDocAndSoftDelete(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.InsertDoc(context.Background(), 500, "rag-doc-500", 100, 7, "task-99", 1700000000, 1024))
	got, err := m.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "rag-doc-500", got.RagDocID)
	require.Equal(t, "task-99", got.LastTaskID)
	require.Equal(t, int64(1024), got.Size)
	require.NoError(t, m.SoftDeleteDoc(context.Background(), 500))
	_, err = m.DocByCozeID(context.Background(), 500)
	require.Error(t, err)
	require.NoError(t, m.RestoreDoc(context.Background(), 500))
	_, err = m.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
}

// Exists is a yes/no lookup -- "no such mapping" must be (false, nil),
// distinguishable from a DB error. This is what Task 4's per-record
// Dataset.Backend tagging needs (it asks: is this KB rag-backed?).
func TestRagKBMapping_Exists(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)

	// unmapped id -> (false, nil), not an error
	exists, err := m.Exists(context.Background(), 12345)
	require.NoError(t, err)
	require.False(t, exists)

	// mapped id -> (true, nil)
	require.NoError(t, m.InsertKB(context.Background(), 100, "uuid-100", "", 0, 0, 0))
	exists, err = m.Exists(context.Background(), 100)
	require.NoError(t, err)
	require.True(t, exists)

	// soft-deleted id -> (false, nil), matches deleted_at IS NULL filter
	require.NoError(t, m.SoftDeleteKB(context.Background(), 100))
	exists, err = m.Exists(context.Background(), 100)
	require.NoError(t, err)
	require.False(t, exists)
}

// ExistsBatch returns a set-shaped map for O(1) membership tests at the
// call site (Task 4 iterates N KBs in batchConvertKnowledgeEntity2Model).
func TestRagKBMapping_ExistsBatch(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)

	// empty slice -> empty map, no DB hit needed, no error
	got, err := m.ExistsBatch(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = m.ExistsBatch(context.Background(), []int64{})
	require.NoError(t, err)
	require.Empty(t, got)

	// mixed: two mapped + one unmapped + a soft-deleted one
	require.NoError(t, m.InsertKB(context.Background(), 1, "uuid-1", "", 0, 0, 0))
	require.NoError(t, m.InsertKB(context.Background(), 2, "uuid-2", "", 0, 0, 0))
	require.NoError(t, m.InsertKB(context.Background(), 3, "uuid-3", "", 0, 0, 0))
	require.NoError(t, m.SoftDeleteKB(context.Background(), 3))

	got, err = m.ExistsBatch(context.Background(), []int64{1, 2, 3, 999})
	require.NoError(t, err)
	require.Len(t, got, 2)
	_, ok1 := got[1]
	_, ok2 := got[2]
	_, ok3 := got[3]
	_, ok999 := got[999]
	require.True(t, ok1)
	require.True(t, ok2)
	require.False(t, ok3, "soft-deleted mappings must not appear")
	require.False(t, ok999, "unmapped ids must not appear")

	// repeated ids in the input should not double-count
	got, err = m.ExistsBatch(context.Background(), []int64{1, 1, 2, 2})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

// --- ChunkMapping tests ---------------------------------------------------

func TestChunkMapping_InsertAndByCozeID(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 1001, RagChunkID: "chunk-aaa", RagDocID: "doc-aaa", CozeDocID: 50,
	}, 1700000000))

	got, err := m.ChunkByCozeID(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, "chunk-aaa", got.RagChunkID)
	require.Equal(t, "doc-aaa", got.RagDocID)
	require.Equal(t, int64(50), got.CozeDocID)
}

func TestChunkMapping_ByCozeID_NotFound(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	_, err := m.ChunkByCozeID(context.Background(), 9999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
}

func TestChunkMapping_ByRagID_EarliestWins(t *testing.T) {
	// Concurrent backfill race: two rows with the same rag_chunk_id but
	// different coze_slice_ids and created_at. The earlier one must always win.
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 2001, RagChunkID: "chunk-race", RagDocID: "doc-r", CozeDocID: 50,
	}, 1700000000))
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 2002, RagChunkID: "chunk-race", RagDocID: "doc-r", CozeDocID: 50,
	}, 1700000050))

	got, err := m.ChunkByRagID(context.Background(), "chunk-race")
	require.NoError(t, err)
	require.Equal(t, int64(2001), got.CozeSliceID, "earliest-created mapping must win")
}

func TestChunkMapping_ByRagID_SkipsSoftDeleted(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 3001, RagChunkID: "chunk-d", RagDocID: "doc-d", CozeDocID: 50,
	}, 1700000000))
	require.NoError(t, m.ChunkSoftDelete(context.Background(), 3001))

	_, err := m.ChunkByRagID(context.Background(), "chunk-d")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
}

func TestChunkMapping_ChunksByCozeIDs(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 1, RagChunkID: "c-1", RagDocID: "d-1", CozeDocID: 50}, 1))
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 2, RagChunkID: "c-2", RagDocID: "d-1", CozeDocID: 50}, 2))
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 3, RagChunkID: "c-3", RagDocID: "d-1", CozeDocID: 50}, 3))

	got, err := m.ChunksByCozeIDs(context.Background(), []int64{1, 2, 999})
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Empty input -> nil, no DB hit, no error.
	got, err = m.ChunksByCozeIDs(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestChunkMapping_ChunksByCozeDocID(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 1, RagChunkID: "c-1", RagDocID: "d-A", CozeDocID: 50}, 1))
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 2, RagChunkID: "c-2", RagDocID: "d-A", CozeDocID: 50}, 2))
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{CozeSliceID: 3, RagChunkID: "c-3", RagDocID: "d-B", CozeDocID: 51}, 3))
	require.NoError(t, m.ChunkSoftDelete(context.Background(), 2))

	got, err := m.ChunksByCozeDocID(context.Background(), 50)
	require.NoError(t, err)
	require.Len(t, got, 1, "soft-deleted rows must not appear")
	require.Equal(t, int64(1), got[0].CozeSliceID)
}

func TestChunkMapping_SoftDelete(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 4001, RagChunkID: "c", RagDocID: "d", CozeDocID: 50,
	}, 0))
	require.NoError(t, m.ChunkSoftDelete(context.Background(), 4001))
	_, err := m.ChunkByCozeID(context.Background(), 4001)
	require.Error(t, err)
}

func TestChunkMapping_InsertOrGetCozeID_FreshInsert(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)
	var allocCount int
	alloc := func(_ context.Context) (int64, error) {
		allocCount++
		return int64(5000 + allocCount), nil
	}

	id, err := m.ChunkInsertOrGetCozeID(context.Background(), "chunk-new", "doc-new", 50, alloc, 1700000000)
	require.NoError(t, err)
	require.Equal(t, int64(5001), id)
	require.Equal(t, 1, allocCount)

	// Second call with the same rag_chunk_id must NOT allocate; returns the
	// existing coze id.
	id2, err := m.ChunkInsertOrGetCozeID(context.Background(), "chunk-new", "doc-new", 50, alloc, 1700000100)
	require.NoError(t, err)
	require.Equal(t, int64(5001), id2)
	require.Equal(t, 1, allocCount, "second call must not allocate")
}

// TestChunkMapping_InsertOrGetCozeID_ConcurrentRace simulates the race: two
// callers see an unmapped rag_chunk_id, both allocate, both insert. After
// the dust settles every reader must converge on the earliest row's id.
func TestChunkMapping_InsertOrGetCozeID_ConcurrentRace(t *testing.T) {
	db := setupDB(t)
	m := NewMappingRepo(db)

	// Hand-crank both inserts (the ChunkInsertOrGetCozeID re-resolve step is
	// what we want to exercise here).
	allocA := func(_ context.Context) (int64, error) { return 7001, nil }

	// Caller A: no existing row -> inserts (7001, t=1).
	idA, err := m.ChunkInsertOrGetCozeID(context.Background(), "chunk-race", "doc-r", 50, allocA, 1)
	require.NoError(t, err)
	require.Equal(t, int64(7001), idA)

	// Caller B races in and (because there's no UNIQUE) also inserts (7002, t=2).
	// We simulate "B's lookup missed because A's insert hadn't yet committed"
	// by manually inserting B's row -- the production path's existing-row
	// check would otherwise short-circuit.
	require.NoError(t, m.ChunkInsert(context.Background(), &ChunkMapping{
		CozeSliceID: 7002, RagChunkID: "chunk-race", RagDocID: "doc-r", CozeDocID: 50,
	}, 2))

	// Now any subsequent caller -- including B's eventual re-resolve --
	// converges on 7001 (earliest).
	idC, err := m.ChunkInsertOrGetCozeID(context.Background(), "chunk-race", "doc-r", 50, func(_ context.Context) (int64, error) {
		t.Fatal("must not allocate on third call -- mapping already present")
		return 0, nil
	}, 3)
	require.NoError(t, err)
	require.Equal(t, int64(7001), idC, "all callers must converge on earliest mapping id")
}
