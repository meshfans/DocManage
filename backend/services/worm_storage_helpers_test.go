package services

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"doc/database"
)

// initWormDBForTest 用临时 sqlite 建 worm_record 表，覆盖全局 DB。
// 返回清理函数（关闭 DB + 恢复原 DB）。
//
// 注：services 包测试如需访问 worm_record 表，必须先调本函数，
// 因为完整 InitDatabase 会把全业务表都建一遍，对单元测试过重。
func initWormDBForTest(t *testing.T) func() {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "worm_services_test.db")
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开临时 sqlite 失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS worm_record (
			snowid           TEXT    PRIMARY KEY,
			file_path        TEXT    NOT NULL UNIQUE,
			file_hash_sha256 TEXT    NOT NULL,
			locked_at        INTEGER NOT NULL,
			locked_by        INTEGER NOT NULL,
			locked_reason    TEXT    NOT NULL DEFAULT ''
		)
	`); err != nil {
		t.Fatalf("建 worm_record 失败: %v", err)
	}

	orig := database.DB
	database.DB = db
	return func() {
		_ = db.Close()
		database.DB = orig
	}
}
