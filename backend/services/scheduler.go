package services

import (
	"context"
	"doc/config"
	"doc/database"
	"doc/utils"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
)

// JobFunc 任务执行函数。
// 返回 (output, error)：
//   - output：截断后写入 scheduled_task_log.output
//   - error：非 nil 则任务视为 failed
type JobFunc func(ctx context.Context, params json.RawMessage) (string, error)

// HandlerInfo 描述一个可注册的处理函数（用于前端下拉）。
type HandlerInfo struct {
	Name string `json:"name"`
}

// Scheduler 集中式定时任务调度器。
// 设计要点：
//   - 任务定义存 DB，启动时按 is_active=1 加载
//   - 任务执行并发：同任务默认串行防重入；is_concurrent=1 才允许并发
//   - cron 表达式同时支持 5 段（Linux 标准）和 6 段（含秒）
//   - 优雅停机：cron.Stop() + waitGroup 等正在执行的任务结束
type Scheduler struct {
	cron        *cron.Cron
	parser5     cron.Parser
	parser6     cron.Parser
	mu          sync.RWMutex
	handlers    map[string]JobFunc     // handler_name -> func
	entries     map[int64]cron.EntryID // task_id -> cron EntryID
	running     map[int64]bool         // task_id -> 是否在执行
	wg          sync.WaitGroup         // 等正在执行的任务
	stopCh      chan struct{}
	location    *time.Location
	logRetent   int
	logOutputKB int

	// started/stopped 追踪运行态，供 health probe 查询
	started atomic.Bool
	stopped atomic.Bool
}

var globalScheduler *Scheduler

// InitScheduler 初始化并启动调度器。必须在 database.InitDatabase 之后调用。
func InitScheduler(cfg *config.Config) error {
	if !cfg.Scheduler.Enabled {
		utils.Info("定时任务调度器已禁用（scheduler.enabled=false）")
		return nil
	}

	loc, err := time.LoadLocation(cfg.Scheduler.Timezone)
	if err != nil {
		utils.Warn("加载时区失败: %v, 使用本机时区", err)
		loc = time.Local
	}

	// 5 段：Linux 标准（分 时 日 月 周）
	parser5 := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	// 6 段：含秒（秒 分 时 日 月 周）
	parser6 := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)

	s := &Scheduler{
		cron:        cron.New(cron.WithLocation(loc)),
		parser5:     parser5,
		parser6:     parser6,
		handlers:    make(map[string]JobFunc),
		entries:     make(map[int64]cron.EntryID),
		running:     make(map[int64]bool),
		stopCh:      make(chan struct{}),
		location:    loc,
		logRetent:   cfg.Scheduler.LogRetention,
		logOutputKB: cfg.Scheduler.LogOutputMaxKB,
	}
	// 注意：故意延后到 loadAllActive 成功后再赋值给 globalScheduler，
	// 避免部分初始化的 s 被 GetScheduler() 取到后 Stop() 触发 close(nil)/nil deref。
	// 但 cfg.Scheduler.Enabled=false 时 init 早 return，下面的 registerBuiltinHandlers
	// 不会被调用；这里用 prevScheduler / restore 模式做"原子提交"。
	prevScheduler := globalScheduler
	globalScheduler = s

	// 1. 注册内置 handler
	registerBuiltinHandlers(s, cfg)

	// 2. 启动时种子数据（幂等；任务 + 提醒模板一起处理）
	if err := seedDefaultScheduledTasks(); err != nil {
		utils.Warn("种子数据写入失败: %v", err)
	}

	// 3. 加载所有 active 任务到 cron
	if err := s.loadAllActive(); err != nil {
		// 加载失败：started 保持 false，Running() 返回 false；
		// 回退 globalScheduler 到原值，并把部分构造的 cron 停掉防止泄漏。
		globalScheduler = prevScheduler
		if ctx := s.cron.Stop(); ctx != nil {
			<-ctx.Done()
		}
		// Round 16：加载失败时也刷新一次指标，避免 scheduler_running 残留为 1
		utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())
		return fmt.Errorf("加载定时任务失败: %w", err)
	}

	// 4. 启动 cron
	s.cron.Start()
	s.started.Store(true)
	utils.Info("✅ 定时任务调度器启动成功（时区: %s, 日志保留: %d 条）", loc.String(), s.logRetent)

	// 5. 启动后台清理 goroutine（每天清一次）
	go s.cleanupLoop()

	// 6. Round 16：启动后立即刷新一次指标（让 /metrics 第一次响应即可见）
	utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())

	return nil
}

