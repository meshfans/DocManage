package llmclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// chatAnthropic Anthropic 官方协议聊天
func (c *HTTPClient) chatAnthropic(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		payload["top_p"] = req.TopP
	}

	respBody, statusCode, err := c.sendRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, &APIError{Code: statusCode, Message: string(respBody)}
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
		return nil, err
	}

	text := ""
	for _, c := range resp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	text = stripThinking(text)

	return &ChatResponse{
		Text: text,
		Usage: &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

// chatZhipu 智谱 GLM 协议聊天
func (c *HTTPClient) chatZhipu(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	respBody, statusCode, err := c.sendRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, &APIError{Code: statusCode, Message: string(respBody)}
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
		return nil, err
	}

	text := ""
	for _, c := range resp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	text = stripThinking(text)

	return &ChatResponse{
		Text: text,
		Usage: &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

// chatMiniMax MiniMax 协议聊天（使用 Bearer Auth + /v1/messages）
func (c *HTTPClient) chatMiniMax(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	respBody, statusCode, err := c.sendRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, &APIError{Code: statusCode, Message: string(respBody)}
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
		return nil, err
	}

	text := ""
	for _, c := range resp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	text = stripThinking(text)

	return &ChatResponse{
		Text: text,
		Usage: &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

// extractMessages 提取非 system 消息
func extractMessages(req *ChatRequest) []map[string]any {
	messages := make([]map[string]any, 0)
	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}

		msg := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}

		// Anthropic 多模态
		if len(req.Images) > 0 && m.Role == "user" {
			contentBlocks := make([]map[string]any, 0)
			// 图片在前，文本在后（MiniMax Anthropic 兼容接口要求）
			for _, img := range req.Images {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": img.Mime,
						"data":       img.Base64,
					},
				})
			}
			if m.Content != "" {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			msg["content"] = contentBlocks
		}

		messages = append(messages, msg)
	}
	return messages
}

// chatStreamAnthropic Anthropic 流式聊天
func (c *HTTPClient) chatStreamAnthropic(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	resp, err := c.sendStreamRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{Code: resp.StatusCode, Message: string(bodyBytes)}
	}

	return parseAnthropicStream(resp.Body), nil
}

// chatStreamZhipu 智谱流式聊天
func (c *HTTPClient) chatStreamZhipu(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	resp, err := c.sendStreamRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{Code: resp.StatusCode, Message: string(bodyBytes)}
	}

	return parseAnthropicStream(resp.Body), nil
}

// chatStreamMiniMax MiniMax 流式聊天
func (c *HTTPClient) chatStreamMiniMax(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	messages := extractMessages(req)

	payload := map[string]any{
		"model":      c.config.ModelName,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}

	if req.System != "" {
		payload["system"] = req.System
	} else if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	resp, err := c.sendStreamRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{Code: resp.StatusCode, Message: string(bodyBytes)}
	}

	return parseAnthropicStream(resp.Body), nil
}

// parseAnthropicStream 解析 Anthropic SSE 流
func parseAnthropicStream(body io.Reader) <-chan StreamChunk {
	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		reader := bufio.NewReader(body)

		// 2026-09-30 对齐 DocCRM 修复：Anthropic 协议的 input_tokens 只在
		// message_start（message.usage.input_tokens）下发，message_delta 只带
		// output_tokens。原实现只读 message_delta，导致 prompt token 恒为 0。
		inputTokens := 0

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					ch <- StreamChunk{Error: err}
				}
				return
			}

			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			// 尝试解析为通用事件
			var generic struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(data), &generic); err != nil {
				continue
			}

			// 根据事件类型处理
			switch generic.Type {
			case "content_block_delta":
				var event struct {
					Type  string `json:"type"`
					Index int    `json:"index"`
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(data), &event); err == nil {
					if event.Delta.Text != "" {
						ch <- StreamChunk{Delta: event.Delta.Text}
					}
				}
			case "text_delta":
				// MiniMax 可能使用 text_delta
				var event struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Index int    `json:"index"`
				}
				if err := json.Unmarshal([]byte(data), &event); err == nil {
					if event.Text != "" {
						ch <- StreamChunk{Delta: event.Text}
					}
				}
			case "message_start":
				// prompt token 只在这里出现
				var startEvent struct {
					Message struct {
						Usage struct {
							InputTokens int `json:"input_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &startEvent); err == nil {
					inputTokens = startEvent.Message.Usage.InputTokens
				}
			case "message_delta":
				var event struct {
					Type  string `json:"type"`
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(data), &event); err == nil {
					// message_delta 只带 output_tokens，input_tokens 取
					// message_start 的缓存值（部分网关两处都带）
					in := event.Usage.InputTokens
					if in == 0 {
						in = inputTokens
					} else {
						inputTokens = in
					}
					ch <- StreamChunk{
						Done: true,
						Usage: Usage{
							PromptTokens:     in,
							CompletionTokens: event.Usage.OutputTokens,
							TotalTokens:      in + event.Usage.OutputTokens,
						},
					}
					return
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
				return
			case "ping":
				// 忽略 ping 事件
				continue
			}
		}
	}()

	return ch
}

// embeddingAnthropic Anthropic 原生协议向量嵌入
//
// 2026-09-30 对齐 DocCRM 移植。Anthropic 未公开 embeddings endpoint，
// 返回 ErrEmbeddingUnsupported 让上游明确报错，避免静默失败。
func (c *HTTPClient) embeddingAnthropic(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	return nil, ErrEmbeddingUnsupported
}
