package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AuthType 认证类型
type AuthType int

const (
	AuthTypeBearer AuthType = iota
	AuthTypeXAPIKey
	AuthTypeQueryParam
	AuthTypeNone
	AuthTypeInvalid // 无效值，用于检测是否设置过
)

// HTTPClient 统一 HTTP 客户端实现
type HTTPClient struct {
	config      *Config
	httpClient  *http.Client
	provider    Provider
	authType    AuthType
	defaultPath string
}

// NewClient 创建 LLM HTTP 客户端
func NewClient(cfg *Config) Client {
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = DefaultTimeoutMs
	}

	provider := cfg.Provider
	if provider == "" {
		provider = DetectProviderByBaseURL(cfg.APIBase)
	}

	preset := GetProviderPreset(provider)

	authType := cfg.AuthType()
	if authType == AuthTypeInvalid {
		authType = preset.AuthType
	}

	// 优先使用用户配置的 APIPath，否则使用预设默认值
	path := cfg.APIPath
	if path == "" {
		path = preset.DefaultPath
	}

	return &HTTPClient{
		config:      cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
		},
		provider:    provider,
		authType:    authType,
		defaultPath: path,
	}
}

// AuthType 获取配置的认证类型
func (c *Config) AuthType() AuthType {
	// 根据 Provider 明确指定
	if c.Provider != "" {
		return GetAuthType(c.Provider)
	}
	// 根据 Protocol 推断
	switch c.Protocol {
	case ProtocolOpenAIChat, ProtocolOllamaChat:
		return AuthTypeBearer
	case ProtocolAnthropic:
		// MiniMax Anthropic 兼容接口也使用 x-api-key（与原生 Anthropic 一致）
		if strings.Contains(strings.ToLower(c.APIBase), "minimax") {
			return AuthTypeXAPIKey
		}
		return AuthTypeXAPIKey
	case ProtocolGoogleGenerative:
		return AuthTypeQueryParam
	default:
		return AuthTypeBearer
	}
}

// Name 返回模型名称
func (c *HTTPClient) Name() string {
	return fmt.Sprintf("%s:%s", c.provider, c.config.ModelName)
}

// buildURL 构建请求 URL
func (c *HTTPClient) buildURL(path string) string {
	base := strings.TrimSuffix(c.config.APIBase, "/")
	if strings.HasPrefix(path, "http") {
		return path
	}
	return base + path
}

// buildURLWithDefaultPath 使用默认路径构建 URL
func (c *HTTPClient) buildURLWithDefaultPath() string {
	return c.buildURL(c.defaultPath)
}

// replaceModelPlaceholder 替换路径中的 :model 占位符
func replaceModelPlaceholder(path, modelName string) string {
	return strings.ReplaceAll(path, ":model", modelName)
}

// buildHeaders 构建请求头
func (c *HTTPClient) buildHeaders() map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	switch c.authType {
	case AuthTypeBearer:
		if c.config.APIKey != "" {
			headers["Authorization"] = "Bearer " + c.config.APIKey
		}
	case AuthTypeXAPIKey:
		if c.config.APIKey != "" {
			headers["x-api-key"] = c.config.APIKey
		}
		headers["anthropic-version"] = "2023-06-01"
	case AuthTypeQueryParam:
		// API Key 在 URL 中处理
	case AuthTypeNone:
		// 无需认证
	}

	return headers
}

// sendRequest 发送 HTTP 请求（返回响应体）
func (c *HTTPClient) sendRequest(ctx context.Context, method, url string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, err
	}

	for k, v := range c.buildHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

// sendStreamRequest 发送流式 HTTP 请求（返回响应体 reader）
func (c *HTTPClient) sendStreamRequest(ctx context.Context, method, url string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	for k, v := range c.buildHeaders() {
		req.Header.Set(k, v)
	}

	// 添加 API Key 到 URL（Google 等）- 直接修改 URL 字符串
	if c.authType == AuthTypeQueryParam && c.config.APIKey != "" && !strings.Contains(url, "key=") {
		if strings.Contains(url, "?") {
			url = url + "&key=" + c.config.APIKey
		} else {
			url = url + "?key=" + c.config.APIKey
		}
	}

	// 重新创建请求以使用新的 URL
	req, _ = http.NewRequestWithContext(ctx, method, url, reqBody)
	for k, v := range c.buildHeaders() {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// Chat 实现 Client 接口
func (c *HTTPClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	switch c.provider {
	case ProviderOpenAI, ProviderDeepSeek, ProviderQwen, ProviderKimi, ProviderDoubao, ProviderSiliconFlow, ProviderCustom:
		return c.chatOpenAI(ctx, req)
	case ProviderAnthropic:
		return c.chatAnthropic(ctx, req)
	case ProviderZhipu:
		return c.chatZhipu(ctx, req)
	case ProviderMiniMax:
		return c.chatMiniMax(ctx, req)
	case ProviderGoogle:
		return c.chatGoogle(ctx, req)
	case ProviderOllama:
		return c.chatOllama(ctx, req)
	default:
		return c.chatOpenAI(ctx, req)
	}
}

// ChatStream 实现 Client 接口
func (c *HTTPClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	switch c.provider {
	case ProviderOpenAI, ProviderDeepSeek, ProviderQwen, ProviderKimi, ProviderDoubao, ProviderSiliconFlow, ProviderCustom:
		return c.chatStreamOpenAI(ctx, req)
	case ProviderAnthropic:
		return c.chatStreamAnthropic(ctx, req)
	case ProviderZhipu:
		return c.chatStreamZhipu(ctx, req)
	case ProviderMiniMax:
		return c.chatStreamMiniMax(ctx, req)
	case ProviderGoogle:
		return c.chatStreamGoogle(ctx, req)
	case ProviderOllama:
		return c.chatStreamOllama(ctx, req)
	default:
		return c.chatStreamOpenAI(ctx, req)
	}
}

// TestConnection 实现 Client 接口
func (c *HTTPClient) TestConnection(ctx context.Context) (*TestResult, error) {
	t0 := time.Now()

	testReq := &ChatRequest{
		Messages:  []Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	}

	resp, err := c.Chat(ctx, testReq)
	latencyMs := int(time.Since(t0).Milliseconds())

	if err != nil {
		return &TestResult{
			OK:        false,
			LatencyMs: latencyMs,
			Message:   err.Error(),
		}, nil
	}

	return &TestResult{
		OK:        true,
		LatencyMs: latencyMs,
		Message:   resp.Text,
	}, nil
}
