package middleware

import (
	"regexp"

	"doc/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TraceIDKey Gin Context 中 trace_id 的 key
const TraceIDKey = "trace_id"

// TraceIDHeader 请求/响应头名称
const TraceIDHeader = "X-Trace-ID"

// MaxTraceIDLen trace_id 最大长度。客户端传入的 X-Trace-ID 超过此长度会被丢弃并生成新 UUID。
// 选择 64：UUID 去掉连字符是 32，W3C TraceContext trace-id 是 32（hex），B3 也是 32，留一倍余量。
const MaxTraceIDLen = 64

// traceIDPattern trace_id 合法字符白名单。仅允许 [A-Za-z0-9_-]，避免日志注入 / HTTP 头走私。
// 客户端传入不合规字符时降级生成新 UUID，原值记 debug 日志便于排查。
var traceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Trace 返回 trace_id 中间件，确保每个请求都有唯一的 trace_id。
// 如果请求头中已有 X-Trace-ID（如网关传入）且通过白名单校验（长度 ≤ 64 且仅含 [A-Za-z0-9_-]），
// 则复用；否则生成新 UUID。
// trace_id 会写入 Gin Context（TraceIDKey）和响应头（TraceIDHeader）。
//
// 注册顺序建议先于 RequestLogger，但并非强依赖：c.Set() 写入 gin ctx，
// 后续任何中间件都能通过 c.Get() 读到。提前注册只是为了便于阅读与排查。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 中间件级 recover 兜底：理论上不会 panic，但 trace_id 链路是基础能力，
		// 不应因 uuid 库异常等导致整个请求失败。
		defer func() {
			if r := recover(); r != nil {
				utils.LogErrorT(uuid.NewString(), "Trace() panic recovered: %v", r)
				// panic 已捕获，确保请求继续
				if !c.Writer.Written() {
					c.Next()
				}
			}
		}()

		traceID := sanitizeTraceID(c.GetHeader(TraceIDHeader))
		if traceID == "" {
			traceID = uuid.NewString()
		}

		c.Set(TraceIDKey, traceID)
		c.Header(TraceIDHeader, traceID)

		c.Next()
	}
}

// GetTraceID 从 gin.Context 中获取 trace_id。
// 如果不存在，返回空字符串。
func GetTraceID(c *gin.Context) string {
	if tid, exists := c.Get(TraceIDKey); exists {
		if s, ok := tid.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// sanitizeTraceID 校验客户端传入的 X-Trace-ID：长度 ≤ MaxTraceIDLen 且仅含白名单字符。
// 不合规时返回空字符串（调用方会生成新 UUID），并在 debug 日志留痕。
func sanitizeTraceID(raw string) string {
	if raw == "" {
		return ""
	}
	if len(raw) > MaxTraceIDLen {
		utils.Debug("[trace] X-Trace-ID 超过最大长度 %d，已丢弃: len=%d", MaxTraceIDLen, len(raw))
		return ""
	}
	if !traceIDPattern.MatchString(raw) {
		utils.Debug("[trace] X-Trace-ID 含非法字符，已丢弃: %q", raw)
		return ""
	}
	return raw
}