package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Analyzer 分析顾问模块
type Analyzer struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewAnalyzer 创建分析模块
func NewAnalyzer(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &Analyzer{
		llm:       llm,
		templates: templates,
	}
}

func (s *Analyzer) Intent() []string {
	return []string{"分析", "研究", "评估"}
}

func (s *Analyzer) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	prompt, err := s.templates.Render("analyzer", req)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.Chat(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.5,
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

func (s *Analyzer) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	prompt, err := s.templates.Render("analyzer", req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	return s.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.5,
		MaxTokens:   1000,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("analyzer", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewAnalyzer(llm, tmpl)
	})
}
