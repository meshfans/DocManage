package middleware

import (
	"strconv"
	"time"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

// HTTP 指标采集中间件（Round 15）。
//
// 注册的指标：
//   - http_requests_total{method,path,status}        计数器
//   - http_request_duration_seconds{method,path}      直方图
//   - http_in_flight_requests                         Gauge
//
// 设计要点：
//   - path 使用 c.FullPath()（路由模板，如 "/api/customer/:id"）而非实际 URL，
//     避免高基数（每个 UUID 都成一类撑爆 Prometheus 内存）。
//   - 未匹配的路由用 "unknown" 作为 path label（404 等）。
//   - 指标句柄在包加载时通过 utils.Register*Vec 注册到 utils.DefaultRegistry()，
//     重复调用 Metrics() 安全：复用同一份已注册的句柄，不会再注册或 panic。
//   - 即使 handler 内部 panic，最终也能落到 defer 里采集并上报指标。

var (
	httpRequestsTotal   *utils.CounterVec
	httpRequestDuration *utils.HistogramVec
	httpInFlight        *utils.Gauge
)

// init 在包加载时注册一次。后续 Metrics() 调用复用同一份句柄，
// 因此 NewCombinedServer 在测试或重启时多次调用本中间件也不会重复注册同名指标。
func init() {
	httpRequestsTotal = utils.RegisterCounterVec(
		"http_requests_total",
		"HTTP 请求总数",
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = utils.RegisterHistogramVec(
		"http_request_duration_seconds",
		"HTTP 请求延迟（秒）",
		[]string{"method", "path"},
		utils.HTTPRequestDurationBuckets,
	)
	httpInFlight = utils.RegisterGauge(
		"http_in_flight_requests",
		"当前正在处理的 HTTP 请求数",
	)
}

// Metrics 返回 HTTP 指标采集中间件。
// 多次调用安全：复用同一份全局注册的指标句柄。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		httpInFlight.Inc()
		defer httpInFlight.Dec()

		// 即使 handler panic 也保证指标能上报。
		defer func() {
			if r := recover(); r != nil {
				path := resolvePath(c)
				status := safeStatus(c)
				httpRequestsTotal.WithLabels(map[string]string{
					"method": c.Request.Method,
					"path":   path,
					"status": status,
				}).Inc()
				httpRequestDuration.WithLabels(map[string]string{
					"method": c.Request.Method,
					"path":   path,
				}).Observe(time.Since(start).Seconds())
				// 重新抛出 panic，保留原有错误链路
				panic(r)
			}
		}()

		c.Next()

		path := resolvePath(c)
		status := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabels(map[string]string{
			"method": c.Request.Method,
			"path":   path,
			"status": status,
		}).Inc()
		httpRequestDuration.WithLabels(map[string]string{
			"method": c.Request.Method,
			"path":   path,
		}).Observe(time.Since(start).Seconds())
	}
}

// resolvePath 返回指标使用的 path label。
// 优先取 c.FullPath()（路由模板，避免基数爆炸）；未匹配路由用 "unknown"。
func resolvePath(c *gin.Context) string {
	if p := c.FullPath(); p != "" {
		return p
	}
	return "unknown"
}

// safeStatus 在 c.Writer.Status() 还未被赋值（如 panic 发生在 handler 写状态之前）
// 时返回 "500"，确保指标始终有合法 status label。
func safeStatus(c *gin.Context) string {
	if s := c.Writer.Status(); s != 0 {
		return strconv.Itoa(s)
	}
	return "500"
}
