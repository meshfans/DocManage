package modules

import (
	"context"

	"doc/services/llm"
	"doc/services/llm/models"
)

// Generator 文案生成模块
type Generator struct {
	llm       models.Model
	templates *llm.TemplateEngine
}

// NewGenerator 创建文案生成模块
func NewGenerator(llm models.Model, templates *llm.TemplateEngine) llm.Module {
	return &Generator{
		llm:       llm,
		templates: templates,
	}
}

func (g *Generator) Intent() []string {
	return []string{"写", "生成", "创建"}
}

// getTemplateName 根据文案类型获取模板名
func (g *Generator) getTemplateName(req llm.Request) string {
	// 优先使用请求中指定的模板
	if req.Context != nil {
		if v, ok := req.Context["template"]; ok {
			if tmpl, ok := v.(string); ok && tmpl != "" {
				return tmpl
			}
		}
		// 根据 type 推断
		if v, ok := req.Context["type"]; ok {
			if t, ok := v.(string); ok {
				switch t {
				case "微信", "短信", "消息":
					return "followup_message"
				case "邮件", "email":
					return "business_email"
				case "拜访纪要", "拜访记录":
					return "visit_report"
				}
			}
		}
	}
	return "generator"
}

func (g *Generator) Execute(ctx context.Context, req llm.Request) (*llm.Response, error) {
	templateName := g.getTemplateName(req)
	prompt, err := g.templates.Render(templateName, req)
	if err != nil {
		// 降级到通用模板
		prompt, err = g.templates.Render("generator", req)
		if err != nil {
			return nil, err
		}
	}

	// 调用 LLM
	resp, err := g.llm.Chat(ctx, models.ChatRequest{
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

func (g *Generator) ExecuteStream(ctx context.Context, req llm.Request) (<-chan models.StreamChunk, error) {
	templateName := g.getTemplateName(req)
	prompt, err := g.templates.Render(templateName, req)
	if err != nil {
		prompt, err = g.templates.Render("generator", req)
		if err != nil {
			ch := make(chan models.StreamChunk, 1)
			ch <- models.StreamChunk{Error: err}
			close(ch)
			return ch, nil
		}
	}

	return g.llm.ChatStream(ctx, models.ChatRequest{
		Model:       "",
		Messages:    []models.Message{{Role: "user", Content: prompt}},
		Temperature: 0.7,
		MaxTokens:   1200,
	})
}

// 注册模块
func init() {
	llm.RegisterModule("generate", func(llm models.Model, tmpl *llm.TemplateEngine) llm.Module {
		return NewGenerator(llm, tmpl)
	})
}
