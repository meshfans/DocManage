package handlers

import (
	"encoding/json"
	"os"
	"time"

	"doc/config"
	"doc/database"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type SystemHandler struct{}

func NewSystemHandler() *SystemHandler {
	return &SystemHandler{}
}

type SystemConfig struct {
	SystemName    string `json:"systemName"`
	CompanyName   string `json:"companyName"`
	ContactPerson string `json:"contactPerson"`
	ContactPhone  string `json:"contactPhone"`
	ContactEmail  string `json:"contactEmail"`
	Address       string `json:"address"`
	Website       string `json:"website"`
	Copyright     string `json:"copyright"`
	IcpNumber     string `json:"icpNumber"`
	Version       string `json:"version"`
}

func (h *SystemHandler) GetConfig(c *gin.Context) {
	config, err := database.GetAllSystemConfig()
	if err != nil {
		utils.Err(c, utils.CodeInternal, "获取配置失败")
		return
	}

	result := SystemConfig{
		SystemName:    getConfigValue(config, "systemName", "文档管理系统"),
		CompanyName:   getConfigValue(config, "companyName", ""),
		ContactPerson: getConfigValue(config, "contactPerson", ""),
		ContactPhone:  getConfigValue(config, "contactPhone", ""),
		ContactEmail:  getConfigValue(config, "contactEmail", ""),
		Address:       getConfigValue(config, "address", ""),
		Website:       getConfigValue(config, "website", ""),
		Copyright:     getConfigValue(config, "copyright", ""),
		IcpNumber:     getConfigValue(config, "icpNumber", ""),
		Version:       getConfigValue(config, "version", "1.0.0"),
	}

	utils.Success(c, result)
}

func (h *SystemHandler) SetConfig(c *gin.Context) {
	// Phase 2a (Critical #2)：系统配置是 admin 级权限，普通用户改自己昵称
	// 走 /api/users/me 而不是 SetConfig。
	if !RequireAdmin(c) {
		return
	}

	var req SystemConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	configs := map[string]string{
		"systemName":    req.SystemName,
		"companyName":   req.CompanyName,
		"contactPerson": req.ContactPerson,
		"contactPhone":  req.ContactPhone,
		"contactEmail":  req.ContactEmail,
		"address":       req.Address,
		"website":       req.Website,
		"copyright":     req.Copyright,
		"icpNumber":     req.IcpNumber,
		"version":       req.Version,
	}

	err := database.SetMultipleSystemConfig(configs)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "保存配置失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"message": "保存成功",
	})
}

func getConfigValue(config map[string]string, key, defaultValue string) string {
	if value, ok := config[key]; ok && value != "" {
		return value
	}
	return defaultValue
}

type ServerConfig struct {
	Host      string `json:"host"`
	Port      string `json:"port"`
	Domain    string `json:"domain"`
	EnableSSL bool   `json:"enable_ssl"`
	SSLCert   string `json:"ssl_cert"`
	SSLKey    string `json:"ssl_key"`
}

type JWTConfig struct {
	Secret        string `json:"secret"`
	AccessExpire  string `json:"access_expire"`
	RefreshExpire string `json:"refresh_expire"`
}

type DatabaseConfig struct {
	Path              string `json:"path"`
	Mode              string `json:"mode"`
	JournalMode       string `json:"journal_mode"`
	Synchronous       string `json:"synchronous"`
	CacheSize         int    `json:"cache_size"`
	WalAutocheckpoint int    `json:"wal_autocheckpoint"`
	BusyTimeout       int    `json:"busy_timeout"`
}

type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

type UploadConfig struct {
	Dir     string `json:"dir"`
	MaxSize int64  `json:"max_size"`
}

type LogConfig struct {
	Dir        string `json:"dir"`
	Level      string `json:"level"`
	DaysToKeep int    `json:"days_to_keep"`
	Enabled    bool   `json:"enabled"`
}

type BackupConfig struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
	// 调度已统一到 scheduled_task 表（Phase 4.1+），Interval 字段已删除。
	DaysToKeep      int  `json:"days_to_keep"`
	DatabaseEnabled bool `json:"database_enabled"`
	UploadEnabled   bool `json:"upload_enabled"`
}

