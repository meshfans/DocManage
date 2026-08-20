package config

import (
	"doc/utils"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Server    ServerConfig    `json:"server"`
	JWT       JWTConfig       `json:"jwt"`
	Database  DatabaseConfig  `json:"database"`
	CORS      CORSConfig      `json:"cors"`
	Upload    UploadConfig    `json:"upload"`
	WORM      WORMConfig      `json:"worm"`
	Log       LogConfig       `json:"log"`
	Backup    BackupConfig    `json:"backup"`
	Scheduler SchedulerConfig `json:"scheduler"`

	// MaintenanceMode 进程内维护模式开关（第四阶段 P0）。
	//
	// 用途：恢复期间（RestoreFromBackup）拦截所有业务 API，避免数据漂移。
	//   - false（默认）：正常服务
	//   - true：仅放行白名单（/api/health、/api/system/maintenance）
	//
	// 不写入 JSON（json:"-"）：纯运行时状态，重启后回到 false。
	// 进程重启意味着恢复中断，状态丢失是正确行为。
	MaintenanceMode bool `json:"-"`
}

type ServerConfig struct {
	Host      string `json:"host"`
	Port      string `json:"port"`
	Domain    string `json:"domain"`
	EnableSSL bool   `json:"enable_ssl"`
	SSLCert   string `json:"ssl_cert"`
	SSLKey    string `json:"ssl_key"`
	// Debug 启用后挂载 /debug/pprof/*（Phase 5b High #16）。生产默认 false。
	Debug bool `json:"debug"`
}

type JWTConfig struct {
	Secret        string `json:"secret"`
	AccessExpire  string `json:"access_expire"`
	RefreshExpire string `json:"refresh_expire"`
}

// DatabaseConfig 数据库配置。
type DatabaseConfig struct {
	Path string `json:"path"`

	// Mode 控制业务写入行为。允许值：
	//   - "" / "development" / "test" / "production": 正常模式
	//   - "experience": 体验模式（ExperienceReadOnly 中间件拦截业务写请求，返回 423）
	// 未知值触发 warn 日志但不中断启动（向后兼容）。
	Mode string `json:"mode"`

	// JournalMode: WAL / DELETE / TRUNCATE / MEMORY / OFF，默认 DELETE。
	JournalMode string `json:"journal_mode"`

	// Synchronous: FULL / NORMAL / OFF，默认 NORMAL。
	Synchronous string `json:"synchronous"`

	// CacheSize 单位 KB。负数 = KB，正数 = 字节。默认 64000 = 64MB。
	CacheSize int `json:"cache_size"`

	// WalAutocheckpoint 每写入多少页触发一次 checkpoint。默认 1000，仅 WAL 模式生效。
	WalAutocheckpoint int `json:"wal_autocheckpoint"`

	// BusyTimeout 毫秒。获取写锁的超时，默认 5000。
	BusyTimeout int `json:"busy_timeout"`
}

type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

type UploadConfig struct {
	Dir     string `json:"dir"`
	MaxSize int64  `json:"max_size"`
}

// WORMConfig WORM 存储配置（Write Once Read Many，仅用于第三方合同归档）。
type WORMConfig struct {
	Dir       string `json:"dir"`
	ExportDir string `json:"export_dir"`
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
	// 调度已统一到 scheduled_task 表的 cron_expr 管理（Phase 4.1+）。
	// 旧的 Interval 字段已删除（曾用于 BackupService 自带 ticker）。
	DaysToKeep      int  `json:"days_to_keep"`
	DatabaseEnabled bool `json:"database_enabled"`
	UploadEnabled   bool `json:"upload_enabled"`

	// 备份加密字段（EncryptLocal / EncryptPassphrase）已下线，备份 zip 不再加密落盘。
}

type SchedulerConfig struct {
	Enabled        bool   `json:"enabled"`
	Timezone       string `json:"timezone"`
	LogRetention   int    `json:"log_retention"`
	LogOutputMaxKB int    `json:"log_output_max_kb"`
}

var GlobalConfig *Config

func LoadConfig() *Config {
	config := &Config{}

	configFile := getEnv("CONFIG_FILE", "config.json")
	configFilePath = configFile

	if _, err := os.Stat(configFile); err == nil {
		utils.Info("Loading configuration from: %s", configFile)
		if err := loadConfigFromFile(configFile, config); err != nil {
			utils.Info("Warning: Failed to load config file: %v, using defaults", err)
		}
	} else {
		utils.Info("Config file not found: %s, using defaults", configFile)
	}

	applyEnvOverrides(config)
	validateConfig(config)

	GlobalConfig = config
	return config
}

