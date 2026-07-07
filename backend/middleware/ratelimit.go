package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"doc/utils"
)

// P0 修复（2026-06-28）：登录 / 改密速率限制。
//
// 实现要点：
//   - 进程内内存令牌桶（无外部依赖，简单可靠）
//   - 失败计数器：同 IP + endpoint 连续失败 N 次 → 锁定 M 分钟
//   - 成功调用清零计数（避免正常用户被误锁）
//   - 单机部署（README：局域网单实例），无需 Redis 集群
//
// 设计取舍：
//   - 用 sliding window + 失败计数（更符合"防爆破"语义，比单纯 QPS 限流更安全）
//   - 默认：5 次失败 → 锁定 10 分钟；冷却期内所有请求都返回 429
//   - 白名单：localhost（避免运维脚本被锁）
//
// 为什么不直接用 golang.org/x/time/rate：
//   - 那是 QPS 限流，无法区分"失败 vs 成功"
//   - 防爆破需要"失败 N 次锁 M 分钟"的语义
//
// 已知限制：
//   - 多实例部署时每个实例独立计数（边界情况，单机产品可接受）
//   - 重启后计数清零（可接受，攻击窗口从"24h"缩短为"重启周期"）

// RateLimitEntry 单个 (key, endpoint) 的失败计数与锁定状态。
type RateLimitEntry struct {
	mu              sync.Mutex
	failCount       int
	lockedUntil     time.Time
	lastFailAt      time.Time
}

func (e *RateLimitEntry) isLocked(now time.Time) bool {
	return now.Before(e.lockedUntil)
}

func (e *RateLimitEntry) recordFailure(now time.Time, maxFails int, lockDuration time.Duration) {
	e.failCount++
	e.lastFailAt = now
	if e.failCount >= maxFails {
		e.lockedUntil = now.Add(lockDuration)
	}
}

func (e *RateLimitEntry) recordSuccess() {
	e.failCount = 0
	e.lockedUntil = time.Time{}
}

// rateLimitStore 全局 (key, endpoint) → entry 映射。
// 简单内存存储，定期清理过期 entry 避免内存泄漏。
var rateLimitStore sync.Map

func getOrCreateRateLimitEntry(key, endpoint string) *RateLimitEntry {
	k := endpoint + "|" + key
	if v, ok := rateLimitStore.Load(k); ok {
		return v.(*RateLimitEntry)
	}
	entry := &RateLimitEntry{}
	actual, _ := rateLimitStore.LoadOrStore(k, entry)
	return actual.(*RateLimitEntry)
}

// rateLimitConfig 速率限制配置（可在 Init 中调整）。
type rateLimitConfig struct {
	maxFails     int           // 触发锁定的连续失败次数
	lockDuration time.Duration // 锁定时长
}

var (
	loginRateLimit    = rateLimitConfig{maxFails: 5, lockDuration: 10 * time.Minute}
	changePwdLimit    = rateLimitConfig{maxFails: 5, lockDuration: 10 * time.Minute}
	refreshTokenLimit = rateLimitConfig{maxFails: 10, lockDuration: 10 * time.Minute} // 阈值稍高，refresh 是合法高频操作
)

// LoginRateLimit 登录端点速率限制。
// 失败 = 用户名/密码错误；成功 = 校验通过。
// 用法：r.POST("/login", middleware.LoginRateLimit(), handler.Login)
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		applyRateLimit(c, "login", loginRateLimit)
	}
}

// ChangePasswordRateLimit 改密端点速率限制。
// 失败 = 旧密码错误 / 新密码强度不足；成功 = 改密完成。
func ChangePasswordRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		applyRateLimit(c, "change-password", changePwdLimit)
	}
}

// RefreshTokenRateLimit 刷新 token 端点速率限制。
// P1 修复（2026-06-28）：防 token 枚举攻击。
//   - 阈值比 login 略高（10 次而非 5 次），因为合法用户可能因 access token 过期而多次 refresh
//   - 失败 = refresh token 无效 / 已过期 / 用户已禁用
//   - 成功 = 签发新的 access+refresh token
func RefreshTokenRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		applyRateLimit(c, "refresh-token", refreshTokenLimit)
	}
}

func applyRateLimit(c *gin.Context, endpoint string, cfg rateLimitConfig) {
	ip := c.ClientIP()
	// 白名单：localhost
	if ip == "127.0.0.1" || ip == "::1" {
		c.Next()
		return
	}

	entry := getOrCreateRateLimitEntry(ip, endpoint)
	entry.mu.Lock()
	now := time.Now()
	if entry.isLocked(now) {
		retryAfter := int(time.Until(entry.lockedUntil).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		entry.mu.Unlock()
		c.Header("Retry-After", itoa(retryAfter))
		utils.LogError("[RateLimit] 锁定期内拒绝: endpoint=%s, ip=%s, retry_after=%ds",
			endpoint, ip, retryAfter)
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"success":     false,
			"code":        "RATE_LIMITED",
			"message":     "请求过于频繁，请稍后再试",
			"retry_after": retryAfter,
		})
		return
	}
	entry.mu.Unlock()

	// 用 callback 让 handler 报告成功/失败
	c.Set("__ratelimit_entry", entry)
	c.Set("__ratelimit_cfg", cfg)
	c.Next()

	// handler 通过 c.Set("__ratelimit_result", "success"/"failure") 标记结果
	if v, ok := c.Get("__ratelimit_result"); ok {
		result, _ := v.(string)
		entry.mu.Lock()
		switch result {
		case "success":
			entry.recordSuccess()
		case "failure":
			entry.recordFailure(time.Now(), cfg.maxFails, cfg.lockDuration)
			if entry.isLocked(time.Now()) {
				utils.LogError("[RateLimit] 触发锁定: endpoint=%s, ip=%s, fails=%d, lock_until=%s",
					endpoint, ip, entry.failCount, entry.lockedUntil.Format(time.RFC3339))
			}
		}
		entry.mu.Unlock()
	}
}

// itoa 小工具，避免引入 strconv 引用膨胀。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// startRateLimitGC 定期清理过期 entry（每 30 分钟清理锁定已过期的 entry）。
// 启动方式：main.go 调一次 startRateLimitGC()。
func startRateLimitGC() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			rateLimitStore.Range(func(k, v interface{}) bool {
				e := v.(*RateLimitEntry)
				e.mu.Lock()
				// 锁定已过期 且 距上次失败 > 1 小时 → 可清理
				if !e.isLocked(now) && now.Sub(e.lastFailAt) > time.Hour {
					rateLimitStore.Delete(k)
				}
				e.mu.Unlock()
				return true
			})
		}
	}()
}

// InitRateLimitGC 在 main 启动时调用，启动后台 GC goroutine。
func InitRateLimitGC() {
	startRateLimitGC()
}