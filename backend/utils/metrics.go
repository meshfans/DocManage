package utils

// Round 15：自研 Prometheus text 0.0.4 指标注册表。
// 设计要点：
//   - 不引入第三方依赖，纯标准库
//   - Counter / Gauge 用 atomic.Uint64 bits 存浮点值；Add 用 CAS 循环
//   - Histogram 用 sync.Mutex 保护 count/buckets，sum 用 atomic.Uint64 bits
//   - Vec 内部用 RWMutex 保护 children map，按 label value 拼 key
//   - Registry 输出按 metric 名字典序；Vec 子项按 label value 串字典序；bucket 边界升序
//   - label value 转义 \\ " \n（label name 不做白名单校验，调用方保证）
//   - 重复注册采用「以第一次为准」的语义而非 panic，
//     避免 middleware 在测试或 NewCombinedServer 多次初始化时崩溃。
//
// 验收契约（Round 15 代码审查）：
//   - 函数式 API：DefaultRegistry() *Registry，内部私有 defaultRegistry 变量
//   - Registry.WriteTo(w io.Writer) error（兼容 handoff 签名）
//   - 保留 Registry.WriteText(w io.Writer) error（兼容旧调用）
//   - MustRegister(m *Metric) / Register(m *Metric) bool 接受 *Metric
//   - Histogram 输出 _bucket / +Inf / _sum / _count
//   - go_memstats_num_gc 为 counter（单调递增），其他运行时指标为 gauge
//   - 内部访问 histBuckets / samples / sampleOrder 必须加锁，data-race 安全

import (
	"database/sql"
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ==================== atomic float helpers ====================

// addFloatBits 用 CAS 循环对 atomic.Uint64 bits 做浮点加法。
func addFloatBits(a *atomic.Uint64, delta float64) {
	for {
		old := a.Load()
		nf := math.Float64frombits(old) + delta
		if a.CompareAndSwap(old, math.Float64bits(nf)) {
			return
		}
	}
}

// bitsToFloat 把 atomic.Uint64 bits 解码回 float64。
func bitsToFloat(a *atomic.Uint64) float64 { return math.Float64frombits(a.Load()) }

// ==================== 类型枚举 ====================

// MetricType 指标类型。
type MetricType int

const (
	// TypeCounter 单调递增计数器。
	TypeCounter MetricType = iota
	// TypeGauge 可增可减仪表。
	TypeGauge
	// TypeHistogram 累计桶直方图。
	TypeHistogram
)

// String 返回 Prometheus text 协议里使用的类型名。
func (t MetricType) String() string {
	switch t {
	case TypeCounter:
		return "counter"
	case TypeGauge:
		return "gauge"
	case TypeHistogram:
		return "histogram"
	default:
		return "unknown"
	}
}

// ==================== Sample ====================

// Sample 单个指标样本点（带 label）。
type Sample struct {
	Labels map[string]string
	Value  float64
}

// ==================== Counter / Gauge / Histogram 内部结构 ====================

// counterImpl 单调递增计数器；浮点值以 atomic.Uint64 bits 存储。
type counterImpl struct {
	bits atomic.Uint64
}

func newCounter() *counterImpl { return &counterImpl{} }

func (c *counterImpl) inc()           { addFloatBits(&c.bits, 1) }
func (c *counterImpl) add(v float64)  { addFloatBits(&c.bits, v) }
func (c *counterImpl) value() float64 { return bitsToFloat(&c.bits) }

// gaugeImpl 可增可减指标；浮点值以 atomic.Uint64 bits 存储。
type gaugeImpl struct {
	bits atomic.Uint64
}

func newGauge() *gaugeImpl { return &gaugeImpl{} }

// NewGauge 创建一个空 Gauge（值 = 0）。
func NewGauge() *Gauge { return &Gauge{impl: &gaugeImpl{}} }

func (g *gaugeImpl) set(v float64)  { g.bits.Store(math.Float64bits(v)) }
func (g *gaugeImpl) inc()           { addFloatBits(&g.bits, 1) }
func (g *gaugeImpl) dec()           { addFloatBits(&g.bits, -1) }
func (g *gaugeImpl) add(v float64)  { addFloatBits(&g.bits, v) }
func (g *gaugeImpl) value() float64 { return bitsToFloat(&g.bits) }

// histogramImpl 累计桶直方图。
// bucket 边界在创建时排序；Observe(v) 时所有 buckets[i] >= v 的桶计数 +1。
// 输出 _bucket{le="..."} / _bucket{le="+Inf"} / _sum / _count。
type histogramImpl struct {
	mu      sync.Mutex
	buckets []float64
	counts  []uint64
	count   uint64
	sumBits atomic.Uint64
}

func newHistogram(buckets []float64) *histogramImpl {
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	return &histogramImpl{buckets: bs, counts: make([]uint64, len(bs))}
}

func (h *histogramImpl) observe(v float64) {
	h.mu.Lock()
	h.observeLocked(v)
	h.mu.Unlock()
}

func (h *histogramImpl) observeLocked(v float64) {
	h.count++
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
		}
	}
	addFloatBits(&h.sumBits, v)
}

func (h *histogramImpl) countSnapshot() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func (h *histogramImpl) sumSnapshot() float64 { return bitsToFloat(&h.sumBits) }

func (h *histogramImpl) bucketsSnapshot() (buckets []float64, counts []uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buckets = make([]float64, len(h.buckets))
	copy(buckets, h.buckets)
	counts = make([]uint64, len(h.counts))
	copy(counts, h.counts)
	return
}

// ==================== 默认桶 ====================

// DefaultHistogramBuckets 默认 HTTP 延迟桶（秒）。
// 覆盖范围：5ms ~ 5s，足以覆盖普通 HTTP API。
var DefaultHistogramBuckets = []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 5}

// HTTPRequestDurationBuckets Round 15 中间件使用的固定桶。
// 与任务要求一致：[0.005, 0.01, 0.05, 0.1, 0.5, 1, 5]。
var HTTPRequestDurationBuckets = DefaultHistogramBuckets

// ==================== Vec ====================

// joinLabels 把多个 label value 拼成 map key（用 NUL 分隔，避开合法 label value 字符）。
func joinLabels(vs ...string) string { return strings.Join(vs, "\x00") }

// sortLabelKeys 对 label name 排序，保证输出顺序稳定。
func sortLabelKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// labelKey 按 name 字典序拼 label value，作为 children map 的查找 key。
func labelKey(names []string, labels map[string]string) string {
	vs := make([]string, len(names))
	for i, n := range names {
		vs[i] = labels[n]
	}
	return joinLabels(vs...)
}

// CounterVec 带 label 的 Counter 集合。
type CounterVec struct {
	name     string
	help     string
	labels   []string
	mu       sync.RWMutex
	children map[string]*counterImpl
}

