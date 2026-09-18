import { expect, test } from "@playwright/test";
import { E2E_CONFIG } from "../../playwright.config";

// AI 配置管理 E2E 测试
test.describe("AI 配置管理", () => {
  // 登录并导航到 AI 配置页面
  test.beforeEach(async ({ page }) => {
    await page.goto("/");

    // 等待页面加载
    await page.waitForLoadState("networkidle");

    // 如果有登录弹窗，先登录
    const loginDialog = page.locator('.el-dialog');
    if (await loginDialog.isVisible()) {
      await page.fill('input[placeholder*="账号"], input[placeholder*="用户名"]', "admin");
      await page.fill('input[type="password"]', "admin123");
      await page.click('button:has-text("登录"), button:has-text("确定")');
      await page.waitForLoadState("networkidle");
    }

    // 导航到 AI 配置页面
    await page.goto("/system/ai-config");
    await page.waitForLoadState("networkidle");
  });

  test("页面加载正常，显示 AI 配置列表", async ({ page }) => {
    // 验证页面标题
    await expect(page.locator("text=AI 配置")).toBeVisible();

    // 验证表格存在
    await expect(page.locator(".el-table")).toBeVisible();
  });

  test("新建 AI 配置弹窗正常打开", async ({ page }) => {
    // 点击新建按钮
    await page.click('button:has-text("新建 AI 配置")');

    // 验证弹窗打开
    await expect(page.locator('.el-dialog:has-text("新建 AI 配置")')).toBeVisible();

    // 验证 Tab 页
    await expect(page.locator('.el-tabs__item:has-text("服务商预设")')).toBeVisible();
    await expect(page.locator('.el-tabs__item:has-text("自定义配置")')).toBeVisible();

    // 关闭弹窗
    await page.click('.el-dialog__headerbtn');
    await expect(page.locator('.el-dialog')).not.toBeVisible();
  });

  test("服务商预设 Tab 可选择服务商", async ({ page }) => {
    // 打开新建弹窗
    await page.click('button:has-text("新建 AI 配置")');

    // 选择服务商
    await page.click('.el-select:has-text("选择服务商")');
    await page.waitForTimeout(300);

    // 验证有选项
    const options = page.locator('.el-select-dropdown__item');
    await expect(options.first()).toBeVisible();
  });

  test("测试按钮可见且可点击", async ({ page }) => {
    // 等待表格加载
    await page.waitForSelector(".el-table");

    // 如果有配置行，应该有测试按钮
    const testButton = page.locator('button:has-text("测试")').first();
    if (await testButton.isVisible()) {
      await expect(testButton).toBeEnabled();
    }
  });
});

// Copilot AI 助手 E2E 测试
test.describe("Copilot AI 助手", () => {
  test("浮动按钮可见", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // 等待 Copilot 浮动按钮出现
    const copilotFab = page.locator(".copilot-fab");
    await expect(copilotFab).toBeVisible();
  });

  test("点击浮动按钮可打开 Copilot 面板", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // 点击浮动按钮
    await page.click(".copilot-fab");

    // 验证面板打开
    await expect(page.locator(".copilot-panel")).toBeVisible();

    // 验证欢迎信息
    await expect(page.locator(".copilot-panel:has-text(\"你好，我是 AI 助手\")")).toBeVisible();
  });

  test("快捷按钮可见", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // 打开 Copilot 面板
    await page.click(".copilot-fab");
    await expect(page.locator(".copilot-panel")).toBeVisible();

    // 验证快捷按钮
    await expect(page.locator('.quick-btn:has-text("内容摘要")')).toBeVisible();
    await expect(page.locator('.quick-btn:has-text("文本分析")')).toBeVisible();
    await expect(page.locator('.quick-btn:has-text("内容生成")')).toBeVisible();
  });

  test("输入框可输入内容", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // 打开 Copilot 面板
    await page.click(".copilot-fab");

    // 找到输入框
    const textarea = page.locator(".copilot-panel .el-textarea__inner");
    await expect(textarea).toBeVisible();

    // 输入内容
    await textarea.fill("测试消息");
    await expect(textarea).toHaveValue("测试消息");
  });

  test("ESC 可关闭面板", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // 打开 Copilot 面板
    await page.click(".copilot-fab");
    await expect(page.locator(".copilot-panel")).toBeVisible();

    // 按 ESC 关闭
    await page.keyboard.press("Escape");
    await expect(page.locator(".copilot-panel")).not.toBeVisible();
  });
});
