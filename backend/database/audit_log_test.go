package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// auditTestDB 打开临时 SQLite，建 audit_log 表 + 触发器，赋值给全局 DB 并返回清理函数。
//
// 注：本测试不调 InitDatabase，因为 InitDatabase 会创建全部业务表 + seed。
// 我们只关心 audit_log 自身的哈希链 + 触发器行为。
func auditTestDB(t *testing.T) func() {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "audit_test.db")
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开临时 sqlite 失败: %v", err)
	}

	// 建表（与 database/db.go 中的 CREATE TABLE IF NOT EXISTS 同步）
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type  TEXT    NOT NULL,
			target_id    INTEGER NOT NULL,
			action       TEXT    NOT NULL,
			actor_id     INTEGER NOT NULL,
			actor_ip     TEXT    NOT NULL DEFAULT '',
			user_agent   TEXT    NOT NULL DEFAULT '',
			detail       TEXT    NOT NULL DEFAULT '{}',
			hash_sm3     TEXT    NOT NULL DEFAULT '',
			prev_hash    TEXT    NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
		)
	`); err != nil {
		t.Fatalf("建 audit_log 失败: %v", err)
	}

	// 触发器
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_update
		BEFORE UPDATE ON audit_log
		BEGIN
			SELECT RAISE(ABORT, 'audit_log is append-only');
		END
	`); err != nil {
		t.Fatalf("建 trg_audit_log_no_update 失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_delete
		BEFORE DELETE ON audit_log
		BEGIN
			SELECT RAISE(ABORT, 'audit_log is append-only');
		END
	`); err != nil {
		t.Fatalf("建 trg_audit_log_no_delete 失败: %v", err)
	}

	orig := DB
	DB = db
	return func() {
		_ = db.Close()
		DB = orig
	}
}

// contains 辅助函数：判断子串是否在 s 中。
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestAuditAppendAndChain 验证 AppendAudit 写入顺序 + 哈希链连续性。
// 覆盖 5 种 target_type 混合写入。
func TestAuditAppendAndChain(t *testing.T) {
	defer auditTestDB(t)()

	tests := []struct {
		targetType string
		targetID   int64
		action     string
		actorID    int64
		detail     string
	}{
		{AuditTargetCustomer, 1, "create", 100, `{"seq":1}`},
		{AuditTargetSignature, 2, "sign", 101, `{"seq":2}`},
		{AuditTargetReminder, 3, "create", 102, `{"seq":3}`},
		{AuditTargetCustomer, 2, "update", 100, `{"seq":4}`},
		{AuditTargetRBACRole, 5, "upsert", 102, `{"seq":5}`},
	}
	for i, tc := range tests {
		_, err := AppendAudit(
			tc.targetType, tc.targetID, tc.action, tc.actorID,
			"127.0.0.1", "ua-test", tc.detail,
		)
		if err != nil {
			t.Fatalf("AppendAudit 第 %d 条失败: %v", i+1, err)
		}
	}

	list, total, err := ListAudit("", 0, 0, "", 0, 0, 1, 20)
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if total != 5 {
		t.Errorf("total 期望=5, 实际=%d", total)
	}
	if len(list) != 5 {
		t.Fatalf("len(list) 期望=5, 实际=%d", len(list))
	}

	// 按 id DESC：第 0 条是 seq=5 (rbac_role upsert)
	if list[0].TargetType != AuditTargetRBACRole || list[0].Detail != `{"seq":5}` {
		t.Errorf("list[0] 期望 rbac_role+seq5, 实际=%s+%s", list[0].TargetType, list[0].Detail)
	}

	// 验链
	brokenAt, err := VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain 失败: %v", err)
	}
	if brokenAt != 0 {
		t.Errorf("链应完整, broken_at=%d", brokenAt)
	}
}

// TestAuditTriggerBlocksUpdate 验证 DB 触发器阻止 UPDATE。
func TestAuditTriggerBlocksUpdate(t *testing.T) {
	defer auditTestDB(t)()

	if _, err := AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", "{}"); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	_, err := DB.Exec(`UPDATE audit_log SET action = 'tampered' WHERE id = 1`)
	if err == nil {
		t.Fatal("UPDATE audit_log 应被触发器阻止, 实际无错")
	}
	if !contains(err.Error(), "append-only") {
		t.Errorf("错误信息应包含 'append-only', 实际=%q", err.Error())
	}
}

// TestAuditTriggerBlocksDelete 验证 DB 触发器阻止 DELETE。
func TestAuditTriggerBlocksDelete(t *testing.T) {
	defer auditTestDB(t)()

	if _, err := AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", "{}"); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	_, err := DB.Exec(`DELETE FROM audit_log WHERE id = 1`)
	if err == nil {
		t.Fatal("DELETE audit_log 应被触发器阻止, 实际无错")
	}
	if !contains(err.Error(), "append-only") {
		t.Errorf("错误信息应包含 'append-only', 实际=%q", err.Error())
	}
}

// TestAuditChainDetectsTampering 验证链式校验能检测手动篡改的 hash。
func TestAuditChainDetectsTampering(t *testing.T) {
	defer auditTestDB(t)()

	if _, err := AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", `{"a":1}`); err != nil {
		t.Fatalf("Append 1 失败: %v", err)
	}
	if _, err := AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", `{"a":2}`); err != nil {
		t.Fatalf("Append 2 失败: %v", err)
	}
	brokenAt, err := VerifyAuditChain()
	if err != nil {
		t.Fatalf("初始 VerifyChain 失败: %v", err)
	}
	if brokenAt != 0 {
		t.Errorf("初始链应完整, broken_at=%d", brokenAt)
	}

	// 临时 DROP 触发器 → 改 prev_hash → 重建触发器
	if _, err := DB.Exec(`DROP TRIGGER trg_audit_log_no_update`); err != nil {
		t.Fatalf("DROP TRIGGER 失败: %v", err)
	}
	if _, err := DB.Exec(`UPDATE audit_log SET prev_hash = 'TAMPERED' WHERE id = 1`); err != nil {
		t.Fatalf("绕过触发器后 UPDATE 仍应成功: %v", err)
	}
	if _, err := DB.Exec(`
		CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_update
		BEFORE UPDATE ON audit_log
		BEGIN
			SELECT RAISE(ABORT, 'audit_log is append-only');
		END
	`); err != nil {
		t.Fatalf("重建 TRIGGER 失败: %v", err)
	}

	// 验证应能检测出篡改
	brokenAt, err = VerifyAuditChain()
	if err == nil {
		t.Fatal("VerifyChain 应检测出 prev_hash 篡改, 实际无错")
	}
	if brokenAt != 1 {
		t.Errorf("broken_at 应为 1 (第一行的 prev_hash 是 GENESIS), 实际=%d", brokenAt)
	}
}

// TestAuditListByTarget 验证按 target_type + target_id 过滤。
func TestAuditListByTarget(t *testing.T) {
	defer auditTestDB(t)()

	_, _ = AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", "{}")
	_, _ = AppendAudit(AuditTargetCustomer, 1, "view", 100, "", "", "{}")
	_, _ = AppendAudit(AuditTargetSignature, 5, "sign", 101, "", "", "{}")
	_, _ = AppendAudit(AuditTargetCustomer, 7, "create", 102, "", "", "{}")

	list, total, err := ListAudit(AuditTargetCustomer, 1, 0, "", 0, 0, 1, 20)
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if total != 2 {
		t.Errorf("期望 2 条 customer/target=1, 实际=%d", total)
	}
	for _, r := range list {
		if r.TargetType != AuditTargetCustomer || r.TargetID != 1 {
			t.Errorf("过滤错误: 期望 customer/1, 实际=%s/%d", r.TargetType, r.TargetID)
		}
	}

	// 按 customer（不同 id）
	_, total, err = ListAudit(AuditTargetCustomer, 7, 0, "", 0, 0, 1, 20)
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if total != 1 {
		t.Errorf("期望 1 条 customer/target=7, 实际=%d", total)
	}
}

// TestAuditAppendRejectsInvalid 验证 AppendAudit 对非法入参拒绝。
func TestAuditAppendRejectsInvalid(t *testing.T) {
	defer auditTestDB(t)()

	// 空 target_type
	if _, err := AppendAudit("", 1, "view", 100, "", "", "{}"); err == nil {
		t.Error("空 target_type 应报错")
	}
	// target_id == 0
	if _, err := AppendAudit(AuditTargetCustomer, 0, "view", 100, "", "", "{}"); err == nil {
		t.Error("target_id=0 应报错")
	}
	// target_id < 0 一律拒绝（2026-09-04：consent_letter 下线后无例外）
	if _, err := AppendAudit(AuditTargetCustomer, -1, "view", 100, "", "", "{}"); err == nil {
		t.Error("target_id<0 应报错（无任何类型例外）")
	}
	// 空 action
	if _, err := AppendAudit(AuditTargetCustomer, 1, "", 100, "", "", "{}"); err == nil {
		t.Error("空 action 应报错")
	}
}

// TestAuditListPageSize 验证分页上限生效。
func TestAuditListPageSize(t *testing.T) {
	defer auditTestDB(t)()

	for i := int64(1); i <= 5; i++ {
		if _, err := AppendAudit(AuditTargetCustomer, i, "view", 100, "", "", "{}"); err != nil {
			t.Fatalf("Append %d 失败: %v", i, err)
		}
	}

	// page=1, page_size=2
	list, total, err := ListAudit("", 0, 0, "", 0, 0, 1, 2)
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if total != 5 || len(list) != 2 {
		t.Errorf("page=1/size=2 期望 total=5/len=2, 实际 total=%d/len=%d", total, len(list))
	}
	// 第二页
	list2, _, _ := ListAudit("", 0, 0, "", 0, 0, 2, 2)
	if len(list2) != 2 {
		t.Errorf("page=2/size=2 期望 len=2, 实际=%d", len(list2))
	}
	// 验证两页不重复
	ids := map[int64]bool{}
	for _, r := range list {
		ids[r.ID] = true
	}
	for _, r := range list2 {
		if ids[r.ID] {
			t.Errorf("分页重复 id=%d", r.ID)
		}
	}
}
