package database

import (
	"database/sql"
	"fmt"
	"time"
)

// 本文件：备份清单（backup_manifest）表 CRUD 操作。
//
// 第四阶段 Phase 4.1：
//   - full / incremental 备份记录
//   - parent_id 链接增量 → 全量
//   - 双哈希（SM3 + SHA-256）用于完整性校验
//   - 异地备份追踪（offsite_* 字段）

// ==================== Backup Manifest ====================

// BackupType 备份类型
const (
	BackupTypeFull        = "full"
	BackupTypeIncremental = "incremental"
	// BackupTypeRestoreAudit 恢复审计记录（不是真正的备份文件）
	//   - 2026-06-27 修复演练失败 bug #2：之前用 BackupTypeFull 占位，导致
	//     findLatestSuccessfulFullBackup / GetLatestFullBackup 把审计记录当备份，
	//     FilePath = "restore: full_id=21, ..." 当 zip 路径打开 → 系统找不到文件
	//   - 现在用独立 Type 标识，所有 type='full' 的查询自然排除审计记录
	BackupTypeRestoreAudit = "restore_audit"
)

// BackupStatus 备份状态
const (
	BackupStatusPending   = "pending"   // 刚创建，未执行
	BackupStatusRunning   = "running"   // 正在打包
	BackupStatusSuccess   = "success"   // 打包成功
	BackupStatusFailed    = "failed"    // 打包失败
	BackupStatusVerified  = "verified"  // 已验证完整
	BackupStatusCorrupted = "corrupted" // 验证失败
	BackupStatusMissing   = "missing"   // 文件丢失（孤儿，zip 不存在）
)

// BackupManifest 备份清单记录。
type BackupManifest struct {
	ID       int64  `json:"id"`
	SnowID   string `json:"snowid"`
	Type     string `json:"type"` // full / incremental
	ParentID int64  `json:"parent_id"`
	Status   string `json:"status"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
	// 三哈希（与 media / seal / contract 表保持一致）
	//   SM3       — 国密哈希（合规）
	//   SHA256    — 国际标准哈希
	//   Combined  — SHA256(SM3 + SHA256)，交叉校验冗余
	FileHashSM3      string `json:"file_hash_sm3"`
	FileHashSHA256   string `json:"file_hash_sha256"`
	FileHashCombined string `json:"file_hash_combined"`

	// 2026-07-06 round5 精简：加密标记字段（Encrypted / EncryptionAlgo / EncryptPassphrase / HkdfInputsHash）已删除
	//   - 备份 zip 不再 AES-256-GCM 加密
	//   - 历史 DB 中 encrypted=true 的行：colScan 仍会读到字段（schema 表头还在），但 Go struct 不再持有
	//     → 用 backupManifestCols_new 重新映射 SELECT 列表，**不** SELECT 这些列
	//   - 用户确认会删 doc.db 重置 → schema CREATE TABLE 也不再有这些列

	// 增量备份专用
	WALRangeStart int64 `json:"wal_range_start"`
	WALRangeEnd   int64 `json:"wal_range_end"`
	ChangedFiles  int64 `json:"changed_files"`

	// 全量备份专用
	TotalFiles int64 `json:"total_files"`

	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	DurationMS int64  `json:"duration_ms"`
	ErrorMsg   string `json:"error_msg"`

	// 异地备份追踪（Phase 4.4 用）
	OffsiteURL     string `json:"offsite_url"`
	OffsiteStatus  string `json:"offsite_status"`
	OffsiteAt      int64  `json:"offsite_at"`
	VerifiedAt     int64  `json:"verified_at"`
	VerifiedResult string `json:"verified_result"`

	CreatedBy int64 `json:"created_by"`

	// KeepUntil 自动清理截止时间。
	//   0 = 永不清除（手动备份 / 用户保护的关键备份）
	//   >0 = unix 时间戳，过期后由 cleanOldBackups 清理
	// 设计：增量备份的 keep_until = max(父全量.keep_until, 自身 started_at + days_to_keep)
	// 这样整条链过期后才一起删，避免孤儿。
	KeepUntil int64 `json:"keep_until"`
}

// backupManifestCols 是查询 backup_manifest 全部字段的列名。
// 2026-07-06 round5 精简：移除 encrypted / encryption_algo / encrypt_passphrase / hkdf_inputs_hash 四列
//   - 用户确认会删 doc.db 重置 → schema 不再创建这些列
const backupManifestCols = `
	id, snowid, type, parent_id, status, file_path, file_size,
	file_hash_sm3, file_hash_sha256, file_hash_combined,
	wal_range_start, wal_range_end, changed_files, total_files,
	started_at, finished_at, duration_ms, error_msg,
	offsite_url, offsite_status, offsite_at,
	verified_at, verified_result,
	created_by, keep_until
`

// ScanBackupManifestRow 将单行扫描到 BackupManifest 指针。
// 接受 QueryRow().Scan 或 Rows().Scan 的可变参数接口。
// 导出供 services 包使用（第四阶段 Phase 4.5 备份演练）。
// 2026-07-06 round5 精简：移除 4 个加密相关 scan 目标
func ScanBackupManifestRow(scan func(...interface{}) error, m *BackupManifest) error {
	var (
		finishedAt, offsiteAt, verifiedAt sql.NullInt64
		parentID                          sql.NullInt64
	)
	if err := scan(
		&m.ID, &m.SnowID, &m.Type, &parentID, &m.Status, &m.FilePath, &m.FileSize,
		&m.FileHashSM3, &m.FileHashSHA256, &m.FileHashCombined,
		&m.WALRangeStart, &m.WALRangeEnd, &m.ChangedFiles, &m.TotalFiles,
		&m.StartedAt, &finishedAt, &m.DurationMS, &m.ErrorMsg,
		&m.OffsiteURL, &m.OffsiteStatus, &offsiteAt,
		&verifiedAt, &m.VerifiedResult,
		&m.CreatedBy, &m.KeepUntil,
	); err != nil {
		return err
	}
	if parentID.Valid {
		m.ParentID = parentID.Int64
	}
	if finishedAt.Valid {
		m.FinishedAt = finishedAt.Int64
	}
	if offsiteAt.Valid {
		m.OffsiteAt = offsiteAt.Int64
	}
	if verifiedAt.Valid {
		m.VerifiedAt = verifiedAt.Int64
	}
	return nil
}

// CreateBackupManifest 插入一条备份清单记录（status 默认 pending）。
// m.KeepUntil: 0 = 永不清除（手动备份 / 重要备份），>0 = unix 时间戳（自动清理截止时间）。
func CreateBackupManifest(m *BackupManifest) (int64, error) {
	if m.SnowID == "" {
		return 0, fmt.Errorf("snowid 不能为空")
	}
	if m.Type != BackupTypeFull && m.Type != BackupTypeIncremental && m.Type != BackupTypeRestoreAudit {
		return 0, fmt.Errorf("type 必须是 full / incremental / restore_audit，实际=%s", m.Type)
	}

	res, err := DB.Exec(`
		INSERT INTO backup_manifest (
			snowid, type, parent_id, status, file_path,
			started_at, created_by, keep_until
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		m.SnowID, m.Type, m.ParentID, BackupStatusPending, m.FilePath,
		m.StartedAt, m.CreatedBy, m.KeepUntil,
	)
	if err != nil {
		return 0, fmt.Errorf("插入 backup_manifest 失败: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取 LastInsertId 失败: %w", err)
	}
	m.ID = id
	return id, nil
}

