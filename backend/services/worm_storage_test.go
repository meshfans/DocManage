package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// wormStorageTestSetup 在 tmp 目录写一份 dummy 文件，并把全局 DB 指向一个临时 sqlite
// （含 worm_record 表）。返回清理函数 + 文件绝对路径。
//
// 注意：调用方在调用 LockOnce / VerifyWorm 之前必须先调 InitWormDBForTest。
func wormStorageTestSetup(t *testing.T) (cleanup func(), absPath, snowid string) {
	t.Helper()
	dir := t.TempDir()
	absPath = filepath.Join(dir, "asset.pdf")
	if err := os.WriteFile(absPath, []byte("hello-worm"), 0644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	cleanup = initWormDBForTest(t)
	snowid = "WORM-TEST-SNOWID-001"
	return
}

// 本测试独立于数据库层的 worm_test.go：复用 services 测试不需要 InitDatabase 全部表。
// 仅注入 worm_record 表。

// 复用 database 包内已有的 initWormDBForTest 入口（由 worm_storage_test.go 自身实现）。
// 真实定义在文件下方。

func TestLockOnce_FilePath(t *testing.T) {
	cleanup, absPath, snowid := wormStorageTestSetup(t)
	defer cleanup()

	rec, err := LockOnce(absPath, snowid, 42, "test.upload")
	if err != nil {
		t.Fatalf("LockOnce 应成功: %v", err)
	}
	if rec == nil || rec.SnowID != snowid || rec.FilePath != absPath {
		t.Fatalf("record 字段不匹配: %+v", rec)
	}
	if len(rec.FileHashSHA) != 64 { // SHA256 hex 长度
		t.Fatalf("hash 应为 64 hex chars，实际: %q", rec.FileHashSHA)
	}
}

func TestVerifyWorm_NotLocked(t *testing.T) {
	cleanup, absPath, _ := wormStorageTestSetup(t)
	defer cleanup()

	// 未 LockOnce → locked=false
	locked, hash, err := VerifyWorm(absPath)
	if err != nil {
		t.Fatalf("Verify 未锁文件应 nil err: %v", err)
	}
	if locked {
		t.Fatalf("未锁文件应 locked=false，实际 true")
	}
	if hash != "" {
		t.Fatalf("未锁文件应 hash=\"\"，实际 %q", hash)
	}
}

func TestVerifyWorm_AppendMutation(t *testing.T) {
	cleanup, absPath, snowid := wormStorageTestSetup(t)
	defer cleanup()

	if _, err := LockOnce(absPath, snowid, 1, "test.upload"); err != nil {
		t.Fatalf("LockOnce 失败: %v", err)
	}

	// append 字节模拟篡改
	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("打开追加失败: %v", err)
	}
	if _, err := f.WriteString("TAMPER"); err != nil {
		f.Close()
		t.Fatalf("追加写失败: %v", err)
	}
	f.Close()

	locked, _, err := VerifyWorm(absPath)
	if !locked {
		t.Fatalf("已锁文件应 locked=true，实际 false")
	}
	if !errors.Is(err, ErrWormTampered) {
		t.Fatalf("应返 ErrWormTampered，实际: %v", err)
	}
}

func TestVerifyWorm_DeleteMutation(t *testing.T) {
	cleanup, absPath, snowid := wormStorageTestSetup(t)
	defer cleanup()

	if _, err := LockOnce(absPath, snowid, 1, "test.upload"); err != nil {
		t.Fatalf("LockOnce 失败: %v", err)
	}
	if err := os.Remove(absPath); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	locked, _, err := VerifyWorm(absPath)
	if !locked {
		t.Fatalf("文件已删但 DB 仍记锁，locked 应为 true")
	}
	if !errors.Is(err, ErrWormTampered) {
		t.Fatalf("文件已删应返 ErrWormTampered，实际: %v", err)
	}
}

func TestVerifyWorm_OverwriteMutation(t *testing.T) {
	cleanup, absPath, snowid := wormStorageTestSetup(t)
	defer cleanup()

	if _, err := LockOnce(absPath, snowid, 1, "test.upload"); err != nil {
		t.Fatalf("LockOnce 失败: %v", err)
	}
	// 整文件覆写
	if err := os.WriteFile(absPath, []byte("different content entirely"), 0644); err != nil {
		t.Fatalf("覆写失败: %v", err)
	}
	locked, _, err := VerifyWorm(absPath)
	if !locked || !errors.Is(err, ErrWormTampered) {
		t.Fatalf("覆写后应报 tampered，实际 locked=%v err=%v", locked, err)
	}
}

func TestLockOnce_HashMatchesActual(t *testing.T) {
	cleanup, absPath, snowid := wormStorageTestSetup(t)
	defer cleanup()

	rec, err := LockOnce(absPath, snowid, 1, "test.upload")
	if err != nil {
		t.Fatalf("LockOnce 失败: %v", err)
	}
	// 独立计算 hash 比对
	data, _ := os.ReadFile(absPath)
	h := sha256.Sum256(data)
	expected := hex.EncodeToString(h[:])
	if rec.FileHashSHA != expected {
		t.Fatalf("DB 记录的 hash %s 与实际 %s 不匹配", rec.FileHashSHA, expected)
	}
}
