package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Summarizer 文本摘要模块
type Summarizer struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewSummarizer 创建摘要模块
func NewSummarizer(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &Summarizer{
		llm:       llm,
		templates: templates,
	}
}

func (s *Summarizer) Intent() []string {
	return []string{"总结", "摘要", "概括", "提炼"}
}

// getTemplateName 根据场景获取模板名
func (s *Summarizer) getTemplateName(req llm.Request) string {
	scene := ""
	if req.Context != nil {
		if v, ok := req.Context["scene"]; ok {
			scene, _ = v.(string)
		}
	}

	// 根据场景选择模板
	switch scene {
	case "通话", "电话", "call":
		return "call_summary"
	default:
		return "summarizer"
	}
}

func (s *Summarizer) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	// 根据场景选择模板
	templateName := s.getTemplateName(req)
	prompt, err := s.templates.Render(templateName, req)
	if err != nil {
		// 模板不存在时降级到通用模板
		prompt, err = s.templates.Render("summarizer", req)
		if err != nil {
			return nil, err
		}
	}

	// 调用 LLM
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

func (s *Summarizer) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	templateName := s.getTemplateName(req)
	prompt, err := s.templates.Render(templateName, req)
	if err != nil {
		prompt, err = s.templates.Render("summarizer", req)
		if err != nil {
			ch := make(chan models.StreamChunk, 1)
			ch <- models.StreamChunk{Error: err}
			close(ch)
			return ch, nil
		}
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
	llm.RegisterModule("summarize", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewSummarizer(llm, tmpl)
	})
}
