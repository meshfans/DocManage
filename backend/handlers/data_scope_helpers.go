package handlers

// 本文件：data_scope 感知的访问控制 helper（统一版）。
//
// 调用示例：
//
//	if !CheckDataScopeAccess(c, ownerID, departmentID) {
//	    utils.Error(c, 403, "无权访问")
//	    return
//	}

import (
	"doc/middleware"

	"github.com/gin-gonic/gin"
)

// CheckDataScopeAccess 检查当前用户的 data_scope 是否允许访问指定 owner / department 的资源。
// 规则（按优先级）：
//  1. admin (IsAdminUser) → 允许
//  2. ownerID == 0（公开/系统创建） → 允许
//  3. ownerID == 当前用户 → 允许
//  4. data_scope == "all" → 允许
//  5. data_scope ∈ {dept, dept_and_sub, self_and_sub_dept} 且 departmentID ∈ SubDeptIDs → 允许
//  6. data_scope == "custom" 且 departmentID ∈ CustomDepts → 允许
//  7. 其他 → 拒绝
//
// 参数：
//   - c: gin context（含 JWT user_id + 已加载的 data_scope 中间件）
//   - ownerID: 资源所有者（如 customer.OwnerUserID / media.TakenBy / third_party.CreatedBy）
//   - departmentID: 资源所属部门（resources.department_id 列快照）
func CheckDataScopeAccess(c *gin.Context, ownerID, departmentID int64) bool {
	if IsAdminUser(c) {
		return true
	}
	if ownerID == 0 {
		// 公开资源 / 系统创建 → 所有登录用户可读
		return true
	}
	currentUserID := c.GetInt64("user_id")
	if ownerID == currentUserID {
		return true
	}
	scope := middleware.GetDataScope(c)
	if scope == nil {
		return false
	}
	switch scope.DataScope {
	case "all":
		return true
	case "self":
		// ownerID == currentUserID 的场景已在 switch 前短路；走到这里说明 owner 不是自己 → 拒绝。
		return false
	case "dept", "dept_and_sub", "self_and_sub_dept":
		if departmentID <= 0 || len(scope.SubDeptIDs) == 0 {
			return false
		}
		for _, id := range scope.SubDeptIDs {
			if id == departmentID {
				return true
			}
		}
		return false
	case "custom":
		if departmentID <= 0 || len(scope.CustomDepts) == 0 {
			return false
		}
		for _, id := range scope.CustomDepts {
			if id == departmentID {
				return true
			}
		}
		return false
	}
	return false
}
