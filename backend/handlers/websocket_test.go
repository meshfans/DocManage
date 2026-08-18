package handlers

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"doc/utils"

	"github.com/gorilla/websocket"
)

// Round 16 修正测试：覆盖以下不变量
//  1) nil receiver 安全性（SendToUser / SendToAll / IsClosed / GetClientCount /
//     GetOnlineUserCount 不 panic）
//  2) failed 计数不重复（同一事件路径只计一次）
//  3) handleUnregister 幂等（重复 unregister 同一个 client 不会双扣 connCount）
//  4) 同 userID 替换旧连接时 connCount 不会双重扣减
//  5) handleRegister / handleUnregister 的"锁内不取 RLock"不变量
//     （间接通过 timeout 验证：原 bug 会在 Lock 内 RLock → 死锁 → timeout）
//
// 设计要点：测试不真正拨号 / upgrade websocket。所有 wsClient.conn 用 nil 占位，
// closeDone 预关闭，让 closeWithCode 立即返回（不依赖 writePump）。

// newTestWsClient 构造一个用于 handle* 直接调用的 wsClient。
//   - conn: nil（不拨号）
//   - closeDone: 预关闭（让 closeWithCode 立刻返回，避免 15s 兜底超时）
//   - send / closeCode: 带缓冲，与正常流程一致
func newTestWsClient(userID int64) *wsClient {
	closeDone := make(chan struct{})
	close(closeDone)
	return &wsClient{
		conn:      nil,
		userID:    userID,
		authToken: "",
		send:      make(chan []byte, WebSocketSendBuffer),
		closeCode: make(chan int, 1),
		closeDone: closeDone,
	}
}

// newTestHub 构造一个测试用 hub，不启动 Run() 循环（直接调 handle*）。
func newTestHub() *WebSocketHandler {
	return NewWebSocketHandler([]byte("test-jwt-secret"))
}

// ==================== nil receiver 安全性 ====================

func TestSendToUser_NilReceiver_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("(*WebSocketHandler)(nil).SendToUser 不应 panic：%v", r)
		}
	}()
	var h *WebSocketHandler
	h.SendToUser(42, "test", map[string]string{"k": "v"})
}

func TestSendToAll_NilReceiver_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("(*WebSocketHandler)(nil).SendToAll 不应 panic：%v", r)
		}
	}()
	var h *WebSocketHandler
	h.SendToAll("test", map[string]string{"k": "v"})
}

func TestIsClosed_NilReceiver_ReturnsTrue(t *testing.T) {
	var h *WebSocketHandler
	if !h.IsClosed() {
		t.Fatalf("(*WebSocketHandler)(nil).IsClosed 应返回 true")
	}
}

// ==================== failed 计数不重复 ====================

// readCounterValue 从 DefaultRegistry text 输出里读某 counter 的当前值。
// handlers 包不能直接访问 utils 包的私有 counter 变量，统一通过 text 协议读。
func readCounterValue(t *testing.T, metric string) float64 {
	t.Helper()
	var buf bytes.Buffer
	if err := utils.DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("DefaultRegistry.WriteTo 失败：%v", err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "# ") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, metric+" ") {
			continue
		}
		// 形如 "ws_messages_failed_total 5"
		var v float64
		n, err := fmt.Sscanf(line, metric+" %f", &v)
		if err != nil || n != 1 {
			return 0
		}
		return v
	}
	return 0
}

func TestSendToUser_OfflineUser_FailedCounterOnce(t *testing.T) {
	hub := newTestHub()

	before := readCounterValue(t, "ws_messages_failed_total")
	hub.SendToUser(12345, "test", map[string]string{"k": "v"})
	after := readCounterValue(t, "ws_messages_failed_total")

	if got := int(after - before); got != 1 {
		t.Fatalf("离线用户 SendToUser 应只记 1 次 failed，实际 %d", got)
	}
}

func TestSendToAll_IncrementsFailedOnceForEncodeFailure(t *testing.T) {
	// 编码失败：用 func 值不可 marshal，强制 json.Marshal 返回 error
	hub := newTestHub()

	before := readCounterValue(t, "ws_messages_failed_total")
	hub.SendToAll("test", func() {}) // func 不可 marshal → 编码失败
	after := readCounterValue(t, "ws_messages_failed_total")

	if got := int(after - before); got != 1 {
		t.Fatalf("编码失败路径应只记 1 次 failed，实际 %d", got)
	}
}

