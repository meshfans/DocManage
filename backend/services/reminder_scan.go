package services

import (
	"context"
	"doc/database"
	"doc/utils"
	"encoding/json"
	"fmt"
	"time"
)

// 本文件：第十一阶段 提醒扫描逻辑（reminder.scan handler 实现）。
//
// 扫描范围：
//   - third_party_contract（文档）：需要 end_date > 0
//
// 扫描流程：
//   1. 加载所有 active 模板（ListActiveReminderTemplates）
//   2. 加载所有 active 订阅（ListAllActiveSubscriptions）
//   3. 对每条订阅，解析"目标合同集合"
//      - 合同级订阅（link_id 非空）→ 单个文档
//      - 客户级订阅（customer_id 非空）→ 该客户所有 active 文档
//   4. 对每个目标合同，按模板的 advance_days 检查到期
//   5. 去重（reminder_log UNIQUE 约束）
//   6. 命中 → 写站内信 + reminder_log
//
// v1 接收人策略：第一个 admin。

// ==================== 统一合同结构 ====================

// ReminderContract 提醒扫描的统一合同结构（文档）。
type ReminderContract struct {
	ID         int64  // 合同 ID
	CustomerID int64  // 客户 ID
	Status     string // 状态（用于终态过滤）
	Title      string // 合同标题
	ContractNo string // 合同编号
	EndDate    int64  // 到期日（unix timestamp）
}

// ==================== Handler ====================

// MakeReminderScanJobHandler 构造 reminder.scan handler。
func MakeReminderScanJobHandler() JobFunc {
	return runReminderScan
}

// runReminderScan 实际执行扫描。
func runReminderScan(ctx context.Context, _ json.RawMessage) (string, error) {
	stats, err := ScanReminders("scheduler", 0)
	if err != nil {
		return "", fmt.Errorf("扫描失败: %w", err)
	}
	output := fmt.Sprintf("reminder_scan: templates=%d subs=%d scanned=%d matched=%d sent=%d skipped=%d failed=%d",
		stats.Templates, stats.Subscriptions, stats.ContractsScanned,
		stats.Matched, stats.Sent, stats.Skipped, stats.Failed)
	utils.Info("[reminder.scan] %s", output)
	return output, nil
}

// ReminderScanStats 扫描统计。
type ReminderScanStats struct {
	Templates        int `json:"templates"`
	Subscriptions    int `json:"subscriptions"`
	ContractsScanned int `json:"contracts_scanned"`
	Matched          int `json:"matched"`
	Sent             int `json:"sent"`
	Skipped          int `json:"skipped"`
	Failed           int `json:"failed"`
}

// expandTargetContracts 根据订阅的 link_type + link_id（CSV）展开"目标合同集合"。
//
// 第十三阶段 v4：替代 v1-v3 的"if ContractID/else CustomerID"逻辑。
//   - link_type='customer'              → 每个客户 ID 展开为该客户所有 active 文档
//   - link_type='third_party_contract' → 每个合同 ID 加载文档
//
// 入参：sub.LinkID 是 CSV（如 "5" 或 "1,2,3"）
// 返回：去重后的 *ReminderContract 列表（顺序不保证）
func expandTargetContracts(sub *database.ReminderSubscription) []*ReminderContract {
	ids := database.ParseReceiverIDs(sub.LinkID)
	if len(ids) == 0 {
		return nil
	}
	var result []*ReminderContract
	switch sub.LinkType {
	case "customer":
		// 客户级：每个 ID 展开为该客户所有 active 文档
		for _, cid := range ids {
			contracts := loadActiveContractsByCustomer(cid)
			result = append(result, contracts...)
		}
	case "third_party_contract":
		// 文档级：每个 ID 加载文档
		for _, cid := range ids {
			if c := loadThirdPartyContractByID(cid); c != nil {
				result = append(result, c)
			}
		}
	default:
		// 未知 link_type → 不展开（跳过）
		utils.Warn("[reminder] 未知 link_type=%q，订阅 #%d 跳过", sub.LinkType, sub.ID)
	}
	return result
}

