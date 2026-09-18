package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Translator 翻译模块
type Translator struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewTranslator 创建翻译模块
func NewTranslator(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &Translator{
		llm:       llm,
		templates: templates,
	}
}

func (s *Translator) Intent() []string {
	return []string{"翻译", "英文", "中文"}
}

func (s *Translator) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("translator", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.3,
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

func (s *Translator) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("translator", req)
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
		MaxTokens:   800,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("translator", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewTranslator(llm, tmpl)
	})
}
