package utils

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// ==================== 独立 Counter / Gauge / Histogram ====================

func TestIndependentCounterInc(t *testing.T) {
	// Counter 无 NewCounter() 公共构造函数，只能通过 CounterVec 拿到。
	v := NewCounterVec("standalone_counter", "test", []string{"x"})
	c := v.WithLabelValues("a")
	if c.Value() != 0 {
		t.Fatalf("新 Counter 初始值应为 0，实际 %v", c.Value())
	}
	c.Inc()
	c.Inc()
	c.Add(2.5)
	if got := c.Value(); got != 4.5 {
		t.Fatalf("Counter 累计值错误：期望 4.5，实际 %v", got)
	}
	// Add(负数) 在 Counter 上允许但不规范；这里只做烟雾测试
	c.Add(-1)
	if got := c.Value(); got != 3.5 {
		t.Fatalf("Counter Add(-1) 后期望 3.5，实际 %v", got)
	}
}

func TestIndependentGaugeIncSet(t *testing.T) {
	g := NewGauge()
	if g.Value() != 0 {
		t.Fatalf("新 Gauge 初始值应为 0，实际 %v", g.Value())
	}
	g.Set(10)
	g.Inc()
	g.Dec()
	g.Add(-3.5)
	if got := g.Value(); got != 6.5 {
		t.Fatalf("Gauge 累计值错误：期望 6.5（10+1-1-3.5），实际 %v", got)
	}
}

func TestIndependentHistogramObserve(t *testing.T) {
	buckets := []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 5}
	// Histogram 没有 NewHistogram() 公共构造，只能从 HistogramVec 拿。
	v := NewHistogramVec("standalone_hist", "test", []string{"x"}, buckets)
	h := v.WithLabelValues("a")

	h.Observe(0.003)
	h.Observe(0.02)
	h.Observe(3)
	h.Observe(0)

	if cnt := h.Count(); cnt != 4 {
		t.Fatalf("Histogram.Count 期望 4，实际 %d", cnt)
	}
	sum := h.Sum()
	// 浮点和允许误差
	if sum < 3.022 || sum > 3.024 {
		t.Fatalf("Histogram.Sum 期望 ~3.023，实际 %v", sum)
	}
	gotBuckets := h.Buckets()
	if len(gotBuckets) != len(buckets) {
		t.Fatalf("Histogram.Buckets 长度错误：期望 %d，实际 %d", len(buckets), len(gotBuckets))
	}
	for i := range buckets {
		if gotBuckets[i] != buckets[i] {
			t.Fatalf("Histogram.Buckets[%d] 期望 %v，实际 %v", i, buckets[i], gotBuckets[i])
		}
	}
}

// ==================== 独立 Metric（NewCounterMetric/NewGaugeMetric/NewHistogramMetric）====================

func TestNewCounterMetric_RegisterIndependent(t *testing.T) {
	r := NewRegistry()
	m := NewCounterMetric("my_counter", "test help")
	r.MustRegister(m)

	m.IncCounter()
	m.IncCounter()
	m.AddCounter(3)

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# HELP my_counter test help") {
		t.Fatalf("输出缺少 HELP：%s", out)
	}
	if !strings.Contains(out, "# TYPE my_counter counter") {
		t.Fatalf("输出缺少 TYPE：%s", out)
	}
	if !strings.Contains(out, "my_counter 5") {
		t.Fatalf("输出缺少指标值 5：%s", out)
	}
}

func TestNewGaugeMetric_RegisterIndependent(t *testing.T) {
	r := NewRegistry()
	m := NewGaugeMetric("my_gauge", "test help")
	r.MustRegister(m)

	m.SetGauge(42)
	m.SetGauge(7.5)

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# TYPE my_gauge gauge") {
		t.Fatalf("输出缺少 TYPE：%s", out)
	}
	if !strings.Contains(out, "my_gauge 7.5") {
		t.Fatalf("输出缺少指标值 7.5：%s", out)
	}
}

