package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"doc/utils"

	_ "github.com/mattn/go-sqlite3"
)

// 本文件：DB 连接、Schema 创建、索引与种子用户。

// DB 全局数据库连接（由 InitDatabase 初始化；包内其他文件可直接访问）
var DB *sql.DB

// lastInitOptions 缓存最近一次 InitDatabaseWithOptions 的入参。
// 恢复服务（services/restore.go）在 doc.db 被替换后调用 ReloadDatabase 重开连接。
var lastInitOptions atomic.Pointer[DatabaseInitOptions]

// DatabaseInitOptions 数据库初始化选项。
type DatabaseInitOptions struct {
	Path              string
	JournalMode       string // WAL / DELETE / TRUNCATE / MEMORY / OFF
	Synchronous       string // FULL / NORMAL / OFF
	CacheSize         int    // KB（负数）
	WalAutocheckpoint int    // 页数
	BusyTimeout       int    // 毫秒
}

// fillDefaults 填充默认值。空字符串 / 0 视为"未设置"。
func (o *DatabaseInitOptions) fillDefaults() {
	if o.JournalMode == "" {
		o.JournalMode = "WAL"
	}
	if o.Synchronous == "" {
		o.Synchronous = "NORMAL"
	}
	if o.CacheSize == 0 {
		o.CacheSize = -64000 // 64MB
	}
	if o.WalAutocheckpoint == 0 {
		o.WalAutocheckpoint = 1000
	}
	if o.BusyTimeout == 0 {
		o.BusyTimeout = 5000
	}
}

// InitDatabase 打开 SQLite 数据库、建表、建索引、写入种子用户。
// 由 main.go 在启动时调用一次。
func InitDatabase(dbPath string) error {
	return InitDatabaseWithOptions(DatabaseInitOptions{
		Path: dbPath,
	})
}

// InitDatabaseWithOptions 接受完整 DatabaseInitOptions 打开数据库。
func InitDatabaseWithOptions(opts DatabaseInitOptions) error {
	opts.fillDefaults()
	lastInitOptions.Store(&opts)

	dir := filepath.Dir(opts.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// DSN：包含全部 PRAGMA（journal_mode / synchronous / cache_size /
	// wal_autocheckpoint / busy_timeout）。
	// 注：DSN 参数仅作为初始值，applyWALConfig() 会再显式 PRAGMA 设置（更可靠）。
	dsn := fmt.Sprintf(
		"file:%s?_journal_mode=%s&_synchronous=%s&_cache_size=%d&_busy_timeout=%d&_wal_autocheckpoint=%d",
		opts.Path, opts.JournalMode, opts.Synchronous, opts.CacheSize, opts.BusyTimeout, opts.WalAutocheckpoint,
	)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		_ = db.Close() // 资源安全：失败时关闭连接
		return fmt.Errorf("数据库连接失败: %w", err)
	}

	// 显式应用 WAL / checkpoint 配置（DSN 参数只是初始值，
	// PRAGMA 设置才是权威）。这样日志能确认实际生效的模式。
	if err := applyWALConfig(db, opts); err != nil {
		return fmt.Errorf("应用 WAL 配置失败: %w", err)
	}

	// 启动日志：明确告知当前模式（出问题排查时关键）
	journalMode, err := querySinglePragma(db, "journal_mode")
	if err != nil {
		utils.Warn("查询 journal_mode 失败: %v", err)
	} else {
		utils.Info("SQLite journal_mode = %s (配置: %s)", journalMode, opts.JournalMode)
		if !strings.EqualFold(journalMode, opts.JournalMode) && opts.JournalMode != "DELETE" {
			utils.Warn("journal_mode 与配置不一致（可能在网络文件系统上，WAL 不受支持）")
		}
	}

	DB = db

	if err = createTables(); err != nil {
		return fmt.Errorf("建表失败: %w", err)
	}

	if err = migrateDatabase(); err != nil {
		return fmt.Errorf("迁移失败: %w", err)
	}

	if err = createIndexes(); err != nil {
		return fmt.Errorf("建索引失败: %w", err)
	}

	if err = seedDefaultUsers(); err != nil {
		return fmt.Errorf("种子用户失败: %w", err)
	}

	// RBAC 种子（role + permission）
	if err = seedRBAC(); err != nil {
		return fmt.Errorf("种子 RBAC 失败: %w", err)
	}

	// 提醒模板种子（3 个系统预置：合同到期前 30/7/1 天）
	if err = SeedDefaultReminderTemplates(); err != nil {
		return fmt.Errorf("种子提醒模板失败: %w", err)
	}

	return nil
}