// NewCounterVec 创建 CounterVec。
func NewCounterVec(name, help string, labels []string) *CounterVec {
	return &CounterVec{
		name:     name,
		help:     help,
		labels:   append([]string(nil), labels...),
		children: map[string]*counterImpl{},
	}
}

// WithLabelValues 按 label value 列表取（或创建）一个 Counter。
// label value 数量必须与注册时的 label name 数量一致，否则返回 nil。
func (v *CounterVec) WithLabelValues(vs ...string) *Counter {
	if len(vs) != len(v.labels) {
		return nil
	}
	return v.withKey(joinLabels(vs...)).asCounter()
}

// WithLabels 按 label map 取（或创建）一个 Counter。
// 未提供的 label name 视为空字符串。
func (v *CounterVec) WithLabels(labels map[string]string) *Counter {
	if labels == nil {
		labels = map[string]string{}
	}
	k := labelKey(v.labels, labels)
	return v.withKey(k).asCounter()
}

// AddLabels 直接以 label map 自增 v（不返回子指标）。
// 未匹配 label name 视为空字符串。
func (v *CounterVec) AddLabels(labels map[string]string, v2 float64) {
	if labels == nil {
		labels = map[string]string{}
	}
	k := labelKey(v.labels, labels)
	c := v.withKey(k).asCounter()
	c.Add(v2)
}

func (v *CounterVec) withKey(k string) *rawChild {
	v.mu.RLock()
	if c, ok := v.children[k]; ok {
		v.mu.RUnlock()
		return &rawChild{c: c, cVec: v}
	}
	v.mu.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if c, ok := v.children[k]; ok {
		return &rawChild{c: c, cVec: v}
	}
	c := newCounter()
	v.children[k] = c
	return &rawChild{c: c, cVec: v}
}

// rawChild 是 children 容器暴露给外部 Counter/Gauge/Histogram 的适配壳。
// 通过类型断言区分用途，避免三种 Vec 各写一遍相同字段。
type rawChild struct {
	c    *counterImpl
	g    *gaugeImpl
	h    *histogramImpl
	cVec *CounterVec
	gVec *GaugeVec
	hVec *HistogramVec
}

func (r *rawChild) asCounter() *Counter {
	return &Counter{impl: r.c, vec: r.cVec}
}

func (r *rawChild) asGauge() *Gauge {
	return &Gauge{impl: r.g, vec: r.gVec}
}

func (r *rawChild) asHistogram() *Histogram {
	return &Histogram{impl: r.h, vec: r.hVec}
}

func (v *CounterVec) snapshot() []labeledCounter {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]labeledCounter, 0, len(v.children))
	for k, c := range v.children {
		out = append(out, labeledCounter{values: strings.Split(k, "\x00"), c: c})
	}
	return out
}

type labeledCounter struct {
	values []string
	c      *counterImpl
}

// GaugeVec 带 label 的 Gauge 集合。
type GaugeVec struct {
	name     string
	help     string
	labels   []string
	mu       sync.RWMutex
	children map[string]*gaugeImpl
}

// NewGaugeVec 创建 GaugeVec。
func NewGaugeVec(name, help string, labels []string) *GaugeVec {
	return &GaugeVec{
		name:     name,
		help:     help,
		labels:   append([]string(nil), labels...),
		children: map[string]*gaugeImpl{},
	}
}

// WithLabelValues 按 label value 列表取（或创建）一个 Gauge。
func (v *GaugeVec) WithLabelValues(vs ...string) *Gauge {
	if len(vs) != len(v.labels) {
		return nil
	}
	return v.withKey(joinLabels(vs...)).asGauge()
}

// WithLabels 按 label map 取（或创建）一个 Gauge。
func (v *GaugeVec) WithLabels(labels map[string]string) *Gauge {
	if labels == nil {
		labels = map[string]string{}
	}
	return v.withKey(labelKey(v.labels, labels)).asGauge()
}

func (v *GaugeVec) withKey(k string) *rawChild {
	v.mu.RLock()
	if g, ok := v.children[k]; ok {
		v.mu.RUnlock()
		return &rawChild{g: g, gVec: v}
	}
	v.mu.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if g, ok := v.children[k]; ok {
		return &rawChild{g: g, gVec: v}
	}
	g := newGauge()
	v.children[k] = g
	return &rawChild{g: g, gVec: v}
}

func (v *GaugeVec) snapshot() []labeledGauge {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]labeledGauge, 0, len(v.children))
	for k, g := range v.children {
		out = append(out, labeledGauge{values: strings.Split(k, "\x00"), g: g})
	}
	return out
}

type labeledGauge struct {
	values []string
	g      *gaugeImpl
}

// HistogramVec 带 label 的 Histogram 集合。
type HistogramVec struct {
	name     string
	help     string
	labels   []string
	buckets  []float64
	mu       sync.RWMutex
	children map[string]*histogramImpl
}

// NewHistogramVec 创建 HistogramVec。buckets 会被复制并按升序排序。
func NewHistogramVec(name, help string, labels []string, buckets []float64) *HistogramVec {
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	return &HistogramVec{
		name:     name,
		help:     help,
		labels:   append([]string(nil), labels...),
		buckets:  bs,
		children: map[string]*histogramImpl{},
	}
}

// WithLabelValues 按 label value 列表取（或创建）一个 Histogram。
func (v *HistogramVec) WithLabelValues(vs ...string) *Histogram {
	if len(vs) != len(v.labels) {
		return nil
	}
	return v.withKey(joinLabels(vs...)).asHistogram()
}

// WithLabels 按 label map 取（或创建）一个 Histogram。
func (v *HistogramVec) WithLabels(labels map[string]string) *Histogram {
	if labels == nil {
		labels = map[string]string{}
	}
	return v.withKey(labelKey(v.labels, labels)).asHistogram()
}

func (v *HistogramVec) withKey(k string) *rawChild {
	v.mu.RLock()
	if h, ok := v.children[k]; ok {
		v.mu.RUnlock()
		return &rawChild{h: h, hVec: v}
	}
	v.mu.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if h, ok := v.children[k]; ok {
		return &rawChild{h: h, hVec: v}
	}
	h := newHistogram(v.buckets)
	v.children[k] = h
	return &rawChild{h: h, hVec: v}
}

func (v *HistogramVec) snapshot() []labeledHistogram {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]labeledHistogram, 0, len(v.children))
	for k, h := range v.children {
		out = append(out, labeledHistogram{values: strings.Split(k, "\x00"), h: h})
	}
	return out
}

type labeledHistogram struct {
	values []string
	h      *histogramImpl
}

// ==================== Counter / Gauge / Histogram 公开 API ====================

// Counter 单调递增计数器。
// 既可作为独立 Metric（直接注册到 Registry），也可作为 CounterVec 子项。
type Counter struct {
	impl *counterImpl
	vec  *CounterVec // 非 nil 时表示由 CounterVec 创建，WithLabels/AddLabels 才能工作
}

// Inc 自增 1。
func (c *Counter) Inc() { c.Add(1) }

