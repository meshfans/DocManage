/**
 * AI 服务商 / 模型预制清单
 */
export type ProviderKey =
  | "openai"
  | "anthropic"
  | "google"
  | "deepseek"
  | "zhipu"
  | "qwen"
  | "kimi"
  | "doubao"
  | "minimax"
  | "siliconflow"
  | "ollama"
  | "custom";

export type ProtocolKey =
  | "openai_chat"
  | "anthropic_messages"
  | "google_generative"
  | "ollama_chat";

export interface PresetProvider {
  value: ProviderKey;
  label: string;
  desc: string;
  defaultBase: string;
  defaultPath: string; // API 路径，如 /v1/messages 或 /chat/completions
  models: PresetModel[];
}

export interface PresetModel {
  key: string;
  displayName: string;
  protocol: ProtocolKey;
  inputTokens: number;
  outputTokens: number;
  badge?: "旗舰" | "主力" | "免费" | "极速" | "推理";
  recommended?: boolean;
  desc?: string;
  multimodal?: boolean; // 是否支持图片输入（vision）
}

// 1. DeepSeek
const deepseekProvider: PresetProvider = {
  value: "deepseek",
  label: "DeepSeek",
  desc: "DeepSeek V4 Pro / V4 Flash",
  defaultBase: "https://api.deepseek.com",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "deepseek-v4-pro",
      displayName: "DeepSeek V4 Pro",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 384_000,
      badge: "旗舰",
      recommended: true,
      desc: "1M 上下文，V4 Pro",
      multimodal: true
    },
    {
      key: "deepseek-v4-flash",
      displayName: "DeepSeek V4 Flash",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 384_000,
      badge: "极速",
      desc: "V4 Flash，轻量版",
      multimodal: true
    }
  ]
};

// 2. 智谱 GLM
const zhipuProvider: PresetProvider = {
  value: "zhipu",
  label: "智谱 GLM",
  desc: "GLM-4.7 Flash 免费 / GLM-5",
  defaultBase: "https://open.bigmodel.cn/api/anthropic",
  defaultPath: "/messages", // 注意：无 /v1 前缀
  models: [
    {
      key: "glm-4.7-flash",
      displayName: "GLM-4.7 Flash（免费）",
      protocol: "anthropic_messages",
      inputTokens: 200_000,
      outputTokens: 128_000,
      badge: "免费",
      recommended: true,
      desc: "官方免费模型",
      multimodal: false
    },
    {
      key: "glm-5",
      displayName: "GLM-5",
      protocol: "anthropic_messages",
      inputTokens: 200_000,
      outputTokens: 128_000,
      badge: "旗舰",
      desc: "GLM-5 旗舰",
      multimodal: true
    }
  ]
};

// 3. 通义千问 Qwen
const qwenProvider: PresetProvider = {
  value: "qwen",
  label: "通义千问 Qwen",
  desc: "Qwen3.7-Max / Qwen3.6 Flash",
  defaultBase: "https://dashscope.aliyuncs.com/compatible-mode/v1",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "qwen3.7-max",
      displayName: "Qwen 3.7 Max",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 65_536,
      badge: "旗舰",
      recommended: true,
      desc: "Qwen3.7-Max 旗舰",
      multimodal: true
    },
    {
      key: "qwen3.7-plus",
      displayName: "Qwen 3.7 Plus",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 65_536,
      badge: "主力",
      desc: "Qwen3.7 Plus",
      multimodal: true
    },
    {
      key: "qwen3.6-flash",
      displayName: "Qwen 3.6 Flash",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 32_768,
      badge: "极速",
      desc: "极速低价",
      multimodal: true
    }
  ]
};

