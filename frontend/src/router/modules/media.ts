// 2026-06-27 RBAC v1.2：meta.roles → meta.permissions（与后端 permission.code 对齐）
// 2026-07-06 精简：删摄像记录（/media/camera），父菜单改为"媒体库"

export default {
  path: "/media",
  redirect: "/media/library",
  meta: {
    icon: "ri/file-list-3-line",
    title: "媒体库",
    rank: 8,
    permissions: ["media:list"]
  },
  children: [
    {
      path: "/media/library",
      name: "MediaLibrary",
      component: () => import("@/views/media-library/index.vue"),
      meta: {
        title: "媒体库",
        icon: "ri/file-list-3-line",
        permissions: ["media:list"]
      }
    }
  ]
} satisfies RouteConfigsTable;