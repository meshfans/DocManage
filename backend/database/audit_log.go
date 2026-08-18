package database

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emmansun/gmsm/sm3"

	"doc/models"
)

// ==================== 通用审计 append-only 表（SM3 哈希链）====================
//
// 设计目标：
//   1. 通用：支持多种 target 对象（media / signature / seal / contract / flow /
//      thirdparty / customer / rbac_* / auth / backup / scheduled_task ...）
//   2. append-only：应用层零 UPDATE/DELETE（DB 触发器 trg_audit_log_no_update/no_delete 兜底）
//   3. 哈希链：每行 hash_sm3 = SM3(prev_hash + target_type + target_id + action +
//                                  actor_id + created_at + detail)
//      - 全局链（不按 target 分段），最严格的司法 L4 等级（任何一行被改动全表校验失败）
//      - prev_hash = 全表最后一行的 hash_sm3
//      - 第一行 prev_hash = 'GENESIS'（创世块）
//   4. 验证：VerifyAuditChain() 逐行重算 hash 比对，任意不一致立即报错
//   5. 失败容忍：AppendAudit 返回错误时仅 warn，不阻塞业务事务（审计可丢、不可阻业务）
// ----------------------------------------------------------------------------

// HashChainGenesis 全局哈希链的"创世块"标识。第一行的 prev_hash 字段固定为该值。
const HashChainGenesis = "GENESIS"

// AuditTargetType 审计目标类型常量（与 models/audit.go 注释字典保持一致）。
const (
	AuditTargetMedia           = "media"
	AuditTargetSignature       = "signature"
	AuditTargetSeal            = "seal"
	AuditTargetContract        = "contract"   // 合同表单保存（form_json 修改）
	AuditTargetFlow            = "flow"       // 流转步骤提交
	AuditTargetThirdParty      = "thirdparty" // Trail 业务命名（无下划线，保持向后兼容）
	AuditTargetReminder        = "reminder"
	AuditTargetRBACRole        = "rbac_role"
	AuditTargetRBACPermission  = "rbac_permission"
	AuditTargetRBACUserBinding = "rbac_user_binding"
	AuditTargetConsentLetter   = "consent_letter"
	AuditTargetPDFLock         = "pdf_lock"
	AuditTargetCustomer        = "customer"
	AuditTargetAuth            = "auth"
	AuditTargetBackup          = "backup"
	AuditTargetScheduledTask   = "scheduled_task"
	AuditTargetSystem          = "system"
)

// auditLogCols 是查询 audit_log 表的列名列表。
const auditLogCols = `id, target_type, target_id, action, actor_id, actor_ip, user_agent,
                      detail, hash_sm3, prev_hash, created_at`

// auditLogMu 全表全局互斥锁，保证哈希链 prev_hash 顺序正确。
var auditLogMu sync.Mutex

// AppendAudit 写入一条审计记录（哈希链全局顺序）。
//
//   - targetType: 业务对象类型（见 AuditTargetType 常量）
//   - targetID:   关联对象 id（int64；consent_letter 允许 -1 标记匿名）
//   - action:     操作类型（按 targetType 区分字典）
//   - actorID:    操作用户 id（0 表示系统/匿名）
//   - actorIP:    客户端 IP（可空）
//   - userAgent:  UA（可空）
//   - detail:     JSON 字符串（可空，空时存 '{}'）
//
// 返回：新行的 id。
//
// 关键：
//   - 全表全局 mutex 串行化（保证 prev_hash 顺序正确）
//   - 在同一事务内查 prev_hash → 计算本行 hash → INSERT（保证原子性）
func AppendAudit(
	targetType string,
	targetID int64,
	action string,
	actorID int64,
	actorIP string,
	userAgent string,
	detail string,
) (int64, error) {
	if targetType == "" {
		return 0, fmt.Errorf("target_type 不能为空")
	}
	// consent_letter 允许 target_id == -1（标记匿名+无客户的意愿书阅读）
	// auth 允许 target_id == 0（登录失败时用户不存在，无 target）
	if targetID == 0 && targetType != AuditTargetAuth {
		return 0, fmt.Errorf("target_id 非法: %d", targetID)
	}
	if targetID < 0 && targetType != AuditTargetConsentLetter {
		return 0, fmt.Errorf("target_id < 0 仅 consent_letter 允许")
	}
	if action == "" {
		return 0, fmt.Errorf("action 不能为空")
	}

	auditLogMu.Lock()
	defer auditLogMu.Unlock()
	return appendAuditNoLock(targetType, targetID, action, actorID, actorIP, userAgent, detail)
}

