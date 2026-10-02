package database

// N-2 单测：覆盖 MigrateAIConfigs 的三条核心路径。
//
// 2026-10-02 M-4 修复引入：
//   - 历史 meshfans 行（api_base='https://aiv1.meshfans.com/v1'）→ 应被刷新到 'https://lmp.meshfans.com/api/v1'
//   - 非 meshfans provider 的同 base → 不应被误改（防御 provider 误匹配）
//   - 已是新 base 的 meshfans 行 → 不应被改（idempotent）
//   - 已被 deleted_at != NULL 的行 → 不应被改
//
// 模式与 audit_log_test.go 一致：t.TempDir() 临时 SQLite + 全局 DB 注入。

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// aiConfigTestDB 打开临时 SQLite，建 ai_config 表，赋值给全局 DB 并返回清理函数。
func aiConfigTestDB(t *testing.T) func() {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "ai_config_test.db")
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开临时 sqlite 失败: %v", err)
	}

	// ai_config schema（与 database/db.go:747 同步，仅必要字段）
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ai_config (
		    id            INTEGER PRIMARY KEY AUTOINCREMENT,
		    name          TEXT    DEFAULT '',
		    provider      TEXT    NOT NULL,
		    protocol      TEXT    NOT NULL,
		    model_name    TEXT    NOT NULL,
		    api_key       TEXT    NOT NULL DEFAULT '',
		    api_base      TEXT    NOT NULL,
		    proxy_url     TEXT    DEFAULT '',
		    default_params TEXT    DEFAULT '',
		    extra         TEXT    DEFAULT '',
		    is_default    INTEGER NOT NULL DEFAULT 0,
		    status        INTEGER NOT NULL DEFAULT 1,
		    multimodal_supported     INTEGER,
		    multimodal_checked_at   INTEGER,
		    multimodal_check_source TEXT,
		    test_result   TEXT,
		    test_result_at INTEGER,
		    created_at    INTEGER NOT NULL DEFAULT 0,
		    updated_at    INTEGER NOT NULL DEFAULT 0,
		    deleted_at    INTEGER
		)
	`); err != nil {
		t.Fatalf("建 ai_config 表失败: %v", err)
	}

	orig := DB
	DB = db
	return func() {
		_ = db.Close()
		DB = orig
	}
}

// insertAIConfig 注入一行测试数据，返回 rowid。
func insertAIConfig(t *testing.T, provider, apiBase string, deleted bool) int64 {
	t.Helper()
	var deletedAt interface{}
	if deleted {
		deletedAt = 1700000000 // 任意 unix
	}
	res, err := DB.Exec(`
		INSERT INTO ai_config (name, provider, protocol, model_name, api_key, api_base,
		                      is_default, status, created_at, updated_at, deleted_at)
		VALUES (?, ?, 'openai_chat', 'meshfans-v1', '', ?, 0, 1, 1700000000, 1700000000, ?)
	`, "test-"+provider, provider, apiBase, deletedAt)
	if err != nil {
		t.Fatalf("insert ai_config(%s, %s) 失败: %v", provider, apiBase, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// queryBase 取行的 api_base（用 deleted_at IS NULL 过滤，与 migration SQL 一致）。
func queryBase(t *testing.T, id int64) string {
	t.Helper()
	var base string
	if err := DB.QueryRow(`SELECT api_base FROM ai_config WHERE id = ? AND deleted_at IS NULL`, id).Scan(&base); err != nil {
		t.Fatalf("query ai_config(id=%d) 失败: %v", id, err)
	}
	return base
}

// TestMigrateAIConfigs_UpdatesOldMeshfansBase 验证：历史 meshfans 行的旧 base
// 被刷新到新 base。
func TestMigrateAIConfigs_UpdatesOldMeshfansBase(t *testing.T) {
	cleanup := aiConfigTestDB(t)
	defer cleanup()

	id := insertAIConfig(t, "meshfans", "https://aiv1.meshfans.com/v1", false)
	if got := queryBase(t, id); got != "https://aiv1.meshfans.com/v1" {
		t.Fatalf("插入前 base = %q, 期望 https://aiv1.meshfans.com/v1", got)
	}

	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("MigrateAIConfigs 失败: %v", err)
	}

	want := "https://lmp.meshfans.com/api/v1"
	if got := queryBase(t, id); got != want {
		t.Errorf("迁移后 base = %q, 期望 %q", got, want)
	}
}

// TestMigrateAIConfigs_Idempotent 验证：已是新 base 的 meshfans 行不被改。
func TestMigrateAIConfigs_Idempotent(t *testing.T) {
	cleanup := aiConfigTestDB(t)
	defer cleanup()

	const newBase = "https://lmp.meshfans.com/api/v1"
	id := insertAIConfig(t, "meshfans", newBase, false)

	// 记录 updated_at，跑两次 migration 都应保持原值。
	var beforeUpdatedAt int64
	if err := DB.QueryRow(`SELECT updated_at FROM ai_config WHERE id = ?`, id).Scan(&beforeUpdatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}

	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("首次 MigrateAIConfigs 失败: %v", err)
	}
	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("二次 MigrateAIConfigs 失败: %v", err)
	}

	if got := queryBase(t, id); got != newBase {
		t.Errorf("二次迁移后 base = %q, 期望 %q", got, newBase)
	}
	var afterUpdatedAt int64
	if err := DB.QueryRow(`SELECT updated_at FROM ai_config WHERE id = ?`, id).Scan(&afterUpdatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	if afterUpdatedAt != beforeUpdatedAt {
		t.Errorf("updated_at 被改动: %d → %d（idempotent 应保持不变）",
			beforeUpdatedAt, afterUpdatedAt)
	}
}

// TestMigrateAIConfigs_DoesNotTouchOtherProviders 验证：非 meshfans provider
// 即使 api_base 恰好等于旧 meshfans base，也不被误改。
//
// 防御场景：用户可能把硅基流动等 provider 的 api_base 设为 'https://aiv1.meshfans.com/v1'
// （私有部署等），migration 不应越界到其他 provider。
func TestMigrateAIConfigs_DoesNotTouchOtherProviders(t *testing.T) {
	cleanup := aiConfigTestDB(t)
	defer cleanup()

	const oldBase = "https://aiv1.meshfans.com/v1"
	id := insertAIConfig(t, "siliconflow", oldBase, false)

	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("MigrateAIConfigs 失败: %v", err)
	}

	if got := queryBase(t, id); got != oldBase {
		t.Errorf("siliconflow provider 被误改 base = %q, 期望 %q（防御 provider 越界）",
			got, oldBase)
	}
}

// TestMigrateAIConfigs_SkipsSoftDeleted 验证：deleted_at IS NOT NULL 的行不被改。
func TestMigrateAIConfigs_SkipsSoftDeleted(t *testing.T) {
	cleanup := aiConfigTestDB(t)
	defer cleanup()

	const oldBase = "https://aiv1.meshfans.com/v1"
	id := insertAIConfig(t, "meshfans", oldBase, true) // deleted=true

	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("MigrateAIConfigs 失败: %v", err)
	}

	// 直接 query（不筛 deleted_at）看是否被改
	var got string
	if err := DB.QueryRow(`SELECT api_base FROM ai_config WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != oldBase {
		t.Errorf("soft-deleted 行被改 base = %q, 期望 %q（不应碰软删行）",
			got, oldBase)
	}
}

// TestMigrateAIConfigs_EmptyTable 验证：空表迁移不报错、影响行数为 0。
func TestMigrateAIConfigs_EmptyTable(t *testing.T) {
	cleanup := aiConfigTestDB(t)
	defer cleanup()

	if err := MigrateAIConfigs(); err != nil {
		t.Fatalf("空表 MigrateAIConfigs 应不报错: %v", err)
	}
	// 若无错误就算通过；这里不重复断言影响行数（MigrateAIConfigs 内部已用 utils.Info 输出）
}