// ReloadDatabase 用上次保存的 init options 重新初始化连接。
// 恢复服务（services/restore.go）覆盖 doc.db 后调用本函数重开连接。
//
// 关键点：
//  1. 如果 DB 还没 Init 过（lastInitOptions==nil）→ 报错
//  2. 调用方**必须先**自己 CloseDatabase()
//  3. 走完整 InitDatabaseWithOptions 路径（含 schema migration + WAL 配置）
func ReloadDatabase() error {
	opts := lastInitOptions.Load()
	if opts == nil {
		return fmt.Errorf("ReloadDatabase: 未初始化过 DB，无法 reload")
	}
	utils.Info("[database] ReloadDatabase: %s", opts.Path)
	return InitDatabaseWithOptions(*opts)
}

// QuickCheck 对数据库做轻量完整性校验（Round 15 健康探针使用）。
//  1. 检查全局 DB 是否已初始化（非 nil）
//  2. 调用 PingContext(ctx) 验证连接存活
//  3. 执行 `PRAGMA quick_check`，结果必须严格为 "ok"（其它任何字符串视为损坏）
//
// 调用方应传入带超时的 ctx（例如 readyz 用最多 2 秒的 ctx），
// 避免 IO 挂死导致探针自身超时。
//
// 返回非 nil 表示数据库不可用，错误信息描述具体失败原因。
func QuickCheck(ctx context.Context) error {
	if DB == nil {
		return fmt.Errorf("database 未初始化")
	}
	if err := DB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping 失败: %w", err)
	}
	var result string
	if err := DB.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("quick_check 执行失败: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("quick_check 返回非 ok: %q", result)
	}
	return nil
}

// HasInitOptions 返回最近一次 Init 是否已保存 options（用于 drill 等场景前置检查）。
func HasInitOptions() bool {
	return lastInitOptions.Load() != nil
}

// DBStats 返回当前数据库连接池的 sql.DBStats 快照（Round 16 指标刷新用）。
//
//   - DB 为 nil（未初始化 / 已关闭） → 返回 sql.DBStats{} 零值
//   - DB 非 nil → 调用 DB.Stats() 返回 driver 提供的实时统计
//
// 只读 API，不会触发任何数据库 IO；调用方应负责复用结果（避免每次 /metrics 都 Stats 一遍）。
//
// 故意走 database 包而不是 utils 包，保证 database 不依赖 utils metrics（依赖方向单向）。
func DBStats() sql.DBStats {
	if DB == nil {
		return sql.DBStats{}
	}
	return DB.Stats()
}

// LastInitOptions 返回最近一次 Init 时保存的 options 值快照（值传递，不可改全局）。
// 调用方应在 HasInitOptions() == true 时再调用。未初始化时返回零值。
func LastInitOptions() DatabaseInitOptions {
	opts := lastInitOptions.Load()
	if opts == nil {
		return DatabaseInitOptions{}
	}
	return *opts
}

// applyWALConfig 显式应用 WAL 模式 + checkpoint 设置。
// DSN 参数已经设置了初始值，但这里再次执行以确保生效（网络 FS 等异常情况下 DSN 参数可能被忽略）。
func applyWALConfig(db *sql.DB, opts DatabaseInitOptions) error {
	// 设置 journal_mode（仅当不是 DELETE 时显式设置；DELETE 模式不允许在线切换到 WAL）
	if opts.JournalMode != "DELETE" {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA journal_mode = %s", opts.JournalMode)); err != nil {
			return fmt.Errorf("设置 journal_mode=%s 失败: %w", opts.JournalMode, err)
		}
	}

	// synchronous
	if _, err := db.Exec(fmt.Sprintf("PRAGMA synchronous = %s", opts.Synchronous)); err != nil {
		return fmt.Errorf("设置 synchronous=%s 失败: %w", opts.Synchronous, err)
	}

	// wal_autocheckpoint
	if opts.JournalMode == "WAL" && opts.WalAutocheckpoint > 0 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA wal_autocheckpoint = %d", opts.WalAutocheckpoint)); err != nil {
			return fmt.Errorf("设置 wal_autocheckpoint=%d 失败: %w", opts.WalAutocheckpoint, err)
		}
	}

	return nil
}

