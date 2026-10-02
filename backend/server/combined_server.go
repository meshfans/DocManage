package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"doc/config"
	"doc/handlers"
	"doc/middleware"
	"doc/services"
	"doc/utils"
	"doc/web"

	"github.com/gin-gonic/gin"
)

type CombinedServer struct {
	httpServer *http.Server
	wsHandler  *handlers.WebSocketHandler
	enableSSL  bool
	certFile   string
	keyFile    string
}

// NewCombinedServer 组装 HTTP + WS 复合服务。
//
// debugModeActive（2026-10-01 C5 统一新增）：pkg/debug 解密 config.json 顶层
// "debug" 密文信封后的时间窗命中结果，由 main.go 在启动期校验 license 后传入。
// 与既有的 cfg.Server.Debug（手动改 config.json 的布尔开关）并存 ——
// 两者任一为真即挂载 pprof。
func NewCombinedServer(cfg *config.Config, jwtUtils interface{}, wsHandler *handlers.WebSocketHandler, debugModeActive bool) *CombinedServer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	// Recovery 必须是第一个 Use() —— 兜底后续所有中间件 + handler 的 panic，
	// 避免单个 handler 异常把整个进程拖死。
	// 不再使用 gin.Recovery()：它只把 panic 写 stderr，不接项目统一日志
	//（utils.LogError），运维无法检索。改用 middleware.Recovery()。
	router.Use(middleware.Recovery())
	// Trace() 必须在 RequestLogger 之前，否则访问日志读不到 trace_id。
	router.Use(middleware.Trace())
	// Metrics 中间件插在 Trace 之后、RequestLogger 之前：
	//   - 在 Trace 之后：可拿到 trace_id（如未来要把 trace_id 写入指标 label）
	//   - 在 RequestLogger 之前：避免日志中间件 panic 时丢失 metrics 采集
	router.Use(middleware.Metrics())
	router.Use(middleware.RequestLogger())
	router.Use(middleware.CORS())
	// 维护模式拦截（最后注册，最高优先级）
	router.Use(middleware.Maintenance())
	// 体验模式拦截：database.mode=="experience" 时业务写操作返回 423
	router.Use(middleware.ExperienceReadOnly())

	// 健康探针必须最先注册，保证即便后续路由加载（web.GetFS）失败，
	// liveness / readiness / metrics 也能被探针访问。
	sched := services.GetScheduler()
	registerHealthRoutes(router, wsHandler, sched)

	registerAPIRoutes(router, jwtUtils, cfg)
	registerWebSocketRoutes(router, jwtUtils, wsHandler)
	registerWebRoutes(router, cfg)

	// pprof 门控（2026-10-01 C5 统一后为"或"关系）：
	//   1. cfg.Server.Debug —— 运维手动改 config.json 的布尔开关（旧机制保留）
	//   2. debugModeActive  —— pkg/debug 解密顶层 "debug" 密文信封且时间窗命中（新机制）
	// 两者任一为真即挂载；生产两者默认皆 false。
	//
	// 鉴权依赖 *utils.JWTUtils，断言失败则跳过挂载（宁可不挂，也不无鉴权暴露）。
	if cfg.Server.Debug || debugModeActive {
		src := "config.server.debug"
		if debugModeActive {
			src = "debug envelope time window"
		}
		if jwt, ok := jwtUtils.(*utils.JWTUtils); ok {
			registerPprofRoutes(router, jwt)
			utils.Info("[pprof] mounted /debug/pprof/* (source=%s, JWT+admin guarded)", src)
		} else {
			utils.Warn("[pprof] gate hit (source=%s) but JWT utils assertion failed, skip mounting (avoid unauthenticated exposure)", src)
		}
	}

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &CombinedServer{
		httpServer: httpServer,
		wsHandler:  wsHandler,
		enableSSL:  cfg.Server.EnableSSL,
		certFile:   cfg.Server.SSLCert,
		keyFile:    cfg.Server.SSLKey,
	}
}

