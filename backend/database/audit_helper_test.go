package database

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ==================== audit_helper 单测 ====================
//
// 范围（DB-independent）：
//   - RecordAudit：actor_user_id 自动从 c.Get("user_id") 取，nil/未设 → 0
//   - RecordAuditBy：actor_user_id 显式传入
//   - RecordAuditStandalone：无 gin.Context
//   - meta nil/有值/序列化失败的兜底
//
// 不覆盖：AppendAudit 哈希链本身（audit_log_test.go 已覆盖）
// 不覆盖：业务 handler 集成路径（E2E 覆盖）
// ----------------------------------------------------------------------------

func init() {
	gin.SetMode(gin.TestMode)
}

// 构造带 user_id / IP / UA 的 gin.Context
func makeAuditCtx(t *testing.T, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)
	c.Request.Header.Set("User-Agent", "test-ua/1.0")
	c.Set("user_id", userID)
	return c, w
}

// TestRecordAudit_AutoExtractsUserID：从 c.Get("user_id") 自动取。
func TestRecordAudit_AutoExtractsUserID(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	c, _ := makeAuditCtx(t, 42)
	RecordAudit(c, AuditTargetAuth, 7, "login.success", gin.H{"username": "alice"})

	rows, err := DB.Query(`SELECT actor_id, target_type, target_id, action, actor_ip, user_agent FROM audit_log`)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("audit_log 期望 1 行")
	}
	var actorID int64
	var targetType, action, actorIP, userAgent string
	var targetID int64
	if err := rows.Scan(&actorID, &targetType, &targetID, &action, &actorIP, &userAgent); err != nil {
		t.Fatalf("scan 失败: %v", err)
	}
	if actorID != 42 {
		t.Errorf("actor_id 应为 42，实际 %d", actorID)
	}
	if targetType != string(AuditTargetAuth) || targetID != 7 || action != "login.success" {
		t.Errorf("fields 不匹配: type=%s id=%d action=%s", targetType, targetID, action)
	}
	if actorIP == "" {
		t.Error("actor_ip 应非空")
	}
	if userAgent != "test-ua/1.0" {
		t.Errorf("user_agent 应为 test-ua/1.0，实际 %q", userAgent)
	}
}

// TestRecordAudit_NilContext_NoPanic。
func TestRecordAudit_NilContext_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil context 不应 panic: %v", r)
		}
	}()
	// DB nil 时仅 Printf warn，不 panic
	RecordAudit(nil, AuditTargetAuth, 0, "test", gin.H{"x": 1})
}

// TestRecordAudit_NoUserIDInContext_DefaultsToZero。
func TestRecordAudit_NoUserIDInContext_DefaultsToZero(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)
	// 不设 user_id

	RecordAudit(c, AuditTargetAuth, 0, "no_user", gin.H{"reason": "anonymous"})

	var actorID int64
	if err := DB.QueryRow(`SELECT actor_id FROM audit_log WHERE action='no_user'`).Scan(&actorID); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if actorID != 0 {
		t.Errorf("无 user_id 时 actor_id 应为 0，实际 %d", actorID)
	}
}

// TestRecordAuditBy_ExplicitActorID：actor_user_id 显式传入，覆盖 c.Get("user_id")。
func TestRecordAuditBy_ExplicitActorID(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	c, _ := makeAuditCtx(t, 99) // context 里是 99
	RecordAuditBy(c, 7, AuditTargetAuth, 0, "login.failed", gin.H{"reason": "user_not_found"})

	// 期望 actor_id = 7（显式传入），不是 99（context 里的）
	var actorID int64
	if err := DB.QueryRow(`SELECT actor_id FROM audit_log WHERE action='login.failed'`).Scan(&actorID); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if actorID != 7 {
		t.Errorf("显式 actorID 应为 7，实际 %d", actorID)
	}
}

// TestRecordAuditBy_NilContext_NoPanic。
func TestRecordAuditBy_NilContext_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil context 不应 panic: %v", r)
		}
	}()
	RecordAuditBy(nil, 0, AuditTargetAuth, 0, "test", gin.H{"x": 1})
}

// TestRecordAuditStandalone_NoContext：完全无 gin.Context 也能写。
func TestRecordAuditStandalone_NoContext(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	RecordAuditStandalone(7, AuditTargetBackup, 100, "verify.ok",
		"verify_daily", "scheduler/1.0", gin.H{"snowid": "SN-001"})

	var actorID int64
	var actorIP, userAgent string
	if err := DB.QueryRow(`SELECT actor_id, actor_ip, user_agent FROM audit_log WHERE action='verify.ok'`).
		Scan(&actorID, &actorIP, &userAgent); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if actorID != 7 || actorIP != "verify_daily" || userAgent != "scheduler/1.0" {
		t.Errorf("standalone 字段错: actor=%d ip=%q ua=%q", actorID, actorIP, userAgent)
	}
}

// TestRecordAudit_NilMeta_DefaultsToEmptyObject。
func TestRecordAudit_NilMeta_DefaultsToEmptyObject(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	c, _ := makeAuditCtx(t, 1)
	RecordAudit(c, AuditTargetAuth, 0, "no_meta") // meta 缺省

	var detail string
	if err := DB.QueryRow(`SELECT detail FROM audit_log WHERE action='no_meta'`).Scan(&detail); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if detail != "{}" {
		t.Errorf("meta 缺省应为 '{}'，实际 %q", detail)
	}
}

// TestRecordAudit_MetaMarshaledToJSON。
func TestRecordAudit_MetaMarshaledToJSON(t *testing.T) {
	cleanup := auditTestDB(t)
	defer cleanup()

	c, _ := makeAuditCtx(t, 1)
	meta := gin.H{
		"username": "alice",
		"count":    3,
		"reason":   "test",
	}
	RecordAudit(c, AuditTargetAuth, 0, "meta_test", meta)

	var detail string
	if err := DB.QueryRow(`SELECT detail FROM audit_log WHERE action='meta_test'`).Scan(&detail); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	// 反序列化验证
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(detail), &got); err != nil {
		t.Fatalf("detail 不是合法 JSON: %v (raw=%q)", err, detail)
	}
	if got["username"] != "alice" {
		t.Errorf("meta.username 期望 alice，实际 %v", got["username"])
	}
	if got["count"].(float64) != 3 { // JSON 数字反序列化为 float64
		t.Errorf("meta.count 期望 3，实际 %v", got["count"])
	}
}

// 注：当前 helper 不承诺"DB nil 安全"——AppendAudit 内部 DB.QueryRow 会 panic。
// DB nil 是 InitDatabase 之前的状态，业务代码不会进入该路径。
// 若要加 DB nil 兜底需在 AppendAudit 内 DB != nil 守卫，会影响 Round 17 哈希链语义，
// 故不在本轮做。
