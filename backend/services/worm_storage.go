package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"doc/database"
)

// WORM 最小可用（Phase 1 Critical #1）。
//
// 设计要点：
//   - "append-only + 路径锁 + DB 哈希记录"
//   - 文件路径不允许改名前缀（程序生成 snowid 前缀）
//   - DB+OS 双锁：DB 持有上锁瞬间 SHA256；OS 层不 chmod（Windows 无意义）
//   - Verify 决策：
//       locked=false          → 未上锁文件，Verify 不阻拦调用方
//       locked=true, hash==   → 文件未被篡改
//       locked=true, hash!=   → 文件被篡改/修改/删除，返 ErrWormTampered
//   - 本期不做：SM3 数据快照 / 定期调验 / 管理员"临时解锁"

// ErrWormTampered 表示"已上锁文件被修改/删除"，调用方应作为业务级 410 (Gone) 处理。
var ErrWormTampered = errors.New("worm: file has been tampered")

// LockOnce 把文件 + 哈希登记到 worm_record。
//   - absPath: 绝对路径（ThirdPartyStorage 返回的）
//   - snowid: 程序生成的 snowid（与已入库记录保持一致）
//   - userID: 当前用户 ID
//   - reason: 调用场景标识（"thirdparty.contract.upload" / "customer.signature.upload"）
//
// 返回 (record, nil) 表示登记成功；ErrAlreadyLocked 表示重复登记。
func LockOnce(absPath, snowid string, userID int64, reason string) (*database.WormRecord, error) {
	if absPath == "" || snowid == "" {
		return nil, fmt.Errorf("worm: absPath/snowid 不能为空")
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, fmt.Errorf("worm: 锁定的文件不存在: %w", err)
	}

	hash, err := fileSHA256(absPath)
	if err != nil {
		return nil, fmt.Errorf("worm: 计算 SHA256 失败: %w", err)
	}

	rec, err := database.LockOnce(snowid, absPath, hash, userID, reason)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// VerifyWorm 验证文件是否被篡改。
//
//   - 不在 worm_record → (locked=false, "", nil)  调用方应放行
//   - 在 worm_record + 文件不存在 → (locked=true, storedHash, ErrWormTampered)
//   - 在 worm_record + 文件存在 + hash 相同 → (locked=true, storedHash, nil)
//   - 在 worm_record + 文件存在 + hash 不同 → (locked=true, storedHash, ErrWormTampered)
//
// 调用方决策：
//   - locked=false → 不阻断
//   - locked=true && err==nil → 不阻断
//   - locked=true && err==ErrWormTampered → 拒绝（返 CodeContractLocked）
func VerifyWorm(absPath string) (locked bool, storedHash string, err error) {
	if absPath == "" {
		return false, "", nil
	}
	rec, err := database.GetWormByFilePath(absPath)
	if err != nil {
		return false, "", err
	}
	if rec == nil {
		return false, "", nil
	}

	// 文件被删除：仍报告 locked=true + tampered
	if _, statErr := os.Stat(absPath); statErr != nil {
		return true, rec.FileHashSHA, ErrWormTampered
	}

	currentHash, err := fileSHA256(absPath)
	if err != nil {
		return true, rec.FileHashSHA, fmt.Errorf("worm: 计算 hash 失败: %w", err)
	}
	if currentHash != rec.FileHashSHA {
		return true, rec.FileHashSHA, ErrWormTampered
	}
	return true, rec.FileHashSHA, nil
}

// fileSHA256 计算文件 SHA256（流式，恒定内存）。
func fileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 32*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
