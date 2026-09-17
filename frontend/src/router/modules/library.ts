// 2026-09-17：新建"资料库"顶级菜单，包含文档库（原文档列表）+ 媒体库
// 图标使用项目中已注册的离线图标（见 src/components/ReIcon/src/offlineIcon.ts）

export default {
  path: "/library",
  name: "Library",
  redirect: "/library/document",
  meta: {
    icon: "ri/folder-line",
    title: "资料库",
    rank: 2,
    permissions: ["contract:detail", "media:list"]
  },
  children: [
    {
      path: "/library/document",
      name: "LibraryDocument",
      component: () => import("@/views/contract/index.vue"),
      meta: {
        title: "文档库",
        icon: "ri/file-list-3-line",
        permissions: ["contract:detail"]
      }
    },
    {
      path: "/library/media",
      name: "LibraryMedia",
      component: () => import("@/views/media-library/index.vue"),
      meta: {
        title: "媒体库",
        icon: "ri/image-2-line",
        permissions: ["media:list"]
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