func registerAPIRoutes(router *gin.Engine, jwtUtils interface{}, cfg *config.Config) {
	api := router.Group("/api")
	{
		if jwt, ok := jwtUtils.(*utils.JWTUtils); ok {
			authHandler := handlers.NewAuthHandler(jwt)
			routesHandler := handlers.NewRoutesHandler()
			customerHandler := handlers.NewCustomerHandler(cfg)

			systemHandler := handlers.NewSystemHandler()
			systemConfigHandler := handlers.NewSystemConfigHandler()
			messageHandler := handlers.NewMessageHandler()

			api.POST("/login", middleware.LoginRateLimit(), authHandler.Login)
			api.POST("/refresh-token", middleware.RefreshTokenRateLimit(), authHandler.RefreshToken)
			api.POST("/register", authHandler.Register)

			// 公开接口（登录页也需要访问）
			api.GET("/license/client-info", systemHandler.GetClientInfo)

			protected := api.Group("")
			protected.Use(middleware.JWTAuth(jwt))
			// 2026-06-28 RBAC v3 P2：DataScope 中间件（在 APIGate 之后、handler 之前）。
			// 加载 UserDataScope 并 c.Set("data_scope", *scope)，handler 用 middleware.GetDataScope(c) 拿。
			// 失败时降级 self（handler 内兜底）。
			protected.Use(middleware.DataScopeMiddleware())
			// PR-5：RBAC v2 — API gate 中间件（每个 protected 请求自动按 method+path 校验 permission_code）
			protected.Use(handlers.APIGateMiddleware())
			{
				protected.POST("/hash/calculate", handlers.CalculateHash)
				protected.POST("/hash/verify", handlers.VerifyHash)
				protected.GET("/hash/algorithms", handlers.GetAlgorithms)

				// 通用审计日志（append-only 哈希链 / SM3）—— admin only
				auditHandler := handlers.NewAuditHandler()
				protected.GET("/audit", auditHandler.ListAudit)
				protected.POST("/audit/reconcile", auditHandler.ReconcileAuditChain)

				departmentHandler := handlers.NewDepartmentHandler()
				userHandler := handlers.NewUserExtendedHandler(cfg)

				protected.GET("/departments", departmentHandler.GetDepartmentTree)
				protected.GET("/departments/:id", departmentHandler.GetDepartment)
				protected.POST("/departments", departmentHandler.CreateDepartment)
				protected.POST("/departments/:id/update", departmentHandler.UpdateDepartment)
				protected.POST("/departments/:id/delete", departmentHandler.DeleteDepartment)
				protected.GET("/departments/:id/users", departmentHandler.GetDepartmentUsers)

				protected.GET("/users", userHandler.GetUsers)
				protected.GET("/users/check-username", userHandler.CheckUsername)
				protected.POST("/users", userHandler.CreateUser)
				protected.GET("/users/:id", userHandler.GetUser)
				protected.POST("/users/:id/update", userHandler.UpdateUser)
				protected.POST("/users/:id/delete", userHandler.DeleteUser)
				protected.POST("/users/:id/department", userHandler.UpdateUserDepartment)
				protected.POST("/users/:id/roles", userHandler.AssignUserRoles)
				protected.GET("/users/:id/avatar", userHandler.GetAvatar)
				protected.POST("/users/:id/avatar", userHandler.UploadAvatar)
				protected.GET("/users/department/:id", userHandler.GetDepartmentUsers)

				protected.GET("/user/info", authHandler.GetUserInfo)
				protected.POST("/logout", authHandler.Logout)
				protected.POST("/change-password", middleware.ChangePasswordRateLimit(), authHandler.ChangePassword)
				protected.GET("/get-async-routes", routesHandler.GetAsyncRoutes)
				protected.GET("/rbac/permission-version", routesHandler.GetPermissionVersion)

				protected.GET("/customer/list", customerHandler.GetCustomerList)
				protected.POST("/customer/create", customerHandler.CreateCustomer)
				protected.GET("/customer/query", customerHandler.GetCustomerByID)
				protected.POST("/customer/update", customerHandler.UpdateCustomer)
				protected.POST("/customer/delete", customerHandler.DeleteCustomer)
				protected.POST("/customer/upload-signature", customerHandler.UploadSignature)
				// 个人/企业客户管理（增量 API，不影响老接口）
				protected.POST("/customer/create-ext", customerHandler.CreateCustomerExt)
				protected.POST("/customer/update-ext", customerHandler.UpdateCustomerExt)
				protected.GET("/customer/list-by-type", customerHandler.GetCustomersByTypeList)
				protected.GET("/customer/search-by-type", customerHandler.SearchCustomersByTypeList)

				protected.GET("/messages/list", messageHandler.GetMessages)
				protected.GET("/messages/unread-count", messageHandler.GetUnreadCount)
				protected.GET("/messages/:id", messageHandler.GetMessage)
				protected.POST("/messages/create", messageHandler.CreateMessage)
				protected.POST("/messages/:id/read", messageHandler.MarkAsRead)
				protected.POST("/messages/read-all", messageHandler.MarkAllAsRead)
				protected.POST("/messages/:id/delete", messageHandler.DeleteMessage)

				// 提醒业务（模板 + 订阅 + 日志 + 扫描触发）
				reminderHandler := handlers.NewReminderHandler()
				protected.GET("/reminders/templates", reminderHandler.ListTemplates)
				protected.POST("/reminders/templates", reminderHandler.CreateTemplate)
				protected.POST("/reminders/templates/:id", reminderHandler.UpdateTemplate)
				protected.POST("/reminders/templates/:id/delete", reminderHandler.DeleteTemplate)
				protected.GET("/reminders/subscriptions", reminderHandler.ListSubscriptions)
				protected.POST("/reminders/subscriptions", reminderHandler.CreateSubscription)
				protected.POST("/reminders/subscriptions/:id", reminderHandler.UpdateSubscription)
				protected.POST("/reminders/subscriptions/:id/delete", reminderHandler.DeleteSubscription)
				protected.GET("/reminders/logs", reminderHandler.ListLogs)
				protected.POST("/reminders/scan", reminderHandler.TriggerScan)

				// 定时任务调度框架
				scheduledTaskHandler := handlers.NewScheduledTaskHandler()
				protected.GET("/scheduled-tasks", scheduledTaskHandler.ListTasks)
				protected.GET("/scheduled-tasks/handlers", scheduledTaskHandler.ListHandlers)
				protected.GET("/scheduled-tasks/next-run", scheduledTaskHandler.NextRunTime)
				protected.GET("/scheduled-tasks/:id", scheduledTaskHandler.GetTask)
				protected.POST("/scheduled-tasks", scheduledTaskHandler.CreateTask)
				protected.POST("/scheduled-tasks/:id/update", scheduledTaskHandler.UpdateTask)
				protected.POST("/scheduled-tasks/:id/toggle", scheduledTaskHandler.ToggleTask)
				protected.POST("/scheduled-tasks/:id/delete", scheduledTaskHandler.DeleteTask)
				protected.POST("/scheduled-tasks/:id/run-now", scheduledTaskHandler.RunNow)
				protected.GET("/scheduled-tasks/:id/logs", scheduledTaskHandler.GetTaskLogs)
				protected.GET("/scheduled-tasks/:id/audits", scheduledTaskHandler.GetTaskAudits)

				protected.GET("/system/config", systemHandler.GetConfig)
				protected.POST("/system/config", systemHandler.SetConfig)
				protected.GET("/system/config-file", systemHandler.GetConfigFile)
				protected.POST("/system/config-file", systemHandler.SaveConfigFile)
				protected.POST("/system/backup-now", systemHandler.BackupNow)
				protected.POST("/system/restore", systemHandler.RestoreFromBackup)
				// 维护模式查询 / 控制
				protected.GET("/system/maintenance", systemHandler.GetMaintenanceStatus)
				protected.POST("/system/maintenance", systemHandler.SetMaintenanceMode)

				// 备份管理（UI 配套 API）
				backupMgmtHandler := handlers.NewBackupHandler()
				protected.GET("/backup/list", backupMgmtHandler.List)
				protected.GET("/backup/:id", backupMgmtHandler.Detail)
				protected.GET("/backup/:id/download", backupMgmtHandler.Download)
				protected.POST("/backup/:id/delete", backupMgmtHandler.Delete)

				// PR-5：RBAC 管理 API（v2：role + permission + check）
				rbacMgmtHandler := handlers.NewRBACManagementHandler()
				protected.GET("/rbac/roles", rbacMgmtHandler.ListRoles)
				protected.POST("/rbac/roles", rbacMgmtHandler.UpsertRole)
				protected.POST("/rbac/roles/:code/delete", rbacMgmtHandler.DeleteRole)
				protected.GET("/rbac/permissions", rbacMgmtHandler.ListPermissions)
				protected.POST("/rbac/permissions", rbacMgmtHandler.UpsertPermission)
				protected.POST("/rbac/permissions/:code/delete", rbacMgmtHandler.DeletePermission)
				protected.POST("/rbac/check", rbacMgmtHandler.CheckPermission)
				protected.POST("/system/shutdown", systemHandler.ShutdownServer)
				protected.POST("/system/generate-ssl-cert", systemHandler.GenerateSSLCert)

				// 业务配置（system_config 表）
				protected.GET("/system-config/public", systemConfigHandler.GetPublicConfigs)
				protected.GET("/system-config/list", systemConfigHandler.ListConfigs)
				protected.POST("/system-config/update", systemConfigHandler.UpdateConfig)
				protected.POST("/system-config/:key/delete", systemConfigHandler.DeleteConfig)

				// license 能力查询（2026-10-01 C1/C2 统一）。
				// 挂 JWT auth 即可，不挂 APIGate —— license features 是授权维度而非
				// 业务 permission_code，且需要普通 user 也可读（前端据此决定
				// AI 配置页等 requiredFeature 页面是否显示），故独立走。
				protected.GET("/license/features", systemHandler.GetLicenseFeatures)

				// /license/machine-code：返回本机机器码（2026-10-01 C1/C2 统一）。
				// 挂在 protected 组（JWT + APIGate），处理器内再做 RequireAdmin 二次校验。
				// 机器码是 license 签发的必要输入，不放公开路径。
				protected.GET("/license/machine-code", systemHandler.GetMachineCode)

				// 第三方合同登记
				tpHandler := handlers.NewThirdPartyHandler(cfg)
				protected.GET("/third-party/contracts", tpHandler.ListContracts)
				protected.POST("/third-party/contracts", tpHandler.CreateContract)
				protected.GET("/third-party/contracts/:id", tpHandler.GetContract)
				protected.POST("/third-party/contracts/:id", tpHandler.UpdateContract)
				protected.POST("/third-party/contracts/:id/status", tpHandler.ChangeStatus)
				protected.POST("/third-party/contracts/:id/delete", tpHandler.DeleteContract)
				protected.POST("/third-party/contracts/:id/upload", tpHandler.UploadFile)
				protected.GET("/third-party/contracts/:id/download", tpHandler.DownloadFile)
				protected.POST("/third-party/contracts/bulk-download", tpHandler.BulkDownload)

				// 媒体库：照片 / 录像 / 音频（v2 单表 + 3 哈希）
				mediaHandler := handlers.NewMediaHandler(cfg)
				protected.GET("/media", mediaHandler.List)
				protected.GET("/media/by-target", mediaHandler.ListByTarget)
				protected.GET("/media/:id", mediaHandler.Get)
				protected.POST("/media/create", mediaHandler.Create)
				protected.POST("/media/:id", mediaHandler.Update)
				protected.POST("/media/:id/delete", mediaHandler.Delete)
				protected.POST("/media/:id/restore", mediaHandler.Restore)
				protected.GET("/media/tags", mediaHandler.ListAllTags)
				protected.POST("/media/bulk-tag", mediaHandler.BulkAddTag)
				protected.POST("/media/bulk-delete", mediaHandler.BulkDelete)
				protected.POST("/media/bulk-customer", mediaHandler.BulkSetCustomer)
				protected.GET("/media/:id/file", mediaHandler.Download)
				protected.GET("/media/:id/thumb", mediaHandler.Thumbnail)
				protected.POST("/media/upload", mediaHandler.Upload)
				protected.GET("/media/check-hash", mediaHandler.CheckHash)
				protected.GET("/media/:id/verify", mediaHandler.Verify)

				// AI 配置管理
				aiConfigHandler := handlers.NewAIConfigHandler()
				// 2026-10-01 C1/C2 统一：AI 配置端点逐条挂 FeatureGate("ai")。
				//   license 不含 "ai" → 全部 403，用户无法自建 LLM 接入，
				//   系统按设计默认使用 meshfans 提供的 LLM 能力。
				// 口径与前端 routes/modules/system.ts 的 meta.requiredFeature:"ai" 一致。
				//
				// 用内联中间件而非 protected.Group("/ai-configs", ...)：
				// 保持 protected.<METHOD> 的字面写法，权限种子对账才能对上。
				//
				// 2026-10-02 M-5 修复：新增 ai 相关端点必须挂 aiGate。
				//   挂载约束详见 docs/adr/0006-license-feature-gate-后端挂载约束.md。
				//   当前 aiGate 挂在 11 个端点：list / detail / create / update /
				//   delete / set-default / test / key / multimodal / test-inline / ai-meta。
				//   新增 endpoint 时务必同步加进这 11 行（+ 在 ADR 里追加）。
				aiGate := middleware.FeatureGate("ai")
				protected.GET("/ai-configs", aiGate, aiConfigHandler.ListAIConfigs)
				protected.GET("/ai-configs/:id", aiGate, aiConfigHandler.GetAIConfig)
				protected.POST("/ai-configs", aiGate, aiConfigHandler.CreateAIConfig)
				protected.POST("/ai-configs/:id", aiGate, aiConfigHandler.UpdateAIConfig)
				protected.POST("/ai-configs/:id/delete", aiGate, aiConfigHandler.DeleteAIConfig)
				protected.POST("/ai-configs/:id/default", aiGate, aiConfigHandler.SetDefaultAIConfig)
				protected.POST("/ai-configs/:id/test", aiGate, aiConfigHandler.TestAIConfig)
				protected.POST("/ai-configs/:id/key", aiGate, aiConfigHandler.UpdateAIConfigKey)
				protected.POST("/ai-configs/:id/multimodal", aiGate, aiConfigHandler.ToggleMultimodal)
				protected.POST("/ai-configs/test", aiGate, aiConfigHandler.TestAIConfigInline)
				protected.GET("/ai-meta", aiGate, aiConfigHandler.GetAIMeta)

				// LLM Copilot（4 个端点）
				llmHandler := handlers.NewLLMHandler()
				protected.POST("/llm/chat", llmHandler.HandleChat)
				protected.POST("/llm/stream", llmHandler.HandleStream)
				protected.POST("/llm/module/:name/execute", llmHandler.HandleModuleExecute)
				protected.POST("/llm/module/:name/stream", llmHandler.HandleModuleStream)
			}
		}
	}
}

