package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"doc/database"
	"doc/middleware"
	"doc/models"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type UserExtendedHandler struct{}

func NewUserExtendedHandler() *UserExtendedHandler {
	return &UserExtendedHandler{}
}

func (h *UserExtendedHandler) GetUsers(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	page := 1
	pageSize := 20
	search := ""
	fields := ""

	if c.Query("page") != "" {
		if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
			page = p
		}
	}

	if c.Query("page_size") != "" {
		if ps, err := strconv.Atoi(c.Query("page_size")); err == nil && ps > 0 {
			pageSize = ps
		}
	}

	if c.Query("search") != "" {
		search = c.Query("search")
	}

	fields = c.Query("fields")

	users, total, err := database.GetUsersWithDepartmentPaginated(page, pageSize, search)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取用户列表失败")
		return
	}

	if users == nil {
		users = []database.UserWithDepartment{}
	}

	// 关键：database.UserWithDepartment.Roles 是 JSON 字符串，必须经 ToAPIModel 转为 []string
	// 否则前端 `[...user.roles]` 会把字符串拆成字符数组（如 "[\"admin\"]" → ['[','"','a','d',...]）
	apiUsers := make([]*models.User, 0, len(users))
	for i := range users {
		api := users[i].ToAPIModel()
		api.DepartmentName = users[i].DepartmentName
		apiUsers = append(apiUsers, api)
	}

	filteredUsers := FilterFields(apiUsers, fields)

	utils.Success(c, gin.H{
		"list":        filteredUsers,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + pageSize - 1) / pageSize,
	})
}

func (h *UserExtendedHandler) CheckUsername(c *gin.Context) {
	username := c.Query("username")
	excludeIdStr := c.Query("exclude_id")

	if username == "" {
		utils.BadRequest(c, "用户名不能为空")
		return
	}

	user, err := database.GetUserByUsername(username)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Success(c, gin.H{
				"exists": false,
			})
			return
		}
		utils.Error(c, http.StatusInternalServerError, "查询用户名失败")
		return
	}

	var excludeId int64 = 0
	if excludeIdStr != "" {
		excludeId, _ = strconv.ParseInt(excludeIdStr, 10, 64)
	}

	if user.ID == excludeId {
		utils.Success(c, gin.H{
			"exists": false,
		})
		return
	}

	utils.Success(c, gin.H{
		"exists": true,
	})
}

func (h *UserExtendedHandler) CreateUser(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	var req struct {
		Username     string `json:"username" binding:"required"`
		Password     string `json:"password" binding:"required"`
		Nickname     string `json:"nickname"`
		RealName     string `json:"real_name"`
		Email        string `json:"email"`
		Phone        string `json:"phone"`
		Position     string `json:"position"`
		EmployeeNo   string `json:"employee_no"`
		DepartmentID *int64 `json:"department_id"`
		Status       string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.Username == "" || len(req.Username) < 3 {
		utils.Error(c, http.StatusBadRequest, "用户名至少需要3个字符")
		return
	}

	if req.Password == "" || len(req.Password) < 6 {
		utils.Error(c, http.StatusBadRequest, "密码至少需要6个字符")
		return
	}

	existingUser, err := database.GetUserByUsername(req.Username)
	if err == nil && existingUser != nil {
		utils.Error(c, http.StatusBadRequest, "用户名已存在")
		return
	}

	if req.Nickname == "" {
		req.Nickname = req.Username
	}

	if req.Status == "" {
		req.Status = "active"
	}

	userID, err := database.CreateUserExt(req.Username, req.Password, req.Nickname, req.RealName, req.Email, req.Phone, req.Position, req.EmployeeNo, req.Status, req.DepartmentID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			utils.Error(c, http.StatusBadRequest, "用户名已存在")
			return
		}
		utils.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"id":      userID,
		"message": "用户创建成功",
	})
}

func (h *UserExtendedHandler) GetUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	user, err := database.GetUserByID(id)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "用户不存在")
		return
	}

	utils.Success(c, user)
}

