/**
 * License Feature Gate（2026-09-28 RAG Feature Gate 配套）
 *
 * 设计：
 *   - 当前 license features 从 useUserStore().moduleKeys（响应 module_keys 字段）读
 *   - hasFeature(key) 做精确字符串相等匹配（不区分大小写、不 trim、不子串）
 *   - login 后 / pinia 初始化时 fetch /api/license/features 一次缓存
 *   - 注销时 moduleKeys 清空 → 所有 hasFeature 返 false
 *
 * 当前业务实际消费的 module key：rag / ocr（预留）/ ai。
 * 其中 "ai" 控制 AI 配置页显隐（见 isAIConfigVisible）。
 *
 * 用法示例：
 *   const hasRag = hasFeature('rag');
 *   if (hasRag) {
 *     // 启用 RAG 模块 UI
 *   }
 */

import { useUserStore } from "@/store/modules/user";

/**
 * 当前业务实际消费的 module key（与后端 sdk.BusinessModuleKeys 一一对应）。
 *
 * ⚠️ 必须与 backend/pkg/license/sdk/feature.go BusinessModuleKeys 同步维护。
 * 后端是单一权威源；前端这份仅用于 TS 类型推导（KnownModuleKey）+ 编译期 key 收敛，
 * 实际运行时校验走 useUserStore().moduleKeys（来自 /api/license/features 响应）。
 *
 * 当前集合（2026-10-01 统一）：rag / ocr（预留）/ ai。
 *   - "rag" — RAG 向量检索
 *   - "ocr" — OCR 模块（预留，具体功能未接入）
 *   - "ai"  — AI 配置开关，控制 ai_config 页面显隐
 */
export const KNOWN_MODULE_KEYS = ["rag", "ocr", "ai"] as const;
export type KnownModuleKey = (typeof KNOWN_MODULE_KEYS)[number];

/**
 * AI 配置页是否可见。
 *
 * 语义：
 *   - 有 "ai" feature → 允许用户自由配置 LLM 接入（显示 AI 配置页）
 *   - 无 "ai" feature → 隐藏该页，**默认使用 meshfans 提供的 LLM 能力**
 *
 * 未登录 / moduleKeys 尚未拉取时返回 false（保守：先隐藏，拉到 feature 后再显示）。
 */
export function isAIConfigVisible(): boolean {
  return hasFeature("ai");
}

/**
 * 取当前 user store 中的 module keys 集合。
 * 若未登录或未初始化，返回空 Set（→ 所有 hasFeature 返 false）。
 */
function getModuleKeySet(): Set<string> {
  try {
    const store = useUserStore();
    return new Set(store.moduleKeys ?? []);
  } catch {
    // store 可能在 login 前未注入 → return empty
    return new Set<string>();
  }
}

/**
 * 判当前 license 是否启用指定 feature key。
 *
 * 规则：
 *   - 仅做精确字符串相等（"rag" 命中，"RAG" 不命中；"ragi" 不命中）
 *   - 不在 module_keys 集合里 → false
 *
 * @param key 模块 key（推荐用 KnownModuleKey 类型）
 * @returns boolean
 */
export function hasFeature(key: string): boolean {
  return getModuleKeySet().has(key);
}

/**
 * 同步接口：列出当前 license 的全部启用 module key（按 module_keys 数组透传）。
 * 主要给 UI 调试 / banner 用。
 */
export function activeModuleKeys(): string[] {
  try {
    const store = useUserStore();
    return Array.isArray(store.moduleKeys) ? [...store.moduleKeys] : [];
  } catch {
    return [];
  }
}