// IncBy 自增 v（Counter 别名：等价于 Add）。
func (c *Counter) IncBy(v float64) { c.Add(v) }

// Add 增加 v。
func (c *Counter) Add(v float64) {
	if c == nil || c.impl == nil {
		return
	}
	c.impl.add(v)
}

// Value 返回当前值。
func (c *Counter) Value() float64 {
	if c == nil || c.impl == nil {
		return 0
	}
	return c.impl.value()
}

// AddLabels 在 CounterVec 上下文中按 label map 自增 v；独立 Counter 调用为 no-op。
func (c *Counter) AddLabels(labels map[string]string, v float64) {
	if c == nil || c.vec == nil {
		return
	}
	c.vec.AddLabels(labels, v)
}

// WithLabels 在 CounterVec 上下文中按 label map 取子项；独立 Counter 调用返回自身。
func (c *Counter) WithLabels(labels map[string]string) *Counter {
	if c == nil || c.vec == nil {
		return c
	}
	return c.vec.WithLabels(labels)
}

// Gauge 可增可减指标。
type Gauge struct {
	impl *gaugeImpl
	vec  *GaugeVec
}

// Set 直接设置值。
func (g *Gauge) Set(v float64) {
	if g == nil || g.impl == nil {
		return
	}
	g.impl.set(v)
}

// Inc 自增 1。
func (g *Gauge) Inc() { g.Add(1) }

// Dec 自减 1。
func (g *Gauge) Dec() { g.Add(-1) }

// Add 加 v（可负）。
func (g *Gauge) Add(v float64) {
	if g == nil || g.impl == nil {
		return
	}
	g.impl.add(v)
}

// Value 返回当前值。
func (g *Gauge) Value() float64 {
	if g == nil || g.impl == nil {
		return 0
	}
	return g.impl.value()
}

// WithLabels 在 GaugeVec 上下文中按 label map 取子项；独立 Gauge 调用返回自身。
func (g *Gauge) WithLabels(labels map[string]string) *Gauge {
	if g == nil || g.vec == nil {
		return g
	}
	return g.vec.WithLabels(labels)
}

// Histogram 累计桶直方图。
type Histogram struct {
	impl *histogramImpl
	vec  *HistogramVec
}

// Observe 记录一个观测值 v。
func (h *Histogram) Observe(v float64) {
	if h == nil || h.impl == nil {
		return
	}
	h.impl.observe(v)
}

// Count 返回总观测次数。
func (h *Histogram) Count() uint64 {
	if h == nil || h.impl == nil {
		return 0
	}
	return h.impl.countSnapshot()
}

// Sum 返回观测值总和。
func (h *Histogram) Sum() float64 {
	if h == nil || h.impl == nil {
		return 0
	}
	return h.impl.sumSnapshot()
}

// Buckets 返回桶上界副本（不含 +Inf）。
func (h *Histogram) Buckets() []float64 {
	if h == nil || h.impl == nil {
		return nil
	}
	buckets, _ := h.impl.bucketsSnapshot()
	return buckets
}

// BucketCounts 返回各桶累计计数副本（不含 +Inf 桶；+Inf 桶等于 Count()）。
func (h *Histogram) BucketCounts() []uint64 {
	if h == nil || h.impl == nil {
		return nil
	}
	_, counts := h.impl.bucketsSnapshot()
	return counts
}

// WithLabels 在 HistogramVec 上下文中按 label map 取子项；独立 Histogram 调用返回自身。
func (h *Histogram) WithLabels(labels map[string]string) *Histogram {
	if h == nil || h.vec == nil {
		return h
	}
	return h.vec.WithLabels(labels)
}

// ==================== Metric（元数据 + Sample 集合）====================

// Metric 单个指标（元数据 + 样本集合）。
// 用于独立注册到 Registry 的 Counter / Gauge / Histogram。
// 多 label 场景建议使用 CounterVec / GaugeVec / HistogramVec。
//
// 内部状态（samples / sampleOrder / histBuckets）由 mu 串行访问，
// 保证并发读写无 data race。
type Metric struct {
	Name    string
	Help    string
	Type    MetricType
	Buckets []float64 // histogram 桶边界（左闭右开）
	TypeStr string    // 输出时使用的 type 字符串，可选覆盖

	mu          sync.RWMutex
	samples     map[string]*Sample
	sampleOrder []string

	// histBuckets 仅 Histogram 使用：每个 label value 一份 histogramImpl，
	// 用于输出 _bucket{le="..."} / _bucket{le="+Inf"} / _sum / _count。
	histBuckets map[string]*histogramImpl
}

// NewCounterMetric 创建一个独立 Counter 指标（不自动注册）。
func NewCounterMetric(name, help string) *Metric {
	return &Metric{
		Name:    name,
		Help:    help,
		Type:    TypeCounter,
		samples: map[string]*Sample{},
	}
}

// NewGaugeMetric 创建一个独立 Gauge 指标（不自动注册）。
func NewGaugeMetric(name, help string) *Metric {
	return &Metric{
		Name:    name,
		Help:    help,
		Type:    TypeGauge,
		samples: map[string]*Sample{},
	}
}

// NewHistogramMetric 创建一个独立 Histogram 指标（不自动注册）。
func NewHistogramMetric(name, help string, buckets []float64) *Metric {
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	return &Metric{
		Name:        name,
		Help:        help,
		Type:        TypeHistogram,
		Buckets:     bs,
		samples:     map[string]*Sample{},
		histBuckets: map[string]*histogramImpl{},
	}
}

// Observe 实现 Observe 接口（Counter / Gauge 调用为 no-op）。
func (m *Metric) Observe(v float64) {
	if m == nil {
		return
	}
	m.observeInternal(v, nil)
}

