/**
 * 检测当前页面是否运行在 DocManage Client 桌面客户端内。
 *
 * 识别依据：客户端在 webview 启动参数中注入了 `DocClient/1.0` 标识，
 * webview 内 JS 通过 `navigator.userAgent` 可读到该标识。
 */
export function isDocClient(): boolean {
  if (typeof navigator === "undefined") return false;
  return navigator.userAgent.includes("DocClient");
}