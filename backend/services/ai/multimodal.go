package ai

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 多模态检测服务
//   - 方案A：元数据硬编码（MiniMax-M3 固定支持图片，无需探测）
//   - 方案B：API 运行时探测（其他 provider 使用 1x1 PNG 探针测试）
//   - 三态语义：true/false/null
//     true  = 模型支持 vision
//     false = 模型明确不支持
//     null  = 检测不确定（不写 DB，前端显示 ⚠️）

// ProbePNG1x1Base64 1x1 透明 PNG base64（MiniMax 推荐用于探测）
//   - 极小体积（≈70 bytes），减少网络开销
//   - 去掉 data:image/xxx;base64, 前缀
const ProbePNG1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFeAJg2D9u/gAAAABJRU5ErkJggg=="

// MiniMax 模型能力表（Anthropic 兼容接口）
// M3 支持图片，M2.7 及以下不支持
var minimaxMultimodalModels = map[string]bool{
	"minimax-m3":          true, // 支持 image + video
	"minimax-m3-thinker":  true,
}

// KnownMultimodalModels 已知支持多模态的模型
// 注意：key 必须与 frontend/src/types/ai-presets.ts 中的 model.key 一致
var knownMultimodalModels = map[string]bool{
	// DeepSeek
	"deepseek-v4-pro":    true,
	"deepseek-v4-flash":  true,
	// 智谱
	"glm-5": true, // GLM-5 支持图片
	// Qwen
	"qwen3.7-max":   true,
	"qwen3.7-plus":  true,
	"qwen3.6-flash": true,
	// Kimi
	"kimi-k2.6": true,
	"kimi-k2.5": true,
	// 豆包（key 与 ai-presets.ts 一致）
	"doubao-seed-2-1-pro-260628": true,
	"doubao-seed-2-0-lite":       true,
	// OpenAI
	"gpt-5.5": true,
	"gpt-4o":  true,
	// Claude
	"claude-opus-4-8": true,
	"claude-sonnet-5":  true,
	// Gemini
	"gemini-2.5-pro":   true,
	"gemini-2.5-flash": true,
	// 硅基流动
	"deepseek-ai/DeepSeek-V4": true, // DeepSeek V4 支持 vision
}

// KnownNoVisionModels 不支持多模态的模型前缀
var knownNoVisionModels = []string{
	"glm-4.7-flash", // 智谱 Flash 版本不支持
	"glm-4-flash",   // 智谱旧版 Flash 不支持
	"glm-4-air",     // 智谱 Air 版本不支持
	"minimax-m2.7",  // MiniMax M2.7 不支持图片
}

// IsKnownMultimodalByMetadata 根据模型元数据判断是否支持多模态
// 返回值：true=支持，false=不支持，nil=未知需探测
func IsKnownMultimodalByMetadata(provider, modelName string) *bool {
	provider = strings.ToLower(provider)
	modelName = strings.ToLower(modelName)

	// MiniMax Anthropic 兼容接口
	if provider == "minimax" {
		if supported, ok := minimaxMultimodalModels[modelName]; ok {
			return &supported
		}
		// 未知的 MiniMax 模型，保守返回不支持
		f := false
		return &f
	}

	// 检查已知支持多模态的模型
	if supported, ok := knownMultimodalModels[modelName]; ok {
		return &supported
	}

	// 检查已知不支持的模型
	for _, prefix := range knownNoVisionModels {
		if strings.HasPrefix(modelName, prefix) {
			f := false
			return &f
		}
	}

	// 其他模型需要探测
	return nil
}

// okWordRegex OK 单词边界匹配（不区分大小写）
var okWordRegex = regexp.MustCompile(`(?i)\bok\b`)

// TestPrompt 多模态探测 prompt
const TestPrompt = "Read the uppercase word shown in the attached image. " +
	"If and only if the word is MULTIMODAL, reply with exactly OK. " +
	"Otherwise reply with exactly UNREADABLE. " +
	"Do not describe the image. Do not add explanations, punctuation, or any other text."

// KnownNoVisionKeywords 模型明确不支持 vision 的错误关键词
var KnownNoVisionKeywords = []string{
	"vision",
	"doesn't support image",
	"does not support image",
	"does not support images",
	"does not support multimodal",
	"not support image",
	"not support images",
	"unsupported image",
	"invalid image format",
	"no image support",
	"model does not support",
	"multimodal does not support",
	"multimodal is not supported",
}

// IsProbeSuccess 判断响应是否通过检测
func IsProbeSuccess(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	// OK 单词边界匹配（不区分大小写）
	if okWordRegex.MatchString(trimmed) {
		return true
	}
	return false
}

// IsKnownNoVisionError 检测错误消息是否为已知「不支持 vision」错误
func IsKnownNoVisionError(msg string) bool {
	lower := strings.ToLower(msg)
	for _, kw := range KnownNoVisionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// TestMultimodal 执行多模态检测
//   - 优先使用元数据快速判断（MiniMax-M3 等）
//   - 无法判断时进行 API 探测
//   - 返回 *bool 类型（nil 表示检测不确定，不写 DB）
func TestMultimodal(adapter ProviderAdapter) *MultimodalResult {
	// 获取 provider 和 modelName（如果有）
	var provider, modelName string
	if p, ok := adapter.(interface{ Provider() string }); ok {
		provider = p.Provider()
	}
	if m, ok := adapter.(interface{ ModelName() string }); ok {
		modelName = m.ModelName()
	}

	// 方案A：元数据快速判断
	if provider != "" && modelName != "" {
		if result := IsKnownMultimodalByMetadata(provider, modelName); result != nil {
			if *result {
				return &MultimodalResult{
					Supported: result,
					LatencyMs: 0,
					Message:   "模型元数据判定：" + modelName + " 支持图片输入",
				}
			} else {
				return &MultimodalResult{
					Supported: result,
					LatencyMs: 0,
					Message:   "模型元数据判定：" + modelName + " 不支持图片输入",
				}
			}
		}
	}

	// 方案B：API 探测（使用 1x1 透明 PNG）
	t0 := time.Now()

	images := []ChatImage{
		{Base64: ProbePNG1x1Base64, Mime: "image/png"},
	}

	req := &ChatRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: TestPrompt},
		},
		Images:    images,
		MaxTokens: ptrInt(1024),
		TimeoutMs: 60000,
	}

	resp, err := adapter.Chat(req)
	latencyMs := int(time.Since(t0).Milliseconds())

	if err != nil {
		msg := err.Error()

		if IsKnownNoVisionError(msg) {
			f := false
			return &MultimodalResult{
				Supported: &f,
				Message:   "模型明确不支持：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "）",
			}
		}

		if strings.Contains(strings.ToLower(msg), "sensitive") {
			return &MultimodalResult{
				Supported: nil,
				Message:   "内容审核拒收：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "）",
			}
		}

		// 1033 等系统错误视为检测不确定
		return &MultimodalResult{
			Supported: nil,
			Message:   "检测不确定：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "）",
		}
	}

	text := strings.TrimSpace(resp.Text)

	if IsProbeSuccess(text) {
		msg := "检测通过：模型返回 OK（" + ms2str(latencyMs) + "）"
		t := true
		return &MultimodalResult{
			Supported: &t,
			Message:   msg,
			LatencyMs: latencyMs,
		}
	}

	return &MultimodalResult{
		Supported: nil,
		Message:   "响应不含 OK：" + truncate(text, 60) + "（" + ms2str(latencyMs) + "）",
		LatencyMs: latencyMs,
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func ms2str(ms int) string {
	return strconv.Itoa(ms) + "ms"
}