// appendAuditNoLock 在锁内执行实际的 audit_log INSERT。
// 调用方必须先获取 auditLogMu。
func appendAuditNoLock(
	targetType string,
	targetID int64,
	action string,
	actorID int64,
	actorIP string,
	userAgent string,
	detail string,
) (int64, error) {
	if detail == "" {
		detail = "{}"
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op if Commit succeeds

	// 1) 查全表最后一行 hash
	var prevHash string
	row := tx.QueryRow(`SELECT hash_sm3 FROM audit_log ORDER BY id DESC LIMIT 1`)
	if err := row.Scan(&prevHash); err != nil {
		if err != sql.ErrNoRows {
			return 0, fmt.Errorf("查询最后一行 hash 失败: %w", err)
		}
		prevHash = HashChainGenesis
	}

	// 2) 用 Go 端 time.Now().Unix() 作为 created_at 显式传入，保证 hash 与 DB 一致
	now := time.Now().Unix()

	// 3) 算 hash
	hashInput := strings.Join([]string{
		prevHash,
		targetType,
		strconv.FormatInt(targetID, 10),
		action,
		strconv.FormatInt(actorID, 10),
		strconv.FormatInt(now, 10),
		detail,
	}, "|")
	sum := sm3.Sum([]byte(hashInput))
	hashHex := hex.EncodeToString(sum[:])

	// 4) INSERT
	res, err := tx.Exec(`
		INSERT INTO audit_log (
			target_type, target_id, action, actor_id, actor_ip, user_agent,
			detail, hash_sm3, prev_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		targetType, targetID, action, actorID, actorIP, userAgent,
		detail, hashHex, prevHash, now,
	)
	if err != nil {
		return 0, fmt.Errorf("INSERT audit_log 失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return res.LastInsertId()
}

// ListAudit 列出审计记录（按 target_type / target_id / actor_id / action / 时间范围过滤）。
//
//   - targetType: "" = 全部
//   - targetID:   0  = 全部（仅当 targetType 不为空时有效）
//   - actorID:    0  = 全部
//   - action:     "" = 全部
//   - fromTs:     0  = 不限
//   - toTs:       0  = 不限
//   - page:       1-based，默认 1
//   - pageSize:   <= 0 默认 20
//
// 返回 ([]AuditRecord, total, error)。
func ListAudit(
	targetType string,
	targetID, actorID int64,
	action string,
	fromTs, toTs int64,
	page, pageSize int,
) ([]models.AuditRecord, int, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}

	whereParts := []string{}
	args := []interface{}{}
	if targetType != "" {
		whereParts = append(whereParts, "target_type = ?")
		args = append(args, targetType)
	}
	if targetID > 0 {
		whereParts = append(whereParts, "target_id = ?")
		args = append(args, targetID)
	}
	if actorID > 0 {
		whereParts = append(whereParts, "actor_id = ?")
		args = append(args, actorID)
	}
	if action != "" {
		whereParts = append(whereParts, "action = ?")
		args = append(args, action)
	}
	if fromTs > 0 {
		whereParts = append(whereParts, "created_at >= ?")
		args = append(args, fromTs)
	}
	if toTs > 0 {
		whereParts = append(whereParts, "created_at <= ?")
		args = append(args, toTs)
	}
	where := strings.Join(whereParts, " AND ")
	if where != "" {
		where = "WHERE " + where
	}

	// COUNT
	var total int
	if err := DB.QueryRow("SELECT COUNT(*) FROM audit_log "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 列表
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := DB.Query(`SELECT `+auditLogCols+` FROM audit_log `+where+
		` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []models.AuditRecord
	for rows.Next() {
		var r models.AuditRecord
		if err := rows.Scan(
			&r.ID, &r.TargetType, &r.TargetID, &r.Action, &r.ActorID, &r.ActorIP, &r.UserAgent,
			&r.Detail, &r.HashSM3, &r.PrevHash, &r.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	if list == nil {
		list = []models.AuditRecord{}
	}
	return list, total, rows.Err()
}

// VerifyAuditChain 校验全局哈希链。
// 遍历全表（按 id 升序），逐行重算 hash 并与 stored hash_sm3 比对。
// 同时校验 prev_hash 等于上一行 hash_sm3（第一行 prev_hash 必须等于 HashChainGenesis）。
//
// 返回：(brokenAt int64, err error)
//   - brokenAt: 第一个不匹配的行 id；0 表示整链完整
//   - err:      校验过程中的错误（如 SQL 失败）
//
// 用法：管理员/合规工具调用，例如每日 reconcile_audit 任务。
func VerifyAuditChain() (int64, error) {
	rows, err := DB.Query(`SELECT ` + auditLogCols + ` FROM audit_log ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	expectedPrev := HashChainGenesis
	for rows.Next() {
		var r models.AuditRecord
		if err := rows.Scan(
			&r.ID, &r.TargetType, &r.TargetID, &r.Action, &r.ActorID, &r.ActorIP, &r.UserAgent,
			&r.Detail, &r.HashSM3, &r.PrevHash, &r.CreatedAt,
		); err != nil {
			return 0, err
		}
		// 校验 prev_hash
		if r.PrevHash != expectedPrev {
			return r.ID, fmt.Errorf("prev_hash 不匹配: 行 id=%d, 期望=%q, 实际=%q",
				r.ID, expectedPrev, r.PrevHash)
		}
		// 重算 hash
		hashInput := strings.Join([]string{
			r.PrevHash,
			r.TargetType,
			strconv.FormatInt(r.TargetID, 10),
			r.Action,
			strconv.FormatInt(r.ActorID, 10),
			strconv.FormatInt(r.CreatedAt, 10),
			r.Detail,
		}, "|")
		sum := sm3.Sum([]byte(hashInput))
		actual := hex.EncodeToString(sum[:])
		if actual != r.HashSM3 {
			return r.ID, fmt.Errorf("hash_sm3 不匹配: 行 id=%d, 期望=%q, 实际=%q",
				r.ID, r.HashSM3, actual)
		}
		expectedPrev = r.HashSM3
	}
	return 0, rows.Err()
}
