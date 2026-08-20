package database

import (
	"encoding/json"
	"fmt"

	"doc/utils"
)

// seedRBAC 写入 RBAC 种子数据：role + permission
// 1:1 设计：每个 API 对应一条 permission 记录
//
// 紧急关停 = permission.status = 'disabled'（admin 在 RBAC 管理页可操作）。
//
// seed 行为说明：
//   - **不会覆盖**：seedPermissions / seedRoles 启动时用 INSERT OR IGNORE / ON CONFLICT DO NOTHING，
//     业务修改过的种子数据（admin 改名 / permission 描述变更）不会被恢复。
//   - 若要重置种子，需要手动清表后重启：
//     DELETE FROM role WHERE code IN ('admin','manager','common');
//     DELETE FROM permission WHERE code IN (...);
//     -- 然后重启服务，seedRBAC 会重建。
//   - 这避免了"管理员改名 admin → 重启后被种子覆盖"的灾难，是 by-design。
func seedRBAC() error {
	if err := seedPermissions(); err != nil {
		return fmt.Errorf("seedPermissions: %w", err)
	}
	if err := seedRoles(); err != nil {
		return fmt.Errorf("seedRoles: %w", err)
	}
	// 启动时不变式校验：common 角色必须包含 user:info（这是 /welcome 路由的兜底权限）。
	// 如果 admin 从 common 移除 user:info，所有普通用户登录后跳 /welcome → 403。
	// 在 seed 流程末尾做硬断言，启动期立即 panic 比运行时沉默失败强。
	if err := assertCommonHasUserInfo(); err != nil {
		return fmt.Errorf("RBAC 不变式校验失败: %w（common 角色必须包含 user:info，否则前端 /welcome 兜底权限缺失，所有普通用户登录后跳 403）", err)
	}
	return nil
}

// assertCommonHasUserInfo 不变式：common 角色的 permissions JSON 数组必须包含 "user:info"。
//
// 为什么必须包含：
//   - 前端 router/modules/home.ts /welcome 路由声明 permissions: ["user:info"]，
//     作为"已登录用户"兜底（任何登录后访问 /welcome 的人都能看）。
//   - common 角色是所有非 admin / non-manager 用户的默认角色。
//   - 如果 admin 在 RBAC 管理页面误删 common 角色的 user:info → 静默破坏兜底。
//
// 修复时机：seedRBAC 末尾，启动期早死。失败 = panic via InitDatabase 返回 error。
func assertCommonHasUserInfo() error {
	var permsJSON string
	err := DB.QueryRow(`SELECT permissions FROM role WHERE code = ?`, "common").Scan(&permsJSON)
	if err != nil {
		return fmt.Errorf("查 common 角色失败: %w", err)
	}
	var perms []string
	if err := json.Unmarshal([]byte(permsJSON), &perms); err != nil {
		return fmt.Errorf("解析 common.permissions 失败: %w", err)
	}
	for _, p := range perms {
		if p == "user:info" {
			return nil
		}
	}
	return fmt.Errorf("common 角色缺少 user:info（当前权限：%v）", perms)
}

// Role 角色
type roleSeed struct {
	code, name, desc, permsJSON, dataScope string
	isSystem                               bool
}

