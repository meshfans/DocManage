package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"doc/database"
	"doc/services/ai"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// AIConfigHandler AI 配置 Handler
type AIConfigHandler struct{}

// NewAIConfigHandler 创建 handler
func NewAIConfigHandler() *AIConfigHandler {
	return &AIConfigHandler{}
}

// ListAIConfigs 获取所有配置
// GET /api/ai-configs
func (h *AIConfigHandler) ListAIConfigs(c *gin.Context) {
	configs, err := database.ListAIConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取配置列表失败"})
		return
	}

	resp := make([]map[string]interface{}, 0, len(configs))
	for _, cfg := range configs {
		item := map[string]interface{}{
			"id":                    cfg.ID,
			"name":                  cfg.Name,
			"model_name":           cfg.ModelName,
			"provider":             cfg.Provider,
			"protocol":             cfg.Protocol,
			"api_key":             maskAPIKey(cfg.APIKey),
			"api_key_has_value":    cfg.APIKeyHasValue,
			"api_base":             cfg.APIBase,
			"default_params":        cfg.DefaultParams,
			"extra":               cfg.Extra,
			"is_default":          cfg.IsDefault == 1,
			"status":              cfg.Status,
			"multimodal_supported": cfg.MultimodalSupported,
			"multimodal_checked_at": cfg.MultimodalCheckedAt,
			"multimodal_check_source": cfg.MultimodalCheckSrc,
			"test_result":          cfg.TestResult,
			"test_result_at":       cfg.TestResultAt,
			"created_at":           cfg.CreatedAt,
			"updated_at":           cfg.UpdatedAt,
		}
		resp = append(resp, item)
	}

	utils.Success(c, gin.H{"list": resp, "total": len(resp)})
}

// GetAIConfig 获取单个配置
// GET /api/ai-configs/:id
func (h *AIConfigHandler) GetAIConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	cfg, err := database.GetAIConfigByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配置失败"})
		return
	}
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": map[string]interface{}{
			"id":                    cfg.ID,
			"name":                  cfg.Name,
			"model_name":           cfg.ModelName,
			"provider":             cfg.Provider,
			"protocol":             cfg.Protocol,
			"api_key":             maskAPIKey(cfg.APIKey),
			"api_key_has_value":    cfg.APIKeyHasValue,
			"api_base":             cfg.APIBase,
			"default_params":        cfg.DefaultParams,
			"extra":               cfg.Extra,
			"is_default":          cfg.IsDefault == 1,
			"status":              cfg.Status,
			"multimodal_supported": cfg.MultimodalSupported,
			"multimodal_checked_at": cfg.MultimodalCheckedAt,
			"multimodal_check_source": cfg.MultimodalCheckSrc,
			"test_result":          cfg.TestResult,
			"test_result_at":       cfg.TestResultAt,
		},
	})
}

// CreateAIConfig 创建配置
// POST /api/ai-configs
func (h *AIConfigHandler) CreateAIConfig(c *gin.Context) {
	var payload database.AIConfigPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	cfg, err := payload.ToAIConfig()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数转换失败"})
		return
	}

	id, err := database.CreateAIConfig(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建配置失败"})
		return
	}

	// 如果设为默认
	if cfg.IsDefault == 1 {
		database.SetDefaultAIConfig(id)
	}

	c.JSON(http.StatusOK, gin.H{"id": id})
}

// UpdateAIConfig 更新配置
// POST /api/ai-configs/:id
func (h *AIConfigHandler) UpdateAIConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	var payload database.AIConfigPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	existing, err := database.GetAIConfigByID(id)
	if err != nil || existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	payload.ID = id
	cfg, err := payload.ToAIConfig()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数转换失败"})
		return
	}

	// 如果没有提供 API Key，保留原有
	if cfg.APIKey == "" {
		cfg.APIKey = existing.APIKey
	}

	if err := database.UpdateAIConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败"})
		return
	}

	// 如果设为默认
	if cfg.IsDefault == 1 {
		database.SetDefaultAIConfig(id)
	}

	c.JSON(http.StatusOK, gin.H{"id": id})
}

// UpdateAIConfigKey 仅更新 API Key
// POST /api/ai-configs/:id/key
func (h *AIConfigHandler) UpdateAIConfigKey(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	var payload struct {
		APIKey string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API Key 不能为空"})
		return
	}

	if err := database.UpdateAIConfigKey(id, payload.APIKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 API Key 失败"})
		return
	}

	// 清除测试结果（Key 变更后需要重新测试）
	database.ClearTestResult(id)
	database.ClearMultimodalCheck(id)

	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeleteAIConfig 删除配置
// POST /api/ai-configs/:id/delete
func (h *AIConfigHandler) DeleteAIConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	if err := database.DeleteAIConfig(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id})
}

