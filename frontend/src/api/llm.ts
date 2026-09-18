/**
 * LLM Copilot API
 */
import { http } from "@/utils/http";
import type { ApiEnvelope } from "./_envelope";
import { getToken } from "@/utils/auth";

export interface LLMChatRequest {
  input: string;
  context?: Record<string, unknown>;
  module?: string;
}

export interface LLMResponse {
  content: string;
  draft?: unknown;
  citations?: Array<{ title: string; source: string; score: number }>;
  suggestions?: string[];
  usage: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

/** POST /api/llm/chat 非流式对话 */
export function chatLLM(data: LLMChatRequest) {
  return http.request<ApiEnvelope<LLMResponse>>("post", "/api/llm/chat", { data });
}

/** POST /api/llm/stream 流式对话（返回 SSE 流） */
export function streamLLM(data: LLMChatRequest): Promise<Response> {
  const token = getToken();
  const accessToken = typeof token === "string" ? token : token?.accessToken;

  return fetch("/api/llm/stream", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${accessToken}`,
    },
    body: JSON.stringify(data),
  });
}

/** POST /api/llm/module/:name/execute 指定模块非流式 */
export function executeModule(name: string, data: LLMChatRequest) {
  return http.request<ApiEnvelope<LLMResponse>>("post", `/api/llm/module/${name}/execute`, { data });
}

/** POST /api/llm/module/:name/stream 指定模块流式 */
export function streamModule(name: string, data: LLMChatRequest): Promise<Response> {
  const token = getToken();
  const accessToken = typeof token === "string" ? token : token?.accessToken;

  return fetch(`/api/llm/module/${name}/stream`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${accessToken}`,
    },
    body: JSON.stringify(data),
  });
}
