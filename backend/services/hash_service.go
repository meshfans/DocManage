package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/emmansun/gmsm/sm3"
)

type DualHash struct {
	SM3Hash      string `json:"sm3"`
	SHA256Hash   string `json:"sha256"`
	CombinedHash string `json:"combined"`
	DataLength   int64  `json:"data_length"`
	Algorithm    string `json:"algorithm"`
	CalculatedAt int64  `json:"calculated_at"`
}

type HashService struct {
	EnableSM3    bool
	EnableSHA256 bool
}

func NewHashService(enableSM3, enableSHA256 bool) *HashService {
	return &HashService{
		EnableSM3:    enableSM3,
		EnableSHA256: enableSHA256,
	}
}

func (h *HashService) CalculateDualHash(data []byte) (*DualHash, error) {
	result := &DualHash{
		DataLength:   int64(len(data)),
		CalculatedAt: nowUnix(),
	}

	if h.EnableSM3 {
		hash := sm3.Sum(data)
		result.SM3Hash = hex.EncodeToString(hash[:])
		result.Algorithm = "SM3"
	}

	if h.EnableSHA256 {
		hash := sha256.Sum256(data)
		result.SHA256Hash = hex.EncodeToString(hash[:])
		if result.Algorithm == "SM3" {
			result.Algorithm = "SM3+SHA256"
		} else {
			result.Algorithm = "SHA256"
		}
	}

	if h.EnableSM3 && h.EnableSHA256 {
		combined := result.SM3Hash + result.SHA256Hash
		combinedHash := sha256.Sum256([]byte(combined))
		result.CombinedHash = hex.EncodeToString(combinedHash[:])
	}

	return result, nil
}

func (h *HashService) CalculateFileDualHash(filePath string) (*DualHash, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	return h.CalculateDualHash(data)
}

// CalculateFileDualHashStreamed 流式计算文件三哈希（不把整个文件加载到内存）。
//
// 第四阶段 Phase 4.2：备份验证场景下 zip 文件可能达数 GB，os.ReadFile 会导致
// 堆内存峰值 = 文件大小。改用 io.Copy 流式写入 hash.Hash，恒定内存 = 32KB 缓冲区。
//
// 与 CalculateFileDualHash 结果等价（哈希算法一致），仅 IO 路径不同。
func (h *HashService) CalculateFileDualHashStreamed(filePath string) (*DualHash, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	sm3Hash := sm3.New()
	sha256Hash := sha256.New()

	// MultiWriter 让一次 IO 同时更新两个 hash，避免读两次文件
	mw := io.MultiWriter(sm3Hash, sha256Hash)
	buf := make([]byte, 32*1024) // 32KB 缓冲区
	written, err := io.CopyBuffer(mw, f, buf)
	if err != nil {
		return nil, fmt.Errorf("流式读取失败: %w", err)
	}

	result := &DualHash{
		DataLength:   written,
		CalculatedAt: nowUnix(),
	}

	if h.EnableSM3 {
		result.SM3Hash = hex.EncodeToString(sm3Hash.Sum(nil))
		result.Algorithm = "SM3"
	}
	if h.EnableSHA256 {
		result.SHA256Hash = hex.EncodeToString(sha256Hash.Sum(nil))
		if result.Algorithm == "SM3" {
			result.Algorithm = "SM3+SHA256"
		} else {
			result.Algorithm = "SHA256"
		}
	}
	if h.EnableSM3 && h.EnableSHA256 {
		combined := result.SM3Hash + result.SHA256Hash
		combinedHash := sha256.Sum256([]byte(combined))
		result.CombinedHash = hex.EncodeToString(combinedHash[:])
	}

	return result, nil
}

// verifyDualHash 用与 Calculate* 同样的字段对比规则比对。
//   - SM3Hash:   非空就比对
//   - SHA256Hash: 非空就比对
//   - CombinedHash: 非空就比对（如果都开启，CombinedHash 与 SM3Hash/SHA256Hash 不冗余
//                    ——它是 SM3+SHA256 拼接后再 SHA256，能检测出"两个分量被同时替换"的篡改）
//
// 返回 (matched bool, reason string)：reason 描述失败原因（"SM3 哈希不匹配"等），
// 成功时为空。调用方可把 reason 直接返回给客户端。
func (h *HashService) verifyDualHash(calculated, expected *DualHash) (bool, string) {
	if h.EnableSM3 && expected.SM3Hash != "" && calculated.SM3Hash != expected.SM3Hash {
		return false, "SM3 哈希不匹配"
	}
	if h.EnableSHA256 && expected.SHA256Hash != "" && calculated.SHA256Hash != expected.SHA256Hash {
		return false, "SHA256 哈希不匹配"
	}
	// P1 修复（2026-06-14）：CombinedHash 校验。
	//   - 只有在 expected 提供了 CombinedHash 且服务开启了双算法时才比对
	//   - 这样：升级老数据（没存 CombinedHash）不会失败
	//          只存 SM3 或只存 SHA256 的也不会失败
	if h.EnableSM3 && h.EnableSHA256 && expected.CombinedHash != "" {
		if calculated.CombinedHash == "" {
			return false, "CombinedHash 未计算（哈希服务未启用双算法）"
		}
		if calculated.CombinedHash != expected.CombinedHash {
			return false, "CombinedHash 不匹配"
		}
	}
	return true, ""
}

// VerifyHash P1 修复（2026-06-14）：增加 CombinedHash 校验。
// 调用方通过 (bool, string) 拿到匹配结果和失败原因。
// 返回值 signature 调整：返回 (bool, string) 而不是 (bool, error)，因为校验失败
// 不是"错误"而是"业务结论"，error 仅留给真正的 IO/算法异常。
func (h *HashService) VerifyHash(data []byte, expected *DualHash) (bool, string, error) {
	calculated, err := h.CalculateDualHash(data)
	if err != nil {
		return false, "", err
	}
	ok, reason := h.verifyDualHash(calculated, expected)
	return ok, reason, nil
}

// VerifyFileHash P1 修复（2026-06-14）：增加 CombinedHash 校验。
func (h *HashService) VerifyFileHash(filePath string, expected *DualHash) (bool, string, error) {
	calculated, err := h.CalculateFileDualHash(filePath)
	if err != nil {
		return false, "", err
	}
	ok, reason := h.verifyDualHash(calculated, expected)
	return ok, reason, nil
}

func (h *HashService) GetSupportedAlgorithms() []string {
	algos := make([]string, 0)
	if h.EnableSM3 {
		algos = append(algos, "SM3")
	}
	if h.EnableSHA256 {
		algos = append(algos, "SHA256")
	}
	if h.EnableSM3 && h.EnableSHA256 {
		algos = append(algos, "DUAL")
	}
	return algos
}

func nowUnix() int64 {
	return time.Now().Unix()
}
