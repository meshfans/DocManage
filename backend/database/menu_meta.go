package database

// MenuItem 菜单项定义
// 用于 RBAC v2 动态路由下发（替代前端硬编码 meta）
//
// 设计：
//   - 一个 MenuItem 对应一个前端路由
//   - RequiredPerms：用户至少拥有其中一个权限码才能看到此菜单
//     （admin 通过 *:*:* 通配符自动满足所有 RequiredPerms）
//   - Parent="" 表示顶级菜单，否则为父菜单的 Path
//   - Component 是相对路径（如 "system/user"），前端用 import.meta.glob 解析为完整组件
type MenuItem struct {
	Path          string      `json:"path"`
	Name          string      `json:"name,omitempty"`
	Title         string      `json:"title,omitempty"`
	Icon          string      `json:"icon,omitempty"`
	Component     string      `json:"component,omitempty"`
	Rank          int         `json:"rank,omitempty"`
	Parent        string      `json:"parent,omitempty"`
	ShowParent    bool        `json:"showParent,omitempty"`
	KeepAlive     bool        `json:"keepAlive,omitempty"`
	RequiredPerms []string    `json:"-"`
	Children      []*MenuItem `json:"children,omitempty"`
}

// Component 字段约定：
//   - 不填（""）：说明此菜单是父菜单（不含具体页面），前端 addRoute 时不挂 component
//   - 短路径（"system/user"）：前端 addAsyncRoutes 用 includes() 匹配，可行
//   - 全路径（"@/views/system/user"）：更精确，避免 includes 误匹配（推荐）
//   - 当前用短路径，与前端 modulesRoutes glob 匹配（"/src/views/<short>.vue" 包含 short 字符串）

// MENU_REGISTRY 全量菜单注册表（PR-9 + PR-11）
// 当 RBAC 角色/权限变更后，此表不需修改；
// 用户实际看到的菜单 = RequiredPerms 与 effective_permissions 的交集
var MENU_REGISTRY = []MenuItem{
	// ========== 顶级菜单 ==========
	{
		Path:          "/user-mgmt",
		Title:         "用户管理",
		Icon:          "ri/user-settings-line",
		Rank:          8,
		RequiredPerms: []string{"user:list", "dept:list", "rbac:roles:list", "rbac:permissions:list"},
	},
	{
		Path:          "/customer",
		Title:         "客户列表",
		Icon:          "ri/group-line",
		Rank:          1,
		RequiredPerms: []string{"customer:list"},
	},

	{
		Path:          "/system",
		Title:         "系统配置",
		Icon:          "ri/settings-3-line",
		Rank:          10,
		ShowParent:    true,
		RequiredPerms: []string{"system:config:get", "scheduled:list", "reminder:templates:list", "backup:list"},
	},

	// ========== 用户管理子菜单 ==========
	{
		Path:          "/user-mgmt/users",
		Name:          "Users",
		Title:         "员工管理",
		Icon:          "ri/user-follow-line",
		Component:     "system/user",
		Rank:          1,
		Parent:        "/user-mgmt",
		RequiredPerms: []string{"user:list"},
	},
	{
		Path:          "/user-mgmt/departments",
		Name:          "Departments",
		Title:         "部门管理",
		Icon:          "ri/git-branch-line",
		Component:     "system/department",
		Rank:          2,
		Parent:        "/user-mgmt",
		RequiredPerms: []string{"dept:list"},
	},
	{
		Path:          "/user-mgmt/roles",
		Name:          "RoleManagement",
		Title:         "角色管理",
		Icon:          "ri/vip-crown-2-line",
		Component:     "rbac/role",
		Rank:          3,
		Parent:        "/user-mgmt",
		RequiredPerms: []string{"rbac:roles:list"},
	},
	{
		Path:          "/user-mgmt/permissions",
		Name:          "PermissionManagement",
		Title:         "权限管理",
		Icon:          "ri/key-2-line",
		Component:     "rbac/permission",
		Rank:          4,
		Parent:        "/user-mgmt",
		RequiredPerms: []string{"rbac:permissions:list"},
	},

	// ========== 客户子菜单 ==========
	{
		Path:          "/customer/individual",
		Name:          "CustomerIndividual",
		Title:         "个人客户",
		Icon:          "ri/user-line",
		Component:     "customer/individual",
		Rank:          1,
		Parent:        "/customer",
		RequiredPerms: []string{"customer:list"},
	},
	{
		Path:          "/customer/enterprise",
		Name:          "CustomerEnterprise",
		Title:         "企业客户",
		Icon:          "ri/building-line",
		Component:     "customer/enterprise",
		Rank:          2,
		Parent:        "/customer",
		RequiredPerms: []string{"customer:list"},
	},


	// ========== 系统配置子菜单 ==========
	{
		Path:          "/system/config",
		Name:          "SystemConfig",
		Title:         "系统配置",
		Icon:          "ri/settings-4-line",
		Component:     "system/config",
		Rank:          1,
		Parent:        "/system",
		RequiredPerms: []string{"system:config:get"},
	},
	{
		Path:          "/system/scheduled-tasks",
		Name:          "ScheduledTasks",
		Title:         "定时任务",
		Icon:          "ri/time-line",
		Component:     "system/scheduledTask",
		Rank:          2,
		Parent:        "/system",
		RequiredPerms: []string{"scheduled:list"},
	},
	{
		Path:          "/system/backup",
		Name:          "BackupManagement",
		Title:         "备份管理",
		Icon:          "ri/archive-line",
		Component:     "system/backup",
		Rank:          3,
		Parent:        "/system",
		RequiredPerms: []string{"backup:list"},
	},
	{
		Path:          "/system/reminder",
		Name:          "Reminder",
		Title:         "提醒管理",
		Icon:          "ri/notification-3-line",
		Component:     "system/reminder/index",
		Rank:          5,
		Parent:        "/system",
		RequiredPerms: []string{"reminder:templates:list"},
	},
}

