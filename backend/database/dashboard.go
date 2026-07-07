package database

// 本文件：数据库生命周期（CloseDatabase）。

// ==================== Lifecycle ====================

// CloseDatabase 关闭全局 DB 连接（main 退出时调用）。
func CloseDatabase() {
	if DB != nil {
		DB.Close()
	}
}