// GetScheduler 取全局实例。
func GetScheduler() *Scheduler {
	return globalScheduler
}

// Stop 优雅停机。幂等：重复调用不会触发 close(stopCh) panic。
func (s *Scheduler) Stop() {
	if s == nil || s.cron == nil {
		return
	}
	// 幂等守卫：CompareAndSwap 保证只有第一次调用真正进入停机流程，
	// 后续重复调用直接返回（健康探针/关闭路径可能都触发 Stop）。
	if !s.stopped.CompareAndSwap(false, true) {
		return
	}
	// stopCh 只由首次 Stop 关闭；cleanupLoop 的 select 收到信号即退出。
	close(s.stopCh)
	utils.Info("调度器停止中，等待正在执行的任务结束...")
	stopCtx := s.cron.Stop()
	// 等所有任务结束或超时（10 秒）
	select {
	case <-stopCtx.Done():
		utils.Info("cron 已停止")
	case <-time.After(10 * time.Second):
		utils.Warn("cron 停止超时（10s），强制退出")
	}
	s.wg.Wait()
	utils.Info("调度器已完全停止")
	// Round 16：停机后刷新指标，让 scheduler_* 在停止后可见为 0
	utils.RefreshSchedulerMetrics(false, s.ListEntriesLen(), s.RunningTaskCount())
}

// Running 返回 scheduler 是否"已初始化并已启动且尚未 Stop"。
//
//   - nil 接收者或未调用 InitScheduler() → false
//   - cfg.Scheduler.Enabled=false 时 InitScheduler 直接 return，started 不会被置 true → false
//   - Stop() 调用后 → false
//   - 启动后到 Stop() 之前 → true
//
// 使用 atomic.Bool 读写，并发安全。
func (s *Scheduler) Running() bool {
	if s == nil {
		return false
	}
	return s.started.Load() && !s.stopped.Load()
}

// ==================== Handler 注册 ====================

// RegisterHandler 注册一个任务处理函数。可重复注册同名（后者覆盖）。
func (s *Scheduler) RegisterHandler(name string, fn JobFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[name] = fn
}

