package llmclient

import (
	"strings"
)

// DetectProviderByBaseURL 根据 baseURL 检测服务商
func DetectProviderByBaseURL(baseURL string) Provider {
	baseURL = strings.ToLower(baseURL)
	switch {
	case strings.Contains(baseURL, "minimax"):
		return ProviderMiniMax
	case strings.Contains(baseURL, "deepseek"):
		return ProviderDeepSeek
	case strings.Contains(baseURL, "zhipu") || strings.Contains(baseURL, "bigmodel"):
		return ProviderZhipu
	case strings.Contains(baseURL, "qwen") || strings.Contains(baseURL, "dashscope") || strings.Contains(baseURL, "aliyuncs"):
		return ProviderQwen
	case strings.Contains(baseURL, "moonshot") || strings.Contains(baseURL, "kimi"):
		return ProviderKimi
	case strings.Contains(baseURL, "doubao") || strings.Contains(baseURL, "volces"):
		return ProviderDoubao
	case strings.Contains(baseURL, "siliconflow"):
		return ProviderSiliconFlow
	case strings.Contains(baseURL, "openai"):
		return ProviderOpenAI
	case strings.Contains(baseURL, "anthropic"):
		return ProviderAnthropic
	case strings.Contains(baseURL, "generativelanguage") || strings.Contains(baseURL, "aiplatform"):
		return ProviderGoogle
	case strings.Contains(baseURL, "ollama") || strings.Contains(baseURL, "127.0.0.1") || strings.Contains(baseURL, "localhost:11434"):
		return ProviderOllama
	default:
		return ProviderCustom
	}
}

// GetAuthType 获取认证类型
func GetAuthType(provider Provider) AuthType {
	switch provider {
	case ProviderOpenAI, ProviderDeepSeek, ProviderQwen, ProviderKimi, ProviderDoubao, ProviderSiliconFlow:
		return AuthTypeBearer
	case ProviderMiniMax, ProviderAnthropic, ProviderZhipu:
		return AuthTypeXAPIKey
	case ProviderGoogle:
		return AuthTypeQueryParam
	case ProviderOllama:
		return AuthTypeNone
	default:
		return AuthTypeBearer
	}
}

// ProtocolToAuthType 获取协议的默认认证类型
func ProtocolToAuthType(protocol Protocol) AuthType {
	switch protocol {
	case ProtocolOpenAIChat, ProtocolOllamaChat:
		return AuthTypeBearer
	case ProtocolAnthropic:
		return AuthTypeXAPIKey
	case ProtocolGoogleGenerative:
		return AuthTypeQueryParam
	default:
		return AuthTypeBearer
	}
}

// DetectProviderByProtocol 根据协议推断认证类型
func DetectProviderByProtocol(protocol Protocol) AuthType {
	return ProtocolToAuthType(protocol)
}

// GetDefaultPath 获取 provider 的默认 API 路径
func GetDefaultPath(provider Provider) string {
	switch provider {
	case ProviderOpenAI, ProviderDeepSeek, ProviderQwen, ProviderKimi, ProviderDoubao, ProviderSiliconFlow, ProviderCustom:
		return "/chat/completions"
	case ProviderAnthropic, ProviderMiniMax:
		return "/v1/messages"
	case ProviderZhipu:
		return "/messages" // 注意：无 /v1 前缀
	case ProviderGoogle:
		return "/v1beta/models/:model:generateContent"
	case ProviderOllama:
		return "/api/chat"
	default:
		return "/chat/completions"
	}
}

// ProviderPreset 预置服务商配置
type ProviderPreset struct {
	AuthType    AuthType
	DefaultPath string
}

// GetProviderPreset 获取 provider 的预置配置
func GetProviderPreset(provider Provider) *ProviderPreset {
	preset := &ProviderPreset{
		AuthType:    GetAuthType(provider),
		DefaultPath: GetDefaultPath(provider),
	}
	return preset
}
