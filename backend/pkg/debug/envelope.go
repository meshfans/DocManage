package debug

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"doc/pkg/license/crypto"
)

// Source 枚举：判定来源（用于日志 / 审计）。
//
// 所有合法值见下方常量定义。Outcome.Source 必须取这些值之一，
// 避免散落字符串硬编码（#10 修复）。
type Source string

const (
	// SourceAbsent config["debug"] 字段为空或密钥族不全。
	SourceAbsent Source = "absent"

	// SourceDecryptFailed 解密失败（AES-GCM Open / base64 / HKDF 错误）。
	SourceDecryptFailed Source = "decrypt_failed"

	// SourceDecodeFailed JSON 反序列化失败、字段缺失或时间格式错。
	SourceDecodeFailed Source = "decode_failed"

	// SourceBadWindow payload 合法但当前时间不在 [start, end] 内，或窗口非法。
	SourceBadWindow Source = "bad_window"

	// SourceActive 当前时间 ∈ [start, end]，debug 模式生效。
	SourceActive Source = "active"
)

// DebugConfig 启动期一次性判定所需的全部输入。
//
// 字段语义：
//   - AppID       : HKDF salt（与 license 共用 app_id，多应用隔离）
//   - Password    : HKDF ikm（= license payload.password，与机器强绑定）
//   - DebugEnvB64 : config.json["debug"] 顶层字段值（base64 envelope，可空）
//
// 注意：
//   - DebugEnvB64 为空 → 直接返回 Active=false 的 Outcome（不报错）
//   - AppID/Password 任一为空 → SourceAbsent，Active=false（不报错）
//   - Clock 为 nil → 自动降级为 SystemClock{}（main.go 不传即可）
type DebugConfig struct {
	AppID       string
	Password    string
	DebugEnvB64 string
	Clock       Clock // 可选：测试场景注入 mock；nil → SystemClock
}

// Outcome 是 ResolveDebugMode 的返回值。
//
// Source 字段取 SourceAbsent / SourceDecryptFailed / SourceDecodeFailed /
// SourceBadWindow / SourceActive 之一，便于日志聚合 + 单元测试断言。
//
// Active 含义：
//   - true  : 调用方应挂载 /debug/pprof/*
//   - false : 调用方不应挂载 pprof，等同关闭 debug
//
// Err 仅用于内部诊断（不打印到 info 日志）；本包所有失败均不 Fatal。
type Outcome struct {
	Active    bool
	StartTime time.Time
	EndTime   time.Time
	Issued    int64
	Source    Source
	Err       error
}

// HKDFInfo debug 信封 HKDF info，与 license 信封（"lmp-license-v1"）属不同密钥族。
//
// 取值 "lmp-client-request" 由 LMP 签发端协议决定——2026-09-23 用真实信封
// 穷举 6 种 base64/info/AAD 组合实证（仅此组合能解出明文），不可单方面修改。
const HKDFInfo = "lmp-client-request"

// errWindowNotInRange 内部错误哨兵（用于 errors.Is 判别）。
// 注意：故意不含时间戳数值，避免泄露 env 时间特征（#4 修复）。
var errWindowNotInRange = errors.New("debug envelope not in time window")