// ListHandlers 返回已注册的 handler 列表（按名称排序，给前端下拉稳定顺序）。
func (s *Scheduler) ListHandlers() []HandlerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]HandlerInfo, 0, len(s.handlers))
	for name := range s.handlers {
		out = append(out, HandlerInfo{Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ==================== 任务生命周期 ====================

// loadAllActive 启动时调用：把 DB 中所有 is_active=1 的任务加入 cron。
func (s *Scheduler) loadAllActive() error {
	tasks, err := database.GetAllActiveScheduledTasks()
	if err != nil {
		return err
	}
	loaded := 0
	skipped := 0
	for i := range tasks {
		t := &tasks[i]
		if err := s.AddOrUpdateTask(t); err != nil {
			utils.Warn("加载任务 [%s] 失败: %v", t.TaskKey, err)
			skipped++
			continue
		}
		loaded++
	}
	utils.Info("调度器加载任务: %d 成功, %d 跳过", loaded, skipped)
	return nil
}

// AddOrUpdateTask 把任务加入/更新到 cron。
//   - 新建：分配 EntryID，记录到 entries map
//   - 更新：先 RemoveTask 再 AddTask
func (s *Scheduler) AddOrUpdateTask(t *database.ScheduledTask) error {
	if !t.IsActive {
		// 不启用：只保证不挂载
		s.RemoveTask(t.ID)
		return nil
	}
	schedule, err := s.parseSchedule(t.CronExpr)
	if err != nil {
		return fmt.Errorf("无效 cron 表达式 [%s]: %w", t.CronExpr, err)
	}
	if _, ok := s.handlers[t.HandlerName]; !ok {
		return fmt.Errorf("handler [%s] 未注册", t.HandlerName)
	}

	// 若已存在则先移除
	s.RemoveTask(t.ID)

	taskID := t.ID
	timeout := time.Duration(t.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	isConcurrent := t.IsConcurrent

	// 使用预解析的 schedule 绕过 cron 内部 parser（同时支持 5/6 段）
	entryID := s.cron.Schedule(schedule, cron.FuncJob(func() {
		s.executeOnce(taskID, t.TaskKey, t.HandlerName, t.HandlerParams, timeout, isConcurrent, "scheduler", 0)
	}))
	s.mu.Lock()
	s.entries[taskID] = entryID
	s.mu.Unlock()

	// Round 16：entry 注册后刷新指标（entries 数变化）
	utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())

	// 写 next_run_at（仅更新下次执行时间，不触碰 fail_count）
	// ⚠️ Bug 修复：原 UpdateScheduledTaskRunResult(..., false) 会被 reload 调用，
	// 导致 fail_count 持续增长（即使任务未执行）。
	if entry := s.cron.Entry(entryID); entry.ID != 0 {
		next := entry.Schedule.Next(time.Now().In(s.location))
		_ = database.UpdateScheduledTaskNextRun(taskID, next.Unix())
	}
	return nil
}

// RemoveTask 从 cron 移除任务。
func (s *Scheduler) RemoveTask(taskID int64) {
	s.mu.Lock()
	removed := false
	if entryID, ok := s.entries[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
		removed = true
	}
	s.mu.Unlock()
	if removed {
		// Round 16：entry 移除后刷新指标（entries 数变化）
		// 在锁外调用，避免 RefreshSchedulerMetrics 内部 RunningTaskCount 试图
		// 拿 s.mu.RLock() 时与本函数的 s.mu.Lock() 形成死锁。
		utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())
	}
}

// RunNow 立即触发一次（异步执行，立即返回 logID 给前端）。
func (s *Scheduler) RunNow(taskID int64, operatorID int64) (int64, error) {
	t, err := database.GetScheduledTaskByID(taskID)
	if err != nil {
		return 0, fmt.Errorf("任务不存在: %w", err)
	}
	if _, ok := s.handlers[t.HandlerName]; !ok {
		return 0, fmt.Errorf("handler [%s] 未注册", t.HandlerName)
	}
	timeout := time.Duration(t.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	go s.executeOnce(t.ID, t.TaskKey, t.HandlerName, t.HandlerParams, timeout, t.IsConcurrent, "manual", operatorID)
	return t.ID, nil
}

// ListEntries 返回当前 cron 的所有 entry（调试用）。
func (s *Scheduler) ListEntries() []cron.Entry {
	if s == nil || s.cron == nil {
		return nil
	}
	return s.cron.Entries()
}

// ListEntriesLen 返回当前 cron 的 entry 数（Round 16 指标刷新用）。
// 比直接 len(s.ListEntries()) 更省一次切片分配；nil-safe。
func (s *Scheduler) ListEntriesLen() int {
	if s == nil || s.cron == nil {
		return 0
	}
	return len(s.cron.Entries())
}

// RunningTaskCount 返回当前正在执行的任务数（Round 16 指标刷新用）。
//
// 并发安全：在 s.mu 下读取 running map 长度。注意：
//   - 仅 is_concurrent=0 的任务会写入 running map（防重入锁）
//   - is_concurrent=1 的任务并发执行但不会出现在 running map 中
//   - 这与设计意图一致：单点任务 = 串行 = 可观测；并发任务不计入
func (s *Scheduler) RunningTaskCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.running)
}

// ==================== 内部：执行一次任务 ====================

func (s *Scheduler) executeOnce(taskID int64, taskKey, handlerName string, paramsJSON string, timeout time.Duration, isConcurrent bool, triggeredBy string, operatorID int64) {
	// 防重入检查：先在锁内一次性确定 "是否可执行"
	canRun := isConcurrent
	s.mu.Lock()
	if !isConcurrent {
		if s.running[taskID] {
			s.mu.Unlock()
			utils.Warn("任务 [%s] 正在执行中，本次跳过", taskKey)
			now := time.Now().Unix()
			_, _ = database.CreateScheduledTaskLog(&database.ScheduledTaskLog{
				TaskID:      taskID,
				TaskKey:     taskKey,
				StartedAt:   now,
				Status:      "skipped",
				TriggeredBy: triggeredBy,
				OperatorID:  operatorID,
				Error:       "上次执行尚未结束，已跳过本次触发",
			})
			// Round 16 业务事件埋点：防重入跳过的明确结果。
			utils.IncBusinessEvent("scheduled_task.run.skipped")
			return
		}
		// 标记占用
		s.running[taskID] = true
	}
	s.mu.Unlock()
	// Round 16：拿到执行权 / 标记 running 后立即刷新一次指标
	// （skip 路径不刷新——被跳过的任务根本未进入 running map，计数无变化）
	// 在锁外调用，避免 RunningTaskCount 内部 RLock 与本函数 Lock 死锁。
	utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())

	// 先 wg.Add(1) 再注册 defer.Done()：
	//   - 保证 wg.Add(1) 一定在 defer 注册之前执行，
	//   - 避免 Stop() 中的 s.wg.Wait() 在 Add 之前返回导致正在执行的任务被漏等。
	//   - 并发与停止语义不变（defer 仍然在函数返回时调用 Done）。
	s.wg.Add(1)
	// defer 释放占用 + 等所有任务结束
	defer func() {
		if !canRun {
			s.mu.Lock()
			delete(s.running, taskID)
			s.mu.Unlock()
		}
		// Round 16：任务释放 running 后刷新指标（不改变 stop / 日志语义）
		utils.RefreshSchedulerMetrics(s.Running(), s.ListEntriesLen(), s.RunningTaskCount())
		s.wg.Done()
	}()

	now := time.Now().Unix()
	startedAtMs := time.Now().UnixMilli()

	// 写 running 日志
	runningLog := &database.ScheduledTaskLog{
		TaskID:      taskID,
		TaskKey:     taskKey,
		StartedAt:   now,
		Status:      "running",
		TriggeredBy: triggeredBy,
		OperatorID:  operatorID,
	}
	logID, err := database.CreateScheduledTaskLog(runningLog)
	if err != nil {
		utils.LogError("写入运行日志失败: %v", err)
		// 不 return；继续执行
	}

	// 准备 ctx 和 params
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var rawParams json.RawMessage
	if paramsJSON == "" {
		rawParams = json.RawMessage("{}")
	} else {
		rawParams = json.RawMessage(paramsJSON)
	}

	// 取 handler
	s.mu.RLock()
	handler, ok := s.handlers[handlerName]
	s.mu.RUnlock()
	if !ok {
		s.finishLog(logID, "failed", "", fmt.Sprintf("handler [%s] 未注册", handlerName), startedAtMs)
		_ = database.UpdateScheduledTaskRunResult(taskID, now, 0, "failed", fmt.Sprintf("handler [%s] 未注册", handlerName), false)
		return
	}

	// 真正执行
	output, runErr := handler(ctx, rawParams)
	finishedAtMs := time.Now().UnixMilli()
	finishedAt := time.Now().Unix()

	// 截断 output
	output = truncateString(output, s.logOutputKB*1024)

	status := "success"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
	}
	// 检查超时
	if ctx.Err() == context.DeadlineExceeded {
		status = "timeout"
		if errMsg == "" {
			errMsg = "任务执行超时"
		}
	}

	s.finishLog(logID, status, output, errMsg, startedAtMs)

	// 写 next_run_at（仅 scheduler 触发时）
	nextRunAt := int64(0)
	if triggeredBy == "scheduler" {
		s.mu.RLock()
		entryID, has := s.entries[taskID]
		s.mu.RUnlock()
		if has {
			if entry := s.cron.Entry(entryID); entry.ID != 0 {
				nextRunAt = entry.Schedule.Next(time.Now().In(s.location)).Unix()
			}
		}
	}

	success := status == "success"
	_ = database.UpdateScheduledTaskRunResult(taskID, finishedAt, nextRunAt, status, errMsg, success)

	// Round 16 业务事件埋点：在 status 汇合点上报一次业务事件（不改变 scheduled_task_log 状态）。
	switch status {
	case "success":
		utils.IncBusinessEvent("scheduled_task.run.success")
	case "failed":
		utils.IncBusinessEvent("scheduled_task.run.failed")
	case "timeout":
		utils.IncBusinessEvent("scheduled_task.run.timeout")
	case "skipped":
		utils.IncBusinessEvent("scheduled_task.run.skipped")
	}

	utils.Info("任务 [%s] 执行完成 status=%s duration=%dms", taskKey, status, finishedAtMs-startedAtMs)
}

