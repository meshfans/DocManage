package main

// 启动期许可校验 + 机器码绑定 + debug 信封（2026-10-01 C1/C2/C5 统一基准）。
//
// 本文件是**四家客户端共用**的启动期引导实现：
// DocCRM / DocManage / DocWMS / DocManageTrail 逻辑完全一致，
// 仅 import 前缀随 go module 名不同（doc/ 或 wms/）。
//
// # 完整流程（顺序不可调换）
//
//	步骤1  机器码就绪
//	       cfg.machine_code 为空 → 计算本机硬件码 → 写入 config.json
//	                          → 提示"请找 LMP 签发" → 退出
//	步骤2  SDK 校验
//	       Validate → ResolvePublicKey → DeriveAesKey(cfg.machine_code)
//	              → AesGcmDecrypt → 切分签名 → RsaVerifyPayload → Unmarshal
//	步骤3  一致性 + 时效
//	       payload.app_id == cfg.app_id ?
//	       payload.machine_code == cfg.machine_code ?
//	       ExpiresAt vs now：EXPIRED 拒 / EXPIRING_SOON 告警放行
//	步骤4  绑定模式判定   ★ 必须在步骤 2/3 之后
//	       expires_at 剩余 < 30 天 → Rust 托管 → 跳过步骤5
//	步骤5  严格硬件绑定
//	       本机机器码 != cfg.machine_code → 拒绝启动
//	步骤6  debug 信封判定
//
// # 为什么步骤4 必须在验签之后
//
// expires_at 是 payload 字段，而 verify.go 先 RsaVerifyPayload 再 json.Unmarshal。
// 验签通过前无法读取、也无法伪造 payload 的任何字段。
// 若把步骤4 提前，未验签的数据就能决定安全策略，判据会重新变得可伪造。
//
// # 攻击路径覆盖
//
//	原样拷贝部署到异机        → 步骤5 actual != expected → FATAL
//	改 config.machine_code 绕过 → AES 密钥改由新值派生 → 步骤2 解不开 → FATAL
//	清空 machine_code 重新生成 → 生成新值 → 步骤2 同样解不开 → FATAL
//	伪造短周期 expires_at      → 无法伪造，在 RSA 签名内且验签后才解析
//	替换 public.pem           → 验签失败 → FATAL

import (
	"time"

	"doc/config"
	"doc/pkg/debug"
	"doc/pkg/license/sdk"
	"doc/pkg/machinecode"
	"doc/utils"
)

// bootstrapResult 启动期引导结果。
type bootstrapResult struct {
	// DBPassword license payload 的 password 字段。
	// 部分部署用它派生 SQLCipher 的 DB 密钥；为空表示该 license 未签发密码。
	DBPassword string
	// DebugModeActive debug 时间窗是否命中（供 server 层挂 /debug/pprof/*）。
	DebugModeActive bool
	// MachineCode 本机实际机器码（步骤5 计算得到；Rust 托管时为空）。
	MachineCode string
	// BindingMode 实际生效的绑定模式："strict" / "managed"
	BindingMode string
	// MachineCodeTier 机器码命中的降级层级（0 表示未计算）。
	MachineCodeTier int
	// SetupMode 2026-10-02 H-3 修复：首次部署（LicenseKey 为空）时为 true，
	// main.go 据此切换到 setup 启动路径（监听端口但不开放业务 API，
	// 仅暴露 /setup 端点返回机器码 + 签发指引）。
	SetupMode bool
}

