package models

import (
	"context"
)

// Model 统一接口，所有适配器实现此接口
type Model interface {
	// Chat 非流式对话
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 流式对话
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
	// Embeddings 向量嵌入
	Embeddings(ctx context.Context, texts []string) ([]Embedding, error)
	// Name 模型名称
	Name() string
}

// ChatRequest 统一请求
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"` // 0-2
	MaxTokens   int       `json:"max_tokens"` // 最大 Token
	Stream      bool      `json:"stream"`      // 是否流式
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`    // system/user/assistant
	Content string `json:"content"`
}

// ChatResponse 非流式响应
type ChatResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	Usage        Usage  `json:"usage"`
	FinishReason string `json:"finish_reason"`
}

// StreamChunk 流式数据块
type StreamChunk struct {
	Delta    string `json:"delta"`    // 本次增量文本
	Done     bool   `json:"done"`     // 是否结束
	Usage    Usage  `json:"usage"`    // 仅最后一块有
	Error    error  `json:"-"`       // 错误（不序列化）
}

// Usage Token 用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Embedding 向量
type Embedding struct {
	Text   string    `json:"text"`
	Vector []float64 `json:"vector"`
}
