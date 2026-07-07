package services

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrPathTraversal 检测到路径穿越（relPath 含 ".." 段或绝对路径前缀）。
// P0 修复（2026-06-28）：GetAbsolutePath 之前仅做 filepath.Join 不校验，
//   若 DB 中 file_path 被篡改为 "../../../etc/passwd"，
//   filepath.Join(uploadDir, "../../etc/passwd") 会逃出 uploadDir。
//   本函数在三个 Storage 的 GetAbsolutePath 中统一调用，
//   失败返回 (空, ErrPathTraversal)，调用方应据此返回 4xx。
var ErrPathTraversal = errors.New("非法路径：检测到路径穿越")

// resolveSafeAbsPath 把相对路径解析为绝对路径，并强制要求解析结果在 uploadDir 之内。
//
//   - uploadDir: 允许的根目录（绝对路径）
//   - relPath:   入库时的相对路径（来自 Save* 方法或 DB）
//
// 返回：合法绝对路径 或 ErrPathTraversal。
//
// 规则：
//   - relPath 为空 → ErrPathTraversal（保守拒绝）
//   - relPath 为绝对路径（含 Unix /xxx 或 Windows C:\、\\server\share）→ ErrPathTraversal
//   - 解析后不在 uploadDir 子树内（filepath.Rel 返回 ".." 或 ../xxx）→ ErrPathTraversal
//
// 校验流程：
//   1. 拒绝空路径
//   2. 拒绝绝对路径（含盘符前缀，如 Windows 上的 "C:\"）
//   3. 拼接并 Clean 后用 filepath.Rel 验证是否在 uploadDir 子树内（权威检查）
//   4. Rel 检查通过则返回合法绝对路径
//
// 注：不再用 strings.Contains(relPath, "..") 做早期 bail-out。
//   原因：会误拒含 ".." 子串的合法路径（如 "..hidden/file.png"、"foo...bar"）。
//   filepath.Rel 是唯一的权威 containment 检查。
func resolveSafeAbsPath(uploadDir, relPath string) (string, error) {
	if uploadDir == "" {
		return "", errors.New("uploadDir 为空")
	}
	if relPath == "" {
		return "", ErrPathTraversal
	}
	// 1) 绝对路径或盘符前缀（如 C:\、\\server\share、/etc/）一律拒绝
	if filepath.IsAbs(relPath) || filepath.IsAbs(filepath.FromSlash(relPath)) {
		return "", ErrPathTraversal
	}

	cleanRoot := filepath.Clean(uploadDir)
	joined := filepath.Join(cleanRoot, filepath.FromSlash(relPath))
	cleanJoined := filepath.Clean(joined)

	// 2) 权威 containment 检查：Rel 返回 ".." 或 "../xxx" 即逃出 uploadDir
	rel, err := filepath.Rel(cleanRoot, cleanJoined)
	if err != nil {
		return "", ErrPathTraversal
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	return cleanJoined, nil
}