package handlers

// 本文件：data_scope 感知的访问控制 helper（统一版）。
//
// 调用示例：
//
//	if !CheckDataScopeAccess(c, ownerID, departmentID) {
//	    utils.Error(c, 403, "无权访问")
//	    return
//	}
//
// 2026-09-17 P0-1 修复：新增 EnsureListDataScope，scope=nil 时拒绝请求而非退化到"看全部"。
//
// 调用示例：
//
//	if !EnsureListDataScope(c) {
//	    return
//	}

import (
	"doc/middleware"
	"doc/utils"

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

// EnsureListDataScope 2026-09-17 P0-1 修复：list 端点入口 guard。
// 问题：DataScopeMiddleware 加载失败时注入 nil scope，handler 内 "scope == nil" 被当作 "all" 处理，
// 导致 DB 抖动期间任意登录用户可看到全量数据。
//
// 规则：
//   1. admin → 直接返回 true（全量权限，scope 加载失败不影响）
//   2. 非 admin + scope == nil → 拒绝请求，记录 Error 日志，返回 500
//   3. 非 admin + scope != nil → 返回 true（由调用方判断 scope.DataScope 决定过滤范围）
//
// 注意：本函数仅处理 scope=nil 的安全兜底，不替代各 handler 内 "scope.DataScope == all" 的业务分支。
func EnsureListDataScope(c *gin.Context) bool {
	if IsAdminUser(c) {
		return true
	}
	scope := middleware.GetDataScope(c)
	if scope == nil {
		username := c.GetString("username")
		utils.LogError("[data_scope] scope=nil，拒绝 list 请求以防止全量泄露: username=%s, path=%s, ip=%s",
			username, c.Request.URL.Path, c.ClientIP())
		utils.Err(c, utils.CodeInternal, "数据权限未就绪，请稍后重试（data_scope 加载失败）")
		return false
	}
	if scope.DataScope == "" {
		utils.LogError("[data_scope] scope.DataScope 为空，异常...")
		utils.Err(c, utils.CodeInternal, "数据权限配置异常，请联系管理员")
		return false
	}
	return true
}