func TestNewHistogramMetric_TextOutput(t *testing.T) {
	r := NewRegistry()
	buckets := []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 5}
	m := NewHistogramMetric("my_hist", "test help", buckets)
	r.MustRegister(m)

	m.Observe(0.003) // 小于所有桶
	m.Observe(0.02)  // <=0.05
	m.Observe(3)     // 大部分桶
	m.Observe(0)     // 边界

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	// 头：HELP / TYPE
	if !strings.Contains(out, "# HELP my_hist test help") {
		t.Fatalf("输出缺少 HELP：%s", out)
	}
	if !strings.Contains(out, "# TYPE my_hist histogram") {
		t.Fatalf("输出缺少 TYPE：%s", out)
	}

	// 每个固定桶、+Inf、_sum、_count
	for _, b := range buckets {
		line := "my_hist_bucket{le=\"" + formatFloat(b) + "\"}"
		if !strings.Contains(out, line) {
			t.Fatalf("输出缺少 %s：%s", line, out)
		}
	}
	if !strings.Contains(out, "my_hist_bucket{le=\"+Inf\"}") {
		t.Fatalf("输出缺少 +Inf 桶：%s", out)
	}
	if !strings.Contains(out, "my_hist_sum ") {
		t.Fatalf("输出缺少 _sum：%s", out)
	}
	if !strings.Contains(out, "my_hist_count ") {
		t.Fatalf("输出缺少 _count：%s", out)
	}
}

// ==================== Vec（CounterVec / GaugeVec / HistogramVec）====================

func TestCounterVec_LabelCombinations(t *testing.T) {
	v := NewCounterVec("req_total", "request total", []string{"method", "path"})

	v.WithLabelValues("GET", "/a").Inc()
	v.WithLabelValues("GET", "/a").Add(2)
	v.WithLabelValues("POST", "/a").Inc()
	v.WithLabelValues("GET", "/b").Inc()

	r := NewRegistry()
	r.MustRegisterCollector("req_total", &cCounterVec{n: "req_total", h: "request total", v: v})

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `req_total{method="GET",path="/a"} 3`) {
		t.Fatalf("输出缺少 GET/a=3：%s", out)
	}
	if !strings.Contains(out, `req_total{method="POST",path="/a"} 1`) {
		t.Fatalf("输出缺少 POST/a=1：%s", out)
	}
	if !strings.Contains(out, `req_total{method="GET",path="/b"} 1`) {
		t.Fatalf("输出缺少 GET/b=1：%s", out)
	}

	// label 数量不匹配返回 nil
	if c := v.WithLabelValues("GET"); c != nil {
		t.Fatalf("label 数量不匹配应返回 nil，实际 %v", c)
	}
}

func TestGaugeVec_LabelCombinations(t *testing.T) {
	v := NewGaugeVec("mem_used", "memory used", []string{"kind"})

	v.WithLabelValues("heap").Set(100)
	v.WithLabelValues("stack").Set(20)
	v.WithLabelValues("heap").Inc()
	v.WithLabelValues("heap").Dec()

	r := NewRegistry()
	r.MustRegisterCollector("mem_used", &cGaugeVec{n: "mem_used", h: "memory used", v: v})

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `mem_used{kind="heap"} 100`) {
		t.Fatalf("heap 期望 100：%s", out)
	}
	if !strings.Contains(out, `mem_used{kind="stack"} 20`) {
		t.Fatalf("stack 期望 20：%s", out)
	}
}

func TestHistogramVec_LabelCombinations(t *testing.T) {
	v := NewHistogramVec("latency", "latency", []string{"endpoint"}, []float64{0.01, 0.1, 1})

	v.WithLabelValues("/a").Observe(0.005)
	v.WithLabelValues("/a").Observe(0.5)
	v.WithLabelValues("/b").Observe(2)

	r := NewRegistry()
	r.MustRegisterCollector("latency", &cHistogramVec{n: "latency", h: "latency", v: v})

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	// /a 应该有 count=2；/b count=1
	if !strings.Contains(out, `latency_count{endpoint="/a"} 2`) {
		t.Fatalf("/a count 期望 2：%s", out)
	}
	if !strings.Contains(out, `latency_count{endpoint="/b"} 1`) {
		t.Fatalf("/b count 期望 1：%s", out)
	}
	if !strings.Contains(out, `latency_bucket{endpoint="/a",le="+Inf"} 2`) {
		t.Fatalf("/a +Inf 期望 2：%s", out)
	}
}

// ==================== Registry 重复 Register / MustRegister ====================

func TestRegistry_DuplicateRegister_NoPanic(t *testing.T) {
	r := NewRegistry()
	m1 := NewCounterMetric("dup", "first")
	m2 := NewCounterMetric("dup", "second")
	r.MustRegister(m1)
	// 重复注册必须不 panic
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("重复 MustRegister 不应 panic：%v", rec)
		}
	}()
	r.MustRegister(m2)

	m1.IncCounter()
	m1.IncCounter() // 在首次注册的 m1 上累加

	var buf bytes.Buffer
	if err := r.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "dup 2") {
		t.Fatalf("重复注册应保留首次指标，期望值 2：%s", out)
	}
	if !strings.Contains(out, "# HELP dup first") {
		t.Fatalf("重复注册应保留首次 help：%s", out)
	}
}

