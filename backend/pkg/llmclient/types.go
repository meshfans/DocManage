package llmclient

import (
	"context"
	"time"
)

// Provider 服务商类型
type Provider string

const (
	ProviderOpenAI      Provider = "openai"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderQwen        Provider = "qwen"
	ProviderKimi        Provider = "kimi"
	ProviderDoubao      Provider = "doubao"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderAnthropic   Provider = "anthropic"
	ProviderZhipu       Provider = "zhipu"
	ProviderMiniMax     Provider = "minimax"
	ProviderGoogle      Provider = "google"
	ProviderOllama      Provider = "ollama"
	ProviderCustom      Provider = "custom"
)

// Protocol 协议类型
type Protocol string

const (
	ProtocolOpenAIChat        Protocol = "openai_chat"
	ProtocolAnthropic         Protocol = "anthropic_messages"
	ProtocolGoogleGenerative  Protocol = "google_generative"
	ProtocolOllamaChat        Protocol = "ollama_chat"
)

// Config LLM 客户端配置
type Config struct {
	Provider  Provider
	Protocol  Protocol
	ModelName string
	APIKey    string
	APIBase   string
	APIPath   string // API 路径，如 /chat/completions
	TimeoutMs int
	ProxyURL  string
}

// Message 聊天消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Image 图片输入（多模态）
type Image struct {
	Base64 string `json:"base64"`
	Mime   string `json:"mime"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Messages    []Message `json:"messages"`
	Images      []Image   `json:"images,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	System      string    `json:"system,omitempty"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Text  string `json:"text"`
	Usage *Usage `json:"usage,omitempty"`
}

// Usage Token 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
	Usage Usage  `json:"usage,omitempty"`
	Error error  `json:"-"`
}

// TestResult 连接测试结果
type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Message   string `json:"message,omitempty"`
}

// Client LLM HTTP 客户端接口
type Client interface {
	// Chat 发送聊天请求
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// ChatStream 发送流式聊天请求
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
	// TestConnection 测试连接
	TestConnection(ctx context.Context) (*TestResult, error)
	// Name 返回模型名称
	Name() string
}

// DefaultTimeout 默认超时时间
const DefaultTimeout = 60 * time.Second

// DefaultTimeoutMs 默认超时毫秒
const DefaultTimeoutMs = 60000
