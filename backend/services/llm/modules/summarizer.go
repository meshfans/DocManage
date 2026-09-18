package modules

import (
	"context"
	"strings"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Summarizer 文本摘要模块（主入口，索引其他模板）
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
	return []string{}
}

// getTemplateName 智能选择模板
func (s *Summarizer) getTemplateName(req llm.Request) string {
	// 1. 优先使用 Context 中指定的 template
	if req.Context != nil {
		if v, ok := req.Context["template"]; ok {
			if tmpl, ok := v.(string); ok && tmpl != "" {
				return tmpl
			}
		}
	}

	// 2. 使用请求中指定的模板
	if req.Template != "" {
		return req.Template
	}

	// 3. 根据输入内容智能识别
	input := strings.ToLower(req.Input)
	return s.recognizeTemplate(input)
}

// recognizeTemplate 根据输入内容识别模板
func (s *Summarizer) recognizeTemplate(input string) string {
	// 分析类关键词
	analyzerKw := []string{"分析", "研究", "评估", "对比", "调研", "研究下", "分析下"}
	for _, kw := range analyzerKw {
		if strings.Contains(input, kw) {
			return "analyzer"
		}
	}

	// 润色类关键词
	polisherKw := []string{"润色", "优化", "改写", "修改", "调整", "改善", "精简", "压缩"}
	for _, kw := range polisherKw {
		if strings.Contains(input, kw) {
			return "polisher"
		}
	}

	// 要点整理关键词
	bulletKw := []string{"要点", "列表", "整理成", "列出", "清单", "条目"}
	for _, kw := range bulletKw {
		if strings.Contains(input, kw) {
			return "bullet_optimizer"
		}
	}

	// 翻译类关键词
	translatorKw := []string{"翻译", "英文", "中文", "译成", "转换成"}
	for _, kw := range translatorKw {
		if strings.Contains(input, kw) {
			return "translator"
		}
	}

	// 大纲生成关键词
	outlineKw := []string{"大纲", "提纲", "结构", "目录", "框架", "草拟", "草稿"}
	for _, kw := range outlineKw {
		if strings.Contains(input, kw) {
			return "outline_generator"
		}
	}

	// 内容生成关键词
	generatorKw := []string{"写", "生成", "创作", "编写", "起草", "撰写", "帮我", "帮写"}
	for _, kw := range generatorKw {
		if strings.Contains(input, kw) {
			return "content_generator"
		}
	}

	// 简短摘要关键词
	shortKw := []string{"简短", "简述", "简明", "一句话", "概括下"}
	for _, kw := range shortKw {
		if strings.Contains(input, kw) {
			return "text_summary"
		}
	}

	// 默认使用 summarizer
	return "summarizer"
}

func (s *Summarizer) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	templateName := s.getTemplateName(req)
	prompt, err := s.templates.Render(templateName, req)
	if err != nil {
		// 模板不存在时降级到 summarizer
		prompt, err = s.templates.Render("summarizer", req)
		if err != nil {
			return nil, err
		}
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
	llm.RegisterModule("summarizer", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewSummarizer(llm, tmpl)
	})
}
