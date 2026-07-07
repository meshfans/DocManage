package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Permission 权限码（1:1 对应一条 API）
type Permission struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Module      string `json:"module"`
	APIPath     string `json:"api_path"`
	HTTPMethod  string `json:"http_method"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// GetPermissionByCode 按 code 查
func GetPermissionByCode(code string) (*Permission, error) {
	row := DB.QueryRow(
		`SELECT id, code, name, module, api_path, http_method, description, is_system, status, created_at, updated_at
		 FROM permission WHERE code = ?`, code)
	return scanPermission(row)
}

// ListAllPermissions 列出全部 permission（admin UI 表格）
func ListAllPermissions() ([]Permission, error) {
	rows, err := DB.Query(
		`SELECT id, code, name, module, api_path, http_method, description, is_system, status, created_at, updated_at
		 FROM permission ORDER BY module, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Permission
	for rows.Next() {
		p, err := scanPermission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ListAllPermissionCodes 仅返 code 列（轻量）
func ListAllPermissionCodes() ([]string, error) {
	rows, err := DB.Query(`SELECT code FROM permission`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListActivePermissionCodes 仅返 status='active' 的 code
func ListActivePermissionCodes() ([]string, error) {
	rows, err := DB.Query(`SELECT code FROM permission WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertPermission 新建/更新（按 code 幂等）
func UpsertPermission(p *Permission) error {
	if p.Code == "" {
		return errors.New("permission code 不能为空")
	}
	status := p.Status
	if status == "" {
		status = "active"
	}
	now := time.Now().Unix()
	_, err := DB.Exec(`
		INSERT INTO permission
		 (code, name, module, api_path, http_method, description, is_system, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(code) DO UPDATE SET
			name        = excluded.name,
			module      = excluded.module,
			api_path    = excluded.api_path,
			http_method = excluded.http_method,
			description = excluded.description,
			status      = excluded.status,
			updated_at  = excluded.updated_at`,
		p.Code, p.Name, p.Module, p.APIPath, p.HTTPMethod,
		p.Description, boolToInt(p.IsSystem), status, now, now)
	return err
}

// CountRolesWithPermission 统计持有指定 permission code 的角色数。
// role.permissions 是 JSON 数组，用 LIKE 匹配带边界符避免子串误匹配（如 customer:create vs customer:create2）。
// 2026-06-25 P0-6.2 修复：用于 DeletePermission 前引用检查。
func CountRolesWithPermission(permissionCode string) (int, error) {
	pattern := fmt.Sprintf(`%%"%s"%%`, permissionCode)
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM role WHERE permissions LIKE ?`, pattern,
	).Scan(&count)
	return count, err
}

// DeletePermission 删除（is_system=1 拒绝）
// 2026-06-25 P0-6.2 修复：删除前先检查 role.permissions JSON 中是否还有角色引用该 permission。
// 引用 > 0 时拒绝删除（避免角色持有的权限码数组里有失效 code）。
func DeletePermission(code string) error {
	p, err := GetPermissionByCode(code)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("permission 不存在: %s", code)
	}
	if p.IsSystem {
		return fmt.Errorf("系统预置 permission 不可删除: %s", code)
	}
	// 引用检查：role.permissions JSON 是否还引用此 code
	count, err := CountRolesWithPermission(code)
	if err != nil {
		return fmt.Errorf("查询权限引用失败: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("permission %s 仍有 %d 个角色引用，请先解除绑定再删除", code, count)
	}
	_, err = DB.Exec(`DELETE FROM permission WHERE code = ?`, code)
	return err
}

// GetPermissionCodesForAPI 核心：API → permission_code 反查（支持 :id 路径参数匹配）
func GetPermissionCodesForAPI(method, realPath string) ([]string, error) {
	rows, err := DB.Query(
		`SELECT code, api_path FROM permission
		 WHERE http_method = ? AND status = 'active' AND api_path != ''`,
		method)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var matched []string
	for rows.Next() {
		var code, tmpl string
		if err := rows.Scan(&code, &tmpl); err != nil {
			return nil, err
		}
		if matchAPIPath(tmpl, realPath) {
			matched = append(matched, code)
		}
	}
	return matched, rows.Err()
}

// matchAPIPath 比对 '/api/users/:id' 与 '/api/users/42'
// 规则：
//   1. ':' 前缀的段为通配（如 ':id' 可匹配任意值）
//   2. 段数必须严格相等（'/api/users/:id' 不匹配 '/api/users/42/edit'，3 段 vs 2 段）
//   3. 因此 1:1 设计要求：每个 API path 模板对应一个 permission_code
//      不能用 user:detail 覆盖 POST /api/users/:id/update（需要另配 user:update）
func matchAPIPath(tmpl, real string) bool {
	ts := strings.Split(strings.Trim(tmpl, "/"), "/")
	rs := strings.Split(strings.Trim(real, "/"), "/")
	if len(ts) != len(rs) {
		return false
	}
	for i, seg := range ts {
		if strings.HasPrefix(seg, ":") {
			continue
		}
		if seg != rs[i] {
			return false
		}
	}
	return true
}

func scanPermission(s scanner) (*Permission, error) {
	var p Permission
	var isSystem int
	err := s.Scan(
		&p.ID, &p.Code, &p.Name, &p.Module,
		&p.APIPath, &p.HTTPMethod, &p.Description,
		&isSystem, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	p.IsSystem = isSystem != 0
	return &p, nil
}