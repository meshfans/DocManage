package database

import (
	"encoding/json"
	"time"
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
		DefaultJSON string
		ExtraJSON   string
		IsDefault   int
	}{
		{
			Name:      "模小范 · meshfans-v1（官方 OpenAI 兼容）",
			Provider:  "meshfans",
			Protocol:  "openai_chat",
			ModelName: "meshfans-v1",
			APIBase:   "https://aiv1.meshfans.com/v1",
			DefaultJSON: mustMarshalJSON(map[string]interface{}{
				"temperature": 0.7,
				"max_tokens":  2048,
				"top_p":       0.9,
			}),
			ExtraJSON: mustMarshalJSON(map[string]interface{}{
				"free":      false,
				"authFree":  false,
				"signupUrl": "https://aiv1.meshfans.com/",
				"note":      "官方 OpenAI 兼容 API；向 meshfans 官方申请 API Key 填入即可使用。",
			}),
			IsDefault: 1,
		},
		{
			Name:      "智谱 · GLM-4.7-Flash（永久免费）",
			Provider:  "zhipu",
			Protocol:  "anthropic_messages",
			ModelName: "glm-4.7-flash",
			APIBase:   "https://open.bigmodel.cn/api/anthropic",
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
			IsDefault: 0,
		},
		{
			Name:      "硅基流动 · Qwen2.5-7B-Instruct",
			Provider:  "siliconflow",
			Protocol:  "openai_chat",
			ModelName: "Qwen/Qwen2.5-7B-Instruct",
			APIBase:   "https://api.siliconflow.cn/v1",
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
			IsDefault: 0,
		},
		{
			Name:      "Ollama · qwen3:1.7b（本地推理）",
			Provider:  "ollama",
			Protocol:  "ollama_chat",
			ModelName: "qwen3:1.7b",
			APIBase:   "http://127.0.0.1:11434",
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
			IsDefault: 0,
		},
	}

	for _, s := range seeds {
		_, err := DB.Exec(`
			INSERT INTO ai_config (name, provider, protocol, model_name, api_key, api_base,
			                      default_params, extra, is_default, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, 1, ?, ?)
		`, s.Name, s.Provider, s.Protocol, s.ModelName, s.APIBase,
			s.DefaultJSON, s.ExtraJSON, s.IsDefault, now, now)
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
