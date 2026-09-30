package llm

import (
	"database/sql"
	"sync"

	"doc/services/llm/models"
	"doc/utils"
)

// Service LLM 服务
//
// dispatcher 由 Reload 原子替换；流式请求持有的是替换前那一刻的快照，
// 因此不会被打断；新请求拿到新 dispatcher 后即可走新默认模型 / Key / api_base。
type Service struct {
	mu         sync.RWMutex
	db         *sql.DB
	dispatcher *Dispatcher
}

// NewService 创建 LLM 服务
func NewService(db *sql.DB) (*Service, error) {
	svc := &Service{db: db}
	if err := svc.Reload(); err != nil {
		return nil, err
	}
	return svc, nil
}

// Reload 重新从 DB 加载默认 AI 配置并重建 dispatcher。
//
// 由 AI 配置变更 handler（CreateAIConfig / UpdateAIConfig / UpdateAIConfigKey /
// DeleteAIConfig / SetDefaultAIConfig）在写完 DB 后调用，使后续 /api/llm/* 请求立即生效。
// 调用失败仅 warn，不阻塞配置写入。
func (s *Service) Reload() error {
	cfg, err := loadDefaultModelConfig(s.db)
	if err != nil {
		utils.Warn("[LLM] Reload 加载默认配置失败: %v", err)
		return err
	}
	if cfg == nil {
		utils.Warn("[LLM] Reload 未找到默认 AI 配置，dispatcher 置 nil")
		s.mu.Lock()
		s.dispatcher = nil
		s.mu.Unlock()
		return nil
	}

	model, err := models.NewModel(cfg)
	if err != nil {
		utils.Warn("[LLM] Reload 创建模型适配器失败: %v", err)
		return err
	}

	guard := NewDefaultGuard()
	if s.db != nil {
		guard.SetLogger(NewDBAuditLogger(s.db))
	}

	dispatcher := BuildDispatcher(model, guard, GetTemplateEngine())

	s.mu.Lock()
	s.dispatcher = dispatcher
	s.mu.Unlock()
	utils.Info("[LLM] Reload 完成: provider=%s model=%s", cfg.Provider, cfg.ModelName)
	return nil
}

// loadDefaultModelConfig 从数据库加载默认模型配置
func loadDefaultModelConfig(db *sql.DB) (*models.ModelConfig, error) {
	var cfg models.ModelConfig

	err := db.QueryRow(`
		SELECT provider, protocol, model_name, api_key, api_base, api_path, proxy_url
		FROM ai_config
		WHERE is_default = 1 AND status = 1 AND deleted_at IS NULL
		LIMIT 1
	`).Scan(&cfg.Provider, &cfg.Protocol, &cfg.ModelName, &cfg.APIKey, &cfg.APIBase,
		&cfg.APIPath, &cfg.ProxyURL)

	if err == sql.ErrNoRows {
		return nil, nil // 没有默认配置
	}
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// GetDispatcher 获取当前 dispatcher 快照
func (s *Service) GetDispatcher() *Dispatcher {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dispatcher
}

// 全局服务实例
var globalService *Service

// InitService 初始化全局服务
func InitService(db *sql.DB) error {
	svc, err := NewService(db)
	if err != nil {
		return err
	}
	globalService = svc
	return nil
}

// GetService 获取全局服务
func GetService() *Service {
	return globalService
}
