package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"doc/config"
	"doc/database"
	"doc/middleware"
	"doc/models"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// MediaHandler 媒体 12 个 API
// 路由前缀：/api/media
// 鉴权：受 JWTAuth 中间件保护（由 combined_server.go 注入）
type MediaHandler struct {
	cfg *config.Config
}

func NewMediaHandler(cfg *config.Config) *MediaHandler {
	return &MediaHandler{cfg: cfg}
}

// parseCustomerIDFromForm 从 FormData (multipart) 中解析 customer_id
// 注意：upload 接口使用 FormData 传输，不能用 parseInt64Query（那是用于 URL query 参数的）
func parseCustomerIDFromForm(c *gin.Context) int64 {
	v := c.PostForm("customer_id")
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// parseInt64Form 从 FormData (multipart) 中解析 int64 字段
// 失败或 <= 0 时返回 defVal（用于"传了用传的值，没传用默认"的场景）
func parseInt64Form(c *gin.Context, field string, defVal int64) int64 {
	v := c.PostForm(field)
	if v == "" {
		return defVal
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defVal
	}
	return n
}

// parseWatermarkMode 从 FormData (multipart) 中解析 watermark_mode 字段
// 有效值：corner / cross / tile；无效值返回默认 "corner"
func parseWatermarkMode(c *gin.Context) string {
	v := c.PostForm("watermark_mode")
	switch v {
	case "corner", "cross", "tile":
		return v
	}
	return "corner" // 默认值
}

// ==================== 1. 列表（分页 + 过滤）====================
//
//	GET /api/media?type=&source=&status=&taken_by=&from=&to=&q=&page=&page_size=
//
// 默认 status = "" → 仅 active；status = "all" → 不过滤。
// created_by 权限：admin 看全部；普通用户只看自己 (taken_by = 自己)。
func (h *MediaHandler) List(c *gin.Context) {
	currentUserID := c.GetInt64("user_id")
	isAdmin := IsAdminUser(c)

	filter := database.MediaFilter{
		Type:       c.Query("type"),
		Source:     c.Query("source"),
		Status:     c.Query("status"),
		TakenBy:    parseInt64Query(c, "taken_by"),
		CustomerID: parseInt64Query(c, "customer_id"),
		UserID:     parseInt64Query(c, "user_id"),
		FromTs:     parseInt64Query(c, "from"),
		ToTs:       parseInt64Query(c, "to"),
		Q:          strings.TrimSpace(c.Query("q")),
		TagNames:   c.QueryArray("tag_names"),
	}

	// 2026-06-28 RBAC v3 B2 修复：data_scope=self 时强制 taken_by=自己（向后兼容）；
	// 其他 data_scope 模式不过滤，由 BuildWhereSQL 的 department_id 决定范围。
	// 例外：如果已按 customer_id 过滤，则保留该过滤（员工查某客户档案时用）。
	if !isAdmin && filter.TakenBy == 0 && filter.CustomerID == 0 {
		scope := middleware.GetDataScope(c)
		if scope == nil || scope.DataScope == "self" || scope.DataScope == "" {
			filter.TakenBy = currentUserID
		}
		// 其他 scope（dept / dept_and_sub / self_and_sub_dept / custom / all）→ 留空
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// 2026-06-28 RBAC v3 P4：admin 看全部；其他用户按 data_scope 过滤。
	// media 表 owner 是 user_id，department_id 刚加。
	scope := middleware.GetDataScope(c)
	var list []models.Media
	var total int
	var err error
	if scope == nil || scope.DataScope == "all" {
		list, total, err = database.ListMedia(filter, page, pageSize)
	} else {
		whereSQL, args, werr := database.BuildWhereSQL(scope, database.FilterOpts{
			OwnerCol: "user_id",
			DeptCol:  "department_id",
		})
		if werr != nil {
			utils.Error(c, http.StatusInternalServerError, "data_scope 过滤失败: "+werr.Error())
			return
		}
		list, total, err = database.ListMediaByDataScope(filter, page, pageSize, whereSQL, args)
	}
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ==================== 2. 按 target 查询（绑定反查）====================
//
//	GET /api/media/by-target?target_type=contract&target_id=1
func (h *MediaHandler) ListByTarget(c *gin.Context) {
	targetType := c.Query("target_type")
	targetID := parseInt64Query(c, "target_id")
	if targetType == "" || targetID <= 0 {
		utils.BadRequest(c, "缺少 target_type 或 target_id")
		return
	}
	list, err := database.ListMediaByTarget(targetType, targetID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{"list": list, "total": len(list)})
}

// ==================== 3. 详情 ====================
//
//	GET /api/media/:id
//
// 副作用：view_count++ + audit push "view"
//
// 2026-06-27 Bug #6 修复：GetMediaByID 已去掉 deleted_at 过滤，
// 用户从"已删"过滤器打开卡片预览 / 决定是否恢复。
func (h *MediaHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权查看")
		return
	}

	// 副作用：view +1 + audit push（双写）
	// P0 修复（2026-06-14）：用 IncrementViewCountWithAudit 一次完成 view_count++ + JSON 缓存 + 哈希链三件事，
	// 之前 IncrementViewCount + AppendMediaAuditBoth 两次调用会产生 2 条 view 审计。
	currentUserID := c.GetInt64("user_id")
	_ = database.IncrementViewCountWithAudit(id, currentUserID, c.ClientIP(), c.GetHeader("User-Agent"))
	// 重新读（updated audit / view_count）
	m, _ = database.GetMediaByID(id)

	utils.Success(c, m)
}

// ==================== 4. 创建元数据（文件已上传后调）====================
//
//	POST /api/media/create
//	body: {snowid, type, name, original_name, mime_type,
//	       file_path, thumb_path, file_size, width, height, duration,
//	       hash_sm3, hash_sha256, hash_combined,
//	       source, source_ref, watermark_text,
//	       taken_at?, taken_by?, tags?, bindings?,
//	       view_count?, download_count?, remark?, status?}
//
// 注：一般用 POST /api/media/upload 一站式（multipart）；本端点保留给
//
//	"前端已上传到 CDN / 对象存储，metadata 单独入库"的场景。
func (h *MediaHandler) Create(c *gin.Context) {
	var m models.Media
	if err := c.ShouldBindJSON(&m); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if m.SnowID == "" {
		utils.BadRequest(c, "snowid 不能为空")
		return
	}
	if !models.IsValidMediaType(string(m.Type)) {
		utils.BadRequest(c, "非法的 type: "+string(m.Type))
		return
	}
	if m.Source != "" && !models.IsValidMediaSource(string(m.Source)) {
		utils.BadRequest(c, "非法的 source: "+string(m.Source))
		return
	}
	currentUserID := c.GetInt64("user_id")
	m.CreatedBy = currentUserID
	if m.TakenBy == 0 {
		m.TakenBy = currentUserID
	}
	if m.TakenAt == 0 {
		m.TakenAt = time.Now().Unix()
	}
	if m.Status == "" {
		m.Status = models.MediaStatusActive
	}
	id, err := database.CreateMedia(m)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "入库失败: "+err.Error())
		return
	}
	// 审计双写（JSON 缓存 + 哈希链）
	database.AppendMediaAuditBoth(id, "create", currentUserID, c.ClientIP(), c.GetHeader("User-Agent"), "{}")
	created, _ := database.GetMediaByID(id)
	utils.Success(c, gin.H{"id": id, "snowid": m.SnowID, "data": created, "message": "创建成功"})
}

