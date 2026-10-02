//go:build windows

package machinecode

import (
	"os/exec"
	"regexp"
	"strings"
)

// platformCandidates 返回 Windows 的硬件标识降级链。
//
// tier 1 用注册表 MachineGuid 而非 PowerShell/wmic：
//   - wmic 自 Windows 11 24H2 起已弃用，新版系统上可能直接不可用（2026-10-02 M-3 移除）
//   - PowerShell `Get-CimInstance Win32_ComputerSystemProduct` 仍可用，但需起 powershell.exe 子进程
//     + 可能被 Execution Policy 拦截，启动期成本较高
//   - 注册表读数无需起子进程，启动期最稳
//
// MachineGuid 的性质：每台机器的 OS 安装唯一，重装系统会变。
// 对于「一机一授权」场景足够；换系统需重新签发（已在设计文档中说明）。
//
// tier 2 兜底：WMI CIM（PowerShell）。仅在 MachineGuid 读不到时使用（极端场景：
// 注册表 ACL 被锁 / 测试环境用 WinPE 等）。
func platformCandidates() []candidate {
	return []candidate{
		{tier: 1, source: `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`, read: readMachineGuid},
		{tier: 2, source: "powershell Get-CimInstance Win32_ComputerSystemProduct", read: readWmiComputerSystemProduct},
	}
}

// readMachineGuid 从注册表读取 MachineGuid。
//
// 优先用 reg.exe（免引入 cgo/registry 依赖，且与 Go 原生库行为一致）。
func readMachineGuid() (string, error) {
	out, err := exec.Command(
		"reg", "query",
		`HKLM\SOFTWARE\Microsoft\Cryptography`,
		"/v", "MachineGuid",
	).Output()
	if err != nil {
		return "", err
	}
	// 输出形如：
	//   HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography
	//       MachineGuid    REG_SZ    4f8a...-...
	re := regexp.MustCompile(`MachineGuid\s+REG_SZ\s+(\S+)`)
	m := re.FindStringSubmatch(string(out))
	if len(m) != 2 {
		return "", errMachineGuidNotFound
	}
	return m[1], nil
}

var errMachineGuidNotFound = errNotFound("MachineGuid not found in registry output")

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

// readWmiComputerSystemProduct 兜底：用 PowerShell 读 SMBIOS UUID。
// 替代旧的 readWmicProductUUID（wmic 已弃用，2026-10-02 M-3 修复）。
//
// 实现：
//   - 起 powershell.exe 子进程（无 cgo 依赖，与读注册表风格一致）
//   - 调用 Get-CimInstance Win32_ComputerSystemProduct 读 SMBIOS UUID
//   - 用 `-NoProfile -NonInteractive -ExecutionPolicy Bypass` 避开常见 Execution Policy 拦截
//   - regex 提取标准 8-4-4-4-12 UUID 形态
//
// 注意：仍保留兜底语义——读不到 / powershell 不存在 → 降级链继续往下
// （machinecode_other.go 在非 Linux/Windows 平台返回空链 → fail-closed）。
var uuidRe = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

func readWmiComputerSystemProduct() (string, error) {
	out, err := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command",
		"(Get-CimInstance Win32_ComputerSystemProduct).UUID",
	).Output()
	if err != nil {
		return "", err
	}
	m := uuidRe.FindString(string(out))
	if m == "" {
		return "", errNotFound("UUID not found in Get-CimInstance output")
	}
	return strings.TrimSpace(m), nil
}
