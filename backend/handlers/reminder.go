package handlers

import (
	"doc/database"
	"doc/middleware"
	"doc/models"
	"doc/services"
	"doc/utils"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ReminderHandler 第十一阶段 提醒业务 Handler。
// 路由前缀：/api/reminders
// 鉴权：受 JWTAuth 中间件保护；写操作要求 admin（template 删除/扫描触发）。
type ReminderHandler struct{}

func NewReminderHandler() *ReminderHandler { return &ReminderHandler{} }

// ==================== 模板管理 ====================

// ListTemplates GET /api/reminders/templates
// 任何登录用户可查。
//
// Phase 2c (High #20)：admin 看全部；非 admin 仅看 is_system=0 的自定义模板
// （系统预置模板 is_system=1 通常包含敏感扫描规则/邮件模板正文）。
func (h *ReminderHandler) ListTemplates(c *gin.Context) {
	all, err := database.ListReminderTemplates()
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询模板失败: "+err.Error())
		return
	}
	if all == nil {
		all = []database.ReminderTemplate{}
	}
	isAdmin := IsAdminUser(c)
	out := make([]database.ReminderTemplate, 0, len(all))
	for _, t := range all {
		if isAdmin || !t.IsSystem {
			out = append(out, t)
		}
	}
	utils.Success(c, gin.H{"list": out, "total": len(out)})
}

// CreateTemplate POST /api/reminders/templates（admin）
// 2026-06-25 P0-6.1 修复：admin 撤销角色后旧 JWT 仍 24h 有效，
// 改用 IsAdminUser (DB-backed) 而非 IsAdmin (JWT-only)，
// 确保 gate 立即反映角色变更。
func (h *ReminderHandler) CreateTemplate(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "仅 admin 可创建模板")
		return
	}
	var req struct {
		TemplateKey string `json:"template_key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		RuleType    string `json:"rule_type"`
		AdvanceDays int    `json:"advance_days"`
		IsActive    bool   `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的请求数据: "+err.Error())
		return
	}
	if strings.TrimSpace(req.TemplateKey) == "" {
		utils.Err(c, utils.CodeInvalidParam, "template_key 不能为空")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		utils.Err(c, utils.CodeInvalidParam, "name 不能为空")
		return
	}
	if strings.TrimSpace(req.RuleType) == "" {
		req.RuleType = "contract_expiring"
	}
	if req.AdvanceDays < 0 || req.AdvanceDays > 365 {
		utils.Err(c, utils.CodeInvalidParam, "advance_days 应在 [0, 365]")
		return
	}

	// 查重
	existing, _ := database.GetReminderTemplateByKey(req.TemplateKey)
	if existing != nil {
		utils.Err(c, utils.CodeConflict, "template_key 已存在")
		return
	}

	t := &database.ReminderTemplate{
		TemplateKey: req.TemplateKey,
		Name:        req.Name,
		Description: req.Description,
		RuleType:    req.RuleType,
		AdvanceDays: req.AdvanceDays,
		IsActive:    req.IsActive,
		IsSystem:    false, // 业务创建的均为非系统
		SortOrder:   req.SortOrder,
	}
	id, err := database.CreateReminderTemplate(t)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "创建失败: "+err.Error())
		return
	}
	// 审计：模板创建（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "template.create", gin.H{
		"template_key": req.TemplateKey,
		"name":         req.Name,
	})
	utils.Success(c, gin.H{"id": id})
}

