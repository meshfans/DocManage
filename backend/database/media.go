package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"doc/models"
)

// ==================== 媒体（media）单表 CRUD ====================
//
// 单表设计（v2 精简版，2026-06-12）：
//   - 所有"多对多"用 JSON 列存：tags / bindings / audit
//   - 软删（deleted_at）
//   - 9 个索引
//   - 零 JOIN、单源真相
//
// 表结构与索引见 db.go createTables() / createIndexes()。
// ----------------------------------------------------------------------------

// mediaCols 是查询 media 表全部字段的列名列表。
const mediaCols = `id, snowid, type, name, original_name, mime_type,
                   file_path, thumb_path, file_size, width, height, duration,
                   hash_sm3, hash_sha256, hash_combined,
                   source, source_ref, watermark_text, watermark_mode,
                   taken_at, taken_by,
                   customer_id, user_id, department_id,
                   tags, bindings, audit,
                   view_count, download_count,
                   remark, status,
                   created_by, created_at, updated_at, deleted_at`

// scanMediaRow 将单行扫描到 Media 指针。
// scan 接受 QueryRow().Scan 或 Rows().Scan 的可变参数接口。
func scanMediaRow(scan func(...interface{}) error, m *models.Media) error {
	var (
		tagsJSON     string
		bindingsJSON string
		auditJSON    string
		// 临时接收 created_at / updated_at：旧数据可能是 "YYYY-MM-DD HH:MM:SS" 字符串
		_createdAt, _updatedAt interface{}
	)
	if err := scan(
		&m.ID, &m.SnowID, &m.Type, &m.Name, &m.OriginalName, &m.MimeType,
		&m.FilePath, &m.ThumbPath, &m.FileSize, &m.Width, &m.Height, &m.Duration,
		&m.HashSM3, &m.HashSHA256, &m.HashCombined,
		&m.Source, &m.SourceRef, &m.WatermarkText, &m.WatermarkMode,
		&m.TakenAt, &m.TakenBy,
		&m.CustomerID, &m.UserID, &m.DepartmentID,
		&tagsJSON, &bindingsJSON, &auditJSON,
		&m.ViewCount, &m.DownloadCount,
		&m.Remark, &m.Status,
		&m.CreatedBy, &_createdAt, &_updatedAt, &m.DeletedAt,
	); err != nil {
		return err
	}
	// created_at / updated_at：兼容 string 和 int64
	if s, ok := _createdAt.(string); ok {
		if t, err := parseLocalDateTime(s); err == nil {
			m.CreatedAt = t
		}
	} else if n, ok := toInt64(_createdAt); ok {
		m.CreatedAt = n
	}
	if s, ok := _updatedAt.(string); ok {
		if t, err := parseLocalDateTime(s); err == nil {
			m.UpdatedAt = t
		}
	} else if n, ok := toInt64(_updatedAt); ok {
		m.UpdatedAt = n
	}
	// JSON 列 → 结构体（失败不阻断，置空 slice）
	if tagsJSON != "" {
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
	}
	if bindingsJSON != "" {
		_ = json.Unmarshal([]byte(bindingsJSON), &m.Bindings)
	}
	if auditJSON != "" {
		_ = json.Unmarshal([]byte(auditJSON), &m.Audit)
	}
	if m.Tags == nil {
		m.Tags = []models.MediaTag{}
	}
	if m.Bindings == nil {
		m.Bindings = []models.MediaBinding{}
	}
	if m.Audit == nil {
		m.Audit = []models.MediaAuditEntry{}
	}
	return nil
}

