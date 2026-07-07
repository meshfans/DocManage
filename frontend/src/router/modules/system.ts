// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对对齐）
// 2026-07-06 round3 精简：删除 /system/audit 子路由（审计日志查询/对账 API 全栈下线）

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
    }
  ]
} satisfies RouteConfigsTable;