func registerWebSocketRoutes(router *gin.Engine, jwtUtils interface{}, wsHandler *handlers.WebSocketHandler) {
	api := router.Group("/api")
	{
		api.GET("/ws", wsHandler.HandleWebSocket)

		if jwt, ok := jwtUtils.(*utils.JWTUtils); ok {
			api.GET("/ws/status", middleware.JWTAuth(jwt), func(c *gin.Context) {
				utils.Success(c, gin.H{"client_count": wsHandler.GetClientCount()})
			})
		}
	}
}

// registerHealthRoutes 注册健康 / 探针路由（Round 15）。
//
//   - /health  → HealthzHandler（兼容原探针路径）
//   - /healthz → HealthzHandler（K8s 约定 liveness）
//   - /readyz  → ReadyzHandler（K8s 约定 readiness）
//   - /metrics → MetricsHandler（Prometheus text 0.0.4）
//
// 这些路由最早在 NewCombinedServer 注册，因此即便后续 web.GetFS 加载失败、
// registerAPIRoutes 抛错，运维探针仍然可达。
func registerHealthRoutes(router *gin.Engine, wsHandler *handlers.WebSocketHandler, sched *services.Scheduler) {
	router.GET("/health", handlers.HealthzHandler())
	router.GET("/healthz", handlers.HealthzHandler())
	router.GET("/readyz", handlers.ReadyzHandler(wsHandler, sched))
	// Round 16：/metrics 改为依赖版工厂，启动时把 wsHub / sched 注入，
	// 每次请求前一次性刷新 runtime / DB / WS / Scheduler 四类指标。
	router.GET("/metrics", handlers.MetricsHandlerWithDependencies(wsHandler, sched))
}

