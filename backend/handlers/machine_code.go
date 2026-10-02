package handlers

import (
	"github.com/gin-gonic/gin"

	"doc/config"
	"doc/pkg/machinecode"
	"doc/utils"
)

// GetMachineCode 返回本机机器码（2026-10-01 C1/C2 统一）。
//
// # 鉴权
//
// 路由挂在 protected 组（JWT + APIGate + license:features 之外单独挂 seed），
// 处理器内再做 **RequireAdmin** 二次校验。
//
// 为什么不放在公开路径：机器码是 license 签发的必要输入，一个无鉴权端点
// 等于给离线攻击者提供便利（DocManage/DocWMS 在 2026-07-04 就因此永久关闭了
// 原来的无鉴权端点）。此端点改为**管理员可读**，运维通过
// curl -H "Authorization: Bearer <admin token>" 获取。
//
// # 响应
//
//	{
//	  "machine_code": "5A8393FA...",   // 本机算出的硬件码（32 位大写 hex）
//	  "configured":   "FE6A...",       // config.json 中已配置的（license 绑定的那个）
//	  "matched":      true,            // 两者是否一致（false 说明 license 绑到了别的机器）
//	  "tier":         1,               // 命中的降级层级
//	  "source":       "HKLM\\...MachineGuid",  // 实际读取的来源
//	  "available":    true             // 是否能算出机器码（容器环境可能为 false）
//	}
//
// 运维用途：config.machine_code 为空时（首次部署），先调本端点拿 machine_code
// 去 LMP 签发；或换机后拿新码重新签发。
func (h *SystemHandler) GetMachineCode(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	// config 中已配置的（license 实际绑定的那个）
	configured := ""
	if config.GlobalConfig != nil {
		configured = config.GlobalConfig.License.MachineCode
	}

	res, err := machinecode.Compute()
	if err != nil {
		// 算不出不报错：容器/受限环境属预期内情况，运维需要知道"为什么算不出"
		utils.Success(c, gin.H{
			"machine_code": "",
			"configured":   configured,
			"matched":      false,
			"available":    false,
			"error":        err.Error(),
			"hint": "当前环境无可用硬件标识（容器/精简内核/权限受限）。" +
				"此类环境请使用 Rust 托管模式（短周期 license），或联系签发方。",
		})
		return
	}

	utils.Success(c, gin.H{
		"machine_code": res.MachineCode,
		"configured":   configured,
		// configured 为空时不算不匹配 —— 那是"还没签发"的状态，不是"被拷贝"
		"matched": configured == "" || configured == res.MachineCode,
		"available": true,
		"tier":      res.Tier,
		"source":    res.Source,
	})
}
