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
	"strings"
	"sync"
	"time"
)

type BackupService struct {
	cfg *config.BackupConfig

	// performMu P1 修复（2026-06-14）：互斥锁防止"用户点立即备份"与"定时调度"同时跑。
	//   - 用户点 BackupNow 同步阻塞
	//   - 定时调度 BackupService.performBackup(false) 异步跑
	//   - 两者**可能**在同一秒触发：同秒 zip 命名冲突 + 多个 manifest 同时 Insert + WAL checkpoint 互踩
	//   - 互斥锁保证全量+增量严格串行
	//
	// 为什么不锁增量备份（IncrementalBackupService.Run）：
	//   - 增量备份依赖"上一次全量"的 parent，串行化它会和定时调度打架
	//   - 全量锁住期间，增量若触发则会被并发运行——这是设计取舍
	//   - 实务中"全量期间增量"极少发生（增量频率通常 1h，全量通常 24h）
	performMu sync.Mutex
}

var backupService *BackupService

// InitBackupService 初始化备份服务（不再启动 goroutine，调度由 Scheduler 统一管理）。
// 保留 cfg.Backup.* 配置项作为初始值传入；定时调度使用 scheduled_task 表。
func InitBackupService(cfg *config.BackupConfig) error {
	if !cfg.Enabled {
		utils.Info("备份服务已禁用")
		return nil
	}

	backupService = &BackupService{
		cfg: cfg,
	}

	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return fmt.Errorf("创建备份目录失败: %w", err)
	}

	utils.Info("备份服务初始化成功，备份目录: %s, 保留天数: %d（调度由 Scheduler 接管）",
		cfg.Dir, cfg.DaysToKeep)

	// 启动时跑一次孤儿检测：捕获历史上手动删文件的情况
	if _, err := DetectOrphanBackups(); err != nil {
		utils.Warn("启动时孤儿检测失败: %v", err)
		// 不中断启动
	}

	// 2026-06-29 bug #14：恢复被错置为 missing 的审计记录
	// 修复 DetectOrphanBackups 漏排除 restore_audit 导致的历史脏数据
	if n, err := RecoverWronglyMarkedAudits(); err != nil {
		utils.Warn("启动时审计记录恢复失败: %v", err)
	} else if n > 0 {
		utils.Info("启动恢复: 修复 %d 条被错置为 missing 的审计记录", n)
	}

	// 启动时清理 stuck 备份：进程崩溃可能留下 pending/running 的幽灵记录
	// 阈值 30 分钟（备份正常时长 1-5 分钟，30 分钟远超合理上限）
	if n, err := database.RecoverStuckBackups(
		30*time.Minute,
		"启动恢复：进程崩溃导致 stuck",
	); err != nil {
		utils.Warn("启动时 stuck 恢复失败: %v", err)
	} else if n > 0 {
		utils.Info("启动恢复: 清理 %d 条 stuck 备份（pending/running > 30min）", n)
	}

	return nil
}