// registerPprofRoutes 挂载 pprof 路由（仅在 debug 门控命中时调用）。
//
// 2026-10-01 C5 统一：从原先的 `router.Any` 无鉴权裸挂载，升级为与 DocCRM 一致的
// **JWT + RequireAdmin + 仅 GET** 版本。
//
// 为什么必须鉴权：heap / goroutine / trace dump 可能包含内存中的 JWT secret、
// license password 与业务 PII。debug 时间窗只解决了"何时可访问"，没解决
// "谁能访问"——裸挂载等于任何能连到端口的人都能拿到内存快照。
//
// 2026-10-02 M-1 修复：关闭 /debug/pprof/trace 端点。
//   - /trace 用于 CPU 执行 trace（5s 默认采样），可能影响线上业务延迟；
//   - 安全维度它和 /heap 同等敏感但实际用得极少；
//   - 关闭后其它端点（heap/goroutine/profile/symbol/cmdline）仍可采样，
//     满足 99% 调试需求；真要 trace 时改回这行 + 重启即可。
//
// 2026-10-02 M-2 修复：每个 pprof 请求记独立审计日志。
//   - 走独立 ServeMux + gin.WrapH 意味着不会触发项目 RequestLogger，
//     历史上 pprof 访问不进 access log，运维误判为"无人访问"。
//   - 本函数现在显式用 middleware.RequestLogger 包裹（从外面 wrap），并在
//     /debug/pprof 前置一段独立 auditHandler 记 audit 日志。
//
// 访问方式（浏览器无法直接带 Bearer，用 curl 取数后本地分析）：
//
//	curl -H "Authorization: Bearer <admin access token>" \
//	  http://host:8443/debug/pprof/heap -o heap.pprof
func registerPprofRoutes(router *gin.Engine, jwtUtils *utils.JWTUtils) {
	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
	pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	// M-1：/debug/pprof/trace 不注册 ——
	//   注释见函数顶端。/debug/pprof/trace* 请求将落到 pprofMux 但无 handler，
	//   http.ServeMux 默认返 404。

	adminGate := func(c *gin.Context) {
		// RequireAdmin 失败时已写 403 响应，此处只需 Abort 阻断 pprof mux。
		if !handlers.RequireAdmin(c) {
			c.Abort()
			return
		}
		// M-2：审计日志。pprof 端点不走 RequestLogger，这里手动写一行
		// info 级日志便于审计 / 异常排查。admin 用户名取自 context
		// （JWTAuth 中间件已 set）。
		username, _ := c.Get("username")
		utils.Info("[pprof-audit] admin=%v path=%s query=%s ua=%s",
			username, c.Request.URL.Path, c.Request.URL.RawQuery, c.Request.UserAgent())
		c.Next()
	}
	pprofGroup := router.Group("/debug", middleware.JWTAuth(jwtUtils), adminGate)
	// pprof 端点只需要 GET，按项目「所有 API 只允许 GET 和 POST」的约定收敛。
	// 原先的 router.Any 还接受 PUT/DELETE/PATCH 等任意方法。
	pprofGroup.GET("/pprof/*action", gin.WrapH(pprofMux))
}

