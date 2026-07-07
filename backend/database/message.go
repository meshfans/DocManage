package database

import (
	"database/sql"
	"time"
)

// 本文件：系统通知（Message）相关表与 CRUD 操作。

// ==================== Message ====================

// Message 一条系统通知，关联用户（接收者）和可选发送者；status=deleted 表示软删。
type Message struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	SenderID   int64  `json:"sender_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Read       bool   `json:"read"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	SenderName string `json:"sender_name,omitempty"`
}

// CreateMessage 写入一条完整通知（带 title/type 等）。
func CreateMessage(userID int64, senderID int64, title, content, msgType string) (int64, error) {
	result, err := DB.Exec(
		`INSERT INTO messages (user_id, sender_id, title, content, type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, senderID, title, content, msgType, time.Now().Unix(), time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetMessagesByUserID 分页查询某用户全部 active 通知，按时间倒序。
func GetMessagesByUserID(userID int64, limit, offset int) ([]Message, error) {
	rows, err := DB.Query(
		`SELECT m.id, m.user_id, COALESCE(m.sender_id, 0), m.title, m.content, m.type, m.status, m.read, m.created_at, m.updated_at,
		        COALESCE(u.nickname, '系统') as sender_name
		 FROM messages m
		 LEFT JOIN users u ON m.sender_id = u.id
		 WHERE m.user_id = ? AND m.status = 'active'
		 ORDER BY m.created_at DESC
		 LIMIT ? OFFSET ?`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var senderName sql.NullString
		err := rows.Scan(&msg.ID, &msg.UserID, &msg.SenderID, &msg.Title, &msg.Content, &msg.Type, &msg.Status, &msg.Read, &msg.CreatedAt, &msg.UpdatedAt, &senderName)
		if err != nil {
			return nil, err
		}
		if senderName.Valid {
			msg.SenderName = senderName.String
		} else {
			msg.SenderName = "系统"
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// GetUnreadMessageCount 统计某用户未读通知数量。
func GetUnreadMessageCount(userID int64) (int, error) {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND read = 0 AND status = 'active'`,
		userID,
	).Scan(&count)
	return count, err
}

// MarkMessageAsRead 标记单条通知为已读。
func MarkMessageAsRead(messageID, userID int64) error {
	_, err := DB.Exec(
		`UPDATE messages SET read = 1, updated_at = ? WHERE id = ? AND user_id = ?`,
		time.Now().Unix(), messageID, userID,
	)
	return err
}

// MarkAllMessagesAsRead 标记某用户全部通知为已读。
func MarkAllMessagesAsRead(userID int64) error {
	_, err := DB.Exec(
		`UPDATE messages SET read = 1, updated_at = ? WHERE user_id = ? AND read = 0`,
		time.Now().Unix(), userID,
	)
	return err
}

// DeleteMessage 软删除通知（status 置为 deleted）。
func DeleteMessage(messageID, userID int64) error {
	_, err := DB.Exec(
		`UPDATE messages SET status = 'deleted', updated_at = ? WHERE id = ? AND user_id = ?`,
		time.Now().Unix(), messageID, userID,
	)
	return err
}

// GetMessageByID 按 id 查询单条通知。
func GetMessageByID(messageID int64) (*Message, error) {
	msg := &Message{}
	var senderName sql.NullString
	err := DB.QueryRow(
		`SELECT m.id, m.user_id, COALESCE(m.sender_id, 0), m.title, m.content, m.type, m.status, m.read, m.created_at, m.updated_at,
		        COALESCE(u.nickname, '系统') as sender_name
		 FROM messages m
		 LEFT JOIN users u ON m.sender_id = u.id
		 WHERE m.id = ? AND m.status = 'active'`,
		messageID,
	).Scan(&msg.ID, &msg.UserID, &msg.SenderID, &msg.Title, &msg.Content, &msg.Type, &msg.Status, &msg.Read, &msg.CreatedAt, &msg.UpdatedAt, &senderName)
	if err != nil {
		return nil, err
	}
	if senderName.Valid {
		msg.SenderName = senderName.String
	} else {
		msg.SenderName = "系统"
	}
	return msg, nil
}