// UpdateBackupManifestSuccess 标记备份成功（写文件元数据 + 三哈希）。
// 三哈希：SM3 + SHA-256 + CombinedHash（与第三阶段哈希约定一致）。
func UpdateBackupManifestSuccess(id int64, filePath string, fileSize int64,
	hashSM3, hashSHA256, hashCombined string,
	walStart, walEnd, changedFiles, totalFiles int64,
	duration time.Duration) error {

	_, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, file_path = ?, file_size = ?,
		    file_hash_sm3 = ?, file_hash_sha256 = ?, file_hash_combined = ?,
		    wal_range_start = ?, wal_range_end = ?,
		    changed_files = ?, total_files = ?,
		    finished_at = ?, duration_ms = ?, error_msg = ''
		WHERE id = ?
	`,
		BackupStatusSuccess, filePath, fileSize,
		hashSM3, hashSHA256, hashCombined,
		walStart, walEnd,
		changedFiles, totalFiles,
		time.Now().Unix(), duration.Milliseconds(),
		id,
	)
	return err
}

// UpdateBackupManifestFailed 标记备份失败。
func UpdateBackupManifestFailed(id int64, errMsg string) error {
	_, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, error_msg = ?, finished_at = ?
		WHERE id = ?
	`,
		BackupStatusFailed, errMsg, time.Now().Unix(), id,
	)
	return err
}

// UpdateBackupManifestStatus 通用状态更新（用于 running / verified / corrupted）。
func UpdateBackupManifestStatus(id int64, status string) error {
	_, err := DB.Exec(`UPDATE backup_manifest SET status = ? WHERE id = ?`, status, id)
	return err
}

