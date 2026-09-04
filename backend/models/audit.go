package models

// ==================== 通用审计记录（append-only 哈希链）====================
//
// 司法 L4 合规：通用 append-only 审计表，支持多种 target 对象。
// 通过 target_type + target_id 区分不同业务实体。
//
// 权威 target_type 字典（与 database/audit_log.go AuditTargetType 常量一致）：
//
//	signature          签名
//	thirdparty         第三方合同（Trail 业务命名，使用无下划线风格）
//	reminder           提醒业务
//	rbac_role          RBAC 角色 CRUD
//	rbac_permission    RBAC 权限 CRUD
//	rbac_user_binding  RBAC 用户角色绑定
//	pdf_lock           合同 PDF 锁定（WORM）
//	customer           客户 CRUD + 签名上传
//	auth               认证（login/refresh/password.change）
//	backup             备份（manual/restore/verify）
//	scheduled_task     调度任务
//	system             系统高风险操作（维护模式等）
//
// 注意：以下类型已于 2026-09-04 全部下线（无对应业务实体 / 无 handler）。
//   - media            业务实体（media 表）保留，但审计写入走 media.audit JSON 环形字段
//                      （参 handlers/media.go），不再走本 audit_log 表。
//   - consent_letter   业务模块完全未上线（无对应表 / handler）；同时移除
//                      AppendAudit 中"target_id<0 仅 consent_letter 允许"的特殊分支，
//                      所有类型一律使用正整数 target_id。
//   - seal / contract / flow  历史保留位，业务已下线（印章模块 + 流程审批模块
//                      + 独立合同表单均未上线；合同业务统一在 thirdparty）。
//
// 权威 action 字典（按 target_type 分类）：
//
//	signature         sign / sign-date / sign-draft / sign-date-draft
//	thirdparty        create / status-change / delete
//	reminder          create / update / delete / subscribe /
//	                  update-sub / unsubscribe / scan
//	rbac_role         upsert / delete
//	rbac_permission   upsert / delete
//	rbac_user_binding assign_roles / update_roles
//	pdf_lock          pdf_lock / pdf_verify / pdf_verify_failed /
//	                  pdf_download / evidence_export
//	customer          create / update / delete / signature.upload
//	auth              login.success / login.failed /
//	                  refresh.success / refresh.failed /
//	                  password.change.success / password.change.failed
//	backup            manual.success / manual.failed /
//	                  restore.success / restore.failed /
//	                  verify.ok / verify.corrupted / verify.missing
//	scheduled_task    run.success / run.failed / run.timeout / run.skipped
//	system            maintenance.enable / maintenance.disable
//
// 设计要点：
//  1. append-only：DB 触发器禁止 UPDATE/DELETE（trg_audit_log_no_update/no_delete）
//  2. 哈希链：hash_sm3 = SM3(prev_hash + target_type + target_id + action +
//     actor_id + created_at + detail)
//     全表全局链（不按 target 分段），最高 L4 等级
//  3. 与业务事件指标（business_events_total）并存：指标用于实时告警，审计用于取证
// ----------------------------------------------------------------------------

// AuditRecord 通用审计记录。
type AuditRecord struct {
	ID         int64  `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Action     string `json:"action"`
	ActorID    int64  `json:"actor_id"`
	ActorIP    string `json:"actor_ip,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	Detail     string `json:"detail,omitempty"` // JSON
	HashSM3    string `json:"hash_sm3"`         // 本行 SM3 哈希（链式）
	PrevHash   string `json:"prev_hash"`        // 上一行 hash_sm3（哈希链）
	CreatedAt  int64  `json:"created_at"`
}