// loadThirdPartyContractByID 加载文档（third_party_contract 表）。
func loadThirdPartyContractByID(id int64) *ReminderContract {
	tpc, err := database.GetThirdPartyContractByID(id)
	if err != nil || tpc == nil || tpc.EndDate == 0 {
		return nil
	}
	return &ReminderContract{
		ID:         tpc.ID,
		CustomerID: tpc.CustomerID,
		Status:     tpc.Status,
		Title:      tpc.Title,
		ContractNo: tpc.ContractNo,
		EndDate:    tpc.EndDate,
	}
}

// resolveReceivers 解析订阅的接收人列表。
//
// 第十三阶段：按 subscription.ReceiverType 分发：
//   - "admin"          → 第一个 admin（v1 行为）
//   - "contract_owner" → contract.created_by（文档创建人）
//   - "customer_owner" → customer.owner_user_id
//   - "department"     → 每个部门 ID 展开为该部门下所有 active 用户
//   - "user"           → 每个用户 ID 校验存在 + active
//
// 第十三阶段 v2：receiver_id 是 CSV 字符串，支持多 ID（如 "1,2,3"）。
//
// 入参：
//   - sub:   订阅（带 ReceiverType / ReceiverID）
//   - c:     当前命中的合同
//
// 返回：去重后的 userID 列表（顺序不保证）。空列表 = 解析失败，调用方应 fallback 到 admin。
func resolveReceivers(sub database.ReminderSubscription, c *ReminderContract) ([]int64, error) {
	receiverType := sub.ReceiverType
	if receiverType == "" {
		receiverType = "admin" // 兜底（兼容旧数据）
	}

	switch receiverType {
	case "admin":
		adminID, err := database.GetFirstAdminUserID()
		if err != nil {
			return nil, fmt.Errorf("取 admin 失败: %w", err)
		}
		if adminID == 0 {
			return nil, nil
		}
		return []int64{adminID}, nil

	case "contract_owner":
		ownerID, err := database.GetContractCreatedByID(c.ID)
		if err != nil {
			return nil, fmt.Errorf("取合同 created_by 失败: %w", err)
		}
		if ownerID == 0 {
			return nil, nil
		}
		return []int64{ownerID}, nil

	case "customer_owner":
		if c.CustomerID <= 0 {
			return nil, nil
		}
		ownerID, err := database.GetCustomerOwnerID(c.CustomerID)
		if err != nil {
			return nil, fmt.Errorf("取客户 owner 失败: %w", err)
		}
		if ownerID == 0 {
			return nil, nil
		}
		return []int64{ownerID}, nil

	case "department":
		deptIDs := database.ParseReceiverIDs(sub.ReceiverID)
		if len(deptIDs) == 0 {
			return nil, nil
		}
		var allUsers []int64
		for _, deptID := range deptIDs {
			users, err := database.ListUserIDsByDepartment(deptID)
			if err != nil {
				return nil, fmt.Errorf("取部门 %d 用户失败: %w", deptID, err)
			}
			allUsers = append(allUsers, users...)
		}
		return allUsers, nil

	case "user":
		userIDs := database.ParseReceiverIDs(sub.ReceiverID)
		if len(userIDs) == 0 {
			return nil, nil
		}
		var out []int64
		for _, uid := range userIDs {
			id, isActive, err := database.GetUserActiveByID(uid)
			if err != nil {
				return nil, fmt.Errorf("取用户 %d 失败: %w", uid, err)
			}
			if id == 0 || !isActive {
				utils.Warn("[reminder] 用户 %d 不存在或已停用，跳过", uid)
				continue
			}
			out = append(out, id)
		}
		return out, nil

	default:
		// 未知类型 → 兜底 admin
		utils.Warn("[reminder] 未知 receiver_type=%q，兜底为 admin", receiverType)
		adminID, err := database.GetFirstAdminUserID()
		if err != nil {
			return nil, fmt.Errorf("取 admin 失败: %w", err)
		}
		if adminID == 0 {
			return nil, nil
		}
		return []int64{adminID}, nil
	}
}

