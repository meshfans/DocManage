package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"doc/utils"
)

func JWTAuth(jwtUtils *utils.JWTUtils) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		tokenString := parts[1]

		// P0 修复（2026-06-28）：JWT 黑名单校验。
		// 之前 JWT 签发后无法吊销，admin 撤销角色 / 用户 logout 后旧 token 仍 24h 有效。
		// 现在 RevokeToken() 把 token hash 加入内存黑名单，此处同步检查。
		// 检查放在 ValidateToken 之前：避免对已吊销 token 跑昂贵的签名校验。
		if utils.IsTokenRevoked(tokenString) {
			utils.Unauthorized(c, "token 已失效，请重新登录")
			c.Abort()
			return
		}

		claims, err := jwtUtils.ValidateToken(tokenString)
		if err != nil {
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		if claims.TokenType != "access" {
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