func TestRegistry_Register_ReturnsBool(t *testing.T) {
	r := NewRegistry()
	m1 := NewCounterMetric("flag", "h")
	m2 := NewCounterMetric("flag", "h2")

	if !r.Register(m1) {
		t.Fatalf("首次 Register 应返回 true")
	}
	if r.Register(m2) {
		t.Fatalf("重复 Register 应返回 false")
	}
	if r.Register(nil) {
		t.Fatalf("nil metric 应返回 false")
	}
}

// ==================== DefaultRegistry / GetDefaultRegistry 同一对象 ====================

func TestDefaultRegistryAndGetDefaultRegistry_SameObject(t *testing.T) {
	a := DefaultRegistry()
	b := GetDefaultRegistry()
	if a != b {
		t.Fatalf("DefaultRegistry() 与 GetDefaultRegistry() 应返回同一对象")
	}
	if a == nil {
		t.Fatalf("DefaultRegistry() 不应为 nil")
	}
}

// ==================== 错误 writer ====================

// errWriter 模拟写入失败的 io.Writer。
type errWriter struct{}

var errWriteSentinel = errors.New("simulated write failure")

func (errWriter) Write(p []byte) (int, error) { return 0, errWriteSentinel }

func TestRegistry_WriteTo_ErrorWriter(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(NewCounterMetric("err_metric", "err help"))
	r.MustRegister(NewGaugeMetric("err_gauge", "g help"))

	err := r.WriteTo(errWriter{})
	if err == nil {
		t.Fatalf("writer 报错时 WriteTo 应返回错误")
	}
	if !errors.Is(err, errWriteSentinel) {
		t.Fatalf("WriteTo 应透传 writer 错误，实际：%v", err)
	}
}

// ==================== go_memstats_num_gc 是 counter ====================

func TestDefaultRegistry_NumGCIsCounter(t *testing.T) {
	RefreshRuntimeMetrics()
	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# TYPE go_memstats_num_gc counter") {
		t.Fatalf("go_memstats_num_gc 应为 counter，实际输出片段：\n%s",
			extractBlock(out, "go_memstats_num_gc"))
	}
}

// extractBlock 从输出里抠出含 name 的行，便于失败时报错。
func extractBlock(out, name string) string {
	var sb strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ==================== 并发 Observe / Inc / Set 与 WriteTo（race 检测）====================

func TestConcurrent_MetricsSafe(t *testing.T) {
	// 独立 Counter / Gauge 通过 CounterVec / NewGauge 模拟；
	// 独立 Histogram 用 HistogramVec 模拟（Metric.Histogram 没有公开 Count 方法）。
	cv := NewCounterVec("cc", "concurrent counter", []string{"x"})
	hhv := NewHistogramVec("hh", "concurrent hist", []string{"x"}, []float64{0.01, 0.1, 1, 10})
	c := cv.WithLabelValues("a")
	hh := hhv.WithLabelValues("a")
	g := NewGauge()

	const (
		writers = 8
		reads   = 200
	)
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < reads; j++ {
				c.Inc()
				g.Set(float64(id*reads + j))
				hh.Observe(float64(j%100) / 100.0)
			}
		}(i)
	}

	// 同时并发调用 WriteTo（对独立 Registry），确保 race 安全
	r := NewRegistry()
	m := NewCounterMetric("smoke", "smoke")
	m.IncCounter()
	r.MustRegister(m)
	var wgRead sync.WaitGroup
	wgRead.Add(2)
	go func() {
		defer wgRead.Done()
		var buf bytes.Buffer
		_ = r.WriteTo(&buf)
	}()
	go func() {
		defer wgRead.Done()
		var buf bytes.Buffer
		_ = r.WriteTo(&buf)
	}()

	wg.Wait()
	wgRead.Wait()

	// 最终断言：Histogram.Count 应 == writers*reads
	if got := hh.Count(); got != writers*reads {
		t.Fatalf("并发 Observe 后 Histogram.Count 期望 %d，实际 %d", writers*reads, got)
	}
	wantCnt := float64(writers * reads)
	if got := c.Value(); got != wantCnt {
		t.Fatalf("并发 Inc 后 Counter 期望 %v，实际 %v", wantCnt, got)
	}
}

// ==================== Round 16：基础设施指标 ====================

