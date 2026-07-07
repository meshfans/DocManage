package database

import (
	"strings"
)

// 本文件：data_scope 通用适配器。
// 为 reminder_subscription 等低频资源提供统一的 ListXByDataScope 模式：
//   - 在原 ListX 基础上接受 extraWhere + extraArgs
//   - 去掉 BuildWhereSQL 输出的 "deleted_at = 0" 前缀（部分表用 status 列替代）
//   - AND 到原 WHERE 末尾
//
// 设计取舍：保持原 ListX 函数签名不变（向后兼容），新增 ListXByDataScope 走 data_scope 路径。
// 实际 SQL 拼接在每个函数内独立维护（小复制），避免引入抽象层带来的复杂度。

// adaptDataScopeWhere 适配 BuildWhereSQL 输出到具体表的 WHERE 拼接。
//   - hasDeletedAt=true：表有 deleted_at 列 → 直接保留 BuildWhereSQL 输出的 "deleted_at = 0"
//   - hasDeletedAt=false：表无 deleted_at 列 → 把 "deleted_at = 0" 替换为 "1=1" 占位
//   - 清理尾部孤立 AND
//   - 返回 (cleanedWhere, ok)：ok=false 表示 extraWhere 退化为空（无需追加）
//
// 使用示例：
//
//	cleaned, ok := adaptDataScopeWhere(extraWhere, true /* hasDeletedAt */)
//	if ok {
//	    whereParts = append(whereParts, "("+cleaned+")")
//	    args = append(args, extraArgs...)
//	}
func adaptDataScopeWhere(extraWhere string, hasDeletedAt bool) (string, bool) {
	if extraWhere == "" {
		return "", false
	}
	cleaned := extraWhere
	if !hasDeletedAt {
		// 表无 deleted_at → 去掉该前缀
		cleaned = strings.Replace(cleaned, "deleted_at = 0", "1=1", 1)
	}
	cleaned = strings.TrimSpace(cleaned)
	// 去掉尾部孤立 AND
	cleaned = strings.TrimSuffix(cleaned, "AND")
	cleaned = strings.TrimSpace(cleaned)
	// 退化检查：只剩 "1=1" 表示无有效条件
	if cleaned == "" || cleaned == "1=1" {
		return "", false
	}
	return cleaned, true
}
