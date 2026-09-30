package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"doc/config"
	"doc/database"
	"doc/middleware"
	"doc/models"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type UserExtendedHandler struct {
	cfg *config.Config
}

func NewUserExtendedHandler(cfg *config.Config) *UserExtendedHandler {
	return &UserExtendedHandler{cfg: cfg}
}

func (h *UserExtendedHandler) GetUsers(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
		utils.Err(c, utils.CodeInternal, "获取用户列表失败")
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
	// Phase 2b (High #17)：CheckUsername 可被攻击者用于"用户名枚举"。
	// 仅 admin 可调。普通用户自查自己用户名用 /api/users/me。
	if !RequireAdmin(c) {
		return
	}

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
		utils.Err(c, utils.CodeInternal, "查询用户名失败")
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
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
		utils.Err(c, utils.CodeInvalidParam, "用户名至少需要3个字符")
		return
	}

	if req.Password == "" || len(req.Password) < 6 {
		utils.Err(c, utils.CodeInvalidParam, "密码至少需要6个字符")
		return
	}

	existingUser, err := database.GetUserByUsername(req.Username)
	if err == nil && existingUser != nil {
		utils.Err(c, utils.CodeConflict, "用户名已存在")
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
			utils.Err(c, utils.CodeConflict, "用户名已存在")
			return
		}
		utils.Err(c, utils.CodeInternal, err.Error())
		return
	}

	// 审计：用户创建（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, userID, "user.create", gin.H{
		"username": req.Username,
		"status":   req.Status,
	})

	utils.Success(c, gin.H{
		"id":      userID,
		"message": "用户创建成功",
	})
}

