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

	// 2026-07-06 round6 精简：删除 ValidateMachineCodeOnStartup 启动期机器码校验
	//   - 旧逻辑：硬件指纹 vs config.license.machine_code，不匹配 = Fatal
	//   - 新逻辑：无校验，任意机器都能跑（license 模块整体下线）

	// 2026-07-06 round6 精简：删除 License 验证 + 许可证信息日志输出
	//   - 旧逻辑：解 envelope → 校验 issued/exp → 设全局 license → 控制 RBAC 功能码
	//   - 新逻辑：无 license 概念，所有功能默认开启

	// 第四阶段（Phase 4.0）：使用配置驱动的 DB 初始化
	// 2026-07-06 round5：默认 journal_mode=DELETE（取消 WAL），Password 字段已移除
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

	// 2026-07-06 精简：InitSealStorage 已删除（印章全栈下线，seal_storage.go 一并删）

	// 第十阶段：第三方合同 PDF 存储
	services.InitThirdPartyStorage(cfg.Upload.Dir)

	// 媒体库：照片 / 录像 / 音频文件存储（v2 单表 + 3 哈希）
	services.InitMediaStorage(cfg.Upload.Dir)

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

	// 2026-07-06 round4 精简：WebSocket 模块已下线（铃铛通知改由前端轮询或下次登录后查看）
	//   - handlers/websocket.go 删除
	//   - /api/ws + /api/ws/status 路由下线
	//   - customer.go 签字时不再 SendToUser 推消息（消息仍写入 messages 表，前端下次进入可读）
	combinedServer := server.NewCombinedServer(cfg, jwtUtils)

	handlers.SetGracefulShutdownFunc(func() {
		utils.Info("服务器关闭中...")
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