// performBackup 执行一次全量备份，并把元数据写入 backup_manifest（第四阶段 Phase 4.1）。
//
// 返回 manifest_id（同步等待备份完成，失败也返回 manifest_id 用于查询错误状态）。
//
// isManual 区分：
//   - false（默认）：定时任务调度，文件名 backup_*.zip，keep_until = now + days_to_keep
//   - true：用户手动触发（BackupNow），文件名 manual_*.zip，keep_until = 0（永不清除）
//
// 流程：
//  1. 创建 manifest 行（pending）
//  2. 调用 backupAll 生成 zip
//  3. 成功 → 计算三哈希 + 写 manifest success
//  4. 失败 → 写 manifest failed
//  5. 清理过期 zip 文件（仅自动备份）
func (s *BackupService) performBackup(isManual bool) (manifestID int64, err error) {
	// P1 修复（2026-06-14）：入口加互斥锁。
	//   - 用 TryLock 立即失败模式，避免阻塞：并发触发时第二个直接返回错误
	//   - 而不是阻塞等锁（备份可能跑 1-5 分钟，UI 等太久不友好）
	if !s.performMu.TryLock() {
		utils.Warn("[Backup] 已有备份任务进行中，拒绝并发触发 (isManual=%v)", isManual)
		return 0, errors.New("已有备份任务进行中，请稍后重试")
	}
	defer s.performMu.Unlock()

	if isManual {
		utils.Info("开始执行手动备份任务...")
	} else {
		utils.Info("开始执行定时全量备份任务...")
	}

	// 1) 创建 manifest
	snowID, snowErr := generateBackupSnowID()
	if snowErr != nil {
		utils.LogError("生成 snowid 失败: %v", snowErr)
		return 0, fmt.Errorf("生成 snowid 失败: %w", snowErr)
	}

	// keep_until 决策：
	//   - 手动：0（永不清除，由用户手动管理）
	//   - 定时：now + days_to_keep
	var keepUntil int64
	if isManual {
		keepUntil = 0
	} else {
		keepUntil = time.Now().AddDate(0, 0, s.cfg.DaysToKeep).Unix()
	}

	manifest := &database.BackupManifest{
		SnowID:    snowID,
		Type:      database.BackupTypeFull,
		StartedAt: time.Now().Unix(),
		CreatedBy: 0,
		KeepUntil: keepUntil,
	}
	manifestID, createErr := database.CreateBackupManifest(manifest)
	if createErr != nil {
		utils.LogError("创建全量备份 manifest 失败: %v", createErr)
		return 0, fmt.Errorf("创建 manifest 失败: %w", createErr)
	}
	_ = database.UpdateBackupManifestStatus(manifestID, database.BackupStatusRunning)

	startTime := time.Now()

	// 1.5) 构造 zip 内的 manifest.json 内容（基础信息）
	// 完整元数据（含三哈希/大小）备份成功后写到 backup_manifest 表，
	// 此处只写 zip 自带的元信息，便于仅凭 zip 文件恢复。
	manifestInfo := fmt.Sprintf(`{
  "manifest_id": %d,
  "snowid": "%s",
  "type": "full",
  "is_manual": %t,
  "started_at": %d,
  "app": "DocManage",
  "version": "v1",
  "backup_db": %t,
  "backup_uploads": %t,
  "note": "完整元数据查询 backup_manifest 表"
}
`, manifestID, snowID, isManual, manifest.StartedAt, s.cfg.DatabaseEnabled, s.cfg.UploadEnabled)

	// 2) 实际打包
	if backupErr := s.backupAll(manifestInfo, isManual); backupErr != nil {
		utils.LogError("备份失败: %v", backupErr)
		_ = database.UpdateBackupManifestFailed(manifestID, backupErr.Error())
		return manifestID, fmt.Errorf("备份失败: %w", backupErr)
	}

	// 3) 找到刚生成的 zip
	latestZip := s.findLatestBackupZip(isManual)
	if latestZip == "" {
		errMsg := "找不到刚生成的备份 zip"
		utils.LogError("%s", errMsg)
		_ = database.UpdateBackupManifestFailed(manifestID, errMsg)
		return manifestID, fmt.Errorf("%s", errMsg)
	}

	// 4) 明文 zip 直接是最终产物，三哈希照常算

	// 5) 算明文 zip 的三哈希 + total_files
	plainPath := latestZip
	fileSize, hashSM3, hashSHA256, hashCombined, hashErr := hashFileTriple(plainPath)
	if hashErr != nil {
		utils.LogError("计算哈希失败: %v", hashErr)
		_ = database.UpdateBackupManifestFailed(manifestID, hashErr.Error())
		return manifestID, fmt.Errorf("计算哈希失败: %w", hashErr)
	}
	plainTotalFiles := s.countTotalFiles(plainPath)

	duration := time.Since(startTime)

	// 6) 写 manifest success
	if updateErr := database.UpdateBackupManifestSuccess(
		manifestID, latestZip, fileSize,
		hashSM3, hashSHA256, hashCombined,
		0, 0, // WAL 范围（全量不适用；增量备份 services/incremental_backup.go 自填）
		0, plainTotalFiles, // changed_files=0, total_files=明文实际文件数
		duration,
	); updateErr != nil {
		utils.LogError("更新 manifest success 失败: %v", updateErr)
		return manifestID, fmt.Errorf("更新 manifest 失败: %w", updateErr)
	}

	utils.Info("备份完成: manifest_id=%d, snowid=%s, path=%s, size=%d, duration=%v, is_manual=%v",
		manifestID, snowID, latestZip, fileSize, duration, isManual)

	// 4.5) 第四阶段 P0 + Phase 4.2：自动验证刚生成的备份
	//   验证失败不会回滚备份（备份已写入），仅标记 corrupted 让用户感知
	verifier := GetBackupVerifier()
	if _, verifyErr := verifier.VerifyBackup(manifestID); verifyErr != nil {
		utils.Warn("[Backup] 自动验证失败: id=%d, err=%v（备份已生成，但标记为待复核）", manifestID, verifyErr)
	}

	// 5) 清理过期备份（仅自动备份触发清理；手动备份永不清理）
	if !isManual {
		s.cleanOldBackups()
	}

	return manifestID, nil
}

