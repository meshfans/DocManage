package handlers

import (
	"fmt"
	"io"
	"net/http"

	"doc/services/llm"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// LLMHandler LLM API Handler
type LLMHandler struct{}

// NewLLMHandler 创建 handler
func NewLLMHandler() *LLMHandler {
	return &LLMHandler{}
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Input   string         `json:"input" binding:"required"`
	Context map[string]any `json:"context"`
	Module  string         `json:"module"`
}

// HandleChat 通用对话
// POST /api/llm/chat
func (h *LLMHandler) HandleChat(c *gin.Context) {
	utils.Debug("[LLM] HandleChat 请求开始")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Debug("[LLM] HandleChat 参数解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "输入不能为空"})
		return
	}

	utils.Debug("[LLM] HandleChat 收到请求: input=%q", req.Input)

	userID := getUserID(c)

	svc := llm.GetService()
	if svc == nil || svc.GetDispatcher() == nil {
		utils.Warn("[LLM] HandleChat LLM 服务未初始化")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 服务未初始化，请先配置默认 AI 模型"})
		return
	}

	utils.Debug("[LLM] HandleChat userID=%d, 开始处理", userID)

	resp, err := svc.GetDispatcher().Dispatch(c.Request.Context(), llm.Request{
		UserID:   userID,
		Input:    req.Input,
		Context:  req.Context,
		Template: req.Module,
	})
	if err != nil {
		utils.LogError("[LLM] HandleChat Dispatch 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI 服务处理失败，请稍后重试"})
		return
	}

	utils.Debug("[LLM] HandleChat 完成, tokens=%d", resp.Usage.TotalTokens)
	utils.Success(c, resp)
}

// HandleModuleExecute 执行指定模块
// POST /api/llm/module/:name/execute
func (h *LLMHandler) HandleModuleExecute(c *gin.Context) {
	moduleName := c.Param("name")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "输入不能为空"})
		return
	}

	userID := getUserID(c)

	svc := llm.GetService()
	if svc == nil || svc.GetDispatcher() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 服务未初始化，请先配置默认 AI 模型"})
		return
	}

	resp, err := svc.GetDispatcher().ExecuteModule(c.Request.Context(), moduleName, llm.Request{
		UserID:  userID,
		Input:   req.Input,
		Context: req.Context,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AI 服务处理失败，请稍后重试"})
		return
	}

	utils.Success(c, resp)
}

// HandleStream 流式对话
// POST /api/llm/stream
func (h *LLMHandler) HandleStream(c *gin.Context) {
	utils.Debug("[LLM] HandleStream 请求开始")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Debug("[LLM] HandleStream 参数解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "输入不能为空"})
		return
	}

	utils.Debug("[LLM] HandleStream 收到请求: input=%q, context=%+v", req.Input, req.Context)

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Writer.Flush()

	svc := llm.GetService()
	if svc == nil {
		utils.Warn("[LLM] HandleStream LLM Service 为 nil")
		fmt.Fprintf(c.Writer, "data: {\"error\": \"LLM 服务未初始化\"}\n\n")
		c.Writer.Flush()
		return
	}
	if svc.GetDispatcher() == nil {
		utils.Warn("[LLM] HandleStream Dispatcher 为 nil")
		fmt.Fprintf(c.Writer, "data: {\"error\": \"LLM 服务未初始化\"}\n\n")
		c.Writer.Flush()
		return
	}

	userID := getUserID(c)

	// 意图识别（无指定 module 时）
	intent := svc.GetDispatcher().RecognizeIntent(req.Input)
	utils.Debug("[LLM] HandleStream userID=%d, intent=%q, module=%q, model=%s, 开始流式处理", userID, intent, req.Module, svc.GetDispatcher().ModelName())

	stream, err := svc.GetDispatcher().DispatchStream(c.Request.Context(), llm.Request{
		UserID:   userID,
		Input:    req.Input,
		Context:  req.Context,
		Template: req.Module,
	})
	if err != nil {
		utils.LogError("[LLM] HandleStream DispatchStream 错误: %v", err)
		fmt.Fprintf(c.Writer, "data: {\"error\": \"AI 服务处理失败\"}\n\n")
		c.Writer.Flush()
		return
	}

	utils.Debug("[LLM] HandleStream 流开始返回")
	for chunk := range stream {
		if chunk.Error != nil {
			utils.LogError("[LLM] HandleStream 流 chunk 错误: %v", chunk.Error)
			fmt.Fprintf(c.Writer, "data: {\"error\": %q}\n\n", chunk.Error.Error())
			c.Writer.Flush()
			continue
		}

		// 先发送 delta 内容（静态回复如 unknown intent 帮助文本）
		if chunk.Delta != "" {
			fmt.Fprintf(c.Writer, "data: {\"delta\": %q}\n\n", chunk.Delta)
			c.Writer.Flush()
		}

		if chunk.Done {
			utils.Debug("[LLM] HandleStream 流结束, tokens=%d", chunk.Usage.TotalTokens)
			if chunk.Usage.TotalTokens > 0 {
				fmt.Fprintf(c.Writer, "data: {\"done\": true, \"usage\": %+v}\n\n", chunk.Usage)
			} else {
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			}
			c.Writer.Flush()
			return
		}
	}

	utils.Debug("[LLM] HandleStream 流通道关闭")
}

// HandleModuleStream 指定模块流式执行
// POST /api/llm/module/:name/stream
func (h *LLMHandler) HandleModuleStream(c *gin.Context) {
	moduleName := c.Param("name")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "输入不能为空"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	svc := llm.GetService()
	if svc == nil || svc.GetDispatcher() == nil {
		fmt.Fprintf(c.Writer, "data: {\"error\": \"LLM 服务未初始化\"}\n\n")
		c.Writer.Flush()
		return
	}

	userID := getUserID(c)

	stream, err := svc.GetDispatcher().ExecuteModuleStream(c.Request.Context(), moduleName, llm.Request{
		UserID:  userID,
		Input:   req.Input,
		Context: req.Context,
	})
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"error\": \"AI 服务处理失败\"}\n\n")
		c.Writer.Flush()
		return
	}

	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Fprintf(c.Writer, "data: {\"error\": %q}\n\n", chunk.Error.Error())
			c.Writer.Flush()
			continue
		}

		if chunk.Done {
			if chunk.Usage.TotalTokens > 0 {
				fmt.Fprintf(c.Writer, "data: {\"done\": true, \"usage\": %+v}\n\n", chunk.Usage)
			} else {
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			}
			c.Writer.Flush()
			return
		}

		fmt.Fprintf(c.Writer, "data: {\"delta\": %q}\n\n", chunk.Delta)
		c.Writer.Flush()
	}
}

// getUserID 从 context 获取用户 ID
func getUserID(c *gin.Context) int64 {
	if id, exists := c.Get("user_id"); exists {
		if userID, ok := id.(int64); ok {
			return userID
		}
	}
	return 0
}

// DiscardRequestBody 消费请求体（用于流式请求）
func DiscardRequestBody(c *gin.Context) {
	io.Copy(io.Discard, c.Request.Body)
}
