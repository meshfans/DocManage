package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
)

// AES-GCM 协议常量（与 HKDF / 签发端一致）。
//
// 来源：test-go reference #1 §3.3。
const (
	// AESNonceLen GCM nonce 长度（字节）。
	AESNonceLen = 12

	// AESKeyLen AES 密钥长度（AES-256 → 32 字节）。
	AESKeyLen = 32

	// AESTagLen GCM tag 长度（字节）。
	AESTagLen = 16

	// EnvelopeMinLen envelope 最小长度（12 nonce + 0 ct + 16 tag）。
	EnvelopeMinLen = AESNonceLen + AESTagLen

	// EnvelopeMaxLen envelope 上限（防止恶意超大 base64）。
	// 256 KiB 远大于任何合法 license envelope（实际 < 8 KiB）。
	EnvelopeMaxLen = 256 * 1024
)

// AesGcmDecrypt 解 AES-256-GCM envelope。
//
// 入参 envelopeB64：base64.StdEncoding 编码的字节序列
//   - layout: nonce(12) || ciphertext || tag(16)
//
// 入参 key：AES-256 key（32 字节，建议由 DeriveAesKey 派生）
// 入参 aad：associated data（必须与 HKDF info 同值，即 "lmp-license-v1"）
//
// 错误：
//   - base64 decode 失败：license_key 被截断 / padding 缺失
//   - envelope 长度越界：同上
//   - AAD 不匹配 / 密文损坏：AES-GCM Open 返回错误
//
// 来源：test-go reference #1 §3.3 + §3.4 错误对照表。
func AesGcmDecrypt(envelopeB64 string, key []byte, aad string) ([]byte, error) {
	if len(key) != AESKeyLen {
		return nil, fmt.Errorf("aes key length=%d, expected %d", len(key), AESKeyLen)
	}

	envelope, err := base64.StdEncoding.DecodeString(envelopeB64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	if len(envelope) < EnvelopeMinLen {
		return nil, fmt.Errorf("envelope too short: %d bytes, min=%d", len(envelope), EnvelopeMinLen)
	}
	if len(envelope) > EnvelopeMaxLen {
		return nil, fmt.Errorf("envelope too large: %d bytes, max=%d", len(envelope), EnvelopeMaxLen)
	}

	nonce := envelope[:AESNonceLen]
	ctWithTag := envelope[AESNonceLen:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher failed: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes new gcm failed: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ctWithTag, []byte(aad))
	if err != nil {
		// AAD 不匹配 / machine_code 错 / 密文损坏 / tag 校验失败都会落到这里。
		// 统一 wrap，调用方按需分类。
		return nil, fmt.Errorf("aes-gcm open failed (likely AAD/machineCode mismatch): %w", err)
	}

	return plaintext, nil
}