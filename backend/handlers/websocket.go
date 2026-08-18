package handlers

import (
	"context"
	"doc/config"
	"doc/utils"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const (
	// WebSocketReadTimeout 单条消息读超时（同时也是 ping/pong 续约窗口）
	WebSocketReadTimeout = 75 * time.Second
	// WebSocketWriteTimeout 单条消息写超时（防慢客户端拖死 writePump）
	WebSocketWriteTimeout = 10 * time.Second
	// WebSocketPingPeriod 服务端主动 ping 周期（必须 < WebSocketReadTimeout）
	WebSocketPingPeriod = 30 * time.Second
	// WebSocketMaxMessageSize 单条消息上限（防大消息攻击）
	WebSocketMaxMessageSize = 64 * 1024
	// WebSocketSendBuffer 每个 conn 的 send 缓冲（写不进去就丢老消息，避免阻塞业务）
	WebSocketSendBuffer = 64
	// WebSocketAuthTimeout auth 消息必须在 N 秒内到达，否则关闭
	WebSocketAuthTimeout = 10 * time.Second
	// WebSocketMaxConnections 全局最大并发连接数（防 fd 耗尽）
	WebSocketMaxConnections = 1000
)

func getAllowedOrigins() (map[string]bool, bool) {
	allowedOrigins := make(map[string]bool)
	allowAll := false

	cfg := config.GlobalConfig
	if cfg != nil && len(cfg.CORS.AllowedOrigins) > 0 {
		for _, origin := range cfg.CORS.AllowedOrigins {
			if origin == "*" {
				allowAll = true
			} else if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
				allowedOrigins[origin] = true
			}
		}
	}

	return allowedOrigins, allowAll
}

// upgrader：CheckOrigin 强制白名单 + 拒绝空 Origin（防 CSWSH）。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		allowedOrigins, allowAll := getAllowedOrigins()

		if allowAll {
			return true
		}

		origin := r.Header.Get("Origin")
		// 非浏览器 / 同源请求可能没有 Origin（如 curl）—— 放行
		// 浏览器跨域请求一定会带 Origin，被下面的白名单拦截
		if origin == "" {
			return true
		}

		if allowedOrigins[origin] {
			return true
		}

		utils.Info("WebSocket connection rejected from origin: %s, allowedOrigins: %v, allowAll: %v", origin, allowedOrigins, allowAll)
		return false
	},
}

type WebSocketMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

// client 代表一个已注册连接，包含 readPump / writePump 所需的全部状态。
type wsClient struct {
	conn       *websocket.Conn
	userID     int64
	authToken  string        // 用于黑名单校验，登录态失效时立即断开
	send       chan []byte   // writePump 独占消费
	closeCode  chan int      // closeWithCode 通知 writePump 发送 CloseMessage
	closeDone  chan struct{} // writePump 退出时关闭，供 closeWithCode 等待
	closeOnce  sync.Once
	closedFlag atomic.Bool
}

// WebSocketHandler 是 hub：维护连接集合 + 索引，不做任何 conn I/O。
type WebSocketHandler struct {
	sync.RWMutex // 嵌入：所有 clients/userClients/connByPtr 的并发访问统一加锁

	clients     map[*wsClient]struct{}
	userClients map[int64]*wsClient           // userID -> 最新 conn（单点登录）
	connByPtr   map[*websocket.Conn]*wsClient // 反向索引（替代 O(N) 扫描）
	register    chan *wsClient
	unregister  chan *wsClient
	broadcast   chan []byte
	connCount   atomic.Int64

	// 关停信号
	ctx      context.Context
	cancel   context.CancelFunc
	shutdown atomic.Bool
	hubWg    sync.WaitGroup

	jwtSecret []byte
}

