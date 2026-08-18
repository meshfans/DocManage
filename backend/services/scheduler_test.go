package services

// Round 16：Scheduler 基础设施指标工具测试。
// 重点验证 nil-safe 与 running map 计数的并发安全。

import (
	"sync"
	"testing"
)

// TestRunningTaskCount_NilSafe 验证 nil 接收者返回 0，不 panic。
func TestRunningTaskCount_NilSafe(t *testing.T) {
	var s *Scheduler
	if got := s.RunningTaskCount(); got != 0 {
		t.Fatalf("nil scheduler RunningTaskCount 期望 0，实际 %d", got)
	}
	if got := s.ListEntriesLen(); got != 0 {
		t.Fatalf("nil scheduler ListEntriesLen 期望 0，实际 %d", got)
	}
	if got := s.ListEntries(); got != nil {
		t.Fatalf("nil scheduler ListEntries 期望 nil，实际 %v", got)
	}
}

// TestRunningTaskCount_TracksRunningMap 验证 running map 变化被正确计数。
// 这里手动模拟 Scheduler 结构，但只关心 running map 与 mu 锁；
// cron 字段保持 nil（ListEntriesLen 走 nil-safe 分支返回 0）。
func TestRunningTaskCount_TracksRunningMap(t *testing.T) {
	s := &Scheduler{
		running: make(map[int64]bool),
	}

	if got := s.RunningTaskCount(); got != 0 {
		t.Fatalf("初始 RunningTaskCount 期望 0，实际 %d", got)
	}

	s.mu.Lock()
	s.running[1] = true
	s.running[2] = true
	s.mu.Unlock()
	if got := s.RunningTaskCount(); got != 2 {
		t.Fatalf("标记 2 个任务后期望 2，实际 %d", got)
	}

	s.mu.Lock()
	delete(s.running, 1)
	s.mu.Unlock()
	if got := s.RunningTaskCount(); got != 1 {
		t.Fatalf("删除 1 个后期望 1，实际 %d", got)
	}
}

// TestRunningTaskCount_ConcurrentSafe 验证 RunningTaskCount 在并发读写下安全。
func TestRunningTaskCount_ConcurrentSafe(t *testing.T) {
	s := &Scheduler{
		running: make(map[int64]bool),
	}

	const writers = 8
	const iters = 100

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(base int64) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				id := base*int64(iters) + int64(j)
				s.mu.Lock()
				s.running[id] = true
				s.mu.Unlock()
				_ = s.RunningTaskCount()
				s.mu.Lock()
				delete(s.running, id)
				s.mu.Unlock()
			}
		}(int64(i))
	}

	// 同时并发读
	var wgRead sync.WaitGroup
	wgRead.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wgRead.Done()
			for j := 0; j < iters*writers; j++ {
				_ = s.RunningTaskCount()
			}
		}()
	}
	wg.Wait()
	wgRead.Wait()

	// 收尾：所有任务都已被 delete
	if got := s.RunningTaskCount(); got != 0 {
		t.Fatalf("全部释放后期望 0，实际 %d", got)
	}
}
