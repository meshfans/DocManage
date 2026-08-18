package handlers

import (
	"context"
	"net/http"
	"time"

	"doc/config"
	"doc/database"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// Healthz / Readyz / Metrics 处理器（Round 15）。
//
// 端点行为：
//   GET /healthz → liveness：进程能响应 HTTP 即视为存活，永远 200 + status:"alive"
//                  （handoff 约定：与 /health 兼容，但 healthz 显式返回 alive）
//   GET /health  → liveness：兼容旧路径，保留 service/status/version 字段，
//                  与原 /health JSON 结构一致
//   GET /readyz  → readiness：db / ws_hub / scheduler / maintenance 全通过才 200，
//                  任一失败 503 并在 JSON 里写明原因（status + checks）
//   GET /metrics → Prometheus text 0.0.4：每次请求刷新 runtime 指标后输出
//
// 设计要点：
//   - readyz 用请求 ctx 派生 2 秒超时，避免 IO 阻塞拖死探针
//   - 各依赖 nil-safe（DB / wsHub / sched 任一为 nil 视为不可用）
//   - 维护模式下仍允许 /health /healthz /readyz /metrics 通过（见 middleware.Maintenance 白名单）

// ServiceVersion 服务端版本号（与原 /health 输出保持一致：1.0.0）。
const ServiceVersion = "1.0.0"

// readyzTimeout readyz 单检查的最长允许时间。
// 包括 PingContext + PRAGMA quick_check + WS/Scheduler/Config 检查，全部共享同一 2 秒 ctx。
const readyzTimeout = 2 * time.Second

// HealthzHandler liveness 探针：进程存活即 healthy。
// 同时处理 /health（兼容旧路径，返回 service/status/version）
// 与 /healthz（handoff 约定，返回 status:"alive"）。
//
// 旧 /health 的 service/status/version 字段保留；/healthz 显式返回 alive。
//
// 工厂风格：返回 gin.HandlerFunc，handler 内部根据 URL.Path 区分。
func HealthzHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/healthz" {
			c.JSON(http.StatusOK, gin.H{
				"status":  "alive",
				"service": "doc-server",
				"version": ServiceVersion,
			})
			return
		}
		// /health（兼容旧探针路径）
		c.JSON(http.StatusOK, gin.H{
			"service": "doc-server",
			"status":  "healthy",
			"version": ServiceVersion,
		})
	}
}

// ReadyzHandler readiness 探针：依赖全部就绪才返回 200，否则 503。
//
// 检查项（任一失败即 503）：
//  1. database.QuickCheck(ctx)  → DB 非 nil + PingContext + PRAGMA quick_check == "ok"
//  2. wsHub != nil && !wsHub.IsClosed()   → WebSocket hub 已初始化且未关停
//  3. sched != nil && sched.Running()    → Scheduler 已初始化且启动后未 Stop
//  4. !config.IsMaintenanceMode()        → 非维护模式
//
// ctx 使用请求 ctx + 2 秒超时派生，防止底层 IO 阻塞。
//
// 验收契约（Round 15 handoff）：
//   - checks 键名：db / ws_hub / scheduler / maintenance
//   - status 字段：ready / not_ready
//   - 失败值：fail: <reason>
func ReadyzHandler(wsHub *WebSocketHandler, sched *services.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readyzTimeout)
		defer cancel()

		checks := map[string]string{
			"db":          "unknown",
			"ws_hub":      "unknown",
			"scheduler":   "unknown",
			"maintenance": "unknown",
		}
		ok := true

		// 1. DB quick check
		if err := database.QuickCheck(ctx); err != nil {
			checks["db"] = "fail: " + err.Error()
			ok = false
		} else {
			checks["db"] = "ok"
		}

		// 2. WebSocket hub 非 nil 且未关闭
		if wsHub == nil || wsHub.IsClosed() {
			checks["ws_hub"] = "fail: hub 未初始化或已关闭"
			ok = false
		} else {
			checks["ws_hub"] = "ok"
		}

		// 3. Scheduler 非 nil 且 Running
		if sched == nil || !sched.Running() {
			checks["scheduler"] = "fail: 未初始化或未运行"
			ok = false
		} else {
			checks["scheduler"] = "ok"
		}

		// 4. 维护模式
		if config.IsMaintenanceMode() {
			checks["maintenance"] = "fail: 维护模式开启"
			ok = false
		} else {
			checks["maintenance"] = "ok"
		}

		status := http.StatusOK
		body := gin.H{"status": "ready", "checks": checks}
		if !ok {
			status = http.StatusServiceUnavailable
			body["status"] = "not_ready"
		}
		c.JSON(status, body)
	}
}

// MetricsHandler 刷新运行时指标并以 Prometheus text 0.0.4 格式输出。
// Content-Type 固定为 text/plain; version=0.0.4; charset=utf-8。
//
// 使用 utils.DefaultRegistry().WriteTo(c.Writer) 写入响应：
//   - WriteTo 内部按 metric 名字典序遍历并输出
//   - 每次请求前刷新 runtime metrics，确保 goroutines / memstats / gc 是当前快照
//
// 工厂风格：返回 gin.HandlerFunc；handler 内部刷新 runtime 指标、设置
// Prometheus Content-Type、调用 utils.DefaultRegistry().WriteTo(c.Writer)
// 并记录错误。
func MetricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		utils.RefreshRuntimeMetrics()
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if err := utils.DefaultRegistry().WriteTo(c.Writer); err != nil {
			utils.LogError("metrics 输出失败: %v", err)
		}
	}
}
