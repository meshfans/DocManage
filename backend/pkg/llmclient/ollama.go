package llmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// chatOllama Ollama 本地推理协议聊天
func (c *HTTPClient) chatOllama(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}

		// Ollama 多模态
		if len(req.Images) > 0 && m.Role == "user" && m.Content != "" {
			images := make([]string, 0, len(req.Images))
			for _, img := range req.Images {
				images = append(images, "data:"+img.Mime+";base64,"+img.Base64)
			}
			msg["images"] = images
		}

		messages = append(messages, msg)
	}

	payload := map[string]any{
		"model":    c.config.ModelName,
		"messages": messages,
		"stream":   false,
		"think":    false, // 关闭 thinking（Qwen3 等模型默认开启）
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["options"] = map[string]any{"num_predict": req.MaxTokens}
	}

	respBody, statusCode, err := c.sendRequest(ctx, "POST", c.buildURLWithDefaultPath(), payload)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, &APIError{Code: statusCode, Message: string(respBody)}
	}

	var resp struct {
		Message struct {
			Content       string `json:"content"`
		} `json:"message"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}

	return &ChatResponse{
		Text: resp.Message.Content,
		Usage: &Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}, nil
}

// chatStreamOllama Ollama 流式聊天
func (c *HTTPClient) chatStreamOllama(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	payload := map[string]any{
		"model":    c.config.ModelName,
		"messages": messages,
		"stream":   true,
		"think":    false, // 关闭 thinking（Qwen3 等模型默认开启）
	}

	if req.Temperature > 0 {
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

	return parseOllamaStream(resp.Body), nil
}

// parseOllamaStream 解析 Ollama NDJSON 流
func parseOllamaStream(body io.Reader) <-chan StreamChunk {
	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		decoder := json.NewDecoder(body)

		for {
			var resp struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done            bool `json:"done"`
				PromptEvalCount int  `json:"prompt_eval_count"`
				EvalCount       int  `json:"eval_count"`
			}

			if err := decoder.Decode(&resp); err != nil {
				if err != io.EOF {
					ch <- StreamChunk{Error: err}
				}
				return
			}

			if resp.Message.Content != "" {
				ch <- StreamChunk{Delta: resp.Message.Content}
			}

			if resp.Done {
				ch <- StreamChunk{
					Done: true,
					Usage: Usage{
						PromptTokens:     resp.PromptEvalCount,
						CompletionTokens: resp.EvalCount,
						TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
					},
				}
				return
			}
		}
	}()

	return ch
}

// embeddingOllama Ollama 向量嵌入
//
// 2026-09-30 对齐 DocCRM 移植。
// 入参：单条 prompt → 单条 embedding
//   - 若 req.Input 包含多条 → 循环调用 Ollama（Ollama 原生 embed 不支持 batch）
//   - 返回顺序与 req.Input 顺序一致
//
// Ollama 不返回 token 用量（本地推理）；EmbeddingUsage 字段全 0。
func (c *HTTPClient) embeddingOllama(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	if len(req.Input) == 0 {
		return nil, &APIError{Code: -1, Message: "embedding input empty"}
	}

	out := &EmbeddingResponse{
		Model: c.config.ModelName,
		Data:  make([]Embedding, 0, len(req.Input)),
		Usage: &EmbeddingUsage{},
	}

	url := c.buildURL("/api/embeddings")
	for i, text := range req.Input {
		payload := map[string]any{
			"model":  c.config.ModelName,
			"prompt": text,
		}

		respBody, statusCode, err := c.sendRequest(ctx, "POST", url, payload)
		if err != nil {
			return nil, fmt.Errorf("ollama embedding (index=%d): %w", i, err)
		}

		if statusCode < 200 || statusCode >= 300 {
			return nil, &APIError{Code: statusCode, Message: string(respBody)}
		}

		var resp struct {
			Embedding []float64 `json:"embedding"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, fmt.Errorf("decode ollama embedding (index=%d): %w", i, err)
		}

		out.Data = append(out.Data, Embedding{
			Index:     i,
			Embedding: resp.Embedding,
		})
	}
	return out, nil
}