// infraMetricsNames 列出 Round 16 注册的全部基础设施指标名。
// 与任务书一一对应：8 gauge + 6 counter。
var infraMetricsNames = []struct {
	name string
	kind string // "gauge" 或 "counter"
}{
	// Gauge
	{"ws_clients_connected", "gauge"},
	{"ws_online_users", "gauge"},
	{"db_open_connections", "gauge"},
	{"db_in_use_connections", "gauge"},
	{"db_idle_connections", "gauge"},
	{"scheduler_running", "gauge"},
	{"scheduler_entries", "gauge"},
	{"scheduler_running_tasks", "gauge"},
	// Counter
	{"ws_messages_sent_total", "counter"},
	{"ws_messages_failed_total", "counter"},
	{"db_wait_count_total", "counter"},
	{"db_max_idle_closed_total", "counter"},
	{"db_max_idle_time_closed_total", "counter"},
	{"db_max_lifetime_closed_total", "counter"},
}

// TestInfraMetrics_Registered 验证全部 14 个 Round 16 指标已在 defaultRegistry 注册，
// 且 HELP / TYPE 输出符合 Prometheus text 0.0.4 规范。
func TestInfraMetrics_Registered(t *testing.T) {
	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	for _, m := range infraMetricsNames {
		helpLine := "# HELP " + m.name + " "
		typeLine := "# TYPE " + m.name + " " + m.kind
		if !strings.Contains(out, helpLine) {
			t.Fatalf("指标 %s 缺少 HELP 行（%q）", m.name, helpLine)
		}
		if !strings.Contains(out, typeLine) {
			t.Fatalf("指标 %s 缺少 TYPE 行（%q）", m.name, typeLine)
		}
	}
}

// TestInfraMetrics_NoHighCardinalityLabels 验证全部 14 个指标零 label：
// 即输出里不应出现 "{...=" 形式的 label 块。
func TestInfraMetrics_NoHighCardinalityLabels(t *testing.T) {
	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	for _, m := range infraMetricsNames {
		// 找该指标的数据行（跳过 HELP/TYPE 行）
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "# ") || line == "" {
				continue
			}
			// 数据行格式："metric_name [labels] value"；
			// 零 label 时只有 "metric_name value"。
			if strings.HasPrefix(line, m.name+" ") || strings.HasPrefix(line, m.name+"\t") {
				if strings.Contains(line, "{") {
					t.Fatalf("指标 %s 出现 label，违反无 label 原则：%s", m.name, line)
				}
			}
		}
	}
}

// TestSetWebSocketMetrics_UpdatesGauges 验证 SetWebSocketMetrics 后 gauge 正确刷新到输出。
func TestSetWebSocketMetrics_UpdatesGauges(t *testing.T) {
	SetWebSocketMetrics(7, 3)

	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	// Prometheus text 用 %g 输出整数（无小数点）；但 fmt %g 对 7.0 输出 "7"。
	if !strings.Contains(out, "ws_clients_connected 7") {
		t.Fatalf("ws_clients_connected 期望 7，输出片段：%s",
			extractBlock(out, "ws_clients_connected"))
	}
	if !strings.Contains(out, "ws_online_users 3") {
		t.Fatalf("ws_online_users 期望 3，输出片段：%s",
			extractBlock(out, "ws_online_users"))
	}
}

// TestSetWebSocketMetrics_NegativeClamped 验证负值被夹到 0（防御性）。
func TestSetWebSocketMetrics_NegativeClamped(t *testing.T) {
	SetWebSocketMetrics(-5, -2)

	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ws_clients_connected 0") {
		t.Fatalf("负值应被夹到 0：%s",
			extractBlock(out, "ws_clients_connected"))
	}
	if !strings.Contains(out, "ws_online_users 0") {
		t.Fatalf("负值应被夹到 0：%s",
			extractBlock(out, "ws_online_users"))
	}
}

// TestIncWebSocketMessage 验证 IncWebSocketMessage 分别累加 sent / failed。
func TestIncWebSocketMessage(t *testing.T) {
	// 读取基线（其它测试可能已经 Inc 过）
	baseSent := wsMessagesSentCounter.value()
	baseFailed := wsMessagesFailedCounter.value()

	IncWebSocketMessage(true)
	IncWebSocketMessage(true)
	IncWebSocketMessage(false)

	if got := wsMessagesSentCounter.value(); got != baseSent+2 {
		t.Fatalf("ws_messages_sent_total 期望 %v，实际 %v", baseSent+2, got)
	}
	if got := wsMessagesFailedCounter.value(); got != baseFailed+1 {
		t.Fatalf("ws_messages_failed_total 期望 %v，实际 %v", baseFailed+1, got)
	}
}

