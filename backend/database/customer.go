package database

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// 本文件：客户（Customer / CustomerInput / SignatureRecord）相关表与操作。

// ==================== Customer ====================

// Customer 同时支持个人与企业客户。CustomerType 决定哪些字段有效：
//   - "individual"：RealName / IDCard / Gender / BirthDate
//   - "enterprise"：CompanyName / USCC / LegalPerson / ...
type Customer struct {
	ID     int64  `json:"id"`
	SnowID string `json:"snowid"`

	// 区分字段
	CustomerType string `json:"customer_type"` // individual | enterprise

	// 2026-06-28 RBAC v3：data_scope 过滤维度（与 owner/department 联合控制）。
	OwnerUserID  int64 `json:"owner_user_id"`
	DepartmentID int64 `json:"department_id"`

	// 共用字段
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Address   string `json:"address"`
	Remarks   string `json:"remarks"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// 个人专属（individual 时填，enterprise 时为空）
	RealName  string `json:"real_name"`
	IDCard    string `json:"id_card"`
	Gender    string `json:"gender"`
	BirthDate string `json:"birth_date"`

	// 企业专属（enterprise 时填，individual 时为空）
	CompanyName       string `json:"company_name"`
	USCC              string `json:"uscc"`         // 统一社会信用代码
	LegalPerson       string `json:"legal_person"` // 法人姓名
	LegalPersonIDCard string `json:"legal_person_id_card"`
	RegisteredCapital string `json:"registered_capital"`
	CompanyType       string `json:"company_type"`
	Industry          string `json:"industry"`
	EstablishedDate   string `json:"established_date"`
	BusinessScope     string `json:"business_scope"`
	Website           string `json:"website"`
}

// CustomerInput 是 Create/Update 客户 API 的入参；JSON tag 必须与前端 snake_case key 一致，
// 否则 Gin's ShouldBindJSON 会把这些字段静默置零。
type CustomerInput struct {
	CustomerType      string `json:"customer_type"`
	SnowID            string `json:"snowid"`
	Phone             string `json:"phone"`
	Email             string `json:"email"`
	Address           string `json:"address"`
	Remarks           string `json:"remarks"`
	RealName          string `json:"real_name"`
	IDCard            string `json:"id_card"`
	Gender            string `json:"gender"`
	BirthDate         string `json:"birth_date"`
	CompanyName       string `json:"company_name"`
	USCC              string `json:"uscc"`
	LegalPerson       string `json:"legal_person"`
	LegalPersonIDCard string `json:"legal_person_id_card"`
	RegisteredCapital string `json:"registered_capital"`
	CompanyType       string `json:"company_type"`
	Industry          string `json:"industry"`
	EstablishedDate   string `json:"established_date"`
	BusinessScope     string `json:"business_scope"`
	Website           string `json:"website"`
}

// customerCols 是查询客户表全部字段的列名列表。
// 2026-06-29 修复：补 owner_user_id / department_id 两个 RBAC v3 data_scope 必需列。
//   - 之前缺这两列导致所有 Scan 调用 "expected 23 destination arguments in Scan, not 25"
//   - GetCustomerByID / GetCustomersByIDs / GetAllCustomers / ListCustomersByDataScope 等都受影响
//   - 复活 TestTP_Create_DefaultAndCustomerCheck 等 7 个测试
const customerCols = `id, snowid, customer_type, phone, email, address, remarks, created_at, updated_at,
	                 real_name, id_card, gender, birth_date,
	                 company_name, uscc, legal_person, legal_person_id_card,
	                 registered_capital, company_type, industry, established_date,
	                 business_scope, website,
	                 owner_user_id, department_id`

// CreateCustomer 创建一个个人客户（保留旧 API，向后兼容）。
func CreateCustomer(snowid, realName, phone, idCard, address, email, gender, birthDate, remarks string) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO customer (snowid, real_name, phone, id_card, address, email, gender, birth_date, remarks)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, snowid, realName, phone, idCard, address, email, gender, birthDate, remarks)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreateCustomerV3 2026-06-28 RBAC v3 P3：data_scope 感知的客户创建。
// 必填 owner_user_id（负责人）和 department_id（所属部门）。
// 用于"manager 创建客户 → 自动归属本部门"场景。
func CreateCustomerV3(snowid, realName, phone, idCard, address, email, gender, birthDate, remarks string, ownerUserID, departmentID int64) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO customer (snowid, real_name, phone, id_card, address, email, gender, birth_date, remarks, owner_user_id, department_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, snowid, realName, phone, idCard, address, email, gender, birthDate, remarks, ownerUserID, departmentID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetCustomerByID 按主键 id 查询客户（排除已软删）。
func GetCustomerByID(id int64) (*Customer, error) {
	customer := &Customer{}
	err := DB.QueryRow(`SELECT `+customerCols+` FROM customer WHERE id = ? AND deleted_at = 0`, id).Scan(
		&customer.ID, &customer.SnowID, &customer.CustomerType,
		&customer.Phone, &customer.Email, &customer.Address, &customer.Remarks,
		&customer.CreatedAt, &customer.UpdatedAt,
		&customer.RealName, &customer.IDCard, &customer.Gender, &customer.BirthDate,
		&customer.CompanyName, &customer.USCC, &customer.LegalPerson, &customer.LegalPersonIDCard,
		&customer.RegisteredCapital, &customer.CompanyType, &customer.Industry, &customer.EstablishedDate,
		&customer.BusinessScope, &customer.Website,
		&customer.OwnerUserID, &customer.DepartmentID,
	)
	if err != nil {
		return nil, err
	}
	return customer, nil
}

// CustomerExists 检查客户 ID 是否存在（轻量级校验，排除已软删）。
// 用于 media 关联客户时的存在性校验：避免关联到不存在的客户 ID。
func CustomerExists(id int64) (bool, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM customer WHERE id = ? AND deleted_at = 0`, id).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetCustomersByIDs 批量按 id 查客户（v5 第十阶段列表 join 用）。
// 优化：单 SQL IN (...) 查询，避免 N+1。
// 空入参返回空 map（不查库）。
func GetCustomersByIDs(ids []int64) (map[int64]*Customer, error) {
	out := make(map[int64]*Customer, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// 去重
	seen := make(map[int64]struct{}, len(ids))
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			uniq = append(uniq, id)
		}
	}
	// 构造 IN 子句占位符
	placeholders := make([]string, len(uniq))
	args := make([]interface{}, len(uniq))
	for i, id := range uniq {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT ` + customerCols + ` FROM customer WHERE deleted_at = 0 AND id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c := &Customer{}
		if err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID,
		); err != nil {
			return nil, err
		}
		out[c.ID] = c
	}
	return out, rows.Err()
}

