package models

// ThirdPartyContract 第三方合同（第十阶段）
//
// 设计要点（v5 精简版）：
//   - 单一表（无 parties 联表，复用 customer）
//   - 必填 customer_id（对方信息通过联表获取，6 个文本字段全部删除）
//   - 必填 PDF（已签署的扫描件/电子件），3 哈希存证
//   - 5 状态机：draft / pending / signed / archived / cancelled
type ThirdPartyContract struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Type       string `json:"type"`   // paper / electronic
	Status     string `json:"status"` // 5 状态机
	ContractNo string `json:"contract_no"`

	// 对方
	CustomerID int64 `json:"customer_id"` // FK customer.id, 必填

	// 商务
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	SignDate  int64   `json:"sign_date"` // 签署日期
	StartDate int64   `json:"start_date"`
	EndDate   int64   `json:"end_date"`

	// 已签署 PDF（必填，3 哈希存证）
	FilePath         string `json:"file_path"`          // 相对路径：third_party/{YYYYMM}/{snowid}.pdf
	FileSize         int64  `json:"file_size"`          // 字节
	FileSM3Hash      string `json:"file_sm3_hash"`      // SM3
	FileSHA256Hash   string `json:"file_sha256_hash"`   // SHA-256
	FileCombinedHash string `json:"file_combined_hash"` // combined = SHA256(SM3 || SHA256)

	Remark      string `json:"remark"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	DepartmentID int64 `json:"department_id"` // 2026-06-29 RBAC v3 P4：创建者主部门快照
}

// ThirdPartyContractType 类型常量
const (
	TPContractTypePaper      = "paper"      // 纸质合同（扫描件）
	TPContractTypeElectronic = "electronic" // 电子合同
)

// ThirdPartyContractStatus 状态机常量
const (
	TPStatusDraft     = "draft"     // 草稿
	TPStatusPending   = "pending"   // 待签署
	TPStatusSigned    = "signed"    // 已签署
	TPStatusArchived  = "archived"  // 归档
	TPStatusCancelled = "cancelled" // 取消
)

// IsValidTPType 校验 type 字段
func IsValidTPType(t string) bool {
	switch t {
	case TPContractTypePaper, TPContractTypeElectronic:
		return true
	}
	return false
}

// IsValidTPStatus 校验 status 字段
func IsValidTPStatus(s string) bool {
	switch s {
	case TPStatusDraft, TPStatusPending, TPStatusSigned, TPStatusArchived, TPStatusCancelled:
		return true
	}
	return false
}

// TPStatusCanDelete 哪些状态可删除（仅 draft / cancelled）
func TPStatusCanDelete(status string) bool {
	return status == TPStatusDraft || status == TPStatusCancelled
}

// TPStatusCanTransition 状态机转换表
// 返回值：合法 → (true, ""); 非法 → (false, 错误描述)
func TPStatusCanTransition(from, to string) (bool, string) {
	if from == to {
		return false, "状态未变化"
	}
	// 终态不能再转
	if from == TPStatusArchived || from == TPStatusCancelled {
		return false, "终态不可再转"
	}
	// 定义合法转换
	legal := map[string][]string{
		TPStatusDraft:   {TPStatusPending, TPStatusCancelled},
		TPStatusPending: {TPStatusSigned, TPStatusCancelled},
		TPStatusSigned:  {TPStatusArchived},
	}
	for _, t := range legal[from] {
		if t == to {
			return true, ""
		}
	}
	return false, "非法状态转换"
}
