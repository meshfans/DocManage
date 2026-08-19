package services

import (
	"context"
	"doc/config"
	"doc/database"
	"doc/utils"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// registerBuiltinHandlers 注册调度器内置 handler。
// 命名规范：system.* 和 reminder.*。
func registerBuiltinHandlers(s *Scheduler, cfg *config.Config) {
	// 1. 系统备份（替代原 backup.go 的 ticker 调度）
	s.RegisterHandler("system.backup", makeBackupJobHandler())

	// 2. 增量备份（第四阶段 Phase 4.1）
	s.RegisterHandler("system.incremental_backup", makeIncrementalBackupJobHandler(cfg))

	// 3. 备份验证（第四阶段 Phase 4.2：每日扫描所有 success/verified 备份）
	s.RegisterHandler("system.backup_verify", makeBackupVerifyJobHandler())

	// 3.1. Stuck 备份恢复（清理 pending/running > 30min 的幽灵记录）
	s.RegisterHandler("system.backup_recover", makeBackupRecoverJobHandler())

	// 3.2. 月度备份演练（第四阶段 Phase 4.5）
	s.RegisterHandler("system.backup_drill", makeBackupDrillJobHandler())

	// 4. 日志清理（替代原 logger.go 的 ticker 调度）
	s.RegisterHandler("system.log_clean", makeLogCleanJobHandler())

	// 5. 提醒扫描（占位，阶段 13.2 实现）
	s.RegisterHandler("reminder.scan", makeReminderScanJobHandler())
}

// makeBackupJobHandler 包装 BackupService 为 JobFunc。
func makeBackupJobHandler() JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// P1 修复（2026-06-14）：维护模式下跳过调度备份
		//   - 维护模式通常是恢复/升级/数据修复等场景，业务正在被冻结
		//   - 此时跑备份会读到 partial-restored 的 DB 或被锁表阻塞
		//   - 跳过即可，下次调度会补上
		if config.IsMaintenanceMode() {
			utils.Info("[Scheduler] 维护模式中，跳过定时全量备份")
			return "维护模式中，跳过", nil
		}
		bs := GetBackupService() // 同包内方法，保持原样
		if bs == nil {
			return "", fmt.Errorf("备份服务未初始化")
		}
		// 检查 enabled 标记
		if !bs.cfg.Enabled {
			return "备份服务已禁用，跳过", nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// 委托给现有的 performBackup（保留所有业务逻辑）
		// performBackup 内部不感知 ctx；外层 timeout 写库为 timeout 状态
		manifestID, err := bs.performBackup(false, 0) // isManual=false（定时任务调度），actor=system
		if err != nil {
			// Round 16 业务事件埋点：定时调度全量备份失败（与 scheduled_task.run.* 并列通道）。
			utils.IncBusinessEvent("backup.scheduled.failed")
			return "", fmt.Errorf("备份失败: %w", err)
		}
		// Round 16 业务事件埋点：定时调度全量备份成功（与 scheduled_task.run.* 并列通道）。
		utils.IncBusinessEvent("backup.scheduled.success")
		return fmt.Sprintf("备份完成 dir=%s days_to_keep=%d manifest_id=%d",
			bs.cfg.Dir, bs.cfg.DaysToKeep, manifestID), nil
	}
}

// makeIncrementalBackupJobHandler 包装 IncrementalBackupService 为 JobFunc（第四阶段 Phase 4.1）。
// 配置必须传，因为新服务不持有全局状态。
func makeIncrementalBackupJobHandler(cfg *config.Config) JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if cfg == nil || !cfg.Backup.Enabled {
			return "备份服务已禁用，跳过", nil
		}
		// P1 修复（2026-06-14）：维护模式下跳过增量备份
		//   - 与 makeBackupJobHandler 一致：维护中不读 DB，避免读 partial-restored 数据
		if config.IsMaintenanceMode() {
			utils.Info("[Scheduler] 维护模式中，跳过定时增量备份")
			return "维护模式中，跳过", nil
		}

		// 构造增量备份服务（每次新建，持有本次任务的 parent_id 状态）
		svc := NewIncrementalBackupService(
			&cfg.Backup,
			cfg.Upload.Dir,
			cfg.Database.Path,
		)

		manifestID, err := svc.Run()
		if err != nil {
			// Round 16 业务事件埋点：定时调度增量备份失败（与 scheduled_task.run.* 并列通道）。
			utils.IncBusinessEvent("backup.scheduled.failed")
			return "", fmt.Errorf("增量备份失败: %w", err)
		}
		// Round 16 业务事件埋点：定时调度增量备份成功（与 scheduled_task.run.* 并列通道）。
		utils.IncBusinessEvent("backup.scheduled.success")
		return fmt.Sprintf("增量备份完成 manifest_id=%d", manifestID), nil
	}
}

// makeBackupVerifyJobHandler 每日验证所有备份（第四阶段 Phase 4.2）。
//
// 行为：
//   - 遍历所有 status=success/verified 的 backup_manifest
//   - 重算每个 zip 的三哈希，与 manifest 中记录的比对
//   - 一致 → status=verified；不一致 → status=corrupted；文件丢失 → status=missing
//
// 返回信息：扫描总数 + 通过/损坏/丢失计数。
func makeBackupVerifyJobHandler() JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		verifier := GetBackupVerifier()
		results, err := verifier.VerifyAll()
		if err != nil {
			// 有损坏/丢失，但仍返回 results（用于审计日志）
			utils.LogError("[BackupVerify] 全量扫描发现问题: %v", err)
		}
		if len(results) == 0 {
			return "无备份需要验证", nil
		}
		ok, corrupted, missing := 0, 0, 0
		for _, r := range results {
			switch r.Result {
			case "ok":
				ok++
			case "corrupted":
				corrupted++
			case "missing":
				missing++
			}
		}
		summary := fmt.Sprintf("总 %d, 通过 %d, 损坏 %d, 丢失 %d",
			len(results), ok, corrupted, missing)
		if corrupted > 0 || missing > 0 {
			return summary, fmt.Errorf("发现 %d 个损坏 / %d 个丢失", corrupted, missing)
		}
		return summary, nil
	}
}

