package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Protocol 协议类型
type Protocol string

const (
	ProtocolOpenAIChat     Protocol = "openai_chat"
	ProtocolOpenAIResponses Protocol = "openai_responses"
	ProtocolAnthropic     Protocol = "anthropic_messages"
	ProtocolGoogle        Protocol = "google_generative"
	ProtocolOllama        Protocol = "ollama_chat"
)

// Provider 服务商类型
type Provider string

const (
	ProviderOpenAI     Provider = "openai"
	ProviderAnthropic  Provider = "anthropic"
	ProviderGoogle     Provider = "google"
	ProviderOllama     Provider = "ollama"
	ProviderDeepSeek   Provider = "deepseek"
	ProviderZhipu      Provider = "zhipu"
	ProviderQwen       Provider = "qwen"
	ProviderKimi       Provider = "kimi"
	ProviderDoubao     Provider = "doubao"
	ProviderMiniMax    Provider = "minimax"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderCustom     Provider = "custom"
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
	Messages   []ChatMessage `json:"messages"`
	Images     []ChatImage   `json:"images,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens  *int         `json:"max_tokens,omitempty"`
	TopP       *float64     `json:"top_p,omitempty"`
	Stream     bool         `json:"stream,omitempty"`
	OnChunk    func(delta, full string) `json:"-"`
	TimeoutMs  int          `json:"timeout_ms,omitempty"`
	Signal     *chan struct{} `json:"-"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Text   string `json:"text"`
	Usage  *Usage `json:"usage,omitempty"`
}

// Usage Token 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// TestResult 连接测试结果
type TestResult struct {
	OK       bool    `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Message  string  `json:"message,omitempty"`
}

// MultimodalResult 多模态检测结果
//   - Supported: nil=未检测/不确定, true=支持, false=不支持
type MultimodalResult struct {
	Supported *bool  `json:"supported"`
	LatencyMs int    `json:"latencyMs"`
	Message   string `json:"message,omitempty"`
}

// FullTestResult 完整测试结果（含多模态）
type FullTestResult struct {
	OK        bool               `json:"ok"`
	LatencyMs int                `json:"latencyMs"`
	Message   string             `json:"message"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
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
	ProxyURL      string
	DefaultParams map[string]interface{}
}

// ProviderAdapter AI 协议适配器接口
type ProviderAdapter interface {
	TestConnection() (*TestResult, error)
	Chat(req *ChatRequest) (*ChatResponse, error)
}

// buildAdapter 根据协议选择适配器
func BuildAdapter(cfg *Config) ProviderAdapter {
	proto := Protocol(cfg.Protocol)

	switch proto {
	case ProtocolAnthropic:
		return newAnthropicAdapter(cfg)
	case ProtocolGoogle:
		return newGoogleAdapter(cfg)
	case ProtocolOllama:
		return newOllamaAdapter(cfg)
	case ProtocolOpenAIChat, ProtocolOpenAIResponses:
		return newOpenAICompatAdapter(cfg)
	default:
		// 默认走 OpenAI 兼容
		return newOpenAICompatAdapter(cfg)
	}
}

// sendJSON 发送 JSON 请求
func sendJSON(endpoint string, method string, headers map[string]string, body interface{}, timeoutMs int) ([]byte, int, error) {
	if timeoutMs <= 0 {
		timeoutMs = 180000 // 默认 3 分钟
	}

	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("JSON 序列化失败: %w", err)
	}

	req, err := http.NewRequest(method, endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bodyBytes, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return bodyBytes, resp.StatusCode, nil
}

// formatErrorPayload 解析错误响应
func formatErrorPayload(body []byte, fallback string) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		ErrorMsg string `json:"error_message"`
		Message  string `json:"message"`
	}

	if err := json.Unmarshal(body, &errResp); err != nil {
		return fallback
	}

	if errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	if errResp.ErrorMsg != "" {
		return errResp.ErrorMsg
	}
	if errResp.Message != "" {
		return errResp.Message
	}
	return fallback
}

// stripThinking 剥离推理模型的思考内容
func stripThinking(text string) string {
	// 移除 <think>...</think> 块
	for {
		start := strings.Index(text, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</think>")
		if end == -1 {
			break
		}
		text = text[:start] + text[end+len("</think>"):]
	}

	// 移除 <think>...</think> 块
	for {
		start := strings.Index(text, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</think>")
		if end == -1 {
			break
		}
		text = text[:start] + text[end+len("</think>"):]
	}

	return strings.TrimSpace(text)
}
