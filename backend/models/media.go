package models

// ==================== 媒体（media）单表模型 ====================
//
// 单表设计（v2 精简版，2026-06-12）：
//   - "多对多"用嵌套 slice 表达（存库时序列化为 JSON 列）
//   - 3 种哈希：HashSM3 / HashSHA256 / HashCombined
//   - 软删由 DB 层（deleted_at）+ 状态字段（status）双控
// ----------------------------------------------------------------------------

// 媒体类型
type MediaType string

const (
	MediaTypePhoto MediaType = "photo"
	MediaTypeVideo MediaType = "video"
	MediaTypeAudio MediaType = "audio" // 预留（语音备注等）
)

// 来源
type MediaSource string

const (
	MediaSourceCamera MediaSource = "camera"
	MediaSourceUpload MediaSource = "upload"
)

// 状态
type MediaStatus string

const (
	MediaStatusActive   MediaStatus = "active"
	MediaStatusArchived MediaStatus = "archived"
	MediaStatusDeleted  MediaStatus = "deleted"
)

// IsValidMediaType 检查 type 字段是否合法
func IsValidMediaType(t string) bool {
	switch t {
	case string(MediaTypePhoto), string(MediaTypeVideo), string(MediaTypeAudio):
		return true
	}
	return false
}

// IsValidMediaSource 检查 source 字段是否合法
func IsValidMediaSource(s string) bool {
	switch s {
	case string(MediaSourceCamera), string(MediaSourceUpload):
		return true
	}
	return false
}

// IsValidMediaStatus 检查 status 字段是否合法
func IsValidMediaStatus(s string) bool {
	switch s {
	case string(MediaStatusActive), string(MediaStatusArchived), string(MediaStatusDeleted):
		return true
	}
	return false
}

// MediaTag 标签（自由文本 + 显示色）。
// 自由文本不进字典，由用户/系统在使用时直接写。
// Color 存的是 el-tag type（primary/success/warning/danger/info），
// 前端直接绑定 :type="color"；omitempty 让未设颜色的自定义 tag 不输出空字段。
type MediaTag struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"` // el-tag type：primary/success/warning/danger/info
}

// MediaBinding 媒体与文档的关联。
// target_type 字典：third_party（三方合同）
// role 字典：attachment（附件） / original（原件） / copy（副本）
type MediaBinding struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Role       string `json:"role"`
	Remark     string `json:"remark,omitempty"`
}

// MediaAuditEntry 审计环形缓冲单条（保留最近 50 条）。
// 历史事件被覆盖，业务可接受（合规要求 ≠ 完整日志）。
type MediaAuditEntry struct {
	Action  string `json:"action"` // view / download / delete / restore / upload / reused / bind / unbind
	ActorID int64  `json:"actor_id"`
	Ts      int64  `json:"ts"` // unix seconds
}

// MediaHashes 3 种哈希的便捷结构（用于 API 响应）
type MediaHashes struct {
	SM3          string `json:"sm3"`
	SHA256       string `json:"sha256"`
	Combined     string `json:"combined"`
	Algorithm    string `json:"algorithm"`
	DataLength   int64  `json:"data_length"`
	CalculatedAt int64  `json:"calculated_at"`
}

// Media 媒体主结构（对应 media 表）。
type Media struct {
	ID           int64     `json:"id"`
	SnowID       string    `json:"snowid"`
	Type         MediaType `json:"type"`
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name,omitempty"`
	MimeType     string    `json:"mime_type,omitempty"`

	FilePath  string `json:"file_path"`
	ThumbPath string `json:"thumb_path,omitempty"`
	FileSize  int64  `json:"file_size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  int    `json:"duration"` // 秒（视频 / 音频）

	// 3 种哈希（核心）
	//   JSON tag 与前端 Media 类型（api/media.ts）保持一致：hash_sm3 / hash_sha256 / hash_combined
	HashSM3      string `json:"hash_sm3"`
	HashSHA256   string `json:"hash_sha256"`
	HashCombined string `json:"hash_combined"`

	Source        MediaSource `json:"source"`
	SourceRef     string      `json:"source_ref,omitempty"`
	WatermarkText string      `json:"watermark_text,omitempty"`
	WatermarkMode string      `json:"watermark_mode,omitempty"` // corner / cross / tile

	TakenAt int64 `json:"taken_at"`
	TakenBy int64 `json:"taken_by"`

	// v2.1：客户关联（主索引）+ 用户备用字段（内部审计 / 配额）
	CustomerID int64 `json:"customer_id"`
	UserID     int64 `json:"user_id"`

	// 2026-06-28 RBAC v3：创建时快照上传者主部门；data_scope 过滤维度（与 user_id 联合控制）。
	DepartmentID int64 `json:"department_id"`

	// JSON 列（3 个"多"）
	Tags     []MediaTag        `json:"tags"`
	Bindings []MediaBinding    `json:"bindings"`
	Audit    []MediaAuditEntry `json:"audit,omitempty"`

	// 计数（避免每次都解析 audit JSON）
	ViewCount     int `json:"view_count"`
	DownloadCount int `json:"download_count"`

	Remark string      `json:"remark,omitempty"`
	Status MediaStatus `json:"status"`

	CreatedBy int64 `json:"created_by"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
	DeletedAt int64 `json:"deleted_at"` // 0 = 未删
}