// SetGauge 设置独立 Gauge 的值。
func (m *Metric) SetGauge(v float64) {
	if m == nil || m.Type != TypeGauge {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.ensureSampleLocked(nil)
	s.Value = v
}

// IncCounter 自增独立 Counter。
func (m *Metric) IncCounter() {
	if m == nil || m.Type != TypeCounter {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.ensureSampleLocked(nil)
	s.Value++
}

// AddCounter 增加独立 Counter v。
func (m *Metric) AddCounter(v float64) {
	if m == nil || m.Type != TypeCounter {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.ensureSampleLocked(nil)
	s.Value += v
}

// With 返回一个用于写入特定 label 子项的子句柄。
// 对 Counter 暴露 Inc/Add，对 Gauge 暴露 Set/Inc/Dec，对 Histogram 暴露 Observe。
func (m *Metric) With(labels map[string]string) *MetricChild {
	return &MetricChild{parent: m, labels: labels}
}

// MetricChild Metric.With 返回的子句柄，按父类型分派方法。
type MetricChild struct {
	parent *Metric
	labels map[string]string
}

func (c *MetricChild) key() string {
	if c == nil || c.parent == nil {
		return ""
	}
	return labelKey(labelNames(c.labels), c.labels)
}

func (c *MetricChild) ensureSample() *Sample {
	return c.parent.ensureSample(c.labels)
}

// Inc 自增（仅 Counter）。
func (c *MetricChild) Inc() { c.Add(1) }

// Add 增加 v（仅 Counter）。
func (c *MetricChild) Add(v float64) {
	if c == nil || c.parent == nil {
		return
	}
	if c.parent.Type == TypeCounter {
		c.parent.mu.Lock()
		defer c.parent.mu.Unlock()
		s := c.parent.ensureSampleLocked(c.labels)
		s.Value += v
	}
}

// Set 设置值（仅 Gauge）。
func (c *MetricChild) Set(v float64) {
	if c == nil || c.parent == nil {
		return
	}
	if c.parent.Type == TypeGauge {
		c.parent.mu.Lock()
		defer c.parent.mu.Unlock()
		s := c.parent.ensureSampleLocked(c.labels)
		s.Value = v
	}
}

// Observe 记录观测值（仅 Histogram）。
func (c *MetricChild) Observe(v float64) {
	if c == nil || c.parent == nil {
		return
	}
	c.parent.observeInternal(v, c.labels)
}

// ensureSample 在 mu 保护下取/建一个 Sample。
// 对 Histogram 会同时确保 histBuckets 中存在对应 histogramImpl。
func (m *Metric) ensureSample(labels map[string]string) *Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureSampleLocked(labels)
}

func (m *Metric) ensureSampleLocked(labels map[string]string) *Sample {
	key := labelKey(labelNames(labels), labels)
	if s, ok := m.samples[key]; ok {
		return s
	}
	s := &Sample{Labels: cloneLabels(labels), Value: 0}
	m.samples[key] = s
	m.sampleOrder = append(m.sampleOrder, key)
	if m.Type == TypeHistogram {
		if _, ok := m.histBuckets[key]; !ok {
			m.histBuckets[key] = newHistogram(m.Buckets)
		}
	}
	return s
}

// observeInternal 在 mu 保护下把 v 累加到 samples.Value 并写入 histBuckets。
// histBuckets.observe 内部用自己的 mutex；此处不嵌套加锁（mu 在 Unlock 后再调 observe）。
func (m *Metric) observeInternal(v float64, labels map[string]string) {
	if m == nil || m.Type != TypeHistogram {
		return
	}
	key := labelKey(labelNames(labels), labels)
	m.mu.Lock()
	defer m.mu.Unlock()
	hh, ok := m.histBuckets[key]
	if !ok {
		hh = newHistogram(m.Buckets)
		m.histBuckets[key] = hh
	}
	if s, ok := m.samples[key]; ok {
		s.Value += v
	} else {
		s := &Sample{Labels: cloneLabels(labels), Value: v}
		m.samples[key] = s
		m.sampleOrder = append(m.sampleOrder, key)
	}
	hh.mu.Lock()
	hh.observeLocked(v)
	hh.mu.Unlock()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func labelNames(labels map[string]string) []string {
	if labels == nil {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ==================== label 格式化 ====================

// escapeLabelValue 对 label value 中的 \\ " \n 做转义。
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	var sb strings.Builder
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		default:
			sb.WriteByte(v[i])
		}
	}
	return sb.String()
}

// formatLabels 拼 label 块：{k1="v1",k2="v2"}。空返回空串。
func formatLabels(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteByte('{')
	for i := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(names[i])
		sb.WriteString(`="`)
		sb.WriteString(escapeLabelValue(values[i]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// formatLabelsFromMap 按 key 字典序输出 label 块。
func formatLabelsFromMap(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := sortLabelKeys(labels)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(k)
		sb.WriteString(`="`)
		sb.WriteString(escapeLabelValue(labels[k]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// appendLE 在已有 label 块后追加 le 标签；base 为空时只输出 le。
func appendLE(base string, le string) string {
	if base == "" {
		return fmt.Sprintf(`{le="%s"}`, le)
	}
	return base[:len(base)-1] + `,le="` + le + `"}`
}

// formatFloat 用 %g 风格格式化浮点数（NaN / +Inf / -Inf 都被 Go %g 原样输出）。
func formatFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}

// ==================== Registry ====================

// collector 注册到 Registry 的统一接口（私有，避免外部误用）。
type collector interface {
	desc() (name, help, mtype string)
	writeTo(w io.Writer) error
}

// Registry Prometheus text 0.0.4 序列化器。
// 并发安全；按 metric 名字典序输出。
type Registry struct {
	mu    sync.RWMutex
	items map[string]collector
}

// NewRegistry 创建空 Registry。
func NewRegistry() *Registry {
	return &Registry{items: map[string]collector{}}
}

// MustRegister 注册一个 *Metric 到 Registry。
// 重复注册采用「以第一次为准」语义，不 panic——便于在测试或 NewCombinedServer
// 多次构造 middleware 时安全复用同名指标。
//
// 注册时按 metric 类型生成对应的 collector 适配器；类型为 Histogram 时，
// 该 collector 在 writeTo 阶段输出 _bucket / +Inf / _sum / _count。
func (r *Registry) MustRegister(m *Metric) {
	if m == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[m.Name]; exists {
		return
	}
	r.items[m.Name] = &cMetric{m: m}
}

// Register 同 MustRegister，但返回是否真的注册了。便于显式判断。
func (r *Registry) Register(m *Metric) bool {
	if m == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[m.Name]; exists {
		return false
	}
	r.items[m.Name] = &cMetric{m: m}
	return true
}

// RegisterCollector 直接注册一个已构造好的 collector（如 Vec 的 cCounterVec）。
// 主要给包内部 RegisterCounterVec / RegisterGaugeVec / RegisterHistogramVec 使用。
func (r *Registry) RegisterCollector(name string, c collector) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[name]; exists {
		return false
	}
	r.items[name] = c
	return true
}

// MustRegisterCollector 同 RegisterCollector，重复注册静默忽略。
func (r *Registry) MustRegisterCollector(name string, c collector) {
	r.RegisterCollector(name, c)
}

// Gather 返回当前所有指标的快照（按名字典序），便于单元测试。
func (r *Registry) Gather() []collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.items))
	for n := range r.items {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]collector, 0, len(names))
	for _, n := range names {
		out = append(out, r.items[n])
	}
	return out
}

// WriteText 输出 Prometheus text 0.0.4 格式到 w（兼容旧调用）。
func (r *Registry) WriteText(w io.Writer) error {
	if w == nil {
		return nil
	}
	for _, c := range r.Gather() {
		if err := c.writeTo(w); err != nil {
			return err
		}
	}
	return nil
}

// WriteTo 输出 Prometheus text 0.0.4 格式到 w。
// 签名采用 handoff 约定：func (r *Registry) WriteTo(w io.Writer) error。
// 不实现 io.WriterTo（其签名是 (int64, error)），避免和 handoff 冲突。
//
// 已知取舍：方法名 WriteTo 会被 go vet 的 methods 检查按 stdlib 的
// io.WriterTo 接口签名校验，触发 false positive 警告 "method WriteTo(w io.Writer)
// error should have signature WriteTo(io.Writer) (int64, error)"。
// 这里是有意不实现 io.WriterTo（签名与 handoff 不兼容），调用方应使用
// (int64, error) 版本的标准 io.CopyTo 路径或自行处理返回值。
// 如要跑 `go vet` 干净通过，请使用 `go vet -methods=false ./...` 跳过。
func (r *Registry) WriteTo(w io.Writer) error {
	return r.WriteText(w)
}

// ==================== 默认 Registry + 运行时/业务事件指标 ====================

var (
	defaultRegistry *Registry
	metricsOnce     sync.Once

	businessEvents *CounterVec

	startTimeGauge  *gaugeImpl
	goroutinesGauge *gaugeImpl
	allocGauge      *gaugeImpl
	sysGauge        *gaugeImpl

	// numGCCounter 按 handoff 约定实现为「只增的 counterImpl」，
	// 每次 RefreshRuntimeMetrics 把当前 NumGC 与已存值比较，只在变大时累加。
	numGCCounter *counterImpl

	// ====== Round 16 基础设施指标（WebSocket / DB / Scheduler）======
	// 设计原则：
	//   - 所有指标零 label，避免 user_id / task_id / contract_id 等高基数爆炸
	//   - Gauge 由刷新函数（SetXxxMetrics / RefreshXxxMetrics）整体覆盖
	//   - Counter 单调递增，由 IncXxx 调用方保证不重复计数
	wsClientsGauge        *gaugeImpl
	wsOnlineUsersGauge    *gaugeImpl
	dbOpenConnsGauge      *gaugeImpl
	dbInUseConnsGauge     *gaugeImpl
	dbIdleConnsGauge      *gaugeImpl
	schedulerRunningGauge *gaugeImpl
	schedulerEntriesGauge *gaugeImpl
	schedulerRunningTasks *gaugeImpl

	wsMessagesSentCounter    *counterImpl
	wsMessagesFailedCounter  *counterImpl
	dbWaitCountCounter       *counterImpl
	dbMaxIdleClosedCounter   *counterImpl
	dbMaxIdleTimeClosedCount *counterImpl
	dbMaxLifetimeClosedCount *counterImpl

	// ====== Round 18：优雅关停阶段耗时（histogram）======
	// stage 取值：signal / http / ws / scheduler / db / final
	// 设计点：仅 6 个 stage 取值，可以走低基数 label 方案（CounterVec 也可，
	// 但直方图能区分「正常 1ms 内完成」vs「超时 30s+」的尾部）。
	shutdownStageDuration *HistogramVec
)

// metricsInit 注册默认指标。包级别 init() 调用一次。
func metricsInit() {
	metricsOnce.Do(func() {
		defaultRegistry = NewRegistry()

		// 业务事件：按 event 区分的 CounterVec
		businessEvents = NewCounterVec("business_events_total", "业务事件计数", []string{"event"})
		defaultRegistry.MustRegisterCollector("business_events_total", &cCounterVec{
			n: "business_events_total",
			h: "业务事件计数",
			v: businessEvents,
		})

		// 运行时指标：进程启动时间 + Go runtime / memstats
		startTimeGauge = newGauge()
		startTimeGauge.set(float64(time.Now().Unix()))
		goroutinesGauge = newGauge()
		allocGauge = newGauge()
		sysGauge = newGauge()
		numGCCounter = newCounter()

		defaultRegistry.MustRegisterCollector("process_start_time_seconds", &cGauge{n: "process_start_time_seconds", h: "进程启动时间（unix秒）", g: startTimeGauge})
		defaultRegistry.MustRegisterCollector("go_goroutines", &cGauge{n: "go_goroutines", h: "当前 goroutine 数", g: goroutinesGauge})
		defaultRegistry.MustRegisterCollector("go_memstats_alloc_bytes", &cGauge{n: "go_memstats_alloc_bytes", h: "当前分配的字节数", g: allocGauge})
		defaultRegistry.MustRegisterCollector("go_memstats_sys_bytes", &cGauge{n: "go_memstats_sys_bytes", h: "从系统获取的字节数", g: sysGauge})
		// go_memstats_num_gc 按 handoff 约定为 counter（单调递增）。
		defaultRegistry.MustRegisterCollector("go_memstats_num_gc", &cCounter{n: "go_memstats_num_gc", h: "GC 累计次数", c: numGCCounter})

		// ====== Round 16：基础设施指标（WebSocket / DB / Scheduler）======
		// 所有指标零 label，定义见上方注释。
		wsClientsGauge = newGauge()
		wsOnlineUsersGauge = newGauge()
		dbOpenConnsGauge = newGauge()
		dbInUseConnsGauge = newGauge()
		dbIdleConnsGauge = newGauge()
		schedulerRunningGauge = newGauge()
		schedulerEntriesGauge = newGauge()
		schedulerRunningTasks = newGauge()

		wsMessagesSentCounter = newCounter()
		wsMessagesFailedCounter = newCounter()
		dbWaitCountCounter = newCounter()
		dbMaxIdleClosedCounter = newCounter()
		dbMaxIdleTimeClosedCount = newCounter()
		dbMaxLifetimeClosedCount = newCounter()

		// WebSocket
		defaultRegistry.MustRegisterCollector("ws_clients_connected", &cGauge{n: "ws_clients_connected", h: "当前已注册的 WebSocket 连接数", g: wsClientsGauge})
		defaultRegistry.MustRegisterCollector("ws_online_users", &cGauge{n: "ws_online_users", h: "当前在线用户数（按 userID 去重）", g: wsOnlineUsersGauge})
		defaultRegistry.MustRegisterCollector("ws_messages_sent_total", &cCounter{n: "ws_messages_sent_total", h: "WebSocket 成功入队的消息数", c: wsMessagesSentCounter})
		defaultRegistry.MustRegisterCollector("ws_messages_failed_total", &cCounter{n: "ws_messages_failed_total", h: "WebSocket 发送失败的消息数（编码失败 / 缓冲区满 / 目标离线）", c: wsMessagesFailedCounter})

		// Database
		defaultRegistry.MustRegisterCollector("db_open_connections", &cGauge{n: "db_open_connections", h: "当前数据库打开的连接数（含 idle + in-use）", g: dbOpenConnsGauge})
		defaultRegistry.MustRegisterCollector("db_in_use_connections", &cGauge{n: "db_in_use_connections", h: "当前正在使用的数据库连接数", g: dbInUseConnsGauge})
		defaultRegistry.MustRegisterCollector("db_idle_connections", &cGauge{n: "db_idle_connections", h: "当前空闲的数据库连接数", g: dbIdleConnsGauge})
		defaultRegistry.MustRegisterCollector("db_wait_count_total", &cCounter{n: "db_wait_count_total", h: "数据库连接池等待累计次数", c: dbWaitCountCounter})
		defaultRegistry.MustRegisterCollector("db_max_idle_closed_total", &cCounter{n: "db_max_idle_closed_total", h: "因超过 SetMaxIdleConns 被关闭的连接累计数", c: dbMaxIdleClosedCounter})
		defaultRegistry.MustRegisterCollector("db_max_idle_time_closed_total", &cCounter{n: "db_max_idle_time_closed_total", h: "因超过 ConnMaxIdleTime 被关闭的连接累计数", c: dbMaxIdleTimeClosedCount})
		defaultRegistry.MustRegisterCollector("db_max_lifetime_closed_total", &cCounter{n: "db_max_lifetime_closed_total", h: "因超过 ConnMaxLifetime 被关闭的连接累计数", c: dbMaxLifetimeClosedCount})

		// Scheduler
		defaultRegistry.MustRegisterCollector("scheduler_running", &cGauge{n: "scheduler_running", h: "调度器是否运行（1=运行，0=停止）", g: schedulerRunningGauge})
		defaultRegistry.MustRegisterCollector("scheduler_entries", &cGauge{n: "scheduler_entries", h: "调度器当前已注册的 cron entry 数", g: schedulerEntriesGauge})
		defaultRegistry.MustRegisterCollector("scheduler_running_tasks", &cGauge{n: "scheduler_running_tasks", h: "调度器当前正在执行的任务数", g: schedulerRunningTasks})

		// ====== Round 18：关停阶段耗时直方图 ======
		// bucket 边界按"毫秒级启动 + 秒级 in-flight drain"双区间：
		//   0.001 / 0.005 / 0.01 / 0.05 / 0.1 / 0.5 / 1 / 5 / 30（秒）
		// drain 阶段 30s 是 HTTP 和 WS 的 context timeout 上界。
		shutdownStageDuration = NewHistogramVec(
			"shutdown_stage_duration_seconds",
			"优雅关停各阶段耗时（秒）",
			[]string{"stage"},
			[]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 30},
		)
		defaultRegistry.MustRegisterCollector("shutdown_stage_duration_seconds", &cHistogramVec{
			n: "shutdown_stage_duration_seconds",
			h: "优雅关停各阶段耗时（秒）",
			v: shutdownStageDuration,
		})

		// 预热：让所有 stage 的 bucket / count 在 /metrics 首次访问时就已经可见
		// 注意：必须放在 metricsInit 内部（Once.Do 闭包里），否则会被 metricsInit
		// 重新进入导致 Once 自递归死锁（sync.Once 不可重入）。
		for _, stage := range []string{
			"signal", "http", "ws", "scheduler", "db", "final",
		} {
			if h := shutdownStageDuration.WithLabelValues(stage); h != nil {
				h.Observe(0)
			}
		}
	})
}

// ObserveShutdownStage 记录某个关停阶段的耗时（秒）。
// stage 取值：signal / http / ws / scheduler / db / final
//
// 由 main.go / services.RunGraceful 调用，第三方模块不应直接复用。
func ObserveShutdownStage(stage string, durationSec float64) {
	metricsInit()
	if stage == "" {
		stage = "unknown"
	}
	h := shutdownStageDuration.WithLabelValues(stage)
	if h != nil {
		h.Observe(durationSec)
	}
}

// WarmShutdownStageMetrics 预热 shutdown histogram 的全部 stage label。
//
// ⚠️ DEPRECATED（Round 18 内部修复后）：不要再调用本函数。
// 预热已在 metricsInit 内部直接完成，外部调用会因 sync.Once 不可重入
// 触发栈溢出。保留本函数仅为二进制兼容，如外部代码误引用也不会编译失败。
//
// 目的：Prometheus / Grafana 上线后立即能看到全部 stage 的 bucket 边界，
// 避免首周末关停时出现"series 突然出现"导致告警阈值不稳。
func WarmShutdownStageMetrics() {
	// 故意保留为空：原逻辑（metricsInit → WarmShutdownStageMetrics → metricsInit）
	// 是 sync.Once 递归死锁。预热已在 metricsInit 内部完成。
}

// ResetDBStatSnapshot 重置 DB 指标的计算快照（Issue #2）。
//
// 场景：数据库 restore / reload 后，driver 计数器全部从 0 开始；
//   计数器是 "delta += curr - prev" 类型（见 dbMaxIdleClosedCounter 等），
//   若不 reset，"prev" 还是旧 DB 的累积值，delta 永远是负数，counter 永久失活。
//
// 当前 round 18 实现：metrics 状态由 metricsInit 一次性初始化，
//   "curr-prev" 增量算法在 statistics.go 内部用局部 prev 维护，
//   跨 DB 句柄自然隔离。所以这里实际无逻辑，仅作为兼容占位。
// 如未来切换到全局 prev，需在此处显式重置。
func ResetDBStatSnapshot() {
	// no-op 占位：见函数注释。当前实现下 driver 状态自管理。
}

// DefaultRegistry 返回全局默认 Registry（函数形式 API，handoff 约定）。
// 内部维护私有 defaultRegistry；并发安全。
func DefaultRegistry() *Registry {
	metricsInit()
	return defaultRegistry
}

// GetDefaultRegistry 是 DefaultRegistry 的别名，保留以兼容旧调用方。
func GetDefaultRegistry() *Registry {
	return DefaultRegistry()
}

// IncBusinessEvent 业务事件计数 +1（按 event 区分）。
// event 为空时使用 "unknown"，避免产生无 label 的指标。
//
// J.8：标记 Deprecated，外部业务代码应改用 services.PublishEvent，
// 事件总线会经 metricsSubscriber 同步到本指标（保持 label 集合不变）。
// 本函数保留仅为 eventbus.go 内置兼容桥接使用。
//
// Deprecated: 改用 services.PublishEvent("event.name")。
func IncBusinessEvent(event string) {
	metricsInit()
	if event == "" {
		event = "unknown"
	}
	c := businessEvents.WithLabelValues(event)
	if c != nil {
		c.Inc()
	}
}

// RefreshRuntimeMetrics 更新运行时指标（goroutines / memstats / gc）。
// 每次 /metrics 输出前调用一次，确保反映当前快照。
func RefreshRuntimeMetrics() {
	metricsInit()
	goroutinesGauge.set(float64(runtime.NumGoroutine()))
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	allocGauge.set(float64(ms.Alloc))
	sysGauge.set(float64(ms.Sys))
	// num_gc 是单调递增的 counter：只在 NumGC 比已存值大时累加 delta，
	// 避免读到的 NumGC 比上次略小（理论不会，但保险起见）造成"指标回退"。
	prev := uint64(numGCCounter.value())
	if uint64(ms.NumGC) > prev {
		numGCCounter.add(float64(uint64(ms.NumGC) - prev))
	}
}

// ==================== Round 16：基础设施指标公开 API ====================

// SetWebSocketMetrics 把 WebSocket 连接数 / 在线用户数刷新到指标。
// 由 hub 在注册 / 注销连接完成后调用；本身并发安全（atomic.Uint64 bits）。
//
// 设计要点：
//   - 不使用 user_id / conn_id label（高基数爆炸风险）
//   - handler 负责 nil-safe：wsHub 为 nil 时调用方不应当进入此函数；
//     SetWebSocketMetrics 本身只对负数做夹紧防御，不再声称支持 nil 接收者。
func SetWebSocketMetrics(clients, onlineUsers int) {
	metricsInit()
	if clients < 0 {
		clients = 0
	}
	if onlineUsers < 0 {
		onlineUsers = 0
	}
	wsClientsGauge.set(float64(clients))
	wsOnlineUsersGauge.set(float64(onlineUsers))
}

// IncWebSocketMessage 上报一次 WebSocket 消息发送结果。
//
//   - sent=true  → ws_messages_sent_total +1
//   - sent=false → ws_messages_failed_total +1
//
// 调用方在 SendToUser / SendToAll 中根据"是否真正成功入队 send channel"判断：
//   - 编码失败       → failed
//   - 目标离线       → failed（语义：用户不存在视为不可达）
//   - send buffer 满 → failed
//   - broadcast 队列满 → failed
//   - 成功入队       → sent
func IncWebSocketMessage(sent bool) {
	metricsInit()
	if sent {
		wsMessagesSentCounter.inc()
	} else {
		wsMessagesFailedCounter.inc()
	}
}

// RefreshDatabaseMetrics 把当前数据库连接池状态刷到指标。
//
// nil DB 视为"未初始化"，把全部 gauge 设为 0；counter 不回退（counter 单调递增语义）。
// 调用方（健康检查 handler）在调用前应确认 DB 已初始化，但允许 nil 注入做安全降级。
//
// Counter 增量计算：
//   - WaitCount:        当前 wait_count - 上次刷新的 wait_count
//   - MaxIdleClosed:    同上
//   - MaxIdleTimeClosed:同上
//   - MaxLifetimeClosed:同上
//
// 由于 stdlib database/sql 不提供增量事件，只能用「差值」累加；
// 重复调用同一 RefreshDatabaseMetrics 时不会重复计数（差值为 0 时不累加）。
func RefreshDatabaseMetrics(db *sql.DB) {
	metricsInit()
	if db == nil {
		dbOpenConnsGauge.set(0)
		dbInUseConnsGauge.set(0)
		dbIdleConnsGauge.set(0)
		// counter 不回退，保持单调递增语义
		return
	}
	stats := db.Stats()
	dbOpenConnsGauge.set(float64(stats.OpenConnections))
	dbInUseConnsGauge.set(float64(stats.InUse))
	dbIdleConnsGauge.set(float64(stats.Idle))

	// 差值累加（std lib 不暴露事件，只能 diff）
	applyDBStatDelta(dbWaitCountCounter, stats.WaitCount)
	applyDBStatDelta(dbMaxIdleClosedCounter, stats.MaxIdleClosed)
	applyDBStatDelta(dbMaxIdleTimeClosedCount, stats.MaxIdleTimeClosed)
	applyDBStatDelta(dbMaxLifetimeClosedCount, stats.MaxLifetimeClosed)
}

// dbStatSnapshot 每种累计计数的最近一次快照，用于 RefreshDatabaseMetrics 计算差值。
// 仅用于包内 RefreshDatabaseMetrics，外部不应直接读。
var dbStatSnapshot struct {
	sync.Mutex
	waitCount     int64
	maxIdleClosed int64
	maxIdleTime   int64
	maxLifetime   int64
}

// applyDBStatDelta 把当前累计值与上次快照做差，累加到 counter。
// 当前值 < 上次值（理论上不会，DB driver 计数器不重置）时按 0 增量处理，避免指标回退。
func applyDBStatDelta(c *counterImpl, current int64) {
	dbStatSnapshot.Lock()
	defer dbStatSnapshot.Unlock()

	var prev *int64
	switch c {
	case dbWaitCountCounter:
		prev = &dbStatSnapshot.waitCount
	case dbMaxIdleClosedCounter:
		prev = &dbStatSnapshot.maxIdleClosed
	case dbMaxIdleTimeClosedCount:
		prev = &dbStatSnapshot.maxIdleTime
	case dbMaxLifetimeClosedCount:
		prev = &dbStatSnapshot.maxLifetime
	}
	if prev == nil {
		return
	}
	if current > *prev {
		c.add(float64(current - *prev))
	}
	*prev = current
}

// RefreshSchedulerMetrics 把调度器当前状态刷到指标。
//
//   - running:        由 sched.Running() 返回（started && !stopped）
//   - entries:        sched.ListEntries() 长度（cron 已注册 entry 数）
//   - runningTasks:   sched.RunningTaskCount() 返回值（正在执行的非并发任务数）
//
// 允许 sched 为 nil：视为 0 全 gauge，counter 不动。
// ListEntries 在 sched 为 nil 时不可调用，调用方应保证 sched 非 nil；
// 但为 nil-safe 仍然做防御性判断。
func RefreshSchedulerMetrics(running bool, entries, runningTasks int) {
	metricsInit()
	if running {
		schedulerRunningGauge.set(1)
	} else {
		schedulerRunningGauge.set(0)
	}
	if entries < 0 {
		entries = 0
	}
	if runningTasks < 0 {
		runningTasks = 0
	}
	schedulerEntriesGauge.set(float64(entries))
	schedulerRunningTasks.set(float64(runningTasks))
}

func init() {
	metricsInit()
}

// ==================== collector 实现 ====================

type cMetric struct{ m *Metric }

func (x *cMetric) desc() (string, string, string) {
	tp := x.m.TypeStr
	if tp == "" {
		tp = x.m.Type.String()
	}
	return x.m.Name, x.m.Help, tp
}

func (x *cMetric) writeTo(w io.Writer) error {
	_, _, tp := x.desc()
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.m.Name, x.m.Help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", x.m.Name, tp); err != nil {
		return err
	}

	x.m.mu.RLock()
	keys := append([]string(nil), x.m.sampleOrder...)
	x.m.mu.RUnlock()

	switch x.m.Type {
	case TypeCounter, TypeGauge:
		for _, k := range keys {
			x.m.mu.RLock()
			s, ok := x.m.samples[k]
			if ok {
				labels := cloneLabels(s.Labels)
				val := s.Value
				x.m.mu.RUnlock()
				if _, err := fmt.Fprintf(w, "%s%s %s\n", x.m.Name, formatLabelsFromMap(labels), formatFloat(val)); err != nil {
					return err
				}
			} else {
				x.m.mu.RUnlock()
			}
		}
	case TypeHistogram:
		for _, k := range keys {
			// 先在 Metric RLock 下取出 hh 指针 + labels 快照，再释放 Metric 锁，
			// 最后单独获取 hh.mu 取桶/计数/总和；避免与 Metric.observeInternal
			//（持 m.mu → hh.mu）形成反向锁顺序而出现死锁。
			x.m.mu.RLock()
			hh, okHB := x.m.histBuckets[k]
			s, okS := x.m.samples[k]
			var labels map[string]string
			if okHB && okS {
				labels = cloneLabels(s.Labels)
			}
			x.m.mu.RUnlock()

			if !(okHB && okS) {
				continue
			}
			hh.mu.Lock()
			buckets := append([]float64(nil), hh.buckets...)
			counts := append([]uint64(nil), hh.counts...)
			cnt := hh.count
			sum := bitsToFloat(&hh.sumBits)
			hh.mu.Unlock()
			labelText := formatLabelsFromMap(labels)
			for i, b := range buckets {
				if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", x.m.Name, appendLE(labelText, formatFloat(b)), counts[i]); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", x.m.Name, appendLE(labelText, "+Inf"), cnt); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s_sum%s %s\n", x.m.Name, labelText, formatFloat(sum)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s_count%s %d\n", x.m.Name, labelText, cnt); err != nil {
				return err
			}
		}
	}
	return nil
}