// UpdateTemplate POST /api/reminders/templates/:id（admin）
// 2026-06-25 P0-6.1 修复：见 CreateTemplate 注释。
func (h *ReminderHandler) UpdateTemplate(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "仅 admin 可修改模板")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的 id")
		return
	}
	old, err := database.GetReminderTemplateByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if old == nil {
		utils.Err(c, utils.CodeReminderTemplateNotFound, "模板不存在")
		return
	}
	if old.IsSystem {
		utils.Err(c, utils.CodeForbidden, "系统预置模板不可修改")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		RuleType    string `json:"rule_type"`
		AdvanceDays int    `json:"advance_days"`
		IsActive    bool   `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的请求数据: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		utils.Err(c, utils.CodeInvalidParam, "name 不能为空")
		return
	}

	old.SortOrder = req.SortOrder
	old.Name = req.Name
	old.Description = req.Description
	old.RuleType = req.RuleType
	old.AdvanceDays = req.AdvanceDays
	old.IsActive = req.IsActive
	if err := database.UpdateReminderTemplate(old); err != nil {
		utils.Err(c, utils.CodeInternal, "更新失败: "+err.Error())
		return
	}
	// 审计：模板更新（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "template.update", gin.H{
		"template_key": old.TemplateKey,
		"is_active":    old.IsActive,
	})
	utils.Success(c, gin.H{"message": "已更新"})
}

// DeleteTemplate POST /api/reminders/templates/:id/delete（admin）
// 2026-06-25 P0-6.1 修复：见 CreateTemplate 注释。
func (h *ReminderHandler) DeleteTemplate(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "仅 admin 可删除模板")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的 id")
		return
	}
	old, err := database.GetReminderTemplateByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if old == nil {
		utils.Err(c, utils.CodeReminderTemplateNotFound, "模板不存在")
		return
	}
	if old.IsSystem {
		utils.Err(c, utils.CodeForbidden, "系统预置模板不可删除")
		return
	}
	// 检查是否有 active 订阅
	count, _ := database.CountActiveSubscriptionsByTemplate(id)
	if count > 0 {
		utils.Err(c, utils.CodeConflict, "该模板下还有启用中的订阅，请先删除/停用订阅")
		return
	}
	if err := database.DeleteReminderTemplate(id); err != nil {
		utils.Err(c, utils.CodeInternal, "删除失败: "+err.Error())
		return
	}
	// 审计：模板删除（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "template.delete", gin.H{
		"template_key": old.TemplateKey,
	})
	utils.Success(c, gin.H{"message": "已删除"})
}

// ==================== 订阅管理 ====================

// ListSubscriptions GET /api/reminders/subscriptions
// 任何登录用户可查（按 link_type/link_id 过滤时仅返回相关订阅）。
// 第十三阶段 v4：用 link_type + link_id 替代 customer_id + contract_id。
// 兼容旧 query：customer_id=N → 内部转 link_type='customer' + link_id=N
func (h *ReminderHandler) ListSubscriptions(c *gin.Context) {
	// 2026-06-29 RBAC v3 P0：补 reminder:subscriptions:list 权限码校验。
	if !RequirePermission(c, "reminder:subscriptions:list") {
		return
	}
	templateID, _ := strconv.ParseInt(c.Query("template_id"), 10, 64)
	linkType := c.Query("link_type")
	linkID, _ := strconv.ParseInt(c.Query("link_id"), 10, 64)
	// 兼容旧 query
	if linkID == 0 {
		if cid := c.Query("customer_id"); cid != "" {
			if n, err := strconv.ParseInt(cid, 10, 64); err == nil && n > 0 {
				linkType = "customer"
				linkID = n
			}
		}
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var isActive *bool
	if s := c.Query("is_active"); s != "" {
		v := s == "1" || s == "true"
		isActive = &v
	}
	// 2026-06-29 RBAC v3 P1：admin 看全部；其他用户按 data_scope 过滤。
	// reminder_subscription.created_by 是 owner（白名单 created_by），department_id 已加列。
	scope := middleware.GetDataScope(c)
	var list []database.ReminderSubscription
	var total int
	var err error
	if IsAdminUser(c) || scope == nil || scope.DataScope == "all" {
		list, total, err = database.ListAllSubscriptions(templateID, linkType, linkID, isActive, page, pageSize)
	} else {
		whereSQL, args, werr := database.BuildWhereSQL(scope, database.FilterOpts{
			OwnerCol: "created_by",
			DeptCol:  "department_id",
		})
		if werr != nil {
			utils.Err(c, utils.CodeInternal, "data_scope 过滤失败: "+werr.Error())
			return
		}
		list, total, err = database.ListAllSubscriptionsByDataScope(templateID, linkType, linkID, isActive, page, pageSize, whereSQL, args)
	}
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if list == nil {
		list = []database.ReminderSubscription{}
	}
	utils.Success(c, gin.H{"list": list, "total": total, "page": page, "page_size": pageSize})
}

// CreateSubscription POST /api/reminders/subscriptions
// body: { template_id, link_type, link_id, receiver_type?, receiver_id?, remark? }
// 第十三阶段 v4：用 link_type + link_id（CSV）替代 customer_id + contract_id。
// 第十三阶段 v2：receiver_id 改为 string（CSV 格式），支持多 ID。
//
// Phase 2c (Critical #5)：订阅可被普通用户创建任意客户 → 自身收到提醒，
// 等同于"主动泄露任意客户的合同到期信息"。需 reminder:subscriptions:create 权限码。
func (h *ReminderHandler) CreateSubscription(c *gin.Context) {
	if !RequirePermission(c, "reminder:subscriptions:create") {
		return
	}

	var req struct {
		TemplateID   int64  `json:"template_id"`
		LinkType     string `json:"link_type"`     // 'customer' | 'third_party_contract'
		LinkID       string `json:"link_id"`       // CSV: "5" / "1,2,3"
		ReceiverType string `json:"receiver_type"` // 可选，缺省 'admin'
		ReceiverID   string `json:"receiver_id"`   // CSV，可选，缺省 '0'
		Remark       string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的请求数据: "+err.Error())
		return
	}
	if req.TemplateID <= 0 {
		utils.Err(c, utils.CodeInvalidParam, "template_id 必填")
		return
	}
	// 验证模板存在
	tpl, err := database.GetReminderTemplateByID(req.TemplateID)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询模板失败: "+err.Error())
		return
	}
	if tpl == nil {
		utils.Err(c, utils.CodeInvalidParam, "模板不存在")
		return
	}
	// 第十三阶段 v4：校验 link_type + link_id
	if req.LinkType == "" {
		req.LinkType = "customer" // 缺省 = 客户级
	}
	validLinkTypes := map[string]bool{
		"customer": true, "third_party_contract": true,
	}
	if !validLinkTypes[req.LinkType] {
		utils.Err(c, utils.CodeInvalidParam, "link_type 非法，应为 customer/third_party_contract 之一")
		return
	}
	linkIDs := database.ParseReceiverIDs(req.LinkID)
	if len(linkIDs) == 0 {
		utils.Err(c, utils.CodeInvalidParam, "link_id 必填（至少 1 个有效 ID，多个用逗号分隔）")
		return
	}
	// 按 link_type 校验每个 link_id 实体存在
	// MP4（2026-08-21）：N+1 重构。原先 for-loop 对每个 ID 单独查 DB，改为批量 IN 查询（O(1) 次 DB 调用）。
	switch req.LinkType {
	case "customer":
		customers, err := database.GetCustomersByIDs(linkIDs)
		if err != nil {
			utils.LogError("[reminder.CreateSubscription] 批量查询客户失败 ids=%v: %v", linkIDs, err)
			utils.Err(c, utils.CodeInternal, "查询客户失败: "+err.Error())
			return
		}
		for _, cid := range linkIDs {
			if _, ok := customers[cid]; !ok {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("客户 #%d 不存在", cid))
				return
			}
		}
	case "third_party_contract":
		tpcs, err := database.GetThirdPartyContractsByIDs(linkIDs)
		if err != nil {
			utils.LogError("[reminder.CreateSubscription] 批量查询第三方合同失败 ids=%v: %v", linkIDs, err)
			utils.Err(c, utils.CodeInternal, "查询第三方合同失败: "+err.Error())
			return
		}
		// 构建 id → entity 映射，便于 O(1) 查找；同时校验每个 id 都存在
		byID := make(map[int64]*models.ThirdPartyContract, len(tpcs))
		for _, tpc := range tpcs {
			byID[tpc.ID] = tpc
		}
		for _, cid := range linkIDs {
			tpc, ok := byID[cid]
			if !ok {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("第三方合同 #%d 不存在", cid))
				return
			}
			if tpc.EndDate == 0 {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("第三方合同 #%d 无到期日，无法订阅", cid))
				return
			}
		}
	}
	// 规范化 link_id（去重 + join）
	req.LinkID = database.FormatReceiverIDs(linkIDs)

	// 第十三阶段：receiver_type 校验
	if req.ReceiverType == "" {
		req.ReceiverType = "admin" // 缺省 = v1 行为
	}
	if !database.IsValidReceiverType(req.ReceiverType) {
		utils.Err(c, utils.CodeInvalidParam, "receiver_type 非法，应为 admin/contract_owner/customer_owner/department/user 之一")
		return
	}
	// 第十三阶段 v2：解析 receiver_id CSV
	ids := database.ParseReceiverIDs(req.ReceiverID)
	// 校验 receiver_id 与 receiver_type 的搭配
	switch req.ReceiverType {
	case "admin":
		// admin: receiver_id 忽略
		req.ReceiverID = "0"
	case "contract_owner":
		// contract_owner: receiver_id 忽略
		req.ReceiverID = "0"
	case "customer_owner":
		// customer_owner: receiver_id 忽略
		req.ReceiverID = "0"
	case "department":
		// department: 至少 1 个有效 ID
		if len(ids) == 0 {
			utils.Err(c, utils.CodeInvalidParam, "receiver_type=department 时 receiver_id 必填（部门 ID，多个用逗号分隔）")
			return
		}
		// 批量校验部门存在性（避免 N+1）。
		deptMap, err := database.GetDepartmentsByIDs(ids)
		if err != nil {
			utils.Err(c, utils.CodeInternal, "部门查询失败: "+err.Error())
			return
		}
		for _, deptID := range ids {
			if _, ok := deptMap[deptID]; !ok {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("部门 #%d 不存在", deptID))
				return
			}
		}
		// 规范化为去重后的 CSV（防止重复）
		req.ReceiverID = database.FormatReceiverIDs(ids)
	case "user":
		// user: 至少 1 个有效 ID
		if len(ids) == 0 {
			utils.Err(c, utils.CodeInvalidParam, "receiver_type=user 时 receiver_id 必填（用户 ID，多个用逗号分隔）")
			return
		}
		// 校验每个用户存在 + active
		for _, uid := range ids {
			id, isActive, err := database.GetUserActiveByID(uid)
			if err != nil || id == 0 {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("用户 #%d 不存在", uid))
				return
			}
			if !isActive {
				utils.Err(c, utils.CodeInvalidParam, fmt.Sprintf("用户 #%d 已停用", uid))
				return
			}
		}
		// 规范化为去重后的 CSV
		req.ReceiverID = database.FormatReceiverIDs(ids)
	}

	sub := &database.ReminderSubscription{
		TemplateID:   req.TemplateID,
		LinkType:     req.LinkType,
		LinkID:       req.LinkID,
		ReceiverType: req.ReceiverType,
		ReceiverID:   req.ReceiverID,
		IsActive:     true,
		Remark:       req.Remark,
		CreatedBy:    c.GetInt64("user_id"),
	}
	// 2026-06-29 RBAC v3 P1：快照创建者主部门到 department_id（用于 data_scope 过滤）。
	createdBy := c.GetInt64("user_id")
	deptID, derr := database.GetUserMainDepartment(createdBy)
	if derr != nil {
		utils.Warn("[reminder] 加载创建者主部门失败: %v，department_id 置 0", derr)
		deptID = 0
	}
	sub.DepartmentID = deptID
	id, err := database.CreateReminderSubscription(sub)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "创建失败: "+err.Error())
		return
	}
	// 审计：订阅创建（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "subscription.create", gin.H{
		"template_id": sub.TemplateID,
		"link_type":   sub.LinkType,
	})
	utils.Success(c, gin.H{"id": id})
}

// UpdateSubscription POST /api/reminders/subscriptions/:id
// body: { is_active, remark }
//
// Phase 2c (High #19)：admin 或 created_by 本人才可改订阅。
func (h *ReminderHandler) UpdateSubscription(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的 id")
		return
	}
	old, err := database.GetReminderSubscriptionByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if old == nil {
		utils.Err(c, utils.CodeReminderSubscriptionNotFound, "订阅不存在")
		return
	}
	if !IsAdminUser(c) && c.GetInt64("user_id") != old.CreatedBy {
		utils.Err(c, utils.CodeForbidden, "只能修改自己创建的订阅")
		return
	}
	var req struct {
		IsActive *bool  `json:"is_active"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的请求数据: "+err.Error())
		return
	}
	if req.IsActive != nil {
		old.IsActive = *req.IsActive
	}
	if req.Remark != "" {
		old.Remark = req.Remark
	}
	if err := database.UpdateReminderSubscription(old); err != nil {
		utils.Err(c, utils.CodeInternal, "更新失败: "+err.Error())
		return
	}
	// 审计：订阅更新（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "subscription.update", gin.H{
		"is_active": old.IsActive,
	})
	utils.Success(c, gin.H{"message": "已更新"})
}

