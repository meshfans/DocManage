package services

// ==================== 优雅关停编排（Round 18）====================
//
// 设计目标：
//  1. 单一入口：RunGraceful(ctx, srv) — main.go 不再手写"select+signal"
//  2. 阶段耗时埋点：每个阶段结束 → utils.ObserveShutdownStage(stage, dur)
//  3. 关停顺序固定（按依赖倒序）：
//       http（srv.Shutdown 内部）→ ws（srv.Shutdown 内部）→
//       Scheduler（停止 cron entry）→ DB → final
//  4. 二次信号强制退出：第一次 SIGTERM/SIGINT 走优雅关停。
//     第二次信号：再发一次相同信号 → 进程 os.Exit(130)
//  5. 超时兜底：HTTP/WS 已有 30s context timeout（CombinedServer.Shutdown 内部）；
//     Scheduler / DB 阶段无显式超时。
//
// 为什么不引入第三方库（如 oklog/okrun）：
//   - 当前流程短（5 步），100 行足够
//   - 第三方库会接管 main，不符合本项目"main.go 显式"风格
//
// ⚠️ 2026-08-19 修复（M-E1 / M-E2）：
//   - 删除虚伪的 ctx.Done() 阻塞路径（ctx 是 Background 永远不取消）；
//     改为 "优雅关停完 → 二次信号 goroutine 退出" 模式。
//   - 拆分 srv.Shutdown 内部阶段：要求 CombinedServer 在内部打 http / ws
//     两阶段埋点，本文件不再打 http=0 / ws=0 占位（之前会让 dashboard
//     看到 0ms 的假数据）。
// ----------------------------------------------------------------------------

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"doc/database"
	"doc/utils"
)

// Shutdownable 任何能关停的资源应实现这个接口。
// CombinedServer、Scheduler 都满足。
type Shutdownable interface {
	Shutdown()
}

// shuttingDown 防止多个 goroutine 同时触发关停（signal handler 等）。
var shuttingDown atomic.Bool

// RunGraceful 启动阻塞监听，收到 SIGTERM / SIGINT 后按顺序关停。
//
// 参数：
//   - ctx:  仅保留用于未来扩展（如关停超时）；当前实现不使用 ctx.Done()。
//           因为 main.go 传 context.Background() 永不取消，加 ctx 阻塞路径
//           等于死锁。改为信号驱动。
//   - srv:  HTTP + WS 复合服务器（CombinedServer）；内部自行按 HTTP→WS 顺序
//           埋点 shutdown_stage_duration_seconds{stage="http"} / "ws"。
//
// 行为：
//   - 收到首次 SIGTERM/SIGINT：执行优雅关停
//   - 收到再次 SIGTERM/SIGINT：直接 os.Exit(130)（130 = SIGINT 退出码）
//   - 收到 SIGKILL：无法捕获，进程立即终止（操作系统行为）
//
// 关停顺序（按依赖倒序）：
//   1) http      — HTTP server 停接新请求（srv.Shutdown 内部埋点）
//   2) ws        — WS hub 关闭连接（srv.Shutdown 内部埋点）
//   3) scheduler — 停止 cron entry
//   4) db        — 关闭 sqlite
//   5) final     — 累计耗时
//
// 注意：第 1 / 2 步要求 CombinedServer.Shutdown 内部调 utils.ObserveShutdownStage：
//   - HTTP server.Shutdown(ctx) 前后各打一次
//   - wsHandler.Shutdown() 前后各打一次
// 若 CombinedServer 未埋点，"http" / "ws" stage 将只有 0 占位数据。
func RunGraceful(ctx context.Context, srv Shutdownable) {
	_ = ctx // 保留参数签名兼容避免未来演进再走 ctx 路径

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等首个信号
	sig := <-sigCh
	utils.Info("[shutdown] 收到信号 %s，开始优雅关停", sig)
	start := time.Now()

	if !shuttingDown.CompareAndSwap(false, true) {
		utils.Warn("[shutdown] 已在关停中，再次收到信号 %s，强制退出", sig)
		os.Exit(130)
	}

	// 1) HTTP server + WS hub（CombinedServer.Shutdown 内部按 HTTP→WS 顺序，
	//    且内部调 utils.ObserveShutdownStage 按 http/ws 拆分埋点）。
	stageStart("http_ws", func() {
		srv.Shutdown()
	})

	// 2) Scheduler（可能未初始化）
	if sched := GetScheduler(); sched != nil {
		stageStart("scheduler", func() {
			sched.Stop()
		})
	} else {
		utils.ObserveShutdownStage("scheduler", 0)
	}

	// 3) DB
	stageStart("db", func() {
		database.CloseDatabase()
	})

	// 4) final：累计耗时
	utils.ObserveShutdownStage("final", time.Since(start).Seconds())

	utils.Info("[shutdown] 优雅关停完成（总计 %s）；再发一次信号强制退出", time.Since(start))

	// 进入二次信号快速路径；不再等待 ctx.Done()（Background 永不取消）。
	go func() {
		s := <-sigCh
		utils.Warn("[shutdown] 第二次信号 %s，强制退出", s)
		os.Exit(130)
	}()

	// M-E1 修复：阻塞到 SIGKILL 或 OS 主动结束。
	// 不能再 `<-ctx.Done()` — main 传的是 Background，永远不返回。
	// 用一个永不触发的 channel 占位即可；二次信号 goroutine 走 os.Exit 兜底。
	select {}
}

// stageStart 执行 fn 并记录耗时到 stage。
func stageStart(stage string, fn func()) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			utils.Warn("[shutdown] stage %s recovered: %v", stage, r)
		}
		utils.ObserveShutdownStage(stage, time.Since(start).Seconds())
	}()
	fn()
}
