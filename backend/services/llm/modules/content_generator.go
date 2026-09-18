package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// ContentGenerator 内容生成模块
type ContentGenerator struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewContentGenerator 创建内容生成模块
func NewContentGenerator(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &ContentGenerator{
		llm:       llm,
		templates: templates,
	}
}

func (s *ContentGenerator) Intent() []string {
	return []string{"生成", "创作"}
}

func (s *ContentGenerator) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("content_generator", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.7,
		MaxTokens:   1200,
	})
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content: resp.Content,
		Usage:   resp.Usage,
	}, nil
}

func (s *ContentGenerator) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("content_generator", req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	return s.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.7,
		MaxTokens:   1200,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("content_generator", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewContentGenerator(llm, tmpl)
	})
}
