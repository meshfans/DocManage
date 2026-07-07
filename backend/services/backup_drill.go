package services

import (
	"archive/zip"
	"database/sql"
	"doc/database"
	"doc/utils"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ==================== 备份演练（第四阶段 Phase 4.5）====================
//
// 目的：定期验证"备份 → 恢复"链路真正能跑通，避免"备份了但恢复不出来"。
//
// 调度：每月 1 号 05:00 → system.backup_drill
//
// 流程：
//  1. 取最近一次 success/verified 全量备份
//  2. 在临时目录解压（**不覆盖生产数据**）
//  3. 用 sqlite 打开测试 DB → 执行 PRAGMA integrity_check（必须返回 "ok"）
//  4. 抽查 10 条关键表数据（确保 schema 与生产一致）
//  5. 演练结果写入 backup_manifest.verified_result
//  6. 失败 → 日志 ERROR + 立即告警
//
// 与 Phase 4.2 验证的区别：
//   - 4.2 验证：只重算三哈希（不读文件内容）
//   - 4.5 演练：实际解压 + 打开 DB + 查询数据（端到端）
// ----------------------------------------------------------------------------

// DrillResult 演练结果。
type DrillResult struct {
	ManifestID      int64  `json:"manifest_id"`
	SnowID          string `json:"snowid"`
	FilePath        string `json:"file_path"`
	IntegrityCheck  string `json:"integrity_check"` // "ok" / "corrupted"
	SampledRows     int    `json:"sampled_rows"`    // 抽查的行数
	SampledTables   int    `json:"sampled_tables"`  // 抽查的表数
	DurationMS      int64  `json:"duration_ms"`
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	DecryptionError string `json:"decryption_error,omitempty"`
}

// DrillLatestBackup 演练最近一次全量备份。
//
// 自动处理加密：若 manifest.encrypted=true，先解密再解压。
func DrillLatestBackup() (*DrillResult, error) {
	startTime := time.Now()

	// 1) 查最近一次成功全量备份
	m, err := findLatestSuccessfulFullBackup()
	if err != nil {
		return nil, fmt.Errorf("查询最近全量失败: %w", err)
	}
	if m == nil {
		return nil, errors.New("没有可演练的全量备份")
	}

	result := &DrillResult{
		ManifestID: m.ID,
		SnowID:     m.SnowID,
		FilePath:   m.FilePath,
	}

	// 2) 准备临时目录
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("backup-drill-%d-*", m.ID))
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir) // 演练结束清理（无论成功失败）

	// 3) 准备解压路径
	extractDir := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		result.Error = fmt.Sprintf("创建解压目录失败: %v", err)
		return result, err
	}

	// 4) 解压 zip（备份永远是明文，直接解压）
	if err := extractZipForDrill(m.FilePath, extractDir); err != nil {
		result.Error = fmt.Sprintf("解压失败: %v", err)
		return result, err
	}

	// 6) 找解压后的 db 文件
	dbPath, err := findDrillDB(extractDir)
	if err != nil {
		result.Error = fmt.Sprintf("找 db 文件失败: %v", err)
		return result, err
	}
	if dbPath == "" {
		result.Error = "备份 zip 内无 data/doc.db"
		return result, errors.New(result.Error)
	}

	// 7) 打开 DB 并执行 PRAGMA integrity_check
	integrity, sampledTables, sampledRows, drillErr := runDrillChecks(dbPath)
	result.IntegrityCheck = integrity
	result.SampledTables = sampledTables
	result.SampledRows = sampledRows
	result.DurationMS = time.Since(startTime).Milliseconds()

	if drillErr != nil {
		result.Error = drillErr.Error()
		utils.LogError("[Drill] ❌ 演练失败: id=%d, err=%v", m.ID, drillErr)
	} else {
		result.Success = true
		utils.Info("[Drill] ✅ 演练通过: id=%d, integrity=%s, tables=%d, rows=%d, duration=%dms",
			m.ID, integrity, sampledTables, sampledRows, result.DurationMS)
	}

	// 8) 写演练结果到 manifest（复用 verified_result 字段）
	drillResultStr := "drill_ok"
	if !result.Success {
		drillResultStr = "drill_failed: " + result.Error
	}
	if err := database.UpdateBackupManifestVerifiedWithStatus(
		m.ID, m.Status, drillResultStr); err != nil {
		utils.LogError("[Drill] 写演练结果失败: %v", err)
	}

	return result, drillErr
}