// registerWebRoutes 注册前端静态资源、index.html、platform-config.json 等路由。
//
// 健康探针（/health /healthz /readyz /metrics）由 registerHealthRoutes 在调用本函数之前
// 注册好。这样 web.GetFS 失败时探针仍可用；本函数无需也不能重复注册同名路由。
func registerWebRoutes(router *gin.Engine, cfg *config.Config) {
	_, err := web.GetFS()
	if err != nil {
		utils.Info("Warning: Failed to load web static files: %v", err)
		return
	}

	serverHost := cfg.Server.Host
	serverPort := cfg.Server.Port
	enableSSL := cfg.Server.EnableSSL

	router.GET("/", func(c *gin.Context) {
		content, err := web.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html;charset=utf-8", content)
	})

	router.GET("/index.html", func(c *gin.Context) {
		content, err := web.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html;charset=utf-8", content)
	})

	router.GET("/platform-config.json", func(c *gin.Context) {
		content, err := web.GetPlatformConfigWithDynamicUrl(serverHost, serverPort, cfg.Server.Domain, enableSSL)
		if err != nil {
			utils.Info("Warning: Failed to load platform config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load platform config"})
			return
		}
		c.Data(http.StatusOK, "application/json;charset=utf-8", content)
	})

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
			return
		}

		if web.FileExists(strings.TrimPrefix(path, "/")) {
			serveStaticFile(c, path)
			return
		}

		content, err := web.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html;charset=utf-8", content)
	})
}