// ScanReminders 是 reminder.scan 的核心实现，对外暴露给 handler（手动触发）。
func ScanReminders(triggeredBy string, operatorID int64) (*ReminderScanStats, error) {
	if triggeredBy == "" {
		triggeredBy = "manual"
	}
	stats := &ReminderScanStats{}

	// 1. 加载所有 active 模板
	templates, err := database.ListActiveReminderTemplates()
	if err != nil {
		return nil, fmt.Errorf("加载模板失败: %w", err)
	}
	stats.Templates = len(templates)
	if len(templates) == 0 {
		return stats, nil
	}

	// 2. 加载所有 active 订阅
	subs, err := database.ListAllActiveSubscriptions()
	if err != nil {
		return nil, fmt.Errorf("加载订阅失败: %w", err)
	}
	stats.Subscriptions = len(subs)
	if len(subs) == 0 {
		return stats, nil
	}

	// 预索引：template_id → Template
	tplByID := make(map[int64]database.ReminderTemplate, len(templates))
	for i := range templates {
		tplByID[templates[i].ID] = templates[i]
	}

	today := time.Now()
	todayStr := today.Format("2006-01-02")

	// 3-4. 遍历订阅展开"目标合同" + 匹配
	// 第十三阶段 v4：按 sub.LinkType 分发，sub.LinkID 是 CSV（多 ID）
	for _, sub := range subs {
		tpl, ok := tplByID[sub.TemplateID]
		if !ok {
			continue
		}

		contracts := expandTargetContracts(&sub)

		for _, c := range contracts {
			stats.ContractsScanned++

			matched, daysBefore := matchContract(c, tpl, today)
			if !matched {
				continue
			}

			stats.Matched++

			// 5. 去重
			exists, err := database.ExistsReminderLog(tpl.ID, c.ID, todayStr)
			if err != nil {
				utils.Warn("[reminder] ExistsReminderLog 失败: %v", err)
				stats.Failed++
				continue
			}
			if exists {
				stats.Skipped++
				continue
			}

			// 6. 命中 → 写站内信 + reminder_log
			if err := fireReminder(sub, tpl, c, daysBefore, todayStr, triggeredBy, operatorID); err != nil {
				utils.Warn("[reminder] fireReminder 失败: %v", err)
				stats.Failed++
				continue
			}
			stats.Sent++
		}
	}

	return stats, nil
}

// loadActiveContractsByCustomer 加载某客户所有 active 文档。
func loadActiveContractsByCustomer(customerID int64) []*ReminderContract {
	var result []*ReminderContract

	// 加载文档
	if tpcs, err := database.ListActiveThirdPartyContractsByCustomer(customerID); err == nil {
		for _, tpc := range tpcs {
			if tpc.Status == "cancelled" || tpc.Status == "archived" {
				continue
			}
			result = append(result, &ReminderContract{
				ID:         tpc.ID,
				CustomerID: tpc.CustomerID,
				Status:     tpc.Status,
				Title:      tpc.Title,
				ContractNo: tpc.ContractNo,
				EndDate:    tpc.EndDate,
			})
		}
	}

	return result
}

// matchContract 检查合同是否命中模板规则。
func matchContract(c *ReminderContract, tpl database.ReminderTemplate, today time.Time) (bool, int) {
	// 状态过滤：终态不提醒
	if c.Status == "cancelled" || c.Status == "archived" {
		return false, 0
	}

	// 仅支持 contract_expiring
	if tpl.RuleType != "contract_expiring" {
		return false, 0
	}

	// end_date=0 视为无到期日
	if c.EndDate == 0 {
		return false, 0
	}

	// 全部转 UTC：endDate 是 unix timestamp（无时区），按 UTC 解析
	endDate := time.Unix(c.EndDate, 0).UTC()
	todayUTC := today.UTC()
	todayStart := time.Date(todayUTC.Year(), todayUTC.Month(), todayUTC.Day(), 0, 0, 0, 0, time.UTC)

	// 已过期：v1 不提醒
	if endDate.Before(todayStart) {
		return false, 0
	}

	// 命中条件：今天（UTC）在 [endDate - advanceDays, endDate) 区间内
	targetDate := endDate.AddDate(0, 0, -tpl.AdvanceDays)
	if todayStart.Before(targetDate) {
		return false, 0
	}

	daysBefore := int(endDate.Sub(todayStart).Hours() / 24)
	return true, daysBefore
}

