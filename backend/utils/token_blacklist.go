package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// P0 修复（2026-06-28）：JWT 吊销机制（内存版 token 黑名单）。
//
// 背景：
//   - 历史 JWT 签发后无法吊销（除等待 24h 自然过期）
//   - admin 撤销某用户角色后，旧 JWT 仍可访问直到 24h 过期
//   - 用户 Logout 后 token 仍可被复用（前端已清，但 token 本体未失效）
//
// 实现要点：
//   - 进程内 sync.Map 存 hash(token) → 过期时间
//   - JWT 校验时同步检查是否在黑名单
//   - 黑名单项超过 token 原过期时间后自动失效（后台 GC 清理）
//   - 单实例部署够用（README：局域网单实例）；多实例需换 Redis
//
// 为什么不存 DB：
//   - 每个 API 请求都要查黑名单 → DB 查询延迟不可接受
//   - 黑名单与 token 同生命周期（最长 7d refresh），内存足够
//
// 为什么不直接用 jti（JWT ID）：
//   - 当前 JWTUtils 不签 jti claim
//   - 加 jti 需改签名路径，影响所有 token
//   - 用 SHA256(token) 作为 key 足够唯一且无碰撞风险

var (
	blacklist     sync.Map
	blacklistOnce sync.Once
)

// RevokeToken 把 token 加入黑名单，过期时间取 token 自身的 expires_at。
//
// token: 完整 JWT 字符串（不含 "Bearer " 前缀）
// expiresAt: token 的 expires_at 时间戳（unix seconds）；0 表示默认 24h 后过期
func RevokeToken(token string, expiresAt int64) {
	if token == "" {
		return
	}
	h := hashToken(token)
	exp := expiresAt
	if exp <= 0 {
		exp = time.Now().Add(24 * time.Hour).Unix()
	}
	blacklist.Store(h, exp)
}

// IsTokenRevoked 检查 token 是否在黑名单。
// 返回 true 表示已吊销，请求应拒绝。
//
// P1 修复（2026-06-28）：用 LoadAndDelete 原子替换原 Load + Delete 模式，
// 避免 Load→检查→Delete 期间另一 goroutine 写入新条目被误删。
func IsTokenRevoked(token string) bool {
	if token == "" {
		return false
	}
	h := hashToken(token)
	// 原子 load + delete：如果存在且过期，删除后视作未吊销
	// 如果存在且未过期，把值放回（避免误删有效的黑名单项）
	if v, loaded := blacklist.LoadAndDelete(h); loaded {
		exp, _ := v.(int64)
		if time.Now().Unix() < exp {
			// 未过期：放回（Store 是原子操作，不会丢失并发写入）
			blacklist.Store(h, exp)
			return true
		}
		// 已过期：已删除，OK
	}
	return false
}

// StartBlacklistGC 启动后台 GC，每小时清理一次过期黑名单项。
func StartBlacklistGC() {
	blacklistOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for now := range ticker.C {
				blacklist.Range(func(k, v interface{}) bool {
					if exp, ok := v.(int64); ok && now.Unix() >= exp {
						blacklist.Delete(k)
					}
					return true
				})
			}
		}()
	})
}

// hashToken 计算 token 的 SHA-256 哈希（作为黑名单 key）。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
