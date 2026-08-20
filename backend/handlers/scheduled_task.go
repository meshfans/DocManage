package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"doc/database"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type ScheduledTaskHandler struct{}

func NewScheduledTaskHandler() *ScheduledTaskHandler {
	return &ScheduledTaskHandler{}
}

// ==================== 任务定义 ====================

// ListTasks 任务列表
//   - keyword: 模糊匹配 task_key / name
//   - task_type: built_in / business
//   - is_active: 1 / 0
func (h *ScheduledTaskHandler) ListTasks(c *gin.Context) {
	tasks, err := database.GetAllScheduledTasks()
	if err != nil {
		utils.Err(c, utils.CodeInternal, "获取任务列表失败")
		return
	}
	if tasks == nil {
		tasks = []database.ScheduledTask{}
	}

	keyword := strings.TrimSpace(c.Query("keyword"))
	taskType := strings.TrimSpace(c.Query("task_type"))
	isActiveStr := c.Query("is_active")

	filtered := make([]database.ScheduledTask, 0, len(tasks))
	for i := range tasks {
		t := tasks[i]
		if keyword != "" {
			kw := strings.ToLower(keyword)
			if !strings.Contains(strings.ToLower(t.TaskKey), kw) &&
				!strings.Contains(strings.ToLower(t.Name), kw) {
				continue
			}
		}
		if taskType != "" && t.TaskType != taskType {
			continue
		}
		if isActiveStr != "" {
			wantActive := isActiveStr == "1" || isActiveStr == "true"
			if t.IsActive != wantActive {
				continue
			}
		}
		filtered = append(filtered, t)
	}

	utils.Success(c, gin.H{
		"list":  filtered,
		"total": len(filtered),
	})
}

// GetTask 任务详情
func (h *ScheduledTaskHandler) GetTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	t, err := database.GetScheduledTaskByID(id)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "任务不存在")
		return
	}
	utils.Success(c, t)
}