func (s *Scheduler) finishLog(logID int64, status, output, errMsg string, startedAtMs int64) {
	if logID == 0 {
		return
	}
	now := time.Now().Unix()
	nowMs := time.Now().UnixMilli()
	_ = database.FinishScheduledTaskLog(logID, now, nowMs-startedAtMs, status, output, errMsg)
}

// ==================== cron 表达式解析（5 段 / 6 段兼容）====================

// parseSchedule 同时支持 5 段和 6 段 cron 表达式。
//   - 5 段：分 时 日 月 周
//   - 6 段：秒 分 时 日 月 周
func (s *Scheduler) parseSchedule(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("cron 表达式为空")
	}
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		return s.parser5.Parse(expr)
	}
	if len(fields) == 6 {
		return s.parser6.Parse(expr)
	}
	return nil, fmt.Errorf("不支持的 cron 段数: %d (期望 5 或 6)", len(fields))
}

// ValidateCronExpr 校验 cron 表达式（handler 层调用，给前端实时反馈）。
func (s *Scheduler) ValidateCronExpr(expr string) error {
	if s == nil {
		// 调度器未启动时仍可校验
		p5 := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		p6 := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		fields := strings.Fields(expr)
		switch len(fields) {
		case 5:
			_, err := p5.Parse(expr)
			return err
		case 6:
			_, err := p6.Parse(expr)
			return err
		default:
			return fmt.Errorf("不支持的 cron 段数: %d", len(fields))
		}
	}
	_, err := s.parseSchedule(expr)
	return err
}

