package llmclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// chatOpenAI OpenAI 兼容协议聊天
func (c *HTTPClient) chatOpenAI(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	payload := map[string]any{
		"model":    c.config.ModelName,
		"messages": convertMessages(req),
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
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
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, &APIError{Code: statusCode, Message: "response empty"}
	}

	msg := resp.Choices[0].Message
	text := msg.Content
	if text == "" {
		text = msg.ReasoningContent
	}
	if text == "" {
		text = msg.Reasoning
	}
	text = stripThinking(text)

	return &ChatResponse{
		Text: text,
		Usage: &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// chatStreamOpenAI OpenAI 兼容协议流式聊天
func (c *HTTPClient) chatStreamOpenAI(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	payload := map[string]any{
		"model":    c.config.ModelName,
		"messages": convertMessages(req),
		"stream":   true,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
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

	return parseOpenAIStream(resp.Body), nil
}

// parseOpenAIStream 解析 OpenAI SSE 流
func parseOpenAIStream(body io.Reader) <-chan StreamChunk {
	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		reader := bufio.NewReader(body)

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
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}

			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				ch <- StreamChunk{Delta: chunk.Choices[0].Delta.Content}
			}

			if chunk.Usage.TotalTokens > 0 {
				ch <- StreamChunk{
					Done: true,
					Usage: Usage{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					},
				}
			}
		}
	}()

	return ch
}

// convertMessages 转换消息格式（处理多模态）
func convertMessages(req *ChatRequest) []map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))

	for _, m := range req.Messages {
		// 处理 system 消息
		if m.Role == "system" && req.System != "" {
			continue // system 在 anthropic 中单独处理
		}

		msg := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}

		// OpenAI 兼容多模态
		if len(req.Images) > 0 && m.Role == "user" && m.Content != "" {
			contentParts := make([]map[string]any, 0)
			contentParts = append(contentParts, map[string]any{
				"type": "text",
				"text": m.Content,
			})
			for _, img := range req.Images {
				contentParts = append(contentParts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url":    "data:" + img.Mime + ";base64," + img.Base64,
						"detail": "low",
					},
				})
			}
			msg["content"] = contentParts
		}

		messages = append(messages, msg)
	}

	return messages
}
