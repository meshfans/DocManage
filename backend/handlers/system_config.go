package handlers

import (
	"doc/database"
	"doc/utils"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SystemConfigHandler 业务配置 system_config 表的 HTTP API。
// 区别于 system.go（管 backend config.json 文件 + license），本 handler 管 DB 业务配置。
type SystemConfigHandler struct {
	// publicConfigsCache 缓存 GetPublicConfigs 结果，避免每次请求都查 DB。
	// 业务配置变更频率极低（admin 偶尔改一次），5 秒 TTL 是合理折衷。
	publicCacheMu   sync.RWMutex
	publicCache     map[string]string
	publicCacheTime time.Time
}

// 缓存 TTL：业务配置变更频率低，5 秒足够。
const publicCacheTTL = 5 * time.Second

// NewSystemConfigHandler 构造器。
func NewSystemConfigHandler() *SystemConfigHandler {
	return &SystemConfigHandler{}
}

// ---------- 公开端点 ----------

// GetPublicConfigs 返回 DEFAULT_CONFIGS + DB 覆盖 的合并值。
// 不含敏感信息（system_config 都是业务级、可读元数据）。
// 加 5s 进程内缓存：admin 改完配置后最多 5s 生效，避免每请求查 DB。
//
// TOCTOU 优化：用 singleflight 防止并发 miss 时 2 个 goroutine 同时查 DB。
// 这里简化为 "double-check under write lock"：第一次读锁检查 → 释放读锁 →
// 拿写锁 → 再检查一次 → 没缓存就查 DB。读路径是快路径（只读锁），写路径是慢路径。
func (h *SystemConfigHandler) GetPublicConfigs(c *gin.Context) {
	// 快路径：读锁下检查缓存
	h.publicCacheMu.RLock()
	if h.publicCache != nil && time.Since(h.publicCacheTime) < publicCacheTTL {
		cached := h.publicCache
		h.publicCacheMu.RUnlock()
		utils.Success(c, cached)
		return
	}
	h.publicCacheMu.RUnlock()

	// 慢路径：写锁下 double-check 防止并发 miss 重复查 DB
	h.publicCacheMu.Lock()
	defer h.publicCacheMu.Unlock()
	if h.publicCache != nil && time.Since(h.publicCacheTime) < publicCacheTTL {
		// 慢路径二次检查：另一 goroutine 已经填好缓存
		utils.Success(c, h.publicCache)
		return
	}
	// 缓存未命中或过期：查 DB 并写回
	configs := database.GetSystemConfigAllWithDefaults()
	h.publicCache = configs
	h.publicCacheTime = time.Now()
	utils.Success(c, configs)
}

// invalidatePublicCache 由 admin 修改/删除配置时调用，清缓存让新值立刻生效。
func (h *SystemConfigHandler) invalidatePublicCache() {
	h.publicCacheMu.Lock()
	h.publicCache = nil
	h.publicCacheMu.Unlock()
}

// ---------- 管理端点：仅 admin 可读写 ----------

// ListConfigs 列出全部配置项（含元信息：默认值 / 描述 / 是否被覆盖）。
func (h *SystemConfigHandler) ListConfigs(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	metas, err := database.ListAllConfigMeta()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取配置失败: "+err.Error())
		return
	}
	utils.Success(c, metas)
}

// UpdateConfig 修改/新增一个配置项。
// body: { "key": "...", "value": "..." }
func (h *SystemConfigHandler) UpdateConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if req.Key == "" {
		utils.BadRequest(c, "key 不能为空")
		return
	}

	if err := database.SetSystemConfig(req.Key, req.Value); err != nil {
		// 区分：未知 key（白名单未通过）vs 其他 DB 错误
		if errors.Is(err, database.ErrInvalidConfigKey) {
			utils.BadRequest(c, "无效的 key: "+req.Key+"（不在 DEFAULT_CONFIGS 白名单中）")
			return
		}
		utils.Error(c, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	// 改后让 public cache 立刻失效，避免新值延迟生效（最坏情况 5s TTL 才能看到）
	h.invalidatePublicCache()
	utils.Success(c, gin.H{
		"key":   req.Key,
		"value": req.Value,
	})
}

// DeleteConfig 删除一个 key（回退到 DEFAULT_CONFIGS 兜底）。
func (h *SystemConfigHandler) DeleteConfig(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	key := c.Param("key")
	if key == "" {
		utils.BadRequest(c, "key 不能为空")
		return
	}
	rowsAffected, err := database.DeleteSystemConfig(key)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	// 删后让 public cache 立刻失效
	h.invalidatePublicCache()
	if rowsAffected == 0 {
		// key 本来就不在 DB（已是默认状态）
		utils.Success(c, gin.H{
			"message":   "无需操作（该 key 已是代码默认，未在 DB 覆盖）",
			"rows":      rowsAffected,
			"was_in_db": false,
		})
		return
	}
	utils.Success(c, gin.H{
		"message":   "已删除，次回退到代码默认",
		"rows":      rowsAffected,
		"was_in_db": true,
	})
}