func NewWebSocketHandler(jwtSecret []byte) *WebSocketHandler {
	ctx, cancel := context.WithCancel(context.Background())
	return &WebSocketHandler{
		clients:     make(map[*wsClient]struct{}),
		userClients: make(map[int64]*wsClient),
		connByPtr:   make(map[*websocket.Conn]*wsClient),
		broadcast:   make(chan []byte, 256),
		register:    make(chan *wsClient, 64),
		unregister:  make(chan *wsClient, 64),
		jwtSecret:   jwtSecret,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Run 启动 hub 主循环（应在独立 goroutine 中调用）。返回时表示 Shutdown 已触发。
func (h *WebSocketHandler) Run() {
	h.hubWg.Add(1)
	defer h.hubWg.Done()

	for {
		select {
		case <-h.ctx.Done():
			utils.Info("WebSocket hub: 收到关停信号，关闭所有连接")
			h.Lock()
			for c := range h.clients {
				c.closeWithCode(websocket.CloseGoingAway, "server shutting down")
			}
			h.Unlock()
			return

		case c := <-h.register:
			h.handleRegister(c)

		case c := <-h.unregister:
			h.handleUnregister(c)

		case message := <-h.broadcast:
			h.fanoutBroadcast(message)
		}
	}
}

func (h *WebSocketHandler) handleRegister(c *wsClient) {
	h.Lock()
	defer h.Unlock()
	if h.shutdown.Load() {
		c.closeWithCode(websocket.CloseGoingAway, "server shutting down")
		return
	}
	// 全局连接数超限，拒绝新连接
	if h.connCount.Load() >= WebSocketMaxConnections {
		utils.Warn("[WS] 连接数超限 %d/%d，拒绝新连接", h.connCount.Load(), WebSocketMaxConnections)
		c.closeWithCode(websocket.CloseTryAgainLater, "server busy")
		return
	}

	if c.userID > 0 {
		// 单点登录：踢掉该 userID 的旧连接
		if old, exists := h.userClients[c.userID]; exists && old != c {
			old.closeWithCode(websocket.ClosePolicyViolation, "replaced by new login")
			delete(h.clients, old)
			delete(h.connByPtr, old.conn)
		}
		h.userClients[c.userID] = c
	}
	h.clients[c] = struct{}{}
	h.connByPtr[c.conn] = c
	h.connCount.Add(1)
	utils.Debug("WebSocket client connected. UserID: %d, Total clients: %d", c.userID, h.connCount.Load())
}

func (h *WebSocketHandler) handleUnregister(c *wsClient) {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	delete(h.connByPtr, c.conn)
	if c.userID > 0 {
		if cur, ok := h.userClients[c.userID]; ok && cur == c {
			delete(h.userClients, c.userID)
		}
	}
	h.connCount.Add(-1)
	c.closeWithCode(websocket.CloseNormalClosure, "")
	utils.Debug("WebSocket client disconnected. Total clients: %d", h.connCount.Load())
}

// fanoutBroadcast 把广播消息丢到每个 conn 的 send channel，
// 不在 hub 内做 conn I/O；writePump 各自消费。
func (h *WebSocketHandler) fanoutBroadcast(message []byte) {
	h.RLock()
	defer h.RUnlock()
	for c := range h.clients {
		// 非阻塞写入：send 已满则关闭这个 conn（最简单降级）
		select {
		case c.send <- message:
		default:
			utils.Info("WebSocket send buffer full, closing slow client. UserID: %d", c.userID)
			go c.closeWithCode(websocket.CloseInternalServerErr, "send buffer overflow")
		}
	}
}

// HandleWebSocket 是 HTTP 入口。先鉴权（URL ?token= 或 Sec-WebSocket-Protocol），
// 鉴权失败直接 close；鉴权通过后再 upgrade 注册到 hub。
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	// P0#3：前置鉴权，避免消耗 upgrade 资源后再断开
	userID, token, ok := h.authenticateRequest(c.Request)
	if !ok || userID == 0 {
		utils.Info("WebSocket upgrade rejected: invalid token from %s", c.Request.RemoteAddr)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "invalid or missing token",
			"success": false,
		})
		return
	}

	// P0#3：连接数上限（防 fd 耗尽）
	if h.connCount.Load() >= WebSocketMaxConnections {
		utils.Info("WebSocket upgrade rejected: max connections reached (%d)", WebSocketMaxConnections)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "max connections reached",
			"success": false,
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		utils.Info("Failed to upgrade connection: %v", err)
		return
	}

	cli := &wsClient{
		conn:      conn,
		userID:    userID,
		authToken: token,
		send:      make(chan []byte, WebSocketSendBuffer),
		closeCode: make(chan int, 1), // 用于通知 writePump 发送 CloseMessage
		closeDone: make(chan struct{}),
	}

	// 注册到 hub
	select {
	case h.register <- cli:
	case <-h.ctx.Done():
		cli.closeWithCode(websocket.CloseGoingAway, "server shutting down")
		return
	}

	go h.writePump(cli)
	go h.readPump(cli)
}