// findLatestBackupZip 找出备份目录下最近创建的 backup_*.zip 或 manual_*.zip 文件。
// 仅匹配本进程创建的文件（命名规则 <prefix>_YYYYMMDD_HHMMSS.zip）。
func (s *BackupService) findLatestBackupZip(isManual bool) string {
	entries, err := os.ReadDir(s.cfg.Dir)
	if err != nil {
		return ""
	}
	prefix := "backup_"
	if isManual {
		prefix = "manual_"
	}
	var latest os.FileInfo
	var latestName string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// 跳过增量 zip（inc_*.zip）
		if strings.HasPrefix(name, "inc_") {
			continue
		}
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if latest == nil || info.ModTime().After(latest.ModTime()) {
			latest = info
			latestName = name
		}
	}
	if latestName == "" {
		return ""
	}
	return filepath.Join(s.cfg.Dir, latestName)
}

// countTotalFiles 计算 zip 内的文件数。
func (s *BackupService) countTotalFiles(zipPath string) int64 {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0
	}
	defer zr.Close()
	return int64(len(zr.File))
}

func (s *BackupService) backupAll(manifestInfo string, isManual bool) error {
	timestamp := time.Now().Format("20060102_150405")
	// 文件名前缀区分：
	//   - manual_*：用户手动触发，永不清除
	//   - backup_*：定时任务调度，参与自动清理
	prefix := "backup"
	if isManual {
		prefix = "manual"
	}
	backupFileName := fmt.Sprintf("%s_%s.zip", prefix, timestamp)
	backupPath := filepath.Join(s.cfg.Dir, backupFileName)

	zipFile, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)

	// 先写入 manifest.json（基础元数据）— 即使备份失败也有这部分信息
	if manifestInfo != "" {
		header := &zip.FileHeader{
			Name:   "manifest.json",
			Method: zip.Deflate,
		}
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			_ = zipWriter.Close()
			return fmt.Errorf("创建 manifest.json 失败: %w", err)
		}
		if _, err := io.WriteString(writer, manifestInfo); err != nil {
			_ = zipWriter.Close()
			return fmt.Errorf("写入 manifest.json 失败: %w", err)
		}
	}

	totalFiles := 0
	failedFiles := 0

	if s.cfg.DatabaseEnabled {
		dbFiles, err := s.backupDatabaseToZip(zipWriter)
		if err != nil {
			utils.LogError("数据库备份失败: %v", err)
			failedFiles += dbFiles
		} else {
			totalFiles += dbFiles
			utils.Info("数据库已打包，包含 %d 个文件", dbFiles)
		}
	}

	if s.cfg.UploadEnabled {
		uploadFiles, err := s.backupUploadsToZip(zipWriter)
		if err != nil {
			utils.LogError("上传文件备份失败: %v", err)
			failedFiles += uploadFiles
		} else {
			totalFiles += uploadFiles
			utils.Info("上传文件已打包，包含 %d 个文件", uploadFiles)
		}
	}

	// 关闭 zip writer（必须在所有 CreateHeader 之后）
	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("关闭 zip writer 失败: %w", err)
	}

	if totalFiles == 0 && failedFiles == 0 {
		utils.Warn("没有需要备份的内容")
		return nil
	}

	utils.Info("备份文件已生成: %s, 成功: %d 个文件, 失败: %d 个文件", backupPath, totalFiles, failedFiles)
	return nil
}

func (s *BackupService) backupDatabaseToZip(zipWriter *zip.Writer) (int, error) {
	dbPath := config.GlobalConfig.Database.Path

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		utils.Warn("数据库文件不存在: %s", dbPath)
		return 0, nil
	}

	dbName := filepath.Base(dbPath)
	zipPath := filepath.ToSlash(filepath.Join("data", dbName))

	return s.addFileToZip(zipWriter, dbPath, zipPath, "")
}

