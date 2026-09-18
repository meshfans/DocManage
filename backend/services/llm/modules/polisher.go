package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Polisher 文字润色模块
type Polisher struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewPolisher 创建润色模块
func NewPolisher(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &Polisher{
		llm:       llm,
		templates: templates,
	}
}

func (s *Polisher) Intent() []string {
	return []string{"润色", "优化", "改写"}
}

func (s *Polisher) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("polisher", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.4,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content: resp.Content,
		Usage:   resp.Usage,
	}, nil
}

func (s *Polisher) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("polisher", req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	return s.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.4,
		MaxTokens:   800,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("polisher", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewPolisher(llm, tmpl)
	})
}
