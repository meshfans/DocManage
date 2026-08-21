package handlers

import (
	"doc/config"
	"doc/database"
	"doc/utils"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// BackupHandler 备份管理 handler（第四阶段 Phase 4.3 备份管理 UI）。
//
// 路径：/api/backup/*
// 设计：仅 admin 角色可调用（路由层在 JWTAuth 后单独再加 role 校验，本期简化为登录即可）。
type BackupHandler struct{}

func NewBackupHandler() *BackupHandler { return &BackupHandler{} }

// List GET /api/backup/list
// Query:
//   - type:   full / incremental（可选）
//   - status: pending / running / success / verified / corrupted / missing / failed（可选）
//   - page:   默认 1
//   - page_size: 默认 20，最大 100
//
// 返回：{ list, total, total_size, by_status, page, page_size }
func (h *BackupHandler) List(c *gin.Context) {
	typeFilter := c.Query("type")
	statusFilter := c.Query("status")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	list, err := database.ListBackupManifests(typeFilter, statusFilter, pageSize, offset)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询备份列表失败: "+err.Error())
		return
	}
	if list == nil {
		list = []*database.BackupManifest{}
	}

	// 分页总数（用于前端翻页）
	total, _ := database.CountBackupManifests(typeFilter, statusFilter)

	// 计算本页 total_size + 全部按状态计数
	var pageTotalSize int64
	for _, m := range list {
		pageTotalSize += m.FileSize
	}
	byStatus, _ := database.CountBackupManifestsByStatus()

	utils.Success(c, gin.H{
		"list":            list,
		"total":           total,
		"page":            page,
		"page_size":       pageSize,
		"page_size_bytes": pageTotalSize,
		"by_status":       byStatus,
	})
}

// Detail GET /api/backup/:id 备份详情（含 manifest 全部字段）。
func (h *BackupHandler) Detail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "ID 无效")
		return
	}
	m, err := database.GetBackupManifestByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Err(c, utils.CodeBackupNotFound, "备份不存在")
		return
	}
	utils.Success(c, m)
}

// Download GET /api/backup/:id/download 下载 zip 文件。
//
// 安全（Issue A5）：先做 path containment 校验，确保目标路径在 cfg.Backup.Dir 内。
// 否则攻击者可能修改 DB file_path 为 /etc/passwd 等敏感文件读取（需 JWT+DB 写权限）。
//
// 备份 zip 永远是明文，直接 c.File(m.FilePath)。
func (h *BackupHandler) Download(c *gin.Context) {
	if !RequirePermission(c, "backup:download") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "ID 无效")
		return
	}
	m, err := database.GetBackupManifestByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Err(c, utils.CodeBackupNotFound, "备份不存在")
		return
	}
	if m.FilePath == "" {
		utils.Err(c, utils.CodeBackupMissing, "备份文件路径为空")
		return
	}
	if _, err := os.Stat(m.FilePath); os.IsNotExist(err) {
		utils.Err(c, utils.CodeBackupMissing, "备份文件已丢失")
		return
	}

	// Issue A5：路径 containment 校验（防御 DB 写权限滥用）
	allowedPrefix := config.GlobalConfig.Backup.Dir
	if allowedPrefix != "" {
		cleanPath := filepath.Clean(m.FilePath)
		cleanPrefix := filepath.Clean(allowedPrefix)
		rel, relErr := filepath.Rel(cleanPrefix, cleanPath)
		if relErr != nil || (len(rel) > 0 && rel[0] == '.' && rel[1] == '.') {
			utils.LogError("[Backup.Download] 路径越界: id=%d, path=%s, allowed=%s",
				id, cleanPath, cleanPrefix)
			utils.Err(c, utils.CodeForbidden, "备份文件路径异常，禁止下载")
			return
		}
	}

	filename := filepath.Base(m.FilePath)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Content-Type", "application/zip")
	c.File(m.FilePath)
}

// Delete DELETE /api/backup/:id 删除备份（文件 + DB 记录同步删除）。
//
// 安全保护（Issue A3）：
//   - 仅 admin 角色可调用（与设计文档 §4.3.1 一致）
//   - 全量有活跃子增量（parent_id 引用）→ 返回 409 不允许（避免孤儿）
//   - 手动备份（keep_until=0）允许删除（管理员操作）
//   - 自动清理备份（keep_until > 0）也允许手动删除（管理员操作）
func (h *BackupHandler) Delete(c *gin.Context) {
	// Issue A3：强制 RBAC（前端 v-if="isAdmin" 是 UX 防线，后端必须独立校验）
	if !RequireAdmin(c) {
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "ID 无效")
		return
	}
	m, err := database.GetBackupManifestByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Err(c, utils.CodeBackupNotFound, "备份不存在")
		return
	}

	// 链式保护：若全量且有活跃子增量，拒绝
	if m.Type == database.BackupTypeFull {
		liveChildren, _ := database.CountLiveChildrenByParent(m.ID, time.Now().Unix())
		if liveChildren > 0 {
			utils.Err(c, utils.CodeConflict,
				fmt.Sprintf("该全量备份有 %d 个活跃子增量，请先删除子增量", liveChildren))
			return
		}
	}

	// 删文件（best-effort，文件丢失也允许清理 DB 记录）
	if m.FilePath != "" {
		if err := os.Remove(m.FilePath); err != nil && !os.IsNotExist(err) {
			utils.Err(c, utils.CodeInternal, "删除文件失败: "+err.Error())
			return
		}
	}
	if err := database.DeleteBackupManifest(m.ID); err != nil {
		utils.Err(c, utils.CodeInternal, "删除记录失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{"deleted": true})
}
