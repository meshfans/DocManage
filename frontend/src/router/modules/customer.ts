// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对齐）

export default {
  path: "/customer",
  name: "Customer",
  redirect: "/customer/individual",
  meta: {
    icon: "ri/group-line",
    title: "客户列表",
    rank: 1,
    permissions: ["customer:list"]
  },
  children: [
    {
      path: "/customer/individual",
      name: "CustomerIndividual",
      component: () => import("@/views/customer/individual.vue"),
      meta: {
        title: "个人客户",
        icon: "ri/user-line",
        rank: 1,
        permissions: ["customer:list"]
      }
    },
    {
      path: "/customer/enterprise",
      name: "CustomerEnterprise",
      component: () => import("@/views/customer/enterprise.vue"),
      meta: {
        title: "企业客户",
        icon: "ri/building-line",
        rank: 2,
        permissions: ["customer:list"]
      }
    }
  ]
} satisfies RouteConfigsTable;