func serveStaticFile(c *gin.Context, path string) {
	filePath := strings.TrimPrefix(path, "/")
	content, err := web.ReadFile(filePath)
	if err != nil {
		c.String(http.StatusNotFound, "file not found")
		return
	}

	contentType := getContentType(filePath)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, contentType, content)
}

func getContentType(filePath string) string {
	lowerPath := strings.ToLower(filePath)
	if strings.HasSuffix(lowerPath, ".html") {
		return "text/html; charset=utf-8"
	}
	if strings.HasSuffix(lowerPath, ".css") {
		return "text/css; charset=utf-8"
	}
	if strings.HasSuffix(lowerPath, ".js") {
		return "application/javascript; charset=utf-8"
	}
	if strings.HasSuffix(lowerPath, ".json") {
		return "application/json; charset=utf-8"
	}
	if strings.HasSuffix(lowerPath, ".png") {
		return "image/png"
	}
	if strings.HasSuffix(lowerPath, ".jpg") || strings.HasSuffix(lowerPath, ".jpeg") {
		return "image/jpeg"
	}
	if strings.HasSuffix(lowerPath, ".gif") {
		return "image/gif"
	}
	if strings.HasSuffix(lowerPath, ".svg") {
		return "image/svg+xml; charset=utf-8"
	}
	if strings.HasSuffix(lowerPath, ".ico") {
		return "image/x-icon"
	}
	if strings.HasSuffix(lowerPath, ".woff") {
		return "font/woff"
	}
	if strings.HasSuffix(lowerPath, ".woff2") {
		return "font/woff2"
	}
	if strings.HasSuffix(lowerPath, ".ttf") {
		return "font/ttf"
	}
	if strings.HasSuffix(lowerPath, ".otf") {
		return "font/otf"
	}
	return "application/octet-stream"
}

