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
	"time"
)

// ==================== 增量备份服务 ====================
//
// 设计要点：
//   - 增量备份 = DB 快照（checkpoint 后）+ 自上次备份以来变更的上传文件
//   - 通过 mtime 扫描 upload 目录识别变更文件
//   - 写入 backup_manifest 表，关联 parent_id（最近一次成功的全量/增量）
//   - 跳过未变更文件 → 大幅节省空间和时间
//
// ----------------------------------------------------------------------------
// 关于『无变更跳过』的设计决策：
//
// 用户的合理建议：调度增量前，先检查『和上次备份是否相同』，相同则跳过。
//
// 我们的决定：**不实现**『无变更跳过』。原因：
//
//   1. **DB 永远在变**：每个增量 zip 包含完整 DB 快照；DB 内 audit_log 表
//      每次有人查看/签署/操作业务都会 append 一行，**DB 哈希永远不同**。
//      即使『len(changedFiles) == 0』也不能跳过（漏了 audit_log）。
//
//   2. **审计完整性 > 空间效率**：本系统是法律证据系统，漏一条 audit 记录
//      可能导致审计链断裂 → 证据不可用。设计取舍上宁多备不少备。
//
//   3. **空间代价可接受**：DB 快照稳定 ~10-50MB（即使数据增长，SQLite checkpoint
//      后单文件大小有限）。空增量 zip 约 10MB，频次默认 1h/次 → 每天 ~240MB
//      增量 + 1GB 全量 = 总量可控。
//
//   4. **真正要省空间，调频率即可**：
//      - 业务空闲时段（如 22:00-08:00）调低频率到 1次/4h 或 1次/6h
//      - 业务繁忙时段保持 1h/次
//      - 配置入口：scheduled_task 表 system.incremental_backup 行的 cron 表达式
//        （默认 "0 0 * * * *" = 每小时整点）
// ----------------------------------------------------------------------------

// IncrementalBackupService 增量备份服务。
type IncrementalBackupService struct {
	cfg       *config.BackupConfig
	uploadDir string
	dbPath    string
	parentID  int64 // 最近一次成功全量的 ID（Run 时填充）
}

// NewIncrementalBackupService 构造增量备份服务。
// uploadDir: 上传文件根目录（如 ./uploads）
// dbPath: SQLite 数据库路径（如 ./data/doc.db）
func NewIncrementalBackupService(cfg *config.BackupConfig, uploadDir, dbPath string) *IncrementalBackupService {
	return &IncrementalBackupService{
		cfg:       cfg,
		uploadDir: uploadDir,
		dbPath:    dbPath,
	}
}

