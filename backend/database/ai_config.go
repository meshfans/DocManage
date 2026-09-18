package database

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// AIConfig AI 配置表
type AIConfig struct {
	ID                  int64           `json:"id"`
	Name                string          `json:"name"`
	Provider            string          `json:"provider"`
	Protocol            string          `json:"protocol"`
	ModelName           string          `json:"model_name"`
	APIKey              string          `json:"api_key"`
	APIKeyHasValue      bool           `json:"api_key_has_value"`
	APIBase             string          `json:"api_base"`
	ProxyURL            string          `json:"proxy_url,omitempty"`
	DefaultParams       string          `json:"default_params,omitempty"`
	Extra               string          `json:"extra,omitempty"`
	IsDefault           int             `json:"is_default"`
	Status              int             `json:"status"`
	MultimodalSupported *int           `json:"multimodal_supported,omitempty"`
	MultimodalCheckedAt *int64         `json:"multimodal_checked_at,omitempty"`
	MultimodalCheckSrc  string         `json:"multimodal_check_source,omitempty"`
	TestResult          string          `json:"test_result,omitempty"`
	TestResultAt        *int64         `json:"test_result_at,omitempty"`
	CreatedAt           int64           `json:"created_at"`
	UpdatedAt           int64           `json:"updated_at"`
	DeletedAt           *int64         `json:"deleted_at,omitempty"`
}