func seedRoles() error {
	roles := []roleSeed{
		{
			code: "admin", name: "超级管理员",
			desc: "系统所有权限",
			permsJSON: mustJSON([]string{
				"*:*:*",
			}),
			dataScope: "all", // admin 看全部
			isSystem:  true,
		},
		{
			code: "manager", name: "业务管理员",
			desc: "业务数据读写权限",
			permsJSON: mustJSON([]string{
				"customer:list", "customer:detail", "customer:create", "customer:update", "customer:delete",
				"customer:upload-signature", "customer:create-ext", "customer:update-ext",
				"customer:list-by-type", "customer:search-by-type",
				"media:list", "media:by-target", "media:detail",
				"media:create", "media:update", "media:delete", "media:restore",
				"media:tags", "media:bulk-tag", "media:bulk-delete", "media:bulk-customer",
				"media:file", "media:thumb", "media:upload",
				"media:check-hash", "media:verify",
				"user:list", "user:detail", "user:check-username", "user:by-department", "user:info",
				"dept:list", "dept:detail", "dept:users",
				"permission:version:query",
			}),
			dataScope: "dept", // manager 看本部门
			isSystem:  false,
		},
		{
			code: "common", name: "普通用户",
			desc: "基础用户：查看文档 + 消息 + 公开业务配置 + 用户设置",
			permsJSON: mustJSON([]string{
				// 消息：站内信全 CRUD + 已读 / 未读
				"message:list",
				"message:unread-count",
				"message:detail",
				"message:create",
				"message:mark-read",
				"message:mark-all-read",
				"message:delete",
				// 系统：公开业务配置 / 动态路由 / 权限版本轮询
				"systemconfig:public",
				"routes:async",
				"permission:version:query",
				// 用户：自己信息 + 改密 + 登出
				"user:info", // assertCommonHasUserInfo 不变式强依赖
				"user:change-password",
				"user:logout",
			}),
			dataScope: "self", // common 看自己（默认）
			isSystem:  true,
		},
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO role (code, name, description, permissions, is_system, data_scope) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, r := range roles {
		if _, err := stmt.Exec(r.code, r.name, r.desc, r.permsJSON, boolToInt(r.isSystem), r.dataScope); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 对已存在的 admin/manager 角色回填 data_scope
	// （INSERT OR IGNORE 不会更新已存在行，所以单独 UPDATE 一次）
	// common 保持 'self'（schema 默认值，行为不变）
	updates := []struct {
		code, scope string
	}{
		{"admin", "all"},
		{"manager", "dept"},
		{"common", "self"},
	}
	for _, u := range updates {
		if _, err := DB.Exec(`UPDATE role SET data_scope = ? WHERE code = ?`, u.scope, u.code); err != nil {
			return fmt.Errorf("回填 role.data_scope 失败 (code=%s): %w", u.code, err)
		}
	}
	utils.Info("RBAC 种子: role 表写入 %d 条，data_scope 回填完成", len(roles))
	return nil
}

// permSeed 单条 permission 种子
type permSeed struct {
	code, name, module, apiPath, httpMethod, desc string
	isSystem                                      bool
}

func seedPermissions() error {
	perms := buildPermissionSeeds()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO permission
		 (code, name, module, api_path, http_method, description, is_system)
		 VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, p := range perms {
		if _, err := stmt.Exec(p.code, p.name, p.module, p.apiPath, p.httpMethod, p.desc, boolToInt(p.isSystem)); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	utils.Info("RBAC 种子: permission 表写入 %d 条", len(perms))
	return nil
}

// buildPermissionSeeds 构建 permission（1:1 对应所有 protected API）
func buildPermissionSeeds() []permSeed {
	p := func(code, name, module, api, method string) permSeed {
		return permSeed{code: code, name: name, module: module, apiPath: api, httpMethod: method, isSystem: true}
	}

	s := []permSeed{
		// === hash ===
		p("hash:calculate", "计算文件哈希", "hash", "/api/hash/calculate", "POST"),
		p("hash:verify", "校验文件哈希", "hash", "/api/hash/verify", "POST"),
		p("hash:algorithms", "查询支持的哈希算法", "hash", "/api/hash/algorithms", "GET"),

		// === dept ===
		p("dept:list", "部门列表", "dept", "/api/departments", "GET"),
		p("dept:detail", "部门详情", "dept", "/api/departments/:id", "GET"),
		p("dept:create", "创建部门", "dept", "/api/departments", "POST"),
		p("dept:update", "更新部门", "dept", "/api/departments/:id/update", "POST"),
		p("dept:delete", "删除部门", "dept", "/api/departments/:id/delete", "POST"),
		p("dept:users", "部门用户", "dept", "/api/departments/:id/users", "GET"),

		// === user ===
		p("user:list", "用户列表", "user", "/api/users", "GET"),
		p("user:check-username", "检查用户名", "user", "/api/users/check-username", "GET"),
		p("user:create", "创建用户", "user", "/api/users", "POST"),
		p("user:detail", "用户详情", "user", "/api/users/:id", "GET"),
		p("user:update", "更新用户", "user", "/api/users/:id/update", "POST"),
		p("user:delete", "删除用户", "user", "/api/users/:id/delete", "POST"),
		p("user:update-department", "调整用户部门", "user", "/api/users/:id/department", "POST"),
		p("user:assign-roles", "分配用户角色", "user", "/api/users/:id/roles", "POST"),
		p("user:by-department", "部门用户列表", "user", "/api/users/department/:id", "GET"),
		p("user:info", "我的信息", "user", "/api/user/info", "GET"),
		p("user:logout", "登出", "user", "/api/logout", "POST"),
		p("user:change-password", "改密", "user", "/api/change-password", "POST"),

		// === customer ===
		p("customer:list", "客户列表", "customer", "/api/customer/list", "GET"),
		p("customer:create", "创建客户", "customer", "/api/customer/create", "POST"),
		p("customer:detail", "客户详情", "customer", "/api/customer/query", "GET"),
		p("customer:update", "更新客户", "customer", "/api/customer/update", "POST"),
		p("customer:delete", "删除客户", "customer", "/api/customer/delete", "POST"),
		p("customer:upload-signature", "上传客户签名", "customer", "/api/customer/upload-signature", "POST"),
		p("customer:create-ext", "扩展创建客户", "customer", "/api/customer/create-ext", "POST"),
		p("customer:update-ext", "扩展更新客户", "customer", "/api/customer/update-ext", "POST"),
		p("customer:list-by-type", "按类型查客户", "customer", "/api/customer/list-by-type", "GET"),
		p("customer:search-by-type", "按类型搜索客户", "customer", "/api/customer/search-by-type", "GET"),

		// === message ===
		p("message:list", "消息列表", "message", "/api/messages/list", "GET"),
		p("message:unread-count", "未读数", "message", "/api/messages/unread-count", "GET"),
		p("message:detail", "消息详情", "message", "/api/messages/:id", "GET"),
		p("message:create", "发送消息", "message", "/api/messages/create", "POST"),
		p("message:mark-read", "标记已读", "message", "/api/messages/:id/read", "POST"),
		p("message:mark-all-read", "全部标记已读", "message", "/api/messages/read-all", "POST"),
		p("message:delete", "删除消息", "message", "/api/messages/:id/delete", "POST"),

		// === reminder ===
		p("reminder:templates:list", "提醒模板列表", "reminder", "/api/reminders/templates", "GET"),
		p("reminder:templates:create", "创建提醒模板", "reminder", "/api/reminders/templates", "POST"),
		p("reminder:templates:update", "更新提醒模板", "reminder", "/api/reminders/templates/:id", "POST"),
		p("reminder:templates:delete", "删除提醒模板", "reminder", "/api/reminders/templates/:id/delete", "POST"),
		p("reminder:subscriptions:list", "订阅列表", "reminder", "/api/reminders/subscriptions", "GET"),
		p("reminder:subscriptions:create", "创建订阅", "reminder", "/api/reminders/subscriptions", "POST"),
		p("reminder:subscriptions:update", "更新订阅", "reminder", "/api/reminders/subscriptions/:id", "POST"),
		p("reminder:subscriptions:delete", "删除订阅", "reminder", "/api/reminders/subscriptions/:id/delete", "POST"),
		p("reminder:logs", "提醒日志", "reminder", "/api/reminders/logs", "GET"),
		p("reminder:scan", "触发扫描", "reminder", "/api/reminders/scan", "POST"),

		// === scheduled ===
		p("scheduled:list", "定时任务列表", "scheduled", "/api/scheduled-tasks", "GET"),
		p("scheduled:handlers", "处理器列表", "scheduled", "/api/scheduled-tasks/handlers", "GET"),
		p("scheduled:next-run", "下次执行时间", "scheduled", "/api/scheduled-tasks/next-run", "GET"),
		p("scheduled:detail", "任务详情", "scheduled", "/api/scheduled-tasks/:id", "GET"),
		p("scheduled:create", "创建任务", "scheduled", "/api/scheduled-tasks", "POST"),
		p("scheduled:update", "更新任务", "scheduled", "/api/scheduled-tasks/:id/update", "POST"),
		p("scheduled:toggle", "启用/禁用", "scheduled", "/api/scheduled-tasks/:id/toggle", "POST"),
		p("scheduled:delete", "删除任务", "scheduled", "/api/scheduled-tasks/:id/delete", "POST"),
		p("scheduled:run-now", "立即执行", "scheduled", "/api/scheduled-tasks/:id/run-now", "POST"),
		p("scheduled:logs", "任务日志", "scheduled", "/api/scheduled-tasks/:id/logs", "GET"),
		p("scheduled:audits", "任务审计", "scheduled", "/api/scheduled-tasks/:id/audits", "GET"),

		// === system ===
		p("system:config:get", "查系统配置", "system", "/api/system/config", "GET"),
		p("system:config:set", "改系统配置", "system", "/api/system/config", "POST"),
		p("system:config-file:get", "查配置文件", "system", "/api/system/config-file", "GET"),
		p("system:config-file:set", "改配置文件", "system", "/api/system/config-file", "POST"),

		p("system:backup-now", "立即备份", "system", "/api/system/backup-now", "POST"),
		p("system:restore", "恢复备份", "system", "/api/system/restore", "POST"),
		p("system:maintenance:get", "查维护模式", "system", "/api/system/maintenance", "GET"),
		p("system:maintenance:set", "设维护模式", "system", "/api/system/maintenance", "POST"),
		p("system:shutdown", "关闭服务", "system", "/api/system/shutdown", "POST"),
		p("system:ssl-cert", "生成 SSL", "system", "/api/system/generate-ssl-cert", "POST"),

		// === backup ===
		p("backup:list", "备份列表", "backup", "/api/backup/list", "GET"),
		p("backup:detail", "备份详情", "backup", "/api/backup/:id", "GET"),
		p("backup:download", "下载备份", "backup", "/api/backup/:id/download", "GET"),
		p("backup:delete", "删除备份", "backup", "/api/backup/:id", "DELETE"),

		// === systemconfig ===
		p("systemconfig:public", "公开业务配置", "systemconfig", "/api/system-config/public", "GET"),
		p("systemconfig:list", "业务配置列表", "systemconfig", "/api/system-config/list", "GET"),
		p("systemconfig:update", "改业务配置", "systemconfig", "/api/system-config/update", "POST"),
		p("systemconfig:delete", "删业务配置", "systemconfig", "/api/system-config/:key/delete", "POST"),

		// === thirdparty ===
		p("thirdparty:list", "文档列表", "thirdparty", "/api/third-party/contracts", "GET"),
		p("thirdparty:create", "创建文档", "thirdparty", "/api/third-party/contracts", "POST"),
		p("thirdparty:detail", "文档详情", "thirdparty", "/api/third-party/contracts/:id", "GET"),
		p("thirdparty:update", "更新文档", "thirdparty", "/api/third-party/contracts/:id", "POST"),
		p("thirdparty:status", "改状态", "thirdparty", "/api/third-party/contracts/:id/status", "POST"),
		p("thirdparty:delete", "删除文档", "thirdparty", "/api/third-party/contracts/:id/delete", "POST"),
		p("thirdparty:upload", "上传文档", "thirdparty", "/api/third-party/contracts/:id/upload", "POST"),
		p("thirdparty:download", "下载文档", "thirdparty", "/api/third-party/contracts/:id/download", "GET"),
		p("thirdparty:bulk-download", "批量下载文档", "thirdparty", "/api/third-party/contracts/bulk-download", "POST"),

		// audit:list / audit:reconcile 权限码已下线
		// audit_log 表 + database.AppendAudit 仍保留（其它业务写审计用）

		// === media ===
		p("media:list", "媒体列表", "media", "/api/media", "GET"),
		p("media:by-target", "按目标查媒体", "media", "/api/media/by-target", "GET"),
		p("media:detail", "媒体详情", "media", "/api/media/:id", "GET"),
		p("media:create", "创建媒体", "media", "/api/media/create", "POST"),
		p("media:update", "更新媒体", "media", "/api/media/:id", "POST"),
		p("media:delete", "删除媒体", "media", "/api/media/:id/delete", "POST"),
		p("media:restore", "恢复媒体", "media", "/api/media/:id/restore", "POST"),
		p("media:tags", "媒体标签", "media", "/api/media/tags", "GET"),
		p("media:bulk-tag", "批量打标签", "media", "/api/media/bulk-tag", "POST"),
		p("media:bulk-delete", "批量删除", "media", "/api/media/bulk-delete", "POST"),
		p("media:bulk-customer", "批量绑定客户", "media", "/api/media/bulk-customer", "POST"),
		p("media:file", "下载文件", "media", "/api/media/:id/file", "GET"),
		p("media:thumb", "缩略图", "media", "/api/media/:id/thumb", "GET"),
		p("media:upload", "上传媒体", "media", "/api/media/upload", "POST"),
		p("media:check-hash", "检查哈希", "media", "/api/media/check-hash", "GET"),
		p("media:verify", "校验完整性", "media", "/api/media/:id/verify", "GET"),

		// === routes ===
		p("routes:async", "动态路由", "user", "/api/get-async-routes", "GET"),

		// === rbac 管理 API（PR-5 新增 7 个端点）===
		p("rbac:roles:list", "查询角色列表", "rbac", "/api/rbac/roles", "GET"),
		p("rbac:roles:upsert", "新建/更新角色", "rbac", "/api/rbac/roles", "POST"),
		p("rbac:roles:delete", "删除角色", "rbac", "/api/rbac/roles/:code", "DELETE"),
		p("rbac:permissions:list", "查询权限列表", "rbac", "/api/rbac/permissions", "GET"),
		p("rbac:permissions:upsert", "新建/更新权限", "rbac", "/api/rbac/permissions", "POST"),
		p("rbac:permissions:delete", "删除权限", "rbac", "/api/rbac/permissions/:code", "DELETE"),
		p("rbac:check", "权限检查", "rbac", "/api/rbac/check", "POST"),
		// permission-version 端点：所有已登录用户都需持有（前端轮询用）。
		p("permission:version:query", "查询权限版本号", "rbac", "/api/rbac/permission-version", "GET"),
	}
	return s
}

// helpers
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