// 4. 月之暗面 Kimi
const kimiProvider: PresetProvider = {
  value: "kimi",
  label: "月之暗面 Kimi",
  desc: "Kimi K2.6 / K2.5",
  defaultBase: "https://api.moonshot.cn/v1",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "kimi-k2.6",
      displayName: "Kimi K2.6",
      protocol: "openai_chat",
      inputTokens: 262_144,
      outputTokens: 262_144,
      badge: "旗舰",
      recommended: true,
      desc: "Kimi K2.6 旗舰",
      multimodal: true
    },
    {
      key: "kimi-k2.5",
      displayName: "Kimi K2.5",
      protocol: "openai_chat",
      inputTokens: 262_144,
      outputTokens: 262_144,
      badge: "主力",
      desc: "Kimi K2.5 稳定",
      multimodal: true
    }
  ]
};

// 5. 字节豆包
const doubaoProvider: PresetProvider = {
  value: "doubao",
  label: "字节豆包 Doubao",
  desc: "豆包 Seed 2.1 Pro",
  defaultBase: "https://ark.cn-beijing.volces.com/api/v3",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "doubao-seed-2-1-pro-260628",
      displayName: "豆包 Seed 2.1 Pro",
      protocol: "openai_chat",
      inputTokens: 256_000,
      outputTokens: 16_000,
      badge: "旗舰",
      recommended: true,
      desc: "豆包 Seed 2.1 Pro",
      multimodal: true
    },
    {
      key: "doubao-seed-2-0-lite",
      displayName: "豆包 Seed 2.0 Lite",
      protocol: "openai_chat",
      inputTokens: 256_000,
      outputTokens: 16_000,
      badge: "免费",
      desc: "¥0.0008/千 token",
      multimodal: true
    }
  ]
};

// 6. MiniMax
const minimaxProvider: PresetProvider = {
  value: "minimax",
  label: "MiniMax",
  desc: "M3 / M2.7",
  defaultBase: "https://api.minimaxi.com/anthropic",
  defaultPath: "/v1/messages", // MiniMax 使用 /v1/messages
  models: [
    {
      key: "MiniMax-M3",
      displayName: "MiniMax M3",
      protocol: "anthropic_messages",
      inputTokens: 256_000,
      outputTokens: 131_000,
      badge: "旗舰",
      recommended: true,
      desc: "M3 最新旗舰",
      multimodal: true
    },
    {
      key: "MiniMax-M2.7",
      displayName: "MiniMax M2.7",
      protocol: "anthropic_messages",
      inputTokens: 204_800,
      outputTokens: 131_000,
      badge: "主力",
      desc: "M2.7 主力",
      multimodal: false
    }
  ]
};

// 7. 硅基流动
const siliconflowProvider: PresetProvider = {
  value: "siliconflow",
  label: "硅基流动 SiliconFlow",
  desc: "国内开源模型低价",
  defaultBase: "https://api.siliconflow.cn/v1",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "Qwen/Qwen3-235B-A22B-Instruct",
      displayName: "Qwen3-235B-A22B（开源）",
      protocol: "openai_chat",
      inputTokens: 32_000,
      outputTokens: 8_000,
      badge: "免费",
      recommended: true,
      desc: "开源 SOTA MoE",
      multimodal: false
    },
    {
      key: "deepseek-ai/DeepSeek-V4",
      displayName: "DeepSeek-V4（开源）",
      protocol: "openai_chat",
      inputTokens: 128_000,
      outputTokens: 32_000,
      badge: "主力",
      desc: "V4 开源版",
      multimodal: true
    }
  ]
};

// 8. OpenAI
const openaiProvider: PresetProvider = {
  value: "openai",
  label: "OpenAI",
  desc: "GPT-5.5 / GPT-4o",
  defaultBase: "https://api.openai.com/v1",
  defaultPath: "/chat/completions",
  models: [
    {
      key: "gpt-5.5",
      displayName: "GPT-5.5",
      protocol: "openai_chat",
      inputTokens: 1_000_000,
      outputTokens: 128_000,
      badge: "旗舰",
      recommended: true,
      desc: "GPT-5.5 旗舰",
      multimodal: true
    },
    {
      key: "gpt-4o",
      displayName: "GPT-4o",
      protocol: "openai_chat",
      inputTokens: 128_000,
      outputTokens: 16_384,
      badge: "主力",
      desc: "GPT-4o 均衡",
      multimodal: true
    }
  ]
};