// ==================== 5. 更新元数据 ====================
//
//	POST /api/media/:id
//	body: {name?, remark?, status?, tags?, bindings?}
//
// 空白字段表示"不改"，简化 PATCH 语义。
func (h *MediaHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权修改")
		return
	}

	var req struct {
		Name       *string               `json:"name"`
		Remark     *string               `json:"remark"`
		Status     *string               `json:"status"`
		CustomerID *int64                `json:"customer_id"` // > 0 = 关联到该客户；nil = 不变
		UserID     *int64                `json:"user_id"`     // > 0 = 设值；nil = 不变
		Tags       []models.MediaTag     `json:"tags"`
		Bindings   []models.MediaBinding `json:"bindings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求: "+err.Error())
		return
	}

	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	remark := ""
	if req.Remark != nil {
		remark = *req.Remark
	}
	status := ""
	if req.Status != nil {
		if !models.IsValidMediaStatus(*req.Status) {
			utils.BadRequest(c, "非法的 status: "+*req.Status)
			return
		}
		status = *req.Status
	}
	// 客户关联：> 0 才更新（nil = 不变，0 = 不变，> 0 = 设值）
	customerID := int64(0)
	if req.CustomerID != nil && *req.CustomerID > 0 {
		customerID = *req.CustomerID
	}
	userID := int64(0)
	if req.UserID != nil && *req.UserID > 0 {
		userID = *req.UserID
	}

	if err := database.UpdateMediaMeta(id, name, remark, status, customerID, userID, req.Tags, req.Bindings); err != nil {
		utils.Error(c, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	// 审计双写（JSON 缓存 + 哈希链）
	database.AppendMediaAuditBoth(id, "update", c.GetInt64("user_id"), c.ClientIP(), c.GetHeader("User-Agent"), "{}")
	utils.Success(c, gin.H{"message": "更新成功"})
}

// ==================== 6. 软删 ====================
//
//	POST /api/media/:id/delete
func (h *MediaHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权删除")
		return
	}
	if err := database.DeleteMedia(id); err != nil {
		utils.Error(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	// 审计双写
	database.AppendMediaAuditBoth(id, "delete", c.GetInt64("user_id"), c.ClientIP(), c.GetHeader("User-Agent"), "{}")
	utils.Success(c, gin.H{"message": "已删除"})
}

// ==================== 7. 恢复 ====================
//
//	POST /api/media/:id/restore
func (h *MediaHandler) Restore(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	// Restore 不限制 deleted_at = 0（否则查不到）
	m := &models.Media{}
	err = database.DB.QueryRow(`SELECT id, taken_by, status FROM media WHERE id = ?`, id).Scan(&m.ID, &m.TakenBy, &m.Status)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m.Status != models.MediaStatusDeleted {
		utils.BadRequest(c, "该媒体未删除，无需恢复")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权恢复")
		return
	}
	if err := database.RestoreMedia(id); err != nil {
		utils.Error(c, http.StatusInternalServerError, "恢复失败: "+err.Error())
		return
	}
	// 审计双写
	database.AppendMediaAuditBoth(id, "restore", c.GetInt64("user_id"), c.ClientIP(), c.GetHeader("User-Agent"), "{}")
	utils.Success(c, gin.H{"message": "已恢复"})
}

// ==================== 7.6 全部 tag 名 ====================
//
//	GET /api/media/tags
//
// 返回所有 active 媒体中出现过的 tag name（去重，按字母序）。
// 用于前端 tag 过滤下拉框，包含预制 tag 和用户"手打"的 tag。
func (h *MediaHandler) ListAllTags(c *gin.Context) {
	tags, err := database.ListAllTagNames()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{"tags": tags})
}

// ==================== 13. 批量加 tag ====================
//
//	POST /api/media/bulk-tag
//	body: { ids: number[], tag_name: string, color?: string }
//
// 给一组 media 加上同一个 tag（已存在则跳过）。仅本人或 admin 可操作。
func (h *MediaHandler) BulkAddTag(c *gin.Context) {
	var req struct {
		IDs     []int64 `json:"ids"`
		TagName string  `json:"tag_name"`
		Color   string  `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求: "+err.Error())
		return
	}
	if len(req.IDs) == 0 || req.TagName == "" {
		utils.BadRequest(c, "ids 和 tag_name 必填")
		return
	}
	// 权限校验：每个 id 都得是本人或 admin
	currentUserID := c.GetInt64("user_id")
	isAdmin := IsAdminUser(c)
	for _, id := range req.IDs {
		m, err := database.GetMediaByID(id)
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
			return
		}
		if m == nil {
			utils.BadRequest(c, "媒体不存在: id="+strconv.FormatInt(id, 10))
			return
		}
		if !isAdmin && m.TakenBy != 0 && m.TakenBy != currentUserID {
			utils.Error(c, http.StatusForbidden, "无权修改 id="+strconv.FormatInt(id, 10))
			return
		}
	}
	added, err := database.BulkAddTag(req.IDs, req.TagName, req.Color, currentUserID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "批量打 tag 失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{
		"added":   added,
		"total":   len(req.IDs),
		"message": "已为 " + strconv.Itoa(added) + " 项添加 tag「" + req.TagName + "」",
	})
}

