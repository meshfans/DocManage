//go:build sqlite_trace

package database

import (
	"database/sql"
	"os"
	"strings"

	sqlite3 "github.com/mattn/go-sqlite3"

	"doc/utils"
)

// init 当 SQL_DEBUG=1 时，注册带 trace 的 driver
func init() {
	if os.Getenv("SQL_DEBUG") != "1" {
		return
	}

	// 注册带 trace 的 driver
	sql.Register("sqlite3_with_trace",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				// 设置 trace callback
				err := conn.SetTrace(&sqlite3.TraceConfig{
					Callback: func(info sqlite3.TraceInfo) int {
						// 只处理 SQL 语句事件
						if info.EventCode == sqlite3.TraceStmt && info.StmtOrTrigger != "" {
							// 过滤 PRAGMA 和内部查询
							upper := strings.ToUpper(info.StmtOrTrigger)
							if !strings.HasPrefix(upper, "PRAGMA") &&
								!strings.HasPrefix(upper, "SELECT 'sqlite_") &&
								!strings.HasPrefix(info.StmtOrTrigger, "--") {
								// 获取展开的 SQL（包含实际参数）
								if info.ExpandedSQL != "" {
									utils.Debug("[SQL TRACE] %s", info.ExpandedSQL)
								} else {
									utils.Debug("[SQL TRACE] %s", info.StmtOrTrigger)
								}
							}
						}
						return 0
					},
					EventMask:       sqlite3.TraceStmt,
					WantExpandedSQL: true,
				})
				return err
			},
		},
	)

	utils.Warn("[SQL DEBUG] SQLite 全局 SQL 追踪已启用（driver=sqlite3_with_trace）")
}

// LogSQL 手动打印 SQL 调试信息（可选使用）
func LogSQL(label, query string, args ...interface{}) {
	if os.Getenv("SQL_DEBUG") != "1" {
		return
	}
	utils.Warn("[SQL DEBUG] %s: %s\n  Args: %v", label, query, args)
}
