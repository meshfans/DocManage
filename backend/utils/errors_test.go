package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// okTest 拉起一个最小 gin 引擎准备测试。
func setupTest(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

// TestErr_StatusMapping 验证 httpStatusFor 输出与 RFC 一致。
func TestErr_StatusMapping(t *testing.T) {
	cases := []struct {
		code     ErrCode
		wantStatus int
	}{
		// 401
		{CodeAuthInvalidCredentials, http.StatusUnauthorized},
		{CodeAuthTokenExpired, http.StatusUnauthorized},
		// 403
		{CodeForbidden, http.StatusForbidden},
		// 404
		{CodeNotFound, http.StatusNotFound},
		{CodeCustomerNotFound, http.StatusNotFound},
		{CodeBackupNotFound, http.StatusNotFound},
		// reminder 模块（Phase 3c 新增）
		{CodeReminderTemplateNotFound, http.StatusNotFound},
		{CodeReminderSubscriptionNotFound, http.StatusNotFound},
		// 409
		{CodeCustomerExists, http.StatusConflict},
		// 410
		{CodeContractLocked, http.StatusGone},
		{CodeSealRevoked, http.StatusGone},
		// 429
		{CodeRateLimited, http.StatusTooManyRequests},
		// 503
		{CodeMaintenance, http.StatusServiceUnavailable},
		// 500
		{CodeInternal, http.StatusInternalServerError},
		{CodeAuditChainBroken, http.StatusInternalServerError},
		{CodeBackupCorrupted, http.StatusInternalServerError},
		// 400
		{CodeInvalidParam, http.StatusBadRequest},
		{CodeAuthPasswordWeak, http.StatusBadRequest},
		{CodeAuthPasswordReused, http.StatusBadRequest},
		{CodeMediaTooLarge, http.StatusBadRequest},
		// 405
		{CodeMethodNotAllowed, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		got := httpStatusFor(c.code)
		if got != c.wantStatus {
			t.Errorf("httpStatusFor(%s) = %d, want %d", c.code, got, c.wantStatus)
		}
	}
}

// TestErr_WritesEnvelope 验证 Err 写入的 envelope 字段齐全。
//
// ⚠️ 2026-08-19 M-E4 修复后契约：
//   - error / detail 字段 = wrapped error 字符串（不再塞 code）
//   - code 字段 = 归一化错误码（前端 i18n 翻译用）
func TestErr_WritesEnvelope(t *testing.T) {
	r := setupTest(t)
	r.GET("/x", func(c *gin.Context) {
		Err(c, CodeAuthInvalidCredentials, "密码错误", errors.New("bcrypt mismatch"))
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["success"] != false {
		t.Errorf("success = %v, want false", body["success"])
	}
	if body["message"] != "密码错误" {
		t.Errorf("message = %v, want 密码错误", body["message"])
	}
	if body["code"] != "auth.invalid_credentials" {
		t.Errorf("code = %v, want auth.invalid_credentials", body["code"])
	}
	// M-E4 修复：error 字段 = wrapped error 字符串，不再是 code
	if body["error"] != "bcrypt mismatch" {
		t.Errorf("error = %v, want bcrypt mismatch (M-E4 修复后契约)", body["error"])
	}
	if body["detail"] != "bcrypt mismatch" {
		t.Errorf("detail = %v, want bcrypt mismatch", body["detail"])
	}
}

// TestErr_NoDetail 验证没有 detail 时输出为空字符串而非 nil 字段。
func TestErr_NoDetail(t *testing.T) {
	r := setupTest(t)
	r.GET("/x", func(c *gin.Context) {
		Err(c, CodeForbidden, "权限不足")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["detail"] != "" {
		t.Errorf("detail = %v, want empty string", body["detail"])
	}
}

// TestErrInternal 验证 ErrInternal 用 detail.Error() 作为 message。
func TestErrInternal(t *testing.T) {
	r := setupTest(t)
	r.GET("/x", func(c *gin.Context) {
		ErrInternal(c, CodeInternal, errors.New("db disk full"))
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["message"] != "db disk full" {
		t.Errorf("message = %v, want db disk full", body["message"])
	}
	if body["code"] != "common.internal" {
		t.Errorf("code = %v, want common.internal", body["code"])
	}
}

// TestErrCode_AllStringType 验证所有 const 是 ErrCode 字符串。
func TestErrCode_AllStringType(t *testing.T) {
	// 通过 httpStatusFor 间接检查：未知 code 走 default 400。
	bad := ErrCode("nonexistent.foo")
	if got := httpStatusFor(bad); got != http.StatusBadRequest {
		t.Errorf("unknown code should fallback to 400, got %d", got)
	}
}

// TestErrCode_DuplicateStatusDistinctCodes 验证不同 code 共享同一状态码没问题。
func TestErrCode_DuplicateStatusDistinctCodes(t *testing.T) {
	codes := []ErrCode{
		CodeAuthInvalidCredentials,
		CodeAuthUserNotFound,
		CodeAuthTokenExpired,
	}
	for _, c := range codes {
		if httpStatusFor(c) != http.StatusUnauthorized {
			t.Errorf("code %s should map to 401", c)
		}
	}
}
