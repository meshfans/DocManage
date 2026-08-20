package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// wormTestDB 建临时 sqlite + worm_record 表，返回清理函数。
// 与 audit_log_test.go 模式一致：复用全局 DB 变量，避免 mock 化全栈 InitDatabase。
func wormTestDB(t *testing.T) func() {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "worm_test.db")
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

	orig := DB
	DB = db
	return func() {
		_ = db.Close()
		DB = orig
	}
}

func TestLockOnce_New(t *testing.T) {
	cleanup := wormTestDB(t)
	defer cleanup()

	rec, err := LockOnce("SN-001", "/tmp/a.pdf", "abc123", 7, "thirdparty.contract.upload")
	if err != nil {
		t.Fatalf("首次 LockOnce 应成功，实际: %v", err)
	}
	if rec == nil {
		t.Fatal("返回 nil record")
	}
	if rec.SnowID != "SN-001" || rec.FileHashSHA != "abc123" || rec.LockedBy != 7 {
		t.Fatalf("字段不匹配: %+v", rec)
	}
	if rec.LockedAt <= 0 {
		t.Fatalf("locked_at 未填充: %d", rec.LockedAt)
	}
}

func TestLockOnce_DuplicateSnowIDIsIdempotent(t *testing.T) {
	cleanup := wormTestDB(t)
	defer cleanup()

	rec1, err := LockOnce("SN-002", "/tmp/b.pdf", "h1", 1, "x")
	if err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	// 同 snowid 第二次 → 幂等，返回原记录
	rec2, err := LockOnce("SN-002", "/tmp/b.pdf", "h1", 1, "x")
	if err != nil {
		t.Fatalf("同 snowid 二次 LockOnce 应幂等，实际: %v", err)
	}
	if rec1.SnowID != rec2.SnowID {
		t.Fatalf("幂等返回应为同一 snowid: %s vs %s", rec1.SnowID, rec2.SnowID)
	}
}

func TestLockOnce_DuplicatePathRejected(t *testing.T) {
	cleanup := wormTestDB(t)
	defer cleanup()

	if _, err := LockOnce("SN-003", "/tmp/c.pdf", "h1", 1, "x"); err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	// 不同 snowid + 相同 file_path → ErrAlreadyLocked
	_, err := LockOnce("SN-004", "/tmp/c.pdf", "h2", 1, "x")
	if err != ErrAlreadyLocked {
		t.Fatalf("应返 ErrAlreadyLocked，实际: %v", err)
	}
}

func TestListWormLocked(t *testing.T) {
	cleanup := wormTestDB(t)
	defer cleanup()

	// 空表
	list, err := ListWormLocked(0)
	if err != nil {
		t.Fatalf("空表查询失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("空表应返 0 条，实际 %d", len(list))
	}

	// 1 条
	if _, err := LockOnce("SN-A", "/tmp/a.pdf", "ha", 1, "x"); err != nil {
		t.Fatalf("LockOnce A 失败: %v", err)
	}
	list, _ = ListWormLocked(0)
	if len(list) != 1 {
		t.Fatalf("1 条记录应返回 1，实际 %d", len(list))
	}

	// 多条
	if _, err := LockOnce("SN-B", "/tmp/b.pdf", "hb", 2, "y"); err != nil {
		t.Fatalf("LockOnce B 失败: %v", err)
	}
	if _, err := LockOnce("SN-C", "/tmp/c.pdf", "hc", 3, "z"); err != nil {
		t.Fatalf("LockOnce C 失败: %v", err)
	}
	list, _ = ListWormLocked(0)
	if len(list) != 3 {
		t.Fatalf("3 条记录应返回 3，实际 %d", len(list))
	}

	// limit=1
	list, _ = ListWormLocked(1)
	if len(list) != 1 {
		t.Fatalf("limit=1 应返 1 条，实际 %d", len(list))
	}
}

func TestGetWormByFilePath(t *testing.T) {
	cleanup := wormTestDB(t)
	defer cleanup()

	// 不存在
	rec, err := GetWormByFilePath("/tmp/nope.pdf")
	if err != nil {
		t.Fatalf("查询不存在路径应返 nil err，实际: %v", err)
	}
	if rec != nil {
		t.Fatalf("不存在应返 nil rec，实际: %+v", rec)
	}

	// 存在
	if _, err := LockOnce("SN-X", "/tmp/x.pdf", "hx", 9, "test"); err != nil {
		t.Fatalf("LockOnce 失败: %v", err)
	}
	rec, err = GetWormByFilePath("/tmp/x.pdf")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if rec == nil || rec.SnowID != "SN-X" || rec.LockedBy != 9 {
		t.Fatalf("查询字段不匹配: %+v", rec)
	}
}
