package services

// 2026-07-06 round6 精简：license 模块整体下线（services/license.go 整文件删除）。
//   - 原 GetMaxDepartmentLevel() 读 currentLicense.CompanySize 返回层级上限
//     （small=2 / medium=3 / large=4）
//   - 现 license 不存在，部门最大层级**硬编码为 3**（原 medium 默认值）
//   - 想恢复可改：再读取 database.DEFAULT_CONFIGS["company_size"].Value 做映射

// GetMaxDepartmentLevel 部门最大层级。
//
// 2026-07-06 round6 精简：license 下线后写死 3（原 medium 档位）。
// 调用方：handlers/department.go（Create / Update 时校验父链路不超过此值）。
func GetMaxDepartmentLevel() int {
	return 3
}
