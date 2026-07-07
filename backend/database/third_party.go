package database

import (
	"database/sql"
	"strings"
	"time"

	"doc/models"
)

// 本文件：第三方合同（third_party_contract 表）的 CRUD 操作。
// 表结构见 db.go createTables()；索引：idx_tpc_status / idx_tpc_customer / idx_tpc_end_date / idx_tpc_no_file。
//
// 设计要点（v5 精简版）：
//   - 无联表，所有信息都在本表
//   - 4 文件字段（file_path / file_size / file_hash / file_sha256 / file_combined_hash）+ customer_id 必填
//   - 5 状态机：draft / pending / signed / archived / cancelled（转换校验在 handler）

// tpcCols 是查询 third_party_contract 表全部字段的列名列表。
// 所有列加 tpc. 前缀，兼容联表 customer 时的 id 歧义（数据库层要求）。
const tpcCols = `tpc.id, tpc.contract_no, tpc.title, tpc.type, tpc.status, tpc.customer_id,
                tpc.amount, tpc.currency, tpc.sign_date, tpc.start_date, tpc.end_date,
                tpc.file_path, tpc.file_size, tpc.file_sm3_hash, tpc.file_sha256_hash, tpc.file_combined_hash,
                tpc.remark, tpc.created_by, tpc.created_at, tpc.updated_at, tpc.department_id`

// scanTPCRow 将单行扫描到 ThirdPartyContract 指针。
func scanTPCRow(scan func(...interface{}) error, t *models.ThirdPartyContract) error {
	return scan(
		&t.ID, &t.ContractNo, &t.Title, &t.Type, &t.Status, &t.CustomerID,
		&t.Amount, &t.Currency, &t.SignDate, &t.StartDate, &t.EndDate,
		&t.FilePath, &t.FileSize, &t.FileSM3Hash, &t.FileSHA256Hash, &t.FileCombinedHash,
		&t.Remark, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.DepartmentID,
	)
}