// authenticateRequest 从 ?token= 或 Sec-WebSocket-Protocol: bearer, <token> 取 JWT。
// 优先 URL 参数（兼容浏览器 WebSocket API）；子协议模式作为备份，避免 token 落入 access log。
//
// 返回 (userID, token, ok)：token 用于客户端黑名单校验（pong 帧续约时检查是否登出）。
func (h *WebSocketHandler) authenticateRequest(r *http.Request) (int64, string, bool) {
	token := r.URL.Query().Get("token")
	if token == "" {
		// 子协议：Sec-WebSocket-Protocol: bearer, <jwt>
		if sp := r.Header.Get("Sec-WebSocket-Protocol"); sp != "" {
			parts := strings.SplitN(sp, ",", 2)
			if len(parts) == 2 {
				proto := strings.TrimSpace(parts[0])
				token = strings.TrimSpace(parts[1])
				if !strings.EqualFold(proto, "bearer") || token == "" {
					return 0, "", false
				}
				// 回写子协议（服务端选定 bearer）让握手成功
				r.Header.Set("Sec-WebSocket-Protocol", "bearer")
			}
		}
	}
	if token == "" {
		return 0, "", false
	}
	uid := h.validateToken(token)
	if uid == 0 {
		return 0, "", false
	}
	return uid, token, true
}

// readPump 由独立 goroutine 驱动，读 conn + 解析消息 + 调度处理。
// 所有写操作通过 cli.send chan 交给 writePump，避免并发写。
func (h *WebSocketHandler) readPump(cli *wsClient) {
	defer func() {
		// 通知 hub 注销；通过 select 避免 hub 已关停时永久阻塞
		select {
		case h.unregister <- cli:
		case <-h.ctx.Done():
		}
		utils.Debug("[WS] readPump 退出 UserID=%d", cli.userID)
	}()

	conn := cli.conn
	conn.SetReadLimit(WebSocketMaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(WebSocketAuthTimeout))
	// 收到客户端 Pong 帧时续约读超时（纯逻辑，不打日志，避免 30s 一条噪音）
	// 同时检查 JWT 黑名单：用户登出（token 进入黑名单）后，下次心跳立即断开。
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(WebSocketReadTimeout))
		if cli.authToken != "" && utils.IsTokenRevoked(cli.authToken) {
			utils.Info("[WS] token 已注销，关闭连接 UserID=%d", cli.userID)
			cli.closeWithCode(websocket.ClosePolicyViolation, "token revoked")
		}
		return nil
	})

	// auth 消息必须在 WebSocketAuthTimeout 内到达；超时关闭
	authenticated := false
	authTimer := time.AfterFunc(WebSocketAuthTimeout, func() {
		if !authenticated {
			utils.Info("[WS] auth 超时 UserID=%d", cli.userID)
			cli.closeWithCode(websocket.ClosePolicyViolation, "auth timeout")
		}
	})
	defer authTimer.Stop()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// 把正常关闭（1000、1001）从"异常"里排除
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,       // 1001 浏览器/页面关闭
				websocket.CloseAbnormalClosure, // 1006 网络异常
				websocket.CloseNormalClosure,   // 1000 客户端主动断开
			) {
				utils.Info("[WS] 异常关闭 UserID=%d err=%v", cli.userID, err)
			}
			return
		}

		// 首条消息必须是 auth
		if !authenticated {
			var msg WebSocketMessage
			if err := json.Unmarshal(message, &msg); err != nil || msg.Type != "auth" {
				utils.Info("[WS] 首条消息非 auth，关闭连接 UserID=%d", cli.userID)
				cli.closeWithCode(websocket.ClosePolicyViolation, "auth required")
				return
			}
			// token 已通过 URL 校验，这里只确认 userID 一致
			authenticated = true
			authTimer.Stop()
			_ = conn.SetReadDeadline(time.Now().Add(WebSocketReadTimeout))
			// 回复 auth ok
			resp, _ := json.Marshal(map[string]interface{}{
				"type":      "auth",
				"content":   map[string]interface{}{"status": "ok", "user_id": cli.userID},
				"timestamp": time.Now().Unix(),
			})
			select {
			case cli.send <- resp:
			default:
			}
			continue
		}

		// 已认证：处理消息（写操作走 send chan）
		var msg WebSocketMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			utils.Info("[WS] 解析消息失败: %v", err)
			continue
		}
		h.handleMessage(cli, msg)
	}
}

