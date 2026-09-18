package llmclient

import (
	"fmt"
	"strings"
)

// APIError API 错误
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

// stripThinking 移除 <thinking> 块（Claude 等推理模型）
func stripThinking(text string) string {
	for {
		start := strings.Index(text, "<thinking>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</thinking>")
		if end == -1 {
			break
		}
		text = text[:start] + text[end+len("</thinking>"):]
	}
	return strings.TrimSpace(text)
}
