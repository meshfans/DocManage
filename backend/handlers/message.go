package handlers

import (
	"doc/database"
	"doc/utils"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type MessageHandler struct{}

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

func (h *MessageHandler) GetMessages(c *gin.Context) {
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
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "获取消息列表失败", err)
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
	userID := c.GetInt64("user_id")
	
	count, err := database.GetUnreadMessageCount(userID)
	if err != nil {
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "获取未读消息数失败", err)
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
		utils.BadRequest(c, "参数错误: "+err.Error())
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
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "创建消息失败", err)
		return
	}
	
	utils.Success(c, gin.H{
		"id": id,
	})
}

func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的消息ID")
		return
	}
	
	if err := database.MarkMessageAsRead(messageID, userID); err != nil {
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "标记消息已读失败", err)
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) MarkAllAsRead(c *gin.Context) {
	userID := c.GetInt64("user_id")
	
	if err := database.MarkAllMessagesAsRead(userID); err != nil {
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "标记所有消息已读失败", err)
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) DeleteMessage(c *gin.Context) {
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的消息ID")
		return
	}
	
	if err := database.DeleteMessage(messageID, userID); err != nil {
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "删除消息失败", err)
		return
	}
	
	utils.Success(c, nil)
}

func (h *MessageHandler) GetMessage(c *gin.Context) {
	userID := c.GetInt64("user_id")
	
	messageIDStr := c.Param("id")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的消息ID")
		return
	}
	
	msg, err := database.GetMessageByID(messageID)
	if err != nil {
		utils.ErrorWithDetail(c, http.StatusInternalServerError, "获取消息详情失败", err)
		return
	}
	
	if msg.UserID != userID {
		utils.Error(c, http.StatusForbidden, "无权访问此消息")
		return
	}
	
	if !msg.Read {
		database.MarkMessageAsRead(messageID, userID)
		msg.Read = true
	}
	
	utils.Success(c, msg)
}