// ConfigFile 系统配置（已下线 license / pdf 字段）
type ConfigFile struct {
	Description string         `json:"description"`
	Server      ServerConfig   `json:"server"`
	JWT         JWTConfig      `json:"jwt"`
	Database    DatabaseConfig `json:"database"`
	CORS        CORSConfig     `json:"cors"`
	Upload      UploadConfig   `json:"upload"`
	Log         LogConfig      `json:"log"`
	Backup      BackupConfig   `json:"backup"`
}

func (h *SystemHandler) GetConfigFile(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequireAdmin 兜底，避免 APIGateMiddleware 路径配置错误导致越权。
	// APIGate 也会校验，这里是双保险。
	if !RequireAdmin(c) {
		return
	}
	// 2026-08-20：不再从 config.GlobalConfig 读（进程内值会被环境变量覆盖，
	// 如 JWT_SECRET / SERVER_HOST / DB_PATH / UPLOAD_DIR）。
	// 「配置文件」tab 的语义是"展示和编辑磁盘上的 JSON 文件内容"，
	// 应直接读磁盘，env 覆盖值通过单独的"运行时配置"视图展示（如有）。
	// 否则前端会看到 env 兜底值（如 dev_only_local_secret_at_least_32_chars）
	// 而不是 config.json 实际写的 jwt.secret，编辑保存后文件内容也对不上。
	configFile := config.GetConfigFilePath()
	var cfg config.Config
	if data, err := os.ReadFile(configFile); err == nil {
		if jerr := json.Unmarshal(data, &cfg); jerr != nil {
			utils.Err(c, utils.CodeInternal, "解析配置文件失败: "+jerr.Error())
			return
		}
	} else {
		utils.Err(c, utils.CodeInternal, "读取配置文件失败: "+err.Error())
		return
	}

	result := ConfigFile{
		Description: "测试环境配置，源码环境使用",
		Server: ServerConfig{
			Host:      cfg.Server.Host,
			Port:      cfg.Server.Port,
			Domain:    cfg.Server.Domain,
			EnableSSL: cfg.Server.EnableSSL,
			SSLCert:   cfg.Server.SSLCert,
			SSLKey:    cfg.Server.SSLKey,
		},
		JWT: JWTConfig{
			Secret:        cfg.JWT.Secret,
			AccessExpire:  cfg.JWT.AccessExpire,
			RefreshExpire: cfg.JWT.RefreshExpire,
		},
		Database: DatabaseConfig{
			Path:              cfg.Database.Path,
			JournalMode:       cfg.Database.JournalMode,
			Synchronous:       cfg.Database.Synchronous,
			CacheSize:         cfg.Database.CacheSize,
			WalAutocheckpoint: cfg.Database.WalAutocheckpoint,
			BusyTimeout:       cfg.Database.BusyTimeout,
		},
		CORS: CORSConfig{
			AllowedOrigins: cfg.CORS.AllowedOrigins,
		},
		Upload: UploadConfig{
			Dir:     cfg.Upload.Dir,
			MaxSize: cfg.Upload.MaxSize,
		},
		Log: LogConfig{
			Dir:        cfg.Log.Dir,
			Level:      cfg.Log.Level,
			DaysToKeep: cfg.Log.DaysToKeep,
			Enabled:    cfg.Log.Enabled,
		},
		Backup: BackupConfig{
			Enabled:         cfg.Backup.Enabled,
			Dir:             cfg.Backup.Dir,
			DaysToKeep:      cfg.Backup.DaysToKeep,
			DatabaseEnabled: cfg.Backup.DatabaseEnabled,
			UploadEnabled:   cfg.Backup.UploadEnabled,
		},
	}

	utils.Success(c, result)
}

