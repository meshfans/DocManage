package crypto

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
)

// RSA 协议常量。
//
// 来源：test-go reference #1 §4.1。
const (
	// RSAKeyBits 当前实现假设的 RSA 模长（位）。仅用于命名空间，无强制约束。
	RSAKeyBits = 2048

	// RSASigLen2048 RSA-2048 签名长度（字节）。
	RSASigLen2048 = RSAKeyBits / 8
)

// RsaVerifyPayload 使用 RSA-PKCS#1 v1.5 + SHA-256 验签。
//
// 入参：
//   - pubKey  : RSA 公钥
//   - data    : 待验签的**原始 payload 字节**（重要：不要重新序列化 JSON，
//               test-go §4.2 坑 2）
//   - sig     : RSA 签名
//
// 签名长度动态取自 pubKey.N.BitLen()/8，避免 RSA-3072/4096 时长度不对（test-go §4.2 坑 1）。
//
// 错误：nil 表示验签通过；非 nil 表示签名无效或参数错。
func RsaVerifyPayload(pubKey *rsa.PublicKey, data, sig []byte) error {
	if pubKey == nil {
		return errors.New("pubKey is nil")
	}
	if len(data) == 0 {
		return errors.New("data is empty")
	}
	if len(sig) == 0 {
		return errors.New("signature is empty")
	}

	// 动态签名长度检查（防止 RSA-2048 签名的字节流被当作 RSA-4096 验签）
	expectedSigLen := pubKey.N.BitLen() / 8
	if len(sig) != expectedSigLen {
		return fmt.Errorf("signature length=%d, expected %d (key=%d bits)",
			len(sig), expectedSigLen, pubKey.N.BitLen())
	}

	hashed := sha256.Sum256(data)
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], sig); err != nil {
		return fmt.Errorf("rsa pkcs1v15 verify failed: %w", err)
	}
	return nil
}