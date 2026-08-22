package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

type Logger struct {
	logDir     string
	logLevel   LogLevel
	dateStr    string
	currentDay int
	daysToKeep int
	mu         sync.Mutex // 保护 checkAndRotate
	writeMu    sync.Mutex // 保护 OpenFile + WriteString 整体写日志文件
}

var (
	defaultLogger *Logger
	loggerOnce    sync.Once
	debugEnabled  bool
)

func SetDebugMode(enabled bool) {
	debugEnabled = enabled
}

func isDebugEnabled() bool {
	return debugEnabled
}

func NewLogger(logDir string, level LogLevel) (*Logger, error) {
	return NewLoggerWithDays(logDir, level, 30)
}

func NewLoggerWithDays(logDir string, level LogLevel, daysToKeep int) (*Logger, error) {
	logger := &Logger{
		logDir:     logDir,
		logLevel:   level,
		dateStr:    time.Now().Format("2006-01-02"),
		currentDay: time.Now().Day(),
		daysToKeep: daysToKeep,
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 不再启动 goroutine，日志清理改由 Scheduler 调度（system.log_clean）

	return logger, nil
}

func InitLogger(logDir string) error {
	return InitLoggerWithDays(logDir, 30)
}

func InitLoggerWithDays(logDir string, daysToKeep int) error {
	var err error
	loggerOnce.Do(func() {
		defaultLogger, err = NewLoggerWithDays(logDir, LogLevelInfo, daysToKeep)
	})
	return err
}

func InitLoggerWithLevel(logDir string, level LogLevel, daysToKeep int) error {
	var err error
	loggerOnce.Do(func() {
		defaultLogger, err = NewLoggerWithDays(logDir, level, daysToKeep)
	})
	return err
}

// GetLogger 获取默认 logger。
// 2026-06-21 修复测试污染：fallback logDir 不再用相对路径 "./logs"，避免：
//   - 测试运行时 CWD 在 server/handlers/ 等子目录，创建 server/handlers/logs/
//   - 同样的副作用会出现在 server/services/logs/、server/database/logs/、server/middleware/logs/
// 新策略：
//   - 测试环境（通过编译标志或环境变量识别）→ 写入系统临时目录（每次测试隔离）
//   - 生产环境 → 仍用相对路径 "./logs"（CWD 是二进制所在目录）
func GetLogger() *Logger {
	if defaultLogger == nil {
		logDir := resolveDefaultLogDir()
		defaultLogger, _ = NewLoggerWithDays(logDir, LogLevelInfo, 30)
	}
	return defaultLogger
}

// resolveDefaultLogDir 决定未显式 InitLogger 时的 fallback 日志目录。
// 2026-06-21：测试环境（识别方法：环境变量 DOC_TEST_LOG_DIR / 进程名以 .test 结尾）
// 使用 os.TempDir() 子目录，避免污染源码树。
// 注意：
//   - Windows 上 test binary 名为 "pkg.test.exe"，需同时匹配 ".test" 和 ".test.exe"
//   - go test 编译产物路径含 "go-build" 子串
//   - 兜底：环境变量 DOC_TEST_LOG_DIR 强制（推荐 CI 使用）
func resolveDefaultLogDir() string {
	// 1) 测试环境：环境变量指定（推荐）
	if envDir := os.Getenv("DOC_TEST_LOG_DIR"); envDir != "" {
		_ = os.MkdirAll(envDir, 0755)
		return envDir
	}
	// 2) 测试环境：进程名匹配
	execName := filepath.Base(os.Args[0])
	isTestBinary := strings.HasSuffix(execName, ".test") || // Unix: pkg.test
		strings.HasSuffix(execName, ".test.exe") || // Windows: pkg.test.exe
		strings.Contains(execName, "go-build") || // 临时构建路径
		strings.HasSuffix(execName, "test.exe") // 兜底
	if isTestBinary {
		// 测试环境：写入系统临时目录
		logDir := filepath.Join(os.TempDir(), "docmanage-logs")
		_ = os.MkdirAll(logDir, 0755)
		return logDir
	}
	// 3) 生产环境：相对路径（main.go 通常会显式 InitLogger 覆盖）
	return "./logs"
}

// Dir 返回日志目录路径（导出，便于跨包诊断输出）。
func (l *Logger) Dir() string {
	return l.logDir
}

// DaysToKeep 返回日志保留天数（导出，便于跨包诊断输出）。
func (l *Logger) DaysToKeep() int {
	return l.daysToKeep
}

func (l *Logger) checkAndRotate() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	newDateStr := now.Format("2006-01-02")
	newDay := now.Day()

	if newDateStr != l.dateStr || newDay != l.currentDay {
		l.dateStr = newDateStr
		l.currentDay = newDay

		l.CleanOldLogs()
	}
}

func (l *Logger) getLogFileName() string {
	return filepath.Join(l.logDir, fmt.Sprintf("app_%s.log", l.dateStr))
}

func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.logLevel {
		return
	}

	// 非 debug 模式：Info / Warn 静默（既不写文件也不输出控制台）。
	// 与历史行为保持一致：避免生产环境 Info/Warn 刷屏。
	if !isDebugEnabled() && (level == LogLevelInfo || level == LogLevelWarn) {
		return
	}

	l.writeLine(level, "", format, args...)
}