// DeleteSubscription POST /api/reminders/subscriptions/:id/delete
//
// Phase 2c (High #19)：admin 或 created_by 本人才可删订阅。
func (h *ReminderHandler) DeleteSubscription(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的 id")
		return
	}
	old, err := database.GetReminderSubscriptionByID(id)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if old == nil {
		utils.Err(c, utils.CodeReminderSubscriptionNotFound, "订阅不存在")
		return
	}
	if !IsAdminUser(c) && c.GetInt64("user_id") != old.CreatedBy {
		utils.Err(c, utils.CodeForbidden, "只能删除自己创建的订阅")
		return
	}
	if err := database.DeleteReminderSubscription(id); err != nil {
		utils.Err(c, utils.CodeInternal, "删除失败: "+err.Error())
		return
	}
	// 审计：订阅删除（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetReminder, id, "subscription.delete", gin.H{
		"template_id": old.TemplateID,
	})
	utils.Success(c, gin.H{"message": "已删除"})
}

// ==================== 日志 ====================

// ListLogs GET /api/reminders/logs
// 任何登录用户可查（管理页用）。
// 2026-06-29 RBAC v3 P0：补 reminder:logs 权限码校验。
func (h *ReminderHandler) ListLogs(c *gin.Context) {
	if !RequirePermission(c, "reminder:logs") {
		return
	}
	templateID, _ := strconv.ParseInt(c.Query("template_id"), 10, 64)
	customerID, _ := strconv.ParseInt(c.Query("customer_id"), 10, 64)
	contractID, _ := strconv.ParseInt(c.Query("contract_id"), 10, 64)
	triggerDate := c.Query("trigger_date")
	triggeredBy := c.Query("triggered_by")
	status := c.Query("delivery_status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	logs, total, err := database.ListReminderLogs(templateID, customerID, contractID, triggerDate, triggeredBy, status, page, pageSize)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if logs == nil {
		logs = []database.ReminderLog{}
	}
	utils.Success(c, gin.H{"list": logs, "total": total, "page": page, "page_size": pageSize})
}

// ==================== 扫描触发（admin）====================

// TriggerScan POST /api/reminders/scan（admin）
// 立即触发一次扫描（不依赖 scheduler），返回扫描统计。
// 2026-06-25 P0-6.1 修复：见 CreateTemplate 注释。
func (h *ReminderHandler) TriggerScan(c *gin.Context) {
	if !IsAdminUser(c) {
		utils.Err(c, utils.CodeForbidden, "仅 admin 可触发扫描")
		return
	}
	stats, err := services.ScanReminders("manual", c.GetInt64("user_id"))
	if err != nil {
		utils.Err(c, utils.CodeInternal, "扫描失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{
		"message": "扫描完成",
		"stats":   stats,
	})
}

// ==================== helper ====================

// derefInt64 安全解引用 *int64 → interface{}（nil 时返回 nil）。
func derefInt64(p *int64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