// enqueueBroadcast 队列满时只记 1 次 failed（Round 16 语义）。
func TestEnqueueBroadcast_QueueFull_FailedOnce(t *testing.T) {
	hub := newTestHub()
	// 替换 broadcast channel 为无缓冲的，使下次发送命中 default 分支
	hub.broadcast = make(chan []byte) // unbuffered，无人接收 → default

	before := readCounterValue(t, "ws_messages_failed_total")
	hub.enqueueBroadcast([]byte(`{"hello":"world"}`))
	after := readCounterValue(t, "ws_messages_failed_total")

	if got := int(after - before); got != 1 {
		t.Fatalf("broadcast 队列满应只记 1 次 failed，实际 %d", got)
	}
}

// ==================== handleUnregister 幂等 ====================

// TestHandleUnregister_UnknownClient_NoConnCountDelta 验证对一个从未注册的 client
// 调用 handleUnregister 不会让 connCount 变成负数（idempotent early return）。
func TestHandleUnregister_UnknownClient_NoConnCountDelta(t *testing.T) {
	hub := newTestHub()
	c := newTestWsClient(7)

	before := hub.GetClientCount()
	hub.handleUnregister(c)
	after := hub.GetClientCount()

	if before != after {
		t.Fatalf("未注册 client 走 unregister 不应改变 connCount：before=%d after=%d", before, after)
	}
	if after < 0 {
		t.Fatalf("connCount 不应变为负数：%d", after)
	}
}

// TestHandleUnregister_DoubleCall_Idempotent 验证同一个 client 两次 unregister
// 只会扣一次 connCount（closeOnce + 集合幂等）。
func TestHandleUnregister_DoubleCall_Idempotent(t *testing.T) {
	hub := newTestHub()
	c := newTestWsClient(7)

	hub.handleRegister(c)
	if got := hub.GetClientCount(); got != 1 {
		t.Fatalf("register 后 connCount 期望 1，实际 %d", got)
	}

	// 第一次 unregister：扣 -1
	hub.handleUnregister(c)
	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("第一次 unregister 后 connCount 期望 0，实际 %d", got)
	}

	// 第二次 unregister：幂等返回，connCount 不变
	hub.handleUnregister(c)
	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("第二次 unregister 后 connCount 应仍为 0（幂等），实际 %d", got)
	}
}

// ==================== 同 userID 替换：connCount 不双重扣减 ====================

// TestHandleRegister_ReplaceOld_NoDoubleDecrement 验证同一 userID 的新连接替换旧连接时：
//   - 旧连接从 hub 索引删除并 connCount -1
//   - 新连接 register 时 connCount +1
//   - 旧连接后续的 readPump 触发 handleUnregister 应幂等（不再 -1）
//
// 最终 connCount 应为 1，而非 0。
func TestHandleRegister_ReplaceOld_NoDoubleDecrement(t *testing.T) {
	hub := newTestHub()

	old := newTestWsClient(99)
	newC := newTestWsClient(99)

	hub.handleRegister(old)
	if got := hub.GetClientCount(); got != 1 {
		t.Fatalf("旧连接 register 后 connCount 期望 1，实际 %d", got)
	}

	// 新连接 register，触发替换
	hub.handleRegister(newC)
	if got := hub.GetClientCount(); got != 1 {
		t.Fatalf("替换后 connCount 期望仍为 1（新 -1 +1），实际 %d", got)
	}

	// 旧连接后续 readPump 退出 → handleUnregister：因 old 已不在 clients map，幂等返回
	hub.handleUnregister(old)
	if got := hub.GetClientCount(); got != 1 {
		t.Fatalf("旧连接 unregister 后 connCount 应仍为 1（幂等），实际 %d", got)
	}

	// 最后注销新连接
	hub.handleUnregister(newC)
	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("新连接 unregister 后 connCount 期望 0，实际 %d", got)
	}
}

// ==================== lock ordering：handle* 不在 Lock 内调 Get* ====================

// runWithTimeout 在指定时长内跑完 fn；超时则 fail（间接覆盖"Lock 内 RLock 死锁"）。
// 如果原 bug 在 hub 锁内嵌套 RLock，本调用会卡死直到 timeout。
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("操作在 %v 内未完成（疑似死锁）", d)
	}
}

// TestHandleRegister_NoLockReentry_DoesNotHang 间接验证：调用 handleRegister
// 不会因为在 Lock 内再 RLock 而死锁。原 bug 会在 1-2 次调用内挂住 hub。
func TestHandleRegister_NoLockReentry_DoesNotHang(t *testing.T) {
	hub := newTestHub()

	runWithTimeout(t, 5*time.Second, func() {
		for i := 0; i < 20; i++ {
			c := newTestWsClient(int64(i))
			hub.handleRegister(c)
		}
	})

	if got := hub.GetClientCount(); got != 20 {
		t.Fatalf("20 次 register 后 connCount 期望 20，实际 %d", got)
	}
}

