// 2026-06-25 P2-8.2 修复：合并 contractList.ts → contract.ts。
// 2026-07-06 精简：删主合同（在线文档）；contract 路由父菜单改为"文档列表"，仅保留第三方合同。
//   - 删除：/contract/edit（主合同编辑页）、MineContractList（主合同列表）
//   - 保留：/contract/list（指向 ThirdPartyContractList）、/contract/third-party-detail

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