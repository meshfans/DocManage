// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对齐）

export default {
  path: "/user-mgmt",
  redirect: "/user-mgmt/users",
  meta: {
    icon: "ri/user-settings-line",
    title: "用户管理",
    rank: 8,
    permissions: [
      "user:list",
      "dept:list",
      "rbac:roles:list",
      "rbac:permissions:list"
    ]
  },
  children: [
    {
      path: "/user-mgmt/users",
      name: "Users",
      component: () => import("@/views/system/user.vue"),
      meta: {
        title: "员工管理",
        icon: "ri/user-follow-line",
        rank: 1,
        permissions: ["user:list"]
      }
    },
    {
      path: "/user-mgmt/departments",
      name: "Departments",
      component: () => import("@/views/system/department.vue"),
      meta: {
        title: "部门管理",
        icon: "ri/git-branch-line",
        rank: 2,
        permissions: ["dept:list"]
      }
    },
    {
      path: "/user-mgmt/roles",
      name: "RoleManagement",
      component: () => import("@/views/rbac/role.vue"),
      meta: {
        title: "角色管理",
        icon: "ri/vip-crown-2-line",
        rank: 3,
        permissions: ["rbac:roles:list"]
      }
    },
    {
      path: "/user-mgmt/permissions",
      name: "PermissionManagement",
      component: () => import("@/views/rbac/permission.vue"),
      meta: {
        title: "权限管理",
        icon: "ri/key-2-line",
        rank: 4,
        permissions: ["rbac:permissions:list"]
      }
    }
  ]
} satisfies RouteConfigsTable;
