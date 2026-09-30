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

// EmbeddingRequest 向量嵌入请求
//
// 输入文本：单条或多条；服务端返回每条对应一个向量。
// Model 字段缺省时使用 Client 构造时的 config.ModelName。
// Dimensions（可选）：部分 provider（如 OpenAI text-embedding-3-*）支持输出维度裁剪。
//   - 不传 → 服务端默认维度
//   - 传值 → 必须 ≤ provider 支持的最大维度，且仅部分 provider 生效
// EncodingFormat（可选）：仅 OpenAI 系列支持 "float" / "base64"；其余 provider 忽略。
type EmbeddingRequest struct {
	Model          string   `json:"model,omitempty"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitempty"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	User           string   `json:"user,omitempty"` // 可选：关联用户标识，便于 provider 端做滥用检测
}

// Embedding 单条文本的向量结果
type Embedding struct {
	Index     int       `json:"index"`
	Object    string    `json:"object,omitempty"`
	Embedding []float64 `json:"embedding"`
}

// EmbeddingUsage 向量调用 token 用量（部分 provider 返回；Ollama 等本地模型返回 0）
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// EmbeddingResponse 向量调用响应
type EmbeddingResponse struct {
	Model string          `json:"model"`
	Data  []Embedding     `json:"data"`
	Usage *EmbeddingUsage `json:"usage,omitempty"`
}

// Client LLM HTTP 客户端接口
type Client interface {
	// Chat 发送聊天请求
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// ChatStream 发送流式聊天请求
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
	// Embeddings 发送向量嵌入请求
	//
	// 2026-09-30 对齐 DocCRM：替换 services/llm/models/llmclient.go 的 stub。
	// 当前实现：openai.go / ollama.go（provider 兼容 OpenAI Embeddings API）；
	//            anthropic.go / google.go 返回 ErrEmbeddingUnsupported（明确报错，非静默）。
	//            其余 provider 通过 OpenAI 兼容适配转发。
	Embeddings(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
	// TestConnection 测试连接
	TestConnection(ctx context.Context) (*TestResult, error)
	// Name 返回模型名称
	Name() string
}

// ErrEmbeddingUnsupported 标记 provider 不支持 Embeddings
//
// 使用方式：
//
//	if errors.Is(err, llmclient.ErrEmbeddingUnsupported) {
//	    // 引导用户切换 provider 或本地 Ollama
//	}
var ErrEmbeddingUnsupported = &APIError{
	Code:    -1,
	Message: "embedding not supported for this provider",
}

// DefaultTimeout 默认超时时间
const DefaultTimeout = 60 * time.Second

// DefaultTimeoutMs 默认超时毫秒
const DefaultTimeoutMs = 60000
