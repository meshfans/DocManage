package sdk

// HasFeature 判断 license 是否启用指定 feature。
//
// 规则（2026-09-28 RAG/OCR feature gate）：
//   - out == nil：
//     防御性分支。2026-09-29 硬切后，空 license 已在启动期 Fatal 退出，
//     正常启动流程下 GlobalOutcome() 恒非 nil；此分支仅在启动竞态或人造
//     outcome（测试）时触发，按"未授权"处理 → false。
//
//   - out.Result == nil：验签失败 / payload 异常。VerifyStartup 内部已 Fatal 不返回，
//     此分支仅在测试或人造 startup outcome 时触发，按"模块禁用"处理 → false。
//
//   - out.Err != nil：启动期已 Fatal，正常路径不会看到。防御式按 false。
//
//   - payload.Features == nil 或 len == 0：未签发任何模块 → false（与"数组为空所有模块禁用"硬约束一致）。
//
//   - key 在 features slice 里以精确字符串相等命中：true。
//
//   - 其它（含 "ragi" 等子串、不区分大小写）：false（精确匹配，避免误判）。
//
// 用法示例：
//
//	if sdk.HasFeature(sdk.GlobalOutcome(), "rag") {
//	    // 启用 RAG 模块
//	}
// 业务模块 key 白名单见下方 BusinessModuleKeys（含 rag / ocr / ai）。
// BusinessModuleKeys 当前各项目业务实际消费的 license module key 白名单。
//
// 单一权威源：features 数组里超出此集合的 key → 仅作为 license 描述字段存在，
// 不参与前端路由/中间件守门。新增业务模块时同步更新本数组 + 前端
// frontend/src/utils/license.ts 的 KNOWN_MODULE_KEYS。
//
// 当前集合（2026-10-01 统一）：
//   - "rag" — RAG 向量检索（DocCRM 已接入；WMS/Manage/LMP/Trail 预留，具体功能未接入）
//   - "ocr" — OCR 模块（预留，具体功能未接入）
//   - "ai"  — AI 配置开关。控制 ai_config 页面是否显示：
//       · 有 "ai"  → 显示 AI 配置页，用户可自由配置 LLM 接入
//       · 无 "ai"  → 隐藏该页，默认使用 meshfans 提供的 LLM 能力
var BusinessModuleKeys = []string{"rag", "ocr", "ai"}

// IsBusinessModuleKey 判断 key 是否在 BusinessModuleKeys 集合内。
//
// 用于：GetLicenseFeatures / middleware.FeatureGate / 前端 moduleKeys 校验的兜底。
// 不在集合内的 key 一律视为"非业务模块"，返回 false（防御性：避免被恶意 features
// 数组越权打开未注册模块）。
func IsBusinessModuleKey(key string) bool {
	for _, k := range BusinessModuleKeys {
		if k == key {
			return true
		}
	}
	return false
}

// BusinessModuleKeysFromFeatures 从 license payload.Features 派生业务实际消费的
// module key 集合（精确字符串相等，且在 BusinessModuleKeys 白名单内）。
//
// 用法：
//
//	out := sdk.GlobalOutcome()
//	keys := sdk.BusinessModuleKeysFromFeatures(out.Result.Payload.Features)
//	// keys 用于：响应前端 /api/license/features 的 module_keys 字段
//
// 行为：
//   - 保留 BusinessModuleKeys 声明顺序（不按 features 入参顺序）
//   - features 数组里有但不在白名单的 key（如 "ai" 基线）→ 静默忽略
//   - 返回非 nil 空切片（前端 `module_keys.length === 0` 即视为未授权）
func BusinessModuleKeysFromFeatures(features []string) []string {
	out := make([]string, 0, len(BusinessModuleKeys))
	for _, k := range BusinessModuleKeys {
		for _, f := range features {
			if f == k {
				out = append(out, k)
				break
			}
		}
	}
	return out
}

func HasFeature(out *StartupOutcome, key string) bool {
	if out == nil || out.Result == nil {
		return false
	}
	features := out.Result.Payload.Features
	if len(features) == 0 {
		return false
	}
	for _, f := range features {
		if f == key {
			return true
		}
	}
	return false
}
