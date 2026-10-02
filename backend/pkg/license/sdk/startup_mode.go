package sdk

import (
	"errors"

	"doc/utils"
)

// ErrLicenseKeyMissing 表示配置中未提供 license_key。
//
// 2026-09-29 硬切：license 为空不再放行启动，改为与其它无效场景同等的致命错误。
// 历史行为（空 key 跳过校验、返回 nil outcome）已下线。
var ErrLicenseKeyMissing = errors.New("license.license_key is empty")

// resolveStartup 启动期 license 校验的**纯函数**实现：执行完整校验并返回合并结果。
//
// 不做任何进程终止，因此可被单元测试直接覆盖（utils.Fatal 内部调 os.Exit，
// 早前的 Fatal 分支只能 t.Skip，见 startup_mode_test.go）。
//
// 失败分类（全部返回 error，均为致命）：
//   - LicenseKey 为空            → ErrLicenseKeyMissing
//   - 配置字段缺失（app_id 等）   → LicenseConfig.Validate
//   - 公钥解析失败               → ResolvePublicKey
//   - AES-GCM 解密失败（key 损坏 / 换机）→ AesGcmDecrypt
//   - RSA 验签失败（被篡改 / 非本方签发）→ RsaVerifyPayload
//   - app_id / machine_code 不一致 → ValidatePayload
//   - 已过期（now > expires_at）  → ValidatePayload
//
// 唯一非致命情形是 EXPIRING_SOON（剩余 < 7 天），返回 outcome 且 error 为 nil。
func resolveStartup(cfg LicenseConfig) (*StartupOutcome, error) {
	if cfg.LicenseKey == "" {
		return nil, ErrLicenseKeyMissing
	}
	out := VerifyStartup(cfg)
	if out.Err != nil {
		return out, out.Err
	}
	return out, nil
}

// VerifyStartupOrLog 启动期 license 校验入口（main.go 在数据库初始化前调用）。
//
// ⚠️ Breaking Change（2026-09-23）：返回值由 `error` 扩展为 `(*StartupOutcome, error)`。
// 背景：pkg/debug 需要读取 payload.password 作为 HKDF ikm（debug 信封与 license 同密码族）。
// 影响：仅 main.go 一个调用方，已同步更新；外部第三方集成如有依赖旧签名需适配。
//
// ⚠️ Breaking Change（2026-09-29 硬切）：**任何无效 license 一律拒绝启动**。
// 此前 LicenseKey 为空会跳过校验并放行（兼容历史行为），现已下线。
//
// 启用规则（无例外、无开关）：
//   - LicenseKey 为空          → log.Fatal（进程退出）
//   - 任一校验环节失败          → log.Fatal（进程退出）
//   - EXPIRING_SOON（剩余 < 7 天）→ 放行，仅 warn
//   - 其余（VALID）             → 放行，info
//
// 公钥来源由 ResolvePublicKey 解析（无需额外配置）：
//   - LICENSE_PUBKEY_PEM env > --pub-key/--pubkey/--pubkey-pem CLI flag > ./public.pem
//
// 返回值：
//   - 校验成功 → 返回 (*StartupOutcome, nil)，调用方可读取 Payload.Password 等字段
//   - 校验失败 → log.Fatal（os.Exit(1)）后不返回
//
// 由于调用方永远拿不到 nil outcome，sdk.GlobalOutcome() 在进程存活期内恒非 nil。
//
// 调用方：backend/main.go 在配置加载后、数据库初始化前调用。
func VerifyStartupOrLog(cfg LicenseConfig) (*StartupOutcome, error) {
	out, err := resolveStartup(cfg)
	if err != nil {
		// 失败路径：调用方可能临时把 logger 切到 debug 让前面 Info 日志落盘，
		// 这里在 Fatal 前显式恢复 debug=false，避免进程退出前留下不一致的 logger 状态。
		// 注意：utils.Fatal 内部调用 os.Exit(1)，但 SetDebugMode 是纯赋值（atomic.Bool.Store），
		// 在 os.Exit 前会执行完毕。
		utils.SetDebugMode(false)
		utils.Fatal("[license] verification FAILED (aborting): %v", err)
	}

	// 成功路径：合并 2 条日志——banner（公钥+解密+验签+序列化一行总览）+ 结果
	utils.Info("[license] verify+decrypt: pubkey=%s, AES-256-GCM open OK, RSA-PKCS#1 v1.5 + SHA-256 signature OK (sig_b64_len=%d), payload JSON decoded: app=%s expires_at=%d package_expire_at=%d",
		out.PubKeySrc, len(out.Result.SignatureB64),
		out.Result.Payload.AppID, out.Result.Payload.ExpiresAt, out.Result.Payload.PackageExpireAt)

	if out.Status == StatusExpiringSoon {
		utils.Warn("[license] EXPIRING_SOON: expires_at=%d (剩余天数见 FormatOutcome)", out.Result.Payload.ExpiresAt)
	}
	utils.Info("[license] OK: %s", FormatOutcome(out))
	return out, nil
}
