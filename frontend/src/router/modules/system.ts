// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对对齐）
// 2026-07-06 round3 精简：删除 /system/audit 子路由（审计日志查询/对账 API 全栈下线）
// 2026-08-19：审计日志 Round 17 落地，重新启用 /system/audit 路由（admin only）。
//   M-F4 修复：路由用 `meta.adminOnly: true` 标记，由 router/utils.ts 强制要求
//   `useIsAdmin()` 为真（username === "admin"），不再错误复用 `system:config:get` 权限码。
//   后端走 RequireAdmin 兜底；前端审计页面内部仍用 useIsAdmin 二次守门。

export default {
  path: "/system",
  redirect: "/system/config",
  meta: {
    icon: "ri/settings-3-line",
    title: "系统配置",
    rank: 10,
    permissions: [
      "system:config:get",
      "scheduled:list",
      "reminder:templates:list",
      "backup:list"
    ]
  },
  children: [
    {
      path: "/system/config",
      name: "SystemConfig",
      component: () => import("@/views/system/config.vue"),
      meta: {
        title: "系统配置",
        icon: "ri/settings-4-line",
        permissions: ["system:config:get"]
      }
    },
    {
      path: "/system/scheduled-tasks",
      name: "ScheduledTasks",
      component: () => import("@/views/system/scheduledTask.vue"),
      meta: {
        title: "定时任务",
        icon: "ri/time-line",
        rank: 2,
        permissions: ["scheduled:list"]
      }
    },
    {
      path: "/system/backup",
      name: "BackupManagement",
      component: () => import("@/views/system/backup.vue"),
      meta: {
        title: "备份管理",
        icon: "ri/archive-line",
        rank: 3,
        permissions: ["backup:list"]
      }
    },
    {
      path: "/system/reminder",
      name: "Reminder",
      component: () => import("@/views/system/reminder/index.vue"),
      meta: {
        title: "提醒管理",
        icon: "ri/notification-3-line",
        rank: 5,
        permissions: ["reminder:templates:list"]
      }
    },
    {
      path: "/system/audit",
      name: "AuditLog",
      component: () => import("@/views/system/audit.vue"),
      meta: {
        title: "审计日志",
        icon: "ri/shield-keyhole-line",
        rank: 6,
        // ⚠️ M-F4 修复：admin only 由 meta.adminOnly 标记，不复用 system:config:get 权限码。
        // 后端 audit.* 端点未配置权限码（RequireAdmin 兜底），前端守卫也按 admin 角色拦截。
        // permissions 故意留空数组：isRouteGuarded 见到空数组会跳过 permission 检查；
        // adminOnly 标记由 router/utils.ts 单独拦截。
        permissions: [],
        adminOnly: true
      }
    }
  ]
} satisfies RouteConfigsTable;