// makeBackupRecoverJobHandler 清理 stuck 的 pending/running 备份（每小时一次）。
//
// 用途：进程崩溃 / 服务被 kill 时，pending/running 状态的 manifest 可能永远卡住。
// 阈值 30 分钟（正常备份 < 5 分钟，远超合理上限）。
//
// 返回：恢复的条数。
func makeBackupRecoverJobHandler() JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		n, err := database.RecoverStuckBackups(
			30*time.Minute,
			"调度恢复：pending/running 超 30 分钟",
		)
		if err != nil {
			// Issue M-8：调度任务失败写 audit，便于追溯 recurring 失败。
			// 用 AuditTargetSystem 而非 Backup：backup 不允许 target_id==0。
			database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
				"recover.failed", "", "scheduler", gin.H{"err": err.Error(), "task": "system.backup_recover"})
			return "", fmt.Errorf("恢复 stuck 备份失败: %w", err)
		}
		if n == 0 {
			return "无 stuck 备份需要清理", nil
		}
		// Issue M-8：清理了 stuck 备份属于安全事件（进程被 kill 后遗症），留痕。
		database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
			"recover.success", "", "scheduler", gin.H{"recovered": n, "task": "system.backup_recover"})
		utils.Warn("[BackupRecover] 清理 %d 条 stuck 备份", n)
		return fmt.Sprintf("清理 %d 条 stuck 备份", n), nil
	}
}

// makeBackupDrillJobHandler 月度演练。
//
// 行为：
//   - 查最近一次 success/verified/missing 全量备份
//   - 在临时目录解压（**不覆盖生产**）
//   - PRAGMA integrity_check → 必须 "ok"
//   - 抽查 contract/customer/media/user 4 个核心表前 10 条
//   - 演练结果写入 backup_manifest.verified_result
//   - 失败立即 ERROR 日志（生产可对接告警系统）
func makeBackupDrillJobHandler() JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		result, err := DrillLatestBackup()
		if err != nil && result == nil {
			// Issue M-8：演练初始化失败属安全事件（环境配置/磁盘异常）。
			database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
				"drill.failed", "", "scheduler",
				gin.H{"err": err.Error(), "task": "system.backup_drill"})
			return "", fmt.Errorf("演练初始化失败: %w", err)
		}
		if result != nil && result.Success {
			// Issue M-8：演练通过是合规审计要求（L4 完整性复核证据）。
			database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
				"drill.success", "", "scheduler", gin.H{
					"task":          "system.backup_drill",
					"manifest_id":   result.ManifestID,
					"integrity":     result.IntegrityCheck,
					"sampled_rows":  result.SampledRows,
					"duration_ms":   result.DurationMS,
				})
			return fmt.Sprintf("演练通过: id=%d, integrity=%s, tables=%d, rows=%d, duration=%dms",
				result.ManifestID, result.IntegrityCheck,
				result.SampledTables, result.SampledRows, result.DurationMS), nil
		}
		// 演练失败：仅组装 output/error（写 DB 用），不在此额外打 LogError。
		// DrillLatestBackup 内部已经打过一次 "[Drill] ❌ 演练失败..."，
		// 并且调度器 executeOnce 也会打 "任务 [...] 执行完成 status=failed"，
		// 再在此处打一次会导致同一条失败在日志里出现两次（用户看到"两条一模一样的日志"）。
		errMsg := "演练失败"
		if result != nil {
			rootCause := result.Error
			if result.DecryptionError != "" {
				rootCause = result.DecryptionError
			}
			errMsg = fmt.Sprintf("演练失败: id=%d, integrity=%s, root=%s",
				result.ManifestID, result.IntegrityCheck, rootCause)
			// Issue M-8：演练失败的取证留痕（含 manifest_id + root cause）。
			database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
				"drill.failed", "", "scheduler", gin.H{
					"task":        "system.backup_drill",
					"manifest_id": result.ManifestID,
					"integrity":   result.IntegrityCheck,
					"root_cause":  rootCause,
				})
		}
		if err != nil {
			errMsg += ": " + err.Error()
		}
		// 直接用 errors.New 而非 fmt.Errorf("%s", ...)，
		// 避免无意义的格式化层（errMsg 是 string）。
		return errMsg, errors.New(errMsg)
	}
}

// makeLogCleanJobHandler 包装 Logger 为 JobFunc。
func makeLogCleanJobHandler() JobFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// 显式使用 utils 包前缀，避免 import 优化导致"imported and not used"
		l := utils.GetLogger()
		if l == nil {
			return "", fmt.Errorf("日志器未初始化")
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// 调用现有方法（导出后的 CleanOldLogs）
		l.CleanOldLogs()
		// Issue M-8：日志清理属可审计操作（清理后无法追溯删除前的日志内容）。
		database.RecordAuditStandalone(0, database.AuditTargetSystem, 0,
			"log_clean.success", "", "scheduler", gin.H{
				"task":         "system.log_clean",
				"dir":          l.Dir(),
				"days_to_keep": l.DaysToKeep(),
			})
		return fmt.Sprintf("日志清理完成 dir=%s days_to_keep=%d", l.Dir(), l.DaysToKeep()), nil
	}
}

// makeReminderScanJobHandler 第十一阶段：调用 services.MakeReminderScanJobHandler。
func makeReminderScanJobHandler() JobFunc {
	return MakeReminderScanJobHandler()
}