func (h *UserExtendedHandler) UpdateUser(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	var req struct {
		Username     string   `json:"username"`
		Password     string   `json:"password"`
		Nickname     string   `json:"nickname"`
		RealName     string   `json:"real_name"`
		Email        string   `json:"email"`
		Phone        string   `json:"phone"`
		Position     string   `json:"position"`
		EmployeeNo   string   `json:"employee_no"`
		DepartmentID *int64   `json:"department_id"`
		Status       string   `json:"status"`
		Roles        []string `json:"roles"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.Username != "" {
		existingUser, err := database.GetUserByUsername(req.Username)
		if err == nil && existingUser != nil && existingUser.ID != id {
			utils.Error(c, http.StatusBadRequest, "用户名已存在")
			return
		}
	}

	if req.Password != "" && len(req.Password) < 6 {
		utils.Error(c, http.StatusBadRequest, "密码至少需要6个字符")
		return
	}

	if req.Status == "" {
		req.Status = "active"
	}

	err = database.UpdateUserDetails(id, req.Username, req.Password, req.Nickname, req.RealName, req.Email, req.Phone,
		req.Position, req.EmployeeNo, req.Status, req.DepartmentID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 角色变更：单独更新 + 失效权限缓存
	if req.Roles != nil {
		oldUser, _ := database.GetUserByID(id)
		if oldUser == nil {
			utils.Error(c, http.StatusNotFound, "用户不存在")
			return
		}
		// 保护：admin 用户安全检查（防止误删 admin 角色导致系统无管理员）
		if err := EnsureAdminSafety(id, oldUser.Username, req.Roles); err != nil {
			utils.BadRequest(c, err.Error())
			return
		}
		if err := database.UpdateUserRoles(id, req.Roles); err != nil {
			utils.Error(c, http.StatusInternalServerError, "更新角色失败: "+err.Error())
			return
		}
		if oldUser != nil {
			InvalidatePermsCache(oldUser.Username)
		}
		// 权限版本缓存也清空（前端轮询能立即看到）
		InvalidatePermissionVersionCache()
		// 2026-06-28 RBAC v3：用户角色变更影响 data_scope（取 roles[0] 决定 data_scope）→ 清该用户缓存
		middleware.InvalidateDataScopeCache(id)
	}

	utils.Success(c, gin.H{
		"message": "用户更新成功",
	})
}

// AssignUserRoles POST /api/users/:id/roles
// 单独更新用户的角色分配（不修改其他字段）
// body: { roles: ["manager", "common"] }
func (h *UserExtendedHandler) AssignUserRoles(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	var req struct {
		Roles []string `json:"roles"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 校验 role.code 必须存在
	if len(req.Roles) > 0 {
		validRoles, err := database.GetAllRoles()
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "查询角色失败: "+err.Error())
			return
		}
		validSet := make(map[string]bool, len(validRoles))
		for _, r := range validRoles {
			validSet[r.Code] = true
		}
		for _, code := range req.Roles {
			if !validSet[code] {
				utils.BadRequest(c, "未知角色: "+code)
				return
			}
		}
	}

	oldUser, _ := database.GetUserByID(id)
	if oldUser == nil {
		utils.Error(c, http.StatusNotFound, "用户不存在")
		return
	}

	// 保护：admin 用户安全检查（防止误删 admin 角色导致系统无管理员）
	if err := EnsureAdminSafety(id, oldUser.Username, req.Roles); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	if err := database.UpdateUserRoles(id, req.Roles); err != nil {
		utils.Error(c, http.StatusInternalServerError, "更新角色失败: "+err.Error())
		return
	}

	// 失效缓存
	InvalidatePermsCache(oldUser.Username)
	InvalidatePermissionVersionCache()
	// 2026-06-28 RBAC v3：用户角色变更影响 data_scope → 清该用户缓存
	middleware.InvalidateDataScopeCache(id)

	utils.Success(c, gin.H{
		"id":    id,
		"roles": req.Roles,
	})
}

func (h *UserExtendedHandler) DeleteUser(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	err = database.DeleteUser(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "删除用户失败")
		return
	}

	utils.Success(c, gin.H{
		"message": "用户删除成功",
	})
}

func (h *UserExtendedHandler) UpdateUserDepartment(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	var req struct {
		DepartmentID *int64 `json:"department_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	err = database.UpdateUserDepartment(id, req.DepartmentID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "调整部门失败")
		return
	}
	// 2026-06-28 RBAC v3：用户主部门变更 → 影响 data_scope=dept/dept_and_sub/self_and_sub_dept → 清该用户缓存
	middleware.InvalidateDataScopeCache(id)

	utils.Success(c, gin.H{
		"message": "部门调整成功",
	})
}

func (h *UserExtendedHandler) GetDepartmentUsers(c *gin.Context) {
	// 2026-06-29 RBAC v3 P0：补 user:by-department 权限码校验。
	// 此前任何登录用户可查任何部门的成员（潜在人员信息泄漏）。
	if !RequirePermission(c, "user:by-department") {
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的部门ID")
		return
	}

	users, err := database.GetUsersByDepartment(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取部门用户失败")
		return
	}

	if users == nil {
		users = []database.UserWithDepartment{}
	}

	utils.Success(c, gin.H{
		"list":  users,
		"total": len(users),
	})
}