func (h *UserExtendedHandler) GetUser(c *gin.Context) {
	// Phase 2b (Critical #3)：普通用户只能查自己的资料；admin 可查任意用户。
	// 等价"自我资料"接口走 /api/users/me，不要在 GetUser 上放宽到 admin only。
	if !IsAdminUser(c) {
		currentUserID := c.GetInt64("user_id")
		idStr := c.Param("id")
		targetID, parseErr := strconv.ParseInt(idStr, 10, 64)
		if parseErr == nil && targetID != currentUserID {
			utils.Err(c, utils.CodeForbidden, "普通用户只能查看自己的资料")
			return
		}
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	user, err := database.GetUserByID(id)
	if err != nil {
		utils.Err(c, utils.CodeNotFound, "用户不存在")
		return
	}

	utils.Success(c, user)
}

func (h *UserExtendedHandler) UpdateUser(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
		Avatar       string   `json:"avatar"`
		Roles        []string `json:"roles"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.Username != "" {
		existingUser, err := database.GetUserByUsername(req.Username)
		if err == nil && existingUser != nil && existingUser.ID != id {
			utils.Err(c, utils.CodeConflict, "用户名已存在")
			return
		}
	}

	if req.Password != "" && len(req.Password) < 6 {
		utils.Err(c, utils.CodeInvalidParam, "密码至少需要6个字符")
		return
	}

	if req.Status == "" {
		req.Status = "active"
	}

	err = database.UpdateUserDetails(id, req.Username, req.Password, req.Nickname, req.RealName, req.Email, req.Phone,
		req.Position, req.EmployeeNo, req.Status, req.Avatar, req.DepartmentID)
	if err != nil {
		utils.Err(c, utils.CodeInternal, err.Error())
		return
	}

	// 角色变更：单独更新 + 失效权限缓存
	if req.Roles != nil {
		oldUser, _ := database.GetUserByID(id)
		if oldUser == nil {
			utils.Err(c, utils.CodeNotFound, "用户不存在")
			return
		}
		// 保护：admin 用户安全检查（防止误删 admin 角色导致系统无管理员）
		if err := EnsureAdminSafety(id, oldUser.Username, req.Roles); err != nil {
			utils.BadRequest(c, err.Error())
			return
		}
		if err := database.UpdateUserRoles(id, req.Roles); err != nil {
			utils.Err(c, utils.CodeInternal, "更新角色失败: "+err.Error())
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

	// 审计：用户更新（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, id, "user.update", gin.H{
		"username":  req.Username,
		"has_roles": req.Roles != nil,
	})

	utils.Success(c, gin.H{
		"message": "用户更新成功",
	})
}

// AssignUserRoles POST /api/users/:id/roles
// 单独更新用户的角色分配（不修改其他字段）
// body: { roles: ["manager", "common"] }
func (h *UserExtendedHandler) AssignUserRoles(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
			utils.Err(c, utils.CodeInternal, "查询角色失败: "+err.Error())
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
		utils.Err(c, utils.CodeNotFound, "用户不存在")
		return
	}

	// 保护：admin 用户安全检查（防止误删 admin 角色导致系统无管理员）
	if err := EnsureAdminSafety(id, oldUser.Username, req.Roles); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	if err := database.UpdateUserRoles(id, req.Roles); err != nil {
		utils.Err(c, utils.CodeInternal, "更新角色失败: "+err.Error())
		return
	}

	// 失效缓存
	InvalidatePermsCache(oldUser.Username)
	InvalidatePermissionVersionCache()
	// 2026-06-28 RBAC v3：用户角色变更影响 data_scope → 清该用户缓存
	middleware.InvalidateDataScopeCache(id)

	// Round 19 #6：用户角色分配是高敏感操作，写 audit_log。
	// target_type='rbac_user_binding'，action='assign'，detail 含 old/new roles + username。
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, id, "assign", gin.H{
		"username":  oldUser.Username,
		"old_roles": oldUser.Roles, // JSON 字符串原样存
		"new_roles": req.Roles,
	})

	utils.Success(c, gin.H{
		"id":    id,
		"roles": req.Roles,
	})
}

func (h *UserExtendedHandler) DeleteUser(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
		utils.Err(c, utils.CodeInternal, "删除用户失败")
		return
	}

	// 审计：用户删除（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, id, "user.delete", gin.H{})

	utils.Success(c, gin.H{
		"message": "用户删除成功",
	})
}

// GetAvatar GET /api/users/:id/avatar
// 获取用户头像（返回文件流，支持 Blob 渲染）
func (h *UserExtendedHandler) GetAvatar(c *gin.Context) {
	idStr := c.Param("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的用户ID")
		return
	}

	user, err := database.GetUserByID(userID)
	if err != nil || user == nil {
		utils.Err(c, utils.CodeNotFound, "用户不存在")
		return
	}

	if user.Avatar == "" {
		utils.Err(c, utils.CodeNotFound, "用户未设置头像")
		return
	}

	// 拼接绝对路径
	// DB 中存的是相对 Upload.Dir 的路径，如 "avatar/1.jpg"
	avatarRel := filepath.Join("avatar", filepath.Base(user.Avatar))
	var absPath string
	if filepath.IsAbs(avatarRel) {
		absPath = avatarRel
	} else {
		absPath = filepath.Join(h.cfg.Upload.Dir, avatarRel)
	}
	utils.Info("[GetAvatar] user.Avatar=%q → absPath=%q (Upload.Dir=%q)", user.Avatar, absPath, h.cfg.Upload.Dir)
	if _, err := os.Stat(absPath); err != nil {
		utils.Err(c, utils.CodeNotFound, "头像文件不存在: "+absPath)
		return
	}

	// 根据扩展名设置 Content-Type
	ext := strings.ToLower(filepath.Ext(absPath))
	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}

	c.Header("Content-Type", contentType)
	c.File(absPath)
}

// UploadAvatar POST /api/users/:id/avatar
// 上传用户头像（前端导出 webp，后端直接保存到 avatar/{id}.webp，覆盖模式）
// 仅本人或管理员可操作。
func (h *UserExtendedHandler) UploadAvatar(c *gin.Context) {
	idStr := c.Param("id")
	currentUserID := c.GetInt64("user_id")

	// 支持 id="me" 表示当前登录用户本人
	targetUserID := currentUserID
	if idStr != "me" {
		var err error
		targetUserID, err = strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			utils.BadRequest(c, "无效的用户ID")
			return
		}
	}

	// 权限校验：本人或管理员
	if !IsAdminUser(c) && currentUserID != targetUserID {
		utils.Err(c, utils.CodeForbidden, "只能修改自己的头像")
		return
	}

	// 用户存在性校验
	user, err := database.GetUserByID(targetUserID)
	if err != nil || user == nil {
		utils.Err(c, utils.CodeNotFound, "用户不存在")
		return
	}

	// 读取文件
	file, err := c.FormFile("file")
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "请选择头像图片")
		return
	}

	// 限制大小 5MB
	if file.Size > 5*1024*1024 {
		utils.Err(c, utils.CodeInvalidParam, "头像图片不能超过 5MB")
		return
	}

	// 限制格式
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
		utils.Err(c, utils.CodeInvalidParam, "仅支持 jpg、png、gif、webp 格式")
		return
	}

	// 打开文件
	f, err := file.Open()
	if err != nil {
		utils.Err(c, utils.CodeInternal, "读取文件失败")
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "读取文件失败")
		return
	}

	// 创建 avatar 目录（使用配置的 upload.dir）
	avatarDir := filepath.Join(h.cfg.Upload.Dir, "avatar")
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		utils.Err(c, utils.CodeInternal, "创建头像目录失败")
		return
	}

	// 旧扩展名文件清理（png → webp 覆盖时避免堆积）
	if user.Avatar != "" {
		if oldExt := strings.ToLower(filepath.Ext(user.Avatar)); oldExt != "" && oldExt != ext {
			oldPath := filepath.Join(avatarDir, fmt.Sprintf("%d%s", targetUserID, oldExt))
			if _, statErr := os.Stat(oldPath); statErr == nil {
				os.Remove(oldPath)
			}
		}
	}

	avatarPath := filepath.Join(avatarDir, fmt.Sprintf("%d%s", targetUserID, ext))
	if err := os.WriteFile(avatarPath, data, 0644); err != nil {
		utils.Err(c, utils.CodeInternal, "保存头像失败")
		return
	}

	// 更新数据库（相对 Upload.Dir 的路径，不再含 "uploads" 前缀）
	// 例：Upload.Dir = "./bin/uploads/" → DB 存 "avatar/1.webp"（前端已导出 webp）
	relativePath := filepath.Join("avatar", fmt.Sprintf("%d%s", targetUserID, ext))
	if err := database.UpdateUserAvatar(targetUserID, relativePath); err != nil {
		// 清理文件
		os.Remove(avatarPath)
		utils.Err(c, utils.CodeInternal, "更新头像失败")
		return
	}

	// 审计
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, targetUserID, "avatar.update", gin.H{})

	utils.Success(c, gin.H{
		"avatar":  relativePath,
		"message": "头像上传成功",
	})
}

func (h *UserExtendedHandler) UpdateUserDepartment(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
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
		utils.Err(c, utils.CodeInternal, "调整部门失败")
		return
	}
	// 2026-06-28 RBAC v3：用户主部门变更 → 影响 data_scope=dept/dept_and_sub/self_and_sub_dept → 清该用户缓存
	middleware.InvalidateDataScopeCache(id)

	// 审计：用户部门变更（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetRBACUserBinding, id, "user.department.change", gin.H{
		"department_id_set": req.DepartmentID != nil,
	})

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
		utils.Err(c, utils.CodeInternal, "获取部门用户失败")
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
