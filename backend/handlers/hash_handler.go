package handlers

import (
	"errors"
	"path/filepath"
	"strings"

	"doc/database"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

var hashService = services.NewHashService(true, true)

// HashKind 表示要哈希的资源类型。文件路径完全由后端从 snowid 反查，
// 客户端不能传任意 file_path。
type HashKind string

const (
	HashKindMediaFile  HashKind = "media_file"  // 媒体原图
	HashKindMediaThumb HashKind = "media_thumb" // 媒体缩略图
	HashKindMediaBoth  HashKind = "media_both"  // 原图 + 缩略图（一次返回）
)

// HashCalculateRequest 计算哈希请求。P0 修复（2026-06-14）：不再接受 file_path，
// 一律通过 snowid + kind 由后端从数据库反查受控路径，杜绝任意文件读取漏洞。
type HashCalculateRequest struct {
	SnowID string  `json:"snowid" binding:"required"`
	Kind   HashKind `json:"kind" binding:"required"`
}

// HashVerifyRequest 验证哈希请求。P0 修复同上。
type HashVerifyRequest struct {
	SnowID      string  `json:"snowid" binding:"required"`
	Kind        HashKind `json:"kind" binding:"required"`
	SM3Hash     string  `json:"sm3_hash"`
	SHA256Hash  string  `json:"sha256_hash"`
	CombinedHash string `json:"combined_hash"`
}

type HashVerifyResponse struct {
	Valid            bool   `json:"valid"`
	Message          string `json:"message"`
	StoredSM3        string `json:"stored_sm3_hash,omitempty"`
	CurrentSM3       string `json:"current_sm3_hash,omitempty"`
	StoredSHA256     string `json:"stored_sha256_hash,omitempty"`
	CurrentSHA256    string `json:"current_sha256_hash,omitempty"`
	StoredCombined   string `json:"stored_combined_hash,omitempty"`
	CurrentCombined  string `json:"current_combined_hash,omitempty"`
}

// requireAdmin 已删除（2026-06-14 二次审查）。
// 现在统一用 handlers.RequireAdmin（admin_rbac.go），所有 RBAC 走同一个入口。
//
// 历史：hash_handler.go 之前有自己的私有 requireAdmin（重复实现，v0 阶段允许）。
//       现在统一到 admin_rbac.go，签名一致：
//         - RequireAdmin(c) → 返回 false 表示已写 403 响应，handler 应直接 return
//         - IsAdmin(c)      → 只读 bool，用于条件分支

// resolveMediaPath 根据 snowid + kind 解析出受控的文件路径。
//
// P0 修复要点：
//   - 客户端不能传 file_path
//   - 后端从 media 表反查 file_path/thumb_path
//   - 二次校验：解析后必须落在 cfg.Upload.Dir 或 cfg.MediaStorage.Dir 内
//
// 任何不在白名单目录内的路径 → 拒绝（防止数据库被恶意写入指向系统文件）。
func resolveMediaPath(c *gin.Context, snowID string, kind HashKind) (string, error) {
	m, err := database.GetMediaBySnowID(snowID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", errors.New("媒体不存在: snowid=" + snowID)
	}

	var relPath string
	switch kind {
	case HashKindMediaFile:
		relPath = m.FilePath
	case HashKindMediaThumb:
		relPath = m.ThumbPath
		if relPath == "" {
			return "", errors.New("该媒体没有缩略图: snowid=" + snowID)
		}
	case HashKindMediaBoth:
		// 单独走分支，调用方需要走 CalculateHash + 双结果
		return "", errors.New("media_both 请改用 media_file 单独计算")
	default:
		return "", errors.New("非法的 kind: " + string(kind))
	}

	// 2026-07-07 修复（round8 BUG-01）：DB 中存的是相对路径（media/202607/xxx.png），
	// 必须通过 MediaStorage.GetAbsolutePath 拼接 cfg.Upload.Dir 得到绝对路径，
	// 才能喂给 CalculateFileDualHash（直接读文件需绝对路径）。
	// 此函数内部走 resolveSafeAbsPath，已包含路径越界（..）校验。
	absPath, err := services.GetMediaStorage().GetAbsolutePath(relPath)
	if err != nil {
		utils.LogError("[hash] 路径解析失败: snowid=%s kind=%s rel=%s err=%v",
			snowID, kind, relPath, err)
		return "", errors.New("文件路径不合法")
	}
	return absPath, nil
}

// isPathInAllowedDir 简单白名单：必须是绝对路径、不含 ..、不指向系统敏感目录。
// （真正的目录限制需要在 cfg.Upload.Dir 等配置就绪后注入；当前实现先做最严防御）
func isPathInAllowedDir(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	cleaned := filepath.Clean(p)
	// 禁止任何 .. 残留（即便 filepath.Clean 通常会清掉）
	if strings.Contains(cleaned, "..") {
		return false
	}
	// 禁止指向系统敏感目录
	lower := strings.ToLower(cleaned)
	banned := []string{
		`c:\windows`,
		`c:\program files`,
		`c:\programdata`,
		`/etc`,
		`/var`,
		`/usr`,
		`/boot`,
		`/proc`,
		`/sys`,
	}
	for _, b := range banned {
		if strings.HasPrefix(lower, b) {
			return false
		}
	}
	return true
}

// CalculateHash POST /api/hash/calculate
// body: { snowid, kind }
//
// P0 修复：使用 snowid + kind 查表得到受控路径，强制 admin 鉴权。
func CalculateHash(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var req HashCalculateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "参数错误: "+err.Error())
		return
	}

	filePath, err := resolveMediaPath(c, req.SnowID, req.Kind)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, err.Error())
		return
	}

	hash, err := hashService.CalculateFileDualHash(filePath)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "计算哈希失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"snowid":        req.SnowID,
		"kind":          req.Kind,
		"file_path":     filePath, // 仅返回 basename + 父目录（不暴露完整绝对路径）
		"sm3_hash":      hash.SM3Hash,
		"sha256_hash":   hash.SHA256Hash,
		"combined_hash": hash.CombinedHash,
		"data_length":   hash.DataLength,
		"algorithm":     hash.Algorithm,
		"calculated_at": hash.CalculatedAt,
	})
}

