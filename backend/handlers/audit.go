package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"doc/database"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// ==================== 通用审计 API ====================
//
// 路由（在 combined_server.go 注册）：
//   GET  /api/audit            — 列出审计（分页 + 过滤，支持多 target_type）
//   POST /api/audit/reconcile  — 触发链验证（admin only）
//
// 设计：
//   - 查询接口仅 GET（不允许写；写通过其他业务 handler 内部调用 database.AppendAudit）
//   - 链验证是只读 + 计算密集，仅管理员可触发（避免高频调用造成 CPU 压力）
// ----------------------------------------------------------------------------

type AuditHandler struct{}

func NewAuditHandler() *AuditHandler {
	return &AuditHandler{}
}

// ListAudit GET /api/audit?target_type=&target_id=&actor_id=&action=&from=&to=&page=&page_size=
//
// Query 参数：
//   - target_type: "" / "media" / "signature" / "seal" / "customer" / ...
//   - target_id:   0  = 全部（仅当 target_type 不为空时按 target_id 过滤）
//   - actor_id:    0  = 全部
//   - action:      "" = 全部
//   - from:        unix seconds 起（0 = 不限）
//   - to:          unix seconds 止（0 = 不限）
//   - page:        1-based，默认 1
//   - page_size:   默认 20，最大 200（防爆）
//
// 响应：{ success, data: { list, total, page, page_size }, message }
//
// 权限：admin only（审计日志含敏感操作历史，仅管理员可查）。
func (h *AuditHandler) ListAudit(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	targetType := c.Query("target_type")
	targetID := parseInt64Query(c, "target_id")
	actorID := parseInt64Query(c, "actor_id")
	action := c.Query("action")
	fromTs := parseInt64Query(c, "from")
	toTs := parseInt64Query(c, "to")
	page := int(parseInt64Query(c, "page"))
	pageSize := int(parseInt64Query(c, "page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	list, total, err := database.ListAudit(targetType, targetID, actorID, action, fromTs, toTs, page, pageSize)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "查询审计失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ReconcileAuditChain POST /api/audit/reconcile
//
// 触发全局哈希链验证（遍历全表逐行重算 hash）。
// 用途：合规工具 / 管理员巡检；通过定时任务每周一次。
//
// 权限：admin only。
//
// 响应：
//   - 200 { success: true, data: { ok: true } } 整链完整
//   - 500 { success: false, error: "<err>", message: "审计链验证失败..." } 链断裂
//     （必须返回 5xx 让 Prometheus / 通用告警能识别；body 走 utils.ErrorWithDetail
//     标准格式，broken_at 通过响应 Header X-Audit-Broken-At 透传，前端可读 header
//     定位断点，且不破坏 body 格式）
//   - 403 { success: false, message: "无权限" } 非管理员
func (h *AuditHandler) ReconcileAuditChain(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	brokenAt, err := database.VerifyAuditChain()
	if err != nil {
		// Issue M-3：走 utils.ErrorWithDetail 标准响应格式（顶层 success/message/error），
		// 不再嵌套 data 字段；broken_at 通过 HTTP 响应 Header X-Audit-Broken-At 透传。
		msg := fmt.Sprintf("审计链验证失败（broken_at=%d）", brokenAt)
		c.Header("X-Audit-Broken-At", strconv.FormatInt(brokenAt, 10))
		utils.ErrorWithDetail(c, http.StatusInternalServerError, msg, err)
		return
	}
	utils.Success(c, gin.H{"ok": true})
}
