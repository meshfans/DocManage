package handlers

import (
	"doc/database"
	"doc/utils"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type MessageHandler struct{}

// wsHandlerForMessage 单例的 WS hub（main.go 启动时通过 SetWebSocketHandlerForMessage 注入）。
// 用于在 CreateMessage 后通过 SendToUser 实时推送给接收人。
var wsHandlerForMessage *WebSocketHandler

// SetWebSocketHandlerForMessage 注入 WS hub 单例。在 main.go 启动早期调用一次。
func SetWebSocketHandlerForMessage(h *WebSocketHandler) {
	wsHandlerForMessage = h
}

func NewMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

type CreateMessageRequest struct {
	UserID   int64  `json:"user_id" binding:"required"`
	SenderID int64  `json:"sender_id"`
	Title    string `json:"title" binding:"required"`
	Content  string `json:"content" binding:"required"`
	Type     string `json:"type"`
}

// wsNewMessagePayload WebSocket new_message 推送的载荷结构。
// 用 struct 而非 gin.H 是为了：
//  1. 编译期保证字段名拼写正确（gin.H 拼错字段名直到运行时才被发现）
//  2. 字段标签即 JSON schema，对前端 / 文档生成友好
//  3. 与前端 lay-notice/index.vue 的 handleWsNewMessage 字段对齐
type wsNewMessagePayload struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	SenderID  int64  `json:"sender_id"`
	UserID    int64  `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
}

func (h *MessageHandler) GetMessages(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:list") {
		return
	}
	userID := c.GetInt64("user_id")
	
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)
	
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	
	messages, err := database.GetMessagesByUserID(userID, limit, offset)
	if err != nil {
		utils.LogError("[message.GetMessages] 失败 userID=%d: %v", userID, err)
		utils.Err(c, utils.CodeInternal, "获取消息列表失败: "+err.Error())
		return
	}
	
	if messages == nil {
		messages = []database.Message{}
	}
	
	utils.Success(c, gin.H{
		"messages": messages,
		"total": len(messages),
	})
}

func (h *MessageHandler) GetUnreadCount(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:unread-count") {
		return
	}
	userID := c.GetInt64("user_id")
	
	count, err := database.GetUnreadMessageCount(userID)
	if err != nil {
		utils.LogError("[message.GetUnreadCount] 失败 userID=%d: %v", userID, err)
		utils.Err(c, utils.CodeInternal, "获取未读消息数失败: "+err.Error())
		return
	}
	
	utils.Success(c, gin.H{
		"count": count,
	})
}

func (h *MessageHandler) CreateMessage(c *gin.Context) {
	if !RequirePermission(c, "message:create") {
		return
	}
	var req CreateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "参数错误: "+err.Error())
		return
	}

	// 安全修复 2026-06-29：senderID 强制取 JWT user_id，禁止客户端伪造发送者（防钓鱼）。
	senderID := c.GetInt64("user_id")
	
	msgType := req.Type
	if msgType == "" {
		msgType = "system"
	}
	
	id, err := database.CreateMessage(req.UserID, senderID, req.Title, req.Content, msgType)
	if err != nil {
		utils.LogError("[message.CreateMessage] 失败 userID=%d senderID=%d: %v", req.UserID, senderID, err)
		utils.Err(c, utils.CodeInternal, "创建消息失败: "+err.Error())
		return
	}

	// WebSocket 实时推送：接收人在线时立即推一条 new_message，前端铃铛即时更新。
	// 离线则下次拉取 getUnreadCount 兜底，SendToUser 内部已是非阻塞。
	if wsHandlerForMessage != nil {
		wsHandlerForMessage.SendToUser(req.UserID, "new_message", wsNewMessagePayload{
			ID:        id,
			Title:     req.Title,
			Content:   req.Content,
			Type:      msgType,
			SenderID:  senderID,
			UserID:    req.UserID,
			CreatedAt: time.Now().Unix(),
		})
	}

	utils.Success(c, gin.H{
		"id": id,
	})
}

func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:mark-read") {
		return
	}
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的消息ID")
		return
	}
	
	if err := database.MarkMessageAsRead(messageID, userID); err != nil {
		utils.LogError("[message.MarkAsRead] 失败 messageID=%d userID=%d: %v", messageID, userID, err)
		utils.Err(c, utils.CodeInternal, "标记消息已读失败: "+err.Error())
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) MarkAllAsRead(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:mark-all-read") {
		return
	}
	userID := c.GetInt64("user_id")
	
	if err := database.MarkAllMessagesAsRead(userID); err != nil {
		utils.LogError("[message.MarkAllAsRead] 失败 userID=%d: %v", userID, err)
		utils.Err(c, utils.CodeInternal, "标记所有消息已读失败: "+err.Error())
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) DeleteMessage(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:delete") {
		return
	}
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的消息ID")
		return
	}
	
	if err := database.DeleteMessage(messageID, userID); err != nil {
		utils.LogError("[message.DeleteMessage] 失败 messageID=%d userID=%d: %v", messageID, userID, err)
		utils.Err(c, utils.CodeInternal, "删除消息失败: "+err.Error())
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) GetMessage(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequirePermission 兜底。
	if !RequirePermission(c, "message:detail") {
		return
	}
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.Err(c, utils.CodeInvalidParam, "无效的消息ID")
		return
	}
	
	msg, err := database.GetMessageByID(messageID)
	if err != nil {
		utils.LogError("[message.GetMessage] 失败 messageID=%d: %v", messageID, err)
		utils.Err(c, utils.CodeInternal, "获取消息详情失败: "+err.Error())
		return
	}
	
	if msg.UserID != userID {
		utils.Err(c, utils.CodeForbidden, "无权访问此消息")
		return
	}
	
	if !msg.Read {
		database.MarkMessageAsRead(messageID, userID)
		msg.Read = true
	}
	
	utils.Success(c, msg)
}
