package services

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"doc/utils"
)

// ==================== 媒体文件存储（v2 单表）====================
//
// 文件路径：{Upload.Dir}/media/{YYYYMM}/{snowid}.{ext}
// 缩略图：  {Upload.Dir}/media/{YYYYMM}/{snowid}_thumb.jpg
// ----------------------------------------------------------------------------

// MediaStorage 负责媒体文件存储 + 缩略图生成。
type MediaStorage struct {
	uploadDir string
}

var mediaStorage *MediaStorage

// InitMediaStorage 初始化媒体存储。在 main 启动时调用一次。
func InitMediaStorage(uploadDir string) {
	mediaStorage = &MediaStorage{uploadDir: uploadDir}
	utils.Info("[媒体存储] 初始化完成，根目录: %s", uploadDir)
}

// GetMediaStorage 获取全局单例。
func GetMediaStorage() *MediaStorage {
	if mediaStorage == nil {
		panic("MediaStorage 未初始化，请先调用 InitMediaStorage")
	}
	return mediaStorage
}

// SaveResult 保存结果
type SaveResult struct {
	SnowID  string
	RelPath string
	AbsPath string
	Width   int
	Height  int
}

// SaveMediaAsset 保存媒体字节到 media/{YYYYMM}/{snowid}.{ext}。
//   - data:    媒体文件字节
//   - ext:     扩展名（不含点，如 "jpg" / "mp4"）
//   - customSnowID: 可选，外部传入的 snowid；传空时函数内生成
//
// 返回：snowid、相对路径、绝对路径、媒体宽高（图片有效，视频为 0）。
func (s *MediaStorage) SaveMediaAsset(data []byte, ext string, customSnowID ...string) (*SaveResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("媒体数据为空")
	}

	// 1. snowid
	var snowid string
	if len(customSnowID) > 0 && customSnowID[0] != "" {
		snowid = customSnowID[0]
	} else {
		snowid = utils.NextSnowIDString()
	}

	// 2. 路径
	yearMonth := time.Now().Format("200601")
	relDir := filepath.Join("media", yearMonth)
	absDir := filepath.Join(s.uploadDir, relDir)
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	// 3. 写文件
	ext = strings.TrimPrefix(ext, ".")
	filename := fmt.Sprintf("%s.%s", snowid, ext)
	absPath := filepath.Join(absDir, filename)
	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	relPath := filepath.ToSlash(filepath.Join(relDir, filename))

	// 4. 解析图片宽高（仅图片类型）
	width, height := decodeImageDimensions(data, ext)

	utils.Info("[媒体存储] 保存成功: %s (size=%d bytes, %dx%d)", relPath, len(data), width, height)
	return &SaveResult{
		SnowID:  snowid,
		RelPath: relPath,
		AbsPath: absPath,
		Width:   width,
		Height:  height,
	}, nil
}

// SaveMediaFromMultipart 从 multipart.File 直接保存（避免大文件全载入内存）。
//
// P0 修复（2026-06-14 续）：
//   - 历史 bug：仅用 filepath.Ext(file.Filename) 决定扩展名 → 客户端误传 .jpg 但实际是 PNG 时，
//     文件存盘后扩展名错，导致下游 thumbnail / HTTP Content-Disposition 都跟着错。
//   - 修复：优先用 http.DetectContentType(data) 检测真实 MIME 派生扩展名；
//     filename 扩展名仅作"快速路径"（与检测结果一致时直接用，避免重复 IO）。
func (s *MediaStorage) SaveMediaFromMultipart(file *multipart.FileHeader, customSnowID ...string) (*SaveResult, []byte, error) {
	if file == nil {
		return nil, nil, fmt.Errorf("文件为空")
	}
	src, err := file.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return nil, nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// P0 修复：先按真实内容检测 MIME（不可被 filename 伪造）
	detectedMime := http.DetectContentType(data)
	extFromContent := detectExtFromContentType(detectedMime)
	extFromFilename := strings.TrimPrefix(strings.ToLower(filepath.Ext(file.Filename)), ".")

	// 决策树：
	//   1. filename 扩展名 与 content 检测一致 → 用 filename（快路径）
	//   2. 不一致 → 用 content（以二进制为准，filename 可能是错的）
	//   3. content 检测不到 → 用 filename
	//   4. 都检测不到 → "bin"
	ext := ""
	if extFromContent != "" && extFromContent == extFromFilename {
		ext = extFromFilename
	} else if extFromContent != "" {
		utils.Warn("[media] 文件名扩展名 %q 与内容检测 %q 不一致，以二进制为准", extFromFilename, extFromContent)
		ext = extFromContent
	} else if extFromFilename != "" {
		ext = extFromFilename
	} else {
		ext = "bin"
	}

	res, err := s.SaveMediaAsset(data, ext, customSnowID...)
	return res, data, err
}

