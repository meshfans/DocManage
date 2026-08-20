package handlers

import (
	"net/http"
	"strconv"

	"doc/database"
	"doc/middleware"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type DepartmentHandler struct{}

func NewDepartmentHandler() *DepartmentHandler {
	return &DepartmentHandler{}
}

func (h *DepartmentHandler) GetDepartmentTree(c *gin.Context) {
	tree, err := database.GetDepartmentTree()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取部门树失败")
		return
	}

	if tree == nil {
		tree = []*database.DepartmentTree{}
	}

	fields := c.Query("fields")
	filteredTree := FilterFields(tree, fields)

	utils.Success(c, filteredTree)
}

func (h *DepartmentHandler) GetDepartment(c *gin.Context) {
	// Phase 2d (Critical #4)：GetDepartment 加 dept:list 权限码。
	// GetDepartmentTree 仍公开（设计意图：登录用户可见组织树）。
	if !RequirePermission(c, "dept:list") {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的部门ID")
		return
	}

	dept, err := database.GetDepartmentByID(id)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "部门不存在")
		return
	}

	utils.Success(c, dept)
}

func (h *DepartmentHandler) CreateDepartment(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	var req struct {
		Name      string `json:"name" binding:"required"`
		ParentID  int64  `json:"parent_id"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.SortOrder == 0 {
		req.SortOrder = 1
	}

	if req.Status == "" {
		req.Status = "active"
	}

	maxLevel := services.GetMaxDepartmentLevel()
	id, err := database.CreateDepartment(req.Name, req.ParentID, req.SortOrder, req.Status, maxLevel)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"id":      id,
		"message": "部门创建成功",
	})
}

func (h *DepartmentHandler) UpdateDepartment(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的部门ID")
		return
	}

	var req struct {
		Name      string `json:"name" binding:"required"`
		ParentID  int64  `json:"parent_id"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.SortOrder == 0 {
		req.SortOrder = 1
	}
	if req.Status == "" {
		req.Status = "active"
	}

	maxLevel := services.GetMaxDepartmentLevel()
	err = database.UpdateDepartment(id, req.Name, req.ParentID, req.SortOrder, req.Status, maxLevel)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 2026-06-29 RBAC v3 P5：部门 parent_id / status 变更影响 SubDeptIDs，
	// 精准失效"该部门子树内用户"的缓存（避免 200 用户全清）。
	middleware.InvalidateDataScopeCacheByDeptSubtree(id)

	utils.Success(c, gin.H{
		"message": "部门更新成功",
	})
}

func (h *DepartmentHandler) DeleteDepartment(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的部门ID")
		return
	}

	childCount, err := database.CountDepartmentChildren(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "检查子部门失败")
		return
	}
	if childCount > 0 {
		utils.Error(c, http.StatusBadRequest, "无法删除部门，该部门还有子部门")
		return
	}

	userCount, err := database.CountDepartmentUsers(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "检查部门用户失败")
		return
	}
	if userCount > 0 {
		utils.Error(c, http.StatusBadRequest, "无法删除部门，该部门还有用户")
		return
	}

	err = database.DeleteDepartment(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"message": "部门删除成功",
	})
}

func (h *DepartmentHandler) GetDepartmentUsers(c *gin.Context) {
	// 2026-06-29 RBAC v3 P0：补 dept:users 权限码校验。
	// 此前任何登录用户可查任何部门的成员（潜在人员信息泄漏）。
	if !RequirePermission(c, "dept:users") {
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
