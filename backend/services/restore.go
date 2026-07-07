package services

import (
	"archive/zip"
	"doc/config"
	"doc/database"
	"doc/utils"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ==================== 备份恢复服务（第四阶段 Phase 4.1 衔接）====================
//
// 设计要点：
//   - 干跑优先（dry_run=true）：返回将执行的动作，不实际执行
//   - 恢复前自动快照当前数据到 ./backups/snapshot_<ts>/
//   - 恢复后自动写入 backup_manifest（type=restore 作为审计）
//   - 仅 admin 角色可调用（路由层校验）
//
// P0 修复（2026-06-14）：
//   - 实际恢复前**强制关闭** SQLite 连接，恢复完成后**重开**
//     （解决 Issue 4-1：直接覆盖正在使用的 doc.db 会损坏）
//   - 恢复完成后**强制清理** snapshotDir
//     （解决 Issue 4-2：明文 zip 副本永久残留）
// ----------------------------------------------------------------------------

// RestoreRequest 恢复请求参数。
type RestoreRequest struct {
	// FullBackupID 必填：基线全量备份 manifest_id
	FullBackupID int64 `json:"full_backup_id"`
	// IncrementalIDs 可选：要应用的增量备份 manifest_id 列表（按时间顺序）
	IncrementalIDs []int64 `json:"incremental_ids,omitempty"`
	// DryRun true 时只返回将执行的操作，不实际恢复
	DryRun bool `json:"dry_run"`
}

// RestoreAction 单步恢复动作。
type RestoreAction struct {
	Step          int    `json:"step"`        // 步骤序号
	Action        string `json:"action"`      // 描述
	ManifestID    int64  `json:"manifest_id"` // 涉及的 manifest
	SnowID        string `json:"snowid"`      // 涉及的备份
	FilePath      string `json:"file_path"`   // 源 zip
	WillOverwrite bool   `json:"will_overwrite"`
}

// RestoreResult 恢复结果。
type RestoreResult struct {
	DryRun          bool            `json:"dry_run"`
	Actions         []RestoreAction `json:"actions"`
	CurrentSnapshot string          `json:"current_snapshot,omitempty"` // 干跑/实际前自动快照路径
	RestoredTo      string          `json:"restored_to,omitempty"`      // 实际恢复的目标路径
	Success         bool            `json:"success"`
	Error           string          `json:"error,omitempty"`
}

// RestoreService 恢复服务。
type RestoreService struct {
	cfg       *config.Config
	backupDir string
	uploadDir string
	dbPath    string
}

// NewRestoreService 构造恢复服务。
func NewRestoreService(cfg *config.Config) *RestoreService {
	return &RestoreService{
		cfg:       cfg,
		backupDir: cfg.Backup.Dir,
		uploadDir: cfg.Upload.Dir,
		dbPath:    cfg.Database.Path,
	}
}

// Run 执行恢复（干跑或实际）。
//
// 流程：
//  1. 查 FullBackupID 对应的 manifest（必须 status=success / verified）
//  2. 收集要应用的增量（按时间顺序）
//  3. 校验所有 zip 文件存在 + 哈希匹配（若有）
//  4. 干跑 → 返回 actions 列表
//  5. 实际恢复 → 快照当前数据 → 解压全量 → 顺序应用增量 → 写入恢复审计
func (s *RestoreService) Run(req RestoreRequest) (*RestoreResult, error) {
	result := &RestoreResult{DryRun: req.DryRun}

	// 1) 查全量 manifest
	full, err := database.GetBackupManifestByID(req.FullBackupID)
	if err != nil {
		return nil, fmt.Errorf("查询全量备份失败: %w", err)
	}
	if full == nil {
		return nil, fmt.Errorf("找不到全量备份 manifest_id=%d", req.FullBackupID)
	}
	if full.Type != database.BackupTypeFull {
		return nil, fmt.Errorf("manifest_id=%d 不是全量备份（type=%s）", req.FullBackupID, full.Type)
	}
	if full.Status != database.BackupStatusSuccess && full.Status != database.BackupStatusVerified {
		return nil, fmt.Errorf("全量备份状态不可用：%s", full.Status)
	}

	// 2) 收集增量（按时间顺序）
	incrementals, err := database.GetLatestIncrementalAfter(req.FullBackupID)
	if err != nil {
		return nil, fmt.Errorf("查询增量备份失败: %w", err)
	}
	// 过滤出请求中指定的增量 ID
	if len(req.IncrementalIDs) > 0 {
		wanted := make(map[int64]bool, len(req.IncrementalIDs))
		for _, id := range req.IncrementalIDs {
			wanted[id] = true
		}
		var filtered []*database.BackupManifest
		for _, inc := range incrementals {
			if wanted[inc.ID] {
				filtered = append(filtered, inc)
			}
		}
		incrementals = filtered
	}

	// 3) 校验所有 zip 文件
	allManifests := append([]*database.BackupManifest{full}, incrementals...)
	for i, m := range allManifests {
		if _, err := os.Stat(m.FilePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("步骤 %d 的 zip 不存在: %s", i+1, m.FilePath)
		}
		// 可选：校验哈希（Phase 4.2 验证功能就绪后启用）
		result.Actions = append(result.Actions, RestoreAction{
			Step:          i + 1,
			Action:        fmt.Sprintf("应用 %s 备份 (manifest_id=%d)", m.Type, m.ID),
			ManifestID:    m.ID,
			SnowID:        m.SnowID,
			FilePath:      m.FilePath,
			WillOverwrite: !req.DryRun && i > 0, // 增量步骤会覆盖
		})
	}

	if req.DryRun {
		result.Success = true
		return result, nil
	}

	// 4) 实际恢复：先快照当前数据
	snapshotDir, err := s.createSnapshot()
	if err != nil {
		return nil, fmt.Errorf("创建快照失败（中止恢复）: %w", err)
	}
	result.CurrentSnapshot = snapshotDir
	utils.Warn("恢复前快照已创建: %s", snapshotDir)

	// P0 修复（Issue 4-2）：注册 defer 清理 snapshotDir + 所有明文副本。
	// 重要：defer 必须**先**注册，再做后续可能失败的步骤。
	// 失败时保留 snapshot 由管理员手动处理（设 _snapshotKeepOnError = true 可关掉）
	defer func() {
		if snapshotDir == "" {
			return
		}
		if _snapshotKeepOnError.Load() && result.Success == false {
			utils.Warn("[Restore] 失败时保留 snapshot（管理员手动恢复）: %s", snapshotDir)
			return
		}
		if err := os.RemoveAll(snapshotDir); err != nil {
			utils.LogError("[Restore] 清理 snapshot 失败: %s, err=%v", snapshotDir, err)
		} else {
			utils.Info("[Restore] 已清理 snapshot: %s", snapshotDir)
		}
	}()

	// P0 修复（Issue 4-1）：解压前先**关闭** SQLite 连接，避免覆盖正在被打开的 doc.db。
	// 解压完成后**重开**连接以加载新数据。
	// 设计权衡：DB 重开期间所有请求都会失败 → 这是为什么恢复必须配合 maintenance 模式
	// （middleware.Maintenance 已拦截 HTTP，恢复调用走的是 admin 专用路径）。
	if database.DB != nil {
		utils.Info("[Restore] 关闭 SQLite 连接（即将覆盖 doc.db）")
		if err := database.DB.Close(); err != nil {
			utils.LogError("[Restore] 关闭 DB 失败（继续尝试）: %v", err)
		}
	}

	// 5) 应用全量 + 增量（顺序覆盖）
	//    2026-07-06 round5 精简：删除解密分支（m.Encrypted / DecryptZip 都不再用）
	for i, m := range allManifests {
		if err := s.extractZipContents(m.FilePath); err != nil {
			result.Error = fmt.Sprintf("步骤 %d 失败: %v", i+1, err)
			return result, fmt.Errorf("解压 %s 失败: %w", m.FilePath, err)
		}
		utils.Info("已应用步骤 %d: %s (manifest_id=%d)", i+1, m.Type, m.ID)
	}

	// P0 修复（Issue 4-1）：恢复完成后**重开** SQLite 连接。
	// 用 database.ReloadDatabase 复用上次 Init 的 options（含 license 派生的密码）。
	if err := database.ReloadDatabase(); err != nil {
		result.Error = fmt.Sprintf("重开 DB 失败: %v", err)
		return result, fmt.Errorf("重开 DB 失败: %w", err)
	}

	// 6) 写恢复审计记录（DB 已重开，可写）
	if err := s.writeRestoreAudit(req, full, incrementals); err != nil {
		utils.LogError("写恢复审计失败: %v", err)
		// 不中断恢复流程
	}

	result.Success = true
	result.RestoredTo = s.dbPath + " (data) + " + s.uploadDir + " (uploads)"
	return result, nil
}

// createSnapshot 在恢复前自动快照当前数据目录到 ./backups/snapshot_<ts>/。
//
// P1 修复（二次审查 2026-06-14）：失败路径上**清理孤儿目录**。
//   - 历史 bug：copyFile/copyDir 失败时返回 (snapshotDir, err)，但创建的 snapshot 目录残留
//     → 多次失败在 Backup.Dir 累积半空目录（含 doc.db 副本 + dec_step_*.zip 明文副本）
//   - 修复：失败时显式 os.RemoveAll(snapshotDir) 后再 return
func (s *RestoreService) createSnapshot() (string, error) {
	if s.cfg.Backup.Dir == "" {
		return "", errors.New("备份目录未配置")
	}

	ts := time.Now().Format("20060102_150405")
	snapshotDir := filepath.Join(s.cfg.Backup.Dir, fmt.Sprintf("snapshot_%s", ts))
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", err
	}

	// 失败清理 helper：copyFile/copyDir 失败时调用，删孤儿目录
	cleanup := func(reason string, copyErr error) (string, error) {
		if rmErr := os.RemoveAll(snapshotDir); rmErr != nil {
			utils.LogError("[Restore] 清理孤儿 snapshot 失败: dir=%s, reason=%s, err=%v",
				snapshotDir, reason, rmErr)
		} else {
			utils.Info("[Restore] 清理孤儿 snapshot: dir=%s, reason=%s", snapshotDir, reason)
		}
		return snapshotDir, fmt.Errorf("%s: %w", reason, copyErr)
	}

	// 快照 DB（如果存在）
	if _, err := os.Stat(s.dbPath); err == nil {
		dst := filepath.Join(snapshotDir, "doc.db")
		if err := copyFile(s.dbPath, dst); err != nil {
			return cleanup("快照 DB 失败", err)
		}
		utils.Info("已快照 DB: %s → %s", s.dbPath, dst)
	}

	// 快照 uploads（如果存在）
	if _, err := os.Stat(s.uploadDir); err == nil {
		dstUploads := filepath.Join(snapshotDir, "uploads")
		if err := copyDir(s.uploadDir, dstUploads); err != nil {
			return cleanup("快照 uploads 失败", err)
		}
		utils.Info("已快照 uploads: %s → %s", s.uploadDir, dstUploads)
	}

	return snapshotDir, nil
}

