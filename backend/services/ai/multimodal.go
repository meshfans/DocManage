package ai

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 多模态检测服务
// 参考 DocSmart v1.8.11+：
//   - 双保险判断：OK 单词边界匹配 + OCR 期望词 "MULTIMODAL" 匹配
//   - 三态语义：true/false/null
//     true  = 模型支持 vision
//     false = 模型明确不支持（含已知错误关键词命中）
//     null  = 检测不确定（不写 DB，前端显示 ⚠️）
//   - M3 内容审核规避：使用真实可见文字图（512x160 "MULTIMODAL" PNG），避免 1×1 透明图触发敏感词

// ProbePNGBase64 512x160 PNG（白底黑字含可识别英文单词 "MULTIMODAL"）
//   - v1.8.11+ Python PIL 生成，CRC 正确
//   - 防止内容审核拒收 + OCR 准确率高
//   - 生成器：update_go_base64.py
//   - 双份事实：与 Python 生成器同源，修改时需同步更新
const ProbePNGBase64 = "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"

// probePNG 当前使用的探测图
var probePNG = ProbePNGBase64

// TestPrompt 多模态探测 prompt
//   - 主：强制读取图片中的目标词，避免纯文本模型忽略图片后直接回复 OK 的假阳性
//   - 副：双保险 prompt
const TestPrompt = "Read the uppercase word shown in the attached image. " +
	"If and only if the word is MULTIMODAL, reply with exactly OK. " +
	"Otherwise reply with exactly UNREADABLE. " +
	"Do not describe the image. Do not add explanations, punctuation, or any other text."

// probeExpectedWord OCR 期望词
const probeExpectedWord = "MULTIMODAL"

// okWordRegex OK 单词边界匹配（不区分大小写）
var okWordRegex = regexp.MustCompile(`(?i)\bok\b`)

// IsProbeSuccess 判断响应是否通过检测（双保险）
//   - 主：OK 单词边界匹配（避免 "tokyo" 误判，大小写不敏感）
//   - 副：OCR 期望词匹配（允许部分 OCR 偏差：大小写 + 忽略空格/标点）
//   - 任一通过即返回 true
func IsProbeSuccess(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	// OK 单词边界匹配（不区分大小写）
	if okWordRegex.MatchString(trimmed) {
		return true
	}
	// OCR 期望词匹配（normalizeOCR 会转大写）
	normalized := normalizeOCR(trimmed)
	if strings.Contains(normalized, probeExpectedWord) {
		return true
	}
	return false
}

// normalizeOCR 去标点 + 转大写
func normalizeOCR(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// KnownNoVisionKeywords 模型明确不支持 vision 的错误关键词
//   - v1.8.10+ 实测各 provider 真实错误格式
//   - 故意去掉 "image" / "multimodal"（M3 内容审核误报会触发）
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

// MultimodalTestResult 多模态检测结果
//   - Supported: true/false，nil 由调用方自行判空（不写 DB）
type MultimodalTestResult struct {
	Supported   *bool  `json:"supported"`
	Message     string `json:"message"`
	FullMessage string `json:"full_message"`
	LatencyMs   int    `json:"latencyMs"`
}

// TestMultimodal 执行多模态检测
//   - 返回 *bool 类型（nil 表示检测不确定，不写 DB）
func TestMultimodal(adapter ProviderAdapter) *MultimodalTestResult {
	t0 := time.Now()

	images := []ChatImage{
		{Base64: probePNG, Mime: "image/png"},
	}

	req := &ChatRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: TestPrompt},
		},
		Images:    images,
		MaxTokens: ptrIntLocal(1024),
		TimeoutMs: 60000,
	}

	resp, err := adapter.Chat(req)
	latencyMs := int(time.Since(t0).Milliseconds())

	if err != nil {
		msg := err.Error()

		if IsKnownNoVisionError(msg) {
			f := false
			return &MultimodalTestResult{
				Supported:   &f,
				Message:     "模型明确不支持：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "）",
				FullMessage: msg,
				LatencyMs:   latencyMs,
			}
		}

		if strings.Contains(strings.ToLower(msg), "sensitive") {
			return &MultimodalTestResult{
				Supported:   nil,
				Message:     "内容审核拒收：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "；这是图的问题，请右键手动标注）",
				FullMessage: msg,
				LatencyMs:   latencyMs,
			}
		}

		return &MultimodalTestResult{
			Supported:   nil,
			Message:     "检测不确定：" + truncate(msg, 200) + "（" + ms2str(latencyMs) + "；请检查 API Key/网络）",
			FullMessage: msg,
			LatencyMs:   latencyMs,
		}
	}

	text := strings.TrimSpace(resp.Text)

	if IsProbeSuccess(text) {
		normalized := normalizeOCR(text)
		isOcr := strings.Contains(normalized, probeExpectedWord)
		msg := "检测通过："
		if isOcr {
			msg += "OCR 识别到「MULTIMODAL」（" + ms2str(latencyMs) + "）"
		} else {
			msg += "模型返回 OK（" + ms2str(latencyMs) + "）"
		}
		t := true
		return &MultimodalTestResult{
			Supported:   &t,
			Message:     msg,
			FullMessage: text,
			LatencyMs:   latencyMs,
		}
	}

	return &MultimodalTestResult{
		Supported:   nil,
		Message:     "响应不含 OK/MULTIMODAL：模型说「" + truncate(text, 60) + "」（" + ms2str(latencyMs) + "；请右键手动标注）",
		FullMessage: text,
		LatencyMs:   latencyMs,
	}
}

func ptrIntLocal(v int) *int {
	return &v
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