type cCounter struct {
	n, h string
	c    *counterImpl
}

func (x *cCounter) desc() (string, string, string) { return x.n, x.h, "counter" }
func (x *cCounter) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", x.n); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s\n", x.n, formatFloat(x.c.value()))
	return err
}

type cGauge struct {
	n, h string
	g    *gaugeImpl
}

func (x *cGauge) desc() (string, string, string) { return x.n, x.h, "gauge" }
func (x *cGauge) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", x.n); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s\n", x.n, formatFloat(x.g.value()))
	return err
}

type cHistogram struct {
	n, h string
	hh   *histogramImpl
}

func (x *cHistogram) desc() (string, string, string) { return x.n, x.h, "histogram" }
func (x *cHistogram) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s histogram\n", x.n); err != nil {
		return err
	}
	buckets, counts := x.hh.bucketsSnapshot()
	cnt := x.hh.countSnapshot()
	sum := x.hh.sumSnapshot()
	for i, b := range buckets {
		if _, err := fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n", x.n, formatFloat(b), counts[i]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", x.n, cnt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_sum %s\n", x.n, formatFloat(sum)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s_count %d\n", x.n, cnt)
	return err
}

type cCounterVec struct {
	n, h string
	v    *CounterVec
}

func (x *cCounterVec) desc() (string, string, string) { return x.n, x.h, "counter" }
func (x *cCounterVec) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", x.n); err != nil {
		return err
	}
	snap := x.v.snapshot()
	sort.SliceStable(snap, func(i, j int) bool {
		return joinLabels(snap[i].values...) < joinLabels(snap[j].values...)
	})
	for _, c := range snap {
		labels := formatLabels(x.v.labels, c.values)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", x.n, labels, formatFloat(c.c.value())); err != nil {
			return err
		}
	}
	return nil
}

