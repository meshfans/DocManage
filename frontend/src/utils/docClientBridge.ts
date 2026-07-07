import { ElMessage } from "element-plus";
import { isDocClient } from "./isDocClient";

/** DocManage Client 桌面客户端 HTTP 桥 base URL（监听 127.0.0.1:9999）*/
export const BRIDGE_URL = "http://127.0.0.1:9999";

export interface BridgeCall {
  /** 操作名（如 'reset' / 'download'）— 自动拼成 `${BRIDGE_URL}/${operation}` */
  operation: string;
  /** 自定义端点（覆盖默认拼接） */
  endpoint?: string;
  /** 该操作的参数（自动转 URL-encoded body） */
  options?: Record<string, any> | null;
  /** 成功回调 */
  success?: (res: Response) => void;
  /** 失败回调 */
  failed?: (err: Error | string) => void;
  /** 跳过 isDocClient 检查 */
  skipCheck?: boolean;
  /** 成功提示文本（默认按 operation 推断；空字符串则不弹） */
  successMessage?: string;
  /** 完全静默：不弹任何 ElMessage 提示（用于 drag/maximize/close 这类同步 UI 操作） */
  silent?: boolean;
}

const DEFAULT_SUCCESS_MSG: Record<string, string> = {
  reset: "已重置客户端",
  download: "下载已开始"
};

/**
 * 通用 HTTP 桥调用：调 DocManage Client 的 `${BRIDGE_URL}/${operation}` 端点
 *
 * @example
 * ```ts
 * // 1. 最简：reset
 * bridgeCall({ operation: 'reset' })
 *
 * // 2. 带参数：download
 * bridgeCall({ operation: 'download', options: { url: '/api/x.pdf', name: 'x.pdf' } })
 *
 * // 3. 完整：自定义回调 + 提示
 * bridgeCall({
 *   operation: 'reset',
 *   successMessage: '已回到输入界面',
 *   success: () => location.reload(),
 *   failed: (err) => console.error(err)
 * })
 * ```
 */
export async function bridgeCall(args: BridgeCall): Promise<boolean> {
  const {
    operation,
    endpoint = `${BRIDGE_URL}/dispatch`,  // 统一入口：operation 通过 body 的 op 字段传
    options = null,
    success,
    failed,
    skipCheck = false,
    successMessage,
    silent = false
  } = args;

  // 1. 环境检查
  if (!skipCheck && !isDocClient()) {
    const msg = "请在 DocManage Client 中使用此功能";
    failed?.(msg);
    if (!silent) ElMessage.warning(msg);
    return false;
  }

  // 2. 构造 body（始终带 op，附带 options）
  const params = new URLSearchParams();
  params.set("op", operation);
  if (options) {
    for (const [k, v] of Object.entries(options)) {
      if (v !== undefined && v !== null) {
        params.set(k, String(v));
      }
    }
  }
  const body = params.toString();

  // 3. 发起请求
  try {
    const res = await fetch(endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body
    });
    if (res.ok) {
      if (!silent) {
        const msg = successMessage ?? DEFAULT_SUCCESS_MSG[operation] ?? `${operation} 成功`;
        if (msg) ElMessage.success(msg);
      }
      success?.(res);
      return true;
    } else {
      const msg = `${operation} 失败：HTTP ${res.status}`;
      if (!silent) ElMessage.error(msg);
      failed?.(msg);
      return false;
    }
  } catch (e: any) {
    const msg = e?.message || String(e);
    if (!silent) ElMessage.error(`${operation} 失败：${msg}`);
    failed?.(e);
    return false;
  }
}
