package middleware

import (
	"strings"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

// K.7：JWT 校验失败原因标签（用于 Prometheus 上报）。
// 区分"签名错"vs"算法错"vs"iss 错"等，运维升级期可观测。
const (
	jwtRejectNoHeader   = "no_header"
	jwtRejectBadFormat  = "bad_format"
	jwtRejectRevoked    = "revoked"
	jwtRejectInvalidIss = "invalid_issuer"
	jwtRejectOther      = "other"
)

// recordReject 把 JWT 校验失败原因上报为业务事件（复用 metrics 计数组件）。
// 注：utils.IncBusinessEvent 是公开函数，事件总线 subscribers 也调；
// 这里直接调它避免 publish/subscribe 往返开销（每秒可能上千次）。
func recordReject(reason string) {
	utils.IncBusinessEvent("auth.jwt.reject." + reason)
}

func JWTAuth(jwtUtils *utils.JWTUtils) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			recordReject(jwtRejectNoHeader)
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			recordReject(jwtRejectBadFormat)
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
			recordReject(jwtRejectRevoked)
			utils.Unauthorized(c, "token 已失效，请重新登录")
			c.Abort()
			return
		}

		claims, err := jwtUtils.ValidateToken(tokenString)
		if err != nil {
			// K.7：区分 iss 错误（升级期高频）vs 其他（签名错、过期、算法错）。
			// 便于运维观测 Phase 4a 升级期间"老 token 拦截数"。
			// 注：与 utils/jwt.go 中 errors.New("invalid issuer") 字符串比较，
			// errors.Is 不行（每次 errors.New 都是不同身份）。
			reason := jwtRejectOther
			if err.Error() == "invalid issuer" {
				reason = jwtRejectInvalidIss
			}
			recordReject(reason)
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		if claims.TokenType != "access" {
			recordReject(jwtRejectOther)
			utils.Unauthorized(c)
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
