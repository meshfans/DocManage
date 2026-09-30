package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"doc/database"
	"doc/services/ai"
	"doc/services/llm"
	"doc/services/llm/models"
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin 双保险（APIGate 未配置 ai-config:* perm seed）。
func (h *AIConfigHandler) ListAIConfigs(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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
			"api_path":             cfg.APIPath,
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate（含 API key 脱敏返回值，仍属敏感配置）。
func (h *AIConfigHandler) GetAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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
			"api_path":             cfg.APIPath,
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) CreateAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 2026-09-30 对齐 DocCRM：创建后立即刷新内存 dispatcher。
	reloadLLMService()

	utils.Success(c, gin.H{"id": id})
}

// UpdateAIConfig 更新配置
// POST /api/ai-configs/:id
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) UpdateAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 编辑时若替换了 API Key，清掉旧的测试 / 多模态结论（与 UpdateAIConfigKey 一致）
	if cfg.APIKey != "" && cfg.APIKey != existing.APIKey {
		database.ClearTestResult(id)
		database.ClearMultimodalCheck(id)
	}

	// 如果设为默认
	if cfg.IsDefault == 1 {
		database.SetDefaultAIConfig(id)
	}

	// 2026-09-30 对齐 DocCRM：更新后立即刷新内存 dispatcher。
	reloadLLMService()

	utils.Success(c, gin.H{"id": id})
}

// UpdateAIConfigKey 仅更新 API Key
// POST /api/ai-configs/:id/key
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate（API Key 是最高敏感凭据）。
func (h *AIConfigHandler) UpdateAIConfigKey(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 2026-09-30 对齐 DocCRM：Key 变更后立即刷新内存 dispatcher。
	reloadLLMService()

	utils.Success(c, gin.H{"id": id})
}

// DeleteAIConfig 删除配置
// POST /api/ai-configs/:id/delete
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) DeleteAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 2026-09-30 对齐 DocCRM：删除后重新选默认模型。
	reloadLLMService()

	utils.Success(c, gin.H{"id": id})
}

// SetDefaultAIConfig 设置默认配置
// POST /api/ai-configs/:id/default
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) SetDefaultAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 2026-09-30 对齐 DocCRM：切默认后立即刷新内存 dispatcher。
	reloadLLMService()

	utils.Success(c, gin.H{"id": id})
}

// TestAIConfig 测试配置连接
// POST /api/ai-configs/:id/test
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate（会真打外部 LLM provider，耗资源/可能产生费用）。
func (h *AIConfigHandler) TestAIConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

	// 2026-09-30 对齐 DocCRM：测试链路收敛到 models.NewLLMClientModel。
	// 原先走 services/ai.BuildAdapter，与实际对话的 llmclient 是两套独立实现，
	// 导致「api_path 认测试不认运行时」的分叉。统一后测试通过 = 运行时可用。
	model := models.NewLLMClientModel(&models.ModelConfig{
		Provider:  cfg.Provider,
		Protocol:  cfg.Protocol,
		ModelName: cfg.ModelName,
		APIKey:    cfg.APIKey,
		APIBase:   cfg.APIBase,
		APIPath:   cfg.APIPath,
		ProxyURL:  cfg.ProxyURL,
	})
	result, err := model.TestConnection()

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
	// 2026-09-30 对齐 DocCRM：探测走与实际对话同一条 llmclient 链路。
	mmResult := ai.TestMultimodal(model)
	if mmResult != nil {
		testResult.Multimodal = &ai.MultimodalResult{
			Supported: mmResult.Supported, // 直接传递三态（nil=不确定）
			LatencyMs: mmResult.LatencyMs,
			Message:   mmResult.Message,
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) TestAIConfigInline(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var payload struct {
		Provider    string                 `json:"provider" binding:"required"`
		Protocol    string                 `json:"protocol" binding:"required"`
		ModelName   string                 `json:"model_name" binding:"required"`
		APIKey      string                 `json:"api_key"`
		APIBase     string                 `json:"api_base" binding:"required"`
		APIPath     string                 `json:"api_path"`
		ProxyURL    string                 `json:"proxy_url"`
		DefaultParams map[string]interface{} `json:"default_params"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不完整"})
		return
	}

	// 2026-09-30 对齐 DocCRM：内联测试同样收敛到 models.NewLLMClientModel，
	// 并补 ProxyURL（DocCRM 侧此处漏传，DocManageTrail 保留更完整行为）。
	model := models.NewLLMClientModel(&models.ModelConfig{
		Provider:  payload.Provider,
		Protocol:  payload.Protocol,
		ModelName: payload.ModelName,
		APIKey:    payload.APIKey,
		APIBase:   payload.APIBase,
		APIPath:   payload.APIPath,
		ProxyURL:  payload.ProxyURL,
	})
	result, err := model.TestConnection()
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) ToggleMultimodal(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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
//
// 2026-09-30 对齐 DocCRM：补 RequireAdmin gate。
func (h *AIConfigHandler) GetAIMeta(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
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

// reloadLLMService 触发 LLM Service 从 DB 重新加载默认配置。
//
// 2026-09-30 对齐 DocCRM 移植。失败仅记录 warn，不影响调用方返回成功——
// 配置写入 DB 已成功，只是内存中的 dispatcher 暂时未刷新，下次启动 / 再次写入会自愈。
func reloadLLMService() {
	svc := llm.GetService()
	if svc == nil {
		utils.Warn("[LLM] Reload 跳过：全局 Service 未初始化（通常意味着首次启动时无默认配置）")
		return
	}
	if err := svc.Reload(); err != nil {
		utils.Warn("[LLM] Reload 失败: %v", err)
	}
}

// ptrInt int 指针
func ptrInt(v int) *int {
	return &v
}
