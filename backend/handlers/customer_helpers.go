package handlers

import (
	"errors"
	"strings"
)

// ==================== USCC Validator (GB 32100-2015) ====================
// Unified Social Credit Code validator. A valid USCC is an 18-char string
// where the first 17 chars are drawn from a 31-char alphabet (digits 0-9
// and uppercase letters A-Z excluding I, O, Z, S, V), and the 18th char
// is a check character that satisfies a weighted sum modulo 31.

// USCCAlphabet is the 31-character alphabet allowed in the first 17
// positions of a USCC (digits 0-9 + uppercase A-Z minus I, O, Z, S, V).
const usccAlphabet = "0123456789ABCDEFGHJKLMNPQRTUWXY"

// usccWeights are the 17 fixed weights per GB 32100-2015.
var usccWeights = [17]int{1, 3, 9, 27, 19, 26, 16, 17, 20, 29, 25, 13, 8, 24, 10, 30, 28}

// usccLookup is a 128-entry ASCII → alphabet-index table built once at
// package load. -1 means "not in the USCC alphabet". Constructed eagerly
// at init time (not per-call) to avoid rebuilding on every validateUSCC.
var usccLookup = func() [128]int {
	var l [128]int
	for i := range l {
		l[i] = -1
	}
	for i, r := range usccAlphabet {
		l[byte(r)] = i
	}
	return l
}()

// validateUSCC returns nil if the given code is a syntactically valid
// unified social credit code. It returns an error describing the first
// problem encountered.
//
// Algorithm (GB 32100-2015):
//  1. The first 17 chars must come from a 31-char alphabet
//     (0-9 + A-Z minus I, O, Z, S, V).
//  2. sum += alphabet_index(char_i) * weight_i  for i in 0..16
//  3. check_value = (31 - sum % 31) % 31
//  4. alphabet[check_value] must equal the 18th char.
func validateUSCC(code string) error {
	// 防御性 trim + 大小写归一（与官方参考实现一致）
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 18 {
		return errors.New("长度必须为 18 位")
	}

	// 直接复用包级 lookup 表（包加载时已构建一次）
	if usccLookup[code[17]] < 0 {
		return errors.New("末位校验字符不合法")
	}

	sum := 0
	for i := 0; i < 17; i++ {
		idx := usccLookup[code[i]]
		if idx < 0 {
			return errors.New("包含非法字符（仅允许 0-9 与大写字母 A-Z，不含 I/O/S/U/V/Z）")
		}
		sum += idx * usccWeights[i]
	}
	checkVal := (31 - sum%31) % 31
	if checkVal != usccLookup[code[17]] {
		return errors.New("校验位计算错误")
	}
	return nil
}

// ==================== JSON helpers ====================

// isUniqueConstraintError reports whether err is a SQLite UNIQUE constraint
// violation on the given index name. We rely on the index name appearing
// in the error message (go-sqlcipher follows the same convention as
// mattn/go-sqlite3).
func isUniqueConstraintError(err error, indexName string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") {
		return false
	}
	if indexName == "" {
		return true
	}
	return strings.Contains(msg, indexName)
}
