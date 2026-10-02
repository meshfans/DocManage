// Package crypto 提供 LMP License 解密 / 验签所需的密码学原语。
//
// 设计目标：
//   - 与 test-go 参考实现 + LMP 后端签发端协议完全一致
//   - 零外部依赖（只用标准库 + golang.org/x/crypto）
//   - 关键协议常量（HKDF info、AAD、envelope 长度）以包级 const 暴露，
//     改名必须前后端同步升级
//
// 本文件：RSA 公钥 PEM 解析。
//
// 支持两种 PEM 格式：
//   - PKIX (SPKI)：-----BEGIN PUBLIC KEY-----
//   - PKCS1     ：-----BEGIN RSA PUBLIC KEY-----
//
// 来源：test-go reference #1 (main.go: parseRSAPublicKey)。
package crypto

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// pemBeginMarker PEM 起始标记前缀（用于 looksLikePEM 启发式判断）。
const pemBeginMarker = "-----BEGIN "

// looksLikePEM 简单判断字符串是否为 PEM 格式。
//
// 仅检查起始标记，不验证解析。用于配置加载时的快速失败。
func looksLikePEM(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), pemBeginMarker)
}

// ParseRSAPublicKey 从 PEM 字符串解析 RSA 公钥。
//
// 优先尝试 PKIX ("PUBLIC KEY")；失败回退 PKCS1 ("RSA PUBLIC KEY")。
//
// 返回的错误信息会包含 PEM 头提示，便于定位问题。
func ParseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	if pemStr == "" {
		return nil, errors.New("pem string is empty")
	}

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	// 优先 PKIX
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := pub.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		// PKIX 解出但不是 RSA（如 ECDSA）→ 报错
		return nil, fmt.Errorf("PKIX public key is not RSA (type=%T)", pub)
	}

	// 回退 PKCS1
	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}

	return nil, fmt.Errorf("failed to parse RSA public key (PEM type=%q)", block.Type)
}