// GenerateThumbnail 为图片生成缩略图（最大 320×240 JPEG，使用 stdlib NearestNeighbor）。
// 视频暂不抽帧（需要 ffmpeg，留待后续）。
func (s *MediaStorage) GenerateThumbnail(originalRelPath string, sourceData []byte, ext string) (thumbRelPath string, err error) {
	if !isImageExt(ext) {
		return "", nil // 非图片不生成
	}

	// 1. 解码
	srcImg, _, err := image.Decode(bytes.NewReader(sourceData))
	if err != nil {
		return "", fmt.Errorf("解码图片失败: %w", err)
	}

	// 2. 缩放（保持纵横比，最大 320×240）
	const maxW, maxH = 320, 240
	srcBounds := srcImg.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}
	if scale > 1.0 {
		scale = 1.0 // 原图已更小，不放大
	}
	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), srcImg, srcBounds, xdraw.Over, nil)

	// 3. 编码为 JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return "", fmt.Errorf("编码缩略图失败: %w", err)
	}

	// 4. 写文件：{stem}_thumb.jpg 紧挨着原文件
	dir := filepath.Dir(originalRelPath)
	base := filepath.Base(originalRelPath)
	extPart := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extPart)
	thumbName := stem + "_thumb.jpg"
	thumbAbs := filepath.Join(s.uploadDir, dir, thumbName)
	if err := os.WriteFile(thumbAbs, buf.Bytes(), 0644); err != nil {
		return "", fmt.Errorf("写缩略图失败: %w", err)
	}
	thumbRel := filepath.ToSlash(filepath.Join(dir, thumbName))
	return thumbRel, nil
}

// GetAbsolutePath 把相对路径（入库的 file_path）解析为绝对路径。
//
// P0 修复（2026-06-28）：增加路径 containment 校验。
//   历史版本仅做 filepath.Join(uploadDir, relPath)，若 DB file_path 被篡改为
//   "../../../etc/passwd" 等，可逃出 uploadDir 读任意文件。
//   现在统一走 resolveSafeAbsPath，relPath 含 ".." 或绝对路径前缀时返回 ErrPathTraversal。
//   调用方应将 ErrPathTraversal 视为 403/404，禁止访问。
func (s *MediaStorage) GetAbsolutePath(relPath string) (string, error) {
	return resolveSafeAbsPath(s.uploadDir, relPath)
}

// SaveThumbnail 把外部传入的缩略图字节写到标准位置（{stem}_thumb.jpg）。
//   - 用于：前端为视频抽帧后随上传一起 POST（避免后端依赖 ffmpeg）
//   - originalRelPath：原始文件的相对路径（用来推导目录和 stem）
//   - thumbData：JPEG 字节（推荐 image/jpeg, 0.7 压缩）
//
// 返回：thumb 相对路径。
func (s *MediaStorage) SaveThumbnail(originalRelPath string, thumbData []byte) (string, error) {
	if len(thumbData) == 0 {
		return "", fmt.Errorf("缩略图数据为空")
	}
	dir := filepath.Dir(originalRelPath)
	base := filepath.Base(originalRelPath)
	extPart := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extPart)
	thumbName := stem + "_thumb.jpg"
	thumbAbs := filepath.Join(s.uploadDir, dir, thumbName)
	if err := os.WriteFile(thumbAbs, thumbData, 0644); err != nil {
		return "", fmt.Errorf("写缩略图失败: %w", err)
	}
	return filepath.ToSlash(filepath.Join(dir, thumbName)), nil
}

// DeleteFiles 删除文件（用于重传前清理 / 软删物理清理等）。
// 不存在不报错。
func (s *MediaStorage) DeleteFiles(relPaths ...string) error {
	for _, rp := range relPaths {
		if rp == "" {
			continue
		}
		abs := filepath.Join(s.uploadDir, rp)
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ==================== 内部辅助 ====================

// decodeImageDimensions 解析图片字节的宽高（仅 image/* 类型有效）。
func decodeImageDimensions(data []byte, ext string) (int, int) {
	if !isImageExt(ext) {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		utils.Warn("[媒体存储] 解析图片尺寸失败: %v", err)
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// isImageExt 判断扩展名是否为图片
func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp":
		return true
	}
	return false
}

// detectExtFromContentType 从 MIME 推导扩展名
func detectExtFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	switch ct {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav":
		return "wav"
	}
	return ""
}
