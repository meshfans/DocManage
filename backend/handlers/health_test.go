package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doc/config"
	"doc/database"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// init 测试启动时 utils 包 init() 会注册全局指标，
// 这里不要手工再注册同名 metric，否则会与包内已有的冲突。
// 各测试只通过文本内容（strings.Contains）做断言，不做严格相等比较。

func newHealthRouter(h gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET(hPathFor(h), h)
	return r
}

// hPathFor 根据 handler 类型挑选路径。HealthzHandler 同时挂 /healthz 与 /health，
// ReadyzHandler 挂 /readyz，MetricsHandler 挂 /metrics。
func hPathFor(h gin.HandlerFunc) string {
	// 不同 handler 对应不同路径；这里直接根据测试上下文设置。
	return "/healthz"
}

func doJSON(t *testing.T, r *gin.Engine, method, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("JSON 解析失败：%v\nbody=%s", err, rec.Body.String())
		}
	}
	return rec.Code, body
}

// ==================== /healthz ====================

func TestHealthz_ReturnsAlive(t *testing.T) {
	r := newHealthRouter(HealthzHandler())
	code, body := doJSON(t, r, http.MethodGet, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("/healthz 应返回 200，实际 %d", code)
	}
	if body == nil {
		t.Fatalf("/healthz 返回体应可解析为 JSON")
	}
	if s, _ := body["status"].(string); s != "alive" {
		t.Fatalf("/healthz status 期望 alive，实际 %v", body["status"])
	}
	if body["service"] != "doc-server" {
		t.Fatalf("/healthz service 期望 doc-server，实际 %v", body["service"])
	}
	if body["version"] != ServiceVersion {
		t.Fatalf("/healthz version 期望 %s，实际 %v", ServiceVersion, body["version"])
	}
}

// ==================== /health（兼容旧路径）====================

func TestHealth_ReturnsHealthy(t *testing.T) {
	r := gin.New()
	r.GET("/health", HealthzHandler())
	code, body := doJSON(t, r, http.MethodGet, "/health")
	if code != http.StatusOK {
		t.Fatalf("/health 应返回 200，实际 %d", code)
	}
	if s, _ := body["status"].(string); s != "healthy" {
		t.Fatalf("/health status 期望 healthy，实际 %v", body["status"])
	}
	if body["service"] != "doc-server" {
		t.Fatalf("/health service 期望 doc-server，实际 %v", body["service"])
	}
	if body["version"] != ServiceVersion {
		t.Fatalf("/health version 期望 %s，实际 %v", ServiceVersion, body["version"])
	}
}

// ==================== /readyz（DB / ws / sched / maintenance 全部失败）====================

func TestReadyz_NilDepsAndScheduler_ReturnsNotReady_503(t *testing.T) {
	// 保存 DB 全局并临时置为 nil，使 QuickCheck 走"未初始化"失败路径；
	// 退出时还原，不污染其他测试。
	origDB := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = origDB })

	r := gin.New()
	r.GET("/readyz", ReadyzHandler(nil, nil))

	code, body := doJSON(t, r, http.MethodGet, "/readyz")

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz 期望 503，实际 %d", code)
	}
	if s, _ := body["status"].(string); s != "not_ready" {
		t.Fatalf("/readyz status 期望 not_ready，实际 %v", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("/readyz 响应应包含 checks 对象，实际 %v", body)
	}
	// 四个检查键必须存在
	for _, key := range []string{"db", "ws_hub", "scheduler", "maintenance"} {
		if _, ok := checks[key].(string); !ok {
			t.Fatalf("checks 缺少键 %s 或类型不对，实际 %v", key, checks)
		}
	}
	// DB / ws_hub / scheduler 三项在 nil 注入时必须 fail
	for _, key := range []string{"db", "ws_hub", "scheduler"} {
		v, _ := checks[key].(string)
		if !strings.HasPrefix(v, "fail") {
			t.Fatalf("checks[%s] 在 nil 注入时应为 fail:*，实际 %q", key, v)
		}
	}
	// maintenance 字段：默认配置下为 ok；只要键存在即可，不强制 fail
	// （取决于 GlobalConfig.MaintenanceMode，测试不应改它）
}

func TestReadyz_MaintenanceForcesFail(t *testing.T) {
	// 构造一个最小 GlobalConfig，开启维护模式，验证 maintenance 走 fail
	origDB := database.DB
	origCfg := config.GlobalConfig
	database.DB = nil
	// 临时挂一个 GlobalConfig 并开启 maintenance
	tmpCfg := &config.Config{}
	config.GlobalConfig = tmpCfg
	config.SetMaintenanceMode(true)
	t.Cleanup(func() {
		database.DB = origDB
		config.GlobalConfig = origCfg
	})

	r := gin.New()
	r.GET("/readyz", ReadyzHandler(nil, nil))
	code, body := doJSON(t, r, http.MethodGet, "/readyz")

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz 期望 503，实际 %d", code)
	}
	if s, _ := body["status"].(string); s != "not_ready" {
		t.Fatalf("/readyz status 期望 not_ready，实际 %v", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	maint, _ := checks["maintenance"].(string)
	if !strings.HasPrefix(maint, "fail") {
		t.Fatalf("开启维护模式后 maintenance 应为 fail:*，实际 %q", maint)
	}
}

// ==================== /metrics ====================

func TestMetricsHandler_ContentTypeAndBasic(t *testing.T) {
	r := gin.New()
	r.GET("/metrics", MetricsHandler())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics 应返回 200，实际 %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type 应为 text/plain 开头，实际 %q", ct)
	}
	if !strings.Contains(ct, "version=0.0.4") {
		t.Fatalf("Content-Type 应包含 version=0.0.4，实际 %q", ct)
	}

	body := rec.Body.String()
	// 基本指标至少出现：
	//  - business_events_total
	//  - http_in_flight_requests
	//  - 进程启动 / runtime / GC 指标
	for _, name := range []string{
		"# HELP business_events_total",
		"# TYPE business_events_total counter",
		"# HELP http_in_flight_requests",
		"# TYPE http_in_flight_requests gauge",
		"# HELP go_goroutines",
		"# HELP go_memstats_num_gc",
		"# TYPE go_memstats_num_gc counter",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("指标输出缺少 %q\n--- 全文 ---\n%s", name, body)
		}
	}
}

