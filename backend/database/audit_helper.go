package database

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

// ==================== 审计写入辅助（database 包内，避免与 utils 的循环依赖）====================
//
// 业务 handler 接入 AppendAudit 的最小改动模板：
//
//	database.RecordAudit(c, database.AuditTargetXxx, targetID, "action", gin.H{...})
//
// 设计要点：
//   - 失败容忍：审计写入失败只 warn，**绝不阻塞**业务事务（审计可丢、不可阻业务）。
//   - 上下文自动注入：从 gin.Context 取 user_id / IP / UA。
//   - 一致性：所有 handler 调用路径相同，避免"有的写、有的没写"。
//   - 位置：本函数放在 database 包内（而非 utils），以避免 utils ↔ database 的循环依赖。
// ----------------------------------------------------------------------------

// RecordAuditBy 与 RecordAudit 类似，但允许显式传入 actor_user_id。
// 适用于 actor_user_id 已知但 c.Get("user_id") 不可用的场景（如 /api/login handler）。
func RecordAuditBy(c *gin.Context, actorUserID int64, targetType string, targetID int64, action string, meta ...interface{}) {
	if c == nil {
		fmt.Printf("[audit] RecordAuditBy: gin.Context 为空, target=%s/%d action=%s\n",
			targetType, targetID, action)
		return
	}

	var detail string
	if len(meta) > 0 && meta[0] != nil {
		b, err := json.Marshal(meta[0])
		if err != nil {
			fmt.Printf("[audit] RecordAuditBy: json.Marshal meta 失败, target=%s/%d action=%s err=%v\n",
				targetType, targetID, action, err)
			detail = "{}"
		} else {
			detail = string(b)
		}
	} else {
		detail = "{}"
	}

	if _, err := AppendAudit(
		targetType,
		targetID,
		action,
		actorUserID,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
		detail,
	); err != nil {
		fmt.Printf("[audit] RecordAuditBy 写入失败, target=%s/%d action=%s err=%v\n",
			targetType, targetID, action, err)
	}
}

// RecordAudit 从 gin.Context 抽取审计上下文并写入 audit_log。
//
// 入参：
//   - c:           gin.Context（取 IP / UA / 默认 user_id）
//   - targetType:  AuditTargetXxx 之一
//   - targetID:    业务对象 id（int64；按业务约定；auth 类允许 0 表示无 target）
//   - action:      操作类型（短横线命名，如 "create"/"login.success"）
//   - meta:        可选；会序列化为 JSON 存 detail 列（nil → "{}"）
//
// 失败：仅 warn，不返回错误（避免破坏调用点"调用即过"的简洁性）。
//
// 设计：actor_user_id 默认从 c.Get("user_id") 提取；当 handler 在 JWTAuth 中间件之前
// 执行（如 /api/login）时，context 中没有 user_id，此时应通过 detail 中的 username
// 字段来追溯；helper 不强制要求 actor_user_id 非 0。
func RecordAudit(c *gin.Context, targetType string, targetID int64, action string, meta ...interface{}) {
	if c == nil {
		fmt.Printf("[audit] RecordAudit: gin.Context 为空, target=%s/%d action=%s\n",
			targetType, targetID, action)
		return
	}

	userID := int64(0)
	if v, ok := c.Get("user_id"); ok {
		switch x := v.(type) {
		case int64:
			userID = x
		case int:
			userID = int64(x)
		case uint:
			userID = int64(x)
		}
	}

	var detail string
	if len(meta) > 0 && meta[0] != nil {
		b, err := json.Marshal(meta[0])
		if err != nil {
			fmt.Printf("[audit] RecordAudit: json.Marshal meta 失败, target=%s/%d action=%s err=%v\n",
				targetType, targetID, action, err)
			detail = "{}"
		} else {
			detail = string(b)
		}
	} else {
		detail = "{}"
	}

	if _, err := AppendAudit(
		targetType,
		targetID,
		action,
		userID,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
		detail,
	); err != nil {
		// 失败仅 warn，不影响业务
		fmt.Printf("[audit] RecordAudit 写入失败, target=%s/%d action=%s err=%v\n",
			targetType, targetID, action, err)
	}
}
