package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emmansun/gmsm/sm3"

	"doc/models"
)

// ==================== 通用审计 append-only 表（SM3 哈希链）====================
//
// 设计目标：
//   1. 通用：支持多种 target 对象（signature / thirdparty / customer /
//      rbac_* / auth / backup / scheduled_task ...）
//      历史下线：media / consent_letter / seal / contract / flow
//      （2026-09-04 统一处理，参 doc string 下方说明）。
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

// AuditTargetType 审计目标类型常量（12 个活跃 + 5 个 2026-09-04 下线，见下方）。
// 权威 target_type 字典见 models/audit.go 顶部注释。
//
// 历史：以下类型已于 2026-09-04 全部下线（无对应业务实体 / 无 handler），
//   - seal           印章（handlers/seal.go 已删除，2026-07-06 round 5/7）
//   - contract       合同表单保存（统一为 thirdparty；保留 third_party_contract 表）
//   - flow           流转步骤（流程审批模块整体未上线）
//   - consent_letter 意愿确认书（业务完全未上线；并清掉 AppendAudit 的 targetID<0 特例）
//
// 当前字典见 models/audit.go 顶部注释。
const (
	AuditTargetSignature       = "signature"
	AuditTargetThirdParty      = "thirdparty" // Trail 业务命名（无下划线，保持向后兼容）
	AuditTargetReminder        = "reminder"
	AuditTargetRBACRole        = "rbac_role"
	AuditTargetRBACPermission  = "rbac_permission"
	AuditTargetRBACUserBinding = "rbac_user_binding"
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
//   - targetID:   关联对象 id（int64；auth / system / backup 允许 0 表示无 target；负值一律拒绝）
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
//
// ============================================================================
// ⚠️ BREAKING CHANGE（Round 17）：hash 算法变更
// ============================================================================
// 旧算法：strings.Join([prevHash, targetType, targetID, action, actorID,
//                       createdAt, detail], "|")
// 新算法：buildHashInput(...) 用 length-prefix 编码 "L%020d|<原值>"，
//         并显式加入 actorIP + userAgent 两个字段
//
// 部署此 commit 前**必须**在 upgrade guide 中执行（与运维对齐）：
//   1. 停服；
//   2. 执行 PRAGMA disable_trigger（绕过 append-only 触发器）：
//      sqlite3 backend/bin/data/doc.db "DROP TRIGGER trg_audit_log_no_update"
//      sqlite3 backend/bin/data/doc.db "DROP TRIGGER trg_audit_log_no_delete"
//   3. 清空 audit_log：
//      sqlite3 backend/bin/data/doc.db "DELETE FROM audit_log"
//      sqlite3 backend/bin/data/doc.db "DELETE FROM sqlite_sequence
//                                       WHERE name='audit_log'"
//   4. 重启服务（InitDatabase 会重建触发器与表结构）；
//   5. 跑一次 POST /api/audit/reconcile 验证 {ok:true}。
//
// 当前 dev / 测试环境 audit_log 仍为测试数据，按用户决策"强制清空表"执行。
// ============================================================================
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
	// auth 允许 target_id == 0（登录失败时用户不存在，无 target）
	// system / backup 也允许 target_id == 0（cron job 类无具体业务对象的系统级事件）
	// 历史：consent_letter 曾允许 target_id == -1（匿名意愿书阅读），
	//       2026-09-04 consent_letter 类型下线 → 一律拒绝负值。
	if targetID == 0 && targetType != AuditTargetAuth &&
		targetType != AuditTargetSystem && targetType != AuditTargetBackup {
		return 0, fmt.Errorf("target_id 非法: %d", targetID)
	}
	if targetID < 0 {
		return 0, fmt.Errorf("target_id < 0 不允许 (所有类型均为正整数 id)")
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
//
// Issue #17：auditLogMu 全局 mutex + Go 端事务包裹保证 prev_hash 串行写入。
// 当前实现不引入 SQLite IMMEDIATE 锁升级（实现复杂、且 sqlite3 driver 与
// sql.Tx 嵌套 BEGIN 的兼容性会带来「database disk image is malformed」风险）。
// 串行化 mutex + 单一事务已经足够保证 prev_hash 链正确性；
// 后续如需去掉 mutex，可在 DSN 加 `_txlock=immediate` 全局生效。
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

	// 3) 算 hash（Issue #15 #16）：
	//   - 输入字段按 length-prefix 编码（`%020d|<原始字段>`），消除 `|` 分隔符歧义
	//     （detail 中含 `|` 时不会与字段边界混淆）。
	//   - 显式纳入 actorIP + userAgent（IP/UA 一旦被改可被 hash 链检测到）。
	hashInput := buildHashInput(prevHash, targetType, targetID, action, actorID, actorIP, userAgent, now, detail)
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
//
// Issue #14：admin 取消查询或客户端断连时透传 ctx 让 SQL 中止；
// 大表 OFFSET 性能问题改用 cursor（beforeID < ? ORDER BY id DESC LIMIT ?）。
// 这里保留向后兼容：传 cursorID > 0 时用 cursor；否则按 page/pageSize 做 OFFSET（保留旧接口）。
func ListAudit(
	targetType string,
	targetID, actorID int64,
	action string,
	fromTs, toTs int64,
	page, pageSize int,
) ([]models.AuditRecord, int, error) {
	return ListAuditCtx(context.Background(), targetType, targetID, actorID, action, fromTs, toTs, page, pageSize, 0)
}

// ListAuditCtx 支持 ctx 透传 + cursor 分页的版本。
//
//   - ctx:        透传调用方上下文（HTTP 取消 / 超时）
//   - cursorID:   >0 时使用 cursor 分页（`WHERE id < cursorID ORDER BY id DESC LIMIT ?`），
//                 比 OFFSET 在大表上快且一致；传 0 则按 page/pageSize 计算 OFFSET
//   - pageSize:   <= 0 默认 20
//   - page:       cursor 模式下忽略；OFFSET 模式下 1-based，默认 1
func ListAuditCtx(
	ctx context.Context,
	targetType string,
	targetID, actorID int64,
	action string,
	fromTs, toTs int64,
	page, pageSize int,
	cursorID int64,
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

	// Issue #14：cursor 分页
	useCursor := cursorID > 0
	if useCursor {
		whereParts = append(whereParts, "id < ?")
		args = append(args, cursorID)
	}

	where := strings.Join(whereParts, " AND ")
	if where != "" {
		where = "WHERE " + where
	}

	// COUNT（Issue #14：admin 取消/超时透传到 SQL）
	var total int
	if err := DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 列表
	var (
		listArgs []interface{}
		listSQL  string
	)
	if useCursor {
		// cursor 模式：拿 pageSize 条即可；不再做 OFFSET
		listArgs = append(append([]interface{}{}, args...), pageSize)
		listSQL = `SELECT ` + auditLogCols + ` FROM audit_log ` + where +
			` ORDER BY id DESC LIMIT ?`
	} else {
		offset := (page - 1) * pageSize
		listArgs = append(append([]interface{}{}, args...), pageSize, offset)
		listSQL = `SELECT ` + auditLogCols + ` FROM audit_log ` + where +
			` ORDER BY id DESC LIMIT ? OFFSET ?`
	}
	rows, err := DB.QueryContext(ctx, listSQL, listArgs...)
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
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
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
		// 重算 hash（Issue #15 #16：与 AppendAudit 一致的长前缀编码）
		hashInput := buildHashInput(r.PrevHash, r.TargetType, r.TargetID, r.Action,
			r.ActorID, r.ActorIP, r.UserAgent, r.CreatedAt, r.Detail)
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

// buildHashInput 用 length-prefix 编码构造 hash 输入。
//
// 每个字段按 `%020d|<原值>` 拼装：
//   - 字段长度用 20 位十进制宽度表示，长度差异常显著（最大 99999999999999999999 也只是边界）
//   - 原始字段不再需要转义，因为分隔符 `|` 不会与字段内 `|` 混淆
//   - 即使 detail 含 `|`、`{`、`}` 等任意字符，编码仍唯一可解析
//
// 字段顺序与历史实现保持兼容：prevHash, targetType, targetID, action, actorID,
// actorIP, userAgent, createdAt, detail。
func buildHashInput(
	prevHash, targetType string,
	targetID int64,
	action string,
	actorID int64,
	actorIP, userAgent string,
	createdAt int64,
	detail string,
) string {
	var b strings.Builder
	// 用 fmt.Fprintf 的格式化串 `L%020d|%s` 做 length-prefix
	fmt.Fprintf(&b, "L%020d|%s", len(prevHash), prevHash)
	fmt.Fprintf(&b, "L%020d|%s", len(targetType), targetType)
	fmt.Fprintf(&b, "L%020d|%d", 20, targetID) // int64 固定长度 20
	fmt.Fprintf(&b, "L%020d|%s", len(action), action)
	fmt.Fprintf(&b, "L%020d|%d", 20, actorID)
	fmt.Fprintf(&b, "L%020d|%s", len(actorIP), actorIP)
	fmt.Fprintf(&b, "L%020d|%s", len(userAgent), userAgent)
	fmt.Fprintf(&b, "L%020d|%d", 20, createdAt)
	fmt.Fprintf(&b, "L%020d|%s", len(detail), detail)
	return b.String()
}