// logWithTraceID 带 trace_id 的日志输出（内部方法，供 *T 系列函数调用）。
// 当 traceID 为空时，行为与 log() 完全一致（仍走 writeLine + 同样的级别控制），
// 不会出现 caller 错位，因为 writeLine 不再走 log()。
func (l *Logger) logWithTraceID(traceID string, level LogLevel, format string, args ...interface{}) {
	if level < l.logLevel {
		return
	}

	// 与 log() 一致：非 debug 模式下 Info / Warn 静默。
	if !isDebugEnabled() && (level == LogLevelInfo || level == LogLevelWarn) {
		return
	}

	l.writeLine(level, traceID, format, args...)
}

// writeLine 统一的日志构造 + 写入入口。traceID 为空时不加 [trace=...] 前缀。
// 所有日志写入（log / logWithTraceID）都通过此方法，保证：
//  1. 并发安全：writeMu 包裹 OpenFile + WriteString 整体，避免多 goroutine 行交错
//  2. caller(2) 一致：无论是否带 traceID，日志里的文件:行号都指向真正的调用方
//  3. 行为一致：是否刷屏控制、是否输出控制台，都由同一个 level 判断
func (l *Logger) writeLine(level LogLevel, traceID string, format string, args ...interface{}) {
	l.checkAndRotate()

	_, file, line, _ := runtime.Caller(3)
	fileName := filepath.Base(file)

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	message := fmt.Sprintf(format, args...)

	var logLine string
	if traceID != "" {
		logLine = fmt.Sprintf("[%s] [%s] [trace=%s] [%s:%d] %s\n",
			timestamp,
			level.String(),
			traceID,
			fileName,
			line,
			message,
		)
	} else {
		logLine = fmt.Sprintf("[%s] [%s] [%s:%d] %s\n",
			timestamp,
			level.String(),
			fileName,
			line,
			message,
		)
	}

	// 控制台输出：Debug / Error / Fatal 总输出；Info / Warn 仅 debug 模式输出
	// （已在 log() / logWithTraceID() 入口短路了非 debug 的 Info/Warn）
	if level == LogLevelDebug || level == LogLevelError || level == LogLevelFatal {
		log.Print(logLine)
	}

	// 文件写入：writeMu 保护 OpenFile + WriteString 整体原子性
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	logFile := l.getLogFileName()
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[日志错误] 写入日志文件失败: %v", err)
		log.Print(logLine)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(logLine); err != nil {
		log.Printf("[日志错误] 写入日志内容失败: %v", err)
	}
}

// CleanOldLogs 清理过期日志文件。可被外部包（如 Scheduler）调用。
func (l *Logger) CleanOldLogs() {
	if l.daysToKeep <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -l.daysToKeep)

	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasPrefix(entry.Name(), "app_") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			logFile := filepath.Join(l.logDir, entry.Name())
			if err := os.Remove(logFile); err != nil {
				log.Printf("[日志清理] 删除过期日志文件失败 %s: %v", logFile, err)
			} else {
				log.Printf("[日志清理] 已清理过期日志文件: %s", logFile)
			}
		}
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LogLevelDebug, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LogLevelInfo, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LogLevelWarn, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LogLevelError, format, args...)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(LogLevelFatal, format, args...)
	os.Exit(1)
}

// Debug 全局便捷函数：调用默认 logger
func Debug(format string, args ...interface{}) {
	GetLogger().Debug(format, args...)
}

// DebugT 使用 trace_id 输出 Debug 日志（便于链路追踪）。
func DebugT(traceID, format string, args ...interface{}) {
	GetLogger().logWithTraceID(traceID, LogLevelDebug, format, args...)
}

func Info(format string, args ...interface{}) {
	GetLogger().Info(format, args...)
}

// InfoT 使用 trace_id 输出 Info 日志（便于链路追踪）。
func InfoT(traceID, format string, args ...interface{}) {
	GetLogger().logWithTraceID(traceID, LogLevelInfo, format, args...)
}

func Warn(format string, args ...interface{}) {
	GetLogger().Warn(format, args...)
}

// WarnT 使用 trace_id 输出 Warn 日志。
func WarnT(traceID, format string, args ...interface{}) {
	GetLogger().logWithTraceID(traceID, LogLevelWarn, format, args...)
}

func LogError(format string, args ...interface{}) {
	GetLogger().Error(format, args...)
}

// LogErrorT 使用 trace_id 输出 Error 日志。
func LogErrorT(traceID, format string, args ...interface{}) {
	GetLogger().logWithTraceID(traceID, LogLevelError, format, args...)
}

func Fatal(format string, args ...interface{}) {
	GetLogger().Fatal(format, args...)
}

func RecoverAndLog() {
	if r := recover(); r != nil {
		LogError("Panic recovered: %v", r)
		LogError("Stack trace: %s", string(getStack()))
	}
}

func getStack() []byte {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

func LogRequest(method, path string, statusCode int, duration time.Duration) {
	level := LogLevelInfo
	if statusCode >= 500 {
		level = LogLevelError
	} else if statusCode >= 400 {
		level = LogLevelWarn
	}

	GetLogger().log(level, "HTTP %s %s %d %v",
		method,
		path,
		statusCode,
		duration,
	)
}

func LogErrorWithContext(context string, err error, format string, args ...interface{}) {
	GetLogger().Error("%s: %v - "+format, append([]interface{}{context, err}, args...)...)
}
