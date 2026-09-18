export default {
  path: "/about",
  name: "About",
  component: () => import("@/views/system/about.vue"),
  meta: {
    icon: "ri/information-line",
    title: "关于我们",
    rank: 11
  }
} satisfies RouteConfigsTable;
