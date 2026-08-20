package database

import (
	"strings"
	"testing"
)

// ==================== Round 19 边界用例测试（#3 修复）====================
//
// 覆盖 BuildWhereSQL 在 DepartmentID=0 时的行为：
//   - dept 模式 DepartmentID=0 → 返错（不再静默 1=0）
//   - dept_and_sub 模式 DepartmentID=0 → 返错
//   - self_and_sub_dept 模式 DepartmentID=0 → 返错
//   - dept_and_sub SubDeptIDs 为空 → 返错
//
// 修复前这些场景会返回 "deleted_at = 0 AND 1=0"，handler 把它当合法结果使用，
// 表现为"看不到任何数据"，与"无权限 403"难以区分。
// 修复后返错，handler 端应映射 500 + 提示"用户未分配主部门"。
//
// 注意：本测试只覆盖 BuildWhereSQL 的纯函数分支，不需 DB。
// ----------------------------------------------------------------------------

func TestBuildWhereSQL_DeptRequiresDepartmentID(t *testing.T) {
	tests := []struct {
		name        string
		scope       *UserDataScope
		opts        FilterOpts
		wantErr     bool
		wantErrText string
	}{
		{
			name: "dept_DepartmentID=0_returns_error",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "dept",
				DepartmentID: 0,
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr:     true,
			wantErrText: "DepartmentID=0",
		},
		{
			name: "dept_and_sub_DepartmentID=0_returns_error",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "dept_and_sub",
				DepartmentID: 0,
				SubDeptIDs:   []int64{1, 2, 3},
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr:     true,
			wantErrText: "DepartmentID=0",
		},
		{
			name: "self_and_sub_dept_DepartmentID=0_returns_error",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "self_and_sub_dept",
				DepartmentID: 0,
				SubDeptIDs:   []int64{1, 2, 3},
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr:     true,
			wantErrText: "DepartmentID=0",
		},
		{
			name: "dept_and_sub_SubDeptIDs_empty_returns_error",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "dept_and_sub",
				DepartmentID: 100,
				SubDeptIDs:   []int64{}, // 空子树
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr:     true,
			wantErrText: "SubDeptIDs",
		},
		{
			name: "dept_happy_path_returns_where",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "dept",
				DepartmentID: 100,
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr: false,
		},
		{
			name: "all_returns_just_deleted_at",
			scope: &UserDataScope{
				UserID:    42,
				DataScope: "all",
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr: false,
		},
		{
			name: "self_DepartmentID=0_returns_owner_only",
			scope: &UserDataScope{
				UserID:       42,
				DataScope:    "self",
				DepartmentID: 0,
			},
			opts: FilterOpts{
				OwnerCol: "created_by",
				DeptCol:  "department_id",
			},
			wantErr: false, // self 模式不需要 DepartmentID
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			where, args, err := BuildWhereSQL(tc.scope, tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Errorf("期望错误，实际成功: where=%q, args=%v", where, args)
					return
				}
				if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
					t.Errorf("错误信息应包含 %q，实际: %v", tc.wantErrText, err)
				}
				return
			}
			if err != nil {
				t.Errorf("不应报错: %v", err)
				return
			}
			if where == "" {
				t.Error("whereSQL 不应为空")
			}
		})
	}
}

// TestBuildWhereSQL_UnknownScopeReturnsError 验证未知 scope 返错。
func TestBuildWhereSQL_UnknownScopeReturnsError(t *testing.T) {
	scope := &UserDataScope{
		UserID:    42,
		DataScope: "bogus_scope",
	}
	_, _, err := BuildWhereSQL(scope, FilterOpts{
		OwnerCol: "created_by",
		DeptCol:  "department_id",
	})
	if err == nil {
		t.Error("未知 scope 应报错")
	}
}

// TestBuildWhereSQL_WhitelistRejectsBadColumns 验证白名单防 SQL 注入。
func TestBuildWhereSQL_WhitelistRejectsBadColumns(t *testing.T) {
	scope := &UserDataScope{UserID: 1, DataScope: "self", DepartmentID: 1}
	tests := []struct {
		name string
		opts FilterOpts
	}{
		{"OwnerCol_not_whitelisted", FilterOpts{OwnerCol: "id; DROP TABLE users", DeptCol: "department_id"}},
		{"DeptCol_not_whitelisted", FilterOpts{OwnerCol: "created_by", DeptCol: "deleted_at; DROP--"}},
		{"TableAlias_invalid_chars", FilterOpts{OwnerCol: "created_by", DeptCol: "department_id", TableAlias: "c; DROP--"}},
		{"both_cols_empty", FilterOpts{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := BuildWhereSQL(scope, tc.opts)
			if err == nil {
				t.Error("白名单校验应拦截")
			}
		})
	}
}