func TestHandleUnregister_NoLockReentry_DoesNotHang(t *testing.T) {
	hub := newTestHub()
	clients := make([]*wsClient, 20)
	for i := 0; i < 20; i++ {
		clients[i] = newTestWsClient(int64(i))
		hub.handleRegister(clients[i])
	}

	runWithTimeout(t, 5*time.Second, func() {
		for i := 0; i < 20; i++ {
			hub.handleUnregister(clients[i])
		}
	})

	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("20 次 unregister 后 connCount 期望 0，实际 %d", got)
	}
}

// TestHandleRegisterMetricsRefresh_DoesNotHang 验证 register 后内部 SetWebSocketMetrics
// 路径（释放锁 → 调 setter）不会因为锁顺序问题卡死。
func TestHandleRegisterMetricsRefresh_DoesNotHang(t *testing.T) {
	hub := newTestHub()

	// userID > 0 才能进入 userClients 分支（userID == 0 不计入 online users）。
	// 这里从 1 开始，确保 50 个 client 都按 userID 去重计数。
	runWithTimeout(t, 5*time.Second, func() {
		for i := 1; i <= 50; i++ {
			c := newTestWsClient(int64(i))
			hub.handleRegister(c)
		}
	})

	if got := hub.GetClientCount(); got != 50 {
		t.Fatalf("50 次 register 后 connCount 期望 50，实际 %d", got)
	}
	if got := hub.GetOnlineUserCount(); got != 50 {
		t.Fatalf("50 次 register 后在线用户数期望 50，实际 %d", got)
	}
}

// ==================== 并发 register / unregister（race detector）====================

// TestConcurrent_RegisterUnregister_NoRace 验证并发 register / unregister 不出现
// data race；死锁会通过 timeout 暴露。
func TestConcurrent_RegisterUnregister_NoRace(t *testing.T) {
	hub := newTestHub()

	const (
		writers      = 8
		opsPerWriter = 100
	)
	var wg sync.WaitGroup
	wg.Add(writers)

	// 用 atomic.Int64 跟踪已注册 client 数量，便于校验
	var registered atomic.Int64

	runWithTimeout(t, 10*time.Second, func() {
		for i := 0; i < writers; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < opsPerWriter; j++ {
					uid := int64(id*opsPerWriter + j)
					c := newTestWsClient(uid)
					hub.handleRegister(c)
					registered.Add(1)
					// 立即注销，模拟短连接
					hub.handleUnregister(c)
					registered.Add(-1)
				}
			}(i)
		}
		wg.Wait()
	})

	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("并发 register/unregister 全部完成后 connCount 期望 0，实际 %d", got)
	}
	if got := hub.GetOnlineUserCount(); got != 0 {
		t.Fatalf("并发 register/unregister 全部完成后 online users 期望 0，实际 %d", got)
	}
}

// TestConcurrent_RegisterSameUserID_NoRace 验证同一 userID 并发注册新连接
// （单点登录替换）不会 data race；最终 connCount 等于最终注册的客户端数。
func TestConcurrent_RegisterSameUserID_NoRace(t *testing.T) {
	hub := newTestHub()

	const (
		writers      = 4
		opsPerWriter = 50
	)
	var wg sync.WaitGroup
	wg.Add(writers)
	var last atomic.Pointer[wsClient]

	runWithTimeout(t, 10*time.Second, func() {
		for i := 0; i < writers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < opsPerWriter; j++ {
					c := newTestWsClient(1) // 同一 userID
					hub.handleRegister(c)
					last.Store(c)
				}
			}()
		}
		wg.Wait()
	})

	// 最终应只剩 1 个 client（最近一次注册的）
	if got := hub.GetClientCount(); got != 1 {
		t.Fatalf("同一 userID 并发替换后 connCount 期望 1，实际 %d", got)
	}
	if got := hub.GetOnlineUserCount(); got != 1 {
		t.Fatalf("同一 userID 并发替换后 online users 期望 1，实际 %d", got)
	}

	// 注销最后一次注册
	if c := last.Load(); c != nil {
		hub.handleUnregister(c)
	}
	if got := hub.GetClientCount(); got != 0 {
		t.Fatalf("注销最后一个 client 后 connCount 期望 0，实际 %d", got)
	}
}

// ==================== 编译期检查：closeWithCode 的 15s 兜底分支不能被触发 ====================

// 校验测试用的 wsClient.closeDone 已预先关闭，closeWithCode 立即返回。
// 这保证上面所有 handle* 测试不会因为 15s 超时兜底卡住测试。
func TestTestHelper_CloseWithCodeReturnsImmediately(t *testing.T) {
	c := newTestWsClient(1)
	done := make(chan struct{})
	go func() {
		c.closeWithCode(websocket.CloseNormalClosure, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("closeWithCode 未在 2s 内返回（pre-close closeDone 失效？）")
	}
}