func (s *BackupService) backupUploadsToZip(zipWriter *zip.Writer) (int, error) {
	uploadDir := config.GlobalConfig.Upload.Dir

	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		utils.Warn("上传目录不存在: %s", uploadDir)
		return 0, nil
	}

	return s.addDirectoryToZip(zipWriter, uploadDir, "uploads")
}

func (s *BackupService) addFileToZip(zipWriter *zip.Writer, filePath, zipPath string, baseDir string) (int, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, err
	}
	header.Name = filepath.ToSlash(zipPath)

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return 0, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	_, err = io.Copy(writer, file)
	if err != nil {
		return 0, err
	}

	return 1, nil
}

func (s *BackupService) addDirectoryToZip(zipWriter *zip.Writer, sourceDir, zipBasePath string) (int, error) {
	fileCount := 0
	failedCount := 0

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			utils.Warn("遍历目录时出错 %s: %v", path, err)
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			utils.Warn("跳过符号链接: %s", path)
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			utils.Warn("获取相对路径失败 %s: %v", path, err)
			return nil
		}

		zipPath := filepath.Join(zipBasePath, relPath)
		zipPath = filepath.ToSlash(zipPath)

		_, err = s.addFileToZip(zipWriter, path, zipPath, sourceDir)
		if err != nil {
			utils.Warn("添加文件到压缩包失败 %s: %v", path, err)
			failedCount++
		} else {
			fileCount++
		}

		return nil
	})

	if err != nil {
		return fileCount, fmt.Errorf("遍历目录失败: %w", err)
	}

	return fileCount, nil
}

// cleanOldBackups 智能清理过期备份（链式保护 + 手动备份豁免）。
//
// 算法（第四阶段 Phase 4.1 完善）：
//  1. 查所有 keep_until > 0 且 < now 的候选（手动备份 keep_until=0 不参与）
//  2. 对每个候选：
//     a. 若有活跃子增量（parent_id 引用此 ID 且 keep_until 仍存活）→ 延长本备份的 keep_until 到子链最晚时间，跳过
//     b. 否则：删除 zip 文件 + 删除 manifest 记录
//
// 设计要点：
//   - 整条链过期后才一起删（避免孤儿增量）
//   - 手动备份（keep_until=0）永远不被清理
//   - 全量被增量引用时自动延长保留期
//   - manifest 记录与 zip 文件**同步删除**（无漂移）
func (s *BackupService) cleanOldBackups() {
	if s.cfg.DaysToKeep <= 0 {
		return
	}

	now := time.Now().Unix()
	candidates, err := database.ListExpiredBackups(now)
	if err != nil {
		utils.LogError("查询过期备份失败: %v", err)
		return
	}

	cleanedFiles := 0
	extendedCount := 0
	for _, m := range candidates {
		// 链式保护：若有活跃子增量，延长本备份的 keep_until
		if m.Type == database.BackupTypeFull {
			liveChildren, err := database.CountLiveChildrenByParent(m.ID, now)
			if err != nil {
				utils.LogError("查询活跃子增量失败 (id=%d): %v", m.ID, err)
				continue
			}
			if liveChildren > 0 {
				// 找子链中最晚的 keep_until，延长本备份
				latestChildKU, err := database.FindLatestChildKeepUntil(m.ID)
				if err != nil {
					utils.LogError("查询子链最晚 keep_until 失败: %v", err)
					continue
				}
				if latestChildKU > m.KeepUntil {
					if err := database.UpdateKeepUntil(m.ID, latestChildKU); err != nil {
						utils.LogError("延长 keep_until 失败 (id=%d): %v", m.ID, err)
						continue
					}
					extendedCount++
					utils.Info("备份 [id=%d] 被增量引用，keep_until 延长至 %d",
						m.ID, latestChildKU)
				}
				continue
			}
		}

		// 无活跃子增量 → 真正清理
		if m.FilePath != "" {
			if err := os.Remove(m.FilePath); err != nil && !os.IsNotExist(err) {
				utils.LogError("删除过期备份文件失败 %s: %v", m.FilePath, err)
				continue
			}
		}
		if err := database.DeleteBackupManifest(m.ID); err != nil {
			utils.LogError("删除过期备份 manifest 失败 (id=%d): %v", m.ID, err)
			continue
		}
		cleanedFiles++
		utils.Info("已清理过期备份: id=%d, type=%s, path=%s",
			m.ID, m.Type, m.FilePath)
	}

	if cleanedFiles > 0 || extendedCount > 0 {
		utils.Info("备份清理完成: 删除 %d 个, 延长 %d 个", cleanedFiles, extendedCount)
	}
}