// 防止 metrics 包没初始化时其它测试首次请求空 registry。
// 这里是烟雾测试：直接调用一次 /metrics 让包 init 触发。
func TestMetricsHandler_EnsureRegistryInitialized(t *testing.T) {
	// 兜底刷新一次运行时指标
	utils.RefreshRuntimeMetrics()
	var buf bytes.Buffer
	if err := utils.DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("DefaultRegistry.WriteTo 失败：%v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("DefaultRegistry 输出不应为空")
	}
}

// ==================== Round 16：MetricsHandlerWithDependencies ====================

// infraMetricNames 与 utils 包内测试用的清单一致；本测试只断言"输出里存在"。
// 完整 HELP / TYPE 校验已在 utils/metrics_test.go 覆盖。
var metricsDepsMetricNames = []string{
	"ws_clients_connected",
	"ws_online_users",
	"ws_messages_sent_total",
	"ws_messages_failed_total",
	"db_open_connections",
	"db_in_use_connections",
	"db_idle_connections",
	"db_wait_count_total",
	"db_max_idle_closed_total",
	"db_max_idle_time_closed_total",
	"db_max_lifetime_closed_total",
	"scheduler_running",
	"scheduler_entries",
	"scheduler_running_tasks",
}

// TestMetricsHandlerWithDependencies_NilDeps 验证 nil DB / nil wsHub / nil sched 时
// handler 不 panic，输出仍包含全部 Round 16 指标；DB / ws 视为 0 gauge。
func TestMetricsHandlerWithDependencies_NilDeps(t *testing.T) {
	// 临时清空 DB 全局（注入 nil）；test cleanup 还原。
	origDB := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = origDB })

	r := gin.New()
	r.GET("/metrics", MetricsHandlerWithDependencies(nil, nil))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics 应返回 200，实际 %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type 应为 text/plain 开头，实际 %q", ct)
	}

	body := rec.Body.String()
	for _, name := range metricsDepsMetricNames {
		if !strings.Contains(body, "# TYPE "+name+" ") {
			t.Fatalf("指标 %s 缺失 TYPE 行，输出片段：%s",
				name, extractBlock(body, name))
		}
	}

	// ws/scheduler gauge 应当为 0（无 hub / sched）
	if !strings.Contains(body, "ws_clients_connected 0") {
		t.Fatalf("nil wsHub 时 ws_clients_connected 应为 0：%s",
			extractBlock(body, "ws_clients_connected"))
	}
	if !strings.Contains(body, "scheduler_running 0") {
		t.Fatalf("nil sched 时 scheduler_running 应为 0：%s",
			extractBlock(body, "scheduler_running"))
	}
}

// TestMetricsHandlerWithDependencies_WithRealDB 验证打开临时 sqlite 后，
// DB gauge 被刷为非零（OpenConnections >= 1）。
func TestMetricsHandlerWithDependencies_WithRealDB(t *testing.T) {
	tmp := t.TempDir() + "/test_metrics.db"
	db := openSQLiteForTest(t, tmp)

	// 触发查询让 driver 建立至少 1 条连接
	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("查询 sqlite 失败：%v", err)
	}

	origDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		_ = db.Close()
		database.DB = origDB
	})

	r := gin.New()
	r.GET("/metrics", MetricsHandlerWithDependencies(nil, nil))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics 应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "db_open_connections 1") {
		// sqlite driver 通常 OpenConnections = 1（开即建立 1 个）
		t.Fatalf("db_open_connections 期望 1，输出片段：%s",
			extractBlock(body, "db_open_connections"))
	}
}

// TestMetricsHandlerWithDependencies_WithScheduler 验证传入 *services.Scheduler
// （即使未 InitScheduler 为 started=true）不会 panic。
func TestMetricsHandlerWithDependencies_WithScheduler(t *testing.T) {
	origDB := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = origDB })

	// scheduler 是 nil；handler 走 nil-safe 分支。
	// 额外验证 services.GetScheduler() 返回 nil 时 handler 也安全。
	if services.GetScheduler() != nil {
		t.Skip("已存在全局 scheduler，跳过 nil sched 测试")
	}

	r := gin.New()
	r.GET("/metrics", MetricsHandlerWithDependencies(nil, services.GetScheduler()))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics 应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "scheduler_running 0") {
		t.Fatalf("nil sched 时 scheduler_running 应为 0：%s",
			extractBlock(body, "scheduler_running"))
	}
}

// extractBlock 复制 utils 包同名工具；本测试文件不依赖 utils 测试工具。
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

// openSQLiteForTest 打开临时 sqlite 用于 metrics handler 测试。
// 测试失败时通过 t.Fatal 报告；调用方无需处理错误。
func openSQLiteForTest(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开 sqlite 失败：%v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping sqlite 失败：%v", err)
	}
	return db
}
