package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// TextSummary 文字简短摘要模块
type TextSummary struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewTextSummary 创建文字摘要模块
func NewTextSummary(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &TextSummary{
		llm:       llm,
		templates: templates,
	}
}

func (s *TextSummary) Intent() []string {
	return []string{"简短", "简述"}
}

func (s *TextSummary) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("text_summary", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.3,
		MaxTokens:   500,
	})
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content: resp.Content,
		Usage:   resp.Usage,
	}, nil
}

func (s *TextSummary) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("text_summary", req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	return s.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.3,
		MaxTokens:   500,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("text_summary", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewTextSummary(llm, tmpl)
	})
}
