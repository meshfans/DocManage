package middleware

import (
	"net/http"

	"doc/config"

	"github.com/gin-gonic/gin"
)

// ExperienceReadOnly blocks business mutations while keeping authentication
// and read APIs available. Used for customer demo/trial deployments where
// data must not be modified.
//
// Behavior:
//   - config.IsExperienceMode() == false → pass-through (no-op)
//   - mode == "experience"
//     → GET / HEAD / OPTIONS: always allowed
//     → whitelist POST paths: login / logout / refresh-token / share audit consent
//     → all other mutations → 423 Locked + "体验环境不允许修改业务数据"
//
// --- 边界（改本文件前必读）-----------------------------------------------
// 本中间件是**唯一的 HTTP 层闸门**，仅按 HTTP method + path 判定。以下写入
// 路径不受它约束，改动时须自行判断体验模式下是否也要拦：
//
//  1. WebSocket：握手是 GET，会被当作读请求直接放行。当前安全——
//     handlers/websocket.go 的 "message" / "broadcast" 只做内存广播、不落库。
//     若日后新增基于 WS 的落库写入，将静默绕过只读拦截。
//  2. 后台调度任务：进程内执行，不经 HTTP，本中间件看不到。本项目
//     validateConfig 不强制开启 backup / scheduler，但配置里显式
//     enabled=true 的定时任务仍会运行并写库。
//
// 另：config.Database.Mode 不得经管理端 API 改写（一次 POST 即可解除全站
// 只读，且无需重启），该约束由 handlers 侧 experience_mode_guard_test.go 守护。
func ExperienceReadOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.IsExperienceMode() || experienceWriteAllowed(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusLocked, gin.H{
			"success": false,
			"message": "体验环境不允许修改业务数据",
		})
	}
}

func experienceWriteAllowed(method, path string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return true
	}
	// Whitelist: non-business-mutation POST/PUT/DELETE endpoints that need to
	// work during experience mode. Share audit pages are public and safe.
	// 2026-09-30 说明：/api/share/audit/consent-letter-view 目前在本项目尚未注册
	// （仅 DocManage / DocWMS 有该路由），故这条暂时是"预留白名单"而非死代码。
	// 故意保留而非删除：若日后补上该公开分享路由，漏改白名单会让它静默返回 423。
	for _, allowedPath := range []string{
		"/api/login",
		"/api/logout",
		"/api/refresh-token",
		"/api/share/audit/consent-letter-view",
	} {
		if path == allowedPath {
			return true
		}
	}
	return false
}
