package debug

import "time"

// Clock 是时间源抽象（#9 修复：替代全局 nowUnix 变量，避免 t.Parallel 测试数据竞争）。
//
// 实现：
//   - SystemClock：默认实现，调用 time.Now().Unix()
//   - 测试场景：实现 NowUnix() 返回固定值
//
// 注入方式：通过 DebugConfig.Clock 字段（详见 envelope.go）。
type Clock interface {
	NowUnix() int64
}

// SystemClock 默认时间源实现，返回真实系统时间。
//
// 进程启动后由 main.go 注入；测试中可替换为 mock clock。
type SystemClock struct{}

func (SystemClock) NowUnix() int64 { return time.Now().Unix() }

// IsInWindow 判定 now 是否在 [start, end] 闭区间内（端点包含）。
//
// 返回值：
//   - true  : now ∈ [start, end]，debug 模式生效
//   - false : now < start 或 now > end，debug 模式关闭
//
// 端点语义：
//   - start == 0（零值，未设置）→ 返回 false（防呆）
//   - end == 0（零值，未设置）→ 返回 false（强制要求窗口边界）
//   - end < start（窗口反向）→ 返回 false（视为非法窗口）
//
// 入参全部为 Unix 秒（避免时区/格式解析副作用在判定环节）。
//
// 导出原因：提供给测试与外部 mock clock 实现方直接使用。
func IsInWindow(now, start, end int64) bool {
	if start == 0 || end == 0 {
		return false
	}
	if end < start {
		return false
	}
	if now < start {
		return false
	}
	if now > end {
		return false
	}
	return true
}