// writePump 由独立 goroutine 驱动，串行化所有 conn.WriteMessage。
// 周期 ping + 消费 send chan + 处理关闭信号。
func (h *WebSocketHandler) writePump(cli *wsClient) {
	ticker := time.NewTicker(WebSocketPingPeriod)
	defer func() {
		ticker.Stop()
		close(cli.closeDone) // 通知 closeWithCode 可以关闭连接
		_ = cli.conn.Close()
		utils.Debug("[WS] writePump 退出 UserID=%d", cli.userID)
	}()

	conn := cli.conn
	for {
		select {
		case <-h.ctx.Done():
			_ = conn.SetWriteDeadline(time.Now().Add(WebSocketWriteTimeout))
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
			return

		case code := <-cli.closeCode:
			// closeWithCode 通知发送 CloseMessage
			_ = conn.SetWriteDeadline(time.Now().Add(WebSocketWriteTimeout))
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""))
			return

		case message, ok := <-cli.send:
			_ = conn.SetWriteDeadline(time.Now().Add(WebSocketWriteTimeout))
			if !ok {
				// send 被关闭（仅 shutdown 时发生）
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				utils.Info("[WS] WriteMessage 失败 UserID=%d err=%v", cli.userID, err)
				return
			}

		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(WebSocketWriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				utils.Info("[WS] Ping 失败 UserID=%d err=%v", cli.userID, err)
				return
			}
		}
	}
}

// handleMessage 仅做业务分发；任何 conn 写都走 cli.send。
func (h *WebSocketHandler) handleMessage(cli *wsClient, msg WebSocketMessage) {
	switch msg.Type {
	case "ping":
		resp := map[string]interface{}{
			"type":      "pong",
			"timestamp": time.Now().Unix(),
		}
		data, _ := json.Marshal(resp)
		select {
		case cli.send <- data:
		default:
			utils.Warn("[WS] pong 入队失败（send 缓冲满）UserID=%d", cli.userID)
		}

	case "message":
		var content map[string]interface{}
		if err := json.Unmarshal(msg.Content, &content); err != nil {
			utils.Info("[WS] Failed to parse message content: %v", err)
			return
		}
		content["timestamp"] = time.Now().Unix()
		content["sender_id"] = cli.userID
		out := map[string]interface{}{
			"type":    "message",
			"content": content,
		}
		data, _ := json.Marshal(out)
		h.enqueueBroadcast(data)

	case "broadcast":
		out := map[string]interface{}{
			"type":      "broadcast",
			"content":   msg.Content,
			"timestamp": time.Now().Unix(),
		}
		data, _ := json.Marshal(out)
		h.enqueueBroadcast(data)

	default:
		utils.Info("[WS] Unknown message type: %s", msg.Type)
	}
}

