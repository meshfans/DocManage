package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AnthropicAdapter Anthropic Messages 协议适配器
type AnthropicAdapter struct {
	cfg      *Config
	endpoint string
	headers  map[string]string
}

// newAnthropicAdapter 创建 Anthropic 适配器
func newAnthropicAdapter(cfg *Config) *AnthropicAdapter {
	base := strings.TrimSuffix(cfg.APIBase, "/")
	endpoint := base + "/v1/messages"

	headers := map[string]string{
		"Content-Type":        "application/json",
		"x-api-key":          cfg.APIKey,
		"anthropic-version":  "2023-06-01",
	}

	return &AnthropicAdapter{
		cfg:      cfg,
		endpoint: endpoint,
		headers:  headers,
	}
}

// buildAnthropicBody 构造 Anthropic 请求体
func (a *AnthropicAdapter) buildAnthropicBody(req *ChatRequest) map[string]interface{} {
	// 分离 system 消息
	var systemMsgs []string
	var otherMsgs []ChatMessage
	for _, m := range req.Messages {
		if m.Role == "system" && strings.TrimSpace(m.Content) != "" {
			systemMsgs = append(systemMsgs, strings.TrimSpace(m.Content))
		} else {
			otherMsgs = append(otherMsgs, m)
		}
	}

	messages := make([]map[string]interface{}, 0, len(otherMsgs))
	for _, m := range otherMsgs {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}

		// 多模态处理
		if len(req.Images) > 0 && m.Role == "user" {
			contentBlocks := make([]map[string]interface{}, 0)
			if m.Content != "" {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, img := range req.Images {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":      "base64",
						"media_type": img.Mime,
						"data":      img.Base64,
					},
				})
			}
			msg["content"] = contentBlocks
		}

		messages = append(messages, msg)
	}

	maxTokens := 2048
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	body := map[string]interface{}{
		"model":    a.cfg.ModelName,
		"messages": messages,
		"max_tokens": maxTokens,
	}

	if len(systemMsgs) > 0 {
		body["system"] = strings.Join(systemMsgs, "\n\n")
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}

	return body
}

// TestConnection 测试连接
func (a *AnthropicAdapter) TestConnection() (*TestResult, error) {
	t0 := time.Now()

	body := map[string]interface{}{
		"model":      a.cfg.ModelName,
		"max_tokens": 1,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "ping"},
		},
	}

	_, _, err := sendJSON(a.endpoint, "POST", a.headers, body, 30000)
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
		Message:   "连接成功",
	}, nil
}

// Chat 聊天
func (a *AnthropicAdapter) Chat(req *ChatRequest) (*ChatResponse, error) {
	body := a.buildAnthropicBody(req)

	respBody, _, err := sendJSON(a.endpoint, "POST", a.headers, body, req.TimeoutMs)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	var text string
	for _, c := range resp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	text = stripThinking(text)

	var usage *Usage
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		usage = &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
		}
	}

	return &ChatResponse{
		Text:  text,
		Usage: usage,
	}, nil
}