// querySinglePragma 查询单个 PRAGMA 设置值。
func querySinglePragma(db *sql.DB, name string) (string, error) {
	var v string
	row := db.QueryRow(fmt.Sprintf("PRAGMA %s", name))
	if err := row.Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

// migrateDatabase 数据迁移钩子（当前为空）。
//
// 2026-06-29：项目为全新部署，所有表 schema 已在 createTables() 中完整定义（含 RBAC v3 字段）。
// migrateDatabase 暂保留为空函数，留待未来需要数据迁移时再填充。
// 历史版本此函数含 11+ 步 RBAC v3 数据迁移 + owner_user_id NULL 回填 + 7 张表加列 + 索引创建，
// 现已全部合入 CREATE TABLE / CREATE INDEX（见 createTables() 与 createIndexes()）。
func migrateDatabase() error {
	// 当前无迁移任务（全新部署，所有字段已在 createTables 中定义）
	return nil
}

// addColumnIfMissing 检查表中是否存在某列，不存在则添加。
// 用于幂等的 schema 迁移（避免重复 ALTER TABLE 导致错误）。
func addColumnIfMissing(table, column, definition string) error {
	// 查询表结构（PRAGMA table_info 返回列信息）
	rows, err := DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("查询表结构失败: %w", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString // dflt_value 可为 NULL
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("扫描列信息失败: %w", err)
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if !found {
		// 列不存在，添加
		utils.Info("[迁移] 表 %s 添加列 %s (%s)", table, column, definition)
		_, err = DB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
		if err != nil {
			return fmt.Errorf("添加列失败: %w", err)
		}
	}
	return nil
}

// createTables 建所有表（CREATE TABLE IF NOT EXISTS，幂等）。
// 全部表结构集中在此：方便审阅 schema 完整性。
func createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		nickname TEXT NOT NULL,
		avatar TEXT DEFAULT '',
		roles TEXT DEFAULT '["common"]',
		permissions TEXT DEFAULT '[]',
		real_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		phone TEXT DEFAULT '',
		department_id INTEGER,
		position TEXT DEFAULT '',
		employee_no TEXT DEFAULT '',
		status TEXT DEFAULT 'active',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		sender_id INTEGER,
		title TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL,
		type TEXT DEFAULT 'system',
		status TEXT DEFAULT 'active',
		read INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (user_id) REFERENCES users(id),
		FOREIGN KEY (sender_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS customer (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		snowid TEXT UNIQUE NOT NULL,
		customer_type TEXT NOT NULL DEFAULT 'individual',
		phone TEXT NOT NULL DEFAULT '',
		email TEXT DEFAULT '',
		address TEXT DEFAULT '',
		remarks TEXT DEFAULT '',
		-- 第十三阶段：客户主负责人（用于"发给我负责的客户"提醒接收人），NULL = 暂未分配
		-- 2026-06-29 修复：补 NOT NULL DEFAULT 0，与 model.Customer.OwnerUserID (int64) 一致；
		-- 旧列允许 NULL 会导致 GetCustomerByID Scan 失败（"converting NULL to int64 is unsupported"）。
		-- 历史 NULL 数据通过下方 UPDATE 迁移回填为 0。
		owner_user_id INTEGER NOT NULL DEFAULT 0,
		-- 2026-06-28 RBAC v3 P3：data_scope 过滤维度（与 owner/department 联合控制）
		department_id INTEGER NOT NULL DEFAULT 0,
		-- 软删除：deleted_at = 0 表示正常；> 0 表示删除时间（Unix 秒）。
		-- 与 contract / media / flow_* 表保持一致，允许"客户下存在历史合同/流程"时也可删除。
		deleted_at INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		-- 个人专属（individual 时填，enterprise 时为空）
		real_name TEXT NOT NULL DEFAULT '',
		id_card TEXT DEFAULT '',
		gender TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		-- 企业专属（enterprise 时填，individual 时为空）
		company_name TEXT DEFAULT '',
		uscc TEXT DEFAULT '',
		legal_person TEXT DEFAULT '',
		legal_person_id_card TEXT DEFAULT '',
		registered_capital TEXT DEFAULT '',
		company_type TEXT DEFAULT '',
		industry TEXT DEFAULT '',
		established_date TEXT DEFAULT '',
		business_scope TEXT DEFAULT '',
		website TEXT DEFAULT '',
		FOREIGN KEY (owner_user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS system_config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		config_key TEXT UNIQUE NOT NULL,
		config_value TEXT NOT NULL DEFAULT '',
		description TEXT DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	CREATE TABLE IF NOT EXISTS department (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		parent_id INTEGER DEFAULT 0,
		level INTEGER NOT NULL DEFAULT 0,
		sort_order INTEGER NOT NULL DEFAULT 0,
		status TEXT DEFAULT 'active',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	-- 定时任务定义表
	CREATE TABLE IF NOT EXISTS scheduled_task (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		task_key        TEXT    UNIQUE NOT NULL,
		name            TEXT    NOT NULL,
		description     TEXT    DEFAULT '',
		task_type       TEXT    NOT NULL DEFAULT 'business',
		cron_expr       TEXT    NOT NULL,
		handler_name    TEXT    NOT NULL,
		handler_params  TEXT    DEFAULT '{}',
		is_active       INTEGER DEFAULT 1,
		is_concurrent   INTEGER DEFAULT 0,
		timeout_seconds INTEGER DEFAULT 3600,
		last_run_at     INTEGER DEFAULT 0,
		last_status     TEXT    DEFAULT 'pending',
		last_error      TEXT    DEFAULT '',
		next_run_at     INTEGER DEFAULT 0,
		run_count       INTEGER DEFAULT 0,
		fail_count      INTEGER DEFAULT 0,
		created_by      INTEGER DEFAULT 0,
		created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	-- 定时任务执行审计日志表
	CREATE TABLE IF NOT EXISTS scheduled_task_log (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id         INTEGER NOT NULL,
		task_key        TEXT    NOT NULL,
		started_at      INTEGER NOT NULL,
		finished_at     INTEGER DEFAULT 0,
		duration_ms     INTEGER DEFAULT 0,
		status          TEXT    NOT NULL,
		output          TEXT    DEFAULT '',
		error           TEXT    DEFAULT '',
		triggered_by    TEXT    DEFAULT 'scheduler',
		operator_id     INTEGER DEFAULT 0,
		FOREIGN KEY (task_id) REFERENCES scheduled_task(id) ON DELETE CASCADE
	);

	-- 定时任务变更审计表（记录对任务定义的修改）
	CREATE TABLE IF NOT EXISTS scheduled_task_audit (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id         INTEGER NOT NULL,
		task_key        TEXT    NOT NULL,
		action          TEXT    NOT NULL,
		field_name      TEXT    DEFAULT '',
		old_value       TEXT    DEFAULT '',
		new_value       TEXT    DEFAULT '',
		operator_id     INTEGER DEFAULT 0,
		operator_name   TEXT    DEFAULT '',
		created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (task_id) REFERENCES scheduled_task(id) ON DELETE CASCADE
	);

	-- 第十一阶段：提醒模板表（系统预置 + 用户可扩展）
	-- template_key 全局唯一；rule_type 决定扫描逻辑（contract_expiring / future: birthday / ...）
	-- is_system=1 表示系统预置不可删，is_active=0 表示禁用
	CREATE TABLE IF NOT EXISTS reminder_template (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		template_key  TEXT    NOT NULL UNIQUE,
		name          TEXT    NOT NULL,
		description   TEXT    DEFAULT '',
		rule_type     TEXT    NOT NULL,
		advance_days  INTEGER NOT NULL DEFAULT 0,
		is_active     INTEGER NOT NULL DEFAULT 1,
		is_system     INTEGER NOT NULL DEFAULT 0,
		sort_order    INTEGER NOT NULL DEFAULT 0,
		created_at    INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at    INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);
	-- 索引见 createIndexes()

	-- 提醒订阅表
	-- 通用多态关联 + 多 ID 设计
	--   link_type 决定查哪张表：
	--     - 'customer'              : 客户级订阅（展开为该客户所有 active 合同）
	--     - 'third_party_contract'  : 第三方合同级订阅
	--   link_id 为 TEXT（CSV 格式，逗号分隔），支持同类型多 ID：
	--     - '5'        : 单 ID 5
	--     - '1,2,3'    : 多 ID [1, 2, 3]（自动去重 + 跳过非法）
	--   应用层 ParseReceiverIDs() 负责解析。
	-- receiver_type / receiver_id 决定提醒发给谁。
	CREATE TABLE IF NOT EXISTS reminder_subscription (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		template_id   INTEGER NOT NULL,
		-- 第十三阶段 v4：通用多态关联
		link_type     TEXT    NOT NULL DEFAULT 'customer',
		link_id       TEXT    NOT NULL DEFAULT '0',
		-- 第十三阶段：提醒接收人
		receiver_type TEXT    NOT NULL DEFAULT 'admin',
		receiver_id   TEXT    NOT NULL DEFAULT '0',
		is_active     INTEGER NOT NULL DEFAULT 1,
		remark        TEXT    DEFAULT '',
		created_by    INTEGER NOT NULL DEFAULT 0,
		-- 2026-06-29 RBAC v3 P1：创建者主部门快照，data_scope 过滤维度
		department_id INTEGER NOT NULL DEFAULT 0,
		created_at    INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at    INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (template_id) REFERENCES reminder_template(id) ON DELETE CASCADE,
		-- link_id 不设外键（多态关联，跨多表，DB 层无法表达）
		CONSTRAINT chk_link_type CHECK (link_type IN
			('customer', 'third_party_contract')),
		CONSTRAINT chk_receiver_type CHECK (receiver_type IN
			('admin', 'customer_owner', 'department', 'user'))
	);
	-- 索引见 createIndexes()

	-- 提醒日志表（实际发送记录，含去重 UNIQUE 约束）
	-- 同一合同+同一模板+同 trigger_date 仅能成功插入一次（DB 层去重）
	-- contract_no / contract_title / customer_name 冗余存储，关联对象删除后仍能查
	-- contract_id 支持 third_party_contract（第三方合同）
	CREATE TABLE IF NOT EXISTS reminder_log (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		template_id      INTEGER NOT NULL,
		subscription_id  INTEGER,
		customer_id      INTEGER,
		contract_id      INTEGER,
		contract_no      TEXT    DEFAULT '',
		contract_title   TEXT    DEFAULT '',
		customer_name    TEXT    DEFAULT '',
		trigger_date     TEXT    NOT NULL,
		days_before      INTEGER NOT NULL,
		message_id       INTEGER,
		delivery_status  TEXT    NOT NULL DEFAULT 'sent',
		error_msg        TEXT    DEFAULT '',
		triggered_by     TEXT    NOT NULL DEFAULT 'scheduler',
		created_at       INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (template_id)     REFERENCES reminder_template(id)    ON DELETE CASCADE,
		FOREIGN KEY (subscription_id) REFERENCES reminder_subscription(id) ON DELETE SET NULL,
		-- contract_id 不设外键（支持 contract 和 third_party_contract 两张表）
		UNIQUE (template_id, contract_id, trigger_date)
	);
	-- 索引见 createIndexes()

	-- 媒体表（v2 单表设计）
	-- 3 个 JSON 列替代附属表：tags / bindings / audit
	-- 软删：deleted_at + status 双控
	-- 9 个索引（见 createIndexes）
	CREATE TABLE IF NOT EXISTS media (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		snowid          TEXT    UNIQUE NOT NULL,
		customer_id     INTEGER NOT NULL DEFAULT 0,
		user_id         INTEGER NOT NULL DEFAULT 0,
		type            TEXT    NOT NULL DEFAULT 'photo',
		name            TEXT    NOT NULL DEFAULT '',
		original_name   TEXT    NOT NULL DEFAULT '',
		mime_type       TEXT    NOT NULL DEFAULT '',
		file_path       TEXT    NOT NULL DEFAULT '',
		thumb_path      TEXT    NOT NULL DEFAULT '',
		file_size       INTEGER NOT NULL DEFAULT 0,
		width           INTEGER NOT NULL DEFAULT 0,
		height          INTEGER NOT NULL DEFAULT 0,
		duration        INTEGER NOT NULL DEFAULT 0,
		hash_sm3        TEXT    NOT NULL DEFAULT '',
		hash_sha256     TEXT    NOT NULL DEFAULT '',
		hash_combined   TEXT    NOT NULL DEFAULT '',
		source          TEXT    NOT NULL DEFAULT 'camera',
		source_ref      TEXT    NOT NULL DEFAULT '',
		watermark_text  TEXT    NOT NULL DEFAULT '',
		watermark_mode  TEXT    NOT NULL DEFAULT 'corner',
		taken_at        INTEGER NOT NULL DEFAULT 0,
		taken_by        INTEGER NOT NULL DEFAULT 0,
		tags            TEXT    NOT NULL DEFAULT '[]',
		bindings        TEXT    NOT NULL DEFAULT '[]',
		audit           TEXT    NOT NULL DEFAULT '[]',
		view_count      INTEGER NOT NULL DEFAULT 0,
		download_count  INTEGER NOT NULL DEFAULT 0,
		remark          TEXT    NOT NULL DEFAULT '',
		status          TEXT    NOT NULL DEFAULT 'active',
		created_by      INTEGER NOT NULL DEFAULT 0,
		-- 2026-06-28 RBAC v3 P3：上传者主部门快照，data_scope 过滤维度
		department_id   INTEGER NOT NULL DEFAULT 0,
		created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		deleted_at      INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (created_by) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS third_party_contract (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		contract_no TEXT UNIQUE NOT NULL,
		title TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'paper',
		status TEXT NOT NULL DEFAULT 'draft',
		customer_id INTEGER NOT NULL,
		amount REAL NOT NULL DEFAULT 0,
		currency TEXT NOT NULL DEFAULT 'CNY',
		sign_date INTEGER NOT NULL DEFAULT 0,
		start_date INTEGER NOT NULL DEFAULT 0,
		end_date INTEGER NOT NULL DEFAULT 0,
		file_path TEXT NOT NULL DEFAULT '',
		file_size INTEGER NOT NULL DEFAULT 0,
		file_sm3_hash TEXT NOT NULL DEFAULT '',
		file_sha256_hash TEXT NOT NULL DEFAULT '',
		file_combined_hash TEXT NOT NULL DEFAULT '',
		remark TEXT NOT NULL DEFAULT '',
		created_by INTEGER NOT NULL,
		-- 2026-06-29 RBAC v3 P4：创建者主部门快照，data_scope 过滤维度
		department_id INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (customer_id) REFERENCES customer(id) ON DELETE RESTRICT,
		FOREIGN KEY (created_by) REFERENCES users(id)
	);

	-- 备份清单表
	-- 记录每次备份（full / incremental）的元数据，用于：
	--   1. 备份列表展示
	--   2. 增量恢复时定位父全量（parent_id 链）
	--   3. 完整性校验（三哈希：hash_sm3 / hash_sha256 / hash_combined）
	--   4. 异地备份追踪（offsite_url / offsite_status）
	--   5. 保留天数链式管理（keep_until：手动备份 = 0 表示永不清除）
	-- 6 个索引（见 createIndexes）
	CREATE TABLE IF NOT EXISTS backup_manifest (
		id                INTEGER PRIMARY KEY AUTOINCREMENT,
		snowid            TEXT    UNIQUE NOT NULL,
		type              TEXT    NOT NULL,                  -- 'full' / 'incremental'
		parent_id         INTEGER,                           -- 增量指向的全量 ID；全量为 NULL
		status            TEXT    NOT NULL DEFAULT 'pending',-- pending / running / success / failed / verified / corrupted / missing
		file_path         TEXT    NOT NULL DEFAULT '',       -- 本地 zip 绝对/相对路径
		file_size         INTEGER NOT NULL DEFAULT 0,        -- 字节
		file_hash_sm3     TEXT    NOT NULL DEFAULT '',       -- SM3 哈希（国密）
		file_hash_sha256  TEXT    NOT NULL DEFAULT '',       -- SHA-256 哈希
		file_hash_combined TEXT   NOT NULL DEFAULT '',       -- CombinedHash = SHA256(SM3 + SHA256)
		wal_range_start   INTEGER NOT NULL DEFAULT 0,        -- WAL 段起始 LSN（增量备份用）
		wal_range_end     INTEGER NOT NULL DEFAULT 0,        -- WAL 段结束 LSN
		changed_files     INTEGER NOT NULL DEFAULT 0,        -- 增量包含的文件数
		total_files       INTEGER NOT NULL DEFAULT 0,        -- 全量包含的文件数
		started_at        INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		finished_at       INTEGER,
		duration_ms       INTEGER NOT NULL DEFAULT 0,
		error_msg         TEXT    NOT NULL DEFAULT '',
		offsite_url       TEXT    NOT NULL DEFAULT '',       -- S3 / OSS 路径
		offsite_status    TEXT    NOT NULL DEFAULT '',       -- uploaded / failed / ''
		offsite_at        INTEGER,
		verified_at       INTEGER,                           -- 最近一次验证时间
		verified_result   TEXT    NOT NULL DEFAULT '',       -- ok / corrupted / missing / ''
		created_by        INTEGER NOT NULL DEFAULT 0,
		keep_until        INTEGER NOT NULL DEFAULT 0        -- 自动清理截止时间：0 = 永不清除（手动备份），>0 = unix 时间戳
	);

	-- RBAC：role 表（角色定义）
	CREATE TABLE IF NOT EXISTS role (
	    id              INTEGER PRIMARY KEY AUTOINCREMENT,
	    code            TEXT    UNIQUE NOT NULL,
	    name            TEXT    NOT NULL,
	    description     TEXT    DEFAULT '',
	    permissions     TEXT    NOT NULL DEFAULT '[]',
	    -- 2026-06-28 RBAC v3：数据范围值域 all/dept_and_sub/dept/self_and_sub_dept/self/custom，默认 'self'（向后兼容）
	    data_scope      TEXT    NOT NULL DEFAULT 'self',
	    -- 2026-06-28 RBAC v3.1：custom 模式部门白名单（JSON 数组，如 "[1,5,10,23]"）
	    custom_dept_ids TEXT    NOT NULL DEFAULT '[]',
	    is_system       INTEGER NOT NULL DEFAULT 0,
	    status          TEXT    NOT NULL DEFAULT 'active',
	    created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    updated_at      INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);

	-- RBAC：permission 表（1:1 对应 API）
	CREATE TABLE IF NOT EXISTS permission (
	    id           INTEGER PRIMARY KEY AUTOINCREMENT,
	    code         TEXT    UNIQUE NOT NULL,
	    name         TEXT    NOT NULL,
	    module       TEXT    NOT NULL DEFAULT '',
	    api_path     TEXT    NOT NULL DEFAULT '',
	    http_method  TEXT    NOT NULL DEFAULT '',
	    description  TEXT    DEFAULT '',
	    is_system    INTEGER NOT NULL DEFAULT 0,
	    status       TEXT    NOT NULL DEFAULT 'active',
	    created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    updated_at   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	`
	if _, err := DB.Exec(query); err != nil {
		return err
	}

	return nil
}

// createIndexes 建所有索引。
func createIndexes() error {
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_customer_department ON customer(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_department ON media(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_third_party_contract_department ON third_party_contract(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_snowid ON customer(snowid)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_type ON customer(customer_type)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_uscc ON customer(uscc) WHERE customer_type = 'enterprise' AND uscc != ''`,
		`CREATE INDEX IF NOT EXISTS idx_messages_user_created ON messages(user_id, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_user_read ON messages(user_id, read, status)`,
		`CREATE INDEX IF NOT EXISTS idx_users_department ON users(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_users_status ON users(status)`,
		`CREATE INDEX IF NOT EXISTS idx_users_employee_no ON users(employee_no)`,
		`CREATE INDEX IF NOT EXISTS idx_department_parent ON department(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_department_level ON department(level)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_active ON scheduled_task(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_key ON scheduled_task(task_key)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_type ON scheduled_task(task_type)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_log_task ON scheduled_task_log(task_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_log_status ON scheduled_task_log(status, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_audit_task ON scheduled_task_audit(task_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_audit_action ON scheduled_task_audit(action, created_at DESC)`,

		// third_party_contract 索引
		`CREATE INDEX IF NOT EXISTS idx_tpc_status  ON third_party_contract(status, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tpc_customer ON third_party_contract(customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tpc_end_date ON third_party_contract(end_date) WHERE end_date > 0`,
		`CREATE INDEX IF NOT EXISTS idx_tpc_no_file  ON third_party_contract(file_size) WHERE file_size = 0`,

		// 媒体表索引
		`CREATE INDEX IF NOT EXISTS idx_media_type        ON media(type)`,
		`CREATE INDEX IF NOT EXISTS idx_media_source      ON media(source)`,
		`CREATE INDEX IF NOT EXISTS idx_media_status      ON media(status) WHERE deleted_at = 0`,
		`CREATE INDEX IF NOT EXISTS idx_media_taken_by    ON media(taken_by)`,
		`CREATE INDEX IF NOT EXISTS idx_media_taken_at    ON media(taken_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_created_at  ON media(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_customer_id ON media(customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_user_id     ON media(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_hash_sm3    ON media(hash_sm3)`,
		`CREATE INDEX IF NOT EXISTS idx_media_hash_sha256 ON media(hash_sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_media_deleted_at  ON media(deleted_at)`,
		// bindings 索引（LIKE 模糊匹配效果有限，但能让查询走索引扫描）
		`CREATE INDEX IF NOT EXISTS idx_media_bindings    ON media(bindings)`,

		// 备份清单索引
		`CREATE INDEX IF NOT EXISTS idx_bm_type        ON backup_manifest(type)`,
		`CREATE INDEX IF NOT EXISTS idx_bm_status      ON backup_manifest(status)`,
		`CREATE INDEX IF NOT EXISTS idx_bm_started_at  ON backup_manifest(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_bm_parent      ON backup_manifest(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bm_verified    ON backup_manifest(verified_at) WHERE verified_at IS NOT NULL`,
		// 链式清理索引：仅索引 keep_until > 0 的（手动备份 = 0 不参与）
		`CREATE INDEX IF NOT EXISTS idx_bm_keep_until  ON backup_manifest(keep_until) WHERE keep_until > 0`,

		// 提醒业务索引
		`CREATE INDEX IF NOT EXISTS idx_rt_active    ON reminder_template(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_rt_rule_type ON reminder_template(rule_type)`,
		`CREATE INDEX IF NOT EXISTS idx_rs_template ON reminder_subscription(template_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rs_link    ON reminder_subscription(link_type, link_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rs_active   ON reminder_subscription(is_active)`,
		// data_scope 过滤索引
		`CREATE INDEX IF NOT EXISTS idx_rs_department ON reminder_subscription(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rl_template ON reminder_log(template_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rl_customer  ON reminder_log(customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rl_contract  ON reminder_log(contract_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rl_date      ON reminder_log(trigger_date)`,

		// 接收人优化索引
		`CREATE INDEX IF NOT EXISTS idx_customer_owner      ON customer(owner_user_id)     WHERE owner_user_id IS NOT NULL`,

		// RBAC 索引
		`CREATE INDEX IF NOT EXISTS idx_role_status   ON role(status)`,
		`CREATE INDEX IF NOT EXISTS idx_perm_path     ON permission(api_path, http_method)`,
		`CREATE INDEX IF NOT EXISTS idx_perm_module   ON permission(module)`,
		`CREATE INDEX IF NOT EXISTS idx_perm_status   ON permission(status)`,
	}
	for _, idx := range indexes {
		if _, err := DB.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

// seedDefaultUsers 仅在 users 表为空时插入 admin/common 两个默认账号。
func seedDefaultUsers() error {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	adminPassword, _ := utils.HashPassword("admin123")
	commonPassword, _ := utils.HashPassword("common123")

	users := []struct {
		username    string
		password    string
		nickname    string
		avatar      string
		roles       string
		permissions string
	}{
		{
			username: "admin",
			password: adminPassword,
			nickname: "管理员",
			// 默认头像：本地 logo.png（前端 import 或 public 资源映射）
			avatar:      "logo.png",
			roles:       `["admin"]`,
			permissions: `[]`,
		},
		{
			username: "common",
			password: commonPassword,
			nickname: "普通用户",
			// 默认头像：本地 logo.png
			avatar:      "logo.png",
			roles:       `["common"]`,
			permissions: `[]`,
		},
	}

	for _, u := range users {
		_, err := DB.Exec(
			`INSERT INTO users (username, password_hash, nickname, avatar, roles, permissions) VALUES (?, ?, ?, ?, ?, ?)`,
			u.username, u.password, u.nickname, u.avatar, u.roles, u.permissions,
		)
		if err != nil {
			return err
		}
	}

	utils.Info("Default users created")
	return nil
}
