package main

import (
	"context"
	"strings"
	"time"

	"doc/config"
	"doc/database"
	"doc/handlers"
	"doc/middleware"
	"doc/server"
	"doc/services" // 触发 LLM 模块注册
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

	// 启动期许可校验 + debug 信封判定（2026-10-01 C1/C5 统一，与 DocCRM 对齐）。
	//
	// **完全硬切**：任一校验失败（空 key / 格式错 / 验签失败 / app_id 或
	// machine_code 不一致 / 已过期）一律 log.Fatal 拒绝启动。
	// DocManageTrail 此前无任何许可校验入口，属未授权运行状态，本次一并补齐。
	//
	// 位置约束：InitLogger 之后（7 步日志落 cfg.Log.Dir）、InitDatabase 之前。
	//
	// 2026-10-02 H-3 修复：首次部署（LicenseKey 为空）时 licBoot.SetupMode=true，
	// 进入 setup 启动分支——不连数据库、不开业务 API，仅暴露 /setup 端点
	// 返回机器码 + 签发指引。
	licBoot := bootstrapLicenseAndDebug(cfg)

	if licBoot.SetupMode {
		// ---------- SETUP 模式：监听端口 + 暴露机器码引导页 ----------
		//
		// 不连接数据库（避免无 license 时仍写 schema / seed 数据），
		// 不初始化 JWT / WS / 调度器 / LLM 等业务子系统（它们都依赖数据库）。
		// 仅最小化起一个 HTTP 服务供运维拿到机器码。
		utils.Warn("[main] SETUP 模式启动：跳过数据库 / JWT / 业务子系统初始化")
		setupServer := server.NewSetupServer(cfg, licBoot.MachineCode)
		// Start 内部阻塞 ListenAndServe；放后台跑，主流程交回 RunGraceful 等信号
		go func() {
			if err := setupServer.Start(); err != nil {
				utils.Fatal("SETUP 服务启动失败: %v", err)
			}
		}()
		// Shutdownable 只要求 Shutdown()，不依赖 HTTP/WS/scheduler 全链路埋点
		//（SETUP 模式无 WS、无 scheduler）。
		services.RunGraceful(context.Background(), setupServer)
		return
	}

	_ = licBoot // DBPassword 供需要加密 DB 的部署使用；本项目暂未启用

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
	// ⚠️ 2026-08-19 Round 18 修复：删除 defer scheduler.Stop()。
	// 此前 defer 在 main 退出时执行，与 services.RunGraceful 内部的 sched.Stop()
	// 重复关停。统一关停入口见文件末尾 RunGraceful。
	if err := services.InitScheduler(cfg); err != nil {
		utils.Warn("调度器初始化失败: %v", err)
	}

	// 初始化 LLM 服务（必须在 database.InitDatabase 之后）
	if err := services.InitLLMService(); err != nil {
		utils.Warn("LLM 服务初始化失败: %v", err)
	}

	jwtUtils, err := utils.NewJWTUtilsFromConfig(
		cfg.JWT.Secret,
		cfg.JWT.AccessExpire,
		cfg.JWT.RefreshExpire,
	)
	if err != nil {
		utils.Fatal("JWT配置初始化失败: %v", err)
	}

	combinedServer := server.NewCombinedServer(cfg, jwtUtils, wsHandler, licBoot.DebugModeActive)

	// ⚠️ 2026-08-19 Round 18 修复：删除 SetGracefulShutdownFunc 注册。
	// 此前该回调在 handler 失败时也会被触发（如 combinedServer.Start 失败），
	// 会与 RunGraceful 内部 combinedServer.Shutdown() + database.CloseDatabase()
	// 重复关停，导致 DB / WS 双重关闭 + recover 不到的二次报错。
	// 统一关停入口见 RunGraceful，它是进程唯一的 SIGTERM/SIGINT 出口。

	go func() {
		if err := combinedServer.Start(); err != nil {
			utils.Fatal("服务器启动失败: %v", err)
		}
	}()

	// 2026-08-19 Round 18：单一阻塞入口，由 SIGTERM/SIGINT 触发优雅关停。
	// 关停顺序：HTTP → WS → Scheduler → DB，全程埋点 shutdown_stage_duration_seconds。
	services.RunGraceful(context.Background(), combinedServer)
}