// SendToUser 向指定用户推送消息。非阻塞：send 满则丢弃（避免阻塞业务）。
func (h *WebSocketHandler) SendToUser(userID int64, messageType string, content interface{}) {
	h.RLock()
	cli, exists := h.userClients[userID]
	h.RUnlock()
	if !exists {
		utils.Info("用户 %d 未连接WebSocket，跳过推送", userID)
		return
	}
	data, err := h.encode(messageType, content)
	if err != nil {
		return
	}
	select {
	case cli.send <- data:
	default:
		utils.Info("SendToUser: 用户 %d send 缓冲满，丢弃", userID)
	}
}

// SendToAll 全员广播。非阻塞：hub 内部 fanout 处理 send 满的情况。
func (h *WebSocketHandler) SendToAll(messageType string, content interface{}) {
	data, err := h.encode(messageType, content)
	if err != nil {
		return
	}
	h.enqueueBroadcast(data)
}

func (h *WebSocketHandler) enqueueBroadcast(data []byte) {
	select {
	case h.broadcast <- data:
	case <-h.ctx.Done():
	default:
		// 广播队列满：丢弃（业务侧应改用 SendToUser 定向推送）
		utils.Warn("WebSocket broadcast queue full, dropping message")
	}
}

func (h *WebSocketHandler) encode(messageType string, content interface{}) ([]byte, error) {
	msg := map[string]interface{}{
		"type":      messageType,
		"content":   content,
		"timestamp": time.Now().Unix(),
	}
	return json.Marshal(msg)
}

func (h *WebSocketHandler) validateToken(tokenString string) int64 {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return h.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return 0
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return 0
	}

	return int64(userID)
}

// GetClientCount 返回当前已注册的 ws 连接数。
func (h *WebSocketHandler) GetClientCount() int {
	return int(h.connCount.Load())
}

// GetOnlineUserCount 返回当前在线用户数（按 userID 去重）。
func (h *WebSocketHandler) GetOnlineUserCount() int {
	h.RLock()
	defer h.RUnlock()
	return len(h.userClients)
}

// IsClosed 返回 hub 是否已调用过 Shutdown() 进入关停状态。
// nil-safe：nil 接收者视为已关闭（readyz 会据此判定 WS 不可用）。
// 实现上复用现有 atomic.Bool shutdown 状态，避免引入额外的标志。
func (h *WebSocketHandler) IsClosed() bool {
	if h == nil {
		return true
	}
	return h.shutdown.Load()
}

// Shutdown 安全关停：触发 ctx 取消 → hub 关闭所有连接 → 等 hub 退出。
// 不会 close channel，避免 send-on-closed panic。
func (h *WebSocketHandler) Shutdown() {
	if !h.shutdown.CompareAndSwap(false, true) {
		return
	}
	utils.Info("WebSocket 正在关闭...")
	h.cancel()
	h.hubWg.Wait()
	utils.Info("WebSocket 已关闭")
}

// closeWithCode 幂等关闭（sync.Once），通过 closeCode chan 通知 writePump 发送 CloseMessage，避免并发写入。
func (c *wsClient) closeWithCode(code int, reason string) {
	if !c.closedFlag.CompareAndSwap(false, true) {
		return
	}
	c.closeOnce.Do(func() {
		close(c.send) // 通知 writePump 退出
		// 发送关闭信号给 writePump（writePump 会发送 CloseMessage，然后关闭 closeDone）
		select {
		case c.closeCode <- code:
		default:
		}
		// 等待 writePump 退出并关闭连接（最长 15s 超时兜底，防止慢客户端阻塞）
		select {
		case <-c.closeDone:
		case <-time.After(15 * time.Second):
			utils.Warn("[WS] closeWithCode 超时，强制关闭 UserID=%d", c.userID)
			_ = c.conn.Close()
		}
	})
}
