package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// WORM 最小可用（Phase 1 Critical #1）
//
// 用途：上传"签名图 / 第三方合同 PDF"成功后立即落 worm_record，
// DB 持有上锁瞬间的 SHA256。后续 UpdateContract 等修改路径在写之前
// 调 services.VerifyWorm(path) → hash 不匹配即拒绝。
//
// 本期不做：
//   - SM3 数据快照（worm_record 仅存 SHA256）
//   - 定期调验 / 后台扫描
//   - 管理员"临时解锁"流程
//   - chmod read-only（Windows server 上无意义）

// ErrAlreadyLocked 表示"该文件路径已在上锁表中存在"。
// 上传路径拿到这个错就当作"重复上传"，按 4xx 返回。
var ErrAlreadyLocked = errors.New("worm: file already locked")

// WormRecord 一条 WORM 记录。
type WormRecord struct {
	SnowID       string
	FilePath     string
	FileHashSHA  string
	LockedAt     int64
	LockedBy     int64
	LockedReason string
}

// LockOnce 把 file_path + hash 写入 worm_record。snowid 程序生成，幂等键。
//
//   - 首次：成功，nil error
//   - snowid 已存在：(WormRecord, nil) — 视为幂等成功，返回原记录
//   - file_path 不同 snowid 重复：返回 ErrAlreadyLocked
//   - 其它 DB 错：(nil, err)
func LockOnce(snowid, filePath, fileHashSHA string, lockedBy int64, reason string) (*WormRecord, error) {
	if snowid == "" || filePath == "" || fileHashSHA == "" {
		return nil, errors.New("worm: snowid/file_path/fileHashSHA 不能为空")
	}
	if DB == nil {
		return nil, errors.New("worm: database 未初始化")
	}

	// 1. snowid 已存在 → 返回原记录（幂等）
	rec, err := GetWormBySnowID(snowid)
	if err != nil {
		return nil, err
	}
	if rec != nil {
		return rec, nil
	}

	// 2. file_path 被别的 snowid 占用 → 拒绝
	other, err := GetWormByFilePath(filePath)
	if err != nil {
		return nil, err
	}
	if other != nil {
		return nil, ErrAlreadyLocked
	}

	now := time.Now().Unix()
	_, err = DB.Exec(
		`INSERT INTO worm_record (snowid, file_path, file_hash_sha256, locked_at, locked_by, locked_reason)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		snowid, filePath, fileHashSHA, now, lockedBy, reason,
	)
	if err != nil {
		// 并发场景：两个 LockOnce 同时通过 GetWormByFilePath 检查 → UNIQUE 触发
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrAlreadyLocked
		}
		return nil, err
	}

	return &WormRecord{
		SnowID:       snowid,
		FilePath:     filePath,
		FileHashSHA:  fileHashSHA,
		LockedAt:     now,
		LockedBy:     lockedBy,
		LockedReason: reason,
	}, nil
}

// GetWormBySnowID 按 snowid 查（PK 索引）。
func GetWormBySnowID(snowid string) (*WormRecord, error) {
	if DB == nil {
		return nil, errors.New("worm: database 未初始化")
	}
	row := DB.QueryRow(
		`SELECT snowid, file_path, file_hash_sha256, locked_at, locked_by, locked_reason
		 FROM worm_record WHERE snowid = ?`, snowid,
	)
	return scanWorm(row)
}

// GetWormByFilePath 按 file_path 查（UNIQUE 索引）。
func GetWormByFilePath(filePath string) (*WormRecord, error) {
	if DB == nil {
		return nil, errors.New("worm: database 未初始化")
	}
	row := DB.QueryRow(
		`SELECT snowid, file_path, file_hash_sha256, locked_at, locked_by, locked_reason
		 FROM worm_record WHERE file_path = ?`, filePath,
	)
	return scanWorm(row)
}

// ListWormLocked 列出所有 WORM 记录（按 locked_at DESC）。
// limit=0 表示无限制。
func ListWormLocked(limit int) ([]*WormRecord, error) {
	if DB == nil {
		return nil, errors.New("worm: database 未初始化")
	}
	q := `SELECT snowid, file_path, file_hash_sha256, locked_at, locked_by, locked_reason
	      FROM worm_record ORDER BY locked_at DESC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*WormRecord, 0)
	for rows.Next() {
		rec, err := scanWorm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// scanWorm 从单行扫描成 WormRecord。
// 接受 *sql.Row 或 *sql.Rows（两者都满足 Scan 方法签名）。
func scanWorm(s wormScanner) (*WormRecord, error) {
	var rec WormRecord
	err := s.Scan(
		&rec.SnowID, &rec.FilePath, &rec.FileHashSHA,
		&rec.LockedAt, &rec.LockedBy, &rec.LockedReason,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// wormScanner worm 包内私有的 scanner 接口（不与 database.rowScanner 同名）。
type wormScanner interface {
	Scan(dest ...any) error
}
