package sdk

import "sync/atomic"

// ==================== 全局 startup outcome（2026-09-28 RAG Feature Gate）====================
//
// 设计：
//   - main.go 在 VerifyStartupOrLog 成功后立刻 SetGlobalOutcome(licOutcome)
//   - middleware / cron handler 通过 GlobalHasFeature(key) / GlobalOutcome() 读
//   - 启动期（中间件尚未挂）其它 goroutine 不应触发读；读到的 nil 视同"未授权"
//
// 并发：atomic.Pointer 提供读无锁、写单写无锁。若未来校验流程可变，可替换 sync.RWMutex。

// globalOutcome 启动期一次性 set，启动后只读。
var globalOutcome atomic.Pointer[StartupOutcome]

// SetGlobalOutcome 把 startup outcome 写到全局。
// 设计：只应在 main.go 启动序列一次性调用。
// 并发：atomic.Pointer.Store 线程安全；多次 Store 视为"以最新一次为准"（启动期通常只一次）。
func SetGlobalOutcome(o *StartupOutcome) {
	globalOutcome.Store(o)
}

// GlobalOutcome 取当前 startup outcome（atomic load）。
// 返回 nil 只可能发生在 SetGlobalOutcome 被调用之前（启动期极短窗口）。
//
// 2026-09-29 硬切后 LicenseKey 为空已改为 Fatal 退出，因此进程一旦进入
// 服务阶段，全局值恒非 nil。
func GlobalOutcome() *StartupOutcome {
	return globalOutcome.Load()
}

// GlobalHasFeature 便捷函数：调 HasFeature(GlobalOutcome(), key)。
// 行为：GlobalOutcome() == nil → false（详见 HasFeature 注释）。
func GlobalHasFeature(key string) bool {
	return HasFeature(GlobalOutcome(), key)
}
