package machinecode

import "strings"

// isDirty 判断一个硬件标识值是否为「无效占位值」。
//
// 必要性：大量设备/虚拟化环境在这些字段上填的是占位串而非真实标识，
// 例如：
//
//	/sys/class/dmi/id/board_serial  → "None" / "Not Specified" / "To Be Filled By O.E.M."
//	/sys/class/dmi/id/product_uuid  → "00000000-0000-0000-0000-000000000000"
//	Windows MachineGuid             → 极少出现，但 VM 模板可能填默认值
//
// 若不过滤，这些占位值会让**大量不同机器算出同一个机器码**，
// 硬件绑定形同虚设。
func isDirty(v string) bool {
	// 调用方已 TrimSpace，这里再兜一次（防御性：直接调用 isDirty 时）
	s := strings.TrimSpace(v)
	if s == "" {
		return true
	}

	// 过短的标识没有区分度
	if len(s) < 8 {
		return true
	}

	lower := strings.ToLower(s)

	// 占位串黑名单
	switch lower {
	case "none", "not specified", "not available", "not applicable",
		"to be filled by o.e.m.", "to be filled by oem",
		"default string", "defaultstring", "unknown", "n/a", "na",
		"null", "nil", "0", "oem", "invalid", "innot specified",
		"system serial number", "system manufacturer", "system product name":
		return true
	}

	// 全 0 / 全 f 形态（含 UUID 与裸串两种写法）
	if isAllSameByte(s) {
		return true
	}

	// 去除分隔符后全 0 / 全 f（UUID 形态 00000000-0000-...）
	if isUniformAfterStrip(s, '0') || isUniformAfterStrip(s, 'f') {
		return true
	}

	return false
}

// isAllSameByte 判断去掉常见分隔符（- _ : 空格）后是否只剩同一种字符，
// 且该字符属于「无区分度」类别（0 / f / x / 9）。
func isUniformAfterStrip(s string, c byte) bool {
	stripped := stripSeparators(s)
	if stripped == "" {
		return true
	}
	for i := 0; i < len(stripped); i++ {
		if stripped[i] != c {
			return false
		}
	}
	return true
}

// isAllSameByte 判断去掉分隔符后是否所有字符都相同（如 "11111111"、"aaaa"）。
//
// 注意：只对 0/f/x/9 判定为脏值。像 "0000abcd" 这种混合串不判定为脏值——
// 它虽然前缀可疑，但后半部分仍有区分度。
func isAllSameByte(s string) bool {
	stripped := stripSeparators(s)
	if len(stripped) < 8 {
		return false
	}
	first := stripped[0]
	switch first {
	case '0', 'f', 'F', 'x', '9':
	default:
		return false
	}
	for i := 1; i < len(stripped); i++ {
		if stripped[i] != first {
			return false
		}
	}
	return true
}

// stripSeparators 去掉常见分隔符。
func stripSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', ':', ' ', '\t', '\r', '\n', '.':
			return -1
		}
		return r
	}, s)
}
