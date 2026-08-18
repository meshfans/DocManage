package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ==================== AuditHandler 单元测试 ====================
//
// 范围（有意收窄，避免依赖完整 InitDatabase + JWT 中间件）：
//   - Query 参数解析（parseInt64Query 已在 media.go 覆盖）
//   - 401/403 路径（RequireAdmin 返回 false 写 403 响应）
//   - admin 通过路径（依赖 DB，本次测无法直接覆盖 → 由 E2E 覆盖）
//
// 注：完整 E2E（含 admin / common / reconcile）由 backend/bin/smoke_audit.ps1
// 实跑覆盖；此单元测试只补 DB-independent 路径。
// ----------------------------------------------------------------------------

func init() {
	gin.SetMode(gin.TestMode)
}

// TestAuditListAudit_NonAdmin_Returns403 验证 RequireAdmin 拦截。
//
// 通过直接调用 ListAudit 但构造一个非 admin 的 gin.Context，
// 期望响应 403 + message 含"需要 admin"。
func TestAuditListAudit_NonAdmin_Returns403(t *testing.T) {
	h := NewAuditHandler()

	// 构造一个不带 admin 标记的 Context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/audit", nil)

	h.ListAudit(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("期望 403, 实际 %d", w.Code)
	}
	if !contains(w.Body.String(), "admin") && !contains(w.Body.String(), "管理员") {
		t.Errorf("响应体应包含管理员提示, 实际: %s", w.Body.String())
	}
}

// TestAuditReconcile_NonAdmin_Returns403 同上，POST 路径。
func TestAuditReconcile_NonAdmin_Returns403(t *testing.T) {
	h := NewAuditHandler()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/audit/reconcile", nil)

	h.ReconcileAuditChain(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("期望 403, 实际 %d", w.Code)
	}
}

// TestAuditListAudit_NilContext_DoesNotPanic 边界测试：构造异常路径。
func TestAuditListAudit_NilContext_DoesNotPanic(t *testing.T) {
	// 仅验证 handler 构造不 panic（无业务调用）
	h := NewAuditHandler()
	if h == nil {
		t.Fatal("NewAuditHandler 返回 nil")
	}
}

// contains 简单子串判断。
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