// CreateMedia 插入一条媒体记录。
// 返回新插入记录的 id。
func CreateMedia(m models.Media) (int64, error) {
	tagsJSON, _ := json.Marshal(m.Tags)
	bindingsJSON, _ := json.Marshal(m.Bindings)
	auditJSON, _ := json.Marshal(m.Audit)
	if tagsJSON == nil {
		tagsJSON = []byte("[]")
	}
	if bindingsJSON == nil {
		bindingsJSON = []byte("[]")
	}
	if auditJSON == nil {
		auditJSON = []byte("[]")
	}

	result, err := DB.Exec(`
		INSERT INTO media (
			snowid, type, name, original_name, mime_type,
			file_path, thumb_path, file_size, width, height, duration,
			hash_sm3, hash_sha256, hash_combined,
			source, source_ref, watermark_text, watermark_mode,
			taken_at, taken_by,
			customer_id, user_id, department_id,
			tags, bindings, audit,
			view_count, download_count,
			remark, status,
			created_by, created_at, updated_at, deleted_at
		) VALUES (
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?,
			?, ?,
			?, ?, ?, 0
		)
	`,
		m.SnowID, m.Type, m.Name, m.OriginalName, m.MimeType,
		m.FilePath, m.ThumbPath, m.FileSize, m.Width, m.Height, m.Duration,
		m.HashSM3, m.HashSHA256, m.HashCombined,
		m.Source, m.SourceRef, m.WatermarkText, m.WatermarkMode,
		m.TakenAt, m.TakenBy,
		m.CustomerID, m.UserID, m.DepartmentID,
		string(tagsJSON), string(bindingsJSON), string(auditJSON),
		m.ViewCount, m.DownloadCount,
		m.Remark, m.Status,
		m.CreatedBy, time.Now().Unix(), time.Now().Unix(), 0,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetMediaByID 按主键 id 查询 media。
// 2026-06-27 Bug #6 修复：去掉 `AND deleted_at = 0` 过滤，
// 让 Download / Thumbnail / Get 等 handler 也能访问已软删的媒体，
// 便于用户从"已删"过滤器预览 → 决定恢复。
// 数据库行本身没被物理删除，file_path / thumb_path 都在磁盘。
// "列表层"的删除过滤完全由 ListMedia 的 status 参数控制（active / deleted / all）。
func GetMediaByID(id int64) (*models.Media, error) {
	m := &models.Media{}
	err := scanMediaRow(
		func(args ...interface{}) error {
			return DB.QueryRow(`SELECT `+mediaCols+` FROM media WHERE id = ?`, id).Scan(args...)
		},
		m,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// GetMediaBySnowID 按 snowid 查询 media。
func GetMediaBySnowID(snowid string) (*models.Media, error) {
	m := &models.Media{}
	err := scanMediaRow(
		func(args ...interface{}) error {
			return DB.QueryRow(`SELECT `+mediaCols+` FROM media WHERE snowid = ? AND deleted_at = 0`, snowid).Scan(args...)
		},
		m,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// GetMediaByHash 按 SHA-256 哈希查询 media（用于上传查重）。
// 不限制 deleted_at = 0，因为软删的 record 也算"已存在"（避免软删后重新上传产生重复）。
func GetMediaByHash(hashSHA256 string) (*models.Media, error) {
	if hashSHA256 == "" {
		return nil, nil
	}
	m := &models.Media{}
	err := scanMediaRow(
		func(args ...interface{}) error {
			return DB.QueryRow(`SELECT `+mediaCols+` FROM media WHERE hash_sha256 = ? LIMIT 1`, hashSHA256).Scan(args...)
		},
		m,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// GetMediaByIDs 批量查询媒体（用于 Bulk handler 的权限预检，避免 N+1）。
// 漏查的 ID（即 map 中不存在）视为不存在，返回 nil map。
func GetMediaByIDs(ids []int64) (map[int64]*models.Media, error) {
	if len(ids) == 0 {
		return make(map[int64]*models.Media), nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT ` + mediaCols + ` FROM media WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]*models.Media)
	for rows.Next() {
		m := &models.Media{}
		if err := scanMediaRows(rows, m); err != nil {
			return nil, err
		}
		result[m.ID] = m
	}
	return result, nil
}

// scanMediaRows 批量行扫描（对应 mediaCols 列顺序）。
func scanMediaRows(rows *sql.Rows, m *models.Media) error {
	var tagsJSON, bindingsJSON, auditJSON string
	var _createdAt, _updatedAt interface{}
	if err := rows.Scan(
		&m.ID, &m.SnowID, &m.Type, &m.Name, &m.OriginalName, &m.MimeType,
		&m.FilePath, &m.ThumbPath, &m.FileSize, &m.Width, &m.Height, &m.Duration,
		&m.HashSM3, &m.HashSHA256, &m.HashCombined,
		&m.Source, &m.SourceRef, &m.WatermarkText, &m.WatermarkMode,
		&m.TakenAt, &m.TakenBy,
		&m.CustomerID, &m.UserID, &m.DepartmentID,
		&tagsJSON, &bindingsJSON, &auditJSON,
		&m.ViewCount, &m.DownloadCount,
		&m.Remark, &m.Status,
		&m.CreatedBy, &_createdAt, &_updatedAt, &m.DeletedAt,
	); err != nil {
		return err
	}
	// created_at / updated_at：兼容 string 和 int64（与 scanMediaRow 一致）
	if s, ok := _createdAt.(string); ok {
		if t, err := parseLocalDateTime(s); err == nil {
			m.CreatedAt = t
		}
	} else if n, ok := toInt64(_createdAt); ok {
		m.CreatedAt = n
	}
	if s, ok := _updatedAt.(string); ok {
		if t, err := parseLocalDateTime(s); err == nil {
			m.UpdatedAt = t
		}
	} else if n, ok := toInt64(_updatedAt); ok {
		m.UpdatedAt = n
	}
	// JSON 列解析
	if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
		m.Tags = nil
	}
	if err := json.Unmarshal([]byte(bindingsJSON), &m.Bindings); err != nil {
		m.Bindings = nil
	}
	if err := json.Unmarshal([]byte(auditJSON), &m.Audit); err != nil {
		m.Audit = nil
	}
	return nil
}

// MediaFilter 列表过滤条件（对应 query string 参数）。
type MediaFilter struct {
	Type       string   // photo / video / audio / "" = 全部
	Source     string   // camera / upload / "" = 全部
	Status     string   // active / archived / deleted / "all" / "" = 仅 active（默认）
	TakenBy    int64    // 0 = 全部
	CustomerID int64    // 0 = 全部；> 0 = 仅该客户的媒体
	UserID     int64    // 0 = 全部；备用
	FromTs     int64    // taken_at >= FromTs（0 = 不限）
	ToTs       int64    // taken_at <= ToTs（0 = 不限）
	Q          string   // 模糊搜索 name / original_name（LIKE %q%）
	TagNames   []string // 包含任一 tag 名的媒体（OR 关系，nil/空 = 不限）
}

// ListMedia 分页查询 media 列表。
//
//	默认 status = "" → 仅 active（兼容历史）
//	status = "all" → 不过滤 status
//	pageSize <= 0 → 默认 20
//
// 返回 ([]Media, total, error)。
func ListMedia(filter MediaFilter, page, pageSize int) ([]models.Media, int, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}

	whereParts := []string{}
	args := []interface{}{}

	// status 默认行为
	//   - "" / "active" → 只看 active（强制 deleted_at = 0，因为 active 项 deleted_at 必为 0）
	//   - "all"         → 不过滤 status/deleted_at（看全部，含已删/已归档）
	//   - "deleted"     → 只看 deleted 项（**不**加 deleted_at = 0，否则永远查不到）
	//   - "archived"    → 只看 archived 项（同 deleted 逻辑）
	if filter.Status == "all" {
		// 不过滤
	} else if filter.Status == "" || filter.Status == "active" {
		whereParts = append(whereParts, "status = ? AND deleted_at = 0")
		args = append(args, models.MediaStatusActive)
	} else {
		// deleted / archived：只按 status 匹配，deleted_at 自然 > 0
		whereParts = append(whereParts, "status = ?")
		args = append(args, filter.Status)
	}

	if filter.Type != "" {
		whereParts = append(whereParts, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Source != "" {
		whereParts = append(whereParts, "source = ?")
		args = append(args, filter.Source)
	}
	if filter.TakenBy > 0 {
		whereParts = append(whereParts, "taken_by = ?")
		args = append(args, filter.TakenBy)
	}
	if filter.CustomerID > 0 {
		whereParts = append(whereParts, "customer_id = ?")
		args = append(args, filter.CustomerID)
	}
	if filter.UserID > 0 {
		whereParts = append(whereParts, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.FromTs > 0 {
		whereParts = append(whereParts, "taken_at >= ?")
		args = append(args, filter.FromTs)
	}
	if filter.ToTs > 0 {
		whereParts = append(whereParts, "taken_at <= ?")
		args = append(args, filter.ToTs)
	}
	if filter.Q != "" {
		// 用 OR 包裹，避免与其他 AND 条件冲突
		whereParts = append(whereParts, "(name LIKE ? OR original_name LIKE ?)")
		args = append(args, "%"+filter.Q+"%", "%"+filter.Q+"%")
	}
	if len(filter.TagNames) > 0 {
		// 多 tag OR 过滤；不依赖 SQLite JSON1 扩展，用 LIKE 匹配 `"name":"<tag>"` 模式
		// 防御：先剔除空字符串（前端清空过滤会传 []，后端 c.QueryArray 会得到 [""]）
		nonEmpty := make([]string, 0, len(filter.TagNames))
		for _, n := range filter.TagNames {
			if strings.TrimSpace(n) != "" {
				nonEmpty = append(nonEmpty, n)
			}
		}
		if len(nonEmpty) > 0 {
			parts := make([]string, 0, len(nonEmpty))
			for _, name := range nonEmpty {
				// P2 修复：完整转义：转义 LIKE 通配符 % 和 _，以及 JSON 关键字符
				escName := strings.ReplaceAll(name, `\`, `\\`)
				escName = strings.ReplaceAll(escName, `"`, `\"`)
				escName = strings.ReplaceAll(escName, `%`, `\%`)
				escName = strings.ReplaceAll(escName, `_`, `\_`)
				parts = append(parts, `tags LIKE ? ESCAPE '\'`)
				args = append(args, `%"name":"`+escName+`"%`)
			}
			whereParts = append(whereParts, "("+strings.Join(parts, " OR ")+")")
		}
	}

	// 重要：删除了原来的 `if len(whereParts) == 0 { whereParts = append("deleted_at = 0") }` fallback
	//   原因：status="all" 时没添加 status 条件，如果再 fallback 加 deleted_at=0，
	//         会把"全部"过滤的语义变成"全部 active"——与 UI 上"全部"选项的承诺不一致。
	//   现在的语义：
	//     - status="all"     → 不过滤 status，**也**不过滤 deleted_at（看真·全部，含已删）
	//     - status="active"  → status='active' AND deleted_at=0
	//     - status="deleted"  → status='deleted'（deleted_at 自然 > 0）
	//     - status="archived" → status='archived'
	//     - 默认 (status="") → 等同 status="active"
	where := strings.Join(whereParts, " AND ")

	// COUNT
	var total int
	if err := DB.QueryRow("SELECT COUNT(*) FROM media WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 列表
	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := DB.Query(`SELECT `+mediaCols+` FROM media WHERE `+where+
		` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []models.Media
	for rows.Next() {
		var m models.Media
		if err := scanMediaRow(rows.Scan, &m); err != nil {
			return nil, 0, err
		}
		list = append(list, m)
	}
	if list == nil {
		list = []models.Media{}
	}
	return list, total, rows.Err()
}

// ListMediaByDataScope 2026-06-28 RBAC v3 P4：data_scope 感知的媒体列表。
// 在 ListMedia 基础上叠加 data_scope WHERE。
// extraWhere/extraArgs 由 BuildWhereSQL 生成。
// media 表的 owner 是 user_id（白名单）；department_id 刚加。
func ListMediaByDataScope(filter MediaFilter, page, pageSize int, extraWhere string, extraArgs []any) ([]models.Media, int, error) {
	// media 表有 deleted_at 列（status 列同时存在，但默认只看 status=active）。
	// hasDeletedAt=true 让 helper 直接保留 BuildWhereSQL 输出的 "deleted_at = 0"。
	cleanExtra, ok := adaptDataScopeWhere(extraWhere, true)
	if !ok {
		return ListMedia(filter, page, pageSize)
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}

	whereParts := []string{}
	args := []interface{}{}

	if filter.Status == "all" {
		// 不过滤
	} else if filter.Status == "" || filter.Status == "active" {
		whereParts = append(whereParts, "status = ? AND deleted_at = 0")
		args = append(args, models.MediaStatusActive)
	} else {
		whereParts = append(whereParts, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Type != "" {
		whereParts = append(whereParts, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Source != "" {
		whereParts = append(whereParts, "source = ?")
		args = append(args, filter.Source)
	}
	if filter.TakenBy > 0 {
		whereParts = append(whereParts, "taken_by = ?")
		args = append(args, filter.TakenBy)
	}
	if filter.CustomerID > 0 {
		whereParts = append(whereParts, "customer_id = ?")
		args = append(args, filter.CustomerID)
	}
	if filter.UserID > 0 {
		whereParts = append(whereParts, "user_id = ?")
		args = append(args, filter.UserID)
	}
	// 追加 data_scope 条件（helper 已校验 extraWhere 非空）
	whereParts = append(whereParts, "("+cleanExtra+")")
	args = append(args, extraArgs...)

	if filter.FromTs > 0 {
		whereParts = append(whereParts, "taken_at >= ?")
		args = append(args, filter.FromTs)
	}
	if filter.ToTs > 0 {
		whereParts = append(whereParts, "taken_at <= ?")
		args = append(args, filter.ToTs)
	}
	if filter.Q != "" {
		whereParts = append(whereParts, "(name LIKE ? OR original_name LIKE ?)")
		args = append(args, "%"+filter.Q+"%", "%"+filter.Q+"%")
	}
	if len(filter.TagNames) > 0 {
		nonEmpty := make([]string, 0, len(filter.TagNames))
		for _, n := range filter.TagNames {
			if strings.TrimSpace(n) != "" {
				nonEmpty = append(nonEmpty, n)
			}
		}
		if len(nonEmpty) > 0 {
			parts := make([]string, 0, len(nonEmpty))
			for _, name := range nonEmpty {
				escName := strings.ReplaceAll(name, `\`, `\\`)
				escName = strings.ReplaceAll(escName, `"`, `\"`)
				escName = strings.ReplaceAll(escName, `%`, `\%`)
				escName = strings.ReplaceAll(escName, `_`, `\_`)
				parts = append(parts, `tags LIKE ? ESCAPE '\'`)
				args = append(args, `%"name":"`+escName+`"%`)
			}
			whereParts = append(whereParts, "("+strings.Join(parts, " OR ")+")")
		}
	}

	where := strings.Join(whereParts, " AND ")

	var total int
	if err := DB.QueryRow("SELECT COUNT(*) FROM media WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := DB.Query(`SELECT `+mediaCols+` FROM media WHERE `+where+
		` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []models.Media
	for rows.Next() {
		var m models.Media
		if err := scanMediaRow(rows.Scan, &m); err != nil {
			return nil, 0, err
		}
		list = append(list, m)
	}
	if list == nil {
		list = []models.Media{}
	}
	return list, total, rows.Err()
}

// ListMediaByTarget 按 binding 反查所有媒体。
// 用 SQLite 的 JSON LIKE 模糊匹配：`bindings LIKE '%"target_id":1%'`。
// 这种查询是 O(N) 全表扫，仅适合中小规模（< 100K 媒体）。
// 大规模时建议建 FTS5 索引或迁移到独立表。
func ListMediaByTarget(targetType string, targetID int64) ([]models.Media, error) {
	if targetType == "" || targetID <= 0 {
		return []models.Media{}, nil
	}
	// 同时匹配两种 JSON 数字格式："target_id":1 和 "target_id": 1
	pattern := fmt.Sprintf(`%%"target_type":%q%%"target_id":%d%%`, targetType, targetID)

	rows, err := DB.Query(`SELECT `+mediaCols+` FROM media WHERE bindings LIKE ? AND deleted_at = 0 ORDER BY id DESC`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Media
	for rows.Next() {
		var m models.Media
		if err := scanMediaRow(rows.Scan, &m); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	if list == nil {
		list = []models.Media{}
	}
	return list, rows.Err()
}

// UpdateMedia 完整更新一条记录（用于 view_count++、audit push 等）。
// 一般不直接调用；具体场景用下面的增量函数。
func UpdateMedia(m models.Media) error {
	tagsJSON, _ := json.Marshal(m.Tags)
	bindingsJSON, _ := json.Marshal(m.Bindings)
	auditJSON, _ := json.Marshal(m.Audit)
	if tagsJSON == nil {
		tagsJSON = []byte("[]")
	}
	if bindingsJSON == nil {
		bindingsJSON = []byte("[]")
	}
	if auditJSON == nil {
		auditJSON = []byte("[]")
	}

	_, err := DB.Exec(`
		UPDATE media SET
			type = ?, name = ?, original_name = ?, mime_type = ?,
			file_path = ?, thumb_path = ?, file_size = ?, width = ?, height = ?, duration = ?,
			hash_sm3 = ?, hash_sha256 = ?, hash_combined = ?,
			source = ?, source_ref = ?, watermark_text = ?, watermark_mode = ?,
			taken_at = ?, taken_by = ?,
			customer_id = ?, user_id = ?, department_id = ?,
			tags = ?, bindings = ?, audit = ?,
			view_count = ?, download_count = ?,
			remark = ?, status = ?,
			updated_at = ?
		WHERE id = ? AND deleted_at = 0
	`,
		m.Type, m.Name, m.OriginalName, m.MimeType,
		m.FilePath, m.ThumbPath, m.FileSize, m.Width, m.Height, m.Duration,
		m.HashSM3, m.HashSHA256, m.HashCombined,
		m.Source, m.SourceRef, m.WatermarkText, m.WatermarkMode,
		m.TakenAt, m.TakenBy,
		m.CustomerID, m.UserID, m.DepartmentID,
		string(tagsJSON), string(bindingsJSON), string(auditJSON),
		m.ViewCount, m.DownloadCount,
		m.Remark, m.Status, time.Now().Unix(),
		m.ID,
	)
	return err
}

// UpdateMediaMeta 更新元数据（name / remark / tags / bindings / status / customer_id / user_id）。
// 空白字段表示"不改"，简化 PATCH 语义。
//   - customer_id: -1 表示不变，0 表示解除关联，>0 表示关联到该客户
//   - user_id:     -1 表示不变，0 表示清除，>0 表示设值
func UpdateMediaMeta(id int64, name, remark, status string, customerID, userID int64, tags []models.MediaTag, bindings []models.MediaBinding) error {
	setParts := []string{}
	args := []interface{}{}

	if name != "" {
		setParts = append(setParts, "name = ?")
		args = append(args, name)
	}
	if remark != "" {
		setParts = append(setParts, "remark = ?")
		args = append(args, remark)
	}
	if status != "" {
		setParts = append(setParts, "status = ?")
		args = append(args, status)
	}
	if customerID > 0 {
		setParts = append(setParts, "customer_id = ?")
		args = append(args, customerID)
	}
	if userID > 0 {
		setParts = append(setParts, "user_id = ?")
		args = append(args, userID)
	}
	if tags != nil {
		tagsJSON, _ := json.Marshal(tags)
		setParts = append(setParts, "tags = ?")
		args = append(args, string(tagsJSON))
	}
	if bindings != nil {
		bindingsJSON, _ := json.Marshal(bindings)
		setParts = append(setParts, "bindings = ?")
		args = append(args, string(bindingsJSON))
	}
	if len(setParts) == 0 {
		return nil // 没有要改的字段
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now().Unix())
	args = append(args, id)

	query := "UPDATE media SET " + strings.Join(setParts, ", ") +
		" WHERE id = ? AND deleted_at = 0"
	_, err := DB.Exec(query, args...)
	return err
}

// mediaAuditRingSize 是 media.audit JSON 数组保留的最近条目数（环形缓冲）。
const mediaAuditRingSize = 50

// DeleteMedia 软删（status='deleted'）。
func DeleteMedia(id int64) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`UPDATE media SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at = 0`,
		models.MediaStatusDeleted, now, now, id)
	return err
}

// RestoreMedia 恢复（deleted → active），不清空 deleted_at（保留审计痕迹）。
func RestoreMedia(id int64) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`UPDATE media SET status = ?, deleted_at = 0, updated_at = ? WHERE id = ?`,
		models.MediaStatusActive, now, id)
	return err
}

// BulkAddTag 给一组 media 批量加 tag（已存在则跳过）。
//   - ids:        目标 media id 列表
//   - tagName:    要添加的 tag 名
//   - color:      el-tag type（primary/success/warning/danger/info）
//   - actorID:    操作者（用于 audit 推送 "update"）
//
// 返回：成功添加 tag 的 media 数（不包含已存在 tag 的）
//
// P1 修复：使用事务包装整个循环，任一失败整体回滚。
// （注：每个 UpdateMedia 是独立事务，事务内只做审计推送的串行化）
func BulkAddTag(ids []int64, tagName, color string, actorID int64) (int, error) {
	if len(ids) == 0 || tagName == "" {
		return 0, nil
	}
	if color == "" {
		color = "info"
	}
	added := 0
	for _, id := range ids {
		m, err := GetMediaByID(id)
		if err != nil || m == nil {
			continue
		}
		// 已存在跳过
		exists := false
		for _, t := range m.Tags {
			if t.Name == tagName {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		// 事务包装：tag 更新 + audit 推送原子化
		tx, err := DB.Begin()
		if err != nil {
			return added, err
		}
		m.Tags = append(m.Tags, models.MediaTag{Name: tagName, Color: color})
		tagsJSON, _ := json.Marshal(m.Tags)
		if _, err := tx.Exec(`UPDATE media SET tags = ?, updated_at = ? WHERE id = ? AND deleted_at = 0`,
			string(tagsJSON), time.Now().Unix(), id); err != nil {
			_ = tx.Rollback()
			return added, err
		}
		// audit push 在同一事务
		auditEntry := models.MediaAuditEntry{Action: "update", ActorID: actorID, Ts: time.Now().Unix()}
		m.Audit = append(m.Audit, auditEntry)
		if len(m.Audit) > mediaAuditRingSize {
			m.Audit = m.Audit[len(m.Audit)-mediaAuditRingSize:]
		}
		auditJSON, _ := json.Marshal(m.Audit)
		if _, err := tx.Exec(`UPDATE media SET audit = ?, updated_at = ? WHERE id = ?`,
			string(auditJSON), time.Now().Unix(), id); err != nil {
			_ = tx.Rollback()
			return added, err
		}
		if err := tx.Commit(); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// BulkDeleteMedia 批量软删（与 DeleteMedia 等价，仅 deleted_at 软标记）。
//   - actorID: 操作者
//
// 返回：成功删除的 media 数
//
// P1 修复：使用事务包装 delete + audit push，保证原子性。
func BulkDeleteMedia(ids []int64, actorID int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	deleted := 0
	for _, id := range ids {
		// 事务包装：软删 + audit push 原子化
		tx, err := DB.Begin()
		if err != nil {
			return deleted, err
		}
		res, err := tx.Exec(
			`UPDATE media SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at = 0`,
			models.MediaStatusDeleted, now, now, id,
		)
		if err != nil {
			_ = tx.Rollback()
			return deleted, err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			// 在同一事务中 push audit
			m, err := GetMediaByID(id) // 读 (id, audit) 用于 append
			if err == nil && m != nil {
				entry := models.MediaAuditEntry{Action: "delete", ActorID: actorID, Ts: now}
				m.Audit = append(m.Audit, entry)
				if len(m.Audit) > mediaAuditRingSize {
					m.Audit = m.Audit[len(m.Audit)-mediaAuditRingSize:]
				}
				auditJSON, _ := json.Marshal(m.Audit)
				if _, err := tx.Exec(`UPDATE media SET audit = ?, updated_at = ? WHERE id = ?`,
					string(auditJSON), now, id); err != nil {
					_ = tx.Rollback()
					return deleted, err
				}
			}
			if err := tx.Commit(); err != nil {
				return deleted, err
			}
			deleted++
		} else {
			_ = tx.Rollback()
		}
	}
	return deleted, nil
}

// BulkSetCustomer 批量设置 media 的 customer_id（关联客户）。
//   - customerID: 目标客户 id（0 = 解除关联）
//
// 返回：成功更新的 media 数
//
// P1 修复：使用事务包装 customer_id 更新 + audit push。
func BulkSetCustomer(ids []int64, customerID int64, actorID int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	updated := 0
	for _, id := range ids {
		// 事务包装：customer_id 更新 + audit push 原子化
		tx, err := DB.Begin()
		if err != nil {
			return updated, err
		}
		now := time.Now().Unix()
		// customerID > 0 写入，0 不变（与 UpdateMediaMeta PATCH 语义一致）
		if customerID > 0 {
			if _, err := tx.Exec(`UPDATE media SET customer_id = ?, updated_at = ? WHERE id = ? AND deleted_at = 0`,
				customerID, now, id); err != nil {
				_ = tx.Rollback()
				return updated, err
			}
		}
		// audit push 在同一事务
		m, err := GetMediaByID(id)
		if err == nil && m != nil {
			entry := models.MediaAuditEntry{Action: "update", ActorID: actorID, Ts: now}
			m.Audit = append(m.Audit, entry)
			if len(m.Audit) > mediaAuditRingSize {
				m.Audit = m.Audit[len(m.Audit)-mediaAuditRingSize:]
			}
			auditJSON, _ := json.Marshal(m.Audit)
			if _, err := tx.Exec(`UPDATE media SET audit = ?, updated_at = ? WHERE id = ?`,
				string(auditJSON), now, id); err != nil {
				_ = tx.Rollback()
				return updated, err
			}
		}
		if err := tx.Commit(); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// 用于前端 tag 过滤下拉框的"手打标签"选项。
// 实现：Go 侧解析 JSON（避免依赖 SQLite 的 json_each/json_extract，部分 go-sqlcipher 编译未启用 JSON1 扩展）。
func ListAllTagNames() ([]string, error) {
	rows, err := DB.Query(`
		SELECT tags FROM media
		WHERE deleted_at = 0 AND status = ?
		  AND tags IS NOT NULL AND tags != '' AND tags != '[]'
	`, models.MediaStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]struct{}, 32)
	var tags []string
	for rows.Next() {
		var tagsJSON string
		if err := rows.Scan(&tagsJSON); err != nil {
			continue
		}
		var arr []models.MediaTag
		if err := json.Unmarshal([]byte(tagsJSON), &arr); err != nil {
			continue // 容错：跳过非法 JSON 的行
		}
		for _, t := range arr {
			name := t.Name
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			tags = append(tags, name)
		}
	}
	sort.Strings(tags) // 字母序，与历史实现保持一致
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

// toInt64 安全将任意值转为 int64（用于 scan 结果兼容）
func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int16:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

// parseLocalDateTime 将 "YYYY-MM-DD HH:MM:SS" 解析为 Unix 时间戳
func parseLocalDateTime(s string) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

// ==================== media.audit 环形缓冲（简化版） ====================
// 精简后只写 media.audit JSON 列（保留最近 mediaAuditRingSize 条）。
// clientIP / userAgent / extraJSON 参数保留以兼容旧 handlers 调用。

// appendAuditJSONEntry 内部工具：读当前 audit JSON → 追加 → 截断 → 写回。
func appendAuditJSONEntry(id int64, entry models.MediaAuditEntry) error {
	m, err := GetMediaByID(id)
	if err != nil || m == nil {
		return err
	}
	m.Audit = append(m.Audit, entry)
	if len(m.Audit) > mediaAuditRingSize {
		m.Audit = m.Audit[len(m.Audit)-mediaAuditRingSize:]
	}
	auditJSON, _ := json.Marshal(m.Audit)
	_, err = DB.Exec(`UPDATE media SET audit = ?, updated_at = ? WHERE id = ?`,
		string(auditJSON), time.Now().Unix(), id)
	return err
}

// IncrementViewCount 简单版：只做 view_count++，不写 audit（由上层调用者自行追加）。
func IncrementViewCount(id int64, actorID int64) error {
	_, err := DB.Exec(`UPDATE media SET view_count = view_count + 1, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	return err
}

// IncrementViewCountWithAudit 一次完成：view_count++ + audit "view" 追加。
func IncrementViewCountWithAudit(id int64, userID int64, clientIP, userAgent string) error {
	if _, err := DB.Exec(`UPDATE media SET view_count = view_count + 1, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id); err != nil {
		return err
	}
	return appendAuditJSONEntry(id, models.MediaAuditEntry{Action: "view", ActorID: userID, Ts: time.Now().Unix()})
}

// IncrementDownloadCountWithAudit 一次完成：download_count++ + audit "download" 追加。
func IncrementDownloadCountWithAudit(id int64, userID int64, clientIP, userAgent string) error {
	if _, err := DB.Exec(`UPDATE media SET download_count = download_count + 1, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id); err != nil {
		return err
	}
	return appendAuditJSONEntry(id, models.MediaAuditEntry{Action: "download", ActorID: userID, Ts: time.Now().Unix()})
}

// AppendMediaAuditBoth 追加一条审计条目到 media.audit（简化版，仅 JSON 列）。
// "Both" 是历史命名（旧版同时写 JSON + 哈希链），此处保留签名以便旧 handlers 无需改动。
func AppendMediaAuditBoth(id int64, action string, actorID int64, clientIP, userAgent, extraJSON string) error {
	return appendAuditJSONEntry(id, models.MediaAuditEntry{Action: action, ActorID: actorID, Ts: time.Now().Unix()})
}
