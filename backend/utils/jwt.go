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
			Issuer:    "doc-server",
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
			Issuer:    "doc-server",
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

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
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
