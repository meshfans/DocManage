package services

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestEventBus_PublishDeliversToSubscribers 验证事件能命中所有订阅者。
func TestEventBus_PublishDeliversToSubscribers(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var got atomic.Int32
	s1 := bus.RegisterSubscriber(func(e Event) error {
		if e.Name != "user.created" {
			t.Errorf("unexpected event name: %s", e.Name)
		}
		got.Add(1)
		return nil
	})
	defer s1()

	bus.RegisterSubscriber(func(e Event) error {
		got.Add(1)
		return nil
	})

	delivered := bus.Publish(Event{Name: "user.created"})
	if delivered != 2 {
		t.Errorf("delivered = %d, want 2", delivered)
	}
	if got.Load() != 2 {
		t.Errorf("got = %d, want 2", got.Load())
	}
}

// TestEventBus_UnsubscribeStopsDelivery 验证取消订阅后不再派发。
func TestEventBus_UnsubscribeStopsDelivery(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var count atomic.Int32
	unsub := bus.RegisterSubscriber(func(e Event) error {
		count.Add(1)
		return nil
	})
	bus.Publish(Event{Name: "first"})
	if count.Load() != 1 {
		t.Fatalf("after first publish: count = %d, want 1", count.Load())
	}

	unsub()
	if bus.SubscriberCount() != 0 {
		t.Errorf("after unsubscribe: count = %d, want 0", bus.SubscriberCount())
	}

	bus.Publish(Event{Name: "second"})
	if count.Load() != 1 {
		t.Errorf("after second publish: count = %d, want 1 (no further delivery)", count.Load())
	}
}

// TestEventBus_SubscriberPanicIsolated 验证单个 subscriber panic 不影响其他。
func TestEventBus_SubscriberPanicIsolated(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var after atomic.Bool
	bus.RegisterSubscriber(func(e Event) error {
		panic("boom")
	})
	bus.RegisterSubscriber(func(e Event) error {
		after.Store(true)
		return nil
	})

	delivered := bus.Publish(Event{Name: "test"})
	if delivered != 2 {
		t.Errorf("delivered = %d, want 2", delivered)
	}
	if !after.Load() {
		t.Error("subscriber after panic did not run")
	}
	if bus.Dropped() != 1 {
		t.Errorf("Dropped = %d, want 1", bus.Dropped())
	}
}

// TestEventBus_SubscriberErrorIgnored 验证 subscriber 返回 error 不会中断派发。
func TestEventBus_SubscriberErrorIgnored(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var after atomic.Bool
	bus.RegisterSubscriber(func(e Event) error {
		return errors.New("nope")
	})
	bus.RegisterSubscriber(func(e Event) error {
		after.Store(true)
		return nil
	})

	delivered := bus.Publish(Event{Name: "test"})
	if delivered != 2 {
		t.Errorf("delivered = %d, want 2 (error should not abort fan-out)", delivered)
	}
	if !after.Load() {
		t.Error("subscriber after error did not run")
	}
}

// TestEventBus_ConcurrentPublish 安全：并发 publish 不应触发 race。
func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var counter atomic.Int64
	bus.RegisterSubscriber(func(e Event) error {
		counter.Add(1)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(Event{Name: "concurrent"})
		}()
	}
	wg.Wait()

	if counter.Load() != 100 {
		t.Errorf("counter = %d, want 100", counter.Load())
	}
	if bus.Published() != 100 {
		t.Errorf("Published = %d, want 100", bus.Published())
	}
}

// TestEventBus_EmptyNameIgnored 验证 metricsSubscriber 风格的 name=="" 短路。
func TestEventBus_EmptyNameIgnored(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var got atomic.Int32
	bus.RegisterSubscriber(func(e Event) error {
		got.Add(1)
		return nil
	})

	// 即使订阅者接受空 name，metricsSubscriber 内部会先过滤。
	// 这里直接验证 Publish 本身不抛 panic。
	delivered := bus.Publish(Event{Name: ""})
	if delivered != 1 {
		t.Errorf("delivered = %d, want 1", delivered)
	}
	if got.Load() != 1 {
		t.Errorf("got = %d, want 1", got.Load())
	}
}

// TestEventBus_PublishSimple 与 Publish(E{Name}) 等价。
func TestEventBus_PublishSimple(t *testing.T) {
	bus := &eventBus{subscribers: make(map[uint64]Subscriber)}

	var seen atomic.Pointer[Event]
	bus.RegisterSubscriber(func(e Event) error {
		seen.Store(&e)
		return nil
	})

	bus.PublishSimple("auth.login.success")
	got := seen.Load()
	if got == nil {
		t.Fatal("event not delivered")
	}
	if got.Name != "auth.login.success" {
		t.Errorf("Name = %s, want auth.login.success", got.Name)
	}
}
