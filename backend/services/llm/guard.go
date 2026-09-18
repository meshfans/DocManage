package llm

import (
	"context"
	"log"
	"regexp"
)

// Guard 防护层
type Guard struct {
	cfg       GuardConfig
	maskRegex []*regexp.Regexp
	logger    AuditLogger
}

// GuardConfig 防护配置
type GuardConfig struct {
	EnableMasking bool     `json:"enable_masking"` // 脱敏
	EnableAudit   bool     `json:"enable_audit"`   // 审计
	EnableHarm    bool     `json:"enable_harm"`    // 敏感词
	MaskPatterns  []string `json:"mask_patterns"` // 脱敏正则
}

// NewGuard 创建防护层
func NewGuard(cfg GuardConfig) *Guard {
	g := &Guard{cfg: cfg, logger: &DefaultAuditLogger{}}
	// 编译脱敏正则
	for _, pattern := range cfg.MaskPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			g.maskRegex = append(g.maskRegex, re)
		}
	}
	return g
}

// Mask 脱敏处理
func (g *Guard) Mask(text string) string {
	if !g.cfg.EnableMasking || text == "" {
		return text
	}
	result := text
	for _, re := range g.maskRegex {
		result = re.ReplaceAllString(result, "***")
	}
	return result
}

// AuditEvent LLM 调用审计事件（存储 Token 数，不存原始内容）
type AuditEvent struct {
	UserID        int64  // 用户ID
	Module       string // 模块名：summarize/generate
	Intent       string // 识别的意图
	Template     string // 使用的模板名
	InputTokens  int    // 输入 Token 数
	OutputTokens int    // 输出 Token 数
	TotalTokens  int    // 总 Token 数
	LatencyMs    int64  // 响应耗时ms
	Model        string // 调用的模型名
	Scene        string // 业务场景
	Status       string // ok/error/timeout
	ErrorMsg     string // 错误信息（脱敏后）
}

// AuditLogger 审计日志接口
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent)
}

// DefaultAuditLogger 默认审计日志（输出到标准日志）
type DefaultAuditLogger struct{}

func (l *DefaultAuditLogger) Log(ctx context.Context, event AuditEvent) {
	// 记录到标准日志
	log.Printf("[LLM Audit] user=%d module=%s template=%s tokens=%d latency=%dms status=%s",
		event.UserID, event.Module, event.Template, event.TotalTokens, event.LatencyMs, event.Status)
}

// Audit 记录审计日志
func (g *Guard) Audit(ctx context.Context, event AuditEvent) {
	if !g.cfg.EnableAudit || g.logger == nil {
		return
	}
	g.logger.Log(ctx, event)
}

// SetLogger 设置审计日志器
func (g *Guard) SetLogger(logger AuditLogger) {
	g.logger = logger
}

// IsHarmful 检测敏感内容
func (g *Guard) IsHarmful(text string) bool {
	if !g.cfg.EnableHarm || text == "" {
		return false
	}
	// TODO: 实现敏感词检测
	return false
}

// DefaultGuardPatterns 默认脱敏正则
var DefaultGuardPatterns = []string{
	`\b1[3-9]\d{9}\b`,                      // 手机号
	`\b\d{17}[\dXx]\b`,                      // 身份证号
	`\b\d{16,19}\b`,                        // 银行卡号
	`\b\d{3,4}[-\s]?\d{3,4}[-\s]?\d{3,4}\b`, // 固话
}

// NewDefaultGuard 创建默认防护层
func NewDefaultGuard() *Guard {
	return NewGuard(GuardConfig{
		EnableMasking: true,
		EnableAudit:   true,
		EnableHarm:    true,
		MaskPatterns:  DefaultGuardPatterns,
	})
}
