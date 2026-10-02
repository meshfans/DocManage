package utils

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   string     `json:"error,omitempty"`

	// Code 归一化错误码（前端 i18n 用，见 frontend/src/utils/error.ts）。
	//
	// 2026-10-01 C1 统一补齐：与 utils/errors.go 的 Err(c, code, msg) 并存 ——
	// 后者用于业务细分码，本字段按 HTTP 状态兜底，覆盖仍用
	// utils.Error / BadRequest / Unauthorized 的出口。
	Code string `json:"code,omitempty"`
}

// defaultCodeForStatus 按 HTTP 状态映射到归一化错误码。
//
// 目的：**零调用点改动**地让既有错误出口都带上可翻译的 code，
// 前端 utils/error.ts 优先用 code 查表，查不到才回落到 message。
//
// 粒度是 HTTP 语义层（400/401/403/404/409/422/429/500/503），不表达业务细分
// （如 auth.invalid_credentials）；业务细分码由 utils.Err(c, code, msg) 显式给出。
func defaultCodeForStatus(status int) string {
	switch status {
	case 400:
		return "common.invalid_param"
	case 401:
		return "common.unauthorized"
	case 403:
		return "common.forbidden"
	case 404:
		return "common.not_found"
	case 409:
		return "common.conflict"
	case 422:
		return "common.unprocessable"
	case 429:
		return "common.too_many_requests"
	case 500:
		return "internal.error"
	case 503:
		return "internal.unavailable"
	default:
		return ""
	}
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(200, Response{
		Success: true,
		Data:    data,
	})
}

func Error(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, Response{
		Success: false,
		Message: message,
		Code:    defaultCodeForStatus(statusCode),
	})
}

func ErrorWithDetail(c *gin.Context, statusCode int, message string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = fmt.Sprintf("%v", err)
	}
	c.JSON(statusCode, Response{
		Success: false,
		Message: message,
		Error:   errMsg,
		Code:    defaultCodeForStatus(statusCode),
	})
}

// Unauthorized 返回 401。可选传入 message 自定义提示（不传则默认 "Unauthorized"）。
// 推荐业务侧传具体原因，例如 utils.Unauthorized(c, "用户名或密码错误")。
func Unauthorized(c *gin.Context, message ...string) {
	msg := "Unauthorized"
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	c.JSON(401, Response{
		Success: false,
		Message: msg,
		Code:    defaultCodeForStatus(401),
	})
}

func BadRequest(c *gin.Context, message string) {
	c.JSON(400, Response{
		Success: false,
		Message: message,
		Code:    defaultCodeForStatus(400),
	})
}
