package handlers

import (
	"os"
	"strings"
	"testing"
)

// TestExperienceModeNotWritableViaAPI 固化 2026-09-23 P0 决策：
// 体验模式（database.mode == "experience"）只能通过改磁盘 config.json 后重启切换，
// 不得经 SaveConfigFile 之类的管理端 API 回写。
//
// 原因：config.IsExperienceMode() 直读内存中的 GlobalConfig.Database.Mode，
// 一次 POST {"database":{"mode":""}} 即可立刻解除全站只读，无需重启，
// ExperienceReadOnly 中间件随即完全失效。
//
// 这里用源码断言而非 handler 行为测试：SaveConfigFile 会 os.WriteFile 真实
// config.json 并调用 database.RecordAudit（依赖已初始化的 DB），行为测试会污染
// 工作区。源码断言无副作用，且能在 CI 里第一时间挡住回写逻辑被重新引入。
func TestExperienceModeNotWritableViaAPI(t *testing.T) {
	src, err := os.ReadFile("system.go")
	if err != nil {
		t.Fatalf("读取 system.go 失败: %v", err)
	}
	body := string(src)

	forbidden := []string{
		"cfg.Database.Mode =",
		"GlobalConfig.Database.Mode =",
		"GlobalConfig.Database.Mode=",
	}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("system.go 出现禁止的运行模式回写 %q：体验模式不得经 API 切换"+
				"（2026-09-23 P0；改磁盘 config.json 后重启，或用 DB_MODE env）", f)
		}
	}

	// 管理端 DTO 也不应再暴露 mode 字段：GET 把它回显出来，前端就会渲染出
	// 一个可编辑控件，POST 时又静默不生效（误导性死控件）。
	// 2026-09-30：DocLMP / DocManageTrail 曾长期保留该字段，已一并移除。
	if strings.Contains(body, "`json:\"mode\"`") {
		t.Error("handlers/system.go 的 DTO 不应再声明 mode 字段"+
			"（体验模式只能改磁盘 config.json，见 2026-09-23 P0）")
	}
}
