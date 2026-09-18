package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OpenAICompatAdapter OpenAI 兼容协议适配器
type OpenAICompatAdapter struct {
	cfg       *Config
	endpoint  string
	headers   map[string]string
}

// newOpenAICompatAdapter 创建 OpenAI 兼容适配器
func newOpenAICompatAdapter(cfg *Config) *OpenAICompatAdapter {
	base := strings.TrimSuffix(cfg.APIBase, "/")
	endpoint := base + "/chat/completions"

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}

	return &OpenAICompatAdapter{
		cfg:      cfg,
		endpoint: endpoint,
		headers:  headers,
	}
}

// buildOpenAIBody 构造 OpenAI 兼容请求体
func (a *OpenAICompatAdapter) buildOpenAIBody(req *ChatRequest) map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}

		// 多模态处理
		if len(req.Images) > 0 && m.Role == "user" {
			contentParts := make([]map[string]interface{}, 0)
			if m.Content != "" {
				contentParts = append(contentParts, map[string]interface{}{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, img := range req.Images {
				contentParts = append(contentParts, map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url":    fmt.Sprintf("data:%s;base64,%s", img.Mime, img.Base64),
						"detail": "low",
					},
				})
			}
			msg["content"] = contentParts
		}

		messages = append(messages, msg)
	}

	body := map[string]interface{}{
		"model":    a.cfg.ModelName,
		"messages": messages,
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}

	// 合并默认参数
	for k, v := range a.cfg.DefaultParams {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}

	return body
}

// TestConnection 测试连接
func (a *OpenAICompatAdapter) TestConnection() (*TestResult, error) {
	t0 := time.Now()

	body := map[string]interface{}{
		"model": a.cfg.ModelName,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
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
func (a *OpenAICompatAdapter) Chat(req *ChatRequest) (*ChatResponse, error) {
	body := a.buildOpenAIBody(req)

	respBody, _, err := sendJSON(a.endpoint, "POST", a.headers, body, req.TimeoutMs)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content         string `json:"content"`
				Reasoning       string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("响应为空")
	}

	msg := resp.Choices[0].Message
	text := msg.Content

	// 兼容推理模型的 reasoning 字段
	if text == "" {
		text = msg.ReasoningContent
	}
	if text == "" {
		text = msg.Reasoning
	}

	text = stripThinking(text)

	var usage *Usage
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		usage = &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}
	}

	return &ChatResponse{
		Text:  text,
		Usage: usage,
	}, nil
}