// ListAIConfigs 获取所有启用的 AI 配置
func ListAIConfigs() ([]AIConfig, error) {
	rows, err := DB.Query(`
		SELECT id, name, provider, protocol, model_name, api_key, api_base, proxy_url,
		       default_params, extra, is_default, status, multimodal_supported,
		       multimodal_checked_at, multimodal_check_source, test_result,
		       test_result_at, created_at, updated_at, deleted_at
		FROM ai_config
		WHERE deleted_at IS NULL
		ORDER BY is_default DESC, updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []AIConfig
	for rows.Next() {
		var c AIConfig
		var apiKey, proxyURL, defaultParams, extra, testResult sql.NullString
		var multimodalSupported sql.NullInt64
		var multimodalCheckedAt, testResultAt sql.NullInt64
		var multimodalCheckSrc sql.NullString
		var deletedAt sql.NullInt64

		err := rows.Scan(
			&c.ID, &c.Name, &c.Provider, &c.Protocol, &c.ModelName,
			&apiKey, &c.APIBase, &proxyURL,
			&defaultParams, &extra, &c.IsDefault, &c.Status,
			&multimodalSupported, &multimodalCheckedAt, &multimodalCheckSrc,
			&testResult, &testResultAt, &c.CreatedAt, &c.UpdatedAt, &deletedAt,
		)
		if err != nil {
			return nil, err
		}

		c.APIKey = apiKey.String
		c.APIKeyHasValue = apiKey.String != ""
		c.ProxyURL = proxyURL.String
		c.DefaultParams = defaultParams.String
		c.Extra = extra.String
		c.TestResult = testResult.String

		if multimodalSupported.Valid {
			v := int(multimodalSupported.Int64)
			c.MultimodalSupported = &v
		}
		if multimodalCheckedAt.Valid {
			c.MultimodalCheckedAt = &multimodalCheckedAt.Int64
		}
		if multimodalCheckSrc.Valid {
			c.MultimodalCheckSrc = multimodalCheckSrc.String
		}
		if testResultAt.Valid {
			c.TestResultAt = &testResultAt.Int64
		}
		if deletedAt.Valid {
			c.DeletedAt = &deletedAt.Int64
		}

		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// GetAIConfigByID 根据 ID 获取单个配置
func GetAIConfigByID(id int64) (*AIConfig, error) {
	var c AIConfig
	var apiKey, proxyURL, defaultParams, extra, testResult sql.NullString
	var multimodalSupported sql.NullInt64
	var multimodalCheckedAt, testResultAt sql.NullInt64
	var multimodalCheckSrc sql.NullString
	var deletedAt sql.NullInt64

	err := DB.QueryRow(`
		SELECT id, name, provider, protocol, model_name, api_key, api_base, proxy_url,
		       default_params, extra, is_default, status, multimodal_supported,
		       multimodal_checked_at, multimodal_check_source, test_result,
		       test_result_at, created_at, updated_at, deleted_at
		FROM ai_config
		WHERE id = ? AND deleted_at IS NULL
	`, id).Scan(
		&c.ID, &c.Name, &c.Provider, &c.Protocol, &c.ModelName,
		&apiKey, &c.APIBase, &proxyURL,
		&defaultParams, &extra, &c.IsDefault, &c.Status,
		&multimodalSupported, &multimodalCheckedAt, &multimodalCheckSrc,
		&testResult, &testResultAt, &c.CreatedAt, &c.UpdatedAt, &deletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	c.APIKey = apiKey.String
	c.APIKeyHasValue = apiKey.String != ""
	c.ProxyURL = proxyURL.String
	c.DefaultParams = defaultParams.String
	c.Extra = extra.String
	c.TestResult = testResult.String

	if multimodalSupported.Valid {
		v := int(multimodalSupported.Int64)
		c.MultimodalSupported = &v
	}
	if multimodalCheckedAt.Valid {
		c.MultimodalCheckedAt = &multimodalCheckedAt.Int64
	}
	if multimodalCheckSrc.Valid {
		c.MultimodalCheckSrc = multimodalCheckSrc.String
	}
	if testResultAt.Valid {
		c.TestResultAt = &testResultAt.Int64
	}
	if deletedAt.Valid {
		c.DeletedAt = &deletedAt.Int64
	}

	return &c, nil
}

// GetDefaultAIConfig 获取默认 AI 配置
func GetDefaultAIConfig() (*AIConfig, error) {
	var c AIConfig
	var apiKey, proxyURL, defaultParams, extra, testResult sql.NullString
	var multimodalSupported sql.NullInt64
	var multimodalCheckedAt, testResultAt sql.NullInt64
	var multimodalCheckSrc sql.NullString
	var deletedAt sql.NullInt64

	err := DB.QueryRow(`
		SELECT id, name, provider, protocol, model_name, api_key, api_base, proxy_url,
		       default_params, extra, is_default, status, multimodal_supported,
		       multimodal_checked_at, multimodal_check_source, test_result,
		       test_result_at, created_at, updated_at, deleted_at
		FROM ai_config
		WHERE is_default = 1 AND deleted_at IS NULL
		LIMIT 1
	`).Scan(
		&c.ID, &c.Name, &c.Provider, &c.Protocol, &c.ModelName,
		&apiKey, &c.APIBase, &proxyURL,
		&defaultParams, &extra, &c.IsDefault, &c.Status,
		&multimodalSupported, &multimodalCheckedAt, &multimodalCheckSrc,
		&testResult, &testResultAt, &c.CreatedAt, &c.UpdatedAt, &deletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	c.APIKey = apiKey.String
	c.APIKeyHasValue = apiKey.String != ""
	c.ProxyURL = proxyURL.String
	c.DefaultParams = defaultParams.String
	c.Extra = extra.String
	c.TestResult = testResult.String

	if multimodalSupported.Valid {
		v := int(multimodalSupported.Int64)
		c.MultimodalSupported = &v
	}
	if multimodalCheckedAt.Valid {
		c.MultimodalCheckedAt = &multimodalCheckedAt.Int64
	}
	if multimodalCheckSrc.Valid {
		c.MultimodalCheckSrc = multimodalCheckSrc.String
	}
	if testResultAt.Valid {
		c.TestResultAt = &testResultAt.Int64
	}
	if deletedAt.Valid {
		c.DeletedAt = &deletedAt.Int64
	}

	return &c, nil
}

// CreateAIConfig 创建 AI 配置
func CreateAIConfig(c *AIConfig) (int64, error) {
	now := time.Now().Unix()
	result, err := DB.Exec(`
		INSERT INTO ai_config (name, provider, protocol, model_name, api_key, api_base, proxy_url,
		                      default_params, extra, is_default, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.Name, c.Provider, c.Protocol, c.ModelName, c.APIKey, c.APIBase, c.ProxyURL,
		c.DefaultParams, c.Extra, c.IsDefault, 1, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateAIConfig 更新 AI 配置
func UpdateAIConfig(c *AIConfig) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`
		UPDATE ai_config SET
			name = ?, provider = ?, protocol = ?, model_name = ?,
			api_base = ?, proxy_url = ?, default_params = ?, extra = ?,
			is_default = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, c.Name, c.Provider, c.Protocol, c.ModelName,
		c.APIBase, c.ProxyURL, c.DefaultParams, c.Extra,
		c.IsDefault, now, c.ID)
	return err
}

// UpdateAIConfigKey 仅更新 API Key
func UpdateAIConfigKey(id int64, apiKey string) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`UPDATE ai_config SET api_key = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, apiKey, now, id)
	return err
}

// DeleteAIConfig 软删除 AI 配置
func DeleteAIConfig(id int64) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`UPDATE ai_config SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

// SetDefaultAIConfig 设置默认配置
func SetDefaultAIConfig(id int64) error {
	now := time.Now().Unix()
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 清除其他默认
	if _, err := tx.Exec(`UPDATE ai_config SET is_default = 0, updated_at = ? WHERE is_default = 1 AND deleted_at IS NULL`, now); err != nil {
		return err
	}
	// 设置新的默认
	if _, err := tx.Exec(`UPDATE ai_config SET is_default = 1, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetMultimodalSupported 设置多模态支持状态
//   - multimodal_checked_at 用毫秒时间戳（与前端 Date.now() 对齐）
func SetMultimodalSupported(id int64, supported int, source string) error {
	nowMs := time.Now().UnixMilli()
	now := time.Now().Unix()
	_, err := DB.Exec(`
		UPDATE ai_config SET
			multimodal_supported = ?,
			multimodal_checked_at = ?,
			multimodal_check_source = ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, supported, nowMs, source, now, id)
	return err
}

// ClearMultimodalCheck 清除多模态检测结果
func ClearMultimodalCheck(id int64) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`
		UPDATE ai_config SET
			multimodal_supported = NULL,
			multimodal_checked_at = NULL,
			multimodal_check_source = NULL,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, id)
	return err
}

// SetTestResult 设置测试结果
//   - test_result_at 用毫秒时间戳（与前端 Date.now() 对齐）
func SetTestResult(id int64, result string) error {
	nowMs := time.Now().UnixMilli()
	now := time.Now().Unix()
	_, err := DB.Exec(`
		UPDATE ai_config SET test_result = ?, test_result_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, result, nowMs, now, id)
	return err
}

// ClearTestResult 清除测试结果
func ClearTestResult(id int64) error {
	now := time.Now().Unix()
	_, err := DB.Exec(`
		UPDATE ai_config SET test_result = NULL, test_result_at = NULL, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, id)
	return err
}

// AIConfigPayload 创建/更新时的输入结构
type AIConfigPayload struct {
	ID             int64                  `json:"id,omitempty"`
	Name           string                 `json:"name"`
	Provider       string                 `json:"provider"`
	Protocol       string                 `json:"protocol"`
	ModelName      string                 `json:"model_name"`
	APIKey         string                 `json:"api_key,omitempty"`
	APIBase        string                 `json:"api_base"`
	ProxyURL       string                 `json:"proxy_url,omitempty"`
	DefaultParams  map[string]interface{} `json:"default_params,omitempty"`
	Extra          map[string]interface{} `json:"extra,omitempty"`
	IsDefault      bool                   `json:"is_default"`
}

// ToAIConfig 将 payload 转换为 AIConfig
func (p *AIConfigPayload) ToAIConfig() (*AIConfig, error) {
	defaultParamsJSON := ""
	if len(p.DefaultParams) > 0 {
		data, err := json.Marshal(p.DefaultParams)
		if err != nil {
			return nil, err
		}
		defaultParamsJSON = string(data)
	}

	extraJSON := ""
	if len(p.Extra) > 0 {
		data, err := json.Marshal(p.Extra)
		if err != nil {
			return nil, err
		}
		extraJSON = string(data)
	}

	isDefault := 0
	if p.IsDefault {
		isDefault = 1
	}

	// 处理 name 为空的情况
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = p.Provider + " · " + p.ModelName
	}

	return &AIConfig{
		ID:            p.ID,
		Name:          name,
		Provider:      p.Provider,
		Protocol:      p.Protocol,
		ModelName:     p.ModelName,
		APIKey:        p.APIKey,
		APIBase:       p.APIBase,
		ProxyURL:      p.ProxyURL,
		DefaultParams: defaultParamsJSON,
		Extra:         extraJSON,
		IsDefault:     isDefault,
	}, nil
}