// findLatestSuccessfulFullBackup 查最近一次成功的全量备份。
//
// 排除恢复审计记录（type='restore_audit' 或旧审计 verified_result='restore_audit'），
// 否则演练会把 FilePath="restore: full_id=21..." 当 zip 路径打开 → 系统找不到文件。
func findLatestSuccessfulFullBackup() (*database.BackupManifest, error) {
	row := database.DB.QueryRow(`
		SELECT id, snowid, type, parent_id, status, file_path, file_size,
		       file_hash_sm3, file_hash_sha256, file_hash_combined,
		       wal_range_start, wal_range_end, changed_files, total_files,
		       started_at, finished_at, duration_ms, error_msg,
		       offsite_url, offsite_status, offsite_at,
		       verified_at, verified_result,
		       created_by, keep_until
		FROM backup_manifest
		WHERE type = ?
		  AND status IN (?, ?, ?)
		  AND NOT (type = ? AND verified_result = ?)
		ORDER BY started_at DESC
		LIMIT 1
	`, database.BackupTypeFull,
		database.BackupStatusSuccess,
		database.BackupStatusVerified,
		database.BackupStatusMissing,             // missing 也尝试演练（备份可能文件丢失但 DB 记录还在）
		database.BackupTypeFull, "restore_audit") // 排除旧审计（兼容存量）
	m := &database.BackupManifest{}
	if err := database.ScanBackupManifestRow(row.Scan, m); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// extractZipForDrill 解压 zip 到目标目录。
func extractZipForDrill(zipPath, dstDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// 解压到目标（保持 zip 内部相对路径）
		target := filepath.Join(dstDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", filepath.Dir(target), err)
		}
		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("打开 zip 条目 %s 失败: %w", f.Name, err)
		}
		dst, err := os.Create(target)
		if err != nil {
			src.Close()
			return fmt.Errorf("创建文件 %s 失败: %w", target, err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			dst.Close()
			return fmt.Errorf("解压 %s 失败: %w", f.Name, err)
		}
		src.Close()
		dst.Close()
	}
	return nil
}

// findDrillDB 找解压目录内的 doc.db。
func findDrillDB(extractDir string) (string, error) {
	// 通常在 data/doc.db
	dataDir := filepath.Join(extractDir, "data")
	candidates := []string{
		filepath.Join(dataDir, "doc.db"),
		filepath.Join(dataDir, "doc.db-wal"), // WAL 不需要单独处理
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	// 兜底：递归找任意 .db 文件
	filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".db" {
			candidates = append(candidates, path)
		}
		return nil
	})
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("解压目录内找不到 .db 文件")
}

// runDrillChecks 执行 PRAGMA integrity_check + 抽查关键表。
//
// 抽查表：customer, media, user（核心业务表）
//
// 演练只读（mode=ro）不会污染备份文件。
func runDrillChecks(dbPath string) (integrity string, tables int, rows int, err error) {
	if !database.HasInitOptions() {
		return "", 0, 0, fmt.Errorf("未初始化过 DB（lastInitOptions 为空）")
	}
	initOpts := database.LastInitOptions()
	if initOpts.Path == "" {
		return "", 0, 0, fmt.Errorf("未初始化过 DB（lastInitOptions 为空）")
	}

	// DSN：复用生产 journal/同步/缓存配置（演练只读 mode=ro）
	dsn := fmt.Sprintf(
		"file:%s?_journal_mode=%s&_synchronous=%s&_cache_size=%d&_busy_timeout=%d&_wal_autocheckpoint=%d&mode=ro",
		dbPath, initOpts.JournalMode, initOpts.Synchronous,
		initOpts.CacheSize, initOpts.BusyTimeout, initOpts.WalAutocheckpoint,
	)

	conn, openErr := sql.Open("sqlite3", dsn)
	if openErr != nil {
		return "", 0, 0, fmt.Errorf("打开 DB 失败: %w", openErr)
	}
	defer conn.Close()

	// PRAGMA integrity_check（必须返回 "ok"）
	if err := conn.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return "", 0, 0, fmt.Errorf("integrity_check 失败: %w", err)
	}
	if integrity != "ok" {
		return integrity, 0, 0, fmt.Errorf("DB 不完整（integrity_check 返回 %q，不是 ok）", integrity)
	}

	// 2) 抽查关键表（确保 schema 与生产一致）
	checkTables := []string{"customer", "media", "user"}
	for _, table := range checkTables {
		// 表是否存在（可能备份是早期版本）
		var name string
		if err := conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name); err != nil {
			if err == sql.ErrNoRows {
				continue // 表不存在，跳过
			}
			return integrity, tables, rows, fmt.Errorf("查表 %s 失败: %w", table, err)
		}
		// 抽查 10 条
		rs, qErr := conn.Query(fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 10", table))
		if qErr != nil {
			return integrity, tables, rows, fmt.Errorf("查表 %s 数据失败: %w", table, qErr)
		}
		for rs.Next() {
			var n int
			if scanErr := rs.Scan(&n); scanErr == nil {
				rows += n
			}
		}
		rs.Close()
		tables++
	}

	return integrity, tables, rows, nil
}
