package handlers

// 本文件：data_scope 感知的访问控制 helper（统一版）。
//
// 2026-06-29 RBAC v3 P2 重构：替代 5 个 handler 中的重复实现
//   - customer.go      customerDataScopeAllows
//   - third_party.go   thirdPartyDataScopeAllows
//   - seal.go          (SealHandler).canAccess
//   - media.go         (MediaHandler).canAccess
//   - template.go      (TemplateHandler).canAccess
//
// 5 个函数体几乎完全相同（仅"owner==0 含义"措辞差异），handover §11 已点名重构。
// 统一后业务逻辑只此一份，避免漂移。
//
// 2026-07-04 移除 flow_instance.go 的 flowInstanceDataScopeAllows：
//   原因：flow 的 GetMyPendingTasks / GetInstance / GetFlowHistory 三处都不应该用 data_scope 二级校验
//   （语义是"我参与的流转"，不是"我能看哪些资源"）。详情/办理权限下沉到 assignee 匹配。
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
//   - ownerID: 资源所有者（customer.OwnerUserID / seal.UserID / media.TakenBy /
//     flow_instance.StartedBy / third_party.CreatedBy 等）
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
		// 2026-07-04 修复：common 角色 data_scope=self，原 switch 缺 case 走 default 全部拒绝，
		// 导致 /flows/my-pending 二级过滤把"分配给我但不是我发起的"待办全部过滤掉。
		// ownerID == currentUserID 的场景已在 switch 前短路；走到这里说明 owner 不是自己 → 拒绝（与 self 语义一致）。
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