func (h *SystemHandler) SaveConfigFile(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequireAdmin（写磁盘配置文件是高危操作，仅 admin 可执行）。
	if !RequireAdmin(c) {
		return
	}
	var req ConfigFile
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	cfg := config.GlobalConfig
	cfg.Server.Host = req.Server.Host
	cfg.Server.Port = req.Server.Port
	cfg.Server.Domain = req.Server.Domain
	cfg.Server.EnableSSL = req.Server.EnableSSL
	cfg.Server.SSLCert = req.Server.SSLCert
	cfg.Server.SSLKey = req.Server.SSLKey
	cfg.JWT.Secret = req.JWT.Secret
	cfg.JWT.AccessExpire = req.JWT.AccessExpire
	cfg.JWT.RefreshExpire = req.JWT.RefreshExpire
	cfg.Database.Path = req.Database.Path
	cfg.CORS.AllowedOrigins = req.CORS.AllowedOrigins
	cfg.Upload.Dir = req.Upload.Dir
	cfg.Upload.MaxSize = req.Upload.MaxSize
	cfg.Log.Dir = req.Log.Dir
	cfg.Log.Level = req.Log.Level
	cfg.Log.DaysToKeep = req.Log.DaysToKeep
	cfg.Log.Enabled = req.Log.Enabled
	cfg.Backup.Enabled = req.Backup.Enabled
	cfg.Backup.Dir = req.Backup.Dir
	cfg.Backup.DaysToKeep = req.Backup.DaysToKeep
	cfg.Backup.DatabaseEnabled = req.Backup.DatabaseEnabled
	cfg.Backup.UploadEnabled = req.Backup.UploadEnabled

	configFile := config.GetConfigFilePath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		utils.Err(c, utils.CodeInternal, "序列化配置失败: "+err.Error())
		return
	}

	err = os.WriteFile(configFile, data, 0644)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "写入配置文件失败: "+err.Error())
		return
	}
	// 审计：配置文件保存（高危操作，写磁盘）。
	database.RecordAudit(c, database.AuditTargetSystem, 0, "config.save", nil)
	utils.Success(c, gin.H{
		"message": "配置文件保存成功",
	})
}

// GetClientInfo 客户端信息（IP 等）。
// 用途：摄像头水印需要显示 IP 时的来源；前端根据 experience_mode 显示体验横幅。
// 注：本地 DocClient 通过 127.0.0.1 代理访问时，本接口返回 127.0.0.1（与审计日志一致）。
func (h *SystemHandler) GetClientInfo(c *gin.Context) {
	utils.Success(c, gin.H{
		"ip":              c.ClientIP(),
		"experience_mode": config.IsExperienceMode(),
	})
}

// BackupNow 同步执行全量备份，等待完成后返回 manifest_id。
// 与之前的 fire-and-forget 行为不同：现在用户能立即拿到备份结果。
//
// P1 修复（2026-06-14）：仅 admin 角色可调用（防误触 / 恶意触发）。
func (h *SystemHandler) BackupNow(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	// Issue C-3：把当前 admin 的 user_id 透传给 service 层，
	// 让 audit_log 中 actor_id 记录真实操作者（不再永久为 0）。
	// Issue C-4：handler 层不再重复 IncBusinessEvent；service 层 defer 已在
	// 出口汇合点统一埋点，避免计数翻倍。
	operatorID := c.GetInt64("user_id")
	manifestID, err := services.PerformBackupNow(true, operatorID) // isManual=true
	if err != nil {
		utils.Err(c, utils.CodeInternal, "备份失败: "+err.Error())
		return
	}
	utils.Success(c, gin.H{
		"message":     "备份完成",
		"manifest_id": manifestID,
	})
}

// RestoreFromBackup 从指定备份恢复（第四阶段 Phase 4.1 衔接 + Phase 4.3 P0 维护模式）。
//
// 请求体：
//
//	{
//	  "full_backup_id": 1,        // 必填：全量备份 manifest_id
//	  "incremental_ids": [3, 5],   // 可选：增量备份 manifest_id 列表
//	  "dry_run": true              // 干跑：只返回将执行的动作，不实际恢复
//	}
//
// ⚠️ **警告**：实际恢复会覆盖当前数据库和上传文件！恢复前会自动快照到 ./backups/snapshot_<ts>/
// 推荐流程：先 dry_run=true 确认操作 → 再 dry_run=false 实际恢复。
//
// **Phase 4.3 P0 维护模式**：进入函数后开启维护模式（拦截所有非白名单 API），
// defer 退出。即使 panic 也会退出（defer 保证）。
//
// P1 修复（2026-06-14）：仅 admin 角色可调用（恢复会覆盖 DB，是最高危操作之一）。
func (h *SystemHandler) RestoreFromBackup(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var req services.RestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "请求参数无效: "+err.Error())
		return
	}
	if req.FullBackupID <= 0 {
		utils.Err(c, utils.CodeInvalidParam, "full_backup_id 必填")
		return
	}

	// dry_run 不进入维护模式（瞬间完成，不阻塞业务）
	if !req.DryRun {
		config.SetMaintenanceMode(true)
		defer config.SetMaintenanceMode(false)
		utils.Info("[Restore] Entering maintenance mode: full_backup_id=%d", req.FullBackupID)
	}

	cfg := config.GlobalConfig
	// Issue C-3：把当前 admin 的 user_id 透传给 service 层，让 audit_log 中
	// actor 字段记录真实操作者（不再永久为 0）。
	// Issue C-4：handler 层不再写 audit 与 IncBusinessEvent，service 层
	// defer 已统一埋点。
	operatorID := c.GetInt64("user_id")
	svc := services.NewRestoreService(cfg)
	result, err := svc.Run(req, operatorID)
	if err != nil {
		if result != nil {
			// dry_run 模式失败也返回详情
			utils.Err(c, utils.CodeInternal, "恢复失败: "+err.Error())
			return
		}
		utils.Err(c, utils.CodeInternal, "恢复失败: "+err.Error())
		return
	}

	// 审计：数据库恢复（最高危操作）。
	database.RecordAudit(c, database.AuditTargetBackup, req.FullBackupID, "restore.execute", gin.H{
		"operator_id":   operatorID,
		"full_backup_id": req.FullBackupID,
		"dry_run":       req.DryRun,
	})

	utils.Success(c, result)
}

