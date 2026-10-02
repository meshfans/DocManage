package machinecode

// 机器码稳定性测试（串行 + 并发），2026-10-01。
//
// 稳定性是硬件绑定的地基：机器码每次启动都必须算出**同一个值**，
// 否则会把同一台机器误判成"换了机器"→ 客户被无故锁死。
//
// 覆盖三件事：
//  1. 串行重复调用：同一进程内连续 Compute() 必须完全一致
//  2. 并发调用：多 goroutine 同时 Compute() 结果必须一致，且无 data race
//  3. 结果与首次一致：并发跑完后再串行取一次，必须与首值相同
//
// 第 2 条建议配合 `go test -race` 运行，能捕获包内共享状态的竞态。

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestCompute_SerialStable 串行稳定性：连续调用必须返回同一个机器码。
func TestCompute_SerialStable(t *testing.T) {
	first, err := Compute()
	if err != nil {
		// 受限环境（容器 / 无 DMI）算不出是预期内的，跳过而非失败
		t.Skipf("当前环境无法计算机器码（预期内）：%v", err)
	}

	const n = 50
	for i := 0; i < n; i++ {
		got, err := Compute()
		if err != nil {
			t.Fatalf("第 %d 次 Compute() 失败：%v", i, err)
		}
		if got.MachineCode != first.MachineCode {
			t.Fatalf("第 %d 次结果漂移：\n  首次 = %s (tier %d, %s)\n  本次 = %s (tier %d, %s)",
				i,
				first.MachineCode, first.Tier, first.Source,
				got.MachineCode, got.Tier, got.Source)
		}
		if got.Tier != first.Tier {
			t.Errorf("第 %d 次命中层级变化：首次 tier=%d，本次 tier=%d（字段可用性波动？）", i, first.Tier, got.Tier)
		}
	}
	t.Logf("串行 %d 次全部一致：%s (tier %d — %s)", n+1, first.MachineCode, first.Tier, first.Source)
}

// TestCompute_ConcurrentStable 并发稳定性：多 goroutine 同时调用，
// 所有结果必须一致，且在 -race 下无数据竞态。
func TestCompute_ConcurrentStable(t *testing.T) {
	baseline, err := Compute()
	if err != nil {
		t.Skipf("当前环境无法计算机器码（预期内）：%v", err)
	}

	const (
		goroutines = 32
		perG       = 20
	)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		bad  []string
		fast = 0
	)
	start := make(chan struct{})

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			<-start // 让所有 goroutine 同时起跑，最大化竞争窗口
			for i := 0; i < perG; i++ {
				got, err := Compute()
				mu.Lock()
				if err != nil {
					bad = append(bad, fmt.Sprintf("g%d/i%d: Compute() 失败: %v", gid, i, err))
				} else if got.MachineCode != baseline.MachineCode {
					bad = append(bad, fmt.Sprintf("g%d/i%d: 得到 %s，期望 %s", gid, i, got.MachineCode, baseline.MachineCode))
				} else if got.Tier != baseline.Tier {
					bad = append(bad, fmt.Sprintf("g%d/i%d: tier=%d，期望 %d", gid, i, got.Tier, baseline.Tier))
				}
				mu.Unlock()
			}
		}(g)
	}
	close(start)
	wg.Wait()

	if len(bad) > 0 {
		t.Fatalf("并发下出现 %d 处不一致：\n  %s", len(bad), strings.Join(bad[:min(len(bad), 10)], "\n  "))
	}
	t.Logf("并发 %d goroutine × %d 次 = %d 次调用全部一致：%s",
		goroutines, perG, goroutines*perG, baseline.MachineCode)

	// 并发跑完后再串行取一次，确认没有残留状态影响
	after, err := Compute()
	if err != nil {
		t.Fatalf("并发后 Compute() 失败：%v", err)
	}
	if after.MachineCode != baseline.MachineCode {
		t.Fatalf("并发结束后结果漂移：%s → %s", baseline.MachineCode, after.MachineCode)
	}
	_ = fast
}

// TestCompute_ConcurrentVsSerialCrossCheck 并发与串行交叉验证：
// 串行取首值 → 并发取 N 次 → 串行取尾值，三者必须全部相同。
func TestCompute_ConcurrentVsSerialCrossCheck(t *testing.T) {
	before, err := Compute()
	if err != nil {
		t.Skipf("当前环境无法计算机器码（预期内）：%v", err)
	}

	const n = 64
	results := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if got, err := Compute(); err == nil {
				results[i] = got.MachineCode
			} else {
				results[i] = "ERR:" + err.Error()
			}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r != before.MachineCode {
			t.Fatalf("第 %d 个并发结果与串行首值不一致：并发=%s 串行=%s", i, r, before.MachineCode)
		}
	}

	after, err := Compute()
	if err != nil {
		t.Fatalf("尾值 Compute() 失败：%v", err)
	}
	if after.MachineCode != before.MachineCode {
		t.Fatalf("串行尾值与首值不一致：%s → %s", before.MachineCode, after.MachineCode)
	}
	t.Logf("串行首值 = 并发 %d 次 = 串行尾值 = %s", n, before.MachineCode)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
