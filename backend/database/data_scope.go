package database

import (
	"encoding/json"
	"fmt"
	"strings"

	"doc/utils"
)

// 本文件：RBAC v3 数据级权限（data_scope）的数据库侧 helper。
// BuildWhereSQL 升级为通用 FilterOpts 接口，所有资源表
// （customer / media / reminder_subscription / third_party_contract）
// 共用同一份 WHERE 拼接逻辑，配合白名单校验防 SQL 注入。
//
// 简化：
//   - 一个用户一个主部门（来自 users.department_id），跨部门走 custom
//   - data_scope 通过 GetUserEffectiveDataScope(userID) 读取
//   - 子部门递归通过 SQLite CTE 单查询完成

// UserDataScope 用户的数据级权限上下文。
type UserDataScope struct {
	UserID       int64   // 当前用户 ID
	Username     string  // 用户名（admin 短路判断）
	RoleCode     string  // 当前生效角色（取 user.roles[0]）
	DataScope    string  // 数据范围值（all/dept_and_sub/dept/self_and_sub_dept/self/custom）
	DepartmentID int64   // 用户主部门 ID（0 = 无主部门）
	SubDeptIDs   []int64 // 本部门 + 所有下级部门 ID（dept_and_sub / self_and_sub_dept 用）
	CustomDepts  []int64 // custom 模式下的部门白名单（P5 接入，本期为空）
}

// FilterOpts BuildWhereSQL 的入参，描述资源表的列名 + 过滤选项。
// 白名单校验防 SQL 注入。
type FilterOpts struct {
	TableAlias      string // SQL 别名（"c" / "cu" / "m"），可空
	OwnerCol        string // 白名单：created_by / owner_user_id / user_id 之一，可空（无 owner 列的表自动降级 dept-only）
	DeptCol         string // 白名单：department_id（可空，缺列的表自动降级 owner-only）
	IncludeSubDepts bool   // true = IN 整个 SubDeptIDs 列表；false = 仅 DepartmentID
}

// 白名单常量（防 SQL 注入）。
var (
	whitelistOwnerCols = map[string]bool{
		"created_by":    true,
		"owner_user_id": true,
		"user_id":       true,
	}
	whitelistDeptCols = map[string]bool{
		"department_id": true,
	}
)