// Run 执行一次增量备份，返回 manifest ID。
//
// 流程：
//  1. 找最近一次成功的全量或增量备份（确定 parent_id）
//  2. 若没有 parent 全量 → 返回错误（必须先有全量）
//  3. checkpoint WAL → 拿到一致 DB 快照
//  4. 扫描 upload 目录，找 mtime > parent.StartedAt 的文件
//  5. 打包：DB 快照 + 变更文件
//  6. 计算 SM3 + SHA-256 双哈希
//  7. 写 backup_manifest 记录（status=success）
//
// ⚠️ **best-effort 限制**：本实现运行期间业务写入仍在继续。
//   - 步骤 4 期间新增的文件可能不在 backup 里（mtime 早于 checkpoint）
//   - 步骤 5 期间新增的文件可能丢失（mtime 晚于 checkpoint）
//   - 严格取证场景需要 Phase 4.3 维护模式（恢复前 stop writes）
func (s *IncrementalBackupService) Run() (int64, error) {
	if s.cfg == nil || !s.cfg.Enabled {
		return 0, errors.New("备份服务未启用")
	}

	startTime := time.Now()
	utils.Info("增量备份开始（upload=%s, db=%s）", s.uploadDir, s.dbPath)
	// 二次审查 2026-06-14：增量备份不实现『无变更跳过』，详见文件顶部注释
	// 真正省空间的方案：调 scheduled_task.system.incremental_backup.cron 频率

	// 1) 找最近的父全量（必须有全量才能做增量）
	parent, err := database.GetLatestFullBackup()
	if err != nil {
		return 0, fmt.Errorf("查询最近全量备份失败: %w", err)
	}

	// ⚠️ 自动回退（第四阶段 Phase 4.1 衔接需求）：
	// 若没有可用全量（如首次部署 / 全量被清空），自动跑一次全量作为基线，
	// 避免首次增量报错阻塞业务。后续增量会基于这次"自动全量"继续。
	if parent == nil {
		utils.Warn("无可用全量备份，自动执行一次全量作为基线...")
		if err := s.runAutoFullBackup(); err != nil {
			return 0, fmt.Errorf("自动全量基线失败: %w", err)
		}
		// 重新查询
		parent, err = database.GetLatestFullBackup()
		if err != nil || parent == nil {
			return 0, fmt.Errorf("自动全量后仍找不到父备份: %v", err)
		}
		utils.Info("自动全量基线完成: id=%d, snowid=%s", parent.ID, parent.SnowID)
	}

	s.parentID = parent.ID // 缓存供 packIncrementalZip 用
	utils.Info("增量备份父全量: id=%d, snowid=%s, started_at=%d",
		parent.ID, parent.SnowID, parent.StartedAt)

	// 2) 创建 manifest 记录（pending → running）
	snowID, err := generateBackupSnowID()
	if err != nil {
		return 0, fmt.Errorf("生成 snowid 失败: %w", err)
	}

	// 链式 keep_until：增量 keep_until = max(父全量.keep_until, 自身 started_at + days_to_keep)
	// 这样整条链过期后才一起删（避免孤儿增量）
	var keepUntil int64
	if parent.KeepUntil > 0 {
		keepUntil = parent.KeepUntil
	}
	selfKeep := time.Now().AddDate(0, 0, s.cfg.DaysToKeep).Unix()
	if selfKeep > keepUntil {
		keepUntil = selfKeep
	}
	// 注：父全量 keep_until=0（手动备份）→ 增量 keep_until 也继承为 0（永不清除）

	manifest := &database.BackupManifest{
		SnowID:    snowID,
		Type:      database.BackupTypeIncremental,
		ParentID:  parent.ID,
		StartedAt: startTime.Unix(),
		CreatedBy: 0, // 系统任务
		KeepUntil: keepUntil,
	}
	manifestID, err := database.CreateBackupManifest(manifest)
	if err != nil {
		return 0, fmt.Errorf("创建 manifest 失败: %w", err)
	}
	if updateErr := database.UpdateBackupManifestStatus(manifestID, database.BackupStatusRunning); updateErr != nil {
		utils.LogError("更新 manifest 状态为 running 失败: %v", updateErr)
	}

	// 3) 收集变更文件 + 触发 checkpoint（串行：先扫描，后 checkpoint）
	since := time.Unix(parent.StartedAt, 0)
	changedFiles, err := s.collectChangedFiles(since)
	if err != nil {
		s.markFailed(manifestID, fmt.Sprintf("扫描变更文件失败: %v", err))
		return manifestID, err
	}
	utils.Info("增量备份扫描到 %d 个变更文件（自 %s 起）", len(changedFiles), since.Format(time.RFC3339))

	// 4) 触发 WAL checkpoint，拿到一致 DB 快照
	if err := s.triggerCheckpoint(); err != nil {
		s.markFailed(manifestID, fmt.Sprintf("WAL checkpoint 失败: %v", err))
		return manifestID, err
	}

	// 5) 打包 zip
	zipPath, fileSize, hashSM3, hashSHA256, hashCombined, err := s.packIncrementalZip(manifestID, changedFiles)
	if err != nil {
		s.markFailed(manifestID, fmt.Sprintf("打包失败: %v", err))
		return manifestID, err
	}

	duration := time.Since(startTime)

	// 6) 写 manifest success（三哈希写入）
	if err := database.UpdateBackupManifestSuccess(
		manifestID, zipPath, fileSize, hashSM3, hashSHA256, hashCombined,
		0, 0, // WAL 范围：本设计未拆分 WAL 段（用 checkpoint 一致性快照）
		int64(len(changedFiles)), 0,
		duration,
	); err != nil {
		utils.LogError("更新 manifest success 失败: %v", err)
		return manifestID, fmt.Errorf("更新 manifest 失败: %w", err)
	}

	utils.Info("增量备份完成: id=%d, snowid=%s, size=%d, files=%d, duration=%v",
		manifestID, snowID, fileSize, len(changedFiles), duration)

	// 第四阶段 P0+Phase 4.2：增量备份完成后自动验证（与全量备份行为一致）
	//   验证失败不会回滚备份（备份已写入），仅标记 corrupted 让用户感知
	verifier := GetBackupVerifier()
	if _, verifyErr := verifier.VerifyBackup(manifestID); verifyErr != nil {
		utils.Warn("[Backup] 增量备份自动验证失败: id=%d, err=%v（备份已生成，但标记为待复核）",
			manifestID, verifyErr)
	}

	return manifestID, nil
}

