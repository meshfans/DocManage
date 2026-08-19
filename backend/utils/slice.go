package utils

// ==================== 通用 slice / set 比较（Issue M-12）====================
//
// 业务方原本在 handlers/rbac_management.go 内联了 permSetsEqual / int64SlicesEqual
// 两份代码。问题：
//   - 单一来源违反 DRY；将来 RBAC 增强时易复制粘贴
//   - nil / [] 的边界处理不同实现会埋坑
//
// 本文件统一定义，导出为公开 API，handler 直接调用 utils.PermSetsEqual 等。
// ----------------------------------------------------------------------------

// PermSetsEqual 比较两个 permission 集合是否相同（无视顺序）。
//
// 边界：
//   - nil 与 [] 视为相同（集合论：空集相等）
//   - 重复元素视为不同：["a","a"] vs ["a"] 返回 false
//
// 时间复杂度 O(n)，空间 O(n)。
func PermSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// Int64SlicesEqual 比较两个 int64 slice 是否相同（无视顺序）。
//
// 边界：同 PermSetsEqual，nil 与 [] 视为相等；重复元素视为不同。
func Int64SlicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[int64]int, len(a))
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
		if m[v] < 0 {
			return false
		}
	}
	return true
}

// NormalizeInt64Slice 将 nil 归一化为非 nil 空 slice，确保 JSON 序列化为 [] 而非 null。
// 用于 audit detail 中写 []int64 字段，避免 null/[] 不一致触发『changed 永远 true』误判。
func NormalizeInt64Slice(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

// NormalizeStringSlice 同 NormalizeInt64Slice，但针对 string slice。
func NormalizeStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}