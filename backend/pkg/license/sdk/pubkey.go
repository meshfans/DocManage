package sdk

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// 公钥接收的默认 PEM 文件路径（与 §2.1 P3 一致）。
//
// 仅在 P1（外部注入 PEM）和 P2（命令行 flag）未提供时使用。
// 生产环境强烈推荐 P1（由宿主 daemon / 容器 init 注入），不要把公钥落盘。
const DefaultPubkeyPath = "./public.pem"

// builtinPublicKeyPEM 是 P4 兜底常量。
//
// 当前实现没有内置公钥：保留常量以满足「内置兜底」接口语义，
// 但实际场景下不会触发（启动期缺公钥会直接报错）。
//
// 来源：test-go reference #1 §2.1。
const builtinPublicKeyPEM = ""

// envPubkeyPEMKey 环境变量名（P1 外部注入的另一种形式）。
//
// LICENSE_PUBKEY_PEM 与命令行注入同优先级；后者优先（P1 显式传入 > env）。
const envPubkeyPEMKey = "LICENSE_PUBKEY_PEM"

// cliPubkeyFlags 命令行 flag 列表（P2），按顺序匹配，第一个非空命中即返回。
//
// 多个 flag 支持兼容不同调用方：
//   - --pub-key      ：registerd Rust daemon 当前使用（goproc.rs 的 Command::arg）
//   - --pubkey       ：常见 Go CLI 风格（license_dump 等工具）
//   - --pubkey-pem   ：语义最显式（明文标 "pem"）
var cliPubkeyFlags = []string{"--pub-key", "--pubkey", "--pubkey-pem"}

// ResolvePublicKey 按四级优先级解析公钥 PEM。
//
// 优先级（test-go §2.1，简化为 DocCRM 适配版）：
//   P1: pemArg（外部注入 PEM 字符串，主要用于测试） > LICENSE_PUBKEY_PEM env（生产推荐）
//   P2: --pub-key / --pubkey / --pubkey-pem 命令行 flag（Rust 守护进程注入用）
//   P3: pemFile（默认 ./public.pem，程序同目录）
//   P4: builtinPublicKeyPEM（内置常量；当前为空 → 触发则报错）
//
// 返回值：
//   pem     : PEM 字符串
//   source  : 人类可读的来源标签（用于日志）
//   error   : 所有来源都失败时的最终错误
func ResolvePublicKey(pemArg, pemFile string) (string, string, error) {
	// P1-a: 外部注入的 PEM 字符串（命令行 / 函数参数）
	if pemArg = strings.TrimSpace(pemArg); pemArg != "" {
		if !looksLikePEM(pemArg) {
			return "", "", fmt.Errorf("injected PEM does not look like PEM (no BEGIN marker)")
		}
		return pemArg, "external injection", nil
	}

	// P1-b: 环境变量直接注入的 PEM 内容
	if envPEM := strings.TrimSpace(os.Getenv(envPubkeyPEMKey)); envPEM != "" {
		if !looksLikePEM(envPEM) {
			return "", "", fmt.Errorf("env %s does not look like PEM", envPubkeyPEMKey)
		}
		return envPEM, "env:" + envPubkeyPEMKey, nil
	}

	// P2: 命令行 flag 注入的 PEM 内容（Rust daemon / Go CLI 工具）
	// 仅在 os.Args 含已识别 flag 时读取下一个 arg；支持多 flag 名兼容不同调用方。
	if cliPEM, cliSrc := readPubkeyFromCLI(); cliPEM != "" {
		if !looksLikePEM(cliPEM) {
			return "", "", fmt.Errorf("cli %s does not look like PEM", cliSrc)
		}
		return cliPEM, "cli:" + cliSrc, nil
	}

	// P3: PEM 文件（参数路径 > 默认路径）
	effectivePath := pemFile
	if effectivePath == "" {
		effectivePath = DefaultPubkeyPath
	}

	if data, err := os.ReadFile(effectivePath); err == nil {
		s := strings.TrimSpace(string(data))
		if !looksLikePEM(s) {
			return "", "", fmt.Errorf("file %q does not look like PEM", effectivePath)
		}
		return s, "file: " + effectivePath, nil
	}

	// P4: 内置兜底常量（当前为空）
	if builtinPublicKeyPEM != "" {
		return builtinPublicKeyPEM, "builtin", nil
	}

	return "", "", errors.New("no public key available: P1 (injection / env) and P2 (cli) and P3 (file) all failed, and P4 (builtin) is empty")
}

// readPubkeyFromCLI 扫描 os.Args，按 cliPubkeyFlags 列表匹配第一个命中 flag，
// 返回其下一个 arg 的 trim 结果 + 来源标签。
//
// 设计要点：
//   - 不使用 flag 包：避免在 import 期强行 Parse 整个 os.Args 副作用；
//     本项目启动期只有 main.go 走 flag，sdk 不应耦合 flag 解析状态。
//   - 仅在调用方显式 ResolvePublicKey 时才扫描，启动期一次性开销可忽略。
//   - 多 flag 同优先级：按 cliPubkeyFlags 声明顺序匹配，第一个命中即返回。
func readPubkeyFromCLI() (string, string) {
	args := os.Args
	for i := 0; i < len(args); i++ {
		for _, flag := range cliPubkeyFlags {
			if args[i] != flag {
				continue
			}
			// 命中 flag：下一个 arg 即 PEM 内容（若已是尾部 arg，返回空串让上层判定）
			if i+1 >= len(args) {
				return "", flag
			}
			return strings.TrimSpace(args[i+1]), flag
		}
	}
	return "", ""
}

// looksLikePEM 复用 crypto 包的判断。
func looksLikePEM(s string) bool {
	// 内部小写包装，避免暴露 crypto.looksLikePEM
	return strings.HasPrefix(s, "-----BEGIN ")
}