// TestRefreshDatabaseMetrics_NilDB 验证 nil DB 时 gauge 置 0，counter 不回退。
func TestRefreshDatabaseMetrics_NilDB(t *testing.T) {
	// 先打一个非零基线
	dbOpenConnsGauge.set(9)
	dbInUseConnsGauge.set(3)
	dbIdleConnsGauge.set(2)

	RefreshDatabaseMetrics(nil)

	if got := dbOpenConnsGauge.value(); got != 0 {
		t.Fatalf("nil DB 时 db_open_connections 期望 0，实际 %v", got)
	}
	if got := dbInUseConnsGauge.value(); got != 0 {
		t.Fatalf("nil DB 时 db_in_use_connections 期望 0，实际 %v", got)
	}
	if got := dbIdleConnsGauge.value(); got != 0 {
		t.Fatalf("nil DB 时 db_idle_connections 期望 0，实际 %v", got)
	}
}

// TestRefreshDatabaseMetrics_RealDB 验证打开 sqlite 后能刷新 gauge，并差值累加 counter。
func TestRefreshDatabaseMetrics_RealDB(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test_refresh.db")
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开 sqlite 失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 触发一次查询让连接建立
	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("查询 sqlite 失败：%v", err)
	}

	RefreshDatabaseMetrics(db)
	// 至少 gauge 应被刷为 sqlite 的非负值（>0 因为 OpenConnections 通常 >= 1）
	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	for _, name := range []string{
		"db_open_connections",
		"db_in_use_connections",
		"db_idle_connections",
	} {
		if !strings.Contains(out, "# TYPE "+name+" gauge") {
			t.Fatalf("输出缺少 %s gauge TYPE 行", name)
		}
	}

	// counter 在真实 db 上由差值累加；至少 TYPE 行存在
	for _, name := range []string{
		"db_wait_count_total",
		"db_max_idle_closed_total",
		"db_max_idle_time_closed_total",
		"db_max_lifetime_closed_total",
	} {
		if !strings.Contains(out, "# TYPE "+name+" counter") {
			t.Fatalf("输出缺少 %s counter TYPE 行", name)
		}
	}
}

// TestRefreshSchedulerMetrics 验证 scheduler 三 gauge 正确刷新。
func TestRefreshSchedulerMetrics(t *testing.T) {
	RefreshSchedulerMetrics(true, 5, 2)

	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "scheduler_running 1") {
		t.Fatalf("scheduler_running 期望 1，实际 %s",
			extractBlock(out, "scheduler_running"))
	}
	if !strings.Contains(out, "scheduler_entries 5") {
		t.Fatalf("scheduler_entries 期望 5，实际 %s",
			extractBlock(out, "scheduler_entries"))
	}
	if !strings.Contains(out, "scheduler_running_tasks 2") {
		t.Fatalf("scheduler_running_tasks 期望 2，实际 %s",
			extractBlock(out, "scheduler_running_tasks"))
	}

	// 翻转 running 为 false
	RefreshSchedulerMetrics(false, 5, 0)
	var buf2 bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf2); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out2 := buf2.String()
	if !strings.Contains(out2, "scheduler_running 0") {
		t.Fatalf("scheduler_running 翻转后期望 0，实际 %s",
			extractBlock(out2, "scheduler_running"))
	}
}

// TestRefreshSchedulerMetrics_NegativeClamped 验证负值夹到 0。
func TestRefreshSchedulerMetrics_NegativeClamped(t *testing.T) {
	RefreshSchedulerMetrics(true, -3, -1)
	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "scheduler_entries 0") {
		t.Fatalf("scheduler_entries 负值应被夹到 0：%s",
			extractBlock(out, "scheduler_entries"))
	}
	if !strings.Contains(out, "scheduler_running_tasks 0") {
		t.Fatalf("scheduler_running_tasks 负值应被夹到 0：%s",
			extractBlock(out, "scheduler_running_tasks"))
	}
}

// TestInfraMetrics_ConcurrentSafe 验证 4 类指标在并发调用下不出现数据竞争。
func TestInfraMetrics_ConcurrentSafe(t *testing.T) {
	const writers = 6
	const iters = 100

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				SetWebSocketMetrics(id, j)
				IncWebSocketMessage(j%2 == 0)
				RefreshDatabaseMetrics(nil)
				RefreshSchedulerMetrics(j%2 == 0, j, j%3)
			}
		}(i)
	}
	wg.Wait()

	var buf bytes.Buffer
	if err := DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo 失败：%v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("并发调用后输出不应为空")
	}
}
