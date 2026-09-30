package models

import "fmt"

// ModelConfig 模型配置（避免循环引用）
//
// APIPath / ProxyURL 必须完整透传到 llmclient.Config，否则用户在 AI 配置页
// 填写的自定义路径与代理会在运行时被静默丢弃（表现为「测试通过但实际对话打错 URL」）。
type ModelConfig struct {
	Provider  string
	Protocol  string
	ModelName string
	APIKey    string
	APIBase   string
	APIPath   string
	ProxyURL  string
}

// NewModel 根据配置自动选择适配器（使用统一 llmclient）
func NewModel(cfg *ModelConfig) (Model, error) {
	return NewLLMClientModel(cfg), nil
}

// ProviderToProtocol 常见 provider 到 protocol 的映射
var ProviderToProtocol = map[string]string{
	"openai":      "openai_chat",
	"qwen":        "openai_chat",
	"doubao":      "openai_chat",
	"kimi":        "openai_chat",
	"deepseek":    "openai_chat",
	"zhipu":       "openai_chat",
	"siliconflow": "openai_chat",
	"anthropic":   "anthropic_messages",
	"google":      "google_generative",
	"ollama":      "ollama_chat",
	"azure":       "openai_chat", // Azure 也用 OpenAI 兼容协议
}

// GetDefaultProtocol 根据 provider 获取默认 protocol
func GetDefaultProtocol(provider string) string {
	if protocol, ok := ProviderToProtocol[provider]; ok {
		return protocol
	}
	return "openai_chat"
}

// GetModel 获取默认模型的适配器实例
func GetModel(cfg *ModelConfig) (Model, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	return NewModel(cfg)
}
