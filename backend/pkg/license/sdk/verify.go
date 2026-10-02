package sdk

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"doc/pkg/license/crypto"
)

// VerifyLicense 完整的 license_key 校验流程：
//
//  1. 解析公钥 PEM（PKIX 或 PKCS1）
//  2. HKDF-SHA256 派生 AES key（salt=appID, ikm=machineCode, info="lmp-license-v1"）
//  3. Base64 解码 envelope，拆 nonce(12) + ct + tag(16)
//  4. AES-256-GCM Open（AAD = "lmp-license-v1"）
//  5. 拆分 plaintext = payload_json || rsa_signature（签名长度按公钥模数动态取）
//  6. RSA-PKCS#1 v1.5 + SHA-256 验签（**用原始 payload 字节**，test-go §4.2 坑 2）
//  7. JSON 反序列化 payload
//
// 返回 DecryptResult 和源错误。失败时 err 不为 nil；调用方根据错误决定后续动作
// （启动期 → log.Fatal；业务侧 → 返回业务错误）。
func VerifyLicense(cfg LicenseConfig) (*DecryptResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("license config invalid: %w", err)
	}

	// 1) 公钥解析（ResolvePublicKey 内部按 LICENSE_PUBKEY_PEM env > ./public.pem 解析）
	pemStr, source, err := ResolvePublicKey("", "")
	if err != nil {
		return nil, fmt.Errorf("resolve public key: %w", err)
	}
	pubKey, err := crypto.ParseRSAPublicKey(pemStr)
	if err != nil {
		return nil, fmt.Errorf("parse public key (source=%s): %w", source, err)
	}

	// 2) HKDF 派生 AES key
	aesKey, err := crypto.DeriveAesKey(cfg.AppID, cfg.MachineCode, crypto.HKDFInfo)
	if err != nil {
		return nil, fmt.Errorf("derive AES key: %w", err)
	}

	// 3) + 4) AES-GCM 解密
	plaintext, err := crypto.AesGcmDecrypt(cfg.LicenseKey, aesKey, crypto.HKDFInfo)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM decrypt: %w", err)
	}

	// 5) 拆分 payload + signature（签名长度按公钥动态取，避免 RSA-3072/4096 时失败）
	sigLen := pubKey.N.BitLen() / 8
	if len(plaintext) < sigLen {
		return nil, fmt.Errorf("plaintext too short: %d bytes, need >= %d (signature)",
			len(plaintext), sigLen)
	}
	dataBytes := plaintext[:len(plaintext)-sigLen]
	signature := plaintext[len(plaintext)-sigLen:]

	// 6) RSA 验签（用原始 payload 字节，不重新序列化）
	if err := crypto.RsaVerifyPayload(pubKey, dataBytes, signature); err != nil {
		return nil, fmt.Errorf("RSA verify (source=%s): %w", source, err)
	}

	// 7) JSON 反序列化 payload（这里 json.Unmarshal 是 OK 的，因为**验签已完成**，
	//    不再需要保留字节序；test-go §4.2 坑 2 仅约束「验签前不要重新序列化」）
	var payload LicensePayload
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode payload JSON: %w", err)
	}

	return &DecryptResult{
		Payload:      payload,
		SignatureB64: base64.StdEncoding.EncodeToString(signature),
		PubKeySource: source,
	}, nil
}

// ValidatePayload 校验 payload 字段一致性 + 时间窗口。
//
// 入参 cfg 用于 appID / machineCode 一致性校验；
// 入参 result 为 VerifyLicense 的成功返回。
//
// 返回 Status (VALID/EXPIRING_SOON/EXPIRED) + 一致性错误（如有）。
//
// 注意：本函数不会再次验签（已由 VerifyLicense 完成）。
func ValidatePayload(cfg LicenseConfig, result *DecryptResult) (Status, error) {
	if result == nil {
		return StatusExpired, errors.New("decrypt result is nil")
	}

	// appID 一致性
	if result.Payload.AppID != cfg.AppID {
		return StatusExpired, fmt.Errorf("app_id mismatch: payload=%q, local=%q",
			result.Payload.AppID, cfg.AppID)
	}

	// machineCode 一致性（硬件绑定）
	if result.Payload.MachineCode != cfg.MachineCode {
		return StatusExpired, fmt.Errorf("machine_code mismatch: payload=%q, local=%q",
			result.Payload.MachineCode, cfg.MachineCode)
	}

	// 时间窗口
	status := StatusOf(result.Payload.ExpiresAt)
	if status == StatusExpired {
		return status, fmt.Errorf("license expired at %d (now=%d)",
			result.Payload.ExpiresAt, nowUnix())
	}
	return status, nil
}

// nowUnix 返回当前 Unix 秒（抽出为函数变量便于测试注入）。
var nowUnix = func() int64 {
	return time.Now().Unix()
}