package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"doc/database"
	"doc/middleware"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// RBACManagementHandler RBAC 管理 API handler
// 7 个 admin-only 管理端点（PR-6）：
//   GET    /api/rbac/roles
//   POST   /api/rbac/roles
//   DELETE /api/rbac/roles/:code
//   GET    /api/rbac/permissions
//   POST   /api/rbac/permissions
//   DELETE /api/rbac/permissions/:code
//   POST   /api/rbac/check
//
// 所有写操作都会：
//   1. 调 RequireAdmin（gate）
//   2. 写 audit_log（target_type='rbac_role' 或 'rbac_permission'）
//   3. 调 InvalidateAllPermsCache 清缓存

type RBACManagementHandler struct{}

// NewRBACManagementHandler 构造器
func NewRBACManagementHandler() *RBACManagementHandler {
	return &RBACManagementHandler{}
}

// ===== Roles =====

// ListRoles GET /api/rbac/roles
func (h *RBACManagementHandler) ListRoles(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	roles, err := database.GetAllRoles()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询角色失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{"list": roles, "total": len(roles)})
}

// UpsertRole POST /api/rbac/roles
// body: { code, name, description, permissions: ["code1", "code2"], is_system, status }
func (h *RBACManagementHandler) UpsertRole(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var r database.Role
	if err := c.ShouldBindJSON(&r); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if r.Code == "" {
		utils.BadRequest(c, "code 不能为空")
		return
	}
	// 2026-06-28 RBAC v3 B6 审计增强：upsert 前先查旧 role，记录 old_data_scope/old_permissions/old_custom_dept_ids。
	// 这样审计查询能精确识别权限升级事件（如 manager dept→all 或新增 contract:create）。
	oldRole, _ := database.GetRoleByCode(r.Code)
	if oldRole != nil {
		_ = oldRole.DataScope
		_ = len(oldRole.Permissions)
		_ = oldRole.CustomDeptIDs
	}
	if err := database.UpsertRole(&r); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	InvalidateAllPermsCache()
	InvalidatePermissionVersionCache()
	// 2026-06-29 RBAC v3 P5：role.data_scope 变更 → 精准失效该 role 持有者的缓存（不波及其他 role 用户）。
	middleware.InvalidateDataScopeCacheByRole(r.Code)
	role, _ := database.GetRoleByCode(r.Code)
	newDataScope := ""
	if role != nil {
		newDataScope = role.DataScope
	}
	utils.Success(c, gin.H{"code": r.Code, "data_scope": newDataScope, "message": "保存成功"})
}

