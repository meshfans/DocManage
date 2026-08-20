package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"doc/models"
	"doc/utils"
)

// 本文件：用户（User）表与带部门信息的扩展结构（UserWithDepartment）相关操作。

// ==================== User ====================

// User 表示系统中的一个用户；Roles/Permissions 存 JSON 字符串。
type User struct {
	ID               int64  `json:"id"`
	Username         string `json:"username"`
	PasswordHash     string `json:"-"`
	Nickname         string `json:"nickname"`
	Avatar           string `json:"avatar"`
	Roles            string `json:"roles"`
	Permissions      string `json:"permissions"`
	RealName         string `json:"real_name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	DepartmentID     *int64 `json:"department_id"`
	Position         string `json:"position"`
	EmployeeNo       string `json:"employee_no"`
	Status           string `json:"status"`
	FailedLoginCount int   `json:"failed_login_count,omitempty"`
	LockedUntil      int64 `json:"locked_until,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// ToAPIModel 将内部 User（含 JSON 字符串字段）转换为对外的 models.User。
func (u *User) ToAPIModel() *models.User {
	var roles []string
	var permissions []string

	if err := json.Unmarshal([]byte(u.Roles), &roles); err != nil {
		utils.Info("Warning: Failed to parse roles for user %s: %v", u.Username, err)
		roles = []string{}
	}
	if err := json.Unmarshal([]byte(u.Permissions), &permissions); err != nil {
		utils.Info("Warning: Failed to parse permissions for user %s: %v", u.Username, err)
		permissions = []string{}
	}

	return &models.User{
		ID:               u.ID,
		Username:         u.Username,
		Nickname:         u.Nickname,
		Avatar:           u.Avatar,
		Roles:            roles,
		Permissions:      permissions,
		RealName:         u.RealName,
		Email:            u.Email,
		Phone:            u.Phone,
		DepartmentID:     u.DepartmentID,
		Position:         u.Position,
		EmployeeNo:       u.EmployeeNo,
		Status:           u.Status,
		FailedLoginCount: u.FailedLoginCount,
		LockedUntil:      u.LockedUntil,
	}
}

// GetUserByUsername 按用户名查询用户（用于登录）。
func GetUserByUsername(username string) (*User, error) {
	user := &User{}
	var deptID sql.NullInt64
	err := DB.QueryRow(
		`SELECT id, username, password_hash, nickname, avatar, roles, permissions,
		        real_name, email, phone, department_id, position, employee_no,
		        status, failed_login_count, locked_until, created_at, updated_at
		 FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Nickname, &user.Avatar,
		&user.Roles, &user.Permissions, &user.RealName, &user.Email, &user.Phone,
		&deptID, &user.Position, &user.EmployeeNo, &user.Status,
		&user.FailedLoginCount, &user.LockedUntil,
		&user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return nil, err
	}
	if deptID.Valid {
		user.DepartmentID = &deptID.Int64
	}
	return user, nil
}

// CreateUserExt 创建用户并返回 id；支持完整字段（实名/邮箱/部门等）。
func CreateUserExt(username, password, nickname, realName, email, phone, position, employeeNo, status string, departmentID *int64) (int64, error) {
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return 0, err
	}

	roles := `["common"]`
	permissions := `[]`
	if nickname == "" {
		nickname = username
	}

	result, err := DB.Exec(
		`INSERT INTO users (username, password_hash, nickname, real_name, email, phone, position, employee_no, roles, permissions, status, department_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		username, hashedPassword, nickname, realName, email, phone, position, employeeNo, roles, permissions, status, departmentID, time.Now().Unix(), time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// UpdateUserPassword 更新指定用户的密码（明文密码入参，内部 bcrypt）。
// 由 handlers/auth.go 的 ChangePassword 调用。
func UpdateUserPassword(userID int64, newPassword string) error {
	hashed, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = DB.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hashed, time.Now().Unix(), userID,
	)
	return err
}

// DeleteUser 删除指定用户。
func DeleteUser(id int64) error {
	_, err := DB.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// ==================== J.6 登录失败计数 + 临时锁定 ====================
//
// 防 brute force：连续失败 N 次后锁定用户 M 分钟，登录前先校验 locked_until。
// 调用方流程：
//   1. GetUserByUsername → 检查 LockedUntil > now() → 拒绝
//   2. CheckPassword 失败 → IncrementFailedLogin(userID)
//      若返回的 count >= maxAttempts → LockUser(userID, lockedUntil)
//   3. CheckPassword 成功 → ResetFailedLogin(userID)
//
// 常量（用户可见，便于改）：
//   - loginMaxAttempts = 5
//   - loginLockMinutes = 15

// LoginMaxAttempts 触发锁定的连续失败次数。
const LoginMaxAttempts = 5

// LoginLockMinutes 锁定时长（分钟）。
const LoginLockMinutes = 15

// IncrementFailedLogin 失败 +1；返回当前 count + locked_until（0 表示未锁）。
//
// 调用方根据 count 是否 >= LoginMaxAttempts 决定是否调 LockUser。
func IncrementFailedLogin(userID int64) (count int, lockedUntil int64, err error) {
	tx, err := DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	_, err = tx.Exec(
		`UPDATE users
		    SET failed_login_count = failed_login_count + 1,
		        updated_at = ?
		  WHERE id = ?`, now, userID)
	if err != nil {
		return 0, 0, err
	}
	row := tx.QueryRow(`SELECT failed_login_count, locked_until FROM users WHERE id = ?`, userID)
	err = row.Scan(&count, &lockedUntil)
	if err != nil {
		return 0, 0, err
	}
	return count, lockedUntil, tx.Commit()
}

// LockUser 设置 locked_until = now + LoginLockMinutes。
func LockUser(userID int64) error {
	until := time.Now().Add(time.Duration(LoginLockMinutes) * time.Minute).Unix()
	_, err := DB.Exec(
		`UPDATE users SET locked_until = ?, updated_at = ? WHERE id = ?`,
		until, time.Now().Unix(), userID)
	return err
}

// ResetFailedLogin 登录成功后清零失败计数与锁定状态。
func ResetFailedLogin(userID int64) error {
	_, err := DB.Exec(
		`UPDATE users SET failed_login_count = 0, locked_until = 0, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), userID)
	return err
}

// ==================== User With Department ====================

// UserWithDepartment 扩展 User，附带 DepartmentName 便于前端展示。
type UserWithDepartment struct {
	User
	DepartmentName string `json:"department_name"`
}

// GetUsersWithDepartment 返回全部用户（LEFT JOIN 部门名）。
func GetUsersWithDepartment() ([]UserWithDepartment, error) {
	rows, err := DB.Query(`
		SELECT u.id, u.username, u.nickname, u.avatar, u.roles, u.permissions,
		       u.real_name, u.email, u.phone, u.department_id, u.position, u.employee_no,
		       u.status, u.created_at, u.updated_at, COALESCE(d.name, '') as department_name
		FROM users u
		LEFT JOIN department d ON u.department_id = d.id
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserWithDepartment
	for rows.Next() {
		var user UserWithDepartment
		var deptID sql.NullInt64
		err := rows.Scan(
			&user.ID, &user.Username, &user.Nickname, &user.Avatar, &user.Roles, &user.Permissions,
			&user.RealName, &user.Email, &user.Phone, &deptID, &user.Position, &user.EmployeeNo,
			&user.Status, &user.CreatedAt, &user.UpdatedAt, &user.DepartmentName,
		)
		if err != nil {
			return nil, err
		}
		if deptID.Valid {
			user.DepartmentID = &deptID.Int64
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// GetUsersWithDepartmentPaginated 分页 + 模糊搜索用户（按 username/nickname/real_name/email/phone/employee_no/department.name）。
func GetUsersWithDepartmentPaginated(page, pageSize int, search string) ([]UserWithDepartment, int, error) {
	var total int
	var countQuery string
	var countArgs []interface{}
	var query string
	var args []interface{}

	if search != "" {
		searchPattern := "%" + search + "%"
		countQuery = `SELECT COUNT(*) FROM users u LEFT JOIN department d ON u.department_id = d.id
		              WHERE u.username LIKE ? OR u.nickname LIKE ? OR u.real_name LIKE ? OR u.email LIKE ? OR u.phone LIKE ? OR u.employee_no LIKE ? OR d.name LIKE ?`
		countArgs = []interface{}{searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern}

		query = `SELECT u.id, u.username, u.nickname, u.avatar, u.roles, u.permissions,
		       u.real_name, u.email, u.phone, u.department_id, u.position, u.employee_no,
		       u.status, u.created_at, u.updated_at, COALESCE(d.name, '') as department_name
		FROM users u
		LEFT JOIN department d ON u.department_id = d.id
		WHERE u.username LIKE ? OR u.nickname LIKE ? OR u.real_name LIKE ? OR u.email LIKE ? OR u.phone LIKE ? OR u.employee_no LIKE ? OR d.name LIKE ?
		ORDER BY u.created_at DESC
		LIMIT ? OFFSET ?`
		args = []interface{}{searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, pageSize, (page - 1) * pageSize}
	} else {
		countQuery = "SELECT COUNT(*) FROM users"
		countArgs = []interface{}{}

		query = `SELECT u.id, u.username, u.nickname, u.avatar, u.roles, u.permissions,
		       u.real_name, u.email, u.phone, u.department_id, u.position, u.employee_no,
		       u.status, u.created_at, u.updated_at, COALESCE(d.name, '') as department_name
		FROM users u
		LEFT JOIN department d ON u.department_id = d.id
		ORDER BY u.created_at DESC
		LIMIT ? OFFSET ?`
		args = []interface{}{pageSize, (page - 1) * pageSize}
	}

	err := DB.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []UserWithDepartment
	for rows.Next() {
		var user UserWithDepartment
		var deptID sql.NullInt64
		err := rows.Scan(
			&user.ID, &user.Username, &user.Nickname, &user.Avatar, &user.Roles, &user.Permissions,
			&user.RealName, &user.Email, &user.Phone, &deptID, &user.Position, &user.EmployeeNo,
			&user.Status, &user.CreatedAt, &user.UpdatedAt, &user.DepartmentName,
		)
		if err != nil {
			return nil, 0, err
		}
		if deptID.Valid {
			user.DepartmentID = &deptID.Int64
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

// GetUserByID 按 id 查询用户（带部门名）。
func GetUserByID(id int64) (*UserWithDepartment, error) {
	var user UserWithDepartment
	var deptID sql.NullInt64
	err := DB.QueryRow(`
		SELECT u.id, u.username, u.nickname, u.avatar, u.roles, u.permissions,
		       u.real_name, u.email, u.phone, u.department_id, u.position, u.employee_no,
		       u.status, u.created_at, u.updated_at, COALESCE(d.name, '') as department_name
		FROM users u
		LEFT JOIN department d ON u.department_id = d.id
		WHERE u.id = ?
	`, id).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Avatar, &user.Roles, &user.Permissions,
		&user.RealName, &user.Email, &user.Phone, &deptID, &user.Position, &user.EmployeeNo,
		&user.Status, &user.CreatedAt, &user.UpdatedAt, &user.DepartmentName,
	)
	if err != nil {
		return nil, err
	}
	if deptID.Valid {
		user.DepartmentID = &deptID.Int64
	}
	return &user, nil
}

// GetUsersByDepartment 返回某部门下全部用户。
func GetUsersByDepartment(departmentID int64) ([]UserWithDepartment, error) {
	rows, err := DB.Query(`
		SELECT u.id, u.username, u.nickname, u.avatar, u.roles, u.permissions,
		       u.real_name, u.email, u.phone, u.department_id, u.position, u.employee_no,
		       u.status, u.created_at, u.updated_at, COALESCE(d.name, '') as department_name
		FROM users u
		LEFT JOIN department d ON u.department_id = d.id
		WHERE u.department_id = ?
		ORDER BY u.created_at DESC
	`, departmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserWithDepartment
	for rows.Next() {
		var user UserWithDepartment
		var deptID sql.NullInt64
		err := rows.Scan(
			&user.ID, &user.Username, &user.Nickname, &user.Avatar, &user.Roles, &user.Permissions,
			&user.RealName, &user.Email, &user.Phone, &deptID, &user.Position, &user.EmployeeNo,
			&user.Status, &user.CreatedAt, &user.UpdatedAt, &user.DepartmentName,
		)
		if err != nil {
			return nil, err
		}
		if deptID.Valid {
			user.DepartmentID = &deptID.Int64
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// UpdateUserDepartment 调整用户的所属部门（传入 nil 表示清空）。
func UpdateUserDepartment(userID int64, departmentID *int64) error {
	_, err := DB.Exec(`UPDATE users SET department_id = ?, updated_at = ? WHERE id = ?`, departmentID, time.Now().Unix(), userID)
	return err
}

// UpdateUserRoles 单独更新用户的角色分配（仅改 users.roles JSON 字段）
// 调用方负责：旧 user 信息用于失效权限缓存、role.code 合法性校验、审计日志
func UpdateUserRoles(userID int64, roles []string) error {
	if roles == nil {
		roles = []string{}
	}
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return err
	}
	_, err = DB.Exec(
		`UPDATE users SET roles = ?, updated_at = ? WHERE id = ?`,
		string(rolesJSON), time.Now().Unix(), userID)
	return err
}

// CountUsersWithRole 统计持有指定 role code 的用户数。
// roles 列是 JSON 数组（如 ["admin","manager"]），所以用 LIKE 匹配带边界符避免子串误匹配。
// 例如 role="admin" 时匹配 "admin" 但不会误匹配 "administrator"。
func CountUsersWithRole(roleCode string) (int, error) {
	// 用双引号 + 逗号 + 边界符包裹 code，避免子串误匹配（如 admin vs administrator）
	pattern := fmt.Sprintf(`%%"%s"%%`, roleCode)
	// 同时匹配首/尾位置：roles 可能是 ["admin"] 或 ["admin","manager"]
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE roles LIKE ?`, pattern,
	).Scan(&count)
	return count, err
}

// GetUserIDsByRole 2026-06-29 RBAC v3 P5：取所有持有指定 role code 的用户 ID 列表。
// 用途：admin 改 role.data_scope / role.permissions 时，精准失效这些用户的缓存。
// 角色 JSON 用 LIKE 匹配，pattern 同 CountUsersWithRole。
func GetUserIDsByRole(roleCode string) ([]int64, error) {
	pattern := fmt.Sprintf(`%%"%s"%%`, roleCode)
	rows, err := DB.Query(`SELECT id FROM users WHERE roles LIKE ?`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetUserIDsByDepartmentSubtree 2026-06-29 RBAC v3 P5：取某部门子树下的所有用户 ID。
// 用途：admin 改 department.parent_id（导致 SubDeptIDs 变化）时，精准失效这些用户的缓存。
// 使用 CTE 单查询完成"部门子树 + 该子树所有 user.id" 的获取（避免 N+1）。
// 只返回 status='active' 的部门内的 user（已停用部门不参与）。
func GetUserIDsByDepartmentSubtree(rootDeptID int64) ([]int64, error) {
	if rootDeptID <= 0 {
		return nil, nil
	}
	rows, err := DB.Query(`
		WITH RECURSIVE dept_tree(id) AS (
			SELECT id FROM department WHERE id = ? AND status = 'active'
			UNION
			SELECT d.id FROM department d
			JOIN dept_tree t ON d.parent_id = t.id
			WHERE d.status = 'active'
		)
		SELECT u.id FROM users u
		JOIN dept_tree t ON u.department_id = t.id
		WHERE u.id > 0
	`, rootDeptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// UpdateUserDetails 按需更新用户字段（空字符串表示不动该字段）。
func UpdateUserDetails(id int64, username, password, nickname, realName, email, phone, position, employeeNo, status string, departmentID *int64) error {
	fields := []string{}
	args := []interface{}{}

	if username != "" {
		fields = append(fields, "username = ?")
		args = append(args, username)
	}
	if password != "" {
		hashedPassword, err := utils.HashPassword(password)
		if err != nil {
			return err
		}
		fields = append(fields, "password_hash = ?")
		args = append(args, hashedPassword)
	}
	if nickname != "" {
		fields = append(fields, "nickname = ?")
		args = append(args, nickname)
	}
	if realName != "" {
		fields = append(fields, "real_name = ?")
		args = append(args, realName)
	}
	if email != "" {
		fields = append(fields, "email = ?")
		args = append(args, email)
	}
	if phone != "" {
		fields = append(fields, "phone = ?")
		args = append(args, phone)
	}
	if position != "" {
		fields = append(fields, "position = ?")
		args = append(args, position)
	}
	if employeeNo != "" {
		fields = append(fields, "employee_no = ?")
		args = append(args, employeeNo)
	}
	if status != "" {
		fields = append(fields, "status = ?")
		args = append(args, status)
	}
	if departmentID != nil {
		fields = append(fields, "department_id = ?")
		args = append(args, *departmentID)
	}

	fields = append(fields, "updated_at = ?")
	args = append(args, time.Now().Unix(), id)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(fields, ", "))
	_, err := DB.Exec(query, args...)
	return err
}

// GetEffectivePermissionsForUser 计算用户最终权限码：
// 1. 合并 users.permissions（直配）+ 所有 role.permissions（角色继承）
// 2. 过滤 status='active' 的 code（permission 表的）
// 3. 去重 + 排序
func GetEffectivePermissionsForUser(userID int64) ([]string, error) {
	user, err := GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户不存在: id=%d", userID)
	}

	// 1. 用户直配
	var userPerms []string
	_ = json.Unmarshal([]byte(user.Permissions), &userPerms)

	// 2. 角色继承
	var roleCodes []string
	_ = json.Unmarshal([]byte(user.Roles), &roleCodes)
	roles, err := GetRolesByCodes(roleCodes)
	if err != nil {
		return nil, err
	}
	var rolePerms []string
	for _, r := range roles {
		rolePerms = append(rolePerms, r.Permissions...)
	}

	// 3. 取 active permission 集合（含通配）
	activeCodes, err := ListActivePermissionCodes()
	if err != nil {
		return nil, err
	}
	activeSet := make(map[string]struct{}, len(activeCodes)+1)
	for _, c := range activeCodes {
		activeSet[c] = struct{}{}
	}
	activeSet["*:*:*"] = struct{}{} // 通配永远 active

	// 4. 去重 + 过滤 + 排序
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, p := range append(userPerms, rolePerms...) {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if _, ok := activeSet[p]; ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}
