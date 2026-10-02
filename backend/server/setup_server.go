package server

// 首次部署引导服务（2026-10-02 H-3 修复）。
//
// 背景：旧版首次部署时机器码写盘后 utils.Fatal 退出，反向代理 / 客户端只看到
// "服务挂了"，看不到 warn 里的机器码与签发指引。新版改为 SETUP 模式：监听端口
// 但不连数据库、不开放业务 API，仅暴露：
//
//   - GET /             静态提示页（HTML，含机器码 + LMP 签发步骤）
//   - GET /api/setup    JSON 端点（机器码 / 签发说明，便于脚本化）
//   - GET /healthz      健康探针（让 K8s liveness 不至于失败重启动荡）
//
// 实现要点：
//   - 不调 utils.Fatal：运维填入 license_key 重启即生效
//   - 不依赖数据库 / JWT / WS / Scheduler / LLM：业务子系统都依赖 DB
//   - 复用 cfg.Server.Host/Port：与正式服务同端口，避免运维改配置
//
// Shutdownable 实现：仅做 httpServer.Shutdown(ctx)，不依赖 WS / DB / Scheduler。

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"doc/config"
	"doc/utils"
)

// SetupServer 首次部署引导服务。
type SetupServer struct {
	cfg         *config.Config
	machineCode string
	httpServer  *http.Server
}

// NewSetupServer 构造 SETUP 模式 HTTP 服务。
func NewSetupServer(cfg *config.Config, machineCode string) *SetupServer {
	return &SetupServer{
		cfg:         cfg,
		machineCode: machineCode,
	}
}

// Start 阻塞监听。出错时返回 error，main.go 据此决定是否 Fatal。
func (s *SetupServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/setup", s.handleJSON)
	mux.HandleFunc("/healthz", s.handleHealthz)

	addr := fmt.Sprintf("%s:%s", s.cfg.Server.Host, s.cfg.Server.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
		// SETUP 模式无数据库依赖：单请求超时放宽至 30s 即可。
		ReadHeaderTimeout: 10 * time.Second,
	}

	utils.Info("[setup] listening on %s (machine-code bootstrap mode)", addr)

	// ListenAndServe 在正常关闭时返回 http.ErrServerClosed，调用方应忽略。
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("setup server listen: %w", err)
	}
	return nil
}

// Shutdown 实现 services.Shutdownable 接口。
func (s *SetupServer) Shutdown() {
	if s.httpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.httpServer.Shutdown(ctx)
}

// handleIndex 返回 HTML 引导页（浏览器打开根路径即可看到）。
func (s *SetupServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = setupPageTpl.Execute(w, struct {
		MachineCode string
		Issued      string
	}{
		MachineCode: s.machineCode,
		Issued:      time.Now().Format(time.RFC3339),
	})
}

// handleJSON 返回机器码 JSON（脚本化场景）。
func (s *SetupServer) handleJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(fmt.Sprintf(`{
  "machine_code": %q,
  "hint": "Take this machine code to LMP authorization server, get license_key, fill into config.json: license.license_key, then restart.",
  "next_step": "edit bin/config.test-main.json (dev) or bin/config.json (prod), set license.license_key, then restart this process"
}`, s.machineCode)))
}

// handleHealthz 健康探针：SETUP 模式也算健康（业务不可用 = readyz fail）。
func (s *SetupServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("setup-mode-ok"))
}

// setupPageTpl 引导页模板。简单一行说明，避免引入前端依赖。
var setupPageTpl = template.Must(template.New("setup").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>DocManageTrail — 首次部署引导</title>
<style>
body{font-family:system-ui,-apple-system,sans-serif;max-width:760px;margin:40px auto;padding:0 20px;color:#222;line-height:1.6}
h1{color:#c2410c}
.machine-code{background:#fef3c7;border:1px solid #f59e0b;padding:14px 18px;border-radius:6px;font-family:ui-monospace,Menlo,monospace;font-size:18px;font-weight:600;color:#78350f;letter-spacing:0.5px;user-select:all}
ol{padding-left:1.5em}
li{margin-bottom:8px}
.warn{background:#fee2e2;border-left:4px solid #dc2626;padding:12px 16px;border-radius:4px;margin:16px 0}
.ok{background:#d1fae5;border-left:4px solid #059669;padding:12px 16px;border-radius:4px;margin:16px 0}
code{background:#f3f4f6;padding:2px 6px;border-radius:3px;font-size:0.9em}
</style>
</head>
<body>
<h1>首次部署引导</h1>
<div class="warn">
本服务尚未配置 license，目前以 <strong>SETUP 模式</strong>运行：监听端口但不开放业务 API，
请按下方步骤完成首次签发后重启。
</div>
<h2>本机机器码</h2>
<div class="machine-code">{{.MachineCode}}</div>
<p style="color:#6b7280;font-size:13px">已生成时间：{{.Issued}}（UTC）</p>
<h2>签发步骤</h2>
<ol>
<li>复制上方机器码。</li>
<li>登录 LMP 授权服务端，把机器码 + 应用标识 <code>doc_crm_v1</code> 提交签发。</li>
<li>把返回的 <code>license_key</code> 填入 <code>bin/config.test-main.json</code>（dev）或 <code>bin/config.json</code>（prod）的
<code>license.license_key</code> 字段。</li>
<li>重启本服务（<code>./run.ps1</code> 或 <code>go run .</code>）。</li>
</ol>
<div class="ok">
重启后服务将以正常模式启动，<code>/api/license/features</code> 会返回 license 包含的 module key 集合。
</div>
<h3>脚本化查询</h3>
<p>无浏览器场景可用：<code>curl http://<host>:<port>/api/setup</code></p>
</body>
</html>`))
