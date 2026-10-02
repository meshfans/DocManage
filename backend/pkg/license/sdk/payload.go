// Package sdk 提供 LMP License 业务封装层（crypto 算法的语义化包装）。
//
// 责任：
//   - 把 license_key / appID / machineCode / 公钥 PEM 组合起来完成一次完整校验
//   - 暴露高层 API（VerifyLicense）+ 启动期辅助（VerifyStartup）
//   - 不关心 LMP 服务端激活 / 续期 / 任务轮询（那是 daemon 职责）
//
// 来源：test-go reference #1 §5、§6。
package sdk

import "time"

// LicensePayload 是 license_key 解密 + 验签通过后的明文结构。
//
// 字段顺序与 JSON tag 与签发端（LMP 后端）严格一致。
// JSON tag 不能换顺序（test-go §4.2 坑 2：验签用原始字节，不重新序列化）。
type LicensePayload struct {
	AppID           string   `json:"app_id"`
	MachineCode     string   `json:"machine_code"`
	Password        string   `json:"password"`
	Features        []string `json:"features"`
	IssuedAt        int64    `json:"issued_at"`
	ExpiresAt       int64    `json:"expires_at"`
	PackageExpireAt int64    `json:"package_expire_at"`
	CompanySize     string   `json:"company_size"`
	Algo            string   `json:"algo"`
}

// DecryptResult 是 VerifyLicense 的返回值。
//
// Payload 已解密 + 验签通过；SignatureB64 便于日志 / 调试。
type DecryptResult struct {
	Payload       LicensePayload
	SignatureB64  string
	PubKeySource  string // 来源标签（"external injection" / "file: ..." / "builtin"）
}

// Status 校验通过后的业务状态。
type Status string

const (
	// StatusValid 正常使用。
	StatusValid Status = "VALID"
	// StatusExpiringSoon 剩余天数 < 7 天，建议续期。
	StatusExpiringSoon Status = "EXPIRING_SOON"
	// StatusExpired 已过期，必须重新激活。
	StatusExpired Status = "EXPIRED"
)

// ExpiringSoonThresholdDays 触发 EXPIRING_SOON 提示的剩余天数阈值。
// 与 test-go §5.2 保持一致（7 天）。
const ExpiringSoonThresholdDays = 7

// ClassifyTimeStatus 根据 expires_at 判断业务状态。
//
// 入参 nowUnix 为当前 Unix 秒（便于测试注入）；expiresAt 为 payload.expires_at。
// 返回值始终是 StatusValid / StatusExpiringSoon / StatusExpired 之一。
func ClassifyTimeStatus(nowUnix, expiresAt int64) Status {
	if nowUnix > expiresAt {
		return StatusExpired
	}
	secondsLeft := expiresAt - nowUnix
	daysLeft := secondsLeft / 86400
	if daysLeft < ExpiringSoonThresholdDays {
		return StatusExpiringSoon
	}
	return StatusValid
}

// StatusOf 是 ClassifyTimeStatus(time.Now().Unix(), expiresAt) 的便捷封装。
func StatusOf(expiresAt int64) Status {
	return ClassifyTimeStatus(time.Now().Unix(), expiresAt)
}