// GetThirdPartyContractByID 按主键查询。
func GetThirdPartyContractByID(id int64) (*models.ThirdPartyContract, error) {
	t := &models.ThirdPartyContract{}
	err := DB.QueryRow(`SELECT `+tpcCols+` FROM third_party_contract tpc WHERE tpc.id = ?`, id).
		Scan(scanTPCRowAssign(t)...)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// GetThirdPartyContractsByIDs 2026-06-29 RBAC v3 P4 优化：批量按 id 查第三方合同。
// 替代 BulkDownload 中对每个 id 单独 GetByID 的 N+1 模式（review issue #1）。
// 返回顺序不保证与入参一致；调用方用 map 索引。
// 入参去重 + 过滤非正整数；空入参返回 (nil, nil)。
func GetThirdPartyContractsByIDs(ids []int64) ([]*models.ThirdPartyContract, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// 去重
	seen := make(map[int64]struct{}, len(ids))
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	// 单 SQL IN 查询
	placeholders := make([]string, len(uniq))
	args := make([]interface{}, len(uniq))
	for i, id := range uniq {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT ` + tpcCols + ` FROM third_party_contract tpc WHERE tpc.id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*models.ThirdPartyContract, 0, len(uniq))
	for rows.Next() {
		t := &models.ThirdPartyContract{}
		if err := scanTPCRow(rows.Scan, t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateThirdPartyContractBasic 更新基本字段（不含文件 + 状态）。
// 不允许修改：id / contract_no / created_by / created_at。
func UpdateThirdPartyContractBasic(
	id int64,
	title, tpType string,
	customerID int64,
	amount float64, currency string,
	signDate, startDate, endDate int64,
	remark string,
) error {
	_, err := DB.Exec(`
		UPDATE third_party_contract
		SET title = ?, type = ?, customer_id = ?,
		    amount = ?, currency = ?,
		    sign_date = ?, start_date = ?, end_date = ?,
		    remark = ?, updated_at = ?
		WHERE id = ?
	`, title, tpType, customerID,
		amount, currency,
		signDate, startDate, endDate,
		remark, time.Now().Unix(), id)
	return err
}

// UpdateThirdPartyContractStatus 状态机更新（带状态合法校验，handler 调）。
func UpdateThirdPartyContractStatus(id int64, status string) error {
	_, err := DB.Exec(`
		UPDATE third_party_contract
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, time.Now().Unix(), id)
	return err
}

// UpdateThirdPartyContractFile 覆盖更新文件字段（用于"重新上传"）。
func UpdateThirdPartyContractFile(
	id int64,
	filePath string, fileSize int64,
	fileSM3Hash, fileSHA256Hash, fileCombinedHash string,
) error {
	_, err := DB.Exec(`
		UPDATE third_party_contract
		SET file_path = ?, file_size = ?,
		    file_sm3_hash = ?, file_sha256_hash = ?, file_combined_hash = ?,
		    updated_at = ?
		WHERE id = ?
	`, filePath, fileSize, fileSM3Hash, fileSHA256Hash, fileCombinedHash,
		time.Now().Unix(), id)
	return err
}

// DeleteThirdPartyContract 物理删除（外层有 status 校验，DB 层直接删）。
func DeleteThirdPartyContract(id int64) error {
	_, err := DB.Exec(`DELETE FROM third_party_contract WHERE id = ?`, id)
	return err
}

// ListThirdPartyContracts 列表 + 过滤 + 分页。
// customerID > 0 时加 customer_id 条件；customerType != "" 时联表 customer 加 customer_type 条件。
// status != "" 时加 status 条件。
// search 关键字同时匹配 contract_no 和 title（LIKE），并联表匹配 customer.real_name/company_name/phone。
// 返回 ([]*models.ThirdPartyContract, total, error)。
func ListThirdPartyContracts(
	customerID int64,
	customerType string,
	status string,
	search string,
	page, pageSize int,
) ([]*models.ThirdPartyContract, int, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	whereParts := []string{"1=1"}
	args := []interface{}{}
	if customerID > 0 {
		whereParts = append(whereParts, "tpc.customer_id = ?")
		args = append(args, customerID)
	}
	if customerType == "individual" || customerType == "enterprise" {
		whereParts = append(whereParts, "cu.customer_type = ?")
		args = append(args, customerType)
	}
	if status != "" {
		whereParts = append(whereParts, "tpc.status = ?")
		args = append(args, status)
	}
	if search != "" {
		// 同时匹配：合同号 / 标题 / 客户名 / 企业名 / 电话
		whereParts = append(whereParts, "(tpc.contract_no LIKE ? OR tpc.title LIKE ? OR cu.real_name LIKE ? OR cu.company_name LIKE ? OR cu.phone LIKE ?)")
		kw := "%" + search + "%"
		args = append(args, kw, kw, kw, kw, kw)
	}
	where := strings.Join(whereParts, " AND ")

	// COUNT
	var total int
	countSQL := `SELECT COUNT(*) FROM third_party_contract tpc
		LEFT JOIN customer cu ON tpc.customer_id = cu.id
		WHERE ` + where
	if err := DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// LIST
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := DB.Query(`SELECT `+tpcCols+` FROM third_party_contract tpc
		LEFT JOIN customer cu ON tpc.customer_id = cu.id
		WHERE `+where+
		` ORDER BY tpc.created_at DESC, tpc.id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.ThirdPartyContract
	for rows.Next() {
		t := &models.ThirdPartyContract{}
		if err := scanTPCRow(rows.Scan, t); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []*models.ThirdPartyContract{}
	}
	return out, total, rows.Err()
}

// ListThirdPartyContractFiles 批量下载：根据 ids 列表返回每条记录的文件字段（相对路径）。
// 返回 (id, relPath, contractNo, err) 列表，跳过 file_path 为空的。
// 注意：返回的是相对路径，由 handler 通过 services.GetThirdPartyStorage() 解析为绝对路径。
// （database 包不能 import services，避免循环依赖）
func ListThirdPartyContractFiles(ids []int64) ([]struct {
	ID         int64
	RelPath    string
	ContractNo string
}, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT id, file_path, contract_no FROM third_party_contract WHERE id IN (` +
		strings.Join(placeholders, ",") + `) AND file_path != ''`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		ID         int64
		RelPath    string
		ContractNo string
	}
	for rows.Next() {
		var id int64
		var relPath, contractNo string
		if err := rows.Scan(&id, &relPath, &contractNo); err != nil {
			return nil, err
		}
		out = append(out, struct {
			ID         int64
			RelPath    string
			ContractNo string
		}{id, relPath, contractNo})
	}
	return out, rows.Err()
}

// ListActiveThirdPartyContractsByCustomer 返回某客户的 active 第三方合同。
// active 定义：status NOT IN ('cancelled', 'archived')，且 end_date > 0。
// 用途：第十一阶段 提醒扫描时按客户级订阅展开"目标合同集合"。
func ListActiveThirdPartyContractsByCustomer(customerID int64) ([]*models.ThirdPartyContract, error) {
	rows, err := DB.Query(`
		SELECT `+tpcCols+` FROM third_party_contract tpc
		WHERE tpc.customer_id = ?
		  AND tpc.status NOT IN ('cancelled', 'archived')
		  AND tpc.end_date > 0
		ORDER BY tpc.end_date ASC, tpc.id ASC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*models.ThirdPartyContract{}
	for rows.Next() {
		t := &models.ThirdPartyContract{}
		if err := scanTPCRow(rows.Scan, t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetCustomerNameByID 拿单个客户的"显示名"（企业优先公司名，个人优先真实名）。
// 用途：第十一阶段 提醒扫描时构造站内信内容。
func GetCustomerNameByID(id int64) string {
	if id <= 0 {
		return ""
	}
	var company, realName string
	err := DB.QueryRow(`SELECT COALESCE(company_name, ''), COALESCE(real_name, '') FROM customer WHERE id = ?`, id).
		Scan(&company, &realName)
	if err != nil {
		return ""
	}
	if company != "" {
		return company
	}
	return realName
}

// scanTPCRowAssign 包装 scanTPCRow 为 []interface{}，供 QueryRow().Scan 使用。
func scanTPCRowAssign(t *models.ThirdPartyContract) []interface{} {
	return []interface{}{
		&t.ID, &t.ContractNo, &t.Title, &t.Type, &t.Status, &t.CustomerID,
		&t.Amount, &t.Currency, &t.SignDate, &t.StartDate, &t.EndDate,
		&t.FilePath, &t.FileSize, &t.FileSM3Hash, &t.FileSHA256Hash, &t.FileCombinedHash,
		&t.Remark, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.DepartmentID,
	}
}

// CreateThirdPartyContractV2 2026-06-29 RBAC v3 P4：插入第三方合同 + 快照 departmentID。
// 创建时快照当前用户主部门到 department_id 列，供 data_scope 过滤使用。
// 2026-06-29 P2 重构：删除原 CreateThirdPartyContract（无 department_id），
// 所有调用方已迁到本函数（含 reminder_test.go）。生产代码唯一入口。
func CreateThirdPartyContractV2(
	contractNo, title, tpType, status string,
	customerID int64,
	amount float64, currency string,
	signDate, startDate, endDate int64,
	filePath string, fileSize int64,
	fileSM3Hash, fileSHA256Hash, fileCombinedHash string,
	remark string,
	createdBy int64,
	departmentID int64,
) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO third_party_contract
		(contract_no, title, type, status, customer_id,
		 amount, currency, sign_date, start_date, end_date,
		 file_path, file_size, file_sm3_hash, file_sha256_hash, file_combined_hash,
		 remark, created_by, created_at, updated_at, department_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		contractNo, title, tpType, status, customerID,
		amount, currency, signDate, startDate, endDate,
		filePath, fileSize, fileSM3Hash, fileSHA256Hash, fileCombinedHash,
		remark, createdBy, time.Now().Unix(), time.Now().Unix(), departmentID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// ListThirdPartyContractsByDataScope 2026-06-29 RBAC v3 P4：data_scope 感知的第三方合同列表。
// 在 ListThirdPartyContracts 基础上叠加 data_scope WHERE。
// extraWhere/extraArgs 由 BuildWhereSQL 生成；AND 拼接到原 WHERE 末尾。
// third_party_contract 表无 deleted_at 列（status 列承担软状态），用 adaptDataScopeWhere 适配。
func ListThirdPartyContractsByDataScope(
	customerID int64,
	customerType string,
	status string,
	search string,
	page, pageSize int,
	extraWhere string,
	extraArgs []interface{},
) ([]*models.ThirdPartyContract, int, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	whereParts := []string{"1=1"}
	args := []interface{}{}
	if customerID > 0 {
		whereParts = append(whereParts, "tpc.customer_id = ?")
		args = append(args, customerID)
	}
	if customerType == "individual" || customerType == "enterprise" {
		whereParts = append(whereParts, "cu.customer_type = ?")
		args = append(args, customerType)
	}
	if status != "" {
		whereParts = append(whereParts, "tpc.status = ?")
		args = append(args, status)
	}
	if search != "" {
		whereParts = append(whereParts, "(tpc.contract_no LIKE ? OR tpc.title LIKE ? OR cu.real_name LIKE ? OR cu.company_name LIKE ? OR cu.phone LIKE ?)")
		kw := "%" + search + "%"
		args = append(args, kw, kw, kw, kw, kw)
	}
	// 拼接 data_scope extraWhere（third_party_contract 无 deleted_at 列）
	cleanExtra, ok := adaptDataScopeWhere(extraWhere, false)
	if ok {
		whereParts = append(whereParts, "("+cleanExtra+")")
		args = append(args, extraArgs...)
	}
	where := strings.Join(whereParts, " AND ")

	// COUNT
	var total int
	countSQL := `SELECT COUNT(*) FROM third_party_contract tpc
		LEFT JOIN customer cu ON tpc.customer_id = cu.id
		WHERE ` + where
	if err := DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// LIST
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := DB.Query(`SELECT `+tpcCols+` FROM third_party_contract tpc
		LEFT JOIN customer cu ON tpc.customer_id = cu.id
		WHERE `+where+
		` ORDER BY tpc.created_at DESC, tpc.id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.ThirdPartyContract
	for rows.Next() {
		t := &models.ThirdPartyContract{}
		if err := scanTPCRow(rows.Scan, t); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []*models.ThirdPartyContract{}
	}
	return out, total, rows.Err()
}