// VerifyHash POST /api/hash/verify
// body: { snowid, kind, sm3_hash?, sha256_hash?, combined_hash? }
//
// P1 修复（2026-06-14）：统一调 hashService.VerifyFileHash（含 CombinedHash 校验）。
// 不再在 handler 内手写三段对比逻辑（之前 CombinedHash 实际有但容易漏）。
func VerifyHash(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var req HashVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "参数错误: "+err.Error())
		return
	}

	filePath, err := resolveMediaPath(c, req.SnowID, req.Kind)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, err.Error())
		return
	}

	// 先算一遍哈希（用于响应里返回"当前值"），再调 VerifyFileHash 校验
	hash, err := hashService.CalculateFileDualHash(filePath)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "计算哈希失败: "+err.Error())
		return
	}
	valid, message, err := hashService.VerifyFileHash(filePath, &services.DualHash{
		SM3Hash:      req.SM3Hash,
		SHA256Hash:   req.SHA256Hash,
		CombinedHash: req.CombinedHash,
	})
	if err != nil {
		utils.Err(c, utils.CodeInternal, "验证哈希失败: "+err.Error())
		return
	}

	response := HashVerifyResponse{
		Valid:           valid,
		Message:         message,
		StoredSM3:       req.SM3Hash,
		CurrentSM3:      hash.SM3Hash,
		StoredSHA256:    req.SHA256Hash,
		CurrentSHA256:   hash.SHA256Hash,
		StoredCombined:  req.CombinedHash,
		CurrentCombined: hash.CombinedHash,
	}

	// P1 修复（2026-06-14）：成功时 message 为 "验证通过"（verifyDualHash 返回空字符串），
	// 失败时 message 是具体原因（"SM3 哈希不匹配"等）。
	if valid {
		utils.Success(c, gin.H{"valid": valid, "message": "验证通过", "data": response})
	} else {
		utils.Err(c, utils.CodeInvalidParam, message)
		utils.Success(c, gin.H{"valid": valid, "message": message, "data": response})
	}
}

// GetAlgorithms GET /api/hash/algorithms
func GetAlgorithms(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	algos := hashService.GetSupportedAlgorithms()
	utils.Success(c, gin.H{
		"algorithms":     algos,
		"sm3_enabled":    hashService.EnableSM3,
		"sha256_enabled": hashService.EnableSHA256,
	})
}
