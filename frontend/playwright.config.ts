import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { homedir } from "node:os";

// 避免 OneDrive 重定向导致 playwright-transform-cache 写入 EPERM
process.env.HOME = homedir();

const __dirname = dirname(fileURLToPath(import.meta.url));

/**
 * Playwright E2E 测试配置
 *
 * 使用前置条件：
 *   1. 后端服务运行（默认 http://localhost:8090）
 *      - 启动: cd backend && go run . --config bin/config.test-main.json
 *   2. 前端 dev server 由本配置 webServer 自动拉起
 *
 * 常用命令：
 *   pnpm test:e2e         运行全部 E2E
 *   pnpm test:e2e:ui      UI 调试模式
 *   pnpm test:e2e:headed  有头浏览器
 *   pnpm test:e2e:auth    仅跑登录相关
 *   pnpm test:e2e:report  查看 HTML 报告
 */
const PORT = Number(process.env.E2E_PORT ?? 18090);
const BASE_URL = process.env.E2E_BASE_URL ?? `http://localhost:${PORT}`;
const BACKEND_URL = process.env.E2E_BACKEND_URL ?? "http://localhost:8090";

export default defineConfig({
  testDir: "./tests/e2e",
  // 完全排除 Vitest 单元测试
  testIgnore: ["**/tests/unit/**", "**/node_modules/**"],

  // 默认串行执行（共用后端数据库，避免数据竞争）
  fullyParallel: false,
  workers: 1,

  // 失败时最多重试 2 次，便于排查 flaky
  retries: process.env.CI ? 2 : 0,

  // 单测超时
  timeout: 30 * 1000,
  expect: { timeout: 5 * 1000 },

  reporter: process.env.CI
    ? [["html", { open: "never" }], ["junit", { outputFile: "tests/reports/e2e-junit.xml" }]]
    : "list",

  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    // 中文 UI 用更宽松的 locale，避免某些断言被影响
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    actionTimeout: 10 * 1000,
    navigationTimeout: 15 * 1000
  },

  // 按需启动 dev server（CI 或本地首次跑会自动拉起）
  webServer: {
    command: "pnpm dev",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 120 * 1000,
    stdout: "ignore",
    stderr: "pipe"
  },

  // 把后端 URL 注入到项目（测试里通过 process.env.E2E_BACKEND_URL 访问）
  // globalSetup: resolve(__dirname, "tests/e2e/setup/global-setup.ts"),

  projects: [
    {
      name: "chromium",
      testMatch: /.*\.(spec|test)\.ts/,
      testIgnore: [
        /.*\/api\/.*\.spec\.ts/, // API 测试走 api project
        /.*\/_demo\/.*\.spec\.ts/, // 演示测试不算 CI
        /.*\/_debug\/.*\.spec\.ts/
      ],
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        // E2E_SLOW=500 让每个操作慢 500ms（用于人工观察 headed 模式）
        actionTimeout: 10_000,
        launchOptions: process.env.E2E_SLOW
          ? { slowMo: Number(process.env.E2E_SLOW) }
          : undefined
      }
    },
    {
      // 纯 API 测试 project：无浏览器，纯 Node fetch
      name: "api",
      testMatch: /.*\/api\/.*\.spec\.ts/,
      use: {},
      // API 测试不需要前端 dev server
      webServer: false
    }
  ]
});

// 暴露给测试用例
export const E2E_CONFIG = {
  baseURL: BASE_URL,
  backendURL: BACKEND_URL,
  port: PORT
};