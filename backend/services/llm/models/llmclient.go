package models

import (
	"context"
	"fmt"
	"time"

	"doc/config"
	"doc/pkg/llmclient"
	"doc/utils"
)

// LLMClientModel 基于 llmclient 的 Model 实现
type LLMClientModel struct {
	client    llmclient.Client
	provider  string
	modelName string
}

// resolveMeshfansAPIKey 兜底注入 LicenseKey（meshfans 专用）
//
// 2026-10-01 对齐 DocCRM / DocManage。
//
// 背景：license 不含 "ai" 时，/api/ai-configs/** 被 FeatureGate("ai") 全部 403，
// 用户无法在页面上填写 API Key。而 /api/llm/** 不设门禁（Copilot 默认全开放），
// 仍会取 is_default=1 的种子行发请求。meshfans 种子行的 api_key 天然为空，
// 没有兜底就会带着空 key 打 meshfans，必然 401。
//
// 优先级：
//  1. ai_config.api_key（非空 → 用户自填 key 优先，兼容老用法）
//  2. config.GlobalConfig.License.LicenseKey（兜底）
//  3. 仍为空 → warn，由 meshfans 返回 401
func resolveMeshfansAPIKey(cfg *ModelConfig) string {
	if cfg.APIKey != "" {
		return cfg.APIKey
	}
	if cfg.Provider != "meshfans" {
		return cfg.APIKey
	}
	lc := config.GlobalConfig
	if lc == nil || lc.License.LicenseKey == "" {
		utils.Warn("[LLM] meshfans api_key 为空且未配置 license_key，请求将被服务端拒绝")
		return ""
	}
	utils.Info("[LLM] meshfans api_key 兜底使用全局 license_key (len=%d)", len(lc.License.LicenseKey))
	return lc.License.LicenseKey
}

// NewLLMClientModel 创建基于 llmclient 的 Model
//
// 2026-09-30 对齐 DocCRM：返回具体类型 *LLMClientModel（而非 Model 接口），
// 便于 TestAIConfig / TestAIConfigInline 等探测场景直接调用
// TestConnection / Provider / ModelName 等非 dispatch 链路方法。
// *LLMClientModel 仍实现 Model 接口（Chat / ChatStream / Embeddings / Name），
// 可直接传入 BuildDispatcher。
func NewLLMClientModel(cfg *ModelConfig) *LLMClientModel {
	provider := llmclient.Provider(cfg.Provider)
	if provider == "" {
		provider = llmclient.DetectProviderByBaseURL(cfg.APIBase)
	}

	llmCfg := &llmclient.Config{
		Provider:  provider,
		Protocol:  llmclient.Protocol(cfg.Protocol),
		ModelName: cfg.ModelName,
		APIKey:    resolveMeshfansAPIKey(cfg),
		APIBase:   cfg.APIBase,
		APIPath:   cfg.APIPath,
		ProxyURL:  cfg.ProxyURL,
		TimeoutMs: 60000,
	}

	return &LLMClientModel{
		client:    llmclient.NewClient(llmCfg),
		provider:  string(provider),
		modelName: cfg.ModelName,
	}
}

// Chat 实现 Model 接口
func (m *LLMClientModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	messages := make([]llmclient.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, llmclient.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	llmReq := &llmclient.ChatRequest{
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	llmResp, err := m.client.Chat(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	var usage Usage
	if llmResp.Usage != nil {
		usage = Usage{
			PromptTokens:     llmResp.Usage.PromptTokens,
			CompletionTokens: llmResp.Usage.CompletionTokens,
			TotalTokens:      llmResp.Usage.TotalTokens,
		}
	}

	return &ChatResponse{
		Content: llmResp.Text,
		Usage:   usage,
	}, nil
}

// ChatStream 实现 Model 接口
func (m *LLMClientModel) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	messages := make([]llmclient.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, llmclient.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	llmReq := &llmclient.ChatRequest{
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	stream, err := m.client.ChatStream(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		for chunk := range stream {
			ch <- StreamChunk{
				Delta: chunk.Delta,
				Done:  chunk.Done,
				Usage: Usage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				},
				Error: chunk.Error,
			}
		}
	}()

	return ch, nil
}

// Embeddings 实现 Model 接口
//
// 2026-09-30 对齐 DocCRM：替换原 stub（原实现返回 nil, nil，一旦接入 RAG 会静默产生空向量）。
//
// 入参：texts → 拼成 llmclient.EmbeddingRequest{Input: texts}
// 出参：llmclient.EmbeddingResponse.Data → []Embedding{Text, Vector}（按 Index 顺序）
//
// 注意：
//   - Model 接口的 Embedding.Vector 是 []float64，与 llmclient.EmbeddingResponse 一致
//   - Provider 不支持 Embedding（Anthropic / Google / Zhipu / MiniMax）→ 透传
//     llmclient.ErrEmbeddingUnsupported，业务层调用方需识别此错误
//   - Ollama 不返回 token 用量；不写 llm_audit_log 的 token 字段（保持 0）
func (m *LLMClientModel) Embeddings(ctx context.Context, texts []string) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("embedding input empty")
	}

	llmReq := &llmclient.EmbeddingRequest{
		Input: texts,
		// Dimensions 留 0 → 服务端默认；后续如需裁剪维度，从 ai_config.default_params 解析覆盖
	}

	resp, err := m.client.Embeddings(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	out := make([]Embedding, 0, len(resp.Data))
	for _, d := range resp.Data {
		if d.Index >= 0 && d.Index < len(texts) {
			out = append(out, Embedding{
				Text:   texts[d.Index],
				Vector: d.Embedding,
			})
		}
	}
	return out, nil
}

// Provider 返回 provider 名称（多模态检测等需要）
func (m *LLMClientModel) Provider() string { return m.provider }

// ModelName 返回模型名
func (m *LLMClientModel) ModelName() string { return m.modelName }

// TestConnection 测试当前 model 的连通性（复用 llmclient.TestConnection）。
//
// 用途：AI 配置页 /api/ai-configs/:id/test 与 /api/ai-configs/test
// 统一通过此方法探测，避免与 services/ai.BuildAdapter 重复实现。
func (m *LLMClientModel) TestConnection() (*llmclient.TestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return m.client.TestConnection(ctx)
}

// ChatRaw 透传到底层 llmclient（多模态探测等独立场景使用）。
//
// 与 Model.Chat 的区别：Model.Chat 走 dispatch + audit + guard 链路；
// 本方法仅供 services/ai.TestMultimodal 等独立探测场景使用，无业务审计。
//
// 命名：与 Model.Chat(ctx, ChatRequest) 区分；两者参数类型不同。
func (m *LLMClientModel) ChatRaw(ctx context.Context, req *llmclient.ChatRequest) (*llmclient.ChatResponse, error) {
	return m.client.Chat(ctx, req)
}

// Name 实现 Model 接口
func (m *LLMClientModel) Name() string {
	return m.client.Name()
}
