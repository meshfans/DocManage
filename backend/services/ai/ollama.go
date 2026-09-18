package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OllamaAdapter Ollama 原生协议适配器
type OllamaAdapter struct {
	cfg      *Config
	endpoint string
	headers  map[string]string
}

// newOllamaAdapter 创建 Ollama 适配器
func newOllamaAdapter(cfg *Config) *OllamaAdapter {
	base := strings.TrimSuffix(cfg.APIBase, "/")
	endpoint := base + "/api/chat"

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return &OllamaAdapter{
		cfg:      cfg,
		endpoint: endpoint,
		headers:  headers,
	}
}

// buildOllamaBody 构造 Ollama 请求体
func (a *OllamaAdapter) buildOllamaBody(req *ChatRequest) map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}

		// Ollama 图片处理：使用 base64 字符串数组
		if len(req.Images) > 0 && m.Role == "user" {
			images := make([]string, 0, len(req.Images))
			for _, img := range req.Images {
				images = append(images, img.Base64)
			}
			msg["images"] = images
		}

		messages = append(messages, msg)
	}

	// options
	options := map[string]interface{}{}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		options["num_predict"] = *req.MaxTokens
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}

	body := map[string]interface{}{
		"model":    a.cfg.ModelName,
		"messages": messages,
		"stream":   false,
	}

	if len(options) > 0 {
		body["options"] = options
	}

	return body
}

// TestConnection 测试连接
func (a *OllamaAdapter) TestConnection() (*TestResult, error) {
	t0 := time.Now()

	body := map[string]interface{}{
		"model": a.cfg.ModelName,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "ping"},
		},
		"stream": false,
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
func (a *OllamaAdapter) Chat(req *ChatRequest) (*ChatResponse, error) {
	body := a.buildOllamaBody(req)

	respBody, _, err := sendJSON(a.endpoint, "POST", a.headers, body, req.TimeoutMs)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Message struct {
			Content  string `json:"content"`
			Thinking string `json:"thinking,omitempty"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count,omitempty"`
		EvalCount       int `json:"eval_count,omitempty"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 优先使用 content，fallback 到 thinking（推理模型）
	text := resp.Message.Content
	if text == "" {
		text = resp.Message.Thinking
	}

	text = stripThinking(text)

	var usage *Usage
	if resp.PromptEvalCount > 0 || resp.EvalCount > 0 {
		usage = &Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
		}
	}

	return &ChatResponse{
		Text:  text,
		Usage: usage,
	}, nil
}
