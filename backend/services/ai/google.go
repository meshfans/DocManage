package ai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GoogleAdapter Google Generative AI 协议适配器
type GoogleAdapter struct {
	cfg      *Config
	endpoint string
	headers  map[string]string
}

// newGoogleAdapter 创建 Google 适配器
func newGoogleAdapter(cfg *Config) *GoogleAdapter {
	base := strings.TrimSuffix(cfg.APIBase, "/")
	model := url.PathEscape(cfg.ModelName)
	endpoint := base + "/v1beta/models/" + model + ":generateContent"

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return &GoogleAdapter{
		cfg:      cfg,
		endpoint: endpoint,
		headers:  headers,
	}
}

// urlWithKey 添加 API Key
func (a *GoogleAdapter) urlWithKey() string {
	if a.cfg.APIKey == "" {
		return a.endpoint
	}
	return a.endpoint + "?key=" + a.cfg.APIKey
}

// buildGoogleBody 构造 Google 请求体
func (a *GoogleAdapter) buildGoogleBody(req *ChatRequest) map[string]interface{} {
	// 分离 system 消息
	var systemParts []map[string]string
	var otherMsgs []ChatMessage
	for _, m := range req.Messages {
		if m.Role == "system" && strings.TrimSpace(m.Content) != "" {
			systemParts = append(systemParts, map[string]string{"text": strings.TrimSpace(m.Content)})
		} else {
			otherMsgs = append(otherMsgs, m)
		}
	}

	contents := make([]map[string]interface{}, 0, len(otherMsgs))
	for _, m := range otherMsgs {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}

		parts := []map[string]interface{}{{"text": m.Content}}

		// 多模态处理
		if len(req.Images) > 0 && m.Role == "user" {
			parts = make([]map[string]interface{}, 0)
			if m.Content != "" {
				parts = append(parts, map[string]interface{}{"text": m.Content})
			}
			for _, img := range req.Images {
				parts = append(parts, map[string]interface{}{
					"inline_data": map[string]string{
						"mime_type": img.Mime,
						"data":      img.Base64,
					},
				})
			}
		}

		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": parts,
		})
	}

	body := map[string]interface{}{
		"contents": contents,
	}

	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]interface{}{
			"role":  "system",
			"parts": systemParts,
		}
	}

	maxTokens := 2048
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	genConfig := map[string]interface{}{
		"maxOutputTokens": maxTokens,
	}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	body["generationConfig"] = genConfig

	return body
}

// TestConnection 测试连接
func (a *GoogleAdapter) TestConnection() (*TestResult, error) {
	t0 := time.Now()

	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{{"text": "ping"}},
			},
		},
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": 1,
		},
	}

	_, _, err := sendJSON(a.urlWithKey(), "POST", a.headers, body, 30000)
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
func (a *GoogleAdapter) Chat(req *ChatRequest) (*ChatResponse, error) {
	body := a.buildGoogleBody(req)

	respBody, _, err := sendJSON(a.urlWithKey(), "POST", a.headers, body, req.TimeoutMs)
	if err != nil {
		return nil, err
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
			PromptTokenCount       int `json:"promptTokenCount"`
			CandidatesTokenCount    int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("响应为空")
	}

	var text string
	for _, p := range resp.Candidates[0].Content.Parts {
		text += p.Text
	}

	text = stripThinking(text)

	var usage *Usage
	if resp.UsageMetadata.PromptTokenCount > 0 || resp.UsageMetadata.CandidatesTokenCount > 0 {
		usage = &Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
		}
	}

	return &ChatResponse{
		Text:  text,
		Usage: usage,
	}, nil
}
