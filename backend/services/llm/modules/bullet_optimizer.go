package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// BulletOptimizer 要点列表整理模块
type BulletOptimizer struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewBulletOptimizer 创建要点整理模块
func NewBulletOptimizer(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &BulletOptimizer{
		llm:       llm,
		templates: templates,
	}
}

func (s *BulletOptimizer) Intent() []string {
	return []string{"要点", "列表", "整理"}
}

func (s *BulletOptimizer) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("bullet_optimizer", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.3,
		MaxTokens:   600,
	})
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content: resp.Content,
		Usage:   resp.Usage,
	}, nil
}

func (s *BulletOptimizer) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("bullet_optimizer", req)
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
		MaxTokens:   600,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("bullet_optimizer", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewBulletOptimizer(llm, tmpl)
	})
}
