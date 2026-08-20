package utils

import (
	"strings"
	"testing"
)

// TestTokenFingerprint_Deterministic 验证 TokenFingerprint 对同一 token 返
// 回相同指纹（用于审计日志 dedupe）。
func TestTokenFingerprint_Deterministic(t *testing.T) {
	tok := "eyJhbGciOiJIUzUxMiJ9.payload.signature"
	fp1 := TokenFingerprint(tok)
	fp2 := TokenFingerprint(tok)
	if fp1 != fp2 {
		t.Errorf("TokenFingerprint 应确定性: fp1=%s fp2=%s", fp1, fp2)
	}
	// 12 hex chars = 6 bytes
	if len(fp1) != 12 {
		t.Errorf("TokenFingerprint 应 12 hex 字符 (6 bytes), 实际 %d: %s", len(fp1), fp1)
	}
}

// TestTokenFingerprint_DifferentTokens 验证不同 token 指纹不同（无碰撞）。
func TestTokenFingerprint_DifferentTokens(t *testing.T) {
	tok1 := "eyJhbGciOiJIUzUxMiJ9.payload1.sig1"
	tok2 := "eyJhbGciOiJIUzUxMiJ9.payload2.sig2"
	fp1 := TokenFingerprint(tok1)
	fp2 := TokenFingerprint(tok2)
	if fp1 == fp2 {
		t.Errorf("不同 token 不应碰撞: fp1=%s fp2=%s", fp1, fp2)
	}
}

// TestTokenFingerprint_NoPrefixBleed 验证 TokenFingerprint 是 raw token 的指纹，
// 不受 "Bearer " 前缀影响（auth.go 的 wrapper 负责剥前缀）。
func TestTokenFingerprint_NoPrefixBleed(t *testing.T) {
	raw := "eyJhbGciOiJIUzUxMiJ9.payload.sig"
	withPrefix := "Bearer " + raw
	// TokenFingerprint 直接吃 raw token；wrapper 决定要不要剥前缀
	fpRaw := TokenFingerprint(raw)
	fpWith := TokenFingerprint(withPrefix)
	if fpRaw == fpWith {
		t.Errorf("带 Bearer 前缀会改变指纹: raw=%s with=%s", fpRaw, fpWith)
	}
}

// TestIsTokenRevoked_NotRevoked 验证未吊销 token 返 false。
func TestIsTokenRevoked_NotRevoked(t *testing.T) {
	tok := "not-revoked-token-" + t.Name()
	if IsTokenRevoked(tok) {
		t.Errorf("未吊销 token 应返 false")
	}
}

// TestIsTokenRevoked_AfterRevoke 验证 RevokeToken 后 IsTokenRevoked 返 true。
//
// 🛠 BUG-1 修复验证（2026-08-20）：J.5 Refresh Token Rotation 闭环核心证据。
func TestIsTokenRevoked_AfterRevoke(t *testing.T) {
	tok := "revoked-token-bug1-" + t.Name()
	RevokeToken(tok, 0) // 0 = 默认 24h 后过期
	if !IsTokenRevoked(tok) {
		t.Errorf("RevokeToken 后 IsTokenRevoked 必须返 true")
	}
}

// TestIsTokenRevoked_DifferentTokensIndependent 验证黑名单按 token 隔离。
func TestIsTokenRevoked_DifferentTokensIndependent(t *testing.T) {
	tok1 := "isolated-tok-1-" + t.Name()
	tok2 := "isolated-tok-2-" + t.Name()
	RevokeToken(tok1, 0)
	if !IsTokenRevoked(tok1) {
		t.Errorf("tok1 应被吊销")
	}
	if IsTokenRevoked(tok2) {
		t.Errorf("tok2 不应被吊销")
	}
}

// TestRevokeToken_EmptyNoop 验证空 token 是 no-op（不污染黑名单）。
func TestRevokeToken_EmptyNoop(t *testing.T) {
	RevokeToken("", 0)
	if IsTokenRevoked("") {
		t.Errorf("空 token 不应被吊销")
	}
}

// TestIsTokenRevoked_ExpiredEntry 验证过期黑名单项自动失效。
//
// 流程：RevokeToken 写入 → 立即读应 revoked → 改 expired 时间戳 → 再读应 NOT revoked。
//
// 🛠 BUG-1 修复验证：黑名单 TTL = token 原始 expires，过期后 IsTokenRevoked
// 自动放行（与 LoadAndDelete 原子路径一致）。
func TestIsTokenRevoked_ExpiredEntry(t *testing.T) {
	tok := "expiry-test-tok-" + t.Name()
	// 写入已过期的黑名单项（expiresAt = 1，即 1970-01-01）
	RevokeToken(tok, 1)
	// IsTokenRevoked 内部 LoadAndDelete 看到已过期 → 视作未吊销
	if IsTokenRevoked(tok) {
		t.Errorf("已过期的黑名单项应被 IsTokenRevoked 自动放行")
	}
}

// TestTokenFingerprint_NotEmptyForValidInput 烟雾测试：合法 token 返非空指纹。
func TestTokenFingerprint_NotEmptyForValidInput(t *testing.T) {
	fp := TokenFingerprint("some-real-token")
	if fp == "" {
		t.Errorf("合法 token 应返非空指纹")
	}
	if strings.ContainsAny(fp, "ghijklmnopqrstuvwxyzGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("指纹应纯 hex 字符, 实际: %s", fp)
	}
}
