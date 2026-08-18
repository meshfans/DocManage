import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";

// https://vitest.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url))
    }
  },
  test: {
    globals: true, // 免 import describe/it/expect
    environment: "happy-dom", // DOM 环境（用于组件挂载）
    include: ["tests/unit/**/*.test.ts"],
    passWithNoTests: true,
    setupFiles: ["tests/unit/setup.ts"],
    // Element Plus + happy-dom 在 happy-dom 环境已稳定
    css: false,
    testTimeout: 10000
  }
});