// CreateTask 新建业务任务（不允许创建 built_in）
func (h *ScheduledTaskHandler) CreateTask(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
		return
	}
	var req struct {
		TaskKey        string `json:"task_key" binding:"required"`
		Name           string `json:"name" binding:"required"`
		Description    string `json:"description"`
		CronExpr       string `json:"cron_expr" binding:"required"`
		HandlerName    string `json:"handler_name" binding:"required"`
		HandlerParams  string `json:"handler_params"`
		IsActive       bool   `json:"is_active"`
		IsConcurrent   bool   `json:"is_concurrent"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if req.HandlerParams == "" {
		req.HandlerParams = "{}"
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 3600
	}

	// 校验 cron
	if s := services.GetScheduler(); s != nil {
		if err := s.ValidateCronExpr(req.CronExpr); err != nil {
			utils.BadRequest(c, "无效的 cron 表达式: "+err.Error())
			return
		}
	}

	// 校验 handler 是否已注册
	if s := services.GetScheduler(); s != nil {
		found := false
		for _, h := range s.ListHandlers() {
			if h.Name == req.HandlerName {
				found = true
				break
			}
		}
		if !found {
			utils.BadRequest(c, "handler 不存在: "+req.HandlerName)
			return
		}
	}

	// 唯一性
	if existing, _ := database.GetScheduledTaskByKey(req.TaskKey); existing != nil {
		utils.Err(c, utils.CodeConflict, "task_key 已存在")
		return
	}

	t := &database.ScheduledTask{
		TaskKey:        req.TaskKey,
		Name:           req.Name,
		Description:    req.Description,
		TaskType:       "business",
		CronExpr:       req.CronExpr,
		HandlerName:    req.HandlerName,
		HandlerParams:  req.HandlerParams,
		IsActive:       req.IsActive,
		IsConcurrent:   req.IsConcurrent,
		TimeoutSeconds: req.TimeoutSeconds,
		CreatedBy:      c.GetInt64("user_id"),
	}
	id, err := database.CreateScheduledTask(t)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "创建任务失败: "+err.Error())
		return
	}
	t.ID = id

	// 重新加载到调度器
	if s := services.GetScheduler(); s != nil {
		if err := s.AddOrUpdateTask(t); err != nil {
			utils.Warn("任务已写入 DB，但加入调度器失败: %v", err)
		}
	}

	utils.Success(c, t)
}

// UpdateTask 更新任务（内置任务只允许改 is_active、description、timeout_seconds）
func (h *ScheduledTaskHandler) UpdateTask(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	existing, err := database.GetScheduledTaskByID(id)
	if err != nil {
		utils.Err(c, utils.CodeNotFound, "任务不存在")
		return
	}
	// 快照原值（用于审计对比；必须 DB 更新前复制，否则 DB 已是新值，审计写不进去）
	oldSnapshot := *existing

	var req struct {
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		CronExpr       *string `json:"cron_expr"`
		HandlerName    *string `json:"handler_name"`
		HandlerParams  *string `json:"handler_params"`
		IsActive       *bool   `json:"is_active"`
		IsConcurrent   *bool   `json:"is_concurrent"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 阶段 13.1 放开：内置任务和业务任务统一可改
	// 保留限制：内置任务的 handler_name 不允许改（避免破坏系统语义）
	isBuiltIn := existing.TaskType == "built_in"
	if isBuiltIn && req.HandlerName != nil && *req.HandlerName != existing.HandlerName {
		utils.Err(c, utils.CodeForbidden, "内置任务的 handler 不允许修改；如需自定义处理逻辑，请新建业务任务")
		return
	}

	// 通用字段：所有任务统一处理
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.CronExpr != nil {
		if s := services.GetScheduler(); s != nil {
			if err := s.ValidateCronExpr(*req.CronExpr); err != nil {
				utils.BadRequest(c, "无效的 cron 表达式: "+err.Error())
				return
			}
		}
		existing.CronExpr = *req.CronExpr
	}
	if req.HandlerName != nil {
		if s := services.GetScheduler(); s != nil {
			found := false
			for _, hh := range s.ListHandlers() {
				if hh.Name == *req.HandlerName {
					found = true
					break
				}
			}
			if !found {
				utils.BadRequest(c, "handler 不存在: "+*req.HandlerName)
				return
			}
		}
		existing.HandlerName = *req.HandlerName
	}
	if req.HandlerParams != nil {
		existing.HandlerParams = *req.HandlerParams
	}
	if req.IsConcurrent != nil {
		existing.IsConcurrent = *req.IsConcurrent
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.TimeoutSeconds != nil && *req.TimeoutSeconds > 0 {
		existing.TimeoutSeconds = *req.TimeoutSeconds
	}

	if err := database.UpdateScheduledTask(existing); err != nil {
		utils.Err(c, utils.CodeInternal, "更新任务失败: "+err.Error())
		return
	}

	// 写入审计记录（使用 DB 更新前的快照，避免 oldVal == newVal 提前返回）
	operatorID := c.GetInt64("user_id")
	operatorName := c.GetString("username")
	writeAuditLog(id, oldSnapshot.TaskKey, operatorID, operatorName, req, &oldSnapshot)

	// 重新加载到调度器（热加载：RemoveTask + Schedule 重新挂载）
	if s := services.GetScheduler(); s != nil {
		fresh, _ := database.GetScheduledTaskByID(id)
		if fresh != nil {
			if err := s.AddOrUpdateTask(fresh); err != nil {
				utils.Warn("调度器热更新失败: %v", err)
			}
		}
	}

	utils.Success(c, existing)
}

// ToggleTask 启停
func (h *ScheduledTaskHandler) ToggleTask(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	t, err := database.GetScheduledTaskByID(id)
	if err != nil {
		utils.Err(c, utils.CodeNotFound, "任务不存在")
		return
	}
	newActive := !t.IsActive
	if err := database.UpdateScheduledTaskActive(id, newActive); err != nil {
		utils.Err(c, utils.CodeInternal, "更新任务状态失败: "+err.Error())
		return
	}
	t.IsActive = newActive

	// 审计
	action := "enable"
	if !newActive {
		action = "disable"
	}
	_, _ = database.CreateScheduledTaskAudit(&database.ScheduledTaskAudit{
		TaskID:       id,
		TaskKey:      t.TaskKey,
		Action:       action,
		FieldName:    "is_active",
		OldValue:     strconv.FormatBool(!newActive),
		NewValue:     strconv.FormatBool(newActive),
		OperatorID:   c.GetInt64("user_id"),
		OperatorName: c.GetString("username"),
	})

	if s := services.GetScheduler(); s != nil {
		if err := s.AddOrUpdateTask(t); err != nil {
			utils.Warn("调度器热更新失败: %v", err)
		}
	}
	utils.Success(c, t)
}

// DeleteTask 删除（仅业务任务）
func (h *ScheduledTaskHandler) DeleteTask(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	t, err := database.GetScheduledTaskByID(id)
	if err != nil {
		utils.Err(c, utils.CodeNotFound, "任务不存在")
		return
	}
	if t.TaskType == "built_in" {
		utils.Err(c, utils.CodeForbidden, "内置任务不允许删除")
		return
	}
	if err := database.DeleteScheduledTask(id); err != nil {
		utils.Err(c, utils.CodeInternal, "删除任务失败: "+err.Error())
		return
	}
	if s := services.GetScheduler(); s != nil {
		s.RemoveTask(id)
	}
	utils.Success(c, gin.H{"id": id})
}

// RunNow 立即执行
func (h *ScheduledTaskHandler) RunNow(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Error(c, http.StatusForbidden, "需要管理员权限")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	s := services.GetScheduler()
	if s == nil {
		utils.Err(c, utils.CodeMaintenance, "调度器未启动")
		return
	}
	operatorID := c.GetInt64("user_id")
	if _, err := s.RunNow(id, operatorID); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, gin.H{"id": id, "triggered_by": "manual", "operator_id": operatorID})
}