// 9. Anthropic Claude
const anthropicProvider: PresetProvider = {
  value: "anthropic",
  label: "Anthropic Claude",
  desc: "Claude Opus 4.8 / Sonnet 5",
  defaultBase: "https://api.anthropic.com",
  defaultPath: "/v1/messages",
  models: [
    {
      key: "claude-opus-4-8",
      displayName: "Claude Opus 4.8",
      protocol: "anthropic_messages",
      inputTokens: 1_000_000,
      outputTokens: 128_000,
      badge: "旗舰",
      recommended: true,
      desc: "Claude Opus 4.8 旗舰",
      multimodal: true
    },
    {
      key: "claude-sonnet-5",
      displayName: "Claude Sonnet 5",
      protocol: "anthropic_messages",
      inputTokens: 1_000_000,
      outputTokens: 128_000,
      badge: "旗舰",
      desc: "Claude Sonnet 5",
      multimodal: true
    }
  ]
};

// 10. Google Gemini
const googleProvider: PresetProvider = {
  value: "google",
  label: "Google Gemini",
  desc: "Gemini 2.5 Pro / Flash",
  defaultBase: "https://generativelanguage.googleapis.com",
  defaultPath: "/v1beta/models/:model:generateContent", // Google 特殊路径格式
  models: [
    {
      key: "gemini-2.5-pro",
      displayName: "Gemini 2.5 Pro",
      protocol: "google_generative",
      inputTokens: 1_000_000,
      outputTokens: 64_000,
      badge: "旗舰",
      recommended: true,
      desc: "Gemini 2.5 Pro",
      multimodal: true
    },
    {
      key: "gemini-2.5-flash",
      displayName: "Gemini 2.5 Flash",
      protocol: "google_generative",
      inputTokens: 1_000_000,
      outputTokens: 65_000,
      badge: "极速",
      desc: "Gemini 2.5 Flash 极速",
      multimodal: true
    }
  ]
};

// 11. Ollama
const ollamaProvider: PresetProvider = {
  value: "ollama",
  label: "Ollama（本地推理）",
  desc: "本地推理，免鉴权",
  defaultBase: "http://127.0.0.1:11434",
  defaultPath: "/api/chat",
  models: [
    {
      key: "qwen3:32b",
      displayName: "Qwen3 32B",
      protocol: "ollama_chat",
      inputTokens: 32_000,
      outputTokens: 8_000,
      badge: "主力",
      recommended: true,
      desc: "Qwen3 32B 多语言",
      multimodal: false
    },
    {
      key: "qwen3-coder:30b",
      displayName: "Qwen3 Coder 30B",
      protocol: "ollama_chat",
      inputTokens: 262_000,
      outputTokens: 32_000,
      badge: "推理",
      desc: "Qwen3 Coder 编程",
      multimodal: false
    }
  ]
};

// 12. 自定义
const customProvider: PresetProvider = {
  value: "custom",
  label: "自定义（OpenAI 兼容）",
  desc: "兼容 OpenAI 协议的任意第三方网关",
  defaultBase: "",
  defaultPath: "/chat/completions",
  models: []
};

// 主清单
export const PRESET_PROVIDERS: PresetProvider[] = [
  deepseekProvider,
  zhipuProvider,
  qwenProvider,
  kimiProvider,
  doubaoProvider,
  minimaxProvider,
  siliconflowProvider,
  openaiProvider,
  anthropicProvider,
  googleProvider,
  ollamaProvider,
  customProvider
];

export const PROTOCOL_LABEL: Record<ProtocolKey, string> = {
  openai_chat: "OpenAI 兼容",
  anthropic_messages: "Anthropic 原生",
  google_generative: "Google Generative AI",
  ollama_chat: "Ollama"
};
