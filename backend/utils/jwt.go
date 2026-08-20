package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
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

func (j *JWTUtils) GenerateAccessToken(userID int64, username string) (string, error) {
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.accessExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	return token.SignedString(j.secret)
}

func (j *JWTUtils) GenerateRefreshToken(userID int64, username string) (string, error) {
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.refreshExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	return token.SignedString(j.secret)
}

func (j *JWTUtils) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
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