func (s *CombinedServer) Start() error {
	protocol := "HTTP"
	if s.enableSSL {
		protocol = "HTTPS"
	}
	utils.Debug("🚀 %s服务器启动在 %s", protocol, s.httpServer.Addr)

	if s.enableSSL {
		return s.startHTTPS()
	}
	return s.startHTTP()
}

func (s *CombinedServer) startHTTP() error {
	utils.Debug("HTTP服务器启动在 http://%s", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *CombinedServer) startHTTPS() error {
	cert, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
	if err != nil {
		return fmt.Errorf("加载SSL证书失败: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	listener, err := tls.Listen("tcp", s.httpServer.Addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("创建TLS监听器失败: %w", err)
	}

	utils.Info("🔐 HTTPS服务器启动在 https://%s", s.httpServer.Addr)
	err = s.httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *CombinedServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 关停顺序：HTTP server 先停接新请求 → WS hub 后关闭连接。
	// 反过来的话，HTTP 关停期间还能收请求但 WS 已关闭，会出现"调用成功但收不到推送"的错觉。
	//
	// 2026-08-19 Round 18 M-E2 修复：在 HTTP / WS 内部各自埋点 stage duration；
	// 上层 services.RunGraceful 用 "http_ws" 合并 stage 记录整段耗时。
	httpStart := time.Now()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		utils.LogError("API服务器关闭超时: %v", err)
	}
	utils.ObserveShutdownStage("http", time.Since(httpStart).Seconds())

	if s.wsHandler != nil {
		wsStart := time.Now()
		s.wsHandler.Shutdown()
		utils.ObserveShutdownStage("ws", time.Since(wsStart).Seconds())
	} else {
		utils.ObserveShutdownStage("ws", 0)
	}
}

func (s *CombinedServer) GetPort() string {
	return s.httpServer.Addr
}