// ==================== 14. 批量软删除 ====================
//
//	POST /api/media/bulk-delete
//	body: { ids: number[] }
//
// 与单条 Delete 一致：仅软标记（status='deleted'，deleted_at=now），可恢复。
func (h *MediaHandler) BulkDelete(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求: "+err.Error())
		return
	}
	if len(req.IDs) == 0 {
		utils.BadRequest(c, "ids 必填")
		return
	}
	currentUserID := c.GetInt64("user_id")
	isAdmin := IsAdminUser(c)
	for _, id := range req.IDs {
		m, err := database.GetMediaByID(id)
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
			return
		}
		if m == nil {
			continue // 已删项跳过
		}
		if !isAdmin && m.TakenBy != 0 && m.TakenBy != currentUserID {
			utils.Error(c, http.StatusForbidden, "无权删除 id="+strconv.FormatInt(id, 10))
			return
		}
	}
	deleted, err := database.BulkDeleteMedia(req.IDs, currentUserID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "批量删除失败: "+err.Error())
		return
	}
	// 审计双写（每条媒体一条 bulk-delete 记录）
	for _, id := range req.IDs {
		database.AppendMediaAuditBoth(id, "bulk-delete", currentUserID, c.ClientIP(), c.GetHeader("User-Agent"),
			fmt.Sprintf(`{"bulk":true,"count":%d}`, len(req.IDs)))
	}
	utils.Success(c, gin.H{
		"deleted": deleted,
		"total":   len(req.IDs),
		"message": "已移到回收站 " + strconv.Itoa(deleted) + " 项",
	})
}

