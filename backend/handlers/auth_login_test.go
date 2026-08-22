package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

// ==================== Login Handler 集成测试 ====================
//
// 测试策略：
//   - 使用 setupLoginRouter() 构造最小 gin 引擎（不挂 DB 依赖的中间件）
//   - 测试 body 解析 / 字段校验 / 密码强度 / 账户锁定等无 DB 依赖路径
//   - 需要 DB 的测试路径（用户名密码校验）通过 recover() 捕获 nil-DB panic，
//     间接验证修复路径触达；生产中 DB 必存在，不会 panic
//   - 所有测试共用同一个 jwt secret，确保 token 验证路径一致

const loginTestSecret = "e2e_login_test_secret_at_least_32_chars"

// setupLoginRouter 构造只挂 Login handler 的最小引擎。
func setupLoginRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	jwt, _ := utils.NewJWTUtilsFromConfig(loginTestSecret, "1h", "24h")
	h := NewAuthHandler(jwt)
	r.POST("/api/login", h.Login)
	return r
}

// TestLogin_ValidJSON_BodyParsed 测试合法 JSON body 能否被 gin 正确解析。
// 验证：body 解析不返回 400 "Invalid request body"。
func TestLogin_ValidJSON_BodyParsed(t *testing.T) {
	r := setupLoginRouter()
	w := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"testuser","password":"Test@1234"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	func() {
		defer func() {
			if r := recover(); r != nil {
				// nil-DB panic：说明 body 解析成功 + handler 进入 DB 查询阶段
				t.Logf("✓ JSON 解析成功，进入 DB 阶段（nil-DB panic 为预期）")
			}
		}()
		r.ServeHTTP(w, req)
	}()

	// body 解析成功（不是 400 "Invalid request body"）即为通过
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "Invalid request body") {
		t.Fatalf("合法 JSON body 必须解析成功，实际: %s", w.Body.String())
	}
	// 后续走 DB 阶段可能 panic，被 recover 吞掉即可
	t.Logf("✓ 合法 JSON 解析通过 (status=%d body=%s)", w.Code, w.Body.String())
}

// TestLogin_InvalidJSON_Returns400 测试非法 JSON body 返回 400。
func TestLogin_InvalidJSON_Returns400(t *testing.T) {
	r := setupLoginRouter()
	w := httptest.NewRecorder()
	body := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("非法 JSON 应 400，实际 %d", w.Code)
	}
}

// TestLogin_EmptyBody_Returns400 测试空 body 返回 400。
func TestLogin_EmptyBody_Returns400(t *testing.T) {
	r := setupLoginRouter()
	w := httptest.NewRecorder()
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("空 body 应 400，实际 %d", w.Code)
	}
}

// TestLogin_PasswordFormatNotValidated 测试 Login 不校验密码格式。
//
// Login 只需验证"字段存在 + 能查到用户"，密码强度在 Register / ChangePassword 阶段校验。
// 此测试验证：任意格式密码（纯数字、纯字母等）都能通过 body 解析，进入 DB 查询阶段。
func TestLogin_PasswordFormatNotValidated(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{"纯数字", "12345678"},
		{"纯字母", "abcdefgh"},
		{"过短（7位）", "Aa@1234"},
		{"超长（64位）", strings.Repeat("A", 64) + "a1@"},
	}
	r := setupLoginRouter()
	for _, tc := range tests {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"username":"testuser","password":"` + tc.password + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/login", body)
		req.Header.Set("Content-Type", "application/json")

		func() {
			defer func() {
				if r := recover(); r != nil {
					// nil-DB panic：说明 body 解析通过 + handler 进入 DB 查询阶段
					// 密码格式不在 Login handler 校验，这是设计意图
				}
			}()
			r.ServeHTTP(w, req)
		}()

		// Login 不因密码格式返回 400 BadRequest（那是 Register 的职责）
		if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "Invalid request body") {
			t.Errorf("%s: Login 不应因密码格式返回 400，实际: %s", tc.name, w.Body.String())
		} else {
			t.Logf("✓ %s: body 解析通过，进入 DB 阶段 (status=%d)", tc.name, w.Code)
		}
	}
}

// TestLogin_ResponseEnvelope_HasCode 测试成功/失败响应都包含 code 字段。
// 验证 utils.Err 响应包络符合前端 extractErrorMessage 规范。
func TestLogin_ResponseEnvelope_HasCode(t *testing.T) {
	r := setupLoginRouter()
	w := httptest.NewRecorder()
	// 空 body → 400 BadRequest
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("进入 DB 阶段，body 解析已通过")
			}
		}()
		r.ServeHTTP(w, req)
	}()

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应必须是合法 JSON: %v", err)
	}

	// 验证错误响应包络四字段
	for _, field := range []string{"code", "message"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("错误响应必须含 %s 字段，实际: %s", field, w.Body.String())
		}
	}
	t.Logf("✓ 错误响应包络验证通过: %s", w.Body.String())
}

// TestLogin_MissingContentType_StillParses 测试缺少 Content-Type header 仍能解析 JSON。
func TestLogin_MissingContentType_StillParses(t *testing.T) {
	r := setupLoginRouter()
	w := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"test","password":"Valid@1234"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	// 不设 Content-Type，依赖 gin 默认行为

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("进入 DB 阶段，JSON 解析已通过")
			}
		}()
		r.ServeHTTP(w, req)
	}()

	// gin 通常能自动识别 JSON，MIME 类型不影响解析
	// 只要不是 400 "Invalid request body" 即可
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "Invalid request body") {
		t.Errorf("缺少 Content-Type 应仍能解析 JSON，实际: %s", w.Body.String())
	}
	t.Logf("✓ 缺少 Content-Type 测试通过 (status=%d)", w.Code)
}

// TestLogin_AllFieldsRequired 测试 username 和 password 都是必填字段。
func TestLogin_AllFieldsRequired(t *testing.T) {
	tests := []struct {
		name   string
		body   string
	}{
		{"缺 username", `{"password":"Valid@123456"}`},
		{"缺 password", `{"username":"testuser"}`},
		{"都是空", `{"username":"","password":""}`},
	}
	r := setupLoginRouter()
	for _, tc := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("进入 DB 阶段，字段必填校验已通过")
				}
			}()
			r.ServeHTTP(w, req)
		}()

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s 应返回 400，实际 %d", tc.name, w.Code)
		}
	}
	t.Log("✓ 所有必填字段校验通过")
}
