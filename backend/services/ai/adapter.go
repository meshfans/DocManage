package ai

import (
	"context"
	"time"

	"doc/pkg/llmclient"
)

// Protocol 协议类型
type Protocol string

const (
	ProtocolOpenAIChat Protocol = "openai_chat"
	ProtocolAnthropic  Protocol = "anthropic_messages"
	ProtocolGoogle     Protocol = "google_generative"
	ProtocolOllama     Protocol = "ollama_chat"
)

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatImage 图片输入
type ChatImage struct {
	Base64 string `json:"base64"`
	Mime   string `json:"mime"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Messages    []ChatMessage `json:"messages"`
	Images     []ChatImage   `json:"images,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens  *int         `json:"max_tokens,omitempty"`
	TopP       *float64     `json:"top_p,omitempty"`
	Stream     bool          `json:"stream,omitempty"`
	OnChunk    func(delta, full string) `json:"-"`
	TimeoutMs  int           `json:"timeout_ms,omitempty"`
	Signal     *chan struct{} `json:"-"`
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
}

// TestResult 连接测试结果
type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Message   string `json:"message,omitempty"`
}

// MultimodalResult 多模态检测结果
type MultimodalResult struct {
	Supported *bool `json:"supported"`
	LatencyMs int   `json:"latencyMs"`
	Message   string `json:"message,omitempty"`
}

// FullTestResult 完整测试结果（含多模态）
type FullTestResult struct {
	OK         bool              `json:"ok"`
	LatencyMs  int              `json:"latencyMs"`
	Message    string           `json:"message"`
	Provider   string           `json:"provider,omitempty"`
	Model      string           `json:"model,omitempty"`
	Multimodal *MultimodalResult `json:"multimodal,omitempty"`
}

// Config AI 配置
type Config struct {
	ID            int64
	Provider      string
	Protocol      string
	ModelName     string
	APIKey        string
	APIBase       string
	APIPath       string // API 路径，如 /chat/completions
	ProxyURL      string
	DefaultParams map[string]interface{}
}

// ProviderAdapter AI Provider 适配器接口
type ProviderAdapter interface {
	TestConnection() (*TestResult, error)
	Chat(req *ChatRequest) (*ChatResponse, error)
}

// BuildAdapter 根据协议类型构建适配器（使用统一 llmclient）
func BuildAdapter(cfg *Config) ProviderAdapter {
	// 检测 Provider
	provider := llmclient.Provider(cfg.Provider)
	if provider == "" {
		provider = llmclient.DetectProviderByBaseURL(cfg.APIBase)
	}

	llmCfg := &llmclient.Config{
		Provider:  provider,
		Protocol:  llmclient.Protocol(cfg.Protocol),
		ModelName: cfg.ModelName,
		APIKey:    cfg.APIKey,
		APIBase:   cfg.APIBase,
		APIPath:   cfg.APIPath,
		ProxyURL:  cfg.ProxyURL,
		TimeoutMs: 60000,
	}

	return &LLMClientAdapter{
		client:    llmclient.NewClient(llmCfg),
		provider:  string(provider),
		modelName: cfg.ModelName,
	}
}

// LLMClientAdapter 基于 llmclient 的适配器
type LLMClientAdapter struct {
	client    llmclient.Client
	provider  string
	modelName string
}

// Provider 返回 provider 名称
func (a *LLMClientAdapter) Provider() string {
	return a.provider
}

// ModelName 返回模型名称
func (a *LLMClientAdapter) ModelName() string {
	return a.modelName
}

// TestConnection 测试连接
func (a *LLMClientAdapter) TestConnection() (*TestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := a.client.TestConnection(ctx)
	if err != nil {
		return &TestResult{
			OK:        false,
			LatencyMs: 0,
			Message:   err.Error(),
		}, nil
	}

	return &TestResult{
		OK:        result.OK,
		LatencyMs: result.LatencyMs,
		Message:   result.Message,
	}, nil
}

// Chat 聊天
func (a *LLMClientAdapter) Chat(req *ChatRequest) (*ChatResponse, error) {
	ctx := context.Background()
	if req.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	// 转换请求
	messages := make([]llmclient.Message, 0)
	for _, m := range req.Messages {
		messages = append(messages, llmclient.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	images := make([]llmclient.Image, 0)
	for _, img := range req.Images {
		images = append(images, llmclient.Image{
			Base64: img.Base64,
			Mime:   img.Mime,
		})
	}

	llmReq := &llmclient.ChatRequest{
		Messages: messages,
		Images:   images,
	}

	if req.Temperature != nil {
		llmReq.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		llmReq.MaxTokens = *req.MaxTokens
	}
	if req.TopP != nil {
		llmReq.TopP = *req.TopP
	}

	resp, err := a.client.Chat(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	var usage *Usage
	if resp.Usage != nil {
		usage = &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}
	}

	return &ChatResponse{
		Text:  resp.Text,
		Usage: usage,
	}, nil
}

// ptrInt 创建 int 指针
func ptrInt(v int) *int {
	return &v
}

// ptrFloat64 创建 float64 指针
func ptrFloat64(v float64) *float64 {
	return &v
}

// sleepShort 短暂等待
func sleepShort(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
