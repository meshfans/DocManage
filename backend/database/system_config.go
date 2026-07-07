package database

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"doc/utils"
)

// SystemConfig 是 system_config 表的 Go 表示。
type SystemConfig struct {
	ID          int64  `json:"id"`
	ConfigKey   string `json:"config_key"`
	ConfigValue string `json:"config_value"`
	Description string `json:"description"`
	UpdatedAt   int64  `json:"updated_at"`
}

// ListSystemConfigs 列出全部 DB 已存的配置（不含代码兜底）。
// 用于后台审计/全量导出。
// 注：不再使用 SQL ORDER BY（避免与 ListAllConfigMeta 双重排序）；
// 调用方如需排序，自行 sort（ListAllConfigMeta 已内置）。
func ListSystemConfigs() ([]SystemConfig, error) {
	rows, err := DB.Query(`
		SELECT id, config_key, config_value, description, updated_at
		FROM system_config
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SystemConfig
	for rows.Next() {
		var s SystemConfig
		if err := rows.Scan(&s.ID, &s.ConfigKey, &s.ConfigValue, &s.Description, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSystemConfigByKey 取单个 key 的 DB 值（空字符串表示"未设置"）。
// 不 fallback：调用方根据需要决定是否回退到 DEFAULT_CONFIGS。
func GetSystemConfigByKey(key string) (string, error) {
	var value string
	err := DB.QueryRow(`SELECT config_value FROM system_config WHERE config_key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// ErrInvalidConfigKey 未知 config key 时返回的错误。
// 防止 admin 误输入或前端 bug 把任意 key 写入 system_config 表。
var ErrInvalidConfigKey = errors.New("无效的 config key（不在 DEFAULT_CONFIGS 中）")

// SetSystemConfig 写入/更新配置（upsert）。
// 安全：
//  1. key 必须在 DEFAULT_CONFIGS 白名单中（防止污染 DB）
//  2. value 自动 TrimSpace（防止 " admin " 导致严格比较失败）
//     + 手动清掉首尾的全角空格 U+3000（strings.TrimSpace 不处理）
//  3. description 优先从 DEFAULT_CONFIGS 拿（与代码兜底同步）
func SetSystemConfig(key, value string) error {
	// 1. 白名单校验
	if _, ok := DEFAULT_CONFIGS[key]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidConfigKey, key)
	}
	// 2. trim（含全角空格 U+3000 + ASCII whitespace）
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " 　\t\n\r")
	// 3. description 从代码兜底
	desc := DEFAULT_CONFIGS[key].Description
	now := time.Now().Unix()
	_, err := DB.Exec(`
		INSERT INTO system_config (config_key, config_value, description, updated_at)
			VALUES (?, ?, ?, ?)
		ON CONFLICT(config_key) DO UPDATE SET
			config_value = excluded.config_value,
			updated_at  = excluded.updated_at
	`, key, value, desc, now)
	return err
}

