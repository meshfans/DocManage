package llmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// chatGoogle Google Generative AI 协议聊天
func (c *HTTPClient) chatGoogle(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	contents := buildGoogleContents(req)

	payload := map[string]any{
		"contents": contents,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["maxOutputTokens"] = req.MaxTokens
	}

	// Google 使用特殊路径格式，需要替换 :model 占位符
	url := replaceModelPlaceholder(c.buildURLWithDefaultPath(), c.config.ModelName)

	respBody, statusCode, err := c.sendRequest(ctx, "POST", url, payload)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, &APIError{Code: statusCode, Message: string(respBody)}
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}

	text := ""
	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			text += part.Text
		}
	}

	return &ChatResponse{
		Text: text,
		Usage: &Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

// chatStreamGoogle Google 流式聊天
func (c *HTTPClient) chatStreamGoogle(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	contents := buildGoogleContents(req)

	payload := map[string]any{
		"contents": contents,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["maxOutputTokens"] = req.MaxTokens
	}

	url := fmt.Sprintf("%s:streamGenerateContent?alt=sse",
		replaceModelPlaceholder(c.buildURLWithDefaultPath(), c.config.ModelName))

	resp, err := c.sendStreamRequest(ctx, "POST", url, payload)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{Code: resp.StatusCode, Message: string(bodyBytes)}
	}

	return parseGoogleStream(resp.Body), nil
}

// buildGoogleContents 构建 Google 格式的 contents
func buildGoogleContents(req *ChatRequest) []map[string]any {
	contents := make([]map[string]any, 0)

	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}

		parts := make([]map[string]any, 0)

		if m.Content != "" {
			parts = append(parts, map[string]any{"text": m.Content})
		}

		// Google 多模态
		for _, img := range req.Images {
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": img.Mime,
					"data":     img.Base64,
				},
			})
		}

		if len(parts) > 0 {
			role := "user"
			if m.Role == "model" || m.Role == "assistant" {
				role = "model"
			}
			contents = append(contents, map[string]any{
				"role":  role,
				"parts": parts,
			})
		}
	}

	return contents
}

// parseGoogleStream 解析 Google SSE 流
func parseGoogleStream(body io.Reader) <-chan StreamChunk {
	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		buf := make([]byte, 0)
		tmp := make([]byte, 1024)

		for {
			n, err := body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}

			lines := strings.Split(string(buf), "\n")
			for i := 0; i < len(lines)-1; i++ {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "data: ") {
					jsonStr := strings.TrimPrefix(line, "data: ")
					var chunk struct {
						Candidates []struct {
							Content struct {
								Parts []struct {
									Text string `json:"text"`
								} `json:"parts"`
							} `json:"content"`
						} `json:"candidates"`
					}
					if err := json.Unmarshal([]byte(jsonStr), &chunk); err == nil {
						for _, c := range chunk.Candidates {
							for _, p := range c.Content.Parts {
								if p.Text != "" {
									ch <- StreamChunk{Delta: p.Text}
								}
							}
						}
					}
				}
			}
			if len(lines) > 0 {
				buf = []byte(lines[len(lines)-1])
			}
		}

		ch <- StreamChunk{Done: true}
	}()

	return ch
}
