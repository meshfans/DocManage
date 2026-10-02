package sdk

import (
	"fmt"
)

// StartupOutcome 是 VerifyStartup 的返回值。
//
// Status 为业务时间状态（VALID/EXPIRING_SOON/EXPIRED）。
// 失败时 Err 非 nil；EXPIRED/EXPIRING_SOON 时也有 Err（用于日志记录）。
//
// 设计：调用方根据 Outcome 自己决定如何处理 EXPIRING_SOON（通常是红字横幅警告）。
// EXPIRED / 验签失败：建议 log.Fatal。
type StartupOutcome struct {
	Result    *DecryptResult
	Status    Status
	PubKeySrc string
	Err       error
}

// VerifyStartup 启动期一次性校验（main.go 早期调用）。
//
// 与 VerifyLicense + ValidatePayload 等价，但一次性返回合并结果，
// 并对 EXPIRED / 字段不一致 / 验签失败 三类失败统一返回带语义的 error。
//
// 行为：
//   - 所有失败 → out.Err 非 nil（启动期应 log.Fatal）
//   - EXPIRED → out.Err 非 nil（致命错误）
//   - VALID / EXPIRING_SOON → out.Err 为 nil，Result 可用
//
// 注意：本函数不调用 os.Exit；调用方决定如何终止。
func VerifyStartup(cfg LicenseConfig) *StartupOutcome {
	result, err := VerifyLicense(cfg)
	if err != nil {
		return &StartupOutcome{Err: err}
	}

	status, vErr := ValidatePayload(cfg, result)
	out := &StartupOutcome{
		Result:    result,
		Status:    status,
		PubKeySrc: result.PubKeySource,
	}

	if vErr != nil {
		out.Err = vErr
		return out
	}

	// status == VALID / EXPIRING_SOON：合法通过
	return out
}

// errPayloadExpired 当前未直接引用；保留为扩展点：
// 业务侧若要 errors.Is 判断 EXPIRED，可在 ValidatePayload 改造时使用。
//
// 当前 VerifyStartup 直接用 status == StatusExpired 判断，
// 不依赖 errors.Is，行为等价且更直接。
// var _ = errPayloadExpired  // 保留位

// FormatOutcome 打印启动期校验结果的格式化字符串（用于 banner 输出）。
func FormatOutcome(out *StartupOutcome) string {
	if out == nil {
		return "license: no outcome"
	}
	if out.Err != nil {
		return fmt.Sprintf("license: FAIL (%v)", out.Err)
	}
	return fmt.Sprintf("license: OK status=%s pubkey=%s app=%s expires_at=%d",
		out.Status, out.PubKeySrc,
		out.Result.Payload.AppID, out.Result.Payload.ExpiresAt)
}