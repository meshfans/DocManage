package middleware

import (
	"doc/config"
	"doc/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Maintenance 返回维护模式拦截中间件（第四阶段 Phase 4.3 P0）。
//
// 用途：恢复（RestoreFromBackup）期间拒绝所有写操作，避免数据漂移。
//
// 行为：
//   - cfg.MaintenanceMode == false → 放行
//   - cfg.MaintenanceMode == true  → 仅放行白名单，其余返回 503 + Retry-After: 60
//
// 白名单（精确前缀匹配）：
//   - /health, /healthz, /readyz, /metrics 健康检查（liveness / readiness / metrics 探针）
//   - /api/system/maintenance              维护模式查询 / 控制自身
//
// 注意：登录 / 刷新 token 也被拦截（避免维护中创建新会话）。
func Maintenance() gin.HandlerFunc {
	whitelist := []string{
		"/health",
		"/healthz",
		"/readyz",
		"/metrics",
		"/api/system/maintenance",
	}

	return func(c *gin.Context) {
		if !config.IsMaintenanceMode() {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		for _, prefix := range whitelist {
			if strings.HasPrefix(path, prefix) {
				c.Next()
				return
			}
		}

		c.Header("Retry-After", "60")
		utils.Warn("[Maintenance] blocked request: %s %s", c.Request.Method, path)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"code":    "MAINTENANCE_MODE",
			"message": "系统维护中，请稍后重试",
		})
	}
}
