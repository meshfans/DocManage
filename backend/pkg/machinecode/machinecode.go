// Package machinecode 生成稳定的机器硬件码，用于 license 的硬件绑定。
//
// # 背景（2026-10-01）
//
// pkg/license/sdk 只校验 config.machine_code 与 payload.machine_code 一致，
// 两者都来自 config.json —— 攻击者拷贝整套部署即可在任意机器运行。
// 本包提供「从本机硬件算出的机器码」，由启动期与 config 里的值比对，
// 堵住拷贝漏洞。
//
// # 设计原则：单一主字段 + 降级链，**不用多字段拼接**
//
// 多字段拼接有个致命问题：只要任意一个字段某次启动读不到（网卡未就绪、
// DMI 未挂载、容器隔离、权限不足），哈希就变了 → 被误判为「换了机器」→
// 误伤客户。单字段降级链只在「所有来源都失效」时才变化，且降级会体现在
// tier 上（启动日志可见），便于运维排查。
//
// # 算法
//
//	machine_code = UPPER(HEX(SHA256("<tier>:<primary_value>")[0:16]))
//
// tier 参与哈希：不同来源算出的值天然不同，避免降级值与真实 UUID 撞车。
// 取 16 字节 = 32 位十六进制大写，与既有 GetMachineCode() 格式一致。
//
// # 明确排除的字段（稳定性反例，勿加入降级链）
//
//	MAC 地址        —— 虚拟网卡 / 随机 MAC / 容器重建 / 多网卡顺序，极易变
//	/etc/machine-id —— 虚拟机镜像克隆时全部相同，无法区分实例
//	CPU 型号        —— 同型号服务器成片重复，无唯一性
//	hostname        —— 可随意修改
//
// # 稳定性保障
//
//  1. 固化优先：调用方在 config.machine_code 非空时直接使用，不重算覆盖。
//     本包只负责「算」，不负责「存」。
//  2. fail-closed：strict 模式下算不出机器码 → 由调用方拒绝启动，
//     不给未绑定授权开后门。
package machinecode

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrUnavailable 表示当前平台没有任何可用的硬件标识源。
//
// 调用方在 strict 模式下应据此拒绝启动（fail-closed）——宁可拒绝启动，
// 也不给未绑定的授权开后门。
var ErrUnavailable = errors.New("machinecode: 当前环境无可用硬件标识，无法生成机器码")

// DefaultBindingThresholdDays Rust 托管 license 的到期阈值（天）。
//
// 语义：license 剩余有效期 < 此值 → 判定为「Rust 托管的短周期 license」
// → 硬件绑定由外部（Rust 侧）接管，Go 不再做本机比对。
//
// 固定 30 天，不做配置项：
//   - Rust 走定时签发，短周期 license 到期即换
//   - Go 独立部署不走定时签发，人工签发长周期 license → 严格硬件绑定
//
// 风险边界：极端情况下（Rust 托管的部署被整包拷贝），攻击者最多获得
// 30 天的可运行窗口，到期后 license 失效。这是本设计已接受的代价。
const DefaultBindingThresholdDays = 30

// Result 机器码计算结果，保留 tier 与来源路径以便运维诊断。
type Result struct {
	// MachineCode 最终机器码（32 位大写十六进制）。
	MachineCode string
	// Tier 实际命中的降级层级（1 起）。层级越高说明越靠后的兜底来源。
	Tier int
	// Source 命中来源的可读描述（如文件路径或注册表键名）。
	Source string
	// Raw 原始硬件标识值（可能含敏感信息，仅用于排查，不要外发）。
	Raw string
}

// candidate 一个候选硬件标识来源。
type candidate struct {
	tier   int
	source string
	read   func() (string, error)
}

// Compute 计算本机机器码。
//
// 返回 ErrUnavailable 表示当前平台无可用硬件标识源（容器 / 受限内核常见），
// 调用方在 strict 模式下应拒绝启动。
func Compute() (*Result, error) {
	return compute(platformCandidates())
}

// compute 遍历降级链，返回第一个「可读且非脏值」的结果。
//
// 全部候选都不可用时返回 ErrUnavailable（不返回部分结果，避免用空值凑出
// 一个所有机器都相同的假机器码）。
func compute(cands []candidate) (*Result, error) {
	for _, c := range cands {
		raw, err := c.read()
		if err != nil {
			// 读不到（文件不存在 / 权限不足）→ 试下一级
			continue
		}
		raw = strings.TrimSpace(raw)
		if isDirty(raw) {
			// 读到了但是占位值 → 试下一级
			continue
		}
		return &Result{
			MachineCode: hash(c.tier, raw),
			Tier:        c.tier,
			Source:      c.source,
			Raw:         raw,
		}, nil
	}
	return nil, ErrUnavailable
}

// hash 机器码主算法：SHA256("<tier>:<value>") 取前 16 字节 → 大写十六进制。
func hash(tier int, value string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", tier, value)))
	return strings.ToUpper(hex.EncodeToString(sum[:16]))
}
