package llm

import (
	"bytes"
	"text/template"
)

// TemplateEngine Prompt 模板引擎
type TemplateEngine struct {
	templates map[string]*template.Template
}

// NewTemplateEngine 创建模板引擎
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		templates: loadDefaultTemplates(),
	}
}

// Render 渲染模板
func (te *TemplateEngine) Render(name string, data any) (string, error) {
	t, ok := te.templates[name]
	if !ok {
		return "", &TemplateError{Name: name, Message: "模板不存在"}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Register 注册模板
func (te *TemplateEngine) Register(name string, tmpl string) {
	te.templates[name] = template.Must(template.New(name).Parse(tmpl))
}

// TemplateError 模板错误
type TemplateError struct {
	Name    string
	Message string
}

func (e *TemplateError) Error() string {
	return e.Message
}

// loadDefaultTemplates 加载默认模板
func loadDefaultTemplates() map[string]*template.Template {
	return map[string]*template.Template{
		// ========== 摘要类模板 ==========

		// 通用摘要（兜底）
		"summarizer": template.Must(template.New("summarizer").Parse(`
你是信息摘要助手。请根据以下内容生成结构化摘要。

【场景】{{.Context.scene}}
【内容】
{{.Input}}

请按以下格式输出（所有信息必须来自原文，不得编造）：
### 一、核心主题
（1句话概括本次沟通的核心目的和主要内容）

### 二、关键信息提取
- 对接人/决策人：
- 客户需求/痛点：
- 预算/费用提及：
- 时间节点/约定：
- 客户异议/顾虑：

### 三、后续行动建议
1. （具体可执行的跟进动作）
2. （如有）

【约束】
- 信息必须100%来自原文，未提及的标注「未提及」，不得编造
- 语言简洁干练，使用销售业务术语
- 禁止输出与本次沟通无关的内容
`)),

		// 通话记录摘要
		"call_summary": template.Must(template.New("call_summary").Parse(`
你是CRM客户沟通分析师，负责从通话记录中提取关键信息，生成结构化、可落地的沟通摘要。

【通话基本信息】
- 客户：{{.Context.customer_name}}
- 通话时长：{{.Context.call_duration}}
- 通话时间：{{.Context.call_time}}

【通话内容】
{{.Input}}

请按以下格式输出：
### 一、沟通核心主题
（1句话概括本次通话的核心目的）

### 二、客户画像提取
- 客户需求：
- 预算范围：
- 决策周期：
- 关键决策人：
- 竞品提及：

### 三、关键行动项
1. 【待确认】{{.Context.customer_name}}的需求细节
2. 【待跟进】预算确认
3. 【待安排】下次沟通时间：

### 四、风险预警
- （如有客户异议、竞品干扰、决策人变更等风险）

### 五、跟进建议
1. （下一步具体动作）
2. （如无特殊情况，保持正常节奏）

【约束】
- 仅基于通话内容分析，不得编造信息
- 未提及的信息标注「未提及」
- 使用销售业务术语，简洁专业
`)),

		// ========== 文案生成类模板 ==========

		// 通用文案生成（兜底）
		"generator": template.Must(template.New("generator").Parse(`
你是资深B端销售文案助手，负责生成专业、得体的商务沟通文案。

【基本信息】
- 角色：{{.Context.role}}
- 文案类型：{{.Context.type}}
- 客户：{{.Context.customer_name}}

【沟通目的】
{{.Context.purpose}}

【客户背景】
{{.Context.customer_info}}

【格式要求】
{{.Context.format}}

请生成文案：
`)),
	}
}

// 全局模板引擎实例
var globalTemplateEngine *TemplateEngine

// GetTemplateEngine 获取全局模板引擎
func GetTemplateEngine() *TemplateEngine {
	if globalTemplateEngine == nil {
		globalTemplateEngine = NewTemplateEngine()
	}
	return globalTemplateEngine
}

// RegisterGlobalTemplate 注册全局模板
func RegisterGlobalTemplate(name string, tmpl string) {
	GetTemplateEngine().Register(name, tmpl)
}
