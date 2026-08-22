package database

import (
	"fmt"
	"strings"
	"time"
)

// 本文件：部门（Department）层级结构与树形查询相关操作。

// ==================== Department ====================

// Department 表示一个组织部门，支持多层级（parent_id 指向父部门，level 表示层级）。
type Department struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ParentID  int64  `json:"parent_id"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CreateDepartment 新建一个部门；maxLevel 用于限制最大层级数，防止无限嵌套。
func CreateDepartment(name string, parentID int64, sortOrder int, status string, maxLevel int) (int64, error) {
	var level int
	if parentID > 0 {
		var parentLevel int
		err := DB.QueryRow("SELECT level FROM department WHERE id = ?", parentID).Scan(&parentLevel)
		if err != nil {
			return 0, fmt.Errorf("父部门不存在或已删除")
		}
		level = parentLevel + 1
	}

	if level >= maxLevel {
		return 0, fmt.Errorf("部门层级已达上限（最大%d层）", maxLevel)
	}

	result, err := DB.Exec(`
		INSERT INTO department (name, parent_id, level, sort_order, status)
		VALUES (?, ?, ?, ?, ?)
	`, name, parentID, level, sortOrder, status)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetDepartmentByID 按 id 查询部门。
func GetDepartmentByID(id int64) (*Department, error) {
	dept := &Department{}
	err := DB.QueryRow(`
		SELECT id, name, parent_id, level, sort_order, status, created_at, updated_at
		FROM department WHERE id = ?
	`, id).Scan(&dept.ID, &dept.Name, &dept.ParentID, &dept.Level, &dept.SortOrder, &dept.Status, &dept.CreatedAt, &dept.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return dept, nil
}

// GetDepartmentsByIDs 批量查询部门（用于 CreateSubscription 权限预检，避免 N+1）。
func GetDepartmentsByIDs(ids []int64) (map[int64]*Department, error) {
	if len(ids) == 0 {
		return make(map[int64]*Department), nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT id, name, parent_id, level, sort_order, status, created_at, updated_at
		  FROM department WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]*Department)
	for rows.Next() {
		d := &Department{}
		if err := rows.Scan(&d.ID, &d.Name, &d.ParentID, &d.Level, &d.SortOrder, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		result[d.ID] = d
	}
	return result, nil
}

// GetAllDepartments 返回全部部门，按 level/sort_order 排序便于前端展示层级。
func GetAllDepartments() ([]Department, error) {
	rows, err := DB.Query(`
		SELECT id, name, parent_id, level, sort_order, status, created_at, updated_at
		FROM department ORDER BY level, sort_order
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var departments []Department
	for rows.Next() {
		var dept Department
		err := rows.Scan(&dept.ID, &dept.Name, &dept.ParentID, &dept.Level, &dept.SortOrder, &dept.Status, &dept.CreatedAt, &dept.UpdatedAt)
		if err != nil {
			return nil, err
		}
		departments = append(departments, dept)
	}
	return departments, rows.Err()
}

// DepartmentTree 部门树形结构，Children 为下级部门列表。
type DepartmentTree struct {
	Department
	Children []*DepartmentTree `json:"children"`
}

// GetDepartmentTree 将 GetAllDepartments 的扁平结果组装为以 ParentID=0 为根的树。
func GetDepartmentTree() ([]*DepartmentTree, error) {
	departments, err := GetAllDepartments()
	if err != nil {
		return nil, err
	}

	deptMap := make(map[int64]*DepartmentTree)
	var roots []*DepartmentTree

	for i := range departments {
		deptMap[departments[i].ID] = &DepartmentTree{
			Department: departments[i],
			Children:   []*DepartmentTree{},
		}
	}

	for i := range departments {
		dept := &departments[i]
		tree := deptMap[dept.ID]
		if dept.ParentID == 0 {
			roots = append(roots, tree)
		} else {
			if parent, ok := deptMap[dept.ParentID]; ok {
				parent.Children = append(parent.Children, tree)
			}
		}
	}
	return roots, nil
}

// UpdateDepartment 更新部门信息；maxLevel 用于限制更新后不超过最大层级。
func UpdateDepartment(id int64, name string, parentID int64, sortOrder int, status string, maxLevel int) error {
	var level int
	if parentID > 0 {
		if parentID == id {
			return fmt.Errorf("不能将部门设置为自己子部门")
		}
		var parentLevel int
		err := DB.QueryRow("SELECT level FROM department WHERE id = ?", parentID).Scan(&parentLevel)
		if err != nil {
			return fmt.Errorf("父部门不存在或已删除")
		}
		level = parentLevel + 1
	}
	if level >= maxLevel {
		return fmt.Errorf("部门层级已达上限（最大%d层）", maxLevel)
	}

	_, err := DB.Exec(`
		UPDATE department SET name = ?, parent_id = ?, level = ?, sort_order = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, name, parentID, level, sortOrder, status, time.Now().Unix(), id)
	return err
}

// DeleteDepartment 删除部门：若有子部门则拒绝；否则将所属用户的 department_id 置空。
func DeleteDepartment(id int64) error {
	var childCount int
	err := DB.QueryRow("SELECT COUNT(*) FROM department WHERE parent_id = ?", id).Scan(&childCount)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return fmt.Errorf("cannot delete department with %d child departments", childCount)
	}

	_, err = DB.Exec(`UPDATE users SET department_id = NULL WHERE department_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`DELETE FROM department WHERE id = ?`, id)
	return err
}

// GetDepartmentChildren 返回某部门下的直接子部门。
func GetDepartmentChildren(parentID int64) ([]Department, error) {
	rows, err := DB.Query(`
		SELECT id, name, parent_id, level, sort_order, status, created_at, updated_at
		FROM department WHERE parent_id = ? ORDER BY sort_order
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var departments []Department
	for rows.Next() {
		var dept Department
		err := rows.Scan(&dept.ID, &dept.Name, &dept.ParentID, &dept.Level, &dept.SortOrder, &dept.Status, &dept.CreatedAt, &dept.UpdatedAt)
		if err != nil {
			return nil, err
		}
		departments = append(departments, dept)
	}
	return departments, rows.Err()
}

// CountDepartmentUsers 统计某部门下的用户数。
func CountDepartmentUsers(departmentID int64) (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE department_id = ?", departmentID).Scan(&count)
	return count, err
}

// CountDepartmentChildren 统计某部门下的直接子部门数。
func CountDepartmentChildren(parentID int64) (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM department WHERE parent_id = ?", parentID).Scan(&count)
	return count, err
}
