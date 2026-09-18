package models

import (
	"context"

	"doc/pkg/llmclient"
)

// LLMClientModel 基于 llmclient 的 Model 实现
type LLMClientModel struct {
	client llmclient.Client
}

// NewLLMClientModel 创建基于 llmclient 的 Model
func NewLLMClientModel(cfg *ModelConfig) Model {
	provider := llmclient.Provider(cfg.Provider)
	if provider == "" {
		provider = llmclient.DetectProviderByBaseURL(cfg.APIBase)
	}

	llmCfg := &llmclient.Config{
		Provider:  provider,
		Protocol:  llmclient.Protocol(cfg.Protocol),
		ModelName: cfg.ModelName,
		APIKey:    cfg.APIKey,
		APIBase:   cfg.APIBase,
		TimeoutMs: 60000,
	}

	return &LLMClientModel{
		client: llmclient.NewClient(llmCfg),
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

// Embeddings 实现 Model 接口（暂不支持）
func (m *LLMClientModel) Embeddings(ctx context.Context, texts []string) ([]Embedding, error) {
	return nil, nil
}

// Name 实现 Model 接口
func (m *LLMClientModel) Name() string {
	return m.client.Name()
}
