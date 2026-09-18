package llm

import (
	"sync/atomic"
)

// Metrics LLM 指标
type Metrics struct {
	// 请求计数
	RequestsTotal   atomic.Uint64
	RequestsSuccess atomic.Uint64
	RequestsFailed  atomic.Uint64

	// Token 计数
	TokensPrompt     atomic.Uint64
	TokensCompletion atomic.Uint64

	// 延迟（毫秒）
	LatencySumMs atomic.Uint64
	LatencyCount atomic.Uint64
}

// 全局指标实例
var metrics = &Metrics{}

// IncRequestsTotal 增加总请求数
func (m *Metrics) IncRequests() {
	m.RequestsTotal.Add(1)
}

// IncSuccess 增加成功数
func (m *Metrics) IncSuccess(tokensPrompt, tokensCompletion int) {
	m.RequestsSuccess.Add(1)
	m.TokensPrompt.Add(uint64(tokensPrompt))
	m.TokensCompletion.Add(uint64(tokensCompletion))
}

// IncFailed 增加失败数
func (m *Metrics) IncFailed() {
	m.RequestsFailed.Add(1)
}

// RecordLatency 记录延迟
func (m *Metrics) RecordLatency(latencyMs int64) {
	m.LatencySumMs.Add(uint64(latencyMs))
	m.LatencyCount.Add(1)
}

// GetStats 获取指标快照
func (m *Metrics) GetStats() map[string]any {
	return map[string]any{
		"requests_total":      m.RequestsTotal.Load(),
		"requests_success":   m.RequestsSuccess.Load(),
		"requests_failed":     m.RequestsFailed.Load(),
		"tokens_prompt":       m.TokensPrompt.Load(),
		"tokens_completion":   m.TokensCompletion.Load(),
		"avg_latency_ms":      m.getAvgLatency(),
	}
}

func (m *Metrics) getAvgLatency() int64 {
	count := m.LatencyCount.Load()
	if count == 0 {
		return 0
	}
	return int64(m.LatencySumMs.Load() / count)
}

// GetMetrics 获取全局指标
func GetMetrics() *Metrics {
	return metrics
}

// RecordLLMCall 记录一次 LLM 调用
func RecordLLMCall(success bool, tokensPrompt, tokensCompletion int, latencyMs int64) {
	metrics.IncRequests()
	if success {
		metrics.IncSuccess(tokensPrompt, tokensCompletion)
	} else {
		metrics.IncFailed()
	}
	metrics.RecordLatency(latencyMs)
}