func loadConfigFromFile(filename string, config *Config) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return err
	}

	return nil
}

func applyEnvOverrides(config *Config) {
	if host := getEnv("SERVER_HOST", ""); host != "" {
		config.Server.Host = host
	}

	if port := getEnv("SERVER_PORT", ""); port != "" {
		config.Server.Port = port
	}

	// WEB_HOST / WEB_PORT 已废弃（Phase 4.1+）：原 WebConfig 已删除

	if secret := getEnv("JWT_SECRET", ""); secret != "" {
		config.JWT.Secret = secret
	}

	if accessExpire := getEnv("JWT_ACCESS_EXPIRE", ""); accessExpire != "" {
		config.JWT.AccessExpire = accessExpire
	}

	if refreshExpire := getEnv("JWT_REFRESH_EXPIRE", ""); refreshExpire != "" {
		config.JWT.RefreshExpire = refreshExpire
	}

	if dbPath := getEnv("DB_PATH", ""); dbPath != "" {
		config.Database.Path = dbPath
	}

	if dbMode := getEnv("DB_MODE", ""); dbMode != "" {
		config.Database.Mode = dbMode
	}

	if uploadDir := getEnv("UPLOAD_DIR", ""); uploadDir != "" {
		config.Upload.Dir = uploadDir
	}

	// LICENSE_MACHINE_CODE / LICENSE_KEY 环境变量已下线
}

func validateConfig(config *Config) {
	if !isValidDatabaseMode(config.Database.Mode) {
		utils.Warn("[config] 未知 database.mode=%q（允许值: %s），按正常模式继续运行",
			config.Database.Mode, strings.Join(validDatabaseModes(), ", "))
	}

	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}

	if config.Server.Port == "" {
		config.Server.Port = "8080"
	}

	if config.Server.Domain == "" {
		config.Server.Domain = "localhost"
	}

	if config.Server.EnableSSL {
		if config.Server.SSLCert == "" {
			config.Server.SSLCert = "./certs/public.pem"
		}
		if config.Server.SSLKey == "" {
			config.Server.SSLKey = "./certs/private.pem"
		}
	}

	// Web 段已废弃（Phase 4.1+）：原 WebConfig 已删除，相关默认值移除

	if config.JWT.Secret == "" {
		panic("FATAL: JWT secret is required! Set it in config.json or JWT_SECRET environment variable")
	}

	// J.2：secret 长度 < 32 直接 panic，不再仅 warn。
	// 32 字符（256 bit）匹配 HS512 算法推荐最低熵（RFC 7518 §3.2）。
	// dev 环境用 run.ps1 自带的 "dev_only_local_secret_at_least_32_chars" 兜底（34 字符），
	// 因此正常 dev 流程不受影响。
	if len(config.JWT.Secret) < 32 {
		panic(fmt.Sprintf("FATAL: JWT_SECRET too short (len=%d, min=32). "+
			"Generate with: openssl rand -hex 32", len(config.JWT.Secret)))
	}

	if config.JWT.AccessExpire == "" {
		config.JWT.AccessExpire = "24h"
	}

	if config.JWT.RefreshExpire == "" {
		config.JWT.RefreshExpire = "168h"
	}

	if config.Database.Path == "" {
		config.Database.Path = "./data/doc.db"
	}

	// bool 零值无法区分"未设置"和"显式 false"，
	// 字符串 / int 零值能区分，这里只对前者用 if-empty 守卫。
	if config.Database.JournalMode == "" {
		config.Database.JournalMode = "DELETE"
	}
	if config.Database.Synchronous == "" {
		config.Database.Synchronous = "NORMAL"
	}
	if config.Database.CacheSize == 0 {
		config.Database.CacheSize = -64000 // 64MB（负数 = KB）
	}
	if config.Database.WalAutocheckpoint == 0 {
		config.Database.WalAutocheckpoint = 1000
	}
	if config.Database.BusyTimeout == 0 {
		config.Database.BusyTimeout = 5000
	}

	if config.CORS.AllowedOrigins == nil || len(config.CORS.AllowedOrigins) == 0 {
		config.CORS.AllowedOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:8080",
			"http://127.0.0.1:8080",
		}
	}

	if config.Upload.Dir == "" {
		config.Upload.Dir = "./uploads"
	}

	// WORM 目录默认在 uploads/worm
	if config.WORM.Dir == "" {
		config.WORM.Dir = config.Upload.Dir + "/worm"
	}

	if config.WORM.ExportDir == "" {
		config.WORM.ExportDir = config.Upload.Dir + "/exports"
	}

	if config.Upload.MaxSize == 0 {
		config.Upload.MaxSize = 10 << 20
	}

	if config.Log.Dir == "" {
		config.Log.Dir = "./logs"
	}

	if config.Log.Level == "" {
		config.Log.Level = "info"
	}

	if config.Log.DaysToKeep == 0 {
		config.Log.DaysToKeep = 30
	}

	if !config.Backup.Enabled {
		config.Backup.Enabled = true
	}

	if config.Backup.Dir == "" {
		config.Backup.Dir = "./backups"
	}

	// Interval 字段已废弃（Phase 4.1+）：调度统一到 scheduled_task 表

	if config.Backup.DaysToKeep == 0 {
		config.Backup.DaysToKeep = 7
	}

	// 默认同时备份数据库 + 上传文件（种子任务「备份任务」依赖此配置）
	// bool 零值无法区分"未设置"和"显式 false"，故直接强制为 true
	// 如需精确控制某一项，编辑 config.json 后重启即可覆盖
	config.Backup.DatabaseEnabled = true
	config.Backup.UploadEnabled = true

	if !config.Scheduler.Enabled {
		config.Scheduler.Enabled = true
	}
	if config.Scheduler.Timezone == "" {
		config.Scheduler.Timezone = "Asia/Shanghai"
	}
	if config.Scheduler.LogRetention == 0 {
		config.Scheduler.LogRetention = 1000
	}
	if config.Scheduler.LogOutputMaxKB == 0 {
		config.Scheduler.LogOutputMaxKB = 4
	}

	// PDF.Font.Default/FilePath 默认值赋值已删除（runtime 用 pdfcpu 内置 Helvetica）
}

