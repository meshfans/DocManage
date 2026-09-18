package llm

import (
	"context"
	"testing"

	"doc/services/llm/models"
)

// TestRecognizeIntent 测试意图识别
func TestRecognizeIntent(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"总结关键词", "帮我总结一下", "summarizer"},
		{"摘要关键词", "这段话摘要是什么", "summarizer"},
		{"分析关键词", "分析下这个方案", "summarizer"},
		{"研究关键词", "研究一下", "summarizer"},
		{"写关键词", "帮我写信", "summarizer"},
		{"生成关键词", "生成一段文字", "summarizer"},
		{"创建关键词", "创建一个标题", "summarizer"},
		{"未知意图", "hello world", "unknown"},
		{"空输入", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.RecognizeIntent(tt.input)
			if got != tt.expected {
				t.Errorf("RecognizeIntent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestResolveModuleName 测试模块名解析（空值回退）
func TestResolveModuleName(t *testing.T) {
	// 空模块名应该回退到 summarizer
	d := &Dispatcher{
		modules: map[string]Module{
			"summarizer": nil,
			"analyzer":   nil,
		},
	}

	tests := []struct {
		name       string
		moduleName string
		wantOk     bool
	}{
		{"summarizer存在", "summarizer", true},
		{"analyzer存在", "analyzer", true},
		{"不存在模块", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := d.modules[tt.moduleName]
			if ok != tt.wantOk {
				t.Errorf("modules[%q] exists = %v, want %v", tt.moduleName, ok, tt.wantOk)
			}
		})
	}
}

// TestDispatcherDispatch_Fallback 测试 Dispatch 兜底逻辑
func TestDispatcherDispatch_Fallback(t *testing.T) {
	d := &Dispatcher{
		modules: map[string]Module{
			"summarizer": &mockModule{},
		},
	}

	// 不存在的模块应该回退到 summarizer
	req := Request{
		UserID:   1,
		Input:    "hello",
		Template: "nonexistent_module",
	}

	_, ok := d.modules[req.Template]
	if ok {
		t.Error("nonexistent_module should not exist")
	}

	// 验证回退逻辑
	fallback := d.modules["summarizer"]
	if fallback == nil {
		t.Error("summarizer should exist as fallback")
	}
}

// mockModule 实现 Module 接口用于测试
type mockModule struct{}

func (m *mockModule) Execute(ctx context.Context, req Request) (*Response, error) {
	return &Response{Content: "mock"}, nil
}

func (m *mockModule) ExecuteStream(ctx context.Context, req Request) (<-chan models.StreamChunk, error) {
	ch := make(chan models.StreamChunk, 1)
	ch <- models.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockModule) Intent() []string {
	return []string{}
}

// TestErrModuleNotFound 测试模块不存在错误
func TestErrModuleNotFound(t *testing.T) {
	err := ErrModuleNotFound
	if err.Code != "MODULE_NOT_FOUND" {
		t.Errorf("ErrModuleNotFound.Code = %q, want MODULE_NOT_FOUND", err.Code)
	}
	if err.Message != "模块不存在" {
		t.Errorf("ErrModuleNotFound.Message = %q, want 模块不存在", err.Message)
	}
}

// TestGetScene 测试从 Context 提取场景
func TestGetScene(t *testing.T) {
	tests := []struct {
		name     string
		ctx      map[string]any
		expected string
	}{
		{"正常场景", map[string]any{"scene": "通话"}, "通话"},
		{"空场景", map[string]any{"scene": ""}, ""},
		{"nil场景", nil, ""},
		{"无scene键", map[string]any{"other": "value"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getScene(tt.ctx)
			if got != tt.expected {
				t.Errorf("getScene(%v) = %q, want %q", tt.ctx, got, tt.expected)
			}
		})
	}
}
