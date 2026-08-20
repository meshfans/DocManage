package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestValidateToken_HappyPath 正常流程：签发 → 校验通过。
func TestValidateToken_HappyPath(t *testing.T) {
	j := NewJWTUtils("test-secret", time.Minute, time.Hour)
	tok, _, err := j.GenerateAccessToken(7, "alice")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := j.ValidateToken(tok)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if claims.UserID != 7 || claims.Username != "alice" {
		t.Fatalf("claims 字段错: %+v", claims)
	}
	if claims.Issuer != jwtIssuer {
		t.Fatalf("Issuer 应为 %s，实际 %s", jwtIssuer, claims.Issuer)
	}
}

// TestValidateToken_InvalidIssuer 伪造 iss → 拒绝。
//
// Phase 4a (High #10)：校验 Issuer 防伪造。
func TestValidateToken_InvalidIssuer(t *testing.T) {
	j := NewJWTUtils("test-secret", time.Minute, time.Hour)

	// 自己签一个 iss 错误的 token（用同样 secret）
	claims := &Claims{
		UserID:    99,
		Username:  "mallory",
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "evil-issuer",
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(j.secret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	_, err = j.ValidateToken(tok)
	if err == nil {
		t.Fatal("iss 不匹配应被拒绝，实际通过")
	}
	if err.Error() != "invalid issuer" {
		t.Fatalf("应返 'invalid issuer'，实际: %v", err)
	}
}

// TestValidateToken_MissingIssuer 老 token 无 iss → 拒绝（升级瞬间所有老 token 失效）。
func TestValidateToken_MissingIssuer(t *testing.T) {
	j := NewJWTUtils("test-secret", time.Minute, time.Hour)

	// iss = ""（模拟升级前的旧 token）
	claims := &Claims{
		UserID:    99,
		Username:  "alice",
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "",
		},
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(j.secret)

	_, err := j.ValidateToken(tok)
	if err == nil {
		t.Fatal("iss 缺失应被拒绝，实际通过")
	}
}

// TestValidateToken_WrongSecret 不同 secret 签的 token → 拒绝。
func TestValidateToken_WrongSecret(t *testing.T) {
	signer := NewJWTUtils("secret-A", time.Minute, time.Hour)
	verifier := NewJWTUtils("secret-B", time.Minute, time.Hour)

	tok, _, _ := signer.GenerateAccessToken(7, "alice")
	_, err := verifier.ValidateToken(tok)
	if err == nil {
		t.Fatal("不同 secret 应被拒绝")
	}
}

// TestValidateToken_Expired 已过期 token → 拒绝。
func TestValidateToken_Expired(t *testing.T) {
	j := NewJWTUtils("test-secret", -time.Minute, -time.Hour) // 立刻过期
	tok, _, _ := j.GenerateAccessToken(7, "alice")
	_, err := j.ValidateToken(tok)
	if err == nil {
		t.Fatal("过期 token 应被拒绝")
	}
}
