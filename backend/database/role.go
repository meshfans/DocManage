package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Role 角色
type Role struct {
	ID          int64    `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	IsSystem    bool     `json:"is_system"`
	Status      string   `json:"status"`
	DataScope   string   `json:"data_scope"` // 2026-06-28 RBAC v3：数据级权限（all/dept_and_sub/dept/self_and_sub_dept/self/custom）
	// 2026-06-28 RBAC v3.1：custom 模式部门白名单（仅 data_scope='custom' 时生效）
	// 存储为 JSON 数组 [1,5,10,23]，与 role.permissions 同风格。
	// LoadUserDataScope 加载时会过滤已删除部门（应用层 FK 校验）。
	CustomDeptIDs []int64 `json:"custom_dept_ids"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

// GetRoleByCode 按 code 查角色（sql.ErrNoRows 时返回 nil, nil）
func GetRoleByCode(code string) (*Role, error) {
	row := DB.QueryRow(
		`SELECT id, code, name, description, permissions, is_system, status, COALESCE(data_scope, 'self') as data_scope, COALESCE(custom_dept_ids, '[]') as custom_dept_ids, created_at, updated_at
		 FROM role WHERE code = ?`, code)
	return scanRole(row)
}

// GetAllRoles 列出全部角色
func GetAllRoles() ([]Role, error) {
	rows, err := DB.Query(
		`SELECT id, code, name, description, permissions, is_system, status, COALESCE(data_scope, 'self') as data_scope, COALESCE(custom_dept_ids, '[]') as custom_dept_ids, created_at, updated_at
		 FROM role ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRolesByCodes 批量查（IN 子句），避免 N+1
func GetRolesByCodes(codes []string) ([]Role, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	// 构造 IN (?,?,...)
	args := make([]interface{}, len(codes))
	placeholders := ""
	for i, c := range codes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = c
	}
	rows, err := DB.Query(
		`SELECT id, code, name, description, permissions, is_system, status, COALESCE(data_scope, 'self') as data_scope, COALESCE(custom_dept_ids, '[]') as custom_dept_ids, created_at, updated_at
		 FROM role WHERE code IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// UpsertRole 新建/更新（按 code 幂等）
// 写入前校验 permissions 中每个 code 都在 permission 表里（不限 active）
// 这样允许把 disabled 权限保留在 role 配置里，恢复启用后自动生效
// 通配符 *:*:* 跳过校验
func UpsertRole(r *Role) error {
	if r.Code == "" {
		return errors.New("角色 code 不能为空")
	}

	// 校验 permissions：用全部 code（含 disabled）而不是 active，
	// 这样禁用期间仍可配置角色，恢复后无需重配
	validCodes, err := ListAllPermissionCodes()
	if err != nil {
		return fmt.Errorf("查询 permission 失败: %w", err)
	}
	validSet := make(map[string]struct{}, len(validCodes))
	for _, c := range validCodes {
		validSet[c] = struct{}{}
	}
	for _, p := range r.Permissions {
		if p == "*:*:*" {
			continue
		}
		if _, ok := validSet[p]; !ok {
			return fmt.Errorf("未知权限码: %s（不在 permission 表中）", p)
		}
	}

	permsJSON, err := json.Marshal(r.Permissions)
	if err != nil {
		return err
	}
	status := r.Status
	if status == "" {
		status = "active"
	}
	// 2026-06-28 RBAC v3 P1：data_scope 字段写入。
	// 合法值：all/dept_and_sub/dept/self_and_sub_dept/self/custom，空值时回退 'self'（最严格，向后兼容）。
	// 系统预置角色（is_system=1）允许通过此 API 调整 data_scope（业务需求：把 manager 改成 dept_and_sub）。
	dataScope := r.DataScope
	if dataScope == "" {
		dataScope = "self"
	}
	if !isValidDataScope(dataScope) {
		return fmt.Errorf("非法 data_scope: %s（合法值：all/dept_and_sub/dept/self_and_sub_dept/self/custom）", dataScope)
	}
	// 2026-06-28 RBAC v3.1：custom 模式部门白名单写入。
	// 仅 data_scope='custom' 时使用；其他模式忽略（保留 []）。
	customDeptsJSON, err := json.Marshal(r.CustomDeptIDs)
	if err != nil {
		return fmt.Errorf("序列化 custom_dept_ids 失败: %w", err)
	}
	if r.CustomDeptIDs == nil {
		customDeptsJSON = []byte("[]")
	}
	now := time.Now().Unix()

	_, err = DB.Exec(`
		INSERT INTO role (code, name, description, permissions, is_system, status, data_scope, custom_dept_ids, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(code) DO UPDATE SET
			name           = excluded.name,
			description    = excluded.description,
			permissions    = excluded.permissions,
			status         = excluded.status,
			data_scope     = excluded.data_scope,
			custom_dept_ids = excluded.custom_dept_ids,
			updated_at     = excluded.updated_at`,
		r.Code, r.Name, r.Description, string(permsJSON), boolToInt(r.IsSystem), status, dataScope, string(customDeptsJSON), now, now)
	return err
}

// isValidDataScope 校验 data_scope 取值合法性。
// 2026-06-28 RBAC v3 P1：白名单校验，防止 admin 误填非法值导致全表数据泄漏。
func isValidDataScope(s string) bool {
	switch s {
	case "all", "dept_and_sub", "dept", "self_and_sub_dept", "self", "custom":
		return true
	}
	return false
}

// DeleteRole 删除（is_system=1 拒绝）
// 2026-06-25 P0-6.2 修复：删除前先检查 users.roles JSON 中是否还有用户引用该 role。
// 引用 > 0 时拒绝删除（避免遗留失效 code 导致前端 hasPerms 失效）。
func DeleteRole(code string) (int64, error) {
	r, err := GetRoleByCode(code)
	if err != nil {
		return 0, err
	}
	if r == nil {
		return 0, fmt.Errorf("角色不存在: %s", code)
	}
	if r.IsSystem {
		return 0, fmt.Errorf("系统预置角色不可删除: %s", code)
	}
	// 引用检查：users.roles JSON 是否还引用此 code
	count, err := CountUsersWithRole(code)
	if err != nil {
		return 0, fmt.Errorf("查询角色引用失败: %w", err)
	}
	if count > 0 {
		return 0, fmt.Errorf("角色 %s 仍有 %d 个用户引用，请先解除绑定再删除", code, count)
	}
	res, err := DB.Exec(`DELETE FROM role WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// scanRole 扫描单行（适配 QueryRow 和 Query）
func scanRole(s scanner) (*Role, error) {
	var r Role
	var permsJSON, customDeptsJSON string
	var isSystem int
	err := s.Scan(
		&r.ID, &r.Code, &r.Name, &r.Description,
		&permsJSON, &isSystem, &r.Status, &r.DataScope, &customDeptsJSON,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(permsJSON), &r.Permissions)
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	_ = json.Unmarshal([]byte(customDeptsJSON), &r.CustomDeptIDs)
	if r.CustomDeptIDs == nil {
		r.CustomDeptIDs = []int64{}
	}
	r.IsSystem = isSystem != 0
	return &r, nil
}

// scanner 抽象 QueryRow 和 Query 共享 Scan
type scanner interface {
	Scan(dest ...interface{}) error
}
