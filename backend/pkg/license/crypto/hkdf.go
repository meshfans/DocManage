package crypto

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

// 协议常量（与 LMP 后端签发端严格保持一致，改名必须前后端同时升级）。
//
// 来源：test-go reference #1 (main.go: deriveAESKey, §3.2)。
const (
	// HKDFInfo HKDF info 参数。同一字符串也用作 AES-GCM 的 AAD。
	// 协议常量，前后端必须一致。
	HKDFInfo = "lmp-license-v1"

	// DebugHKDFInfo debug envelope 专用 HKDF info。
	//
	// 与 license 信用的 HKDFInfo 不同（debug 信封与 license 信封密码族独立，
	// 互不污染 AAD）。仅诊断工具（cmd/license_dump -debug-b64）使用。
	// 协议常量，前后端必须一致。
	DebugHKDFInfo = "lmp-client-request"

	// HKDFOutputLen AES-256 密钥长度（字节）。
	HKDFOutputLen = 32
)

// DeriveAesKey 用 HKDF-SHA256 派生 AES-256 key。
//
// 参数：
//   - appID       : HKDF salt（多应用隔离）
//   - machineCode : HKDF IKM（硬件绑定）
//   - info        : HKDF info（推荐传 HKDFInfo 常量）
//
// 5 个参数（与 RFC 5869 / test-go §3.2 一一对应）：
//
//	hash         = SHA256
//	salt         = app_id (UTF-8 bytes)
//	ikm          = machine_code (UTF-8 bytes)
//	info         = "lmp-license-v1"
//	output length = 32 bytes
//
// 失败场景：HKDF 实现错误（理论上不会发生；发生时报告 SDK bug）。
func DeriveAesKey(appID, machineCode, info string) ([]byte, error) {
	if appID == "" {
		return nil, errors.New("appID is required for HKDF salt")
	}
	if machineCode == "" {
		return nil, errors.New("machineCode is required for HKDF IKM")
	}
	if info == "" {
		return nil, errors.New("info is required for HKDF")
	}

	reader := hkdf.New(sha256.New, []byte(machineCode), []byte(appID), []byte(info))
	key := make([]byte, HKDFOutputLen)
	if _, err := reader.Read(key); err != nil {
		return nil, fmt.Errorf("hkdf read failed: %w", err)
	}
	return key, nil
}