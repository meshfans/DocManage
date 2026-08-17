package main

import (
	"strings"
	"time"

	"doc/config"
	"doc/database"
	"doc/handlers"
	"doc/middleware"
	"doc/server"
	"doc/services"
	"doc/utils"
)

// 版本信息（通过 -ldflags "-X main.Version=..." 注入）
//
//	示例：go build -ldflags "-X main.Version=1.0.0 -X main.GitCommit=abc1234 -X main.BuildTime=2026-06-26T15:04:05Z"
var (
	Version   = "dev"     // 语义版本号
	GitCommit = "unknown" // git short commit
	BuildTime = "unknown" // RFC3339 构建时间
)

func parseLogLevel(level string) utils.LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return utils.LogLevelDebug
	case "info":
		return utils.LogLevelInfo
	case "warn":
		return utils.LogLevelWarn
	case "error":
		return utils.LogLevelError
	case "fatal":
		return utils.LogLevelFatal
	default:
		return utils.LogLevelInfo
	}
}

func init() {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		utils.Info("Warning: Failed to load Asia/Shanghai timezone: %v, using local timezone", err)
	} else {
		time.Local = loc
		utils.Info("Timezone set to Asia/Shanghai (UTC+8)")
	}
}

func main() {
	cfg := config.LoadConfig()

	logLevel := parseLogLevel(cfg.Log.Level)
	if err := utils.InitLoggerWithLevel(cfg.Log.Dir, logLevel, cfg.Log.DaysToKeep); err != nil {
		utils.Fatal("日志初始化失败: %v", err)
	}
	utils.Info("日志系统初始化成功，日志目录: %s, 保留天数: %d, 日志级别: %s", cfg.Log.Dir, cfg.Log.DaysToKeep, cfg.Log.Level)

	if err := database.InitDatabaseWithOptions(database.DatabaseInitOptions{
		Path:              cfg.Database.Path,
		JournalMode:       cfg.Database.JournalMode,
		Synchronous:       cfg.Database.Synchronous,
		CacheSize:         cfg.Database.CacheSize,
		WalAutocheckpoint: cfg.Database.WalAutocheckpoint,
		BusyTimeout:       cfg.Database.BusyTimeout,
	}); err != nil {
		utils.Fatal("数据库初始化失败: %v", err)
	}
	defer database.CloseDatabase()
	utils.Info("数据库初始化成功")

	// P0 修复（2026-06-28）：启动速率限制后台 GC goroutine。
	// 定期清理过期 entry（锁定已过且 1h 内无失败）避免内存累积。
	middleware.InitRateLimitGC()

	// P0 修复（2026-06-28）：启动 JWT 黑名单后台 GC。
	// 清理已过 token 原始过期时间的黑名单项，避免 sync.Map 无限增长。
	utils.StartBlacklistGC()

	// 第三方合同 PDF 存储
	services.InitThirdPartyStorage(cfg.Upload.Dir)

	// 媒体库：照片 / 录像 / 音频文件存储（v2 单表 + 3 哈希）
	services.InitMediaStorage(cfg.Upload.Dir)

	// WebSocket 实时推送（铃铛通知 / 新消息实时推送）
	wsHandler := handlers.NewWebSocketHandler([]byte(cfg.JWT.Secret))
	go wsHandler.Run()
	handlers.SetWebSocketHandlerForMessage(wsHandler)

	if err := services.InitBackupService(&cfg.Backup); err != nil {
		utils.Warn("备份服务初始化失败: %v", err)
	}

	// 初始化统一调度器（必须在 database.InitDatabase 之后）
	if err := services.InitScheduler(cfg); err != nil {
		utils.Warn("调度器初始化失败: %v", err)
	} else {
		defer func() {
			if s := services.GetScheduler(); s != nil {
				s.Stop()
			}
		}()
	}

	jwtUtils, err := utils.NewJWTUtilsFromConfig(
		cfg.JWT.Secret,
		cfg.JWT.AccessExpire,
		cfg.JWT.RefreshExpire,
	)
	if err != nil {
		utils.Fatal("JWT配置初始化失败: %v", err)
	}

	combinedServer := server.NewCombinedServer(cfg, jwtUtils, wsHandler)

	handlers.SetGracefulShutdownFunc(func() {
		utils.Info("服务器关闭中...")
		// WS hub 关闭由 combinedServer.Shutdown() 统一触发（避免重复关停）。
		// 关停顺序：HTTP server 停接新请求 → WS hub 关闭所有连接 → DB 关闭。
		combinedServer.Shutdown()
		database.CloseDatabase()
	})

	go func() {
		if err := combinedServer.Start(); err != nil {
			utils.Fatal("服务器启动失败: %v", err)
		}
	}()

	select {}
}