// ==================== 15. 批量关联客户 ====================
//
//	POST /api/media/bulk-customer
//	body: { ids: number[], customer_id: number }
//
// 一组 media 关联到同一客户；customer_id=0 表示解除关联。仅本人或 admin。
func (h *MediaHandler) BulkSetCustomer(c *gin.Context) {
	var req struct {
		IDs        []int64 `json:"ids"`
		CustomerID int64   `json:"customer_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求: "+err.Error())
		return
	}
	if len(req.IDs) == 0 {
		utils.BadRequest(c, "ids 必填")
		return
	}
	// P1 修复：customer_id > 0 时校验客户存在性（避免关联到不存在的 ID）
	if req.CustomerID > 0 {
		exists, err := database.CustomerExists(req.CustomerID)
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "客户存在性校验失败: "+err.Error())
			return
		}
		if !exists {
			utils.BadRequest(c, fmt.Sprintf("客户 #%d 不存在", req.CustomerID))
			return
		}
	}
	currentUserID := c.GetInt64("user_id")
	isAdmin := IsAdminUser(c)
	for _, id := range req.IDs {
		m, err := database.GetMediaByID(id)
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
			return
		}
		if m == nil {
			continue
		}
		if !isAdmin && m.TakenBy != 0 && m.TakenBy != currentUserID {
			utils.Error(c, http.StatusForbidden, "无权修改 id="+strconv.FormatInt(id, 10))
			return
		}
	}
	updated, err := database.BulkSetCustomer(req.IDs, req.CustomerID, currentUserID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "批量关联客户失败: "+err.Error())
		return
	}
	// 审计双写（每条媒体一条 bulk-customer 记录）
	actionVerb := "bind"
	if req.CustomerID == 0 {
		actionVerb = "unbind"
	}
	for _, id := range req.IDs {
		database.AppendMediaAuditBoth(id, actionVerb, currentUserID, c.ClientIP(), c.GetHeader("User-Agent"),
			fmt.Sprintf(`{"customer_id":%d,"bulk":true,"count":%d}`, req.CustomerID, len(req.IDs)))
	}
	utils.Success(c, gin.H{
		"updated": updated,
		"total":   len(req.IDs),
		"message": "已关联 " + strconv.Itoa(updated) + " 项到客户",
	})
}

// ==================== 8. 下载原文件 ====================
//
//	GET /api/media/:id/file
//
// 副作用：download_count++ + audit push "download"
func (h *MediaHandler) Download(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	// 2026-06-27 Bug #6 修复：GetMediaByID 已不滤 deleted_at，
	// 已删媒体也能预览 / 下载（数据库行没被物理删除）。
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权下载")
		return
	}
	storage := services.GetMediaStorage()
	absPath, pathErr := storage.GetAbsolutePath(m.FilePath)
	if pathErr != nil {
		// P0 修复（2026-06-28）：路径非法 → 404 不暴露内部细节
		utils.Warn("[Media.Download] 路径非法 media_id=%d: %v", id, pathErr)
		utils.Error(c, http.StatusNotFound, "文件不存在")
		return
	}
	if _, err := os.Stat(absPath); err != nil {
		utils.Error(c, http.StatusNotFound, "文件不存在")
		return
	}

	// 仅在显式 ?download=true 时计入下载（避免预览/缩略图回退污染统计）
	// P0 修复（2026-06-14）：用 IncrementDownloadCountWithAudit 一次完成 count + 双写审计，
	// 避免之前 IncrementDownloadCount + AppendAudit 两次调用产生 2 条 download 审计。
	if c.Query("download") == "true" {
		_ = database.IncrementDownloadCountWithAudit(id, c.GetInt64("user_id"), c.ClientIP(), c.GetHeader("User-Agent"))
	}

	c.Header("Content-Disposition", "inline; filename="+m.SnowID+"."+fileExt(m.FilePath))
	if m.MimeType != "" {
		c.Header("Content-Type", m.MimeType)
	}
	c.File(absPath)
}

// ==================== 9. 下载缩略图 ====================
//
//	GET /api/media/:id/thumb
func (h *MediaHandler) Thumbnail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	// 2026-06-27 Bug #6 修复：GetMediaByID 已不滤 deleted_at，
	// 已删媒体的缩略图也能预览（之前显示 404）。
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权查看")
		return
	}
	if m.ThumbPath == "" {
		utils.Error(c, http.StatusNotFound, "无缩略图")
		return
	}
	storage := services.GetMediaStorage()
	absPath, err := storage.GetAbsolutePath(m.ThumbPath)
	if err != nil {
		// P0 修复（2026-06-28）：路径非法 → 404 不暴露内部细节
		utils.Warn("[Media.Thumbnail] 路径非法 media_id=%d: %v", id, err)
		utils.Error(c, http.StatusNotFound, "缩略图文件不存在")
		return
	}
	if _, err := os.Stat(absPath); err != nil {
		utils.Error(c, http.StatusNotFound, "缩略图文件不存在")
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.File(absPath)
}

// ==================== 10. 上传（multipart + 3 哈希 + 去重）====================
//
//	POST /api/media/upload
//	form: file=<binary>, name, type, source, source_ref, watermark_text
//	返回: {id, snowid, hash: {sm3, sha256, combined}, reused: bool}
//
// 大小限制：
//   - 视频 ≤ 500 MB（v2 视频元数据后端无法解析，依赖前端 seek-to-end；过大会导致 server OOM）
//   - 音频 ≤ 50 MB
//   - 图片 ≤ 20 MB
const (
	MaxImageUploadSize = 20 * 1024 * 1024
	MaxAudioUploadSize = 50 * 1024 * 1024
	MaxVideoUploadSize = 500 * 1024 * 1024
)

func (h *MediaHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.BadRequest(c, "缺少 file 字段: "+err.Error())
		return
	}

	// P0 修复：按文件类型做大小限制，防止恶意上传耗尽内存
	uploadType := c.PostForm("type")
	maxSize := int64(MaxImageUploadSize)
	switch uploadType {
	case "video":
		maxSize = MaxVideoUploadSize
	case "audio":
		maxSize = MaxAudioUploadSize
	case "photo", "":
		maxSize = MaxImageUploadSize
	default:
		utils.BadRequest(c, "非法的 type: "+uploadType)
		return
	}
	if file.Size > maxSize {
		utils.BadRequest(c, fmt.Sprintf("文件过大（%.1f MB），%s 类型最大 %d MB",
			float64(file.Size)/1024/1024, uploadType, maxSize/1024/1024))
		return
	}

	// 1. 读取字节（也可走流式，但 CalculateDualHash 需要 []byte）
	f, err := file.Open()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "打开上传文件失败: "+err.Error())
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "读取文件失败: "+err.Error())
		return
	}

	// 2. 算 3 哈希
	hashService := services.NewHashService(true, true)
	hash, err := hashService.CalculateDualHash(data)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "计算哈希失败: "+err.Error())
		return
	}

	// 3. 查重（按 SHA-256 索引）
	existing, err := database.GetMediaByHash(hash.SHA256Hash)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查重失败: "+err.Error())
		return
	}
	if existing != nil {
		// 复用：view_count++ + audit push
		_ = database.IncrementViewCount(existing.ID, c.GetInt64("user_id"))
		// 审计双写（复用已有媒体，view_count 已自增；action 写 reused）
		database.AppendMediaAuditBoth(existing.ID, "reused", c.GetInt64("user_id"), c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf(`{"hash_sha256":"%s"}`, hash.SHA256Hash))
		utils.Success(c, gin.H{
			"id":      existing.ID,
			"snowid":  existing.SnowID,
			"hash":    hash,
			"reused":  true,
			"message": "复用已有媒体",
		})
		return
	}

	// 4. 存文件
	storage := services.GetMediaStorage()
	res, data, err := storage.SaveMediaFromMultipart(file)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "保存文件失败: "+err.Error())
		return
	}

	// 5. 缩略图
	//   - 图片：后端 image.DecodeConfig 自动生成
	//   - 视频 / 其他：前端抽帧后通过 FormData "thumbnail" 字段上传（避免后端 ffmpeg 依赖）
	//   - 优先级：用户提供的 thumbnail > 后端自动生成
	thumbPath := ""
	ext := fileExt(res.RelPath)
	if isImageExtStr(ext) {
		if t, terr := storage.GenerateThumbnail(res.RelPath, data, ext); terr == nil {
			thumbPath = t
		} else {
			utils.Warn("[媒体] 生成缩略图失败: %v", terr)
		}
	}
	// 用户提供的缩略图（覆盖自动生成的）
	if thumbHeader, terr := c.FormFile("thumbnail"); terr == nil && thumbHeader != nil {
		thumbFile, terr2 := thumbHeader.Open()
		if terr2 == nil {
			defer thumbFile.Close()
			thumbData, terr3 := io.ReadAll(thumbFile)
			if terr3 == nil && len(thumbData) > 0 {
				if t, terr4 := storage.SaveThumbnail(res.RelPath, thumbData); terr4 == nil {
					thumbPath = t
					utils.Info("[媒体] 用户提供缩略图已保存: %s", t)
				} else {
					utils.Warn("[媒体] 保存用户缩略图失败: %v", terr4)
				}
			}
		} else {
			utils.Warn("[媒体] 打开 thumbnail 失败: %v", terr2)
		}
	}

	// 6. 解析元数据
	currentUserID := c.GetInt64("user_id")
	// P0 修复：服务端用 DetectContentType 验证 MIME（不可被客户端伪造）
	detectedMime := http.DetectContentType(data)
	clientMime := file.Header.Get("Content-Type")
	mimeType := detectedMime
	// 如果客户端声明的是 image/* / video/* / audio/* 但服务端检测为 octet-stream，
	// 可能是用户上传了非标准格式（如 webm）；这种情况下信任客户端声明
	// 注意：括号必须明确，Go 中 && 优先级高于 ||，但业务上需要的是"detected 是 octet-stream
	//       AND clientMime 以 image/video/audio 开头"
	if detectedMime == "application/octet-stream" &&
		(strings.HasPrefix(clientMime, "image/") ||
			strings.HasPrefix(clientMime, "video/") ||
			strings.HasPrefix(clientMime, "audio/")) {
		mimeType = clientMime
	}
	mediaType := c.PostForm("type")
	if !models.IsValidMediaType(mediaType) {
		// 从 ext 兜底
		switch ext {
		case "mp4", "webm", "mov", "avi":
			mediaType = string(models.MediaTypeVideo)
		case "mp3", "wav", "ogg", "m4a":
			mediaType = string(models.MediaTypeAudio)
		default:
			mediaType = string(models.MediaTypePhoto)
		}
	}
	source := c.PostForm("source")
	if source == "" {
		source = string(models.MediaSourceUpload)
	}
	if !models.IsValidMediaSource(source) {
		utils.BadRequest(c, "非法的 source: "+source)
		return
	}
	name := c.PostForm("name")
	if name == "" {
		name = file.Filename
	}

	// P1 修复：校验 customer_id（如果传入）必须存在
	customerID := parseCustomerIDFromForm(c)
	if customerID > 0 {
		exists, err := database.CustomerExists(customerID)
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "客户存在性校验失败: "+err.Error())
			return
		}
		if !exists {
			utils.BadRequest(c, fmt.Sprintf("客户 #%d 不存在", customerID))
			return
		}
	}

	// 优先用前端传来的元数据；后端提取（如图片宽高）作为兜底
	//   - 视频的 width/height/duration 后端无法解析（无 ffmpeg 依赖），全部依赖前端
	//   - 照片的 width/height 后端 image.DecodeConfig 可解析，但前端也可能传（更高优先级）
	fileSize := int64(len(data))
	if v := c.PostForm("file_size"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			fileSize = n
		}
	}
	width := res.Width
	height := res.Height
	if v := c.PostForm("width"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			width = n
		}
	}
	if v := c.PostForm("height"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			height = n
		}
	}
	duration := 0
	if v := c.PostForm("duration"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			duration = int(n)
		}
	}

	m := models.Media{
		SnowID:        res.SnowID,
		Type:          models.MediaType(mediaType),
		Name:          name,
		OriginalName:  file.Filename,
		MimeType:      mimeType,
		FilePath:      res.RelPath,
		ThumbPath:     thumbPath,
		FileSize:      fileSize,
		Width:         width,
		Height:        height,
		Duration:      duration,
		HashSM3:       hash.SM3Hash,
		HashSHA256:    hash.SHA256Hash,
		HashCombined:  hash.CombinedHash,
		Source:        models.MediaSource(source),
		SourceRef:     c.PostForm("source_ref"),
		WatermarkText: c.PostForm("watermark_text"),
		WatermarkMode: parseWatermarkMode(c),
		// P 修复：taken_at 优先用前端传来的真实拍摄时间（摄像头场景重要），
		// 没传时（普通上传）回退到当前时间
		TakenAt:    parseInt64Form(c, "taken_at", time.Now().Unix()),
		TakenBy:    currentUserID,
		CustomerID: customerID, // 已通过存在性校验
		UserID:     currentUserID,
		Status:     models.MediaStatusActive,
		CreatedBy:  currentUserID,
	}

	id, err := database.CreateMedia(m)
	if err != nil {
		// 入库失败：清理已写文件
		_ = storage.DeleteFiles(res.RelPath, thumbPath)
		utils.Error(c, http.StatusInternalServerError, "入库失败: "+err.Error())
		return
	}
	// 审计双写：按 source 区分 action
	//   source="camera" → "record"（摄像头拍照/录像，含手机+桌面摄像头）
	//   source="upload" → "upload"（普通文件上传/拖拽）
	auditAction := "upload"
	if source == string(models.MediaSourceCamera) {
		auditAction = "record"
	}
	database.AppendMediaAuditBoth(id, auditAction, currentUserID, c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf(`{"hash_sm3":"%s","hash_sha256":"%s","source":"%s"}`, hash.SM3Hash, hash.SHA256Hash, source))

	created, _ := database.GetMediaByID(id)
	utils.Success(c, gin.H{
		"id":      id,
		"snowid":  res.SnowID,
		"hash":    hash,
		"reused":  false,
		"data":    created,
		"message": "上传成功",
	})
}

// ==================== 11. 上传前查重 ====================
//
//	GET /api/media/check-hash?hash_sha256=xxx
func (h *MediaHandler) CheckHash(c *gin.Context) {
	hash := strings.TrimSpace(c.Query("hash_sha256"))
	if hash == "" {
		utils.BadRequest(c, "缺少 hash_sha256 参数")
		return
	}
	// 简单 SHA-256 格式校验
	if len(hash) != 64 {
		utils.BadRequest(c, "hash_sha256 格式错误（应为 64 字符十六进制）")
		return
	}
	m, err := database.GetMediaByHash(hash)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Success(c, gin.H{"exists": false})
		return
	}
	utils.Success(c, gin.H{
		"exists":    true,
		"id":        m.ID,
		"snowid":    m.SnowID,
		"file_path": m.FilePath,
		"name":      m.Name,
	})
}

// ==================== 12. 三哈希校验 ====================
//
//	GET /api/media/:id/verify
//
// 返回：{valid, sm3_match, sha256_match, combined_match, current: {...}}
func (h *MediaHandler) Verify(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	m, err := database.GetMediaByID(id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	if m == nil {
		utils.Error(c, http.StatusNotFound, "媒体不存在")
		return
	}
	if !h.canAccess(c, m.TakenBy, m.DepartmentID) {
		utils.Error(c, http.StatusForbidden, "无权校验")
		return
	}
	storage := services.GetMediaStorage()
	absPath, pathErr := storage.GetAbsolutePath(m.FilePath)
	if pathErr != nil {
		// P0 修复（2026-06-28）：路径非法 → 404 不暴露内部细节
		utils.Warn("[Media.Verify] 路径非法 media_id=%d: %v", id, pathErr)
		utils.Error(c, http.StatusNotFound, "媒体文件不存在")
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "读取文件失败: "+err.Error())
		return
	}
	// 重新算（不依赖入库值）
	current, err := services.NewHashService(true, true).CalculateDualHash(data)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "哈希计算失败: "+err.Error())
		return
	}
	sm3Match := current.SM3Hash == m.HashSM3
	sha256Match := current.SHA256Hash == m.HashSHA256
	combinedMatch := current.CombinedHash == m.HashCombined
	utils.Success(c, gin.H{
		"valid":          sm3Match && sha256Match && combinedMatch,
		"sm3_match":      sm3Match,
		"sha256_match":   sha256Match,
		"combined_match": combinedMatch,
		"stored": gin.H{
			"sm3_hash":      m.HashSM3,
			"sha256_hash":   m.HashSHA256,
			"combined_hash": m.HashCombined,
		},
		"current": current,
	})
}

// ==================== 内部辅助 ====================

// canAccess 2026-06-29 RBAC v3 P2 重构：转调统一 helper。
// 保留方法签名（MediaHandler 字段方法）以最小化调用点改动。
//
// 注意：原版对 departmentID = 0 降级为 owner-only；统一 helper 对 ownerID == 0 仍允许通过
// （"公开资源"语义与 media 创建场景兼容，调用点语义一致）。
func (h *MediaHandler) canAccess(c *gin.Context, takenBy, departmentID int64) bool {
	return CheckDataScopeAccess(c, takenBy, departmentID)
}

// parseInt64Query 安全解析 int64 查询参数（解析失败返回 0）
func parseInt64Query(c *gin.Context, key string) int64 {
	v, _ := strconv.ParseInt(c.Query(key), 10, 64)
	return v
}

// fileExt 从路径中提取扩展名（不含点）
func fileExt(p string) string {
	if idx := strings.LastIndex(p, "."); idx >= 0 {
		return strings.ToLower(p[idx+1:])
	}
	return ""
}

// isImageExtStr 判断扩展名是否为图片（字符串版，service 里是私有）
func isImageExtStr(ext string) bool {
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp":
		return true
	}
	return false
}

// ==================== 防止 lint 误报 ====================
var _ = fmt.Sprintf
var _ = sha256.Sum256
var _ = hex.EncodeToString
