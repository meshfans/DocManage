package machinecode

import (
	"errors"
	"testing"
)

func TestIsDirty_PlaceholderStrings(t *testing.T) {
	dirty := []string{
		"",
		"   ",
		"none",
		"None",
		"NONE",
		"not specified",
		"Not Specified",
		"to be filled by o.e.m.",
		"To Be Filled By O.E.M.",
		"Default string",
		"unknown",
		"N/A",
		"null",
		"0",
		"short",              // 长度 < 8
		"1234567",            // 长度 = 7
		"\n\t",                // 空白
		"System Serial Number",
		"DefaultString",
	}
	for _, v := range dirty {
		if !isDirty(v) {
			t.Errorf("isDirty(%q) = false，期望 true（应被当作占位值）", v)
		}
	}
}

func TestIsDirty_UniformUUIDs(t *testing.T) {
	// 全同字符的哨兵值：0 / f / 9（少数环境用 9 做全填充）
	dirty := []string{
		"00000000-0000-0000-0000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
		"0000000000000000",
		"99999999-9999-9999-9999-999999999999",
	}
	for _, v := range dirty {
		if !isDirty(v) {
			t.Errorf("isDirty(%q) = false，期望 true（全同哨兵字符无区分度）", v)
		}
	}
}

func TestIsDirty_NotOverEager(t *testing.T) {
	// 防止过滤规则过度：以下**不应**被判脏
	//   - "1" 不是哨兵字符，真实标识完全可能全是 1
	//   - 带真实后缀的串（如 xxx-extra）仍有区分度
	//   - 长度刚好 8 的真实串
	clean := []string{
		"1111111111111111",
		"00000000-0000-0000-0000-000000000000-extra",
		"4c4c4544",
		"aaaaaaaa",
		"12345678",
	}
	for _, v := range clean {
		if isDirty(v) {
			t.Errorf("isDirty(%q) = true，期望 false（过滤过度 → 误伤真实机器）", v)
		}
	}
}

func TestIsDirty_RealisticValues(t *testing.T) {
	// 真实标识：必须**不能**被误判为脏值，否则会误伤客户
	real := []string{
		"4c4c4544-0050-3810-8042-b7c04f503332", // SMBIOS UUID
		"FE6A655A-80F0-7855-D8DC-65590FD95FDAB",
		"AB33FF5B-D2AE-ACE2-D453-5129ADC25DC5",
		"d0e5f2a1-9b3c-4d7e-8f21-6a4b0c9e1d23",
		"12345678-1234-1234-1234-123456789012",
		"4F8A2B1C-9D3E-4F5A-8B7C-6D5E4F3A2B1C",
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", // 裸 hex，无分隔符
		"to be filled by oem!!!",             // 混了感叹号，不在黑名单
	}
	for _, v := range real {
		if isDirty(v) {
			t.Errorf("isDirty(%q) = true，期望 false（真实标识被误判 → 会误伤客户）", v)
		}
	}
}

func TestIsDirty_TrimsWhitespace(t *testing.T) {
	// 带换行的文件内容（读 /sys 文件必然带 \n）
	if !isDirty("None\n") {
		t.Error("isDirty(\"None\\n\") = false，期望 true（应忽略首尾空白后判脏）")
	}
	if isDirty("  4c4c4544-0050-3810-8042-b7c04f503332  \n") {
		t.Error("真实 UUID 带空白后被误判为脏值")
	}
}

func TestHash_Deterministic(t *testing.T) {
	// 同输入必须同输出 —— 这是机器码稳定性的基础
	a := hash(1, "4c4c4544-0050-3810-8042-b7c04f503332")
	b := hash(1, "4c4c4544-0050-3810-8042-b7c04f503332")
	if a != b {
		t.Fatalf("hash 不确定：%q != %q", a, b)
	}
	if len(a) != 32 {
		t.Errorf("机器码长度 = %d，期望 32（16 字节十六进制）", len(a))
	}
}

func TestHash_TierAffectsResult(t *testing.T) {
	// tier 参与哈希：不同来源的相同原始值不能撞车
	v := "4c4c4544-0050-3810-8042-b7c04f503332"
	if hash(1, v) == hash(2, v) {
		t.Error("不同 tier 的相同原始值算出了相同机器码，降级值可能与真实 UUID 撞车")
	}
}

