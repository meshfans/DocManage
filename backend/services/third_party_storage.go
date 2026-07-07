package services

import (
	"doc/utils"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ThirdPartyStorage 负责第三方合同 PDF 的文件存储。
// 文件路径：{Upload.Dir}/third_party/{YYYYMM}/{snowid}.pdf
type ThirdPartyStorage struct {
	uploadDir string
}

var thirdPartyStorage *ThirdPartyStorage

// InitThirdPartyStorage 初始化第三方合同存储。在 main 启动时调用一次。
func InitThirdPartyStorage(uploadDir string) {
	thirdPartyStorage = &ThirdPartyStorage{uploadDir: uploadDir}
	utils.Info("[第三方合同存储] 初始化完成，根目录: %s", uploadDir)
}

// GetThirdPartyStorage 获取全局单例。
func GetThirdPartyStorage() *ThirdPartyStorage {
	if thirdPartyStorage == nil {
		panic("ThirdPartyStorage 未初始化，请先调用 InitThirdPartyStorage")
	}
	return thirdPartyStorage
}

// SaveThirdPartyAsset 保存 PDF 字节到 third_party/{YYYYMM}/{snowid}.pdf。
//   - data: PDF 文件字节
//   - customSnowID: 可选，外部传入的 snowid（用于"用占位 snowid"场景）；
//     传空字符串时函数内部用 utils.NextSnowIDString() 生成。
//
// 返回：snowid（与 customSnowID 一致或自生成）、相对路径（用于入库 file_path）、错误。
func (s *ThirdPartyStorage) SaveThirdPartyAsset(data []byte, customSnowID ...string) (snowid, relPath string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("PDF 数据为空")
	}
	// 1. 决定 snowid
	if len(customSnowID) > 0 && customSnowID[0] != "" {
		snowid = customSnowID[0]
	} else {
		snowid = utils.NextSnowIDString()
	}
	yearMonth := time.Now().Format("200601")
	relDir := filepath.Join("third_party", yearMonth)
	absDir := filepath.Join(s.uploadDir, relDir)
	if err = os.MkdirAll(absDir, 0755); err != nil {
		return "", "", fmt.Errorf("创建目录失败: %w", err)
	}
	filename := fmt.Sprintf("%s.pdf", snowid)
	absPath := filepath.Join(absDir, filename)
	if err = os.WriteFile(absPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("写入文件失败: %w", err)
	}
	relPath = filepath.ToSlash(filepath.Join(relDir, filename))
	utils.Info("[第三方合同存储] 保存成功: %s (size=%d bytes)", relPath, len(data))
	return snowid, relPath, nil
}

// GetAbsolutePath 把相对路径（入库的 file_path）解析为绝对路径。
//
// P0 修复（2026-06-28）：增加路径 containment 校验。
//   历史版本仅做 filepath.Join(uploadDir, relPath)，若 DB file_path 被篡改为
//   "../../../etc/passwd" 等，可逃出 uploadDir 读任意文件。
//   现在统一走 resolveSafeAbsPath，relPath 含 ".." 或绝对路径前缀时返回 ErrPathTraversal。
//   调用方应将 ErrPathTraversal 视为 403/404，禁止访问。
func (s *ThirdPartyStorage) GetAbsolutePath(relPath string) (string, error) {
	return resolveSafeAbsPath(s.uploadDir, relPath)
}

// DeleteFile 删除文件（用于"重传前清理"或"删除合同时清理附件"）。
// 不存在不报错。
func (s *ThirdPartyStorage) DeleteFile(relPath string) error {
	if relPath == "" {
		return nil
	}
	absPath := filepath.Join(s.uploadDir, relPath)
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
