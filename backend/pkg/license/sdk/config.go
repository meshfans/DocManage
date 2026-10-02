package sdk

import "errors"

// LicenseConfig 启动期一次性校验所需的全部输入。
//
// 字段语义：
//   - AppID       : 本进程 app_id（必须与 payload.app_id 一致）
//   - MachineCode : 本机硬件码（必须与 payload.machine_code 一致）
//   - LicenseKey  : base64 编码的 envelope（来自 LMP 后端签发）
//
// 公钥来源由 sdk.ResolvePublicKey 解析：
//   - LICENSE_PUBKEY_PEM env > ./public.pem（同目录）
//
// 默认行为：业务调用方应自行解析配置（bin/config.test-main.json），
// 然后构成本结构传入 VerifyLicense / VerifyStartup。
type LicenseConfig struct {
	AppID       string
	MachineCode string
	LicenseKey  string
}

// Validate 校验 LicenseConfig 自身字段合法性。
//
// 注意：本函数不验证 license_key 本身，只检查配置输入完整性。
// VerifyMode 不参与校验（off 时由 VerifyStartupOrLog 直接返回）。
func (c LicenseConfig) Validate() error {
	if c.AppID == "" {
		return errors.New("license.AppID is required")
	}
	if c.MachineCode == "" {
		return errors.New("license.MachineCode is required")
	}
	if c.LicenseKey == "" {
		return errors.New("license.LicenseKey is required")
	}
	return nil
}