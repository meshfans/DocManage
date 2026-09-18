package llm

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// DBAuditLogger 数据库审计日志器
type DBAuditLogger struct {
	db *sql.DB
}

// NewDBAuditLogger 创建数据库审计日志器
func NewDBAuditLogger(db *sql.DB) *DBAuditLogger {
	return &DBAuditLogger{db: db}
}

// Log 写入审计日志到数据库（失败重试 1 次）
func (l *DBAuditLogger) Log(ctx context.Context, event AuditEvent) {
	l.logWithRetry(ctx, event, 2)
}

func (l *DBAuditLogger) logWithRetry(ctx context.Context, event AuditEvent, retries int) {
	for i := 0; i < retries; i++ {
		_, err := l.db.ExecContext(ctx, `
			INSERT INTO llm_audit_log
			(user_id, module, intent, template, input_tokens, output_tokens, total_tokens, latency_ms, model, scene, status, error_msg, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			event.UserID,
			event.Module,
			event.Intent,
			event.Template,
			event.InputTokens,
			event.OutputTokens,
			event.TotalTokens,
			event.LatencyMs,
			event.Model,
			event.Scene,
			event.Status,
			// ErrorMsg 限制长度 500 字符
			truncateStr(event.ErrorMsg, 500),
			time.Now().Unix(),
		)
		if err == nil {
			return
		}
		log.Printf("[LLM Audit] write failed (attempt %d/%d): %v", i+1, retries, err)
	}
}

// truncateStr 字符串截断
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return s
}
