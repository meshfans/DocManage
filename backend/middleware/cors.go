package middleware

import (
	"doc/config"

	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := config.GlobalConfig

		origin := c.Request.Header.Get("Origin")

		allowedOrigins := cfg.CORS.AllowedOrigins
		allowed := false
		isWildcard := len(cfg.CORS.AllowedOrigins) > 0 && cfg.CORS.AllowedOrigins[0] == "*"

		if isWildcard {
			allowed = true
		} else {
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == origin {
					allowed = true
					break
				}
			}
		}

		if allowed {
			if isWildcard {
				c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Token, Sec-WebSocket-Key, Sec-WebSocket-Version, Sec-WebSocket-Extensions, Sec-WebSocket-Protocol, X-Trace-ID")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		// 注：不要在这里设置 Connection: Upgrade / Upgrade: websocket / Access-Control-Allow-Protocols。
		// 这些是 hop-by-hop 的 WS 握手头，gorilla/websocket.Upgrader.Upgrade 会自己写。
		// 全局污染会让 Nginx / 反代断开 keep-alive，并触发 HTTP/1.1 规范违反告警。
		// 跨域下载必须 expose Content-Disposition，前端 fetch+blob 才能读到 filename。
		// X-Trace-ID 必须 expose，前端 fetch 才能读到 trace_id 用于报错时引用。
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Type, Authorization, X-Trace-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