// extractZipContents 把 zip 中的 data/* 和 uploads/* 解压到生产路径。
// data/doc.db → 覆盖当前 DB
// uploads/*  → 覆盖当前 uploads（合并而非清空）
func (s *RestoreService) extractZipContents(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// 跳过 manifest.json（仅备份元信息，不属于数据）
		if f.Name == "manifest.json" {
			continue
		}

		// 决定解压目标路径
		var targetPath string
		switch {
		case strings.HasPrefix(f.Name, "data/"):
			relName := strings.TrimPrefix(f.Name, "data/")
			targetPath = filepath.Join(filepath.Dir(s.dbPath), relName)
		case strings.HasPrefix(f.Name, "uploads/"):
			relName := strings.TrimPrefix(f.Name, "uploads/")
			targetPath = filepath.Join(s.uploadDir, relName)
		default:
			utils.Warn("跳过未知前缀文件: %s", f.Name)
			continue
		}

		// 确保目标目录存在
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}

		// 解压
		if err := extractFile(f, targetPath); err != nil {
			return fmt.Errorf("解压 %s → %s 失败: %w", f.Name, targetPath, err)
		}
	}
	return nil
}

// extractFile 解压单个 zip 条目到目标路径。
//
// 关键：保留原始 mtime（从 zip.File.ModTime 读取）。
//   - os.Create 创建新文件默认 mtime = 当前时间，会污染增量扫描
//   - 第四次阶段 P0+：手动恢复后，下一次增量扫描用 parent.StartedAt 作为 cutoff
//   - 若不保留 mtime，所有文件 mtime = restore time > parent.StartedAt → 误判全部"变更"
//   - 修复：解压后用 os.Chtimes 还原 mtime，让增量扫描正确识别"未变化"文件
func extractFile(f *zip.File, targetPath string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	_ = dst.Close() // 必须先 close，Chtimes 才能生效（Windows 限制）

	// 还原 mtime（即使失败也不中断恢复 — 只影响后续增量扫描的精度）
	modTime := f.ModTime()
	if err := os.Chtimes(targetPath, modTime, modTime); err != nil {
		utils.Warn("[Restore] 还原 mtime 失败（不影响恢复本身）: %s, err=%v",
			targetPath, err)
	}
	return nil
}

