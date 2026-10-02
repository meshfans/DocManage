package handlers

import (
	"github.com/gin-gonic/gin"

	"doc/pkg/license/sdk"
	"doc/utils"
)

// LicenseFeaturesHandler license 能力查询（2026-10-01 C1/C2 统一时引入）。
//
// 单独成文件而非并入 system.go：本项目的 system.go 归属各自业务线，
// 独立文件便于跨项目同步且不产生冲突。
//
// 数据源：sdk.GlobalOutcome().Result.Payload.Features
//
//	由 main.go 在启动期 sdk.VerifyStartupOrLog 后经 sdk.SetGlobalOutcome 注入。
//	若 GlobalOutcome()==nil（仅启动竞态或尚未接入启动期校验），返回空 module_keys，
//	前端据此隐藏所有 requiredFeature 页面（保守策略）。
//
// 响应结构：
//
//	{
//	  "features":    ["rag", "ocr", "ai"],  // 原始 features 数组原样透传
//	  "module_keys": ["rag", "ai"],         // features ∩ sdk.BusinessModuleKeys 派生
//	  "expires_at":  1793101098             // license 过期 Unix 秒
//	}
//
// 设计要点：
//   - features 原样透传（保留 license 自描述全部字段）
//   - module_keys 用 sdk.BusinessModuleKeysFromFeatures 从 features ∩ 白名单派生，
//     单一权威源（pkg/license/sdk/feature.go）；新增业务模块只需扩白名单
//   - 前端用 module_keys 做精确匹配，避免依赖 features 数组语义
//
// 当前白名单（sdk.BusinessModuleKeys）：rag / ocr / ai
//
//	· "ai"  → 控制 /system/ai-config 页面显隐
//	        （有则显示，用户可自由配置 LLM；无则隐藏，默认用 meshfans 的 LLM 能力）
//	· "rag" / "ocr" → 预留，具体功能尚未接入
func (h *SystemHandler) GetLicenseFeatures(c *gin.Context) {
	out := sdk.GlobalOutcome()
	var features []string
	if out != nil && out.Result != nil {
		features = out.Result.Payload.Features
	}
	if features == nil {
		features = []string{}
	}
	moduleKeys := sdk.BusinessModuleKeysFromFeatures(features)
	utils.Success(c, gin.H{
		"features":    features,
		"module_keys": moduleKeys,
		"expires_at":  licenseExpiryFromOutcome(out),
	})
}

// licenseExpiryFromOutcome 安全取 ExpiresAt（nil-safe）。
func licenseExpiryFromOutcome(out *sdk.StartupOutcome) int64 {
	if out == nil || out.Result == nil {
		return 0
	}
	return out.Result.Payload.ExpiresAt
}
