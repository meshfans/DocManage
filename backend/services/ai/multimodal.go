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
//   - v1.8.8.1 替代 1×1 透明图，v1.8.11 升级版（512x160 同源）
//   - 防止内容审核拒收 + OCR 准确率高
//   - 生成器：tools/multimodal-probe/generate-probe-png.cjs
//   - 双份事实：与 generate-probe-png.cjs 同源，修改时需同步更新
//   - 占位字符：实际生产可替换为 generate-probe-png.cjs 输出的真图
const ProbePNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAQAAAACgCAIAAACVvRQ4AAAAGklEQVRYhe3BAQ0AAADCoPdPbQ8HFAAAvA0gAAGR1VmVAAAAAElFTkSuQmCC"

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

// okWordRegex OK 单词边界匹配
var okWordRegex = regexp.MustCompile(`\bok\b`)

// IsProbeSuccess 判断响应是否通过检测（双保险）
//   - 主：OK 单词边界匹配（避免 "tokyo" 误判）
//   - 副：OCR 期望词匹配（允许部分 OCR 偏差：大小写 + 忽略空格/标点）
//   - 任一通过即返回 true
func IsProbeSuccess(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if okWordRegex.MatchString(trimmed) {
		return true
	}
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