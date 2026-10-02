package middleware

// FeatureGate 模块门禁中间件（2026-09-28）。
//
// 用法：
//
//	ragGroup := router.Group("/vec", middleware.FeatureGate("rag"))
//	ragGroup.GET("/stats", h.GetStats)
//
// 行为：
//   - sdk.GlobalHasFeature(key) == true：c.Next() 透传
//   - false：c.AbortWithStatusJSON(403) + code=license.feature_not_licensed
//
// 守门顺序（建议）：
//
//	protected.Use(auth.JWTAuth, ...)
//	protected.Use(handlers.APIGateMiddleware())
//	// 上方组已通过 JWT + RBAC；本中间件再加 license feature 守门
//	protected.Group("/vec", middleware.FeatureGate("rag"))
//
// 弃用历史：本文件新建，2026-09-28 启用；与已存在的 RAG 路由（combined_server.go:138+）配套。

import (
	"doc/pkg/license/sdk"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// FeatureGate 拦截未授权 license feature 的请求。
//
// key 用法：必须与 license payload.features 数组里的字符串精确相等（大小写/前后空格敏感）。
// 当前实际消费的 key：仅 "rag"（2026-09-28 与用户确认范围）。
//
// 注意：本中间件依赖 sdk.GlobalOutcome 已被 main.go SetGlobalOutcome 写入。
// 2026-09-29 硬切后，无效 license 的进程无法启动；GlobalOutcome()==nil 仅可能出现在
// 启动竞态，此时按未授权处理（403）。
func FeatureGate(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sdk.GlobalHasFeature(key) {
			c.Next()
			return
		}
		// 拒：403 + license.feature_not_licensed + module 字段（前端可按 code 翻译）。
		// 不在响应里塞"license"字眼，避免暴露授权细节。
		utils.ErrWithExtras(c, utils.CodeLicenseFeatureNotLicensed,
			"当前账号未授权该功能模块",
			gin.H{"module": key})
		c.Abort()
	}
}
