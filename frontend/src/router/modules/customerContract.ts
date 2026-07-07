// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对齐）

export default {
  path: "/customer/contract",
  name: "CustomerContract",
  component: () => import("@/views/customer/contracts.vue"),
  meta: {
    title: "关联文档",
    showLink: false,
    permissions: ["contract:by-customer"]
  }
} satisfies RouteConfigsTable;
