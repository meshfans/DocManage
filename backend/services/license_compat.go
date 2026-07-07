package services

// GetMaxDepartmentLevel 部门最大层级（硬编码 3）。
// 调用方：handlers/department.go（Create / Update 时校验父链路不超过此值）。
func GetMaxDepartmentLevel() int {
	return 3
}
