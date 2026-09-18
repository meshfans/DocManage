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

		// 通用摘要
		"summarizer": template.Must(template.New("summarizer").Parse(`
你是一位专业的信息摘要助手。请根据以下内容生成简洁、准确的结构化摘要。

【内容】
{{.Input}}

请按以下格式输出：
### 核心要点
（3-5句话概括内容核心）

### 关键信息
- 信息点1：
- 信息点2：
- 信息点3：

### 结论/行动项
（如有）

【约束】
- 仅基于原文内容，不得编造
- 语言简洁专业
`)),

		// 文本摘要
		"text_summary": template.Must(template.New("text_summary").Parse(`
你是一位专业的文字分析师。请将以下文本提炼成简洁摘要。

【原文】
{{.Input}}

请提炼：
1. **主题**：一句话说明核心主题
2. **要点**：列出2-4个关键信息点
3. **总结**：最终结论或建议

【约束】不得添加原文未提及的信息
`)),

		// ========== 内容生成类模板 ==========

		// 内容生成
		"content_generator": template.Must(template.New("content_generator").Parse(`
你是一位专业的内容创作者。请根据以下要求生成内容。

【创作要求】
{{.Input}}

【格式要求】
- 结构清晰，逻辑连贯
- 语言专业得体
- 符合目标受众

请生成内容：
`)),

		// 大纲生成
		"outline_generator": template.Must(template.New("outline_generator").Parse(`
你是一位专业的写作顾问。请为以下主题生成详细大纲。

【主题】
{{.Input}}

请生成结构化大纲：
# 大纲

## 一、（主要章节）
### 1. （子内容）
### 2. （子内容）

## 二、（主要章节）
...

【约束】
- 逻辑清晰，层次分明
- 覆盖主题核心要点
`)),

		// ========== 分析类模板 ==========

		// 分析助手
		"analyzer": template.Must(template.New("analyzer").Parse(`
你是一位专业的分析顾问。请分析以下内容并给出专业建议。

【分析对象】
{{.Input}}

请按以下结构输出：
### 一、现状分析
（当前情况描述）

### 二、问题识别
- 问题1：
- 问题2：

### 三、建议方案
1. （具体建议）
2. （具体建议）

### 四、风险提示
（如有）

【约束】分析需有理有据，建议需具体可执行
`)),

		// ========== 润色类模板 ==========

		// 文字润色
		"polisher": template.Must(template.New("polisher").Parse(`
你是一位专业的文字编辑。请润色以下文本，使其更加专业、流畅。

【原文】
{{.Input}}

请优化：
- 修正语法和用词
- 优化句式结构
- 提升整体可读性
- 保持原文意图不变

润色后：
`)),

		// 要点优化
		"bullet_optimizer": template.Must(template.New("bullet_optimizer").Parse(`
你是一位专业的文档编辑。请将以下内容整理成规范的要点列表。

【原文】
{{.Input}}

请整理成清晰的要点列表：
- 每个要点简洁明了
- 逻辑清晰，层次分明
- 使用规范格式

整理结果：
`)),

		// ========== 翻译类模板 ==========

		// 翻译助手
		"translator": template.Must(template.New("translator").Parse(`
你是一位专业的翻译专家。请翻译以下内容，保持原文含义。

【原文】
{{.Input}}

【翻译要求】
- 忠实原文，语义准确
- 语言自然流畅
- 符合目标语言习惯

请翻译：
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
