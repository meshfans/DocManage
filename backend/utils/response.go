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
	})
}

func BadRequest(c *gin.Context, message string) {
	c.JSON(400, Response{
		Success: false,
		Message: message,
	})
}
