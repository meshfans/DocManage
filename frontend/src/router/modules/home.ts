// 2026-06-27 RBAC v1.2：为 /welcome 增加权限兜底
//   - 后端 seed_rbac.go L92：user:info 在 v1.1 起加入 common 角色
//     （"所有登录用户需持有以刷新自己的 roles/permissions（Plan B reload）"）
//   - 因此 user:info 是天然的"已登录用户"标识，所有正常登录用户都有此权限
//   - 如果某账号连 user:info 都没有，说明账号配置异常，应该跳 403 而不是放行首页

const { VITE_HIDE_HOME } = import.meta.env;
const Layout = () => import("@/layout/index.vue");

export default {
  path: "/",
  name: "Home",
  component: Layout,
  redirect: "/welcome",
  meta: {
    icon: "ep/home-filled",
    title: "首页",
    rank: 0
    // 注：父路由 `/` 不设 permissions，是纯 layout 壳子，
    //     子路由 /welcome 的权限兜底即可覆盖实际访问
  },
  children: [
    {
      path: "/welcome",
      name: "Welcome",
      component: () => import("@/views/welcome/index.vue"),
      meta: {
        title: "首页",
        showLink: VITE_HIDE_HOME === "true" ? false : true,
        // 2026-06-27 RBAC v1.2：所有已登录用户都能看首页
        //   - 选 user:info 作兜底：common 角色默认持有（Plan B 必需）
        //   - 任何能登录的账号理论上都应通过
        permissions: ["user:info"]
      }
    }
  ]
} satisfies RouteConfigsTable;