// bootstrapLicenseAndDebug 执行启动期许可校验 + 机器码绑定 + debug 信封判定。
//
// **完全硬切**：许可无效时 sdk.VerifyStartupOrLog 内部已 log.Fatal，本函数不返回。
//
// 2026-10-02 H-3 修复：首次部署（LicenseKey 为空）时进入 setup 模式：
//   - prepareMachineCode 返回 bootstrapping=true
//   - 跳过 SDK 严格校验（不调 VerifyStartupOrLog，避免 Fatal）
//   - sdk.SetGlobalOutcome 写入零值 outcome（所有 feature 视为 false）
//   - main.go 据此以 setup 模式启动 HTTP（监听 /setup 端点）
//
// 返回值新增 SetupMode 字段，main.go 据此切换启动路径。
func bootstrapLicenseAndDebug(cfg *config.Config) bootstrapResult {
	// ---------- 步骤1：机器码就绪 ----------
	expected, bootstrapping := prepareMachineCode(cfg)

	var licOutcome *sdk.StartupOutcome
	if bootstrapping {
		// setup 模式：构造零值 outcome，所有 HasFeature 返 false
		utils.Warn("[license] SETUP 模式：跳过 SDK 校验，等待 license 签发")
		licOutcome = nil
	} else {
		// ---------- 步骤2：SDK 完整校验 ----------
		licOutcome, _ = sdk.VerifyStartupOrLog(sdk.LicenseConfig{
			AppID:       cfg.License.AppID,
			MachineCode: cfg.License.MachineCode,
			LicenseKey:  cfg.License.LicenseKey,
		})
	}

	// 暴露给全局：feature gate（middleware.FeatureGate）与
	// GET /api/license/features 均依赖此处的写入。
	sdk.SetGlobalOutcome(licOutcome)

	// ---------- 步骤3 之后：步骤4 + 步骤5（绑定模式判定 + 硬件绑定）----------
	bindMode, actual, tier := enforceMachineBinding(licOutcome, expected)

	// ---------- 步骤6：debug 信封判定 ----------
	// 解密成功且当前时间 ∈ [starttime, endtime] 才挂 pprof。
	// 字段缺失 / 解密失败 / 窗口外 → 不挂载，仅 warn（不 Fatal）。
	debugOut := debug.ResolveDebugMode(debug.DebugConfig{
		AppID:       cfg.License.AppID,
		Password:    licensePassword(licOutcome),
		DebugEnvB64: cfg.Debug,
	})
	active := debugOut.Active

	// 未命中时必须显式回填 false：部分项目在 InitLogger 前会临时
	// SetDebugMode(true) 兜底，若不回填，日志会持续污染文件。
	if !active {
		utils.SetDebugMode(false)
	}

	if active {
		utils.Warn("[debug] mode=active issued=%d window=[%s, %s]",
			debugOut.Issued, debugOut.StartTime, debugOut.EndTime)
	} else {
		utils.Warn("[debug] mode=inactive source=%s", debugOut.Source)
	}

	return bootstrapResult{
		DBPassword:      licensePassword(licOutcome),
		DebugModeActive: active,
		MachineCode:     actual,
		BindingMode:     bindMode,
		MachineCodeTier: tier,
		SetupMode:       bootstrapping,
	}
}

// prepareMachineCode 实现步骤1：确保 cfg.License.MachineCode 非空。
//
//	为空   → 计算本机硬件码 → 持久化到 config.json → 提示签发 → **不退出**
//	非空   → 直接返回（**固化优先，绝不重算覆盖**）
//
// 固化优先的理由：同一台机器的硬件标识可用性会波动（网卡未就绪、DMI 暂不可读、
// 权限变化），若每次启动都重算，可能算出不同值 → 误判换机 → 误伤客户。
// 算一次、存下来、之后一直用，是稳定性的根本保障。
//
// 返回值 (machineCode, bootstrapping)：
//   - bootstrapping=true 表示 LicenseKey 也为空（首次部署），调用方应
//     跳过 SDK 严格校验，转入"启动 setup 引导服务"路径。
//   - bootstrapping=false 表示已有机器码，调用方按正常流程走 SDK 校验。
func prepareMachineCode(cfg *config.Config) (string, bool) {
	if cfg.License.MachineCode != "" {
		utils.Info("[binding] 使用配置中的机器码: %s", cfg.License.MachineCode)
		return cfg.License.MachineCode, false
	}

	// 机器码为空 → 算一个并写入，供运维拿去 LMP 签发
	res, err := machinecode.Compute()
	if err != nil {
		utils.Fatal("[binding] FATAL: %v\n"+
			"  当前环境无可用硬件标识，无法生成机器码。\n"+
			"  容器 / 精简内核 / 权限受限环境请使用 Rust 托管模式，或联系签发方处理。\n"+
			"  常见原因：容器内无 /sys/class/dmi/ 与 /sys/devices/virtual/dmi/", err)
	}

	// 写入 config.json
	cfg.License.MachineCode = res.MachineCode
	if err := config.SaveLicenseMachineCode(cfg.License.MachineCode); err != nil {
		utils.Warn("[binding] 机器码写入 config.json 失败（可手工填写）: %v", err)
	} else {
		utils.Info("[binding] 机器码已写入 config.json")
	}

	// 2026-10-02 H-3 修复：不再 Fatal 退出。
	//
	// 历史行为 utils.Fatal → 反向代理 / 客户端只看到"服务挂了"，看不到
	// warn 里的"机器码 + LMP 签发"提示，运维需主动看 logs 才能继续。
	//
	// 新行为：当 LicenseKey 也为空时（典型首次部署），进入 SETUP 模式，
	// 由 main.go 走 setup 模式——监听端口 + 返回"机器码 + 签发指引"页面，
	// **不退出进程**，运维可立刻在浏览器看到提示并填入 license_key。
	bootstrapping := decideBootstrapMode(cfg)
	if bootstrapping {
		utils.Warn("========================================")
		utils.Warn("  首次部署：已生成机器码，等待 license 签发")
		utils.Warn("========================================")
		utils.Warn("  机器码   : %s", res.MachineCode)
		utils.Warn("  来源     : tier %d — %s", res.Tier, res.Source)
		utils.Warn("")
		utils.Warn("  服务将以 SETUP 模式启动（监听端口但不开放业务 API）。")
		utils.Warn("  浏览器打开根路径即可看到机器码 + 签发指引。")
		utils.Warn("  拿到 license_key 后填入 config.json 的 license.license_key，")
		utils.Warn("  重启服务即可进入正常模式。")
		utils.Warn("========================================")
		return res.MachineCode, true
	}
	// 机器码之前缺、现在补上了，但 license_key 已存在 → 让 SDK 走正常校验流程
	// （errAppIdMismatch / errExpired 等会触发 Fatal）
	return res.MachineCode, false
}