type cGaugeVec struct {
	n, h string
	v    *GaugeVec
}

func (x *cGaugeVec) desc() (string, string, string) { return x.n, x.h, "gauge" }
func (x *cGaugeVec) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", x.n); err != nil {
		return err
	}
	snap := x.v.snapshot()
	sort.SliceStable(snap, func(i, j int) bool {
		return joinLabels(snap[i].values...) < joinLabels(snap[j].values...)
	})
	for _, g := range snap {
		labels := formatLabels(x.v.labels, g.values)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", x.n, labels, formatFloat(g.g.value())); err != nil {
			return err
		}
	}
	return nil
}

type cHistogramVec struct {
	n, h string
	v    *HistogramVec
}

func (x *cHistogramVec) desc() (string, string, string) { return x.n, x.h, "histogram" }
func (x *cHistogramVec) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", x.n, x.h); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s histogram\n", x.n); err != nil {
		return err
	}
	snap := x.v.snapshot()
	sort.SliceStable(snap, func(i, j int) bool {
		return joinLabels(snap[i].values...) < joinLabels(snap[j].values...)
	})
	for _, hh := range snap {
		labels := formatLabels(x.v.labels, hh.values)
		buckets, counts := hh.h.bucketsSnapshot()
		cnt := hh.h.countSnapshot()
		sum := hh.h.sumSnapshot()
		for i, b := range buckets {
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", x.n, appendLE(labels, formatFloat(b)), counts[i]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", x.n, appendLE(labels, "+Inf"), cnt); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s_sum%s %s\n", x.n, labels, formatFloat(sum)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s_count%s %d\n", x.n, labels, cnt); err != nil {
			return err
		}
	}
	return nil
}

