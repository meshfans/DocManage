package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// OutlineGenerator 大纲生成模块
type OutlineGenerator struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewOutlineGenerator 创建大纲生成模块
func NewOutlineGenerator(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &OutlineGenerator{
		llm:       llm,
		templates: templates,
	}
}

func (s *OutlineGenerator) Intent() []string {
	return []string{"大纲", "结构", "提纲"}
}

func (s *OutlineGenerator) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("outline_generator", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.6,
		MaxTokens:   1000,
	})
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content: resp.Content,
		Usage:   resp.Usage,
	}, nil
}

func (s *OutlineGenerator) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("outline_generator", req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	return s.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.6,
		MaxTokens:   1000,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("outline_generator", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewOutlineGenerator(llm, tmpl)
	})
}
