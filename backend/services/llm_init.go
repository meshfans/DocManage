package services

import (
	"log"

	"doc/database"
	"doc/services/llm"
)

// InitLLMService 初始化 LLM 服务
func InitLLMService() error {
	if database.DB == nil {
		log.Println("[LLM] 数据库未初始化，跳过 LLM 服务初始化")
		return nil
	}

	if err := llm.InitService(database.DB); err != nil {
		return err
	}

	log.Println("[LLM] LLM 服务初始化完成")
	return nil
}

// GetLLMService 获取 LLM 服务
func GetLLMService() *llm.Service {
	return llm.GetService()
}