// GetMaintenanceStatus 查询维护模式状态。
//
// 用途：前端轮询显示"维护中"横幅，或在恢复期间确认状态。
func (h *SystemHandler) GetMaintenanceStatus(c *gin.Context) {
	utils.Success(c, gin.H{
		"maintenance_mode": config.IsMaintenanceMode(),
	})
}

// SetMaintenanceModeRequest 手动开关维护模式请求体。
type SetMaintenanceModeRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// SetMaintenanceMode 手动开关维护模式（仅 admin，第四阶段 P0）。
//
// 用途：管理员通过 API 临时进入维护模式（不依赖恢复触发）。
// 例如：重大升级、数据修复等需要静默业务的场景。
//
// 请求体：{ "enabled": true, "reason": "升级维护" }
//
// P1 修复（2026-06-14）：增加显式 RequireAdmin 校验（与 comment 里的"仅 admin"对齐）。
func (h *SystemHandler) SetMaintenanceMode(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}
	var req SetMaintenanceModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Err(c, utils.CodeInvalidParam, "请求参数无效: "+err.Error())
		return
	}

	prev := config.IsMaintenanceMode()
	config.SetMaintenanceMode(req.Enabled)

	// 审计：维护模式切换。
	database.RecordAudit(c, database.AuditTargetSystem, 0, "maintenance.set", gin.H{
		"prev":  prev,
		"now":   req.Enabled,
		"reason": req.Reason,
	})
	utils.Info("[Maintenance] mode changed: %v → %v, reason: %s",
		prev, req.Enabled, req.Reason)

	utils.Success(c, gin.H{
		"previous":         prev,
		"maintenance_mode": req.Enabled,
	})
}

var gracefulShutdownFunc func()

func SetGracefulShutdownFunc(f func()) {
	gracefulShutdownFunc = f
}

func (h *SystemHandler) ShutdownServer(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequireAdmin（关停服务是高危操作）。
	if !RequireAdmin(c) {
		return
	}
	utils.Success(c, gin.H{
		"message": "服务器正在关闭",
	})
	go func() {
		time.Sleep(1 * time.Second)
		if gracefulShutdownFunc != nil {
			gracefulShutdownFunc()
		}
	}()
}

type GenerateSSLCertRequest struct {
	CertDir     string `json:"cert_dir"`
	CommonName  string `json:"common_name"`
	ExpiredDays int    `json:"expired_days"`
}

func (h *SystemHandler) GenerateSSLCert(c *gin.Context) {
	// MP3（2026-08-21）：显式 RequireAdmin（生成证书涉及文件系统写入，仅 admin 可执行）。
	if !RequireAdmin(c) {
		return
	}
	var req GenerateSSLCertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.CertDir == "" {
		req.CertDir = "./certs"
	}
	if req.CommonName == "" {
		req.CommonName = "localhost"
	}
	if req.ExpiredDays <= 0 {
		req.ExpiredDays = 365
	}

	certInfo, err := services.GenerateSelfSignedCert(req.CertDir, req.CommonName, req.ExpiredDays)
	if err != nil {
		utils.Err(c, utils.CodeInternal, "生成SSL证书失败: "+err.Error())
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": "SSL证书生成成功",
		"data":    certInfo,
	})
}

// PDF 字体相关代码（/api/system/fonts 路由 + FontInfo 类型 + 系统字体扫描函数）已下线。