// UpdateBackupManifestVerified 标记验证结果。
func UpdateBackupManifestVerified(id int64, result string) error {
	_, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, verified_at = ?, verified_result = ?
		WHERE id = ?
	`,
		BackupStatusVerified, time.Now().Unix(), result, id,
	)
	return err
}

// UpdateBackupManifestVerifiedWithStatus 标记验证结果（支持 corrupted / missing 等状态）。
//
// 第四阶段 Phase 4.2：备份验证可能产生 verified / corrupted / missing 三种结果。
//   - status = verified  + result = "ok"        ：三哈希一致
//   - status = corrupted + result = "corrupted" ：哈希不匹配
//   - status = missing   + result = "missing"   ：文件丢失
func UpdateBackupManifestVerifiedWithStatus(id int64, status, result string) error {
	_, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, verified_at = ?, verified_result = ?
		WHERE id = ?
	`,
		status, time.Now().Unix(), result, id,
	)
	return err
}

// 2026-07-06 round5 精简：SetBackupManifestEncrypted 已删除（备份不再加密，标记列已下线）

// GetBackupManifestByID 按 ID 查询。
func GetBackupManifestByID(id int64) (*BackupManifest, error) {
	row := DB.QueryRow(`SELECT `+backupManifestCols+` FROM backup_manifest WHERE id = ?`, id)
	m := &BackupManifest{}
	if err := ScanBackupManifestRow(row.Scan, m); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// GetLatestFullBackup 查询最近一次成功的全量备份。
// 用于增量备份时锁定 parent_id。
//
// 2026-06-27 bug #2 修复：排除恢复审计记录
//   - 新审计：type = 'restore_audit' → 已通过 "type = BackupTypeFull" 过滤自动排除
//   - 旧审计（兼容存量）：type='full' 但 verified_result='restore_audit' → 用 NOT 子句显式排除
//     （兼容存量数据，等下一次手动恢复触发 writeRestoreAudit 自动转新类型）
//
// 2026-06-27 🟡 #4 改造：与 services/backup_drill.go findLatestSuccessfulFullBackup 保持一致
//
//	正向过滤为主（type=BackupTypeFull），NOT 子句显式排除旧审计
func GetLatestFullBackup() (*BackupManifest, error) {
	row := DB.QueryRow(`
		SELECT `+backupManifestCols+`
		FROM backup_manifest
		WHERE type = ?
		  AND status IN (?, ?)
		  AND NOT (type = ? AND verified_result = ?)
		ORDER BY started_at DESC
		LIMIT 1
	`, BackupTypeFull, BackupStatusSuccess, BackupStatusVerified,
		BackupTypeFull, "restore_audit") // 排除旧审计（type=full 且 verified_result=restore_audit）
	m := &BackupManifest{}
	if err := ScanBackupManifestRow(row.Scan, m); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// GetLatestIncrementalAfter 查询某个全量之后的所有成功增量备份（按时间升序）。
// 用于恢复时按顺序应用增量。
func GetLatestIncrementalAfter(parentID int64) ([]*BackupManifest, error) {
	rows, err := DB.Query(`
		SELECT `+backupManifestCols+`
		FROM backup_manifest
		WHERE type = ? AND parent_id = ? AND status IN (?, ?)
		ORDER BY started_at ASC
	`, BackupTypeIncremental, parentID, BackupStatusSuccess, BackupStatusVerified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*BackupManifest
	for rows.Next() {
		m := &BackupManifest{}
		if err := ScanBackupManifestRow(rows.Scan, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListBackupManifests 分页列表 + 过滤。
// typeFilter / statusFilter 为空时不过滤。
func ListBackupManifests(typeFilter, statusFilter string, limit, offset int) ([]*BackupManifest, error) {
	query := `SELECT ` + backupManifestCols + ` FROM backup_manifest WHERE 1=1`
	args := []interface{}{}

	if typeFilter != "" {
		query += ` AND type = ?`
		args = append(args, typeFilter)
	}
	if statusFilter != "" {
		query += ` AND status = ?`
		args = append(args, statusFilter)
	}

	query += ` ORDER BY started_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*BackupManifest
	for rows.Next() {
		m := &BackupManifest{}
		if err := ScanBackupManifestRow(rows.Scan, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteBackupManifest 删除备份清单记录（不删文件，由调用方负责）。
func DeleteBackupManifest(id int64) error {
	_, err := DB.Exec(`DELETE FROM backup_manifest WHERE id = ?`, id)
	return err
}

// CountBackupManifestsByStatus 统计各状态数量（用于管理面板）。
func CountBackupManifestsByStatus() (map[string]int64, error) {
	rows, err := DB.Query(`SELECT status, COUNT(*) FROM backup_manifest GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

// CountBackupManifests 按 type + status 过滤后统计总数（用于分页）。
// filter 为空时不过滤。
func CountBackupManifests(typeFilter, statusFilter string) (int64, error) {
	query := `SELECT COUNT(*) FROM backup_manifest WHERE 1=1`
	args := []interface{}{}
	if typeFilter != "" {
		query += ` AND type = ?`
		args = append(args, typeFilter)
	}
	if statusFilter != "" {
		query += ` AND status = ?`
		args = append(args, statusFilter)
	}
	var total int64
	err := DB.QueryRow(query, args...).Scan(&total)
	return total, err
}

// RecoverStuckBackups 把 stuck 的 pending/running 备份标记为 failed。
//
// 用途：进程崩溃 / 服务被 kill 时，pending 和 running 状态的 manifest 可能永远卡住。
// 启动时 + 每日调度调用此函数，清理这些"幽灵"记录。
//
// 判定规则：started_at < cutoffUnix（即启动超过 maxAge 的 pending/running）→ 标记为 failed。
//
// 返回：标记为 failed 的条数。
func RecoverStuckBackups(maxAge time.Duration, reason string) (int64, error) {
	cutoffUnix := time.Now().Add(-maxAge).Unix()
	res, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, error_msg = ?, finished_at = ?
		WHERE status IN (?, ?) AND started_at < ?
	`,
		BackupStatusFailed, reason, time.Now().Unix(),
		BackupStatusPending, BackupStatusRunning, cutoffUnix,
	)
	if err != nil {
		return 0, fmt.Errorf("恢复 stuck 备份失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ListExpiredBackups 列出 keep_until > 0 且 < now 的备份（链式清理用）。
// 返回所有已到期但仍"可能"可清理的候选（不含手动备份，keep_until=0 不参与）。
func ListExpiredBackups(now int64) ([]*BackupManifest, error) {
	rows, err := DB.Query(`
		SELECT `+backupManifestCols+`
		FROM backup_manifest
		WHERE keep_until > 0 AND keep_until < ?
		ORDER BY keep_until ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*BackupManifest
	for rows.Next() {
		m := &BackupManifest{}
		if err := ScanBackupManifestRow(rows.Scan, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountLiveChildrenByParent 统计某 parent_id 下"仍存活"（keep_until > 0 且 >= now）的子增量数量。
// 用于链式清理：若有活跃子增量，父全量不删。
//
// 2026-06-27 修复：明确加 type = BackupTypeIncremental 过滤
//   - 恢复审计记录（type=restore_audit）也用了 parent_id 指向被恢复的全量备份
//     （writeRestoreAudit 设 parent_id = req.FullBackupID），如果不过滤 type，
//     删除全量备份时会错误地认为"还有活跃子增量"（实际是审计记录）→ 拒绝删除
//   - 修复后：本函数严格只统计 incremental 类型，确保链式清理 / 删除检查的语义正确
func CountLiveChildrenByParent(parentID, now int64) (int64, error) {
	var count int64
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM backup_manifest
		WHERE parent_id = ? AND (keep_until = 0 OR keep_until >= ?)
		  AND status IN (?, ?)
		  AND type = ?
	`, parentID, now, BackupStatusSuccess, BackupStatusVerified, BackupTypeIncremental).Scan(&count)
	return count, err
}

// FindLatestChildKeepUntil 找某 parent 的所有子增量中**最晚的 keep_until**。
// 若该值 > 父全量的当前 keep_until，父全量应被延长至此。
func FindLatestChildKeepUntil(parentID int64) (int64, error) {
	var keepUntil sql.NullInt64
	err := DB.QueryRow(`
		SELECT MAX(keep_until) FROM backup_manifest
		WHERE parent_id = ? AND keep_until > 0
		  AND status IN (?, ?)
	`, parentID, BackupStatusSuccess, BackupStatusVerified).Scan(&keepUntil)
	if err != nil {
		return 0, err
	}
	if !keepUntil.Valid {
		return 0, nil
	}
	return keepUntil.Int64, nil
}

// UpdateKeepUntil 延长某备份的 keep_until（链式保护：因新子增量而推迟父全量清理）。
func UpdateKeepUntil(id int64, keepUntil int64) error {
	_, err := DB.Exec(`UPDATE backup_manifest SET keep_until = ? WHERE id = ?`,
		keepUntil, id)
	return err
}

// RecoverWronglyMarkedAudits 自动修复被错误标记为 missing 的审计记录。
//
// 2026-06-29 bug #14：根因是 DetectOrphanBackups 未排除 restore_audit，
// 导致审计记录的 FilePath="restore: ..." 经 os.Stat 失败后被错置为 missing。
//
// 修复策略：
//   - 将 status='missing' 且 type='restore_audit' 的记录恢复为 verified
//   - 保留 verified_result="restore_audit"，不影响其他字段
//   - 幂等可重入
//
// 返回：恢复的条数。
func RecoverWronglyMarkedAudits() (int64, error) {
	res, err := DB.Exec(`
		UPDATE backup_manifest
		SET status = ?, verified_result = COALESCE(NULLIF(verified_result, ''), ?)
		WHERE status = ? AND type = ?
	`,
		BackupStatusVerified, "restore_audit",
		BackupStatusMissing, BackupTypeRestoreAudit,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