// SetDefaultAIConfig 设置默认配置
// POST /api/ai-configs/:id/default
func (h *AIConfigHandler) SetDefaultAIConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	if err := database.SetDefaultAIConfig(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置默认配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id})
}

// TestAIConfig 测试配置连接
// POST /api/ai-configs/:id/test
func (h *AIConfigHandler) TestAIConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	cfg, err := database.GetAIConfigByID(id)
	if err != nil || cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	aiCfg := &ai.Config{
		ID:         cfg.ID,
		Provider:   cfg.Provider,
		Protocol:   cfg.Protocol,
		ModelName: cfg.ModelName,
		APIKey:    cfg.APIKey,
		APIBase:   cfg.APIBase,
		ProxyURL:  cfg.ProxyURL,
	}

	if cfg.DefaultParams != "" {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(cfg.DefaultParams), &params); err == nil {
			aiCfg.DefaultParams = params
		}
	}

	adapter := ai.BuildAdapter(aiCfg)
	result, err := adapter.TestConnection()

	testResult := &ai.FullTestResult{
		Provider: cfg.Provider,
		Model:   cfg.ModelName,
	}

	if err != nil || !result.OK {
		testResult.OK = false
		testResult.LatencyMs = 0
		testResult.Message = result.Message
		if err != nil {
			testResult.Message = err.Error()
		}
	} else {
		testResult.OK = true
		testResult.LatencyMs = result.LatencyMs
		testResult.Message = "连接成功"
	}

	// 保存测试结果（毫秒时间戳，前端会兼容秒/毫秒）
	resultJSON, _ := json.Marshal(testResult)
	database.SetTestResult(id, string(resultJSON))

	// 多模态检测（参考 DocSmart 双保险判断）
	mmResult := ai.TestMultimodal(adapter)
	if mmResult != nil {
		testResult.Multimodal = &ai.MultimodalResult{
			Supported: mmResult.Supported, // 直接传递三态（nil=不确定）
			LatencyMs: mmResult.LatencyMs,
			Message:   mmResult.FullMessage,
		}
		// 三态语义：nil = 检测不确定，不写 DB
		if mmResult.Supported != nil {
			if *mmResult.Supported {
				database.SetMultimodalSupported(id, 1, "auto")
			} else {
				database.SetMultimodalSupported(id, 0, "auto")
			}
		}
	}

	c.JSON(http.StatusOK, testResult)
}

// TestAIConfigInline 实时测试（不保存）
// POST /api/ai-configs/test
func (h *AIConfigHandler) TestAIConfigInline(c *gin.Context) {
	var payload struct {
		Provider      string                 `json:"provider" binding:"required"`
		Protocol     string                 `json:"protocol" binding:"required"`
		ModelName   string                 `json:"model_name" binding:"required"`
		APIKey      string                 `json:"api_key"`
		APIBase     string                 `json:"api_base" binding:"required"`
		DefaultParams map[string]interface{} `json:"default_params"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不完整"})
		return
	}

	aiCfg := &ai.Config{
		Provider:      payload.Provider,
		Protocol:     payload.Protocol,
		ModelName:   payload.ModelName,
		APIKey:      payload.APIKey,
		APIBase:     payload.APIBase,
		DefaultParams: payload.DefaultParams,
	}

	adapter := ai.BuildAdapter(aiCfg)
	result, err := adapter.TestConnection()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":        false,
			"latencyMs": 0,
			"message":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":        result.OK,
		"latencyMs": result.LatencyMs,
		"message":   result.Message,
	})
}

// ToggleMultimodal 手动切换多模态支持
// POST /api/ai-configs/:id/multimodal
func (h *AIConfigHandler) ToggleMultimodal(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	var payload struct {
		Supported int `json:"supported"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	if err := database.SetMultimodalSupported(id, payload.Supported, "manual"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	cfg, _ := database.GetAIConfigByID(id)
	checkedAt := int64(0)
	if cfg != nil && cfg.MultimodalCheckedAt != nil {
		checkedAt = *cfg.MultimodalCheckedAt
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                      id,
		"multimodal_supported":    payload.Supported,
		"multimodal_checked_at":  checkedAt,
		"multimodal_check_source": "manual",
	})
}

// GetAIMeta 获取支持的协议列表
// GET /api/ai-meta
func (h *AIConfigHandler) GetAIMeta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"protocols": []map[string]string{
			{"value": "openai_chat", "label": "OpenAI 兼容"},
			{"value": "anthropic_messages", "label": "Anthropic 原生"},
			{"value": "google_generative", "label": "Google Generative AI"},
			{"value": "ollama_chat", "label": "Ollama"},
		},
	})
}

// detectMultimodal 检测多模态支持
// 已迁移至 ai.TestMultimodal（参考 DocSmart v1.8.11+ 双保险判断）

// maskAPIKey 脱敏 API Key
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// ptrInt int 指针
func ptrInt(v int) *int {
	return &v
}