// fireReminder 发送一条提醒：解析接收人 → 写站内信（多接收人）→ 写 reminder_log。
//
// 第十三阶段：接收人由 subscription.ReceiverType 决定，可解析出 0..N 个 userID。
//  - 0 个 userID（解析失败）→ 兜底为 admin
//  - 1 个 userID            → 单条 message
//  - N 个 userID            → 每人一条 message（共享同一份 reminder_log 记录）
func fireReminder(
	sub database.ReminderSubscription,
	tpl database.ReminderTemplate,
	c *ReminderContract,
	daysBefore int,
	todayStr string,
	triggeredBy string,
	operatorID int64,
) error {
	// 1. 解析接收人
	receivers, err := resolveReceivers(sub, c)
	if err != nil {
		return fmt.Errorf("解析接收人失败: %w", err)
	}
	if len(receivers) == 0 {
		// 兜底：admin
		utils.Warn("[reminder] 订阅 #%d receiver_type=%q 解析为空，兜底为 admin", sub.ID, sub.ReceiverType)
		adminID, adminErr := database.GetFirstAdminUserID()
		if adminErr != nil || adminID == 0 {
			return fmt.Errorf("系统无 admin 用户，请先创建 admin")
		}
		receivers = []int64{adminID}
	}

	// 去重
	receivers = dedupInt64(receivers)

	// 2. 客户名 + 消息内容（所有合同均为文档）
	customerName := database.GetCustomerNameByID(c.CustomerID)
	contractType := "文档"

	title := fmt.Sprintf("【合同到期提醒】%s", c.Title)
	var content string
	switch {
	case daysBefore == 0:
		content = fmt.Sprintf("%s「%s」（编号 %s）将于今天到期，请及时处理。", contractType, c.Title, c.ContractNo)
	case daysBefore == 1:
		content = fmt.Sprintf("%s「%s」（编号 %s）将于明天到期，请及时处理。", contractType, c.Title, c.ContractNo)
	default:
		content = fmt.Sprintf("%s「%s」（编号 %s）将于 %d 天后到期，请及时处理。", contractType, c.Title, c.ContractNo, daysBefore)
	}

	// 3. 给每个接收人写一条 message（取第一个 msgID 作为 reminder_log 的代表）
	var firstMsgID int64
	successCount := 0
	for _, uid := range receivers {
		msgID, err := database.CreateMessage(
			uid,
			operatorID,
			title,
			content,
			"reminder",
		)
		if err != nil {
			utils.Warn("[reminder] 写站内信失败 user=%d: %v", uid, err)
			continue
		}
		if firstMsgID == 0 {
			firstMsgID = msgID
		}
		successCount++
	}
	if successCount == 0 {
		return fmt.Errorf("所有接收人 (%d 个) 写站内信均失败", len(receivers))
	}

	// 4. 写 reminder_log（仅写 1 条，用 firstMsgID 代表）
	customerID := c.CustomerID
	contractID := c.ID
	subID := sub.ID
	log := &database.ReminderLog{
		TemplateID:     tpl.ID,
		SubscriptionID: &subID,
		CustomerID:     &customerID,
		ContractID:     &contractID,
		ContractNo:     c.ContractNo,
		ContractTitle:  c.Title,
		CustomerName:   customerName,
		TriggerDate:    todayStr,
		DaysBefore:     daysBefore,
		MessageID:      &firstMsgID,
		DeliveryStatus: "sent",
		TriggeredBy:    triggeredBy,
	}
	if _, err := database.CreateReminderLog(log); err != nil {
		utils.Warn("[reminder] CreateReminderLog 失败（可能 UNIQUE 冲突）: %v", err)
	}

	utils.Info("[reminder] 发送成功: tpl=%s contract=%d(%s) customer=%s daysBefore=%d receivers=%d msgID=%d",
		tpl.TemplateKey, c.ID, contractType, customerName, daysBefore, successCount, firstMsgID)
	return nil
}

// dedupInt64 对 int64 切片去重（保序）。
func dedupInt64(in []int64) []int64 {
	if len(in) == 0 {
		return in
	}
	seen := make(map[int64]bool, len(in))
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
