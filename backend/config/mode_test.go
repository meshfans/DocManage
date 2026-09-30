package config

import (
	"strings"
	"testing"
)

// 2026-09-30 P0-2：database.mode 归一化（去首尾空白 + 转小写）。
//
// 修的问题：原先 isValidDatabaseMode 大小写敏感、未知值只 warn 不阻断启动，而
// IsExperienceMode() 用 == "experience" 精确匹配。于是 {"mode":"Experience"} 会
// 「告警 + 正常启动 + IsExperienceMode()=false」= 静默降级为全站可写。
// 现在拼写错误会落到更严格的一侧：experience 被真正识别为体验模式。
// 本项目支持 DB_MODE 环境变量覆盖（applyEnvOverrides 在 validateConfig 之前），
// 因此 DB_MODE=Experience 同样受益。
func TestIsValidDatabaseMode(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", true},
		{"development", true},
		{"test", true},
		{"experience", true},
		{"production", true},
		// P0-2：大小写 / 首尾空白归一化后视为合法
		{"Experience", true},
		{"EXPERIENCE", true},
		{"experience ", true},
		{"  production  ", true},
		// 归一化后仍不在允许集合内
		{"staging", false},
		{"demo", false},
		{" prod ", false},
		{" experiences ", false},
	}
	for _, c := range cases {
		if got := isValidDatabaseMode(c.mode); got != c.want {
			t.Errorf("isValidDatabaseMode(%q) = %v, want %v", c.mode, got, c.want)
		}
	}
}

func TestNormalizeDatabaseMode(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"experience":     "experience",
		"Experience":     "experience",
		"EXPERIENCE":     "experience",
		"  experience  ": "experience",
		"\texperience\n": "experience",
		"Production":     "production",
		"staging":        "staging",
	}
	for in, want := range cases {
		if got := normalizeDatabaseMode(in); got != want {
			t.Errorf("normalizeDatabaseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestValidateConfigNormalizesModeToExperience 是 P0-2 的核心回归用例：
// 拼写错误的 "Experience" 必须被真正识别为体验模式（只读），
// 而不是「告警一下然后按全量可写运行」。
func TestValidateConfigNormalizesModeToExperience(t *testing.T) {
	orig := GlobalConfig
	defer func() { GlobalConfig = orig }()

	for _, raw := range []string{"Experience", "EXPERIENCE", " experience ", "  experience\t"} {
		cfg := &Config{
			JWT:      JWTConfig{Secret: strings.Repeat("x", 40)},
			Database: DatabaseConfig{Mode: raw},
		}
		validateConfig(cfg)

		if cfg.Database.Mode != "experience" {
			t.Errorf("validateConfig(%q) 后 Mode = %q，期望归一化为 experience", raw, cfg.Database.Mode)
		}
		GlobalConfig = cfg
		if !IsExperienceMode() {
			t.Errorf("mode=%q 归一化后 IsExperienceMode() 应为 true（体验模式 = 只读）", raw)
		}
	}
}

func TestIsExperienceMode_NilSafe(t *testing.T) {
	orig := GlobalConfig
	defer func() { GlobalConfig = orig }()

	GlobalConfig = nil
	if IsExperienceMode() {
		t.Error("GlobalConfig=nil 时 IsExperienceMode 必须返回 false")
	}
	GlobalConfig = &Config{Database: DatabaseConfig{Mode: "experience"}}
	if !IsExperienceMode() {
		t.Error("mode=experience 时必须返回 true")
	}
	// P0-2：绕过 validateConfig 直接写 GlobalConfig（测试 / 未来热更新）也要认
	GlobalConfig = &Config{Database: DatabaseConfig{Mode: "Experience"}}
	if !IsExperienceMode() {
		t.Error("mode=Experience（P0-2 拼写）时必须返回 true，不能静默降级为可写")
	}
	GlobalConfig = &Config{Database: DatabaseConfig{Mode: "production"}}
	if IsExperienceMode() {
		t.Error("mode=production 时必须返回 false")
	}
	// 未知值（validateConfig 只告警不阻断）不应被当作体验模式
	GlobalConfig = &Config{Database: DatabaseConfig{Mode: "staging"}}
	if IsExperienceMode() {
		t.Error("未知 mode 不应被当作体验模式")
	}
}
