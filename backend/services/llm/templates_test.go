package llm

import (
	"strings"
	"testing"
)

// TestTemplateEngineRender 测试模板渲染
func TestTemplateEngineRender(t *testing.T) {
	engine := NewTemplateEngine()

	tests := []struct {
		name    string
		template string
		data    Request
		want    string
		wantErr bool
	}{
		{
			name:    "summarizer模板渲染",
			template: "summarizer",
			data:    Request{Input: "测试内容"},
			want:    "测试内容",
			wantErr: false,
		},
		{
			name:    "analyzer模板渲染",
			template: "analyzer",
			data:    Request{Input: "分析这个"},
			want:    "分析这个",
			wantErr: false,
		},
		{
			name:    "polisher模板渲染",
			template: "polisher",
			data:    Request{Input: "润色这段话"},
			want:    "润色这段话",
			wantErr: false,
		},
		{
			name:    "translator模板渲染",
			template: "translator",
			data:    Request{Input: "翻译这段"},
			want:    "翻译这段",
			wantErr: false,
		},
		{
			name:    "text_summary模板渲染",
			template: "text_summary",
			data:    Request{Input: "简短摘要"},
			want:    "简短摘要",
			wantErr: false,
		},
		{
			name:    "content_generator模板渲染",
			template: "content_generator",
			data:    Request{Input: "生成内容"},
			want:    "生成内容",
			wantErr: false,
		},
		{
			name:    "outline_generator模板渲染",
			template: "outline_generator",
			data:    Request{Input: "生成大纲"},
			want:    "生成大纲",
			wantErr: false,
		},
		{
			name:    "bullet_optimizer模板渲染",
			template: "bullet_optimizer",
			data:    Request{Input: "整理成要点"},
			want:    "整理成要点",
			wantErr: false,
		},
		{
			name:    "不存在的模板",
			template: "nonexistent",
			data:    Request{Input: "测试"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Render(tt.template, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !strings.Contains(got, tt.want) {
				t.Errorf("Render() output should contain %q, got %q", tt.want, got)
			}
		})
	}
}

// TestTemplateEngineRegister 测试动态注册模板
func TestTemplateEngineRegister(t *testing.T) {
	engine := NewTemplateEngine()

	// 注册新模板
	engine.Register("test_template", "Hello {{.Input}}!")

	// 渲染新模板
	got, err := engine.Render("test_template", Request{Input: "World"})
	if err != nil {
		t.Errorf("Render() error = %v", err)
	}
	if got != "Hello World!" {
		t.Errorf("Render() = %q, want %q", got, "Hello World!")
	}
}

// TestTemplateError 测试模板错误
func TestTemplateError(t *testing.T) {
	err := &TemplateError{Name: "test", Message: "模板不存在"}
	if err.Error() != "模板不存在" {
		t.Errorf("TemplateError.Error() = %q, want %q", err.Error(), "模板不存在")
	}
}

// TestGetTemplateEngine 测试全局模板引擎
func TestGetTemplateEngine(t *testing.T) {
	// 第一次获取
	engine1 := GetTemplateEngine()
	if engine1 == nil {
		t.Fatal("GetTemplateEngine() returned nil")
	}

	// 第二次获取应该是同一个实例
	engine2 := GetTemplateEngine()
	if engine1 != engine2 {
		t.Error("GetTemplateEngine() should return the same instance")
	}
}

// TestRegisterGlobalTemplate 测试全局模板注册
func TestRegisterGlobalTemplate(t *testing.T) {
	// 注册全局模板
	RegisterGlobalTemplate("global_test", "Global: {{.Input}}")

	// 获取并渲染
	engine := GetTemplateEngine()
	got, err := engine.Render("global_test", Request{Input: "Test"})
	if err != nil {
		t.Errorf("Render() error = %v", err)
	}
	if got != "Global: Test" {
		t.Errorf("Render() = %q, want %q", got, "Global: Test")
	}
}

// TestAllTemplatesExist 测试所有模板都存在
func TestAllTemplatesExist(t *testing.T) {
	engine := NewTemplateEngine()

	templates := []string{
		"summarizer",
		"text_summary",
		"analyzer",
		"polisher",
		"bullet_optimizer",
		"translator",
		"content_generator",
		"outline_generator",
	}

	for _, tmpl := range templates {
		t.Run(tmpl, func(t *testing.T) {
			_, err := engine.Render(tmpl, Request{Input: "test"})
			if err != nil {
				t.Errorf("Template %q should exist, got error: %v", tmpl, err)
			}
		})
	}
}
