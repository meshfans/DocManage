package database

import (
	"encoding/json"
	"fmt"
	"time"

	"doc/utils"
)

// SeedAIConfigs 插入 AI 配置种子数据（仅当表为空时）
func SeedAIConfigs() error {
	// 检查是否已有数据
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM ai_config WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // 已有数据，跳过
	}

	now := time.Now().Unix()

	seeds := []struct {
		Name        string
		Provider    string
		Protocol    string
		ModelName   string
		APIBase     string
		APIPath     string
		DefaultJSON string
		ExtraJSON   string
		IsDefault   int
		Multimodal  int
		MMCheckedAt int64
		MMSrc       string
	}{
		{
			Name:      "模小范 · meshfans-v1（官方）",
			Provider:  "meshfans",
			Protocol:  "openai_chat",
			ModelName: "meshfans-v1",
			APIBase:   "https://lmp.meshfans.com/api/v1",
			APIPath:   "/chat/completions",
			DefaultJSON: mustMarshalJSON(map[string]interface{}{
				"temperature": 0.7,
				"max_tokens":  2048,
				"top_p":       0.9,
			}),
			ExtraJSON: mustMarshalJSON(map[string]interface{}{
				"free":      false,
				"authFree":  false,
				"signupUrl": "https://www.meshfans.com/",
				"note":      "无需手填 API Key；自动复用全局 license_key (LICENSE_KEY env 或 config.license.license_key)。若需自建 key，在 AI 配置 api_key 字段填写即可覆盖。",
			}),
			IsDefault:   1,
			Multimodal:  0,
			MMCheckedAt: 0,
			MMSrc:       "",
		},
		{
			Name:      "智谱 · GLM-4.7-Flash（永久免费）",
			Provider:  "zhipu",
			Protocol:  "anthropic_messages",
			ModelName: "glm-4.7-flash",
			APIBase:   "https://open.bigmodel.cn/api/anthropic",
			APIPath:   "/messages",
			DefaultJSON: mustMarshalJSON(map[string]interface{}{
				"temperature": 0.7,
				"max_tokens":  2048,
				"top_p":       0.9,
			}),
			ExtraJSON: mustMarshalJSON(map[string]interface{}{
				"free":      true,
				"authFree":  false,
				"signupUrl": "https://bigmodel.cn/",
				"note":      "注册智谱账号 → 控制台拿 API Key 填入；使用 Anthropic Messages 原生协议",
			}),
			IsDefault:   0,
			Multimodal:  0,
			MMCheckedAt: now,
			MMSrc:       "preset",
		},
		{
			Name:      "硅基流动 · Qwen2.5-7B-Instruct",
			Provider:  "siliconflow",
			Protocol:  "openai_chat",
			ModelName: "Qwen/Qwen2.5-7B-Instruct",
			APIBase:   "https://api.siliconflow.cn/v1",
			APIPath:   "/chat/completions",
			DefaultJSON: mustMarshalJSON(map[string]interface{}{
				"temperature": 0.7,
				"max_tokens":  2048,
				"top_p":       0.9,
			}),
			ExtraJSON: mustMarshalJSON(map[string]interface{}{
				"free":      true,
				"authFree":  false,
				"signupUrl": "https://siliconflow.cn/",
				"note":      "注册送免费额度；适合大批量文本生成",
			}),
			IsDefault:   0,
			Multimodal:  0,
			MMCheckedAt: 0,
			MMSrc:       "",
		},
		{
			Name:      "Ollama · qwen3:1.7b（本地推理）",
			Provider:  "ollama",
			Protocol:  "ollama_chat",
			ModelName: "qwen3:1.7b",
			APIBase:   "http://127.0.0.1:11434",
			APIPath:   "/api/chat",
			DefaultJSON: mustMarshalJSON(map[string]interface{}{
				"temperature": 0.7,
				"max_tokens":  2048,
				"top_p":       0.9,
			}),
			ExtraJSON: mustMarshalJSON(map[string]interface{}{
				"free":     true,
				"authFree": true,
				"note":     "需先启动本地 Ollama 服务（ollama serve），再 `ollama pull qwen3:1.7b`",
			}),
			IsDefault:   0,
			Multimodal:  0,
			MMCheckedAt: now,
			MMSrc:       "preset",
		},
	}

	for _, s := range seeds {
		_, err := DB.Exec(`
			INSERT INTO ai_config (name, provider, protocol, model_name, api_key, api_base, api_path,
			                      default_params, extra, is_default, status, created_at, updated_at,
			                      multimodal_supported, multimodal_checked_at, multimodal_check_source)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)
		`, s.Name, s.Provider, s.Protocol, s.ModelName, s.APIBase, s.APIPath,
			s.DefaultJSON, s.ExtraJSON, s.IsDefault, now, now,
			s.Multimodal, s.MMCheckedAt, s.MMSrc)
		if err != nil {
			return err
		}
	}

	return nil
}

func mustMarshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// MigrateAIConfigs 升级历史 ai_config 行的字段（idempotent，重复执行无副作用）。
//
// 2026-10-02 M-4 修复：meshfans 官方 APIBase 已从
// `https://aiv1.meshfans.com/v1` 切换到 `https://lmp.meshfans.com/api/v1`。
// SeedAIConfigs 只在表为空时跑，已有部署的 meshfans 行不会被自动刷新——
// 直接后果是 /api/llm/chat 仍然打到旧域名 → 401。
//
// 本函数每次启动跑一次（轻量 UPDATE），用 `provider + api_base` 精确匹配
// 旧值，避免误改用户在 AI 配置页自填的同 provider 其他 base。
//
// 注意：
//   - 仅改 APIBase，不动 api_key（用户已自填的 key 保留）
//   - 不改 is_default（避免切换默认行）
//   - 不改 multimodal 等无关字段
func MigrateAIConfigs() error {
	// 旧 meshfans base 列表 —— 后续如再次切换，只需在此追加旧值
	oldMeshfansBases := []string{
		"https://aiv1.meshfans.com/v1",
	}

	now := time.Now().Unix()
	for _, oldBase := range oldMeshfansBases {
		res, err := DB.Exec(`
			UPDATE ai_config
			SET api_base = ?,
			    updated_at = ?
			WHERE provider = 'meshfans'
			  AND api_base = ?
			  AND deleted_at IS NULL
		`, "https://lmp.meshfans.com/api/v1", now, oldBase)
		if err != nil {
			return fmt.Errorf("迁移 meshfans api_base (old=%s) 失败: %w", oldBase, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			utils.Info("[ai-config migration] meshfans api_base: %s → https://lmp.meshfans.com/api/v1（更新 %d 行）",
				oldBase, n)
		}
	}
	return nil
}