// decideBootstrapMode 纯函数：是否进入 SETUP 引导模式（首次部署）。
//
// 解耦目的是让 N-1 单测可覆盖（prepareMachineCode 内部含 utils.Fatal / IO，
// 单测无法跑；decision 逻辑本身是纯函数，单独拎出来测）。
//
// 判定：cfg.License.LicenseKey 为空 → true（首次部署，需要签发引导）
//       cfg.License.LicenseKey 非空 → false（已有 license，走正常启动）
func decideBootstrapMode(cfg *config.Config) bool {
	return cfg.License.LicenseKey == ""
}

// enforceMachineBinding 实现步骤4 + 步骤5。
//
// 步骤4（绑定模式判定）：依据**已验签**的 payload.expires_at。
//   - 剩余 < 30 天 → 判定为「Rust 托管的短周期 license」→ 跳过硬件绑定
//     （Rust 走定时签发，到期即换；Go 侧不重复校验硬件）
//   - 否则 → 严格硬件绑定
//
// 步骤5（严格硬件绑定）：本机机器码与 cfg.machine_code 不一致 → 拒绝启动。
//
// 返回 (bindingMode, actualMachineCode, tier)。
func enforceMachineBinding(out *sdk.StartupOutcome, expected string) (string, string, int) {
	// ---- 步骤4：模式判定（必须基于已验签的 payload）----
	thresholdDays := machinecode.DefaultBindingThresholdDays
	if out != nil && out.Result != nil {
		remaining := time.Until(time.Unix(out.Result.Payload.ExpiresAt, 0))
		if remaining < time.Duration(thresholdDays)*24*time.Hour {
			utils.Warn("[binding] 硬件绑定由外部接管"+
				"（license 剩余 %.1f 天 < %d 天，判定为 Rust 托管的定时签发 license）",
				remaining.Hours()/24, thresholdDays)
			return "managed", "", 0
		}
	}

	// ---- 步骤5：严格硬件绑定 ----
	res, err := machinecode.Compute()
	if err != nil {
		// fail-closed：算不出机器码就拒绝启动，不给未绑定授权开后门
		utils.Fatal("[binding] FATAL: %v\n"+
			"  严格模式下无法验证本机硬件标识，拒绝启动。\n"+
			"  若这是容器 / 受限环境，请改用 Rust 托管模式（短周期 license），或联系签发方。", err)
	}

	if res.MachineCode != expected {
		utils.Fatal("[binding] FATAL: 机器码不匹配，拒绝启动\n"+
			"  license 绑定机器码 : %s\n"+
			"  本机检测机器码     : %s  (tier %d — %s)\n"+
			"  可能原因：整套部署被复制到其他机器，或本机硬件已更换（换盘/重装/云主机迁移）。\n"+
			"  处置：用上面的『本机检测机器码』重新到 LMP 签发 license。",
			expected, res.MachineCode, res.Tier, res.Source)
	}

	utils.Info("[binding] 机器码校验通过: %s (tier %d — %s)", res.MachineCode, res.Tier, res.Source)
	return "strict", res.MachineCode, res.Tier
}

// licensePassword 从 license 启动期校验结果中安全提取 password。
//
// 返回空字符串的场景：out / out.Result 为 nil（防御性分支——硬切后
// 正常启动路径下 licOutcome 恒非 nil）。
//
// 用途：作为 HKDF ikm 派生 debug 信封 AES key。
func licensePassword(out *sdk.StartupOutcome) string {
	if out == nil || out.Result == nil {
		return ""
	}
	return out.Result.Payload.Password
}
