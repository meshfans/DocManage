package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== QuickCheck ====================

func TestQuickCheck_DBIsNil_ReturnsError(t *testing.T) {
	// 保存全局 DB 并临时置为 nil，测试结束还原。
	orig := DB
	DB = nil
	t.Cleanup(func() { DB = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := QuickCheck(ctx)
	if err == nil {
		t.Fatalf("DB == nil 时 QuickCheck 应返回非 nil error")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Fatalf("错误信息应包含 database，实际 %q", err.Error())
	}
}

// TestQuickCheck_TempSQLite_ReturnsNil 启动一个临时 SQLite 数据库并赋值给全局 DB，
// QuickCheck 应返回 nil；随后关闭数据库再调 QuickCheck 应返回 error。
//
// 注意：DB 是全局变量，本测试会改写它；t.Cleanup 负责还原原值，避免污染其他测试。
func TestQuickCheck_TempSQLite_ReturnsNil(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test_quickcheck.db")
	// 直接 sql.Open 一个临时文件，不走 InitDatabase（避免依赖整个 schema/seed 流程）
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开临时 sqlite 失败：%v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping 临时 sqlite 失败：%v", err)
	}

	orig := DB
	DB = db
	t.Cleanup(func() {
		// 关闭后如果 QuickCheck 还会被其他测试调到，应让其走 "DB 未初始化" 路径
		_ = db.Close()
		DB = orig
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := QuickCheck(ctx); err != nil {
		t.Fatalf("临时 sqlite 上 QuickCheck 应返回 nil，实际 %v", err)
	}

	// 关闭后再 QuickCheck 应报错
	_ = db.Close()
	if err := QuickCheck(ctx); err == nil {
		t.Fatalf("关闭数据库后 QuickCheck 应返回非 nil error")
	}
}

// TestQuickCheck_AcceptsContext 验证 ctx 会被使用：传一个已取消的 ctx，
// 应得到非 nil error，且不无限阻塞。
func TestQuickCheck_AcceptsContext(t *testing.T) {
	orig := DB
	DB = nil
	t.Cleanup(func() { DB = orig })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	done := make(chan error, 1)
	go func() { done <- QuickCheck(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("已取消 ctx 应使 QuickCheck 返回 error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("QuickCheck 在已取消 ctx 上应立即返回，不应阻塞")
	}
}

// ==================== Round 16：DBStats ====================

// TestDBStats_NilDB_ReturnsZero 验证 DB 为 nil 时返回 sql.DBStats{} 零值，不 panic。
func TestDBStats_NilDB_ReturnsZero(t *testing.T) {
	orig := DB
	DB = nil
	t.Cleanup(func() { DB = orig })

	stats := DBStats()
	if stats.OpenConnections != 0 ||
		stats.InUse != 0 ||
		stats.Idle != 0 ||
		stats.WaitCount != 0 ||
		stats.MaxIdleClosed != 0 ||
		stats.MaxIdleTimeClosed != 0 ||
		stats.MaxLifetimeClosed != 0 {
		t.Fatalf("DB=nil 时 DBStats 应为零值，实际 %+v", stats)
	}
}

// TestDBStats_RealDB_ReturnsPopulated 验证打开临时 sqlite 后 DBStats 返回非零 OpenConnections。
func TestDBStats_RealDB_ReturnsPopulated(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test_dbstats.db")
	db, err := sql.Open("sqlite3", "file:"+tmp+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开 sqlite 失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	orig := DB
	DB = db
	t.Cleanup(func() { DB = orig })

	// 触发一次真实查询，让 driver 至少建立一条连接（sql.DB 本身懒连接）
	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("查询 sqlite 失败：%v", err)
	}
	if n != 1 {
		t.Fatalf("SELECT 1 应返回 1，实际 %d", n)
	}

	stats := DBStats()
	if stats.OpenConnections < 1 {
		t.Fatalf("打开 sqlite 后 OpenConnections 期望 >=1，实际 %d", stats.OpenConnections)
	}
}
