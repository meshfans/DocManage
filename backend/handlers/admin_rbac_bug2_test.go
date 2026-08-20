package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ==================== BUG-2 修复测试（2026-08-20）====================
//
// 目标：APIGateMiddleware admin 旁路判定从 JWT-only 改为 DB-backed。
// 证据：username="admin" 不再直接放行；需 DB 验证 users.roles 含 "admin"。
//
// 由于本测试不打 DB（避免依赖 InitDatabase），我们验证性质：
//   - IsRBACAdmin(username="admin") 现在会查 DB（panics on nil DB）
//   - 这证明 IsRBACAdmin 内部已切到 IsAdminUser，不是之前 JWT-only 的 username 短路

// TestIsRBACAdmin_BUG2_NoLongerHardcodedByUsername 验证 BUG-2 修复。
//
// 流程：
//  1. 构造 username="admin" 的 gin.Context
//  2. 调 IsRBACAdmin(c)
//  3. 期望 panic（DB nil，证明 IsAdminUser 路径被走）
//
// 如果 IsRBACAdmin 仍是 JWT-only `username == "admin"` 短路，本测试
// 会"无 panic 通过"（意味着 bug 没修），t.Errorf 标红。
func TestIsRBACAdmin_BUG2_NoLongerHardcodedByUsername(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	c.Set("username", "admin") // 假装是 admin JWT

	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("BUG-2 修复后 IsRBACAdmin(username=\"admin\") 必须查 DB；" +
				"若无 panic 说明仍是 JWT-only 短路（bug 没修）")
			return
		}
		// 期望 panic 内容是 DB 相关的错误
		// （utils.IsAdminUser 内部调 database.GetUserByUsername，DB nil 会 panic）
		t.Logf("IsRBACAdmin 触达 DB（panic as expected）: %v", r)
	}()

	IsRBACAdmin(c)
	// 走到这里 = 没 panic = bug 没修
}

// TestAPIGateMiddleware_BUG2_CallsIsAdminUser 验证 APIGateMiddleware 改造。
//
// 流程：
//  1. 构造 username="admin" 的 gin.Context
//  2. 走 APIGateMiddleware
//  3. 期望 DB panic（与 IsAdminUser 一致）
//
// 间接证据：APIGateMiddleware 内部第一行就调 IsAdminUser(c)，
// DB 必然 nil → panic。如果原 IsRBACAdmin 被保留 → 短路 → 无 panic。
func TestAPIGateMiddleware_BUG2_CallsIsAdminUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 模拟 protected group 链：JWTAuth 注入 username → APIGate
	r.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("user_id", int64(1))
		c.Next()
	})
	r.GET("/api/test", APIGateMiddleware(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("BUG-2 修复后 APIGateMiddleware 应调 IsAdminUser → 触发 DB panic；" +
				"若无 panic 说明 admin 旁路仍是 JWT-only（bug 没修）")
		}
	}()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)

	// 走到这里 = 没 panic = bug 没修
}