// ResolveDebugMode 启动期一次性判定 config["debug"] 字段是否启用 debug 模式。
//
// 行为：
//
//	字段为空      → Active=false, Source=SourceAbsent
//	AppID 空      → Active=false, Source=SourceAbsent
//	Password 空   → Active=false, Source=SourceAbsent（需 license 先于本函数成功解析）
//	解密失败      → Active=false, Source=SourceDecryptFailed, Err=<wrapped>
//	JSON 缺字段   → Active=false, Source=SourceDecodeFailed,  Err=<wrapped>
//	now < start   → Active=false, Source=SourceBadWindow
//	now > end     → Active=false, Source=SourceBadWindow
//	start>end     → Active=false, Source=SourceBadWindow
//	窗口合法      → Active=true,  Source=SourceActive
//
// 本函数不调用 log.Fatal；调用方根据 Outcome 决定是否挂载 pprof。
func ResolveDebugMode(cfg DebugConfig) *Outcome {
	// 1) 字段缺失：等价于未启用，不报错
	if cfg.DebugEnvB64 == "" {
		return &Outcome{Source: SourceAbsent}
	}
	if cfg.AppID == "" || cfg.Password == "" {
		// DebugEnvB64 有值但密钥族不全：判定为 absent（不报错，
		// 避免 license 未运行时误报 decrypt_failed）。
		return &Outcome{Source: SourceAbsent}
	}

	// 2) 信封解密（URL-safe base64 + HKDF + AES-256-GCM，AAD=nil）
	plaintext, err := openDebugEnvelope(cfg.DebugEnvB64, cfg.AppID, cfg.Password)
	if err != nil {
		return &Outcome{Source: SourceDecryptFailed, Err: fmt.Errorf("open debug envelope: %w", err)}
	}

	// 3) JSON 反序列化
	var payload DebugPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return &Outcome{Source: SourceDecodeFailed, Err: fmt.Errorf("decode payload JSON: %w", err)}
	}

	// 4) 解析时间窗口
	startTime, err := time.Parse(time.RFC3339, payload.StartTime)
	if err != nil {
		return &Outcome{Source: SourceDecodeFailed, Err: fmt.Errorf("parse starttime %q: %w", payload.StartTime, err)}
	}
	endTime, err := time.Parse(time.RFC3339, payload.EndTime)
	if err != nil {
		return &Outcome{Source: SourceDecodeFailed, Err: fmt.Errorf("parse endtime %q: %w", payload.EndTime, err)}
	}

	// 5) 窗口判定
	startUnix := startTime.Unix()
	endUnix := endTime.Unix()
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.NowUnix()
	if !IsInWindow(now, startUnix, endUnix) {
		// Err 不含具体时间戳，避免日志泄露 env 时间特征（#4 修复）。
		// 详细时间信息通过 StartTime / EndTime 字段返回，调用方按需 Debug 级别打印。
		return &Outcome{
			StartTime: startTime,
			EndTime:   endTime,
			Issued:    payload.Issued,
			Source:    SourceBadWindow,
			Err:       errWindowNotInRange,
		}
	}

	// 6) 命中窗口
	return &Outcome{
		Active:    true,
		StartTime: startTime,
		EndTime:   endTime,
		Issued:    payload.Issued,
		Source:    SourceActive,
	}
}

// openDebugEnvelope 解密 config["debug"] 信封，返回明文 JSON。
//
// 协议（与 LMP 签发端实证对齐，2026-09-23）：
//  1. base64 URL-safe 解码（URLEncoding；同时容忍无 padding 的 RawURL 形式）
//  2. envelope = nonce(12) || ciphertext || tag(16)
//  3. HKDF-SHA256(salt=appID, ikm=password, info=HKDFInfo, L=32) 派生 AES-256 key
//  4. AES-256-GCM Open，AAD=nil
//
// 不复用 license crypto.AesGcmDecrypt：后者硬编码 base64.StdEncoding 且强制
// 传入 AAD，与 debug 信封协议（URL-safe、无 AAD）不兼容。
func openDebugEnvelope(envelopeB64, appID, password string) ([]byte, error) {
	encoded := strings.TrimSpace(envelopeB64)

	// 1) URL-safe base64 解码。先按带 padding 的 URLEncoding，失败再尝试 RawURL。
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("base64 url-safe decode: %w", err)
		}
	}

	// 2) 长度校验：nonce(12) + 至少 1 字节密文 + tag(16)
	if len(raw) < crypto.AESNonceLen+crypto.AESTagLen+1 {
		return nil, fmt.Errorf("envelope too short: got %d bytes", len(raw))
	}

	// 3) HKDF 派生 AES-256 key
	key, err := crypto.DeriveAesKey(appID, password, HKDFInfo)
	if err != nil {
		return nil, fmt.Errorf("derive AES key: %w", err)
	}

	// 4) AES-GCM Open（AAD=nil）
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	nonce, ciphertext := raw[:crypto.AESNonceLen], raw[crypto.AESNonceLen:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM open: %w", err)
	}
	return plaintext, nil
}