// validTableAlias 表别名白名单（仅允许字母数字下划线）。
// 空字符串视为合法（表示无别名）；非空时所有字符必须在 [a-zA-Z0-9_] 内。
func validTableAlias(alias string) bool {
	if alias == "" {
		return true
	}
	for _, r := range alias {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	// 走到这里说明循环未返回 false，且 alias 非空（上面 early return 处理过空）→ 必合法
	return true
}

// GetUserMainDepartment 返回用户主部门 ID（0 = 无主部门）。
func GetUserMainDepartment(userID int64) (int64, error) {
	var deptID *int64
	err := DB.QueryRow(`SELECT department_id FROM users WHERE id = ?`, userID).Scan(&deptID)
	if err != nil {
		return 0, err
	}
	if deptID == nil {
		return 0, nil
	}
	return *deptID, nil
}

// GetDescendantDepartmentIDs 返回 rootID 及其所有后代部门 ID（含 rootID 自身）。
// 使用 SQLite CTE 单查询完成树形遍历，避免 BFS 的 N+1 查询。
// status='active' 过滤已禁用部门；parent_id 形成环时自动终止（UNION 不递归重复行）。
func GetDescendantDepartmentIDs(rootID int64) ([]int64, error) {
	if rootID <= 0 {
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
		SELECT id FROM dept_tree
	`, rootID)
	if err != nil {
		return nil, fmt.Errorf("递归查部门子树失败 (root=%d): %w", rootID, err)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 根部门不存在（status!='active' 或已删除）→ 返回空而非 nil，避免上层误判
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// GetUserEffectiveDataScope 解析用户的有效 data_scope（向后兼容入口）。
// 多角色时取"最宽松"合并结果；单角色场景行为一致（all > dept_and_sub > dept > custom > self_and_sub_dept > self）。
func GetUserEffectiveDataScope(userID int64) (roleCode, dataScope string, err error) {
	merged, err := GetUserMergedDataScope(userID)
	if err != nil {
		return "", "self", err
	}
	// merged.RoleCodes 非空时取第一个作为"主角色"（保持旧 API 行为，便于日志/审计）
	if len(merged.RoleCodes) > 0 {
		return merged.RoleCodes[0], merged.DataScope, nil
	}
	return "", merged.DataScope, nil
}

// MergedDataScope 多角色 data_scope 合并结果。
// 当用户持有多个 role 时，effective data_scope 取"最宽松"的。
// 合并规则（按优先级从高到低）：
//
//	all > dept_and_sub > dept > custom > self_and_sub_dept > self
//
// 特殊：custom 模式时 CustomDepts 为所有 custom 角色部门的并集（去重）。
type MergedDataScope struct {
	RoleCodes  []string // 参与合并的所有 role code（按 codes 顺序）
	DataScope  string   // 合并后的有效 data_scope
	CustomDepts []int64 // 仅当 DataScope='custom' 时有意义
}

// GetUserMergedDataScope 加载用户多角色的合并 data_scope。
// 合并策略：
//  1. 解析 user.roles JSON → role code 列表
//  2. 一次性查所有 role 的 data_scope + custom_dept_ids（避免 N+1）
//  3. 按优先级取最宽松的 scope；custom 模式下合并所有 custom 部门的并集
//  4. roles 为空 / 解析失败 → 回退 'self'
func GetUserMergedDataScope(userID int64) (*MergedDataScope, error) {
	var rolesJSON string
	if err := DB.QueryRow(`SELECT roles FROM users WHERE id = ?`, userID).Scan(&rolesJSON); err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal([]byte(rolesJSON), &codes); err != nil || len(codes) == 0 {
		return &MergedDataScope{RoleCodes: nil, DataScope: "self", CustomDepts: nil}, nil
	}
	roles, err := GetRolesByCodes(codes)
	if err != nil {
		return nil, err
	}
	merged := mergeDataScopes(codes, roles)
	return merged, nil
}

// dataScopePriority 6 个 scope 的优先级（数字越大越宽松）。
// "最宽松" 意味着 user 能看的数据越多，因此合并时取 max。
var dataScopePriority = map[string]int{
	"all":              60,
	"dept_and_sub":     50,
	"dept":             40,
	"custom":           30,
	"self_and_sub_dept": 20,
	"self":             10,
}

// mergeDataScopes 合并多角色的 data_scope（取最宽松 + custom 部门并集）。
// codes 保留原顺序（用于审计/日志），roles 可少于 codes（角色被删/未配置）。
func mergeDataScopes(codes []string, roles []Role) *MergedDataScope {
	// code → role 索引
	roleByCode := make(map[string]Role, len(roles))
	for _, r := range roles {
		roleByCode[r.Code] = r
	}
	bestScope := "self"
	bestPriority := dataScopePriority["self"]
	var customUnion []int64
	customSeen := make(map[int64]bool)

	for _, code := range codes {
		r, ok := roleByCode[code]
		if !ok || r.DataScope == "" {
			continue
		}
		p, known := dataScopePriority[r.DataScope]
		if !known {
			// 未知 scope（schema 异常）→ 跳过；不让它污染合并结果
			utils.Warn("[data_scope] role=%s 携带未知 scope=%s，跳过合并", code, r.DataScope)
			continue
		}
		if p > bestPriority {
			bestPriority = p
			bestScope = r.DataScope
		}
		// 无论 bestScope 是否被更新，都累计 custom 部门（后续如切到 custom 可直接用）
		if r.DataScope == "custom" {
			for _, id := range r.CustomDeptIDs {
				if !customSeen[id] {
					customSeen[id] = true
					customUnion = append(customUnion, id)
				}
			}
		}
	}
	if customUnion == nil {
		customUnion = []int64{}
	}
	return &MergedDataScope{
		RoleCodes:   codes,
		DataScope:   bestScope,
		CustomDepts: customUnion,
	}
}

// LoadUserDataScope 加载用户完整数据级权限上下文。
// admin 短路：username=="admin" → data_scope='all'。
// 错误时返回 nil + err（middleware 决定 fallback 策略）。
//
// 当 data_scope='custom' 时，从 role.custom_dept_ids 加载部门白名单，
// 并通过 CTE 单查询过滤掉已删除 / 已禁用的部门。
//
// 多角色合并后，custom 模式使用所有 custom 角色部门的并集（已去重 + 过滤死引用）。
func LoadUserDataScope(userID int64, username string) (*UserDataScope, error) {
	scope := &UserDataScope{UserID: userID, Username: username}
	merged, err := GetUserMergedDataScope(userID)
	if err != nil {
		utils.Warn("[data_scope] 加载 user=%d data_scope 失败: %v，回退 self", userID, err)
		merged = &MergedDataScope{DataScope: "self"}
	}
	// 保留 RoleCode 字段兼容（取第一个 role code，便于审计/日志）
	roleCode := ""
	if len(merged.RoleCodes) > 0 {
		roleCode = merged.RoleCodes[0]
	}
	scope.RoleCode = roleCode
	scope.DataScope = merged.DataScope

	if username == "admin" {
		scope.DataScope = "all"
		return scope, nil
	}

	if merged.DataScope == "self" {
		return scope, nil
	}
	deptID, err := GetUserMainDepartment(userID)
	if err != nil {
		return nil, fmt.Errorf("加载用户主部门失败: %w", err)
	}
	scope.DepartmentID = deptID
	if deptID == 0 {
		scope.DataScope = "self"
		return scope, nil
	}
	if merged.DataScope == "dept_and_sub" || merged.DataScope == "self_and_sub_dept" {
		ids, err := GetDescendantDepartmentIDs(deptID)
		if err != nil {
			return nil, fmt.Errorf("加载部门子树失败: %w", err)
		}
		scope.SubDeptIDs = ids
	}
	if merged.DataScope == "custom" {
		// 2026-06-29 P5：用合并后的 custom 部门并集，过滤死引用
		liveIDs, err := filterLiveDeptIDs(merged.CustomDepts)
		if err != nil {
			return nil, fmt.Errorf("加载 custom 模式部门白名单失败: %w", err)
		}
		scope.CustomDepts = liveIDs
		if len(liveIDs) == 0 {
			utils.Warn("[data_scope] user=%d custom 模式无有效部门（可能被删除）", userID)
		}
	}
	return scope, nil
}

// filterLiveDeptIDs 过滤掉已删除/已禁用的部门。
// 支持任意 int64 slice（多角色合并场景）。
func filterLiveDeptIDs(rawIDs []int64) ([]int64, error) {
	if len(rawIDs) == 0 {
		return nil, nil
	}
	// 构造 IN (?,?,...) 单查询过滤
	placeholders := make([]string, len(rawIDs))
	args := make([]interface{}, len(rawIDs))
	for i, id := range rawIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := DB.Query(
		`SELECT id FROM department WHERE status = 'active' AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	live := make([]int64, 0, len(rawIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		live = append(live, id)
	}
	return live, rows.Err()
}

// loadCustomDeptIDs 已废弃：被 filterLiveDeptIDs 取代（支持多角色合并）。
// Deprecated: use filterLiveDeptIDs with merged CustomDepts instead.
func loadCustomDeptIDs(roleCode string) ([]int64, error) {
	var rawJSON string
	if err := DB.QueryRow(`SELECT COALESCE(custom_dept_ids, '[]') FROM role WHERE code = ?`, roleCode).Scan(&rawJSON); err != nil {
		return nil, err
	}
	var rawIDs []int64
	if err := json.Unmarshal([]byte(rawJSON), &rawIDs); err != nil || len(rawIDs) == 0 {
		return nil, nil
	}
	return filterLiveDeptIDs(rawIDs)
}

// BuildWhereSQL 根据 UserDataScope + FilterOpts 拼装资源表的 WHERE 子句。
//
// 返回值：
//   - whereSQL：包含 "deleted_at = 0" 前缀（handler 可直接 AND 业务条件）
//   - args：占位符对应的实参
//   - err：白名单校验失败 / 必填列缺失
//
// 白名单规则：
//   - TableAlias：仅允许 [a-zA-Z0-9_]+，空表示无别名
//   - OwnerCol：必须 ∈ {"created_by", "owner_user_id", "user_id"} 或空
//   - DeptCol：必须 ∈ {"department_id"} 或空
//
// 6 个 scope 行为：
//   - all                       → 仅 deleted_at=0
//   - self                      → deleted_at=0 AND owner_col=?
//   - dept                      → deleted_at=0 AND dept_col=?
//   - dept_and_sub              → deleted_at=0 AND dept_col IN (...)
//   - self_and_sub_dept         → deleted_at=0 AND (owner_col=? OR dept_col IN (...))
//   - custom                    → deleted_at=0 AND dept_col IN (CustomDepts...)
//   - 缺列自动降级：缺 OwnerCol → 降级为 dept-only；缺 DeptCol → 降级为 owner-only
//   - 未知 scope / 参数非法 → 1=0 恒假 + 错误返回
func BuildWhereSQL(scope *UserDataScope, opts FilterOpts) (string, []any, error) {
	if scope == nil {
		return "", nil, fmt.Errorf("scope 不能为 nil")
	}
	// 白名单校验
	if !validTableAlias(opts.TableAlias) {
		return "", nil, fmt.Errorf("非法 TableAlias: %q（仅允许 [a-zA-Z0-9_]）", opts.TableAlias)
	}
	if opts.OwnerCol != "" && !whitelistOwnerCols[opts.OwnerCol] {
		return "", nil, fmt.Errorf("非法 OwnerCol: %q（白名单：created_by/owner_user_id/user_id）", opts.OwnerCol)
	}
	if opts.DeptCol != "" && !whitelistDeptCols[opts.DeptCol] {
		return "", nil, fmt.Errorf("非法 DeptCol: %q（白名单：department_id）", opts.DeptCol)
	}
	if opts.OwnerCol == "" && opts.DeptCol == "" {
		return "", nil, fmt.Errorf("OwnerCol 和 DeptCol 至少需要一个")
	}

	// 列名加别名（如 "c.created_by"）
	qualify := func(col string) string {
		if opts.TableAlias == "" {
			return col
		}
		return opts.TableAlias + "." + col
	}

	// 公共：deleted_at = 0（资源表都默认有 soft delete）
	deletedAtCol := "deleted_at"
	if opts.TableAlias != "" {
		deletedAtCol = qualify("deleted_at")
	}
	base := deletedAtCol + " = 0"

	if scope.DataScope == "all" {
		return base, nil, nil
	}

	switch scope.DataScope {
	case "self":
		if opts.OwnerCol == "" {
			// 缺 owner 列 → 降级为 dept（如果有）
			if opts.DeptCol != "" {
				return base + " AND " + qualify(opts.DeptCol) + " = ?", []any{scope.DepartmentID}, nil
			}
			return base + " AND 1=0", nil, nil
		}
		return base + " AND " + qualify(opts.OwnerCol) + " = ?", []any{scope.UserID}, nil

	case "dept":
		if opts.DeptCol == "" {
			// 缺 dept 列 → 降级为 owner
			if opts.OwnerCol != "" {
				return base + " AND " + qualify(opts.OwnerCol) + " = ?", []any{scope.UserID}, nil
			}
			return base + " AND 1=0", nil, nil
		}
		return base + " AND " + qualify(opts.DeptCol) + " = ?", []any{scope.DepartmentID}, nil

	case "dept_and_sub":
		if opts.DeptCol == "" {
			if opts.OwnerCol != "" {
				return base + " AND " + qualify(opts.OwnerCol) + " = ?", []any{scope.UserID}, nil
			}
			return base + " AND 1=0", nil, nil
		}
		if len(scope.SubDeptIDs) == 0 {
			return base + " AND 1=0", nil, nil
		}
		where, args := buildINWhere(qualify(opts.DeptCol), scope.SubDeptIDs)
		return base + " AND " + where, args, nil

	case "self_and_sub_dept":
		// 防御性：OwnerCol 为空时无法拼接 owner_col = ? OR ...，降级到 dept-only。
		if opts.OwnerCol == "" {
			if len(scope.SubDeptIDs) == 0 {
				return base + " AND 1=0", nil, nil
			}
			where, args := buildINWhere(qualify(opts.DeptCol), scope.SubDeptIDs)
			return base + " AND " + where, args, nil
		}
		if len(scope.SubDeptIDs) == 0 {
			return base + " AND " + qualify(opts.OwnerCol) + " = ?", []any{scope.UserID}, nil
		}
		where, args := buildINWhere(qualify(opts.DeptCol), scope.SubDeptIDs)
		cond := base + " AND (" + qualify(opts.OwnerCol) + " = ? OR " + where + ")"
		return cond, append([]any{scope.UserID}, args...), nil

	case "custom":
		if len(scope.CustomDepts) == 0 {
			// custom 模式未配置 → 返回错误而非静默降级 self。
			return base + " AND 1=0", nil, fmt.Errorf("custom 模式未配置部门白名单（CustomDepts 为空）")
		}
		where, args := buildINWhere(qualify(opts.DeptCol), scope.CustomDepts)
		return base + " AND " + where, args, nil

	default:
		// 未知 scope：恒假 + 错误返回（handler 应拒绝）
		return base + " AND 1=0", nil, fmt.Errorf("未知 data_scope: %s", scope.DataScope)
	}
}

// buildINWhere 把 slice 拼成 `col IN (?,?,...)`。
// ids 必须非空（调用方保证）。
func buildINWhere(col string, ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ",")), args
}
