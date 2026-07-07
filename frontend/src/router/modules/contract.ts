export default {
  path: "/contract",
  name: "Contract",
  redirect: "/contract/list",
  meta: {
    icon: "ri/file-list-3-line",
    title: "文档列表",
    rank: 2,
    permissions: ["contract:detail"]
  },
  children: [
    {
      path: "/contract/list",
      name: "ContractList",
      component: () => import("@/views/contract/index.vue"),
      meta: {
        title: "文档列表",
        permissions: ["contract:detail"]
      }
    },
    {
      path: "/contract/third-party-detail",
      name: "ThirdPartyContractDetail",
      component: () =>
        import("@/views/contract/components/ThirdPartyContractDetail.vue"),
      meta: {
        title: "文档详情",
        showLink: false,
        permissions: ["contract:detail"]
      }
    }
  ]
} satisfies RouteConfigsTable;