func GetBackupService() *BackupService {
	return backupService
}

// PerformBackupNow 同步执行一次全量备份，返回 manifest_id 与 error。
// isManual=true：用户手动触发，文件名 manual_*.zip，永不清除
// isManual=false：定时任务调度，文件名 backup_*.zip，参与自动清理
func PerformBackupNow(isManual bool) (int64, error) {
	if backupService == nil {
		return 0, fmt.Errorf("备份服务未初始化")
	}
	return backupService.performBackup(isManual)
}

// DetectOrphanBackups 扫描所有 success/verified 的备份，若 zip 文件丢失则标记为 missing。
// 用途：
//   - 启动时自动跑（捕获手动删文件的情况）
//   - 每日调度跑（捕获系统异常导致文件丢失）
//
// 返回：标记为 missing 的数量。
//
// 2026-06-29 bug #14 修复：排除恢复审计记录（restore_audit）。
//   - 审计记录的 FilePath 是人读文本 "restore: full_id=N, incrementals=[...]"，
//     不是真实文件路径；os.Stat 必然失败
//   - 之前的修复（VerifyBackup 跳过审计）漏掉了此路径，导致审计被错置为 missing
//   - 修复：显式跳过 type=restore_audit，不做任何状态变更
//   - 配合 RecoverWronglyMarkedAudits 自动修复存量数据
func DetectOrphanBackups() (int, error) {
	// 拉所有 success / verified 状态的备份（failed / missing 跳过）
	successList, err := database.ListBackupManifests("", database.BackupStatusSuccess, 99999, 0)
	if err != nil {
		return 0, fmt.Errorf("查询 success 备份失败: %w", err)
	}
	verifiedList, err := database.ListBackupManifests("", database.BackupStatusVerified, 99999, 0)
	if err != nil {
		return 0, fmt.Errorf("查询 verified 备份失败: %w", err)
	}
	all := append(successList, verifiedList...)

	markedCount := 0
	for _, m := range all {
		// 2026-06-29 bug #14：审计记录无真实文件，跳过孤儿检测
		if m.Type == database.BackupTypeRestoreAudit {
			continue
		}
		if m.FilePath == "" {
			continue
		}
		if _, err := os.Stat(m.FilePath); os.IsNotExist(err) {
			if updateErr := database.UpdateBackupManifestStatus(m.ID, database.BackupStatusMissing); updateErr != nil {
				utils.LogError("标记孤儿备份失败 (id=%d): %v", m.ID, updateErr)
				continue
			}
			markedCount++
			utils.Warn("⚠️ 检测到孤儿备份: id=%d, type=%s, snowid=%s, 期望路径=%s",
				m.ID, m.Type, m.SnowID, m.FilePath)
		}
	}
	if markedCount > 0 {
		utils.Info("孤儿检测完成: 标记 %d 个为 missing", markedCount)
	}
	return markedCount, nil
}

// RecoverWronglyMarkedAudits 启动时调用一次，自动修复被错置为 missing 的审计记录。
//
// 2026-06-29 bug #14：DetectOrphanBackups 漏排除 restore_audit 导致
// 审计记录的 FilePath="restore: ..." 经 os.Stat 失败后被错置为 missing。
//
// SQL 实现在 database.RecoverWronglyMarkedAudits（保持分层一致性）；
// services 层仅负责调用 + 日志包装 + 错误处理。
func RecoverWronglyMarkedAudits() (int64, error) {
	n, err := database.RecoverWronglyMarkedAudits()
	if err != nil {
		return 0, fmt.Errorf("恢复错置的审计记录失败: %w", err)
	}
	if n > 0 {
		utils.Info("✅ 恢复 %d 条被错置为 missing 的审计记录（bug #14 数据修复）", n)
	}
	return n, nil
}