func (c *Config) GetAccessExpireDuration() time.Duration {
	d, err := time.ParseDuration(c.JWT.AccessExpire)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

func (c *Config) GetRefreshExpireDuration() time.Duration {
	d, err := time.ParseDuration(c.JWT.RefreshExpire)
	if err != nil {
		return 7 * 24 * time.Hour
	}
	return d
}

// GetBackupIntervalDuration 已废弃（Phase 4.1+）：Interval 字段已删除。
// 调度统一由 scheduled_task 表的 cron_expr 接管。
// 保留此方法仅作向后兼容兜底（返回 6h），如有调用请改用 scheduled_task API。
func (c *Config) GetBackupIntervalDuration() time.Duration {
	return 6 * time.Hour
}

var configFilePath string

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func GetConfigFilePath() string {
	return configFilePath
}

func (c *Config) IsPathAllowed(filePath string) bool {
	return true
}

func IsPathTraversal(filePath string) bool {
	cleanPath := filepath.Clean(filePath)
	return strings.Contains(cleanPath, "..")
}

// IsExperienceMode 查询是否处于「体验模式」（database.mode == "experience"）。
//
// 体验模式：所有业务写请求被 ExperienceReadOnly 中间件拦截（返回 423），
// 但登录、登出、刷新 token、审计 share 页面等白名单路径正常。
// 用于对外演示 / 售前试用，数据不会被改动。
//
// 返回 false 当 GlobalConfig 未初始化，保证 nil-safe。
func IsExperienceMode() bool {
	return GlobalConfig != nil && GlobalConfig.Database.Mode == "experience"
}

// validDatabaseModes 返回 database.mode 允许值集合。
//
//   - "" / "development" / "test" / "production": 正常模式
//   - "experience": 体验模式（中间件拦截业务写入）
//
// 保持为函数而非包级变量，便于测试与热更新扩展。
func validDatabaseModes() []string {
	return []string{"", "development", "test", "experience", "production"}
}

func isValidDatabaseMode(mode string) bool {
	for _, m := range validDatabaseModes() {
		if mode == m {
			return true
		}
	}
	return false
}

// IsMaintenanceMode 查询当前是否处于维护模式（第四阶段 P0）。
//
// 返回 false 当 GlobalConfig 未初始化（启动早期），保证 nil-safe。
func IsMaintenanceMode() bool {
	if GlobalConfig == nil {
		return false
	}
	return GlobalConfig.MaintenanceMode
}

// SetMaintenanceMode 设置维护模式（第四阶段 P0）。
//
// 线程安全：bool 赋值在 Go 中是字长对齐原子操作，无需 mutex。
// nil-safe：GlobalConfig 未初始化时不 panic。
func SetMaintenanceMode(enabled bool) {
	if GlobalConfig == nil {
		return
	}
	GlobalConfig.MaintenanceMode = enabled
}
