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

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Token")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		// 跨域下载必须 expose Content-Disposition，前端 fetch+blob 才能读到 filename
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