// UpdateCustomer 更新个人客户字段（仅对未软删客户生效；保留旧 API，向后兼容）。
func UpdateCustomer(id int64, realName, phone, idCard, address, email, gender, birthDate, remarks string) error {
	_, err := DB.Exec(`
		UPDATE customer SET real_name = ?, phone = ?, id_card = ?, address = ?, email = ?, gender = ?, birth_date = ?, remarks = ?, updated_at = ?
		WHERE id = ? AND deleted_at = 0
	`, realName, phone, idCard, address, email, gender, birthDate, remarks, time.Now().Unix(), id)
	return err
}

// GetCustomersWithPagination 分页返回全部客户（排除已软删，不过滤类型/关键词）。
func GetCustomersWithPagination(page, pageSize int) ([]Customer, int, error) {
	var total int
	err := DB.QueryRow("SELECT COUNT(*) FROM customer WHERE deleted_at = 0").Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	rows, err := DB.Query(`SELECT `+customerCols+` FROM customer WHERE deleted_at = 0 ORDER BY created_at DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID)
		if err != nil {
			return nil, 0, err
		}
		customers = append(customers, c)
	}
	if customers == nil {
		customers = []Customer{}
	}
	return customers, total, rows.Err()
}

// ListCustomersByDataScope 2026-06-28 RBAC v3 P3：data_scope 感知的客户列表。
// 在 GetCustomersWithPaginationAndSearch 基础上叠加 data_scope WHERE。
// extraWhere/extraArgs 由 BuildWhereSQL 生成（必含 deleted_at = 0 前缀）；AND 拼接到原 WHERE 末尾。
// 2026-06-28 修复 Issue #6：去掉外层冗余的 `deleted_at = 0`（extraWhere 已含），避免 SQL 中重复评估同一谓词。
func ListCustomersByDataScope(page, pageSize int, keyword, extraWhere string, extraArgs []any) ([]Customer, int, error) {
	if extraWhere == "" {
		return GetCustomersWithPaginationAndSearch(page, pageSize, keyword)
	}
	// 构造带 data_scope 的查询：extraWhere 已自带 `deleted_at = 0` 前缀，直接用括号包起来 AND 到原 WHERE
	searchPattern := "%" + keyword + "%"
	phonePrefix := keyword + "%"
	var whereClause string
	var countArgs, queryArgs []interface{}

	if keyword == "" {
		whereClause = "(" + extraWhere + ")"
	} else {
		whereClause = `(real_name LIKE ? OR phone LIKE ? OR id_card = ? OR address LIKE ?
			   OR company_name LIKE ? OR uscc LIKE ? OR legal_person LIKE ?)
			AND (` + extraWhere + `)`
	}

	// count query
	var countQuery string
	if keyword == "" {
		countQuery = "SELECT COUNT(*) FROM customer WHERE " + whereClause
	} else {
		countQuery = "SELECT COUNT(*) FROM customer WHERE " + whereClause
		countArgs = []interface{}{searchPattern, phonePrefix, keyword, searchPattern, searchPattern, searchPattern, searchPattern}
		countArgs = append(countArgs, extraArgs...)
	}

	// list query
	var query string
	if keyword == "" {
		query = `SELECT ` + customerCols + ` FROM customer WHERE ` + whereClause + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	} else {
		query = `SELECT ` + customerCols + ` FROM customer WHERE ` + whereClause + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = []interface{}{searchPattern, phonePrefix, keyword, searchPattern, searchPattern, searchPattern, searchPattern}
		queryArgs = append(queryArgs, extraArgs...)
	}
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)

	// count
	var total int
	if keyword == "" {
		// args = extraArgs
		if err := DB.QueryRow(countQuery, extraArgs...).Scan(&total); err != nil {
			return nil, 0, err
		}
	} else {
		if err := DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	// list
	rows, err := DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var customers []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID)
		if err != nil {
			return nil, 0, err
		}
		customers = append(customers, c)
	}
	if customers == nil {
		customers = []Customer{}
	}
	return customers, total, rows.Err()
}

// GetCustomersWithPaginationAndSearch 分页 + 模糊搜索客户。
// 搜索范围覆盖个人 + 企业字段。（排除已软删客户）
func GetCustomersWithPaginationAndSearch(page, pageSize int, keyword string) ([]Customer, int, error) {
	var total int
	var err error
	var countQuery string
	var countArgs []interface{}
	var query string
	var queryArgs []interface{}

	if keyword == "" {
		countQuery = "SELECT COUNT(*) FROM customer WHERE deleted_at = 0"
		query = `SELECT ` + customerCols + ` FROM customer WHERE deleted_at = 0 ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = []interface{}{pageSize, (page - 1) * pageSize}
	} else {
		searchPattern := "%" + keyword + "%"
		phonePrefix := keyword + "%"

		countQuery = `SELECT COUNT(*) FROM customer
		              WHERE deleted_at = 0
		                AND (real_name LIKE ? OR phone LIKE ? OR id_card = ? OR address LIKE ?
		                  OR company_name LIKE ? OR uscc LIKE ? OR legal_person LIKE ?)`
		countArgs = []interface{}{searchPattern, phonePrefix, keyword, searchPattern, searchPattern, searchPattern, searchPattern}
		query = `SELECT ` + customerCols + `
		         FROM customer
		         WHERE deleted_at = 0
		           AND (real_name LIKE ? OR phone LIKE ? OR id_card = ? OR address LIKE ?
		             OR company_name LIKE ? OR uscc LIKE ? OR legal_person LIKE ?)
		         ORDER BY created_at DESC
		         LIMIT ? OFFSET ?`
		queryArgs = []interface{}{searchPattern, phonePrefix, keyword, searchPattern, searchPattern, searchPattern, searchPattern, pageSize, (page - 1) * pageSize}
	}

	if keyword == "" {
		err = DB.QueryRow(countQuery).Scan(&total)
	} else {
		err = DB.QueryRow(countQuery, countArgs...).Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}

	rows, err := DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID)
		if err != nil {
			return nil, 0, err
		}
		customers = append(customers, c)
	}
	if customers == nil {
		customers = []Customer{}
	}
	return customers, total, rows.Err()
}