// collectChangedFiles 扫描 upload 目录，返回所有 mtime > since 的文件路径列表。
// 不递归（uploads 是扁平结构）。
func (s *IncrementalBackupService) collectChangedFiles(since time.Time) ([]string, error) {
	if s.uploadDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(s.uploadDir); os.IsNotExist(err) {
		utils.Warn("upload 目录不存在: %s", s.uploadDir)
		return nil, nil
	}

	var changed []string
	err := filepath.Walk(s.uploadDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			utils.Warn("遍历 %s 失败: %v", path, err)
			return nil // 跳过错误，继续
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // 跳过软链接
		}
		// 增量判断：mtime > since（包含 since 之后的所有文件）
		if info.ModTime().After(since) {
			relPath, err := filepath.Rel(s.uploadDir, path)
			if err != nil {
				utils.Warn("获取相对路径失败 %s: %v", path, err)
				return nil
			}
			changed = append(changed, relPath)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// triggerCheckpoint 触发 SQLite WAL checkpoint（TRUNCATE 模式），让 main DB 包含全部数据。
// 必须在打 DB 快照前调用，否则 DB 文件可能不含最新提交的事务。
func (s *IncrementalBackupService) triggerCheckpoint() error {
	if database.DB == nil {
		return errors.New("database.DB 未初始化")
	}
	// PASSIVE / FULL / RESTART / TRUNCATE — TRUNCATE 最彻底（截断 WAL 文件）
	_, err := database.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("PRAGMA wal_checkpoint 失败: %w", err)
	}
	utils.Info("WAL checkpoint 完成（TRUNCATE 模式）")
	return nil
}

// packIncrementalZip 打包增量 zip：DB 快照 + 变更文件 + 元数据。
// 返回：zip 路径、文件大小、SM3、SHA-256、CombinedHash。
func (s *IncrementalBackupService) packIncrementalZip(manifestID int64, changedFiles []string) (
	string, int64, string, string, string, error) {

	// 1) 准备目录
	if err := os.MkdirAll(s.cfg.Dir, 0755); err != nil {
		return "", 0, "", "", "", fmt.Errorf("创建备份目录失败: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	zipName := fmt.Sprintf("inc_%d_%s.zip", manifestID, timestamp)
	zipPath := filepath.Join(s.cfg.Dir, zipName)

	// 2) 创建 zip
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", 0, "", "", "", fmt.Errorf("创建 zip 文件失败: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)

	// 3) 写入 manifest.json（含 parent_id / changed_files / 时间戳 / 备份元数据）
	parentID := s.parentID // 由 Run() 提前赋值
	manifestJSON := fmt.Sprintf(`{
  "manifest_id": %d,
  "type": "incremental",
  "parent_id": %d,
  "started_at": %d,
  "changed_files_count": %d,
  "db_path_in_zip": "data/%s",
  "uploads_in_zip": "uploads/",
  "app": "DocManage",
  "version": "v1",
  "note": "恢复流程：解压全量基线，再按时间顺序应用本增量"
}
`, manifestID, parentID, time.Now().Unix(), len(changedFiles), filepath.Base(s.dbPath))
	if err := writeStringToZip(zipWriter, "manifest.json", manifestJSON); err != nil {
		return "", 0, "", "", "", err
	}

	// 4) 写入 DB 快照
	dbName := filepath.Base(s.dbPath)
	if err := addFileToZip(zipWriter, s.dbPath, filepath.ToSlash(filepath.Join("data", dbName))); err != nil {
		return "", 0, "", "", "", fmt.Errorf("添加 DB 文件到 zip 失败: %w", err)
	}

	// 5) 写入变更的上传文件
	for _, relPath := range changedFiles {
		srcPath := filepath.Join(s.uploadDir, relPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			utils.Warn("文件已不存在，跳过: %s", srcPath)
			continue
		}
		zipPath := filepath.ToSlash(filepath.Join("uploads", relPath))
		if err := addFileToZip(zipWriter, srcPath, zipPath); err != nil {
			utils.Warn("添加 %s 到 zip 失败: %v", relPath, err)
			continue // 不中断整个备份
		}
	}

	// 6) 关闭 zip
	if err := zipWriter.Close(); err != nil {
		return "", 0, "", "", "", fmt.Errorf("关闭 zip writer 失败: %w", err)
	}

	// 7) 读 zip 算三哈希
	fileSize, hashSM3, hashSHA256, hashCombined, err := hashFileTriple(zipPath)
	if err != nil {
		return "", 0, "", "", "", fmt.Errorf("计算哈希失败: %w", err)
	}

	utils.Info("增量打包完成: path=%s, size=%d, sm3=%s..., sha256=%s..., combined=%s...",
		zipPath, fileSize, hashSM3[:8], hashSHA256[:8], hashCombined[:8])
	return zipPath, fileSize, hashSM3, hashSHA256, hashCombined, nil
}

// markFailed 把 manifest 标记为 failed 并记录错误信息。
func (s *IncrementalBackupService) markFailed(manifestID int64, errMsg string) {
	if err := database.UpdateBackupManifestFailed(manifestID, errMsg); err != nil {
		utils.LogError("更新 manifest failed 失败: %v", err)
	}
}

// runAutoFullBackup 自动执行一次全量备份作为基线。
// 复用 BackupService.performBackup()，保持与 system.backup 任务一致行为。
// 注意：performBackup 内部已写 backup_manifest，无需重复处理。
func (s *IncrementalBackupService) runAutoFullBackup() error {
	bs := GetBackupService()
	if bs == nil {
		// 没有现成的 BackupService：临时构造一个
		bs = &BackupService{cfg: s.cfg}
	}
	if !bs.cfg.Enabled {
		return fmt.Errorf("备份服务未启用（cfg.Backup.Enabled=false），无法自动全量")
	}
	utils.Info("自动全量基线启动（dir=%s）", bs.cfg.Dir)
	_, err := bs.performBackup(false, 0) // isManual=false（自动全量基线），actor=system
	// performBackup 是同步的，调用返回即完成
	// Issue #9：自动全量基线是定时任务入口的成功/失败路径，
	// 应与 scheduler_jobs.go 一样上报 backup.scheduled.{success,failed}。
	if err != nil {
		utils.IncBusinessEvent("backup.scheduled.failed")
		return fmt.Errorf("自动全量执行失败: %w", err)
	}
	utils.IncBusinessEvent("backup.scheduled.success")
	return nil
}

// ==================== 辅助函数 ====================

// generateBackupSnowID 生成备份用的 snowid（基于时间戳 + 随机后缀）。
func generateBackupSnowID() (string, error) {
	now := time.Now().UnixNano()
	// 用 utils.NextSnowID 但 fallback 到时间戳（如果未初始化）
	id := utils.NextSnowIDString()
	if id == "" {
		id = fmt.Sprintf("BKP-%d", now)
	}
	return id, nil
}

// addFileToZip 把本地文件添加到 zip 流。
// 与 services/backup.go 中已存在的 addFileToZip 类似但独立（本文件自包含）。
func addFileToZip(zipWriter *zip.Writer, filePath, zipPath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = zipPath

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(writer, file)
	return err
}

// writeStringToZip 把字符串写入 zip 的一个条目。
func writeStringToZip(zipWriter *zip.Writer, name, content string) error {
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, content)
	return err
}

// hashFileTriple 计算文件的 SM3 + SHA-256 + CombinedHash + 大小。
// 使用流式 hash 避免大文件一次性加载到堆内存。
//
// 返回：size, sm3, sha256, combined, error
func hashFileTriple(path string) (int64, string, string, string, error) {
	hs := NewHashService(true, true)
	dh, err := hs.CalculateFileDualHashStreamed(path)
	if err != nil {
		return 0, "", "", "", err
	}
	return dh.DataLength, dh.SM3Hash, dh.SHA256Hash, dh.CombinedHash, nil
}
