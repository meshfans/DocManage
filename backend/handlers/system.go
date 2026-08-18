package handlers

import (
	"encoding/json"
	"net/http"
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
		utils.Error(c, http.StatusInternalServerError, "获取配置失败")
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
		utils.Error(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
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
	cfg := config.GlobalConfig

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
		utils.Error(c, http.StatusInternalServerError, "序列化配置失败: "+err.Error())
		return
	}

	err = os.WriteFile(configFile, data, 0644)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "写入配置文件失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"message": "配置文件保存成功",
	})
}

// GetClientInfo 客户端信息（IP 等）。
// 用途：摄像头水印需要显示 IP 时的来源。
// 注：本地 DocClient 通过 127.0.0.1 代理访问时，本接口返回 127.0.0.1（与审计日志一致）。
func (h *SystemHandler) GetClientInfo(c *gin.Context) {
	utils.Success(c, gin.H{
		"ip": c.ClientIP(),
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
	manifestID, err := services.PerformBackupNow(true) // isManual=true（用户手动触发）
	if err != nil {
		// Round 16 业务事件埋点：手动备份失败（最稳定的服务层错误出口，HTTP 中间件已计 4xx/5xx）。
		utils.IncBusinessEvent("backup.manual.failed")
		utils.Error(c, 500, "备份失败: "+err.Error())
		return
	}
	// Round 16 业务事件埋点：手动备份成功（仅在 PerformBackupNow 明确成功后上报）。
	utils.IncBusinessEvent("backup.manual.success")
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
		utils.Error(c, 400, "请求参数无效: "+err.Error())
		return
	}
	if req.FullBackupID <= 0 {
		utils.Error(c, 400, "full_backup_id 必填")
		return
	}

	// dry_run 不进入维护模式（瞬间完成，不阻塞业务）
	if !req.DryRun {
		config.SetMaintenanceMode(true)
		defer config.SetMaintenanceMode(false)
		utils.Info("[Restore] Entering maintenance mode: full_backup_id=%d", req.FullBackupID)
	}

	cfg := config.GlobalConfig
	svc := services.NewRestoreService(cfg)
	result, err := svc.Run(req)
	if err != nil {
		// 业务事件埋点统一由 RestoreService.Run 在内部完成（按服务层结果计），此处不重复触发。
		if result != nil {
			// dry_run 模式失败也返回详情
			utils.Error(c, 500, "恢复失败: "+err.Error())
			return
		}
		utils.Error(c, 500, "恢复失败: "+err.Error())
		return
	}

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
		utils.Error(c, 400, "请求参数无效: "+err.Error())
		return
	}

	prev := config.IsMaintenanceMode()
	config.SetMaintenanceMode(req.Enabled)

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
		utils.Error(c, http.StatusInternalServerError, "生成SSL证书失败: "+err.Error())
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": "SSL证书生成成功",
		"data":    certInfo,
	})
}

// PDF 字体相关代码（/api/system/fonts 路由 + FontInfo 类型 + 系统字体扫描函数）已下线。