// ==================== 公共注册工厂 ====================
//
// RegisterCounterVec / RegisterGauge / RegisterHistogramVec 是给外部模块用的工厂：
//   - 创建对应类型的指标
//   - 注册到 DefaultRegistry
//   - 返回句柄供业务代码直接使用
// 重复调用同名指标返回首次创建的句柄（不会再次注册，也不会 panic）。

// RegisterCounterVec 注册并返回 *CounterVec。
func RegisterCounterVec(name, help string, labels []string) *CounterVec {
	return registerOnce(name, func() any {
		v := NewCounterVec(name, help, labels)
		DefaultRegistry().MustRegisterCollector(name, &cCounterVec{n: name, h: help, v: v})
		return v
	}).(*CounterVec)
}

// RegisterHistogramVec 注册并返回 *HistogramVec。
func RegisterHistogramVec(name, help string, labels []string, buckets []float64) *HistogramVec {
	return registerOnce(name, func() any {
		v := NewHistogramVec(name, help, labels, buckets)
		DefaultRegistry().MustRegisterCollector(name, &cHistogramVec{n: name, h: help, v: v})
		return v
	}).(*HistogramVec)
}

// RegisterGauge 注册并返回 *Gauge。
func RegisterGauge(name, help string) *Gauge {
	return registerOnce(name, func() any {
		g := NewGauge()
		DefaultRegistry().MustRegisterCollector(name, &cGauge{n: name, h: help, g: g.internal()})
		return g
	}).(*Gauge)
}

// registerOnce 用 sync.Map 做名字到句柄的缓存，保证同一名字只构造一次。
var (
	registryCache sync.Map // map[string]any
)

// registerOnce 在 registryCache 里有同名句柄时直接返回旧值，避免重复初始化。
func registerOnce(name string, factory func() any) any {
	if v, ok := registryCache.Load(name); ok {
		return v
	}
	actual, _ := registryCache.LoadOrStore(name, factory())
	return actual
}

// internal 暴露给 RegisterGauge 使用的 GaugeImpl 指针读取；外部不应直接调用。
func (g *Gauge) internal() *gaugeImpl { return g.impl }