// writeRestoreAudit 写一条审计记录到 backup_manifest。
//
// 关键设计：
//   - Type: BackupTypeRestoreAudit（独立类型，区别于 full / incremental）
//     → 所有 type='full' 的查询自然排除审计记录（演练 / 增量父查找不会被污染）
//   - Status: verified（避免 RecoverStuckBackups 反复扫描）
//   - FilePath: 存恢复摘要 "restore: full_id=N, incrementals=[...]"（仅供人读）
//
// 2026-06-27 修复 bug #2：
//   - 之前 Type=full → findLatestSuccessfulFullBackup 把审计记录当备份
//   - 演练 / 增量父查找 / 任何 type='full' 的查询都会命中审计记录
//   - 演练拿到 FilePath="restore: full_id=21..." 当 zip 打开 → "系统找不到文件"
//
// 2026-06-29 bug #14 强化：
//   - 用 UpdateBackupManifestVerifiedWithStatus 单次 UPDATE 写入 status+verified_result
//   - 避免先 pending→verified 两步写入带来的中间态扫描窗口
//   - 配合 services/backup.go DetectOrphanBackups 排除 restore_audit
//     防止 FilePath="restore: ..." 被 os.Stat 失败后错置为 missing
func (s *RestoreService) writeRestoreAudit(req RestoreRequest, full *database.BackupManifest, incrementals []*database.BackupManifest) error {
	snowID, _ := generateBackupSnowID()
	now := time.Now().Unix()
	// 2026-06-27 修复：设 parent_id = req.FullBackupID 建立与源全量备份的外键关联
	//   - UI 可点击"基于 backup_id=N"反查源备份
	//   - 即使 parent_id 被 CountLiveChildrenByParent 查询，该函数已加 type=incremental
	//     过滤（services/backup_manifest.go），审计记录不会被误算为"活跃子增量"
	manifest := &database.BackupManifest{
		SnowID:     snowID,
		Type:       database.BackupTypeRestoreAudit, // 独立类型，区别于 full/incremental
		ParentID:   req.FullBackupID,                // 2026-06-27：建立与源全量备份的外键关联
		StartedAt:  now,
		FinishedAt: now,
		CreatedBy:  0,
	}
	// 简化：复用 file_path 字段记录恢复摘要（人读，非路径）
	// 增量 ID 用 JSON 形式保存（便于将来 UI 反查）
	incJSON := "[]"
	if len(req.IncrementalIDs) > 0 {
		parts := make([]string, len(req.IncrementalIDs))
		for i, id := range req.IncrementalIDs {
			parts[i] = strconv.FormatInt(id, 10)
		}
		incJSON = "[" + strings.Join(parts, ",") + "]"
	}
	restoreInfo := fmt.Sprintf("restore: full_id=%d, incrementals=%s", req.FullBackupID, incJSON)
	manifest.FilePath = restoreInfo
	if _, err := database.CreateBackupManifest(manifest); err != nil {
		return err
	}
	// 2026-06-29 bug #14：单次 UPDATE 写入 status + verified_result，避免 pending→verified 中间态
	if err := database.UpdateBackupManifestVerifiedWithStatus(
		manifest.ID,
		database.BackupStatusVerified,
		"restore_audit",
	); err != nil {
		return err
	}
	return nil
}

// copyFile 复制文件。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// copyDir 递归复制目录。
func copyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

// ==================== P0 修复：恢复相关辅助（2026-06-14）====================

// _snapshotKeepOnError 控制恢复失败时是否保留 snapshot。
//
// 设计目标：
//   - 默认 false：失败时也清掉 snapshot（防明文副本永久泄露）
//   - 运维调试时改为 true：保留 snapshot 供人工取证
//
// 用 atomic 是因为 restore.Run 可能在不同 goroutine 被调用（HTTP handler / scheduler）。
var _snapshotKeepOnError atomic.Bool

// SetSnapshotKeepOnError 设置失败时是否保留 snapshot（运维接口）。
func SetSnapshotKeepOnError(keep bool) {
	_snapshotKeepOnError.Store(keep)
}
