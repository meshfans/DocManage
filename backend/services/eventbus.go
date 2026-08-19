package services

// ==================== 业务事件总线（EventBus）====================
//
// 设计目标：
//  1. 把"业务事件"从直接调 utils.IncBusinessEvent 升级为 publish/subscribe 总线
//  2. 内置 metrics subscriber：保证所有 event 仍能被 Prometheus 抓到
//     （保持向后兼容，Round 16 已记录的 28 种 event 名称不变）
//  3. 第三方 subscriber（webhook / audit / ws 等）可热插拔，无需改业务代码
//  4. 同步派发 + 失败容忍：subscriber panic 被 recover，单个失败不影响其它
//
// 为什么不引入第三方库（如 watney/event-bus / asaskevich/EventBus）：
//   - 当前只需要同步 fan-out，goroutine 编排暂不需要
//   - 维护一个 80 行的文件比依赖一个 50k star 的库更可控
//   - 后续如需异步 / 持久化 / 跨实例，再评估官方 NATS / Redis Streams
//
// 与现有代码的关系：
//   - utils.IncBusinessEvent 仍保留，新代码改用 services.PublishEvent
//   - DB 审计 / WS 推送 / Webhook 出站（未来）只需 RegisterSubscriber
//   - 测试：eventbus_test.go 覆盖订阅/取消订阅/panic 隔离/并发安全
// ----------------------------------------------------------------------------

import (
	"sync"
	"sync/atomic"

	"doc/utils"
)

// Event 业务事件通用结构。
//
//   - Name: 事件名（如 "auth.login.success" / "backup.manual.success"）
//     命名规范：<target>.<action>.<result>，与 Round 16 业务事件指标名一致
//   - Target: 业务对象类型（"customer" / "backup" / "auth" / ...），可空
//   - TargetID: 业务对象 id，可空
//   - ActorID: 操作人 id（0 = 系统/匿名）
//   - Detail: 业务附加信息（map），subscriber 按需使用
//
// 设计点：避免在 Event 里塞大对象（文件、bytes）。subscriber 需要大量上下文
// 时，按 Name 单独查 DB；这与 append-only audit 的设计一致。
type Event struct {
	Name     string                 `json:"name"`
	Target   string                 `json:"target,omitempty"`
	TargetID int64                  `json:"target_id,omitempty"`
	ActorID  int64                  `json:"actor_id,omitempty"`
	Detail   map[string]interface{} `json:"detail,omitempty"`
}

// Subscriber 事件订阅者。
// 返回 error 当前不会暴露给 publisher（失败仅 log），但保留接口以便后续
// 接入持久化 / 死信队列时直接复用。
type Subscriber func(e Event) error

// eventBus 内部实现。
//
// subscriber 用 map[id]Subscriber 保存：
//   - O(1) 取消订阅（直接 delete）
//   - 不会因为切片 append 触发地址失效
//   - 派发时按注册顺序不强制（map 随机），但 subscriber 应当独立无序
type eventBus struct {
	mu          sync.RWMutex
	subscribers map[uint64]Subscriber
	nextID      atomic.Uint64
	published   atomic.Uint64 // 累计发布次数（debug / 监控用）
	dropped     atomic.Uint64 // 因 subscriber 失败/panic 导致的 drop 次数
}

// 全局单例（进程内唯一）。
//
// 选全局变量而非依赖注入的原因：
//   - 业务模块（handler / service / scheduler）都会发事件，注入链路会拉得很绕
//   - 测试用 resetForTest() 局部清理，不影响全局行为
var defaultBus = &eventBus{
	subscribers: make(map[uint64]Subscriber),
}

// Default 返回全局事件总线。
func Default() *eventBus {
	return defaultBus
}

// RegisterSubscriber 注册一个订阅者，返回取消函数。
//
// 注意：subscriber 应当轻量且非阻塞。当前实现是同步派发，panic 会触发
// recover 不影响其他订阅者，但耗时操作会拖慢调用方。
func (b *eventBus) RegisterSubscriber(s Subscriber) func() {
	id := b.nextID.Add(1)
	b.mu.Lock()
	b.subscribers[id] = s
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
}

// Publish 发布事件：同步调用所有 subscriber，失败/panic 隔离。
//
// 返回值：实际触达的 subscriber 数量（用于测试断言）。
func (b *eventBus) Publish(e Event) int {
	b.mu.RLock()
	subs := make([]Subscriber, 0, len(b.subscribers))
	for _, s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	b.published.Add(1)
	delivered := 0
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		delivered++
		// 每个 subscriber 独立 recover，单个 panic 不影响其他
		func() {
			defer func() {
				if r := recover(); r != nil {
					b.dropped.Add(1)
					// 不打印细节，避免日志注入；后续若需要可换成
					// utils.ErrorWithDetail，但当前 publisher 无 logger 句柄
					_ = r
				}
			}()
			_ = sub(e)
		}()
	}
	return delivered
}

// PublishSimple 便捷发布：只指定事件名。
func (b *eventBus) PublishSimple(name string) {
	b.Publish(Event{Name: name})
}

// Published 返回累计发布次数（测试 / 监控用）。
func (b *eventBus) Published() uint64 {
	return b.published.Load()
}

// Dropped 返回累计失败次数（panic 隔离计数）。
func (b *eventBus) Dropped() uint64 {
	return b.dropped.Load()
}

// SubscriberCount 返回当前订阅者数量（监控 / 测试）。
func (b *eventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// init：注册内置的 metrics subscriber。
//
// 兼容点：业务代码原来调 utils.IncBusinessEvent("xxx")，
// 改用 services.PublishEvent("xxx") 后，所有 Prometheus 业务事件指标
// 会由这个 subscriber 继续累加，零迁移成本。
func init() {
	Default().RegisterSubscriber(metricsSubscriber)
}

// metricsSubscriber 内置订阅者：把 Event 同步到 Prometheus 业务事件指标。
//
// 命名兼容：Event.Name 直接复用为指标 label event=，确保原有 dashboard
// / alert 规则（label event="auth.login.success"）无需任何改动。
func metricsSubscriber(e Event) error {
	if e.Name == "" {
		return nil
	}
	utils.IncBusinessEvent(e.Name)
	return nil
}

// PublishEvent 是 Default().PublishSimple 的便捷别名，业务代码直接调。
func PublishEvent(name string) {
	Default().PublishSimple(name)
}

// PublishEventWith 发布完整事件（带 meta），业务代码按需调。
func PublishEventWith(e Event) {
	Default().Publish(e)
}
