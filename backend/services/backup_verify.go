package services

import (
	"doc/database"
	"doc/utils"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 备份验证（第四阶段 Phase 4.2）====================
//
// 设计要点：
//   - 重算 zip 文件的 SM3 + SHA-256 + CombinedHash 三哈希
//   - 与 manifest 中记录的哈希比对，一致 → verified；不一致 → corrupted
//   - 文件不存在 → missing（与 DetectOrphanBackups 配合，覆盖更多场景）
//   - 验证结果写入 backup_manifest 表（status + verified_at + verified_result）
//   - 失败 → 日志 ERROR + 邮件告警（可选，本期不实现）
//
// 触发场景：
//   1. performBackup 成功后自动验证（实时）
//   2. 每日 03:00 调度扫描（覆盖历史备份，发现硬盘故障等）
// ----------------------------------------------------------------------------

// VerifyResult 验证结果。
type VerifyResult struct {
	ManifestID int64  `json:"manifest_id"`
	SnowID     string `json:"snowid"`
	Type       string `json:"type"`
	FilePath   string `json:"file_path"`

	// Result 三态：
	//   "ok"        ：三哈希完全一致，status → verified
	//   "corrupted" ：哈希不一致，status → corrupted
	//   "missing"   ：文件不存在，status → missing
	//   "skipped"   ：跳过（如 status=failed / pending 不验证）
	Result string `json:"result"`

	// 哈希比对详情（corrupted 时填充）
	ExpectedSM3        string `json:"expected_sm3,omitempty"`
	CalculatedSM3      string `json:"calculated_sm3,omitempty"`
	ExpectedSHA256     string `json:"expected_sha256,omitempty"`
	CalculatedSHA256   string `json:"calculated_sha256,omitempty"`
	ExpectedCombined   string `json:"expected_combined,omitempty"`
	CalculatedCombined string `json:"calculated_combined,omitempty"`

	// 时间
	VerifiedAt int64 `json:"verified_at"`
	DurationMS int64 `json:"duration_ms"`

	// Error 原始 error（保留 wrapped 链，便于 errors.Is / errors.As 根因定位）。
	// 第四阶段 P0+Phase 4.2：与 Error 字符串互为冗余，调用方任选其一。
	Error string `json:"error,omitempty"`

	// Err 原始 error 对象（不导出到 JSON，避免序列化歧义）。
	Err error `json:"-"`
}

// BackupVerifier 备份验证服务。
type BackupVerifier struct {
	// 阈值：单文件 > 此大小则跳过单文件验证（超大备份视为"信任"，避免误判）
	// 默认 5GB。可由调用方覆盖。
	MaxFileSize int64
}

var backupVerifier *BackupVerifier

// GetBackupVerifier 返回单例。
func GetBackupVerifier() *BackupVerifier {
	if backupVerifier == nil {
		backupVerifier = &BackupVerifier{
			MaxFileSize: 5 << 30, // 5 GB
		}
	}
	return backupVerifier
}

// VerifyBackup 验证单个备份（按 manifest_id）。
//
// 流程：
//  1. 查 manifest（不存在则报错）
//  2. 检查 status（pending / running 跳过）
//  3. 文件不存在 → missing
//  4. 重算三哈希，比对 → ok / corrupted
//
// 返回 VerifyResult，err 仅在 manifest 不存在等"无法验证"的场景返回。
func (v *BackupVerifier) VerifyBackup(manifestID int64) (*VerifyResult, error) {
	m, err := database.GetBackupManifestByID(manifestID)
	if err != nil {
		return nil, fmt.Errorf("查询 manifest 失败: %w", err)
	}
	if m == nil {
		return nil, fmt.Errorf("manifest 不存在: id=%d", manifestID)
	}

	return v.verifyManifest(m), nil
}

// verifyManifest 内部验证（不查 DB，直接用 manifest 对象）。
// 状态判断 + 文件检查 + 哈希比对。
func (v *BackupVerifier) verifyManifest(m *database.BackupManifest) *VerifyResult {
	startTime := time.Now()
	result := &VerifyResult{
		ManifestID: m.ID,
		SnowID:     m.SnowID,
		Type:       m.Type,
		FilePath:   m.FilePath,
		VerifiedAt: startTime.Unix(),
	}

	// 恢复审计记录（restore_audit）没有真实文件路径（FilePath 是人读的恢复摘要），
	// 不应走"文件存在 + 哈希校验"流程，否则 os.Stat 失败会被错误地标记为 missing。
	if m.Type == database.BackupTypeRestoreAudit {
		result.Result = "ok"
		result.VerifiedAt = startTime.Unix()
		result.DurationMS = time.Since(startTime).Milliseconds()
		utils.Info("[Verify] 跳过审计记录验证: id=%d, snowid=%s (type=restore_audit)",
			m.ID, m.SnowID)
		return result
	}

	// 跳过验证的状态
	if m.Status == database.BackupStatusPending || m.Status == database.BackupStatusRunning {
		result.Result = "skipped"
		result.Error = fmt.Sprintf("manifest status=%s 跳过", m.Status)
		result.DurationMS = time.Since(startTime).Milliseconds()
		return result
	}

	// 文件路径为空（理论上不应发生，但 defensive）
	if m.FilePath == "" {
		result.Result = "missing"
		result.Error = "manifest file_path 为空"
		result.DurationMS = time.Since(startTime).Milliseconds()
		// Issue A6：DB 写错误不再静默（即便此处 verifyResult 正确，DB 持久化失败也是问题）
		if err := database.UpdateBackupManifestVerifiedWithStatus(
			m.ID, database.BackupStatusMissing, "missing"); err != nil {
			utils.LogError("[Verify] DB 写失败（empty path → missing）: id=%d, err=%v",
				m.ID, err)
		}
		return result
	}

	// 文件不存在 → missing
	info, statErr := os.Stat(m.FilePath)
	if statErr != nil || info == nil {
		result.Result = "missing"
		result.Err = statErr
		result.Error = fmt.Sprintf("文件不存在: %v", statErr)
		result.DurationMS = time.Since(startTime).Milliseconds()
		utils.LogError("[Verify] 备份文件丢失: id=%d, snowid=%s, path=%s",
			m.ID, m.SnowID, m.FilePath)
		if err := database.UpdateBackupManifestVerifiedWithStatus(
			m.ID, database.BackupStatusMissing, "missing"); err != nil {
			utils.LogError("[Verify] DB 写失败（missing）: id=%d, err=%v", m.ID, err)
		}
		return result
	}

	// 超大文件跳过（避免误判 + 性能）
	if info.Size() > v.MaxFileSize {
		result.Result = "skipped"
		result.Error = fmt.Sprintf("文件过大 (%d > %d)，跳过单文件验证",
			info.Size(), v.MaxFileSize)
		result.DurationMS = time.Since(startTime).Milliseconds()
		utils.Warn("[Verify] 跳过超大备份验证: id=%d, size=%d", m.ID, info.Size())
		// Issue B2：标记 DB 避免每日调度重复扫描超大文件
		// 不强制 success/verified（没验证过），用 "skipped" 作为 verified_result
		if err := database.UpdateBackupManifestVerifiedWithStatus(
			m.ID, m.Status, "skipped"); err != nil {
			utils.LogError("[Verify] 标记 skipped 状态失败: id=%d, err=%v", m.ID, err)
		}
		return result
	}

	// 重算三哈希
	_, calcSM3, calcSHA256, calcCombined, hashErr := hashFileTriple(m.FilePath)
	if hashErr != nil {
		// Issue 4：TOCTOU 修正——若 hashFileTriple 失败原因是文件不存在（Stat 后被并发删）
		// 应归类为 missing 而非 corrupted
		if os.IsNotExist(hashErr) || errors.Is(hashErr, os.ErrNotExist) {
			result.Result = "missing"
			result.Err = hashErr
			result.Error = fmt.Sprintf("文件在验证过程中丢失: %v", hashErr)
			result.DurationMS = time.Since(startTime).Milliseconds()
			utils.LogError("[Verify] 备份文件丢失（TOCTOU）: id=%d, snowid=%s, path=%s",
				m.ID, m.SnowID, m.FilePath)
			if err := database.UpdateBackupManifestVerifiedWithStatus(
				m.ID, database.BackupStatusMissing, "missing"); err != nil {
				utils.LogError("[Verify] DB 写失败（TOCTOU → missing）: id=%d, err=%v",
					m.ID, err)
			}
			return result
		}

		result.Result = "corrupted"
		result.Err = hashErr
		result.Error = fmt.Sprintf("计算哈希失败: %v", hashErr)
		result.DurationMS = time.Since(startTime).Milliseconds()
		utils.LogError("[Verify] 哈希计算失败: id=%d, path=%s, err=%v",
			m.ID, m.FilePath, hashErr)
		if err := database.UpdateBackupManifestVerifiedWithStatus(
			m.ID, database.BackupStatusCorrupted, "corrupted"); err != nil {
			utils.LogError("[Verify] DB 写失败（corrupted）: id=%d, err=%v", m.ID, err)
		}
		return result
	}

	// 比对三哈希
	result.ExpectedSM3 = m.FileHashSM3
	result.CalculatedSM3 = calcSM3
	result.ExpectedSHA256 = m.FileHashSHA256
	result.CalculatedSHA256 = calcSHA256
	result.ExpectedCombined = m.FileHashCombined
	result.CalculatedCombined = calcCombined

	if m.FileHashSM3 == calcSM3 &&
		m.FileHashSHA256 == calcSHA256 &&
		m.FileHashCombined == calcCombined {
		result.Result = "ok"
		result.DurationMS = time.Since(startTime).Milliseconds()
		if err := database.UpdateBackupManifestVerifiedWithStatus(
			m.ID, database.BackupStatusVerified, "ok"); err != nil {
			utils.LogError("[Verify] DB 写失败（verified）: id=%d, err=%v", m.ID, err)
		}
		utils.Info("[Verify] 验证通过: id=%d, snowid=%s, duration=%dms",
			m.ID, m.SnowID, result.DurationMS)
		return result
	}

	// 哈希不一致 → corrupted
	result.Result = "corrupted"
	result.DurationMS = time.Since(startTime).Milliseconds()
	utils.LogError("[Verify] ⚠️ 备份损坏: id=%d, snowid=%s, "+
		"SM3 期望=%s 实算=%s, SHA256 期望=%s 实算=%s",
		m.ID, m.SnowID, m.FileHashSM3, calcSM3, m.FileHashSHA256, calcSHA256)
	if err := database.UpdateBackupManifestVerifiedWithStatus(
		m.ID, database.BackupStatusCorrupted, "corrupted"); err != nil {
		utils.LogError("[Verify] DB 写失败（corrupted → corrupted）: id=%d, err=%v",
			m.ID, err)
	}
	return result
}

// VerifyAll 验证所有 status=success/verified 的备份。
// 返回所有验证结果 + 错误聚合。
//
// 用途：每日调度全量扫描，发现硬盘故障 / 文件损坏。
func (v *BackupVerifier) VerifyAll() ([]*VerifyResult, error) {
	// 拉所有 success 备份
	successList, err := database.ListBackupManifests(
		"", database.BackupStatusSuccess, 99999, 0)
	if err != nil {
		return nil, fmt.Errorf("查询 success 备份失败: %w", err)
	}
	verifiedList, err := database.ListBackupManifests(
		"", database.BackupStatusVerified, 99999, 0)
	if err != nil {
		return nil, fmt.Errorf("查询 verified 备份失败: %w", err)
	}
	all := append(successList, verifiedList...)

	if len(all) == 0 {
		return nil, nil
	}

	results := make([]*VerifyResult, 0, len(all))
	var firstErr error
	okCount, corruptedCount, missingCount := 0, 0, 0

	for _, m := range all {
		r := v.verifyManifest(m)
		results = append(results, r)

		switch r.Result {
		case "ok":
			okCount++
			// Round 16 业务事件埋点：备份验证通过。
			PublishEvent("backup.verify.ok")
			// Issue M-7：verify.ok 不再写 audit——全量扫描每日 03:00 跑，
			// 1000 条历史备份每天都会产生 1000 行 audit_log，导致 SM3 链校验
			// 与 ListAudit 性能雪崩。corrupted/missing 仍然写（取证需要）。
		case "corrupted":
			corruptedCount++
			if firstErr == nil {
				firstErr = errors.New(r.Error)
			}
			// Round 16 业务事件埋点：备份验证发现损坏。
			PublishEvent("backup.verify.corrupted")
			// Issue #14 + M-9：审计留痕（损坏事件对取证至关重要），actorIP
			// 填 "verify_daily" 让 hash 链不会因 actorIP='' 被攻击者伪造。
			database.RecordAuditStandalone(0, database.AuditTargetBackup, m.ID, "verify.corrupted",
				"", "verify_daily", gin.H{"snowid": m.SnowID, "err": r.Error})
		case "missing":
			missingCount++
			if firstErr == nil {
				firstErr = errors.New(r.Error)
			}
			// Round 16 业务事件埋点：备份验证发现文件丢失。
			PublishEvent("backup.verify.missing")
			// Issue #14 + M-9：审计留痕。
			database.RecordAuditStandalone(0, database.AuditTargetBackup, m.ID, "verify.missing",
				"", "verify_daily", gin.H{"snowid": m.SnowID, "err": r.Error})
		}
	}

	utils.Info("[Verify] 全量验证完成: 总 %d, 通过 %d, 损坏 %d, 丢失 %d",
		len(all), okCount, corruptedCount, missingCount)

	if firstErr != nil {
		return results, firstErr
	}
	return results, nil
}

// VerifyRecent 已删除（第四阶段 Phase 4.2 二次审查 Issue A1：dead code）。
// 原因：VerifyBackup（单条）和 VerifyAll（全量）已覆盖所有使用场景，
// 本函数自 v1 实现后无任何调用方，保留只会增加维护负担。