func TestCompute_FallbackChain(t *testing.T) {
	// tier1 不可读 → 降级到 tier2
	cands := []candidate{
		{tier: 1, source: "t1", read: func() (string, error) { return "", errors.New("unreadable") }},
		{tier: 2, source: "t2", read: func() (string, error) { return "4c4c4544-0050-3810-8042-b7c04f503332", nil }},
		{tier: 3, source: "t3", read: func() (string, error) { return "should-not-be-used", nil }},
	}
	got, err := compute(cands)
	if err != nil {
		t.Fatalf("compute 失败: %v", err)
	}
	if got.Tier != 2 {
		t.Errorf("命中 tier = %d，期望 2", got.Tier)
	}
	if got.Source != "t2" {
		t.Errorf("命中来源 = %q，期望 t2", got.Source)
	}
	if got.MachineCode != hash(2, "4c4c4544-0050-3810-8042-b7c04f503332") {
		t.Errorf("机器码与 hash(2, raw) 不一致")
	}
}

func TestCompute_SkipsDirtyValue(t *testing.T) {
	// tier1 读到脏值 → 必须继续降级，不能拿占位串凑出机器码
	cands := []candidate{
		{tier: 1, source: "t1", read: func() (string, error) { return "Not Specified", nil }},
		{tier: 2, source: "t2", read: func() (string, error) { return "None\n", nil }},
		{tier: 3, source: "t3", read: func() (string, error) { return "FE6A655A80F0785D8DC65590FD95FDAB", nil }},
	}
	got, err := compute(cands)
	if err != nil {
		t.Fatalf("compute 失败: %v", err)
	}
	if got.Tier != 3 {
		t.Errorf("命中 tier = %d，期望 3（前两级都是占位值，应被跳过）", got.Tier)
	}
}

func TestCompute_AllDirtyReturnsUnavailable(t *testing.T) {
	// 全部是占位值 → 必须返回 ErrUnavailable，
	// 绝不能退化成「所有机器算出同一个机器码」
	cands := []candidate{
		{tier: 1, source: "t1", read: func() (string, error) { return "None", nil }},
		{tier: 2, source: "t2", read: func() (string, error) { return "00000000-0000-0000-0000-000000000000", nil }},
	}
	if _, err := compute(cands); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v，期望 ErrUnavailable", err)
	}
}

func TestCompute_EmptyChainReturnsUnavailable(t *testing.T) {
	if _, err := compute(nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v，期望 ErrUnavailable（空降级链）", err)
	}
}

func TestPlatformCandidatesNonEmpty(t *testing.T) {
	// 编译期保证：当前平台必须有候选链
	c := platformCandidates()
	if len(c) == 0 {
		t.Fatalf("platformCandidates() 为空，当前平台无法生成机器码（strict 模式会 fail-closed）")
	}
	for i, cand := range c {
		if cand.tier != i+1 {
			t.Errorf("第 %d 个候选 tier = %d，期望 %d（tier 应从 1 连续递增）", i, cand.tier, i+1)
		}
		if cand.source == "" {
			t.Errorf("第 %d 个候选缺少 source 描述（启动日志需要它排查）", i)
		}
	}
}

func TestCompute_Real(t *testing.T) {
	// 真实调用：CI 上可能算不出（受限容器），只要不 panic 且结果自洽即可
	got, err := Compute()
	if err != nil {
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("Compute() 返回了非预期错误: %v", err)
		}
		t.Logf("当前环境无法计算机器码（预期内，容器/受限环境）: %v", err)
		return
	}
	if len(got.MachineCode) != 32 {
		t.Errorf("机器码长度 = %d，期望 32", len(got.MachineCode))
	}
	t.Logf("机器码 = %s (tier=%d, source=%s)", got.MachineCode, got.Tier, got.Source)

	// 同一进程内重复计算必须一致
	again, err := Compute()
	if err != nil {
		t.Fatalf("第二次 Compute() 失败: %v", err)
	}
	if again.MachineCode != got.MachineCode {
		t.Errorf("两次计算结果不一致：%q vs %q（稳定性要求同机同值）", got.MachineCode, again.MachineCode)
	}
}
