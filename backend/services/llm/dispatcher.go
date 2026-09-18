package llm

import (
	"context"
	"strings"
	"time"

	"doc/services/llm/models"
)

// Dispatcher 调度器
type Dispatcher struct {
	llm       models.Model
	guard     *Guard
	templates *TemplateEngine
	modules   map[string]Module
}

// ModelName 获取模型名称
func (d *Dispatcher) ModelName() string {
	if d.llm == nil {
		return "nil"
	}
	return d.llm.Name()
}

// ModuleFactory 模块工厂函数
type ModuleFactory func(llm models.Model, tmpl *TemplateEngine) Module

// 全局模块注册表
var moduleRegistry = make(map[string]ModuleFactory)

// RegisterModule 注册模块工厂
func RegisterModule(name string, factory ModuleFactory) {
	moduleRegistry[name] = factory
}

// BuildDispatcher 构建调度器
func BuildDispatcher(llm models.Model, guard *Guard, templates *TemplateEngine) *Dispatcher {
	modules := make(map[string]Module)
	for name, factory := range moduleRegistry {
		modules[name] = factory(llm, templates)
	}
	return &Dispatcher{
		llm:       llm,
		guard:     guard,
		templates: templates,
		modules:   modules,
	}
}

// Request 请求结构
type Request struct {
	UserID   int64
	Intent   string
	Input    string
	Context  map[string]any // 业务上下文
	Template string        // 指定模板名
	Lang     string         // 回复语言
}

// Response 响应结构
type Response struct {
	Content     string            `json:"content"`
	Draft       any               `json:"draft,omitempty"`
	Citations   []Citation        `json:"citations,omitempty"`
	Suggestions []string          `json:"suggestions,omitempty"`
	Usage       models.Usage     `json:"usage"`
}

// Citation 引用
type Citation struct {
	Title  string  `json:"title"`
	Source string  `json:"source"`
	Score  float64 `json:"score"`
}

// Module 能力模块接口
type Module interface {
	Execute(ctx context.Context, req Request) (*Response, error)
	ExecuteStream(ctx context.Context, req Request) (<-chan models.StreamChunk, error)
	Intent() []string // 识别的意图关键词
}

// RecognizeIntent 意图识别（统一使用 summarizer 做智能路由）
func (d *Dispatcher) RecognizeIntent(text string) string {
	intentKeywords := map[string][]string{
		"summarizer": {"总结", "摘要", "概括", "提炼", "写", "生成", "创建", "分析", "研究"},
	}
	text = strings.ToLower(text)
	for intent, keywords := range intentKeywords {
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				return intent
			}
		}
	}
	return "unknown"
}

// Dispatch 调度执行
func (d *Dispatcher) Dispatch(ctx context.Context, req Request) (*Response, error) {
	start := time.Now()

	// 0. 优先使用 Context 中的 template，否则用意图识别
	moduleName := req.Template
	if moduleName == "" {
		moduleName = d.RecognizeIntent(req.Input)
		// unknown 时使用默认的 summarizer
		if moduleName == "unknown" {
			moduleName = "summarizer"
		}
	}

	// 1. 获取模块（使用默认 summarizer）
	module, ok := d.modules[moduleName]
	if !ok {
		module, ok = d.modules["summarizer"] // 兜底
		if !ok {
			return nil, ErrModuleNotFound
		}
		moduleName = "summarizer"
	}

	// 2. 防护脱敏
	_ = d.guard.Mask(req.Input)

	// 3. 执行模块
	resp, err := module.Execute(ctx, req)
	if err != nil {
		d.guard.Audit(ctx, AuditEvent{
			UserID:   req.UserID,
			Module:   moduleName,
			Status:   "error",
			ErrorMsg: err.Error(),
		})
		return nil, err
	}

	// 4. 输出脱敏
	resp.Content = d.guard.Mask(resp.Content)

	// 5. 审计成功
	d.guard.Audit(ctx, AuditEvent{
		UserID:       req.UserID,
		Module:       moduleName,
		Template:     req.Template,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
		LatencyMs:    time.Since(start).Milliseconds(),
		Model:        d.llm.Name(),
		Scene:        getScene(req.Context),
		Status:       "ok",
	})

	return resp, nil
}

