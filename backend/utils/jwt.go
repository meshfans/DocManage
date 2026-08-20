package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

// newJTI J.7：每个 token 唯一 ID（128 bit 随机），用于：
//   - 多副本部署时 Redis blacklist 的 key（当前仍是进程内）
//   - audit log 关联具体 token（防 replay / 复用分析）
// 不存 DB，仅作为 JWT claim 字段，校验时不强制检查（仅签发时设置）。
func newJTI() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵不足时退化为时间戳 + username（不阻塞登录）
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// jwtIssuer 本服务签发的 token 唯一 Issuer（Phase 4a Critical #10）。
// 生成与校验必须使用同一字符串，避免伪造 / 不同服务的 token 互用。
const jwtIssuer = "doc-server"

type JWTUtils struct {
	secret        []byte
	accessExpire  time.Duration
	refreshExpire time.Duration
}

func NewJWTUtils(secret string, accessExpire, refreshExpire time.Duration) *JWTUtils {
	return &JWTUtils{
		secret:        []byte(secret),
		accessExpire:  accessExpire,
		refreshExpire: refreshExpire,
	}
}

func NewJWTUtilsFromConfig(secret string, accessExpireStr, refreshExpireStr string) (*JWTUtils, error) {
	accessExpire, err := time.ParseDuration(accessExpireStr)
	if err != nil {
		accessExpire = 24 * time.Hour
	}

	refreshExpire, err := time.ParseDuration(refreshExpireStr)
	if err != nil {
		refreshExpire = 7 * 24 * time.Hour
	}

	return &JWTUtils{
		secret:        []byte(secret),
		accessExpire:  accessExpire,
		refreshExpire: refreshExpire,
	}, nil
}

func (j *JWTUtils) GenerateAccessToken(userID int64, username string) (string, time.Time, error) {
	expAt := time.Now().Add(j.accessExpire)
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    jwtIssuer,
			ID:        newJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := token.SignedString(j.secret)
	return signed, expAt, err
}

func (j *JWTUtils) GenerateRefreshToken(userID int64, username string) (string, time.Time, error) {
	expAt := time.Now().Add(j.refreshExpire)
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    jwtIssuer,
			ID:        newJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := token.SignedString(j.secret)
	return signed, expAt, err
}

// jwtParseLeeway J.4：clock skew 容差（30s）。RFC 7519 §4.1.4 推荐 ±30s。
// 解决 NTP 不同步 / 跨时区部署导致的"刚签发就过期"问题。
const jwtParseLeeway = 30 * time.Second

func (j *JWTUtils) ValidateToken(tokenString string) (*Claims, error) {
	parser := jwt.NewParser(jwt.WithLeeway(jwtParseLeeway))
	token, err := parser.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// J.1：算法白名单仅接受 HS512，拒绝 HS256/HS384 + 任何 RSA/EC/None。
		// 防"alg=none"攻击 + 算法强度降级攻击（HS512 secret 拿去做 HS256 校验）。
		if token.Method.Alg() != jwt.SigningMethodHS512.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return j.secret, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Phase 4a (High #10)：校验 Issuer = "doc-server"（与生成端对齐）。
	// 防伪造：攻击者若拿到 secret 之前的 token（无 iss）→ 拒绝；不同服务的 token → 拒绝。
	if claims.Issuer != jwtIssuer {
		return nil, errors.New("invalid issuer")
	}

	return claims, nil
}

// GetExpirationTime J.3：保留作为 fallback，但**不推荐**用于响应包络的 expires 字段。
//
// 真实 token expires_at 应由 GenerateAccessToken 第二返回值给出。
// 此方法仅供"无 token 上下文"场景（如 GetUserInfo 演示）。
//
// Deprecated: 改用 GenerateAccessToken 的 expAt 返回值。
func (j *JWTUtils) GetExpirationTime() time.Time {
	return time.Now().Add(j.accessExpire)
}

// GetAccessExpire 返回 access token 的有效期（P0 修复 2026-06-28：供 token 吊销时计算黑名单 TTL）。
func (j *JWTUtils) GetAccessExpire() time.Duration {
	return j.accessExpire
}

// GetRefreshExpire 返回 refresh token 的有效期（同上）。
func (j *JWTUtils) GetRefreshExpire() time.Duration {
	return j.refreshExpire
}
