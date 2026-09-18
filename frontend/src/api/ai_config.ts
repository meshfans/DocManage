/**
 * AI 配置 API
 */
import { http } from "@/utils/http";
import type { ApiEnvelope } from "./_envelope";

export type Provider =
  | "openai"
  | "anthropic"
  | "google"
  | "ollama"
  | "deepseek"
  | "zhipu"
  | "qwen"
  | "kimi"
  | "doubao"
  | "minimax"
  | "siliconflow"
  | "custom";

export type Protocol =
  | "openai_chat"
  | "openai_responses"
  | "anthropic_messages"
  | "google_generative"
  | "ollama_chat";

export interface AIConfigItem {
  id: number;
  name: string;
  model_name: string;
  provider: Provider | string;
  protocol: Protocol | string;
  api_base: string;
  api_path: string;
  api_key: string;
  api_key_has_value: boolean;
  default_params: string;
  extra: string;
  is_default: boolean;
  status: number;
  multimodal_supported: number | null;
  multimodal_checked_at: number | null;
  multimodal_check_source: string | null;
  test_result: string | null;
  test_result_at: number | null;
  created_at: number;
  updated_at: number;
}

export interface AIConfigPayload {
  id?: number;
  name?: string;
  provider: Provider | string;
  protocol: Protocol | string;
  model_name: string;
  api_base: string;
  api_path?: string;
  api_key?: string;
  default_params?: Record<string, unknown>;
  extra?: Record<string, unknown>;
  is_default?: boolean;
}

export interface AIConfigListResp {
  list: AIConfigItem[];
  total: number;
}

export interface AITestResult {
  ok: boolean;
  latencyMs: number;
  message: string;
  provider?: string;
  model?: string;
  multimodal?: {
    supported: boolean | null;
    message: string;
    latencyMs: number;
  };
}

export interface AIToggleMultimodalResult {
  id: number;
  multimodal_supported: number;
  multimodal_checked_at: number;
  multimodal_check_source: string;
}

export interface AIMeta {
  protocols: Array<{ value: string; label: string }>;
}

/** GET /api/ai-configs */
export function listAIConfigs() {
  return http.request<ApiEnvelope<AIConfigListResp>>("get", "/api/ai-configs");
}

/** GET /api/ai-configs/:id */
export function getAIConfig(id: number) {
  return http.request<ApiEnvelope<{ data: AIConfigItem }>>("get", `/api/ai-configs/${id}`);
}

/** POST /api/ai-configs */
export function createAIConfig(payload: AIConfigPayload) {
  return http.request<ApiEnvelope<{ id: number }>>("post", "/api/ai-configs", { data: payload });
}

/** POST /api/ai-configs/:id */
export function updateAIConfig(id: number, payload: AIConfigPayload) {
  return http.request<ApiEnvelope<{ id: number }>>("post", `/api/ai-configs/${id}`, { data: payload });
}

/** POST /api/ai-configs/:id/key */
export function updateAIConfigKey(id: number, apiKey: string) {
  return http.request<ApiEnvelope<{ id: number }>>("post", `/api/ai-configs/${id}/key`, {
    data: { api_key: apiKey }
  });
}

/** POST /api/ai-configs/:id/delete */
export function deleteAIConfig(id: number) {
  return http.request<ApiEnvelope<{ id: number }>>("post", `/api/ai-configs/${id}/delete`);
}

/** POST /api/ai-configs/:id/default */
export function setDefaultAIConfig(id: number) {
  return http.request<ApiEnvelope<{ id: number }>>("post", `/api/ai-configs/${id}/default`);
}

/** POST /api/ai-configs/:id/test */
export function testAIConfig(id: number) {
  return http.request<AITestResult>("post", `/api/ai-configs/${id}/test`);
}

/** POST /api/ai-configs/test */
export function testAIConfigInline(payload: {
  provider: string;
  protocol: string;
  model_name: string;
  api_base: string;
  api_path?: string;
  api_key?: string;
  default_params?: Record<string, unknown>;
}) {
  return http.request<AITestResult>("post", "/api/ai-configs/test", { data: payload });
}

/** POST /api/ai-configs/:id/multimodal */
export function toggleAIMultimodal(id: number, supported: 0 | 1) {
  return http.request<ApiEnvelope<AIToggleMultimodalResult>>("post", `/api/ai-configs/${id}/multimodal`, {
    data: { supported }
  });
}

/** GET /api/ai-meta */
export function getAIMeta() {
  return http.request<ApiEnvelope<AIMeta>>("get", "/api/ai-meta");
}
