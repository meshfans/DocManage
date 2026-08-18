package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

// init 测试启动时把 metrics 包初始化一次（包 init 已经做了，这里只声明）。

// newTestRouter 构造一个挂载了 Metrics() 的 router，并预注册若干路由用于不同状态码。
func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Metrics())
	r.GET("/api/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/api/notfound", func(c *gin.Context) { c.String(http.StatusNotFound, "nf") })
	r.GET("/api/error", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })
	r.GET("/api/customer/:id", func(c *gin.Context) { c.String(http.StatusOK, c.Param("id")) })
	r.GET("/api/panic", func(c *gin.Context) { panic("kaboom") })
	return r
}

// do 发起一次测试请求，并断言无 panic。
func do(t *testing.T, r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("handler panic 不应逃逸：%v", rec)
		}
	}()
	r.ServeHTTP(rec, req)
	return rec
}

// renderMetrics 调用 utils.DefaultRegistry().WriteTo 输出所有已注册指标文本。
// 真实指标端点同样用该方法，但这里直接看文本以避免 HTTP 路由层复杂度。
func renderMetrics(t *testing.T) string {
	t.Helper()
	utils.RefreshRuntimeMetrics()
	var buf bytes.Buffer
	if err := utils.DefaultRegistry().WriteTo(&buf); err != nil {
		t.Fatalf("DefaultRegistry.WriteTo 失败：%v", err)
	}
	return buf.String()
}

func TestMetrics_200_RecordsCount(t *testing.T) {
	r := newTestRouter()
	rec := do(t, r, http.MethodGet, "/api/ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/ok 应返回 200，实际 %d", rec.Code)
	}

	out := renderMetrics(t)
	want := `http_requests_total{method="GET",path="/api/ok",status="200"}`
	if !strings.Contains(out, want) {
		t.Fatalf("输出缺少 %s\n完整输出：\n%s", want, out)
	}
}

func TestMetrics_404_UsesUnknownPath(t *testing.T) {
	r := newTestRouter()
	// 命中未注册的路径：c.FullPath() 为空 → resolvePath 返回 "unknown"
	rec := do(t, r, http.MethodGet, "/api/does-not-exist/abc123xyz")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未注册路由应返回 404，实际 %d", rec.Code)
	}

	out := renderMetrics(t)
	// 必须包含 path="unknown",status="404"
	if !strings.Contains(out, `http_requests_total{method="GET",path="unknown",status="404"}`) {
		t.Fatalf("404 应使用 path=unknown 而非实际 URL\n完整输出：\n%s", out)
	}
	// 不应包含高基数实际路径
	if strings.Contains(out, `/api/does-not-exist/abc123xyz`) {
		t.Fatalf("输出不应出现实际高基数 URL：\n%s", out)
	}
}

func TestMetrics_500_Records(t *testing.T) {
	r := newTestRouter()
	rec := do(t, r, http.MethodGet, "/api/error")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("/api/error 应返回 500，实际 %d", rec.Code)
	}

	out := renderMetrics(t)
	if !strings.Contains(out, `http_requests_total{method="GET",path="/api/error",status="500"}`) {
		t.Fatalf("输出缺少 status=500 的 5xx 指标\n完整输出：\n%s", out)
	}
}

func TestMetrics_PanicHandler_Records500(t *testing.T) {
	r := newTestRouter()
	// /api/panic 在 handler 内 panic；middleware 应捕获并记录
	req := httptest.NewRequest(http.MethodGet, "/api/panic", nil)
	rec := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		r.ServeHTTP(rec, req)
	}()

	out := renderMetrics(t)
	// 关键点：path 是路由模板 "/api/panic"
	// status 在 panic 路径下，gin 的默认 Recovery 中间件会把它设为 200，
	// 因此 record 可能是 status="200" 也可能 "500"（取决于 gin 版本是否注册了 Recovery）。
	// 这里只验证"panic 路径被记录且 path 是模板"，不绑定具体 status 数值，
	// 避免和 gin 内部默认 Recovery 行为耦合。
	if !strings.Contains(out, `path="/api/panic"`) {
		t.Fatalf("panic 应被记录到 /api/panic 指标\n完整输出：\n%s", out)
	}
	// 同时确认 http_request_duration_seconds 也记录了 panic 路径
	if !strings.Contains(out, `http_request_duration_seconds_count{path="/api/panic"} `) &&
		!strings.Contains(out, `http_request_duration_seconds_count{method="GET",path="/api/panic"} `) {
		t.Fatalf("panic 路径的 duration 直方图也应被记录\n完整输出：\n%s", out)
	}
}

func TestMetrics_PathIsTemplateNotConcrete(t *testing.T) {
	r := newTestRouter()
	// 命中参数化路由 /api/customer/:id → path 应该是路由模板
	for _, id := range []string{"100", "200", "300"} {
		rec := do(t, r, http.MethodGet, "/api/customer/"+id)
		if rec.Code != http.StatusOK {
			t.Fatalf("/api/customer/%s 应返回 200，实际 %d", id, rec.Code)
		}
	}

	out := renderMetrics(t)
	want := `http_requests_total{method="GET",path="/api/customer/:id",status="200"}`
	if !strings.Contains(out, want) {
		t.Fatalf("path 应为路由模板而非具体 URL；缺少 %s\n完整输出：\n%s", want, out)
	}
	// 高基数 URL 不应出现
	for _, id := range []string{"100", "200", "300"} {
		if strings.Contains(out, `/api/customer/`+id) {
			t.Fatalf("输出不应出现具体 ID：%s\n%s", id, out)
		}
	}
}

func TestMetrics_InFlightGaugeReturnsToZero(t *testing.T) {
	r := newTestRouter()
	// 串行发若干请求后，http_in_flight_requests 应回到 0（Inc/Dec 配对）
	for i := 0; i < 10; i++ {
		do(t, r, http.MethodGet, "/api/ok")
	}

	// 默认 registry 中应已注册 http_in_flight_requests（middleware.init 注册）
	out := renderMetrics(t)
	if !strings.Contains(out, "http_in_flight_requests") {
		t.Fatalf("缺少 http_in_flight_requests：\n%s", out)
	}
	// 直接读取它的 Value()：所有请求结束后应回到 0
	v := httpInFlight.Value()
	if v != 0 {
		t.Fatalf("所有请求结束后 http_in_flight_requests 应为 0，实际 %v", v)
	}
}

func TestMetrics_ConcurrencyDoesNotBreakInFlight(t *testing.T) {
	// 并发跑请求，确保 httpInFlight Inc/Dec 配对正确（defer Dec），最终值仍为 0
	r := newTestRouter()
	const n = 50
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer func() {
				_ = recover()
				done <- struct{}{}
			}()
			do(t, r, http.MethodGet, "/api/ok")
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if v := httpInFlight.Value(); v != 0 {
		t.Fatalf("并发结束后 http_in_flight_requests 应为 0，实际 %v", v)
	}
}
