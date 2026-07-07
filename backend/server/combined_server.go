package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"doc/config"
	"doc/handlers"
	"doc/middleware"
	"doc/utils"
	"doc/web"

	"github.com/gin-gonic/gin"
)

type CombinedServer struct {
	httpServer *http.Server
	enableSSL  bool
	certFile   string
	keyFile    string
}

func NewCombinedServer(cfg *config.Config, jwtUtils interface{}) *CombinedServer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestLogger())
	router.Use(middleware.CORS())
	// 维护模式拦截（最后注册，最高优先级）
	router.Use(middleware.Maintenance())

	registerAPIRoutes(router, jwtUtils, cfg)
	registerWebRoutes(router, cfg)

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

				departmentHandler := handlers.NewDepartmentHandler()
				userHandler := handlers.NewUserExtendedHandler()

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
				protected.DELETE("/backup/:id", backupMgmtHandler.Delete)

				// PR-5：RBAC 管理 API（v2：role + permission + check）
				rbacMgmtHandler := handlers.NewRBACManagementHandler()
				protected.GET("/rbac/roles", rbacMgmtHandler.ListRoles)
				protected.POST("/rbac/roles", rbacMgmtHandler.UpsertRole)
				protected.DELETE("/rbac/roles/:code", rbacMgmtHandler.DeleteRole)
				protected.GET("/rbac/permissions", rbacMgmtHandler.ListPermissions)
				protected.POST("/rbac/permissions", rbacMgmtHandler.UpsertPermission)
				protected.DELETE("/rbac/permissions/:code", rbacMgmtHandler.DeletePermission)
				protected.POST("/rbac/check", rbacMgmtHandler.CheckPermission)
				protected.POST("/system/shutdown", systemHandler.ShutdownServer)
				protected.POST("/system/generate-ssl-cert", systemHandler.GenerateSSLCert)

				// 业务配置（system_config 表）
				protected.GET("/system-config/public", systemConfigHandler.GetPublicConfigs)
				protected.GET("/system-config/list", systemConfigHandler.ListConfigs)
				protected.POST("/system-config/update", systemConfigHandler.UpdateConfig)
				protected.POST("/system-config/:key/delete", systemConfigHandler.DeleteConfig)

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
			}
		}
	}
}

func registerWebRoutes(router *gin.Engine, cfg *config.Config) {
	_, err := web.GetFS()
	if err != nil {
		utils.Info("Warning: Failed to load web static files: %v", err)
		return
	}

	serverHost := cfg.Server.Host
	serverPort := cfg.Server.Port
	enableSSL := cfg.Server.EnableSSL

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "doc-server",
			"version": "1.0.0",
		})
	})

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

	if err := s.httpServer.Shutdown(ctx); err != nil {
		utils.LogError("API服务器关闭超时: %v", err)
	}
}

func (s *CombinedServer) GetPort() string {
	return s.httpServer.Addr
}