// menuItemVisible 判断 user 是否有权看 menu
func menuItemVisible(item *MenuItem, userPerms []string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	if len(item.RequiredPerms) == 0 {
		return true
	}
	permSet := make(map[string]struct{}, len(userPerms))
	for _, p := range userPerms {
		permSet[p] = struct{}{}
		if p == "*:*:*" {
			return true
		}
	}
	for _, need := range item.RequiredPerms {
		if _, ok := permSet[need]; ok {
			return true
		}
	}
	return false
}

// BuildMenuTree 根据 user 权限构建菜单树
// 规则：
//  1. 顶层菜单（Parent=""）：用户有任何 RequiredPerms 则显示
//  2. 子菜单：用户有任何 RequiredPerms 则显示
//  3. 顶层菜单有至少 1 个可见子菜单才显示
func BuildMenuTree(userPerms []string, isAdmin bool) []*MenuItem {
	// 1. 先过滤所有可见的菜单
	var visible []*MenuItem
	for i := range MENU_REGISTRY {
		if menuItemVisible(&MENU_REGISTRY[i], userPerms, isAdmin) {
			visible = append(visible, &MENU_REGISTRY[i])
		}
	}

	// 2. 统计每个父菜单的可见子菜单数
	parentHasChild := make(map[string]bool)
	for _, item := range visible {
		if item.Parent != "" {
			parentHasChild[item.Parent] = true
		}
	}

	// 3. 顶层菜单：自己有权限 OR 有可见子菜单
	var topLevel []*MenuItem
	for _, item := range visible {
		if item.Parent == "" {
			topLevel = append(topLevel, item)
		}
	}

	// 4. 把所有 visible 项挂到对应父菜单下
	childrenByParent := make(map[string][]*MenuItem)
	for _, item := range visible {
		if item.Parent != "" {
			childrenByParent[item.Parent] = append(childrenByParent[item.Parent], item)
		}
	}

	// 5. 顶层菜单按 rank 升序
	sortMenuItems(topLevel)

	// 6. 子菜单按 rank 升序
	for k := range childrenByParent {
		sortMenuItems(childrenByParent[k])
	}

	// 7. 组装树
	var result []*MenuItem
	for _, parent := range topLevel {
		kids := childrenByParent[parent.Path]
		// 如果父菜单自己没有权限但有可见子菜单，保留父菜单
		// （某些场景父菜单"管理"没具体权限，但子菜单有，父菜单应保留）
		if len(kids) == 0 && !menuItemVisible(parent, userPerms, isAdmin) {
			continue
		}
		parent.Children = kids
		result = append(result, parent)
	}
	return result
}

func sortMenuItems(items []*MenuItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Rank < items[j-1].Rank; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
