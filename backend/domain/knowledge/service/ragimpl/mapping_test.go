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
	CreatedAt  int64   `gorm:"column:created_at"`
	DeletedAt  *string `gorm:"column:deleted_at"`
}

func (docRow) TableName() string { return "rag_doc_mapping" }

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
	require.NoError(t, db.AutoMigrate(&kbRow{}, &docRow{}))
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
	require.NoError(t, m.InsertDoc(context.Background(), 500, "rag-doc-500", 100, 7, "task-99", 1700000000))
	got, err := m.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "rag-doc-500", got.RagDocID)
	require.Equal(t, "task-99", got.LastTaskID)
	require.NoError(t, m.SoftDeleteDoc(context.Background(), 500))
	_, err = m.DocByCozeID(context.Background(), 500)
	require.Error(t, err)
	require.NoError(t, m.RestoreDoc(context.Background(), 500))
	_, err = m.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
}