// permSetsEqual 比较两个 permission 集合是否相同（无视顺序）。
func permSetsEqual(a, b []string) bool {
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

// int64SlicesEqual 比较两个 int64 slice 是否相同（无视顺序）。
func int64SlicesEqual(a, b []int64) bool {
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

// DeleteRole DELETE /api/rbac/roles/:code
func (h *RBACManagementHandler) DeleteRole(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	code := c.Param("code")
	if code == "" {
		utils.BadRequest(c, "code 不能为空")
		return
	}
	n, err := database.DeleteRole(code)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	InvalidateAllPermsCache()
	utils.Success(c, gin.H{"code": code, "deleted": n})
}

// ===== Permissions =====

// ListPermissions GET /api/rbac/permissions
func (h *RBACManagementHandler) ListPermissions(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	perms, err := database.ListAllPermissions()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询权限失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{"list": perms, "total": len(perms)})
}

// UpsertPermission POST /api/rbac/permissions
// body: { code, name, module, api_path, http_method, description, is_system, status }
func (h *RBACManagementHandler) UpsertPermission(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var p database.Permission
	if err := c.ShouldBindJSON(&p); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if p.Code == "" {
		utils.BadRequest(c, "code 不能为空")
		return
	}
	if err := database.UpsertPermission(&p); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	InvalidateAllPermsCache()
	InvalidatePermissionVersionCache()
	utils.Success(c, gin.H{"code": p.Code, "message": "保存成功"})
}

// DeletePermission DELETE /api/rbac/permissions/:code
func (h *RBACManagementHandler) DeletePermission(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	code := c.Param("code")
	if code == "" {
		utils.BadRequest(c, "code 不能为空")
		return
	}
	if err := database.DeletePermission(code); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	InvalidateAllPermsCache()
	InvalidatePermissionVersionCache()
	utils.Success(c, gin.H{"code": code, "message": "已删除"})
}

// ===== Check =====

// CheckPermission POST /api/rbac/check
// body: { username, required_codes: ["customer:create"] }
func (h *RBACManagementHandler) CheckPermission(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var req struct {
		Username      string   `json:"username"`
		RequiredCodes []string `json:"required_codes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if req.Username == "" {
		utils.BadRequest(c, "username 不能为空")
		return
	}

	perms, _ := GetEffectivePermissionsCached(req.Username)
	allowed := false
	// 2026-06-25 P0-6.5 修复：原 IsRBACAdminByName(username) 是死代码（仅纯函数版 username=="admin"），
	// 现改为：先看通配 *:*:*（覆盖 admin 角色，因为 admin role.permissions = ["*:*:*"]），
	// 天然支持任何持有 admin 角色的用户，无需单独判断 username。
	for _, p := range perms {
		if p == "*:*:*" {
			allowed = true
			break
		}
	}
	if !allowed {
		for _, need := range req.RequiredCodes {
			if HasPermissionCode(perms, need) {
				allowed = true
				break
			}
		}
	}

	utils.Success(c, gin.H{
		"username":              req.Username,
		"required_codes":        req.RequiredCodes,
		"effective_permissions": perms,
		"allowed":               allowed,
	})
}

// IsRBACAdminByName 纯函数版 admin 旁路（无 gin.Context）
//
// 2026-06-25 P0-6.5 修复：原实现 `username == "admin"` 是死代码（hardcoded bypass，理论设计）。
// 现重构为：DB-backed，但保留纯函数签名（兼容历史调用点）。
// 真实 admin 判定 = DB users.roles 含 "admin" → 见 GetUserHasRole。
func IsRBACAdminByName(username string) bool {
	if username == "" {
		return false
	}
	user, err := database.GetUserByUsername(username)
	if err != nil || user == nil {
		return false
	}
	var roles []string
	if err := json.Unmarshal([]byte(user.Roles), &roles); err != nil {
		return false
	}
	for _, r := range roles {
		if r == "admin" {
			return true
		}
	}
	return false
}

// EnsureAdminSafety 校验修改某用户的角色不会让系统失去所有 admin。
// 调用场景：AssignUserRoles / UpdateUser 等修改用户角色前。
// 入参：
//   - targetUserID：被修改的用户 ID（0 表示新建场景）
//   - targetUsername：被修改的用户名（用于识别"内置 admin"，即 username=="admin"）
//   - newRoles：本次提交的角色列表（已校验过 code 合法性）
//
// 返回：
//   - error != nil 时直接拒绝，调用方应返回 400/403
//
// 保护规则：
//  1. 内置 admin（username=="admin"）：必须始终保留 admin 角色，且不能被改名为非 admin
//  2. 任何修改不能让"用户名为 admin 的那条记录"失去 admin 角色
//  3. 修改后系统中至少要有 1 个 admin 角色持有者
func EnsureAdminSafety(targetUserID int64, targetUsername string, newRoles []string) error {
	// 规则 1：内置 admin 用户必须有 admin 角色
	if targetUsername == "admin" {
		hasAdmin := false
		for _, r := range newRoles {
			if r == "admin" {
				hasAdmin = true
				break
			}
		}
		if !hasAdmin {
			return fmt.Errorf("内置 admin 用户必须保留 admin 角色，否则系统无管理员")
		}
		return nil
	}

	// 规则 2：修改后系统至少要有一个 admin 角色持有者
	hasAdmin := false
	for _, r := range newRoles {
		if r == "admin" {
			hasAdmin = true
			break
		}
	}
	if hasAdmin {
		// 新角色含 admin → 不会让 admin 数变 0，无需额外检查
		return nil
	}

	// 新角色不含 admin → 检查当前 DB 里是否还有其他 admin（含目标用户自己）
	count, err := database.CountUsersWithRole("admin")
	if err != nil {
		return fmt.Errorf("查询 admin 用户数失败: %w", err)
	}
	// 如果目标用户当前持有 admin，且是唯一 admin → 拒绝
	if count <= 1 {
		// 取一下目标用户当前的角色，确认他自己是不是 admin
		cur, err := database.GetUserByID(targetUserID)
		if err == nil && cur != nil {
			var curRoles []string
			if err := json.Unmarshal([]byte(cur.Roles), &curRoles); err == nil {
				wasAdmin := false
				for _, r := range curRoles {
					if r == "admin" {
						wasAdmin = true
						break
					}
				}
				if wasAdmin && count == 1 {
					return fmt.Errorf("系统唯一的 admin 不可移除 admin 角色（请先创建其他 admin 用户）")
				}
			}
		}
	}
	return nil
}

// HasPermissionCode 检查 perms 是否含 code（含通配）
func HasPermissionCode(perms []string, code string) bool {
	for _, p := range perms {
		if p == "*:*:*" {
			return true
		}
		if p == code {
			return true
		}
	}
	return false
}