// DeleteSystemConfig 删除某 key（回退到代码兜底）。
// 返回受影响的行数（0 表示 key 不存在；>0 表示成功删除）。
func DeleteSystemConfig(key string) (int64, error) {
	res, err := DB.Exec(`DELETE FROM system_config WHERE config_key = ?`, key)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetSystemConfigAllWithDefaults 合并 DB + 代码兜底，返回全部 key 的最终值（key-value map）。
// 用于前端"业务配置"页 + GetPublicConfigs。
//
// 注：DB 出错（连接失败、SQL 错）只 warn，不返回 err（前端优先用 defaults）。
// 这是 by-design：业务配置的核心价值是"前端能读"，DB 是覆盖层，DB 故障
// 应降级到 defaults，而不是让整个接口 500。
func GetSystemConfigAllWithDefaults() map[string]string {
	defaults := AllConfigDefaults()
	out := make(map[string]string, len(defaults))
	for k, v := range defaults {
		out[k] = v.Value
	}
	// DB 覆盖
	rows, err := DB.Query(`SELECT config_key, config_value FROM system_config`)
	if err != nil {
		utils.Warn("[system_config] DB 查询失败，降级到 defaults: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil && v != "" {
			out[k] = v
		}
	}
	return out
}

// ListAllConfigMeta 列出全部 ConfigMeta，按 key 升序（A-Z）排序后返回。
// 这是 ListConfigs handler 调用的统一入口；保证前端"业务配置"tab 展示顺序稳定。
//
// 行为：
//  1. 来源是 DEFAULT_CONFIGS（代码兜底）
//  2. DB override 合并：值非空 → 覆盖；值与默认相同 → 不标 IsOverridden
//  3. DB-only key（不在 defaults）→ 静默丢弃 + warn log（防止脏数据长期隐藏）
func ListAllConfigMeta() ([]ConfigMeta, error) {
	defaults := AllConfigDefaults()
	overrides, err := ListSystemConfigs()
	if err != nil {
		return nil, err
	}
	overrideMap := make(map[string]SystemConfig, len(overrides))
	for _, o := range overrides {
		overrideMap[o.ConfigKey] = o
		// DB-only key 警告：DB 有但 DEFAULT_CONFIGS 没有（说明历史残留或误入）
		if _, isKnown := defaults[o.ConfigKey]; !isKnown {
			utils.Warn("[system_config] DB-only key %q 不在 DEFAULT_CONFIGS 中，已被忽略。请检查是否有遗留数据或前端误写。", o.ConfigKey)
		}
	}

	// 按 key 升序排（核心修复：map 迭代顺序不固定）
	keys := make([]string, 0, len(defaults))
	for k := range defaults {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]ConfigMeta, 0, len(keys))
	for _, k := range keys {
		def := defaults[k]
		row := ConfigMeta{
			Key:          k,
			Value:        def.Value,
			DefaultVal:   def.Value,
			Description:  def.Description,
			Category:     def.Category,
			IsOverridden: false,
		}
		if o, ok := overrideMap[k]; ok && o.ConfigValue != "" {
			row.Value = o.ConfigValue
			// 仅当 override 值 != 默认值时标 IsOverridden（避免"覆盖默认值"被误标）
			row.IsOverridden = (o.ConfigValue != def.Value)
		}
		out = append(out, row)
	}
	return out, nil
}

// ConfigMeta 是单条配置项的元信息结构（key / value / 默认值 / 描述 / 分类 / 是否被覆盖）。
type ConfigMeta struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	DefaultVal   string `json:"default_value"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	IsOverridden bool   `json:"is_overridden"`
}

// GetAllSystemConfig 列出 DB 中全部（key-value map），不含 DEFAULT_CONFIGS。
// 兼容旧 handler：system.go 在用。
func GetAllSystemConfig() (map[string]string, error) {
	rows, err := DB.Query(`SELECT config_key, config_value FROM system_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		config[key] = value
	}
	return config, rows.Err()
}

// SetMultipleSystemConfig 批量 upsert（事务保证）。
// 兼容旧 handler：system.go 在用。
//
// 2026-07-04 修复：与 SetSystemConfig 对齐加白名单守卫。
// 历史：system.go 的 SetConfig 用 camelCase key（如 systemName / companyName）写入，
//
//	而 DEFAULT_CONFIGS 是 snake_case（company_name 等）。旧实现无白名单校验，
//	导致 camelCase key 落到 system_config 表后，每次 ListAllConfigMeta 都触发
//	"DB-only key" warn（system_config.go:154）。本次与 SetSystemConfig 对齐：
//	不在 DEFAULT_CONFIGS 白名单的 key → 返回 ErrInvalidConfigKey（事务回滚）。
//	旧 camelCase 残留数据保留不动（admin 可手动 SQL 清理；不影响业务读取）。
func SetMultipleSystemConfig(configs map[string]string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO system_config (config_key, config_value, updated_at)
		VALUES (?, ?, strftime('%s', 'now'))
		ON CONFLICT(config_key) DO UPDATE SET config_value = ?, updated_at = strftime('%s', 'now')
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range configs {
		// 白名单守卫（与 SetSystemConfig 对齐）
		if _, ok := DEFAULT_CONFIGS[key]; !ok {
			return fmt.Errorf("%w: %q（请使用 DEFAULT_CONFIGS 中已注册的 key）", ErrInvalidConfigKey, key)
		}
		_, err = stmt.Exec(key, value, value)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
