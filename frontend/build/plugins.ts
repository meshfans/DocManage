import vue from "@vitejs/plugin-vue";
import { viteBuildInfo } from "./info";
import svgLoader from "vite-svg-loader";
import Icons from "unplugin-icons/vite";
import type { PluginOption } from "vite";
import vueJsx from "@vitejs/plugin-vue-jsx";
import tailwindcss from "@tailwindcss/vite";
import { configCompressPlugin } from "./compress";
import removeNoMatch from "vite-plugin-router-warn";
import { visualizer } from "rollup-plugin-visualizer";
import removeConsole from "vite-plugin-remove-console";
import { vitePluginFakeServer } from "vite-plugin-fake-server";

// CDN 模块已删除（2026-07-06 简化版）
// 原因：项目为纯内网部署，CDN 模式无意义；
//       vite-plugin-cdn-import 在 vite.config.ts 加载时会立即读取
//       node_modules/<name>/package.json，pnpm 严格模式不会提升间接依赖
//       （如 vue-demi）到根 node_modules，导致 dev 启动失败。
// 如未来需要恢复，把 build/cdn.ts 重新加回并取消下面 null 行的注释即可。

export function getPluginsList(
  VITE_CDN: boolean,
  VITE_COMPRESSION: ViteCompression
): PluginOption[] {
  const lifecycle = process.env.npm_lifecycle_event;
  return [
    tailwindcss(),
    vue(),
    // jsx、tsx语法支持
    vueJsx(),
    // 2026-07-06 简化版：禁用 code-inspector-plugin
    // 原因：该插件需要写入 node_modules/.pnpm/code-inspector-plugin/dist/record.json，
    //       在 pnpm 严格模式下该目录只读，会触发 EPERM 错误。
    //       功能上对纯本地部署无意义（依赖 IDE 集成），故禁用。
    //       如未来需要恢复 IDE 元素定位功能，请取消下面代码的注释并配置 IDE。
    // codeInspectorPlugin({
    //   bundler: "vite",
    //   hideConsole: true
    // }),
    viteBuildInfo(),
    /**
     * 开发环境下移除非必要的vue-router动态路由警告No match found for location with path
     * 非必要具体看 https://github.com/vuejs/router/issues/521 和 https://github.com/vuejs/router/issues/359
     * vite-plugin-router-warn只在开发环境下启用，只处理vue-router文件并且只在服务启动或重启时运行一次，性能消耗可忽略不计
     */
    removeNoMatch(),
    // mock支持
    vitePluginFakeServer({
      logger: false,
      include: "mock",
      infixName: false,
      enableProd: true
    }),
    // svg组件化支持
    svgLoader(),
    // 自动按需加载图标
    Icons({
      compiler: "vue3",
      scale: 1
    }),
    // VITE_CDN ? cdn : null,  // CDN 已禁用，见文件顶部注释
    configCompressPlugin(VITE_COMPRESSION),
    // 线上环境删除console
    removeConsole({ external: ["src/assets/iconfont/iconfont.js"] }),
    // 打包分析（pnpm report 或 VITE_ANALYZE=1 启用）
    lifecycle === "report" || process.env.VITE_ANALYZE === "1"
      ? visualizer({ open: false, brotliSize: true, filename: "report.html" })
      : (null as any)
  ];
}