// NextRun 计算 cron 表达式的下一次触发时间。
// 用于前端"测试"按钮；不依赖具体任务是否存在。
func (s *Scheduler) NextRun(expr string) (time.Time, error) {
	schedule, err := s.parseSchedule(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(time.Now().In(s.location)), nil
}

// ==================== 后台清理 ====================

// cleanupLoop 每天清理一次过期日志。
func (s *Scheduler) cleanupLoop() {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if s.logRetent > 0 {
				affected, err := database.CleanupOldScheduledTaskLogs(s.logRetent)
				if err != nil {
					utils.Warn("清理定时任务日志失败: %v", err)
				} else if affected > 0 {
					utils.Info("清理定时任务日志 %d 条（保留最新 %d）", affected, s.logRetent)
				}
			}
		}
	}
}

// ==================== 工具函数 ====================

// truncateString 限制 output 长度（按 KB 截断）。
func truncateString(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return s
	}
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "...[truncated]"
}

// ==================== 种子数据 ====================

// seedDefaultScheduledTasks 启动时幂等写入内置任务。
// 已有同名 task_key 不会被覆盖；如需更新已有任务的默认值，
// 请通过 UI 编辑或手动 UPDATE DB。
func seedDefaultScheduledTasks() error {
	// 整体包在事务里，避免半成品（部分种子插入成功部分失败）
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	defaults := []database.ScheduledTask{
		{
			TaskKey:       "backup_daily",
			Name:          "全量备份任务",
			Description:   "每天12点全量备份（数据库 + 上传文件）。增量备份的基线。",
			TaskType:      "built_in",
			CronExpr:      "0 0 12 * * *", // 每天 12:00:00
			HandlerName:   "system.backup",
			HandlerParams: "{}",
			IsActive:      true,
			IsConcurrent:  false,
			// 备份时间可能较长（数据库 + 上传文件），设 2 小时超时
			TimeoutSeconds: 7200,
		},
		{
			TaskKey:        "incremental_backup_hourly",
			Name:           "增量备份任务",
			Description:    "每小时增量备份（仅变更文件 + WAL checkpoint）。无全量时自动跑一次全量作为基线。",
			TaskType:       "built_in",
			CronExpr:       "0 30 * * * *", // 每小时 30 分（避开整点，与全量错开）
			HandlerName:    "system.incremental_backup",
			HandlerParams:  "{}",
			IsActive:       true,
			IsConcurrent:   false,
			TimeoutSeconds: 1800, // 增量备份通常 < 5 分钟，留 30 分钟余量
		},
		{
			TaskKey:        "backup_verify_daily",
			Name:           "每日备份验证",
			Description:    "每日 03:00 全量扫描所有 success/verified 备份，重算三哈希校验完整性，发现 corrupted/missing 即时标记。",
			TaskType:       "built_in",
			CronExpr:       "0 0 3 * * *", // 每天 03:00:00
			HandlerName:    "system.backup_verify",
			HandlerParams:  "{}",
			IsActive:       true,
			IsConcurrent:   false,
			TimeoutSeconds: 3600, // 全量验证可能耗时（按 backup 数 × 校验耗时）
		},
		{
			TaskKey:        "backup_recover_hourly",
			Name:           "每小时 stuck 备份清理",
			Description:    "每小时扫描 pending/running 状态超过 30 分钟的备份，标记为 failed（避免幽灵记录永久卡住）。",
			TaskType:       "built_in",
			CronExpr:       "0 15 * * * *", // 每小时 15 分（与 verify 错开）
			HandlerName:    "system.backup_recover",
			HandlerParams:  "{}",
			IsActive:       true,
			IsConcurrent:   false,
			TimeoutSeconds: 300,
		},
		{
			TaskKey:        "backup_drill_monthly",
			Name:           "每月备份恢复演练",
			Description:    "每月 1 号 05:00 端到端演练最近一次全量备份：解压 → PRAGMA integrity_check → 抽查 5 个核心业务表。失败立即告警。",
			TaskType:       "built_in",
			CronExpr:       "0 0 5 1 * *", // 每月 1 号 05:00:00
			HandlerName:    "system.backup_drill",
			HandlerParams:  "{}",
			IsActive:       true,
			IsConcurrent:   false,
			TimeoutSeconds: 1800, // 演练可能耗时（解压 + DB 完整性检查）
		},
		{
			TaskKey:        "log_cleanup",
			Name:           "每小时日志清理",
			Description:    "清理过期日志文件",
			TaskType:       "built_in",
			CronExpr:       "0 0 * * * *", // 每小时 00 分 00 秒
			HandlerName:    "system.log_clean",
			HandlerParams:  "{}",
			IsActive:       true,
			IsConcurrent:   false,
			TimeoutSeconds: 600,
		},
		{
			TaskKey:        "reminder_scan",
			Name:           "每日提醒扫描",
			Description:    "每天 09:00 扫描所有 active 订阅，命中规则后通过站内信通知。详细逻辑见 services/reminder_scan.go。",
			TaskType:       "built_in",
			CronExpr:       "0 0 9 * * *", // 每天 09:00:00
			HandlerName:    "reminder.scan",
			HandlerParams:  "{}",
			IsActive:       true, // 2026-06-23：第十一阶段启用
			IsConcurrent:   false,
			TimeoutSeconds: 1800,
		},
	}
	for i := range defaults {
		t := defaults[i]
		existing, err := database.GetScheduledTaskByKey(t.TaskKey)
		if err != nil {
			return err
		}
		if existing != nil {
			continue // 幂等：已存在则跳过
		}
		if _, txErr := database.CreateScheduledTaskTx(tx, &t); txErr != nil {
			err = fmt.Errorf("写入种子任务 [%s] 失败: %w", t.TaskKey, txErr)
			return err
		}
		utils.Info("种子定时任务已写入: %s", t.TaskKey)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}
