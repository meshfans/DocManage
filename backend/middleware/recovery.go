package middleware

import (
	"net/http"
	"runtime/debug"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

// Recovery panic 兜底中间件。
//
// 背景（2026-09-26）：
//   - Go server 在 idle 后偶发 panic（net/http server 协程被 cancel 时
//     抛出"negative response"，根因待定位），panic 沿 goroutine 传播未捕获，
//     直接导致整个进程退出。
//   - 历史上曾用 gin.Recovery()，但它的默认行为是写 500 + log 到 stderr，
//     没接 utils.LogError（项目统一日志体系），不便于运维查询。
//
// 设计：
//  1. 沿用 gin.RecoveryWithWriter 的核心逻辑（recover panic → 写 500 → 不 abort 进程）
//  2. 但 panic stack + request 上下文都写到 utils.LogError（项目统一日志）
//
// 注册顺序：router.Use(middleware.Recovery()) —— 必须是**最先注册**的中间件，
// 兜底所有后续 handler panic。Trace 之前。
//
// 测试建议：手动 `panic("test")` 嵌入某 handler；确认：
//   - 客户端收到 HTTP 500（不再是连接断开）
//   - utils.LogError 输出一行含 "panic recovered" + stack + request 信息
//   - server 进程仍存活（不会 panic 中断整个进程）
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		// 1. 收集请求上下文（method / path / query）
		method := c.Request.Method
		path := c.Request.URL.Path
		// 路径上的 :id 等通配符替换为真实值（gin 的 FullPath 返回注册模板）
		if p := c.FullPath(); p != "" {
			path = p
		}
		// query 不超过 256 字符（日志避免过长被截）
		query := c.Request.URL.RawQuery
		if len(query) > 256 {
			query = query[:256] + "...(truncated)"
		}

		// 2. panic 内容（可能是 error / string / 其他类型）
		//    L-2 修复（2026-10-02）：不再拼成 errMsg 给前端 ——
		//    详见下方第 6 步注释。stack + err 已在第 4 步走 utils.LogError 落盘。

		// 3. stack trace
		stack := string(debug.Stack())

		// 4. 打日志（统一走 utils.LogError，便于 ELK / 告警关联）
		utils.LogError("[panic] %s %s%s recovered: %v\nstack:\n%s",
			method, path,
			func() string {
				if query == "" {
					return ""
				}
				return "?" + query
			}(),
			err, stack)

		// 5. 标记发生过 panic，供后续中间件/handler 判断（可选）
		if c.Keys != nil {
			c.Set("panic_recovered", true)
		}

		// 6. 返回 500 JSON。字段与后端统一响应信封保持一致
		//    （success / code / message）。
		//
		// 注意：2026-10-02 修复 L-2 — 不再回传 panic 原文 detail 字段。
		//   原"仅前端调试用"风险路径：panic 原文可能含 DB 连接串 / JWT secret
		//   / license password / 业务 PII 等敏感信息，对外暴露即泄露通道。
		//   调试信息已在第 4 步走 utils.LogError 落盘，运维可查日志定位。
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"code":    "internal.panic_recovered",
			"message": "服务内部异常（panic 已记录），请稍后重试",
		})
	})
}
