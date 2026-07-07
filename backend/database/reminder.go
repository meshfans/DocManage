package database

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 本文件：第十一阶段 提醒业务（reminder_template / reminder_subscription / reminder_log）的 CRUD。
//
// 设计要点：
//   - 模板（template）+ 订阅（subscription）+ 日志（log）三层
//   - 订阅支持多态关联：客户级 / 第三方合同级
//   - log 表 UNIQUE (template_id, contract_id, trigger_date) 由 DB 层去重
//   - contract_no / contract_title / customer_name 冗余存储（防关联删除后无法查询）

// ==================== ReminderTemplate ====================

// ReminderTemplate 提醒模板。
// RuleType 取值：contract_expiring（合同到期前 N 天）| future: birthday / contract_overdue。
// IsSystem=1 表示系统预置不可删。
type ReminderTemplate struct {
	ID          int64  `json:"id"`
	TemplateKey string `json:"template_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RuleType    string `json:"rule_type"`
	AdvanceDays int    `json:"advance_days"`
	IsActive    bool   `json:"is_active"`
	IsSystem    bool   `json:"is_system"`
	SortOrder   int    `json:"sort_order"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

const reminderTemplateCols = `
	id, template_key, name, description, rule_type, advance_days,
	is_active, is_system, sort_order, created_at, updated_at
`

// CreateReminderTemplate 插入一条模板。
func CreateReminderTemplate(t *ReminderTemplate) (int64, error) {
	now := time.Now().Unix()
	if t.CreatedAt == 0 {
		t.CreatedAt = now
	}
	if t.UpdatedAt == 0 {
		t.UpdatedAt = now
	}
	res, err := DB.Exec(`
		INSERT INTO reminder_template
			(template_key, name, description, rule_type, advance_days,
			 is_active, is_system, sort_order, created_at, updated_at)
		VALUES (?,?,?,?,?, ?,?,?,?,?)
	`,
		t.TemplateKey, t.Name, t.Description, t.RuleType, t.AdvanceDays,
		boolToInt(t.IsActive), boolToInt(t.IsSystem), t.SortOrder,
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetReminderTemplateByID 按 id 查询。
func GetReminderTemplateByID(id int64) (*ReminderTemplate, error) {
	return scanOneReminderTemplate(DB.QueryRow(
		`SELECT `+reminderTemplateCols+` FROM reminder_template WHERE id = ?`, id))
}

// GetReminderTemplateByKey 按 template_key 查询（启动种子幂等用）。
func GetReminderTemplateByKey(key string) (*ReminderTemplate, error) {
	return scanOneReminderTemplate(DB.QueryRow(
		`SELECT `+reminderTemplateCols+` FROM reminder_template WHERE template_key = ?`, key))
}

// ListReminderTemplates 列出全部模板（管理页用，按 sort_order + id 排序）。
func ListReminderTemplates() ([]ReminderTemplate, error) {
	rows, err := DB.Query(`SELECT ` + reminderTemplateCols + `
		FROM reminder_template ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReminderTemplate{}
	for rows.Next() {
		t, err := scanReminderTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveReminderTemplates 列出启用的模板（扫描器用）。
func ListActiveReminderTemplates() ([]ReminderTemplate, error) {
	rows, err := DB.Query(`SELECT ` + reminderTemplateCols + `
		FROM reminder_template WHERE is_active = 1 ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReminderTemplate{}
	for rows.Next() {
		t, err := scanReminderTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// UpdateReminderTemplate 更新模板（不可改 template_key / is_system）。
func UpdateReminderTemplate(t *ReminderTemplate) error {
	_, err := DB.Exec(`
		UPDATE reminder_template SET
			name = ?, description = ?, rule_type = ?, advance_days = ?,
			is_active = ?, sort_order = ?, updated_at = ?
		WHERE id = ?
	`,
		t.Name, t.Description, t.RuleType, t.AdvanceDays,
		boolToInt(t.IsActive), t.SortOrder, time.Now().Unix(), t.ID)
	return err
}

// DeleteReminderTemplate 删除模板（is_system=1 不可删，由 handler 校验）。
func DeleteReminderTemplate(id int64) error {
	_, err := DB.Exec(`DELETE FROM reminder_template WHERE id = ?`, id)
	return err
}

// CountActiveSubscriptionsByTemplate 统计某模板下的启用订阅数（用于"删除前检查"）。
func CountActiveSubscriptionsByTemplate(templateID int64) (int, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM reminder_subscription WHERE template_id = ? AND is_active = 1`,
		templateID).Scan(&n)
	return n, err
}

func scanOneReminderTemplate(row *sql.Row) (*ReminderTemplate, error) {
	t := &ReminderTemplate{}
	var isActive, isSystem int
	var description sql.NullString
	err := row.Scan(
		&t.ID, &t.TemplateKey, &t.Name, &description, &t.RuleType, &t.AdvanceDays,
		&isActive, &isSystem, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.IsActive = isActive != 0
	t.IsSystem = isSystem != 0
	if description.Valid {
		t.Description = description.String
	}
	return t, nil
}

func scanReminderTemplate(s rowScanner) (*ReminderTemplate, error) {
	t := &ReminderTemplate{}
	var isActive, isSystem int
	var description sql.NullString
	err := s.Scan(
		&t.ID, &t.TemplateKey, &t.Name, &description, &t.RuleType, &t.AdvanceDays,
		&isActive, &isSystem, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.IsActive = isActive != 0
	t.IsSystem = isSystem != 0
	if description.Valid {
		t.Description = description.String
	}
	return t, nil
}

// ==================== ReminderSubscription ====================

// ReminderSubscription 提醒订阅。
//
// 第十三阶段 v4：通用多态关联
//   - LinkType: 'customer' / 'third_party_contract'
//   - LinkID: CSV 字符串（"5" / "1,2,3"），应用层 ParseReceiverIDs() 解析
//   - 至少 1 个有效 link_id（必填，handler 校验）
//
// 第十三阶段：ReceiverType / ReceiverID 决定提醒发给谁。
//   - "admin"          : 第一个 admin（receiver_id 忽略），v1 行为
//   - "contract_owner" : contract.created_by（receiver_id 忽略）
//   - "customer_owner" : customer.owner_user_id（receiver_id 忽略）
//   - "department"     : 部门内所有用户（receiver_id 解析为部门 ID 列表）
//   - "user"           : 指定用户（receiver_id 解析为用户 ID 列表）
//
// 第十三阶段 v2：ReceiverID 改为 string（CSV 格式），支持多 ID。
//   - ""  / "0"      : 空（admin / *_owner 类型占位）
//   - "5"             : 单 ID 5
//   - "1,2,3"         : 多 ID [1, 2, 3]（自动去重）
// 应用层 ParseReceiverIDs() 负责解析。
type ReminderSubscription struct {
	ID         int64  `json:"id"`
	TemplateID int64  `json:"template_id"`
	// 第十三阶段 v4：通用多态关联
	LinkType string `json:"link_type"`
	LinkID   string `json:"link_id"` // CSV 字符串
	// 第十三阶段：接收人
	ReceiverType string `json:"receiver_type"`
	ReceiverID   string `json:"receiver_id"` // CSV 字符串
	IsActive     bool   `json:"is_active"`
	Remark       string `json:"remark"`
	CreatedBy    int64  `json:"created_by"`
	// 2026-06-29 RBAC v3 P1：data_scope 过滤维度（创建者主部门快照）
	DepartmentID int64 `json:"department_id"`
	CreatedAt    int64 `json:"created_at"`
	UpdatedAt    int64 `json:"updated_at"`

	// 联表冗余字段（JOIN 时填充，方便前端展示）
	// 注意：customer_name / contract_no / contract_title 在 v4 中改为 link_*
	TemplateName    string `json:"template_name,omitempty"`
	TemplateKey     string `json:"template_key,omitempty"`
	TemplateAdvance int    `json:"template_advance_days,omitempty"`

	// v4：冗余展示用 link 关联的"显示名"（按 link_type 不同表）
	LinkDisplayName string `json:"link_display_name,omitempty"` // 通用：客户名 / 合同标题
	LinkSubInfo     string `json:"link_sub_info,omitempty"`     // 通用：合同号 / 客户 ID
}

const reminderSubscriptionCols = `
	rs.id, rs.template_id,
	rs.link_type, rs.link_id,
	rs.receiver_type, rs.receiver_id,
	rs.is_active, rs.remark, rs.created_by,
	COALESCE(rs.department_id, 0) AS department_id,
	rs.created_at, rs.updated_at
`

// reminderSubscriptionJoinCols v4 简化：link_* 改为运行时解析（按 link_type 不同表 JOIN）
// 暂时保留一个简化版：先 JOIN 客户（90% 场景）；后续可优化为 CASE WHEN
const reminderSubscriptionJoinCols = reminderSubscriptionCols + `,
	rt.name AS template_name, rt.template_key, rt.advance_days AS template_advance_days
	-- link_display_name / link_sub_info 由 handler 层根据 link_type 单独查询后填充
`

// reminderSubscriptionJoinCols 注释：
// v4：去掉 cu/co/tpc JOIN（link_type 多态后无法用单一 JOIN）
// 改为：handler 层根据 link_type 调用 GetLinkDisplayName(linkType, linkID) 取得显示名
//   - 'customer'             → customer.real_name / company_name
//   - 'third_party_contract' → third_party_contract.contract_no / title

// CreateReminderSubscription 插入订阅。
// 第十三阶段 v4：写 link_type + link_id（CSV）。空值兜底为 'customer' / '0'。
func CreateReminderSubscription(s *ReminderSubscription) (int64, error) {
	now := time.Now().Unix()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	if s.UpdatedAt == 0 {
		s.UpdatedAt = now
	}
	if s.LinkType == "" {
		s.LinkType = "customer"
	}
	if s.LinkID == "" {
		s.LinkID = "0"
	}
	if s.ReceiverType == "" {
		s.ReceiverType = "admin"
	}
	if s.ReceiverID == "" {
		s.ReceiverID = "0"
	}
	res, err := DB.Exec(`
		INSERT INTO reminder_subscription
			(template_id, link_type, link_id,
			 receiver_type, receiver_id,
			 is_active, remark, created_by, department_id, created_at, updated_at)
		VALUES (?,?,?, ?,?, ?,?,?,?,?,?)
	`,
		s.TemplateID, s.LinkType, s.LinkID,
		s.ReceiverType, s.ReceiverID,
		boolToInt(s.IsActive),
		s.Remark, s.CreatedBy, s.DepartmentID, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetReminderSubscriptionByID 按 id 查询（带联表）。
// v4：去掉 customer/contract/tpc JOIN，由 handler 层按 link_type 单独查询显示名。
func GetReminderSubscriptionByID(id int64) (*ReminderSubscription, error) {
	return scanOneReminderSubscription(DB.QueryRow(
		`SELECT `+reminderSubscriptionJoinCols+`
		 FROM reminder_subscription rs
		 LEFT JOIN reminder_template rt ON rs.template_id = rt.id
		 WHERE rs.id = ?`, id))
}

// ListAllActiveSubscriptions 拉取全部启用的订阅（扫描器用）。
// 返回字段：id / template_id / link_type / link_id / receiver_type / receiver_id（精简列，性能考虑）。
// 第十三阶段 v4：link_type + link_id（CSV，多 ID）。
func ListAllActiveSubscriptions() ([]ReminderSubscription, error) {
	rows, err := DB.Query(`
		SELECT id, template_id, link_type, link_id, receiver_type, receiver_id
		FROM reminder_subscription WHERE is_active = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReminderSubscription{}
	for rows.Next() {
		s := ReminderSubscription{IsActive: true}
		var linkID, receiverID sql.NullString
		if err := rows.Scan(&s.ID, &s.TemplateID, &s.LinkType, &linkID, &s.ReceiverType, &receiverID); err != nil {
			return nil, err
		}
		if linkID.Valid {
			s.LinkID = linkID.String
		} else {
			s.LinkID = "0"
		}
		if receiverID.Valid {
			s.ReceiverID = receiverID.String
		} else {
			s.ReceiverID = "0"
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListAllSubscriptions 分页 + 过滤（管理页用）。
// v4：用 link_type / link_id 替代 customer_id / contract_id。
//   - linkType != "" + linkID > 0：按 link_type 过滤 + CSV 包含 linkID
//   - linkType == "" + linkID > 0：默认按第三方合同级查（link_type IN 'third_party_contract'）
//   - linkID == 0：不按 link 过滤
func ListAllSubscriptions(templateID int64, linkType string, linkID int64, isActive *bool, page, pageSize int) ([]ReminderSubscription, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if templateID > 0 {
		where = append(where, "rs.template_id = ?")
		args = append(args, templateID)
	}
	if linkID > 0 {
		if linkType == "" {
			// 默认按第三方合同级查
			where = append(where, `rs.link_type IN ('third_party_contract')`)
		} else {
			where = append(where, "rs.link_type = ?")
			args = append(args, linkType)
		}
		idStr := strconv.FormatInt(linkID, 10)
		where = append(where, `(',' || rs.link_id || ',') LIKE '%,` + idStr + `,%'`)
	}
	if isActive != nil {
		where = append(where, "rs.is_active = ?")
		args = append(args, boolToInt(*isActive))
	}
	whereStr := joinAnd(where)

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM reminder_subscription rs WHERE `+whereStr, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)

	rows, err := DB.Query(
		`SELECT `+reminderSubscriptionJoinCols+`
		 FROM reminder_subscription rs
		 LEFT JOIN reminder_template rt ON rs.template_id = rt.id
		 WHERE `+whereStr+` ORDER BY rs.created_at DESC, rs.id DESC LIMIT ? OFFSET ?`,
		listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReminderSubscription{}
	for rows.Next() {
		s, err := scanReminderSubscription(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}

// ListAllSubscriptionsByDataScope 2026-06-29 RBAC v3 P1：data_scope + 丰富过滤 的订阅列表。
// 与 ListAllSubscriptions 同形参 + extraWhere/extraArgs（BuildWhereSQL 输出）。
// 行为：rich filter AND data_scope filter；任一为空则降级对应部分。
// 用途：handler 拿到 UserDataScope 后拼 WHERE 一次性查。
func ListAllSubscriptionsByDataScope(templateID int64, linkType string, linkID int64, isActive *bool, page, pageSize int, extraWhere string, extraArgs []any) ([]ReminderSubscription, int, error) {
	// 复制 ListAllSubscriptions 的 rich filter 拼装
	where := []string{"1=1"}
	args := []interface{}{}
	if templateID > 0 {
		where = append(where, "rs.template_id = ?")
		args = append(args, templateID)
	}
	if linkID > 0 {
		if linkType == "" {
			where = append(where, `rs.link_type IN ('third_party_contract')`)
		} else {
			where = append(where, "rs.link_type = ?")
			args = append(args, linkType)
		}
		idStr := strconv.FormatInt(linkID, 10)
		where = append(where, `(',' || rs.link_id || ',') LIKE '%,` + idStr + `,%'`)
	}
	if isActive != nil {
		where = append(where, "rs.is_active = ?")
		args = append(args, boolToInt(*isActive))
	}
	// 拼接 data_scope extraWhere
	cleanExtra, ok := adaptDataScopeWhere(extraWhere, true)
	if ok {
		where = append(where, "("+cleanExtra+")")
		args = append(args, extraArgs...)
	}
	whereStr := joinAnd(where)

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM reminder_subscription rs WHERE `+whereStr, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)

	rows, err := DB.Query(
		`SELECT `+reminderSubscriptionJoinCols+`
		 FROM reminder_subscription rs
		 LEFT JOIN reminder_template rt ON rs.template_id = rt.id
		 WHERE `+whereStr+` ORDER BY rs.created_at DESC, rs.id DESC LIMIT ? OFFSET ?`,
		listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReminderSubscription{}
	for rows.Next() {
		s, err := scanReminderSubscription(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}

// UpdateReminderSubscription 更新订阅（仅 is_active / remark 可改）。
func UpdateReminderSubscription(s *ReminderSubscription) error {
	_, err := DB.Exec(`
		UPDATE reminder_subscription
		SET is_active = ?, remark = ?, updated_at = ?
		WHERE id = ?
	`, boolToInt(s.IsActive), s.Remark, time.Now().Unix(), s.ID)
	return err
}

// DeleteReminderSubscription 删除订阅。
func DeleteReminderSubscription(id int64) error {
	_, err := DB.Exec(`DELETE FROM reminder_subscription WHERE id = ?`, id)
	return err
}

func scanOneReminderSubscription(row *sql.Row) (*ReminderSubscription, error) {
	s := &ReminderSubscription{}
	var isActive int
	var remark, tmplName, tmplKey sql.NullString
	var templateAdvance sql.NullInt64
	var receiverType, receiverIDStr, linkTypeStr, linkIDStr sql.NullString
	err := row.Scan(
		&s.ID, &s.TemplateID,
		&linkTypeStr, &linkIDStr,
		&receiverType, &receiverIDStr,
		&isActive, &remark, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
		&tmplName, &tmplKey, &templateAdvance,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.IsActive = isActive != 0
	if linkTypeStr.Valid {
		s.LinkType = linkTypeStr.String
	} else {
		s.LinkType = "customer" // 兜底
	}
	if linkIDStr.Valid {
		s.LinkID = linkIDStr.String
	} else {
		s.LinkID = "0"
	}
	if receiverType.Valid {
		s.ReceiverType = receiverType.String
	} else {
		s.ReceiverType = "admin" // 兜底（兼容旧数据）
	}
	if receiverIDStr.Valid {
		s.ReceiverID = receiverIDStr.String
	} else {
		s.ReceiverID = "0"
	}
	if remark.Valid {
		s.Remark = remark.String
	}
	if tmplName.Valid {
		s.TemplateName = tmplName.String
	}
	if tmplKey.Valid {
		s.TemplateKey = tmplKey.String
	}
	if templateAdvance.Valid {
		s.TemplateAdvance = int(templateAdvance.Int64)
	}
	return s, nil
}

func scanReminderSubscription(s rowScanner) (*ReminderSubscription, error) {
	sub := &ReminderSubscription{}
	var isActive int
	var remark, tmplName, tmplKey sql.NullString
	var templateAdvance sql.NullInt64
	var receiverType, receiverIDStr, linkTypeStr, linkIDStr sql.NullString
	err := s.Scan(
		&sub.ID, &sub.TemplateID,
		&linkTypeStr, &linkIDStr,
		&receiverType, &receiverIDStr,
		&isActive, &remark, &sub.CreatedBy, &sub.DepartmentID, &sub.CreatedAt, &sub.UpdatedAt,
		&tmplName, &tmplKey, &templateAdvance,
	)
	if err != nil {
		return nil, err
	}
	sub.IsActive = isActive != 0
	if linkTypeStr.Valid {
		sub.LinkType = linkTypeStr.String
	} else {
		sub.LinkType = "customer"
	}
	if linkIDStr.Valid {
		sub.LinkID = linkIDStr.String
	} else {
		sub.LinkID = "0"
	}
	if receiverType.Valid {
		sub.ReceiverType = receiverType.String
	} else {
		sub.ReceiverType = "admin" // 兜底（兼容旧数据）
	}
	if receiverIDStr.Valid {
		sub.ReceiverID = receiverIDStr.String
	} else {
		sub.ReceiverID = "0"
	}
	if remark.Valid {
		sub.Remark = remark.String
	}
	if tmplName.Valid {
		sub.TemplateName = tmplName.String
	}
	if tmplKey.Valid {
		sub.TemplateKey = tmplKey.String
	}
	if templateAdvance.Valid {
		sub.TemplateAdvance = int(templateAdvance.Int64)
	}
	return sub, nil
}

func listReminderSubscriptions(query string, args ...interface{}) ([]ReminderSubscription, error) {
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReminderSubscription{}
	for rows.Next() {
		s, err := scanReminderSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// ==================== ReminderLog ====================

// ReminderLog 提醒发送记录（去重：UNIQUE template_id+contract_id+trigger_date）。
// ContractNo / ContractTitle / CustomerName 冗余（防关联删除后查询失败）。
type ReminderLog struct {
	ID             int64  `json:"id"`
	TemplateID     int64  `json:"template_id"`
	SubscriptionID *int64 `json:"subscription_id"`
	CustomerID     *int64 `json:"customer_id"`
	ContractID     *int64 `json:"contract_id"` // 第三方合同 id 或 0
	ContractNo     string `json:"contract_no"`
	ContractTitle  string `json:"contract_title"`
	CustomerName   string `json:"customer_name"`
	TriggerDate    string `json:"trigger_date"` // YYYY-MM-DD
	DaysBefore     int    `json:"days_before"`
	MessageID      *int64 `json:"message_id"`
	DeliveryStatus string `json:"delivery_status"` // sent / failed
	ErrorMsg       string `json:"error_msg"`
	TriggeredBy    string `json:"triggered_by"` // scheduler / manual
	CreatedAt      int64  `json:"created_at"`
}

const reminderLogCols = `
	id, template_id, subscription_id, customer_id, contract_id,
	contract_no, contract_title, customer_name, trigger_date, days_before,
	message_id, delivery_status, error_msg, triggered_by, created_at
`

// CreateReminderLog 插入日志（依赖 UNIQUE 约束去重）。
// 若 (template_id, contract_id, trigger_date) 已存在，返回 sql.ErrNoRows（caller 可忽略）。
func CreateReminderLog(l *ReminderLog) (int64, error) {
	now := time.Now().Unix()
	if l.CreatedAt == 0 {
		l.CreatedAt = now
	}
	res, err := DB.Exec(`
		INSERT INTO reminder_log
			(template_id, subscription_id, customer_id, contract_id,
			 contract_no, contract_title, customer_name, trigger_date, days_before,
			 message_id, delivery_status, error_msg, triggered_by, created_at)
		VALUES (?,?,?,?, ?,?,?,?,?, ?,?,?,?,?)
	`,
		l.TemplateID, l.SubscriptionID, l.CustomerID, l.ContractID,
		l.ContractNo, l.ContractTitle, l.CustomerName, l.TriggerDate, l.DaysBefore,
		l.MessageID, l.DeliveryStatus, l.ErrorMsg, l.TriggeredBy, l.CreatedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ExistsReminderLog 检查去重键是否存在。
func ExistsReminderLog(templateID, contractID int64, triggerDate string) (bool, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM reminder_log WHERE template_id = ? AND contract_id = ? AND trigger_date = ?`,
		templateID, contractID, triggerDate).Scan(&n)
	return n > 0, err
}

// ListReminderLogs 分页查询日志。
func ListReminderLogs(templateID, customerID, contractID int64, triggerDate, triggeredBy, status string, page, pageSize int) ([]ReminderLog, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if templateID > 0 {
		where = append(where, "template_id = ?")
		args = append(args, templateID)
	}
	if customerID > 0 {
		where = append(where, "customer_id = ?")
		args = append(args, customerID)
	}
	if contractID > 0 {
		where = append(where, "contract_id = ?")
		args = append(args, contractID)
	}
	if triggerDate != "" {
		where = append(where, "trigger_date = ?")
		args = append(args, triggerDate)
	}
	if triggeredBy != "" {
		where = append(where, "triggered_by = ?")
		args = append(args, triggeredBy)
	}
	if status != "" {
		where = append(where, "delivery_status = ?")
		args = append(args, status)
	}
	whereStr := joinAnd(where)

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM reminder_log WHERE `+whereStr, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)

	rows, err := DB.Query(`SELECT `+reminderLogCols+` FROM reminder_log WHERE `+
		whereStr+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReminderLog{}
	for rows.Next() {
		l, err := scanReminderLog(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *l)
	}
	return out, total, rows.Err()
}

func scanReminderLog(s rowScanner) (*ReminderLog, error) {
	l := &ReminderLog{}
	var subID, custID, contID, msgID sql.NullInt64
	var contractNo, contractTitle, customerName, errorMsg sql.NullString
	var createdAt int64
	err := s.Scan(
		&l.ID, &l.TemplateID, &subID, &custID, &contID,
		&contractNo, &contractTitle, &customerName, &l.TriggerDate, &l.DaysBefore,
		&msgID, &l.DeliveryStatus, &errorMsg, &l.TriggeredBy, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	if subID.Valid {
		v := subID.Int64
		l.SubscriptionID = &v
	}
	if custID.Valid {
		v := custID.Int64
		l.CustomerID = &v
	}
	if contID.Valid {
		v := contID.Int64
		l.ContractID = &v
	}
	if msgID.Valid {
		v := msgID.Int64
		l.MessageID = &v
	}
	if contractNo.Valid {
		l.ContractNo = contractNo.String
	}
	if contractTitle.Valid {
		l.ContractTitle = contractTitle.String
	}
	if customerName.Valid {
		l.CustomerName = customerName.String
	}
	if errorMsg.Valid {
		l.ErrorMsg = errorMsg.String
	}
	l.CreatedAt = createdAt
	return l, nil
}

// ==================== 第十三阶段：接收人解析辅助函数 ====================

// ValidReceiverTypes receiver_type 合法枚举值（应用层兜底：SQLite 不支持给已有表加 CHECK）。
var ValidReceiverTypes = map[string]bool{
	"admin":          true,
	"contract_owner": true,
	"customer_owner": true,
	"department":     true,
	"user":           true,
}

// IsValidReceiverType 判断 receiver_type 是否合法。
func IsValidReceiverType(t string) bool {
	return ValidReceiverTypes[t]
}

// ParseReceiverIDs 解析 receiver_id CSV 字符串为 int64 列表。
//   - "" / "0" / "  " → 返回 nil（admin / *_owner 类型占位）
//   - "5"             → 返回 [5]
//   - "1,2,3"         → 返回 [1, 2, 3]（自动去重 + 跳过 0 / 负数 / 非法）
//   - "1, 2, 3"       → 返回 [1, 2, 3]（自动 TrimSpace）
//
// 第十三阶段 v2：receiver_id 改为 CSV 字符串，支持多 ID。
func ParseReceiverIDs(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "0" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n <= 0 {
			continue // 跳过非法
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// FormatReceiverIDs 逆操作：int64 列表 → CSV 字符串。
// 主要用于"前端回显"和"测试断言"。
func FormatReceiverIDs(ids []int64) string {
	if len(ids) == 0 {
		return "0"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// ListUserIDsByDepartment 拿某部门下所有 active 用户 id。
// 用途：receiver_type = 'department' 时展开成 userID 列表。
// 返回去重后的 id 列表（不会保留顺序）。
func ListUserIDsByDepartment(departmentID int64) ([]int64, error) {
	if departmentID <= 0 {
		return nil, nil
	}
	rows, err := DB.Query(`
		SELECT id FROM users
		WHERE department_id = ? AND status = 'active'
		ORDER BY id ASC`, departmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	seen := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, rows.Err()
}

// GetFirstAdminUserID 拿第一个 admin 用户的 ID（0 = 无）。
// 用途：reminder scan 的接收人兜底（admin 用户 ID）。
func GetFirstAdminUserID() (int64, error) {
	ids, err := GetUserIDsByRole("admin")
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// GetContractCreatedByID 拿第三方合同的 created_by（0 表示未知/系统创建）。
// 用途：receiver_type = 'contract_owner' 时取第三方合同的创建人。
// 保留旧函数名以兼容上层调用；底层仅查询 third_party_contract 表。
func GetContractCreatedByID(contractID int64) (int64, error) {
	return GetThirdPartyContractCreatedByID(contractID)
}

// GetThirdPartyContractCreatedByID 拿第三方合同的 created_by。
// 用途：receiver_type = 'contract_owner' 时取第三方合同的创建人。
func GetThirdPartyContractCreatedByID(contractID int64) (int64, error) {
	var id int64
	err := DB.QueryRow(`SELECT COALESCE(created_by, 0) FROM third_party_contract WHERE id = ?`, contractID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetCustomerOwnerID 拿客户的主负责人 userID（0 = NULL → 未分配）。
// 用途：receiver_type = 'customer_owner' 时取客户主负责人。
func GetCustomerOwnerID(customerID int64) (int64, error) {
	var id sql.NullInt64
	err := DB.QueryRow(`SELECT owner_user_id FROM customer WHERE id = ?`, customerID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

// GetUserActiveByID 拿某用户是否 active（用于"无效用户 → 跳过"过滤）。
// 返回 (id, isActive, err)；id=0 表示用户不存在。
func GetUserActiveByID(userID int64) (int64, bool, error) {
	var id int64
	var status string
	err := DB.QueryRow(`SELECT id, status FROM users WHERE id = ?`, userID).Scan(&id, &status)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, status == "active", nil
}

// SeedDefaultReminderTemplates 启动时幂等写入系统预置模板。
// 已存在同 template_key 跳过；不可改 is_system=1 的模板（业务上靠 UI 隐藏删除/编辑入口）。
func SeedDefaultReminderTemplates() error {
	defaults := []ReminderTemplate{
		{
			TemplateKey: "contract_expiring_30d",
			Name:        "合同到期前 30 天",
			Description: "提醒合同将在 30 天后到期（提前 30 天通知）",
			RuleType:    "contract_expiring",
			AdvanceDays: 30,
			IsActive:    true,
			IsSystem:    true,
			SortOrder:   10,
		},
		{
			TemplateKey: "contract_expiring_7d",
			Name:        "合同到期前 7 天",
			Description: "提醒合同将在 7 天后到期（提前 1 周紧急通知）",
			RuleType:    "contract_expiring",
			AdvanceDays: 7,
			IsActive:    true,
			IsSystem:    true,
			SortOrder:   20,
		},
		{
			TemplateKey: "contract_expiring_1d",
			Name:        "合同到期前 1 天",
			Description: "提醒合同明天到期（最后窗口）",
			RuleType:    "contract_expiring",
			AdvanceDays: 1,
			IsActive:    true,
			IsSystem:    true,
			SortOrder:   30,
		},
	}
	for i := range defaults {
		t := defaults[i]
		existing, err := GetReminderTemplateByKey(t.TemplateKey)
		if err != nil {
			return fmt.Errorf("查询模板 %s 失败: %w", t.TemplateKey, err)
		}
		if existing != nil {
			continue // 幂等
		}
		if _, err := CreateReminderTemplate(&t); err != nil {
			return fmt.Errorf("写入模板 %s 失败: %w", t.TemplateKey, err)
		}
	}
	return nil
}
