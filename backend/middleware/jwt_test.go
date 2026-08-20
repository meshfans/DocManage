package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"doc/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// ==================== JWTAuth 中间件测试 ====================
//
// 范围（DB-independent）：
//   - 无 Authorization 头 → 401
//   - 错误格式（无 Bearer 前缀）→ 401
//   - token type=refresh 而非 access → 401
//   - 过期 token → 401
//   - 篡改 token（wrong secret）→ 401
//   - iss 不匹配 → 401（Phase 4a High #10 验证中间件层生效）
//   - 黑名单 token → 401
//   - 合法 token → 200，context 含 user_id/username
// ----------------------------------------------------------------------------

func init() {
	gin.SetMode(gin.TestMode)
}

// runJWTMiddleware 构造一个最小路由，调用 JWTAuth，断言响应码。
func runJWTMiddleware(t *testing.T, jwtUtils *utils.JWTUtils, authHeader string) int {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)
	if authHeader != "" {
		c.Request.Header.Set("Authorization", authHeader)
	}
	JWTAuth(jwtUtils)(c)
	return w.Code
}

// TestJWTAuth_NoAuthHeader_NoHeaderReturns401
func TestJWTAuth_NoAuthHeader_Returns401(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	if code := runJWTMiddleware(t, j, ""); code != http.StatusUnauthorized {
		t.Fatalf("无 Authorization 头期望 401，实际 %d", code)
	}
}

// TestJWTAuth_NotBearer_Returns401
func TestJWTAuth_NotBearer_Returns401(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	if code := runJWTMiddleware(t, j, "Basic abc"); code != http.StatusUnauthorized {
		t.Fatalf("非 Bearer 期望 401，实际 %d", code)
	}
}

// TestJWTAuth_RefreshTokenUsedAsAccess_Returns401
func TestJWTAuth_RefreshTokenUsedAsAccess_Returns401(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	tok, _, err := j.GenerateRefreshToken(7, "alice")
	if err != nil {
		t.Fatalf("签发 refresh 失败: %v", err)
	}
	if code := runJWTMiddleware(t, j, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("refresh token 作 access 期望 401，实际 %d", code)
	}
}

// TestJWTAuth_ExpiredToken_Returns401
func TestJWTAuth_ExpiredToken_Returns401(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", -time.Minute, -time.Hour)
	tok, _, _ := j.GenerateAccessToken(7, "alice")
	if code := runJWTMiddleware(t, j, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("过期 token 期望 401，实际 %d", code)
	}
}

// TestJWTAuth_WrongSecret_Returns401
func TestJWTAuth_WrongSecret_Returns401(t *testing.T) {
	signer := utils.NewJWTUtils("secret-A-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	verifier := utils.NewJWTUtils("secret-B-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	tok, _, _ := signer.GenerateAccessToken(7, "alice")
	if code := runJWTMiddleware(t, verifier, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("错 secret 期望 401，实际 %d", code)
	}
}

// TestJWTAuth_InvalidIssuer_Returns401 Phase 4a (High #10) 中间件层验证。
//
// 手动签发 iss=evil-issuer 的 token（用同样 secret），JWTAuth 应拦截。
func TestJWTAuth_InvalidIssuer_Returns401(t *testing.T) {
	secret := "test-secret-32-chars-xxxxxxxxxxxxxx"
	j := utils.NewJWTUtils(secret, time.Minute, time.Hour)

	claims := &utils.Claims{
		UserID:    7,
		Username:  "mallory",
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "evil-issuer",
		},
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(secret))

	if code := runJWTMiddleware(t, j, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("iss 不匹配期望 401，实际 %d", code)
	}
}

// TestJWTAuth_ValidToken_Returns200 ContextContainsClaims
func TestJWTAuth_ValidToken_Returns200(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	tok, _, _ := j.GenerateAccessToken(7, "alice")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tok)

	var seenUID int64
	var seenUname string
	// JWTAuth 必须先跑（解析 token + c.Set），再走 handler 验证 context
	JWTAuth(j)(c)
	if !c.IsAborted() {
		// 模拟后续 handler
		seenUID = c.GetInt64("user_id")
		seenUname = c.GetString("username")
		c.Status(http.StatusOK)
	}
	if seenUID != 7 || seenUname != "alice" {
		t.Errorf("claims 错误: uid=%d uname=%q", seenUID, seenUname)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("合法 token 期望 200，实际 %d", w.Code)
	}
}

// TestJWTAuth_RevokedToken_Returns401 黑名单路径（Phase 4b 之前的 RevokeToken）。
func TestJWTAuth_RevokedToken_Returns401(t *testing.T) {
	j := utils.NewJWTUtils("test-secret-32-chars-xxxxxxxxxxxxxx", time.Minute, time.Hour)
	tok, _, _ := j.GenerateAccessToken(7, "alice")

	// 加入黑名单（TTL 60s > 当前测试时长）
	utils.RevokeToken(tok, time.Now().Add(time.Minute).Unix())

	if code := runJWTMiddleware(t, j, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("黑名单 token 期望 401，实际 %d", code)
	}
}
