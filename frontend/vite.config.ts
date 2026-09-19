import { getPluginsList } from "./build/plugins";
import { include, exclude } from "./build/optimize";
import { type UserConfigExport, type ConfigEnv, loadEnv } from "vite";
import {
  root,
  alias,
  wrapperEnv,
  pathResolve,
  __APP_INFO__
} from "./build/utils";

export default ({ mode }: ConfigEnv): UserConfigExport => {
  const env = loadEnv(mode, root);
  const {
    VITE_CDN,
    VITE_PORT,
    VITE_COMPRESSION,
    VITE_PUBLIC_PATH,
    VITE_API_BASE_URL,
    VITE_PROXY_TARGET
  } = wrapperEnv(env);

  return {
    base: VITE_PUBLIC_PATH,
    root,
    resolve: {
      alias
    },
    assetsInclude: ["**/*.ico"],
    server: {
      // 端口号
      port: VITE_PORT,
      host: "0.0.0.0",
      // 浏览器 dev 代理：/api/* 转发到后端，避免 CORS
      // 留空则不启用（生产模式不需要 dev proxy）
      proxy: VITE_PROXY_TARGET
        ? {
            "/api": {
              target: VITE_PROXY_TARGET,
              changeOrigin: true
            },
            // WebSocket 透传：Vite 默认只代理 HTTP，需显式声明 ws: true + http→ws 协议替换。
            // VITE_PROXY_TARGET 形如 http://127.0.0.1:8090，正则把 http 替换为 ws 后变 ws://127.0.0.1:8090；
            // 同理 https → wss。/api/ws 比 /api 更具体，所以即使 /api 在前也会优先匹配此条。
            "/api/ws": {
              target: VITE_PROXY_TARGET.replace(/^http/, "ws"),
              ws: true,
              changeOrigin: true
            }
          }
        : undefined
    },
    plugins: getPluginsList(VITE_CDN, VITE_COMPRESSION),
    // https://cn.vitejs.dev/config/dep-optimization-options.html#dep-optimization-options
    optimizeDeps: {
      include,
      exclude
    },
    build: {
      // https://cn.vitejs.dev/guide/build.html#browser-compatibility
      target: "es2015",
      sourcemap: false,
      // 消除打包大小超过500kb警告
      chunkSizeWarningLimit: 4000,
      // 将小资源内联为 base64（50KB）
      assetsInlineLimit: 50000,
      rollupOptions: {
        input: {
          index: pathResolve("./index.html", import.meta.url)
        },
        // 静态资源分类打包
        output: {
          chunkFileNames: "static/js/[name]-[hash].js",
          entryFileNames: "static/js/[name]-[hash].js",
          assetFileNames: "static/[ext]/[name]-[hash].[ext]"
        }
      }
    },
    define: {
      __INTLIFY_PROD_DEVTOOLS__: false,
      __APP_INFO__: JSON.stringify(__APP_INFO__)
    }
  };
};
