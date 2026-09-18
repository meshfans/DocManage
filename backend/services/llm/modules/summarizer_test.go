package modules

import (
	"testing"

	"doc/services/llm"
)

// Request 是 llm.Request 的别名，用于测试
type Request = llm.Request

// TestRecognizeTemplate 测试模板识别逻辑
func TestRecognizeTemplate(t *testing.T) {
	s := &Summarizer{}

	tests := []struct {
		name     string
		input   string
		expected string
	}{
		// 分析类
		{"分析", "帮我分析一下这个方案", "analyzer"},
		{"研究", "研究下这个市场", "analyzer"},
		{"评估", "评估一下风险", "analyzer"},

		// 润色类
		{"润色", "润色这段文字", "polisher"},
		{"优化", "优化一下表达", "polisher"},
		{"改写", "改写这段话", "polisher"},
		{"修改", "修改语法", "polisher"},

		// 要点整理类
		{"要点", "列出要点", "bullet_optimizer"},
		{"列表", "整理成列表", "bullet_optimizer"},
		{"清单", "生成清单", "bullet_optimizer"},

		// 翻译类
		{"翻译", "翻译成英文", "translator"},
		{"英文", "帮我翻译成英文", "translator"},
		{"中文", "转成中文", "translator"},

		// 大纲生成类
		{"大纲", "生成大纲", "outline_generator"},
		{"提纲", "写个提纲", "outline_generator"},
		{"结构", "梳理一下结构", "outline_generator"},

		// 内容生成类
		{"写", "帮我写信", "content_generator"},
		{"生成", "生成一段文案", "content_generator"},
		{"创作", "创作一篇文章", "content_generator"},
		{"帮我", "帮我写个标题", "content_generator"},
		{"帮写", "帮写一份报告", "content_generator"},

		// 简短摘要类（需要单独使用简述，不能与其他关键词混用）
		{"简短", "简短总结一下", "text_summary"},
		{"简述", "简述", "text_summary"},
		{"简明", "简明扼要", "text_summary"},
		{"一句话", "一句话概括", "text_summary"},

		// 默认
		{"默认", "随便聊聊", "summarizer"},
		{"空输入", "", "summarizer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.recognizeTemplate(tt.input)
			if got != tt.expected {
				t.Errorf("recognizeTemplate(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestGetTemplateName 测试完整模板选择逻辑
func TestGetTemplateName(t *testing.T) {
	s := &Summarizer{}

	tests := []struct {
		name     string
		req      Request
		expected string
	}{
		{
			name: "Context中指定template",
			req: Request{
				Context:  map[string]any{"template": "analyzer"},
				Input:   "hello",
				Template: "",
			},
			expected: "analyzer",
		},
		{
			name: "请求中指定Template",
			req: Request{
				Context:  nil,
				Input:   "hello",
				Template: "polisher",
			},
			expected: "polisher",
		},
		{
			name: "Content优先于Template",
			req: Request{
				Context:  map[string]any{"template": "analyzer"},
				Input:   "帮我分析",
				Template: "summarizer",
			},
			expected: "analyzer",
		},
		{
			name: "内容识别-分析",
			req: Request{
				Context:  nil,
				Input:   "分析一下这个方案",
				Template: "",
			},
			expected: "analyzer",
		},
		{
			name: "内容识别-生成",
			req: Request{
				Context:  nil,
				Input:   "帮我写封信",
				Template: "",
			},
			expected: "content_generator",
		},
		{
			name: "内容识别-默认",
			req: Request{
				Context:  nil,
				Input:   "hello world",
				Template: "",
			},
			expected: "summarizer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.getTemplateName(tt.req)
			if got != tt.expected {
				t.Errorf("getTemplateName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestIntent 测试 Intent 方法
func TestIntent(t *testing.T) {
	s := &Summarizer{}
	got := s.Intent()
	// summarizer 作为主入口，Intent 返回空切片
	if len(got) != 0 {
		t.Errorf("Summarizer.Intent() should return empty slice, got %v", got)
	}
}
