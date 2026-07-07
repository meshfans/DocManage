/**
 * 检测当前页面是否运行在 DocClient（Tauri 桌面客户端）内。
 *
 * 识别依据：Tauri 客户端在 webview 启动参数中注入了 `DocClient/1.0` 标识
 * （见 client/src-tauri/tauri.conf.json 的 additionalBrowserArgs），
 * webview 内 JS 通过 `navigator.userAgent` 可读到该标识。
 *
 * 用途：业务代码可以据此决定是否调用 Tauri HTTP 桥的端点（如 /reset, /download）。
 *
 * @example
 * ```ts
 * import { isDocClient } from "@/utils/isDocClient";
 *
 * if (isDocClient()) {
 *   // 在 Tauri 客户端内：调桥端点
 *   await fetch("http://127.0.0.1:9999/reset", { method: "POST" });
 * } else {
 *   // 普通浏览器：提示用户在客户端内操作
 * }
 * ```
 */
export function isDocClient(): boolean {
  if (typeof navigator === "undefined") return false;
  return navigator.userAgent.includes("DocClient");
}