// CanDeleteCustomer 检查客户是否可以软删（客户下存在进行中的流转则不可软删）。
//   - 进行中的流转：flow_instance.status = 'active' 且 deleted_at = 0
//
// 2026-06-27：Bug #4 修复。
//
// Bug B 修复（2026-06-27）：之前 `if err == nil` 静默吞掉 DB 错误。
//
//	DB 出错时返回 (true, "") 等于"无在途流转"→ 允许删除实际有在途的客户。
//	现在改为返回 (false, errMsg) 触发 DeleteCustomer 拒绝删除 + 上抛 500。
func CanDeleteCustomer(id int64) (bool, string) {
	// 进行中的流转实例
	var activeFlowCount int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM flow_instance
		WHERE contract_id IN (SELECT id FROM contract WHERE customer_id = ? AND deleted_at = 0)
		  AND deleted_at = 0
		  AND status = 'active'
	`, id).Scan(&activeFlowCount)
	if err != nil {
		return false, fmt.Sprintf("检查在途流转失败（拒绝删除以策安全）: %v", err)
	}
	if activeFlowCount > 0 {
		return false, fmt.Sprintf("客户下存在 %d 个进行中的流转实例，请先处理后再删除", activeFlowCount)
	}
	return true, ""
}

// ErrDeleteCustomerBlocked 删除被业务规则阻止时返回的哨兵错误。
// handler 层用 errors.Is(err, ErrDeleteCustomerBlocked) 判断返回 409 而不是 500。
var ErrDeleteCustomerBlocked = errors.New("customer delete blocked by business rule")

// DeleteCustomer 软删除客户（customer.deleted_at = now）。
// 若客户下存在"进行中"的流转实例 → 拒绝并返回 ErrDeleteCustomerBlocked。
// 软删后该客户不在列表默认显示。
//
// 返回值：
//   - nil：删除成功
//   - ErrDeleteCustomerBlocked 包裹的错误：业务规则阻止（handler 应返回 409）
//   - 其他 error：DB 错误（handler 应返回 500）
func DeleteCustomer(id int64) error {
	canDelete, msg := CanDeleteCustomer(id)
	if !canDelete {
		return fmt.Errorf("%w: %s", ErrDeleteCustomerBlocked, msg)
	}
	_, err := DB.Exec(`UPDATE customer SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at = 0`,
		time.Now().Unix(), time.Now().Unix(), id)
	return err
}

// CreateCustomerExt 创建一个客户（个人或企业），供 /api/customer/create-ext 端点使用。
func CreateCustomerExt(in *CustomerInput) (int64, error) {
	if in == nil {
		return 0, fmt.Errorf("customer input is nil")
	}
	if in.CustomerType == "" {
		in.CustomerType = "individual"
	}
	result, err := DB.Exec(`
		INSERT INTO customer (
			snowid, customer_type, phone, email, address, remarks,
			real_name, id_card, gender, birth_date,
			company_name, uscc, legal_person, legal_person_id_card,
			registered_capital, company_type, industry, established_date,
			business_scope, website
		) VALUES (?,?,?,?,?,?, ?,?,?,?, ?,?,?,?, ?,?,?,?, ?,?)
	`,
		in.SnowID, in.CustomerType, in.Phone, in.Email, in.Address, in.Remarks,
		in.RealName, in.IDCard, in.Gender, in.BirthDate,
		in.CompanyName, in.USCC, in.LegalPerson, in.LegalPersonIDCard,
		in.RegisteredCapital, in.CompanyType, in.Industry, in.EstablishedDate,
		in.BusinessScope, in.Website,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateCustomerExt 按 id 更新一个客户（个人或企业）的全部字段（仅对未软删客户生效）。
func UpdateCustomerExt(id int64, in *CustomerInput) error {
	if in == nil {
		return fmt.Errorf("customer input is nil")
	}
	if in.CustomerType == "" {
		in.CustomerType = "individual"
	}
	_, err := DB.Exec(`
		UPDATE customer SET
			customer_type      = ?,
			phone              = ?,
			email              = ?,
			address            = ?,
			remarks            = ?,
			real_name          = ?,
			id_card            = ?,
			gender             = ?,
			birth_date         = ?,
			company_name       = ?,
			uscc               = ?,
			legal_person       = ?,
			legal_person_id_card = ?,
			registered_capital = ?,
			company_type       = ?,
			industry           = ?,
			established_date   = ?,
			business_scope     = ?,
			website            = ?,
			updated_at         = ?
		WHERE id = ? AND deleted_at = 0
	`,
		in.CustomerType, in.Phone, in.Email, in.Address, in.Remarks,
		in.RealName, in.IDCard, in.Gender, in.BirthDate,
		in.CompanyName, in.USCC, in.LegalPerson, in.LegalPersonIDCard,
		in.RegisteredCapital, in.CompanyType, in.Industry, in.EstablishedDate,
		in.BusinessScope, in.Website,
		time.Now().Unix(), id,
	)
	return err
}

// GetCustomersByType 按 customer_type 分页过滤客户（排除已软删）；ctype 为空时不过滤。
//   - type=individual  -> 个人客户管理
//   - type=enterprise  -> 企业客户管理
func GetCustomersByType(page, pageSize int, ctype string) ([]Customer, int, error) {
	var total int
	var countQuery string
	var countArgs []interface{}
	var query string
	var queryArgs []interface{}

	if ctype == "" {
		countQuery = "SELECT COUNT(*) FROM customer WHERE deleted_at = 0"
		query = `SELECT ` + customerCols + ` FROM customer WHERE deleted_at = 0 ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = []interface{}{pageSize, (page - 1) * pageSize}
	} else {
		countQuery = "SELECT COUNT(*) FROM customer WHERE customer_type = ? AND deleted_at = 0"
		countArgs = []interface{}{ctype}
		query = `SELECT ` + customerCols + ` FROM customer WHERE customer_type = ? AND deleted_at = 0 ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = []interface{}{ctype, pageSize, (page - 1) * pageSize}
	}

	if ctype == "" {
		if err := DB.QueryRow(countQuery).Scan(&total); err != nil {
			return nil, 0, err
		}
	} else {
		if err := DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	rows, err := DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID)
		if err != nil {
			return nil, 0, err
		}
		customers = append(customers, c)
	}
	if customers == nil {
		customers = []Customer{}
	}
	return customers, total, rows.Err()
}

// SearchCustomersByType 按 customer_type + keyword 分页搜索客户（排除已软删）。
// 搜索范围同时覆盖个人与企业字段。
func SearchCustomersByType(page, pageSize int, keyword, ctype string) ([]Customer, int, error) {
	var total int
	var countQuery string
	var countArgs []interface{}
	var query string
	var queryArgs []interface{}

	typeClause := ""
	typeArgs := []interface{}{}
	if ctype != "" {
		typeClause = " AND customer_type = ?"
		typeArgs = []interface{}{ctype}
	}

	if keyword == "" {
		countQuery = "SELECT COUNT(*) FROM customer WHERE deleted_at = 0" + typeClause
		countArgs = typeArgs
		query = `SELECT ` + customerCols + ` FROM customer WHERE deleted_at = 0` + typeClause + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = append(append([]interface{}{}, typeArgs...), pageSize, (page-1)*pageSize)
	} else {
		searchPattern := "%" + keyword + "%"
		phonePrefix := keyword + "%"

		searchClause := ` AND (
			real_name LIKE ? OR phone LIKE ? OR id_card = ? OR address LIKE ?
			OR company_name LIKE ? OR uscc LIKE ? OR legal_person LIKE ?
		)`
		searchArgs := []interface{}{searchPattern, phonePrefix, keyword, searchPattern, searchPattern, searchPattern, searchPattern}

		countQuery = `SELECT COUNT(*) FROM customer WHERE deleted_at = 0` + typeClause + searchClause
		countArgs = append(append([]interface{}{}, typeArgs...), searchArgs...)
		query = `SELECT ` + customerCols + ` FROM customer WHERE deleted_at = 0` + typeClause + searchClause + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
		queryArgs = append(append(append([]interface{}{}, typeArgs...), searchArgs...), pageSize, (page-1)*pageSize)
	}

	if err := DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(
			&c.ID, &c.SnowID, &c.CustomerType,
			&c.Phone, &c.Email, &c.Address, &c.Remarks,
			&c.CreatedAt, &c.UpdatedAt,
			&c.RealName, &c.IDCard, &c.Gender, &c.BirthDate,
			&c.CompanyName, &c.USCC, &c.LegalPerson, &c.LegalPersonIDCard,
			&c.RegisteredCapital, &c.CompanyType, &c.Industry, &c.EstablishedDate,
			&c.BusinessScope, &c.Website,
			&c.OwnerUserID, &c.DepartmentID)
		if err != nil {
			return nil, 0, err
		}
		customers = append(customers, c)
	}
	return customers, total, rows.Err()
}

// ==================== Signature ====================

// SignatureRecord 签名/印章记录，含三组哈希（SM3 / SHA256 / 联合哈希）用于完整性校验。
type SignatureRecord struct {
	ID           int64  `json:"id"`
	SnowID       string `json:"snowid"`
	CustomerID   int64  `json:"customer_id"`
	Type         string `json:"type"`
	FilePath     string `json:"file_path"`
	SM3Hash      string `json:"sm3_hash"`
	SHA256Hash   string `json:"sha256_hash"`
	CombinedHash string `json:"combined_hash"`
	SignedAt     string `json:"signed_at"`
}

// CreateSignatureRecord 新增一条签名/印章记录。
func CreateSignatureRecord(snowid string, customerID int64, sigType string, filePath string, sm3Hash string, sha256Hash string, combinedHash string) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO signature_record (snowid, customer_id, type, file_path, sm3_hash, sha256_hash, combined_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, snowid, customerID, sigType, filePath, sm3Hash, sha256Hash, combinedHash)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetSignatureRecordByID 按 id 查询签名记录。
func GetSignatureRecordByID(id int64) (*SignatureRecord, error) {
	record := &SignatureRecord{}
	err := DB.QueryRow(`
		SELECT id, snowid, customer_id, type, file_path, sm3_hash, sha256_hash, combined_hash, signed_at
		FROM signature_record WHERE id = ?
	`, id).Scan(&record.ID, &record.SnowID, &record.CustomerID, &record.Type, &record.FilePath, &record.SM3Hash, &record.SHA256Hash, &record.CombinedHash, &record.SignedAt)
	if err != nil {
		return nil, err
	}
	return record, nil
}