// ListHandlers 可注册的 handler 列表
func (h *ScheduledTaskHandler) ListHandlers(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "需要管理员权限")
		return
	}
	s := services.GetScheduler()
	if s == nil {
		utils.Err(c, utils.CodeMaintenance, "调度器未启动，无法列出可用 Handler")
		return
	}
	utils.Success(c, s.ListHandlers())
}

// GetTaskLogs 查询任务执行日志
//   - 支持分页 page / page_size
func (h *ScheduledTaskHandler) GetTaskLogs(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	logs, total, err := database.GetScheduledTaskLogs(id, page, pageSize)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Success(c, gin.H{"list": []database.ScheduledTaskLog{}, "total": 0})
			return
		}
		utils.Error(c, http.StatusInternalServerError, "查询日志失败: "+err.Error())
		return
	}
	if logs == nil {
		logs = []database.ScheduledTaskLog{}
	}
	utils.Success(c, gin.H{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// NextRunTime 计算 cron 表达式的下一次触发时间（不依赖任务是否存在）。
// 用于前端"测试"按钮。
func (h *ScheduledTaskHandler) NextRunTime(c *gin.Context) {
	expr := c.Query("expr")
	if expr == "" {
		utils.BadRequest(c, "缺少 expr 参数")
		return
	}
	s := services.GetScheduler()
	if s == nil {
		utils.Error(c, http.StatusServiceUnavailable, "调度器未启动")
		return
	}
	next, err := s.NextRun(expr)
	if err != nil {
		utils.BadRequest(c, "无效的 cron 表达式: "+err.Error())
		return
	}
	utils.Success(c, gin.H{
		"expr":      expr,
		"next_run":  next.Unix(),
		"next_time": next.Format("2006-01-02 15:04:05"),
	})
}

// GetTaskAudits 任务变更审计日志。
func (h *ScheduledTaskHandler) GetTaskAudits(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的任务ID")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	audits, total, err := database.GetScheduledTaskAudits(id, page, pageSize)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询审计失败: "+err.Error())
		return
	}
	if audits == nil {
		audits = []database.ScheduledTaskAudit{}
	}
	utils.Success(c, gin.H{
		"list":      audits,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ==================== 内部工具 ====================

// writeAuditLog 把 UpdateTask 的字段变更逐项写入审计表。
//   - oldTask 必须是 DB 更新**之前**的快照，否则 oldVal 和 newVal 会都是新值导致审计写不进去。
func writeAuditLog(taskID int64, taskKey string, operatorID int64, operatorName string, req struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	CronExpr       *string `json:"cron_expr"`
	HandlerName    *string `json:"handler_name"`
	HandlerParams  *string `json:"handler_params"`
	IsActive       *bool   `json:"is_active"`
	IsConcurrent   *bool   `json:"is_concurrent"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
}, oldTask *database.ScheduledTask) {
	if oldTask == nil {
		return
	}

	// 写一条审计记录的辅助函数
	write := func(field, oldVal, newVal string) {
		if oldVal == newVal {
			return
		}
		_, _ = database.CreateScheduledTaskAudit(&database.ScheduledTaskAudit{
			TaskID:       taskID,
			TaskKey:      taskKey,
			Action:       "update_" + field,
			FieldName:    field,
			OldValue:     oldVal,
			NewValue:     newVal,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}

	if req.Name != nil {
		write("name", oldTask.Name, *req.Name)
	}
	if req.Description != nil {
		write("description", oldTask.Description, *req.Description)
	}
	if req.CronExpr != nil {
		write("cron_expr", oldTask.CronExpr, *req.CronExpr)
	}
	if req.HandlerName != nil {
		write("handler_name", oldTask.HandlerName, *req.HandlerName)
	}
	if req.HandlerParams != nil {
		write("handler_params", oldTask.HandlerParams, *req.HandlerParams)
	}
	if req.IsActive != nil {
		write("is_active", strconv.FormatBool(oldTask.IsActive), strconv.FormatBool(*req.IsActive))
	}
	if req.IsConcurrent != nil {
		write("is_concurrent", strconv.FormatBool(oldTask.IsConcurrent), strconv.FormatBool(*req.IsConcurrent))
	}
	if req.TimeoutSeconds != nil {
		write("timeout_seconds", strconv.Itoa(oldTask.TimeoutSeconds), strconv.Itoa(*req.TimeoutSeconds))
	}
}