// DispatchStream 流式调度执行
func (d *Dispatcher) DispatchStream(ctx context.Context, req Request) (<-chan models.StreamChunk, error) {
	// 0. 优先使用 Context 中的 template，否则用意图识别
	moduleName := req.Template
	if moduleName == "" {
		moduleName = d.RecognizeIntent(req.Input)
		// unknown 时使用默认的 summarizer
		if moduleName == "unknown" {
			moduleName = "summarizer"
		}
	}

	// 1. 防护脱敏
	_ = d.guard.Mask(req.Input)

	// 2. 获取模块（使用默认 summarizer）
	module, ok := d.modules[moduleName]
	if !ok {
		module, ok = d.modules["summarizer"] // 兜底
		if !ok {
			ch := make(chan models.StreamChunk, 1)
			ch <- models.StreamChunk{Error: ErrModuleNotFound}
			close(ch)
			return ch, nil
		}
		moduleName = "summarizer"
	}

	// 3. 执行流式
	stream, err := module.ExecuteStream(ctx, req)
	if err != nil {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	// 4. 包装流，加入审计
	out := make(chan models.StreamChunk, 100)
	go func() {
		defer close(out)
		start := time.Now()

		for chunk := range stream {
			if chunk.Error != nil {
				out <- chunk
				continue
			}
			out <- chunk

			if chunk.Done {
				d.guard.Audit(ctx, AuditEvent{
					UserID:      req.UserID,
					Module:      moduleName,
					Template:    req.Template,
					TotalTokens: chunk.Usage.TotalTokens,
					LatencyMs:   time.Since(start).Milliseconds(),
					Model:       d.llm.Name(),
					Scene:       getScene(req.Context),
					Status:      "ok",
				})
			}
		}
	}()

	return out, nil
}

// ExecuteModule 直接执行指定模块
func (d *Dispatcher) ExecuteModule(ctx context.Context, moduleName string, req Request) (*Response, error) {
	// 空字符串时使用默认模块
	if moduleName == "" {
		moduleName = "summarizer"
	}
	module, ok := d.modules[moduleName]
	if !ok {
		return nil, ErrModuleNotFound
	}

	_ = d.guard.Mask(req.Input)
	start := time.Now()

	resp, err := module.Execute(ctx, req)
	if err != nil {
		d.guard.Audit(ctx, AuditEvent{
			UserID:   req.UserID,
			Module:   moduleName,
			Status:   "error",
			ErrorMsg: err.Error(),
		})
		return nil, err
	}

	resp.Content = d.guard.Mask(resp.Content)

	d.guard.Audit(ctx, AuditEvent{
		UserID:       req.UserID,
		Module:       moduleName,
		Template:     req.Template,
		TotalTokens:  resp.Usage.TotalTokens,
		LatencyMs:    time.Since(start).Milliseconds(),
		Model:        d.llm.Name(),
		Scene:        getScene(req.Context),
		Status:       "ok",
	})

	return resp, nil
}

// ExecuteModuleStream 执行指定模块（流式）
func (d *Dispatcher) ExecuteModuleStream(ctx context.Context, moduleName string, req Request) (<-chan models.StreamChunk, error) {
	// 空字符串时使用默认模块
	if moduleName == "" {
		moduleName = "summarizer"
	}
	module, ok := d.modules[moduleName]
	if !ok {
		ch := make(chan models.StreamChunk, 1)
		ch <- models.StreamChunk{Error: ErrModuleNotFound}
		close(ch)
		return ch, nil
	}

	return module.ExecuteStream(ctx, req)
}

// getScene 从 Context 中提取场景
func getScene(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx["scene"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// 错误定义
var ErrModuleNotFound = &LLMError{Code: "MODULE_NOT_FOUND", Message: "模块不存在"}

// LLMError LLM 错误
type LLMError struct {
	Code    string
	Message string
}

func (e *LLMError) Error() string {
	return e.Message
}
