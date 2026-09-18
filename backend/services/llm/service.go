package llm

import (
	"database/sql"
	"log"

	"doc/services/llm/models"
)

// Service LLM 服务
type Service struct {
	dispatcher *Dispatcher
}

// NewService 创建 LLM 服务
func NewService(db *sql.DB) (*Service, error) {
	// 加载默认模型配置
	cfg, err := loadDefaultModelConfig(db)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		log.Println("[LLM] 未找到默认 AI 配置，请先在系统设置中添加 AI 配置")
		return &Service{dispatcher: nil}, nil
	}

	// 创建模型适配器
	model, err := models.NewModel(cfg)
	if err != nil {
		return nil, err
	}

	// 创建防护层 + 数据库审计
	guard := NewDefaultGuard()
	if db != nil {
		guard.SetLogger(NewDBAuditLogger(db))
	}

	// 创建模板引擎
	templates := GetTemplateEngine()

	// 构建调度器
	dispatcher := BuildDispatcher(model, guard, templates)

	return &Service{
		dispatcher: dispatcher,
	}, nil
}

// loadDefaultModelConfig 从数据库加载默认模型配置
func loadDefaultModelConfig(db *sql.DB) (*models.ModelConfig, error) {
	var cfg models.ModelConfig

	err := db.QueryRow(`
		SELECT provider, protocol, model_name, api_key, api_base
		FROM ai_config
		WHERE is_default = 1 AND status = 1
		LIMIT 1
	`).Scan(&cfg.Provider, &cfg.Protocol, &cfg.ModelName, &cfg.APIKey, &cfg.APIBase)

	if err == sql.ErrNoRows {
		return nil, nil // 没有默认配置
	}
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// GetDispatcher 获取调度器
func (s *Service) GetDispatcher() *Dispatcher {
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
