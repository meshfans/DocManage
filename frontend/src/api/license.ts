/**
 * License API（2026-09-28 RAG Feature Gate 配套）
 *
 * 后端：handlers/system.go GetLicenseFeatures
 * 数据源：sdk.GlobalOutcome().Result.Payload.Features（启动期 VerifyStartupOrLog 后注入）
 *
 * 用途：前端 any-page 启动时 fetch 一次，缓存到 useUserStore().licenseFeatures，
 *       路由 meta + 页面按钮 / v-if 判 hasFeature("rag") 用。
 *
 * 鉴权：需要 JWT 登录。不需要 admin。
 */

import { http } from "@/utils/http";
import { getToken } from "@/utils/auth";
import type { ApiEnvelope } from "./_envelope";

/**
 * License features 响应。
 *
 * 字段语义：
 *   - features：原始 features 数组（精确字符串相等的元素集合，如 ["rag","ai"]）
 *   - module_keys：业务实际消费的 module key 集合（2026-09-28 仅 ["rag"]）
 *                  前端 hasFeature(key) 应用此集合做精确匹配，避免依赖 features 数组里恰好有 rag
 *                  （若 features=["rag"]，module_keys=["rag"] 都命中；features=["rag","ai"] 都命中）
 *   - expires_at：license 过期时间（Unix 秒），用于 banner 提示 EXPIRING_SOON
 */
export interface LicenseFeatures {
  features: string[];
  module_keys: string[];
  expires_at: number;
}

export function getLicenseFeatures(): Promise<ApiEnvelope<LicenseFeatures>> {
  // 与 llm.ts 对齐：token 是 DataInfo 对象，需要取 .accessToken。
  return http.request<ApiEnvelope<LicenseFeatures>>(
    "get",
    "/api/license/features",
    { headers: { Authorization: `Bearer ${getToken().accessToken}` } }
  );
}
