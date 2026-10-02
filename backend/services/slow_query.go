package services

// 慢查询监控服务
//
// 功能：
//   - 记录执行时间超过阈值的 SQL 查询
//   - 在 /metrics 端点暴露慢查询统计
//   - 便于运维发现需要优化的查询

import (
	"sync/atomic"
	"time"

	"doc/utils"
)

// SlowQueryThresholdMs 慢查询阈值（毫秒）
const SlowQueryThresholdMs = 1000 // 1秒

var (
	// 慢查询计数器（atomic，无锁）
	slowQueryCount atomic.Int64
	// 最近一次慢查询耗时（毫秒）
	slowQueryLastMs atomic.Int64
	// DB 查询总次数
	dbQueryCount atomic.Int64
	// DB 查询总耗时（毫秒）
	dbQueryTotalMs atomic.Int64
	// DB 查询最长耗时（毫秒）
	dbQueryMaxMs atomic.Int64

	// 用于暴露 metrics 的 Gauge
	slowQueryGauge    *utils.Gauge
	dbQueryAvgGauge   *utils.Gauge
	dbQueryMaxGauge   *utils.Gauge
)

func init() {
	// 注册慢查询相关指标
	slowQueryGauge = utils.RegisterGauge("db_slow_queries_total", "数据库慢查询累计次数（超过1秒）")
	dbQueryAvgGauge = utils.RegisterGauge("db_query_avg_ms", "数据库平均查询耗时（毫秒）")
	dbQueryMaxGauge = utils.RegisterGauge("db_query_max_ms", "数据库最长查询耗时（毫秒）")
}

// RecordSlowQuery 记录一次慢查询
func RecordSlowQuery(query string, durationMs int64) {
	if durationMs >= SlowQueryThresholdMs {
		slowQueryCount.Add(1)
		slowQueryLastMs.Store(durationMs)
		// 更新最大值
		for {
			current := dbQueryMaxMs.Load()
			if durationMs <= current {
				break
			}
			if dbQueryMaxMs.CompareAndSwap(current, durationMs) {
				break
			}
		}
		utils.Info("[SLOW_QUERY] duration=%dms threshold=%dms query=%s",
			durationMs, SlowQueryThresholdMs, truncateQuery(query))
	}
}

// RecordQuery 记录一次数据库查询
func RecordQuery(durationMs int64) {
	dbQueryCount.Add(1)

	// 原子更新总耗时
	for {
		oldTotal := dbQueryTotalMs.Load()
		newTotal := oldTotal + durationMs
		if dbQueryTotalMs.CompareAndSwap(oldTotal, newTotal) {
			break
		}
	}

	// 更新最大值
	for {
		current := dbQueryMaxMs.Load()
		if durationMs <= current {
			break
		}
		if dbQueryMaxMs.CompareAndSwap(current, durationMs) {
			break
		}
	}

	// 如果超过阈值，记录慢查询
	if durationMs >= SlowQueryThresholdMs {
		slowQueryCount.Add(1)
		slowQueryLastMs.Store(durationMs)
	}

	// 更新 metrics gauge
	updateQueryMetrics()
}

// updateQueryMetrics 更新查询 metrics
func updateQueryMetrics() {
	count := dbQueryCount.Load()
	totalMs := dbQueryTotalMs.Load()
	maxMs := dbQueryMaxMs.Load()

	// 更新慢查询计数
	if slowQueryGauge != nil {
		slowQueryGauge.Set(float64(slowQueryCount.Load()))
	}

	// 更新平均耗时
	if count > 0 && dbQueryAvgGauge != nil {
		avgMs := float64(totalMs) / float64(count)
		dbQueryAvgGauge.Set(avgMs)
	}

	// 更新最长耗时
	if dbQueryMaxGauge != nil {
		dbQueryMaxGauge.Set(float64(maxMs))
	}
}

// SlowQueryCount 返回累计慢查询次数
func SlowQueryCount() int64 {
	return slowQueryCount.Load()
}

// SlowQueryLastMs 返回最近一次慢查询耗时（毫秒）
func SlowQueryLastMs() int64 {
	return slowQueryLastMs.Load()
}

// DBQueryCount 返回累计查询次数
func DBQueryCount() int64 {
	return dbQueryCount.Load()
}

// DBQueryAvgMs 返回平均查询耗时（毫秒）
func DBQueryAvgMs() float64 {
	count := dbQueryCount.Load()
	if count == 0 {
		return 0
	}
	return float64(dbQueryTotalMs.Load()) / float64(count)
}

// DBQueryMaxMs 返回最长查询耗时（毫秒）
func DBQueryMaxMs() int64 {
	return dbQueryMaxMs.Load()
}

// truncateQuery 截断过长的查询语句，便于日志显示
func truncateQuery(query string) string {
	if len(query) > 200 {
		return query[:200] + "..."
	}
	return query
}

// ObserveDBDuration 观察数据库查询耗时（用于 metrics）
// 返回值表示是否超过阈值
func ObserveDBDuration(operation string, duration time.Duration) bool {
	ms := duration.Milliseconds()
	RecordQuery(ms)
	return ms >= SlowQueryThresholdMs
}
