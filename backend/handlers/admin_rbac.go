package handlers

import (
	"net/http"
	"sync"
	"time"

	"doc/database"
	"doc/middleware"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// ==================== 管理员权限统一校验（P1 修复 2026-06-14）====================
//
// 设计目标：
//   1. **统一入口**：所有"需要 admin 才能调"的 HTTP API 必须走 RequireAdmin(c)。
//      不再在 handler 内手写 `username == "admin"` 判断。
//   2. **可扩展**：未来引入 role/permission 字段时，只改本文件即可全部生效。
//   3. **可观测**：失败时统一打 ERROR 日志（username / path / IP），便于安全审计。
//   4. **可分支**：用 IsAdmin(c) 做条件分支（"admin 可看更多"），不强制退出。
//
// 二次审查（2026-06-14 21:30）覆盖：
//   - customer.go         9 处 → 全部用 RequireAdmin
//   - system_config.go    3 处 → 全部用 RequireAdmin
//   - audit.go            1 处 → 用 RequireAdmin
//   - seal.go             1 处（AssignSeal）→ 用 RequireAdmin
//   - hash_handler.go     3 处 → 删除局部 requireAdmin，统一用 RequireAdmin
//   - backup.go           1 处（Delete）→ 用 RequireAdmin
//   - media.go            5 处（isAdmin 条件分支）→ 用 IsAdmin
//   - seal.go             4 处（isAdmin 条件分支）→ 用 IsAdmin
//   - third_party.go      2 处（isAdmin 条件分支）→ 用 IsAdmin
//
// v0.2 安全加固（2026-06-14 22:00）：
//   - RequireAdmin 从 JWT-only 升级为 DB-backed（调 IsAdminUser）
//   - IsAdmin 保持 JWT-only（性能优先，用于分支）
//   - 用法决策树见下方注释
//
// ----------------------------------------------------------------------------

// RequireAdmin 强制 RBAC：admin 角色才能继续访问（gate，写 403 + ERROR 日志）。
//
// v0.2（二次审查 2026-06-14）：从 JWT-only 升级为 DB-backed。
//   - 调用 IsAdminUser 查 DB users.roles（不是只看 JWT username）
//   - 优点：admin 撤销角色后旧 JWT 立即失效
//   - 代价：每次 admin check 多 1 次 DB 查询（gate 场景，可接受）
//   - 替代方案：v1 把 roles 写进 JWT claims，本函数 0 DB 查询
//
// 用法决策树：
//   - 拦截 / gate / 删除 / 改密 → 用 RequireAdmin（必须安全）
//   - 列表过滤 / 显示控制 / 性能敏感分支 → 用 IsAdmin（JWT-only，性能优先）
//
// TODO（精细化 RBAC 路线图）：
//
//   v0.2（当前）：IsAdminUser 查 DB users.roles
//   v1（短期）：user 表加 role 枚举（admin/manager/user），JWT claims 加 role 字段
//              → IsAdmin 从 JWT 直接读，0 DB 查询
//   v2（中期）：permissions TEXT[] 细粒度权限码 → RequirePermission(c, "code")
//   v3（远期）：role_permissions 关系表 + UI 配置
func RequireAdmin(c *gin.Context) bool {
	if IsAdminUser(c) {
		return true
	}
	username := c.GetString("username")
	utils.LogError("[RBAC] 非 admin 用户尝试调用受限 API: username=%s, path=%s, ip=%s",
		username, c.Request.URL.Path, c.ClientIP())
	utils.Error(c, http.StatusForbidden, "仅管理员可调用此 API")
	return false
}

// IsAdmin JWT 快速检查（**不**查 DB），仅看 c.GetString("username") == "admin"。
// 用于 handler 内条件分支（"admin 可看全部"），不要用于权限拦截。
//
// 与 RequireAdmin 的关键区别（v0.2 重要）：
//   - IsAdmin → JWT-only，0 DB 查询，性能高；但**不安全**（admin 撤销后旧 JWT 仍有效）
//   - RequireAdmin → DB-backed，1 DB 查询；**安全**（角色变更立即生效）
//
// 用法决策树：
//   - 拦截 / gate / 删除 / 改密 → 用 RequireAdmin（必须安全）
//   - 列表过滤 / 显示控制 / 性能敏感 → 用 IsAdmin（接受不立即生效）
//   - 等 v1（JWT 携带 roles）后两者都 0 DB 查询 + 立即生效
//
// 2026-06-25 P2-8.1：与 IsRBACAdmin 完全等价（都是 username == "admin"）。
// 语义上：IsAdmin 是更通用的 JWT 快速检查，IsRBACAdmin 是 RBAC 语境下的别名。
// 新代码请用 IsAdmin；IsRBACAdmin 保留为 deprecated。
func IsAdmin(c *gin.Context) bool {
	return c.GetString("username") == "admin"
}

// ==================== RBAC v2：细粒度权限码（permission_code）====================
//
// 2026-06 新增：基于 role + permission 两表 + permission_code 通配符的 gate。
// 设计要点：
//   1. effective permissions = users.permissions ∪ Σ(role.permissions)
//                            ∩ permission.status='active'
//   2. 通配符 "*:*:*" 直接命中所有权限（admin 角色专用）
//   3. 进程内 30s 缓存（per-user）+ 60s 缓存（API → permission_codes）
//   4. 角色/权限/user.roles 变更时调用 InvalidateAllPermsCache() 清缓存

type cachedPerms struct {
	perms     []string
	expiresAt time.Time
}

type cachedAPIPerms struct {
	codes     []string
	expiresAt time.Time
}

var (
	permsCache sync.Map
	// permsCacheTTL 缓存有效期。
	// 2026-06-28 同步调整：与 dataScopeCache 一致延长到 5min。
	// 主动失效已覆盖所有已知路径（InvalidateAllPermsCache + InvalidatePermsCache(username)）。
	permsCacheTTL    = 5 * time.Minute
	apiPermsCache    sync.Map
	apiPermsCacheTTL = 5 * time.Minute
)

// GetEffectivePermissionsCached 读有效权限码（带 30s 缓存）
func GetEffectivePermissionsCached(username string) ([]string, error) {
	if v, ok := permsCache.Load(username); ok {
		c := v.(*cachedPerms)
		if time.Now().Before(c.expiresAt) {
			return c.perms, nil
		}
	}
	user, err := database.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	perms, err := database.GetEffectivePermissionsForUser(user.ID)
	if err != nil {
		return nil, err
	}
	permsCache.Store(username, &cachedPerms{
		perms:     perms,
		expiresAt: time.Now().Add(permsCacheTTL),
	})
	return perms, nil
}

// GetPermissionCodesForAPICached API → permission_code 反查（带 60s 缓存）
// 替代直接调 database.GetPermissionCodesForAPI：每次请求都扫 178 行做 path 匹配会很慢
func GetPermissionCodesForAPICached(method, realPath string) ([]string, error) {
	key := method + " " + realPath
	if v, ok := apiPermsCache.Load(key); ok {
		c := v.(*cachedAPIPerms)
		if time.Now().Before(c.expiresAt) {
			return c.codes, nil
		}
	}
	codes, err := database.GetPermissionCodesForAPI(method, realPath)
	if err != nil {
		return nil, err
	}
	apiPermsCache.Store(key, &cachedAPIPerms{
		codes:     codes,
		expiresAt: time.Now().Add(apiPermsCacheTTL),
	})
	return codes, nil
}

// InvalidatePermsCache 清指定用户的缓存
func InvalidatePermsCache(username string) {
	permsCache.Delete(username)
}

// InvalidateAllPermsCache 清全部缓存（角色/权限变更后调用）
func InvalidateAllPermsCache() {
	permsCache.Range(func(k, _ interface{}) bool {
		permsCache.Delete(k)
		return true
	})
	apiPermsCache.Range(func(k, _ interface{}) bool {
		apiPermsCache.Delete(k)
		return true
	})
	// 2026-06-28 RBAC v3 P2：联动清 data_scope 缓存。
	// role.data_scope 变更会影响所有持有该 role 的用户，必须同步清。
	middleware.InvalidateAllDataScopeCache()
}

// IsRBACAdmin 提取 admin 旁路判定（hardcoded by username）
//
// 2026-06-25 P2-8.1 修复：与 IsAdmin 完全等价，合并到 IsAdmin。
// 保留此函数作为 deprecated 别名，调用点暂不强制迁移（向后兼容）。
// 新代码请直接用 IsAdmin。
func IsRBACAdmin(c *gin.Context) bool {
	return c.GetString("username") == "admin"
}

// HasPermission 单个权限码检查（对齐前端 hasPerms）
func HasPermission(c *gin.Context, requiredCode string) bool {
	username := c.GetString("username")
	if username == "" {
		return false
	}
	if IsRBACAdmin(c) {
		return true
	}
	perms, err := GetEffectivePermissionsCached(username)
	if err != nil {
		return false
	}
	for _, p := range perms {
		if p == "*:*:*" {
			return true
		}
		if p == requiredCode {
			return true
		}
	}
	return false
}

// HasAnyPermission 多权限码 OR 检查
func HasAnyPermission(c *gin.Context, codes ...string) bool {
	if IsRBACAdmin(c) {
		return true
	}
	username := c.GetString("username")
	if username == "" {
		return false
	}
	perms, err := GetEffectivePermissionsCached(username)
	if err != nil {
		return false
	}
	for _, p := range perms {
		if p == "*:*:*" {
			return true
		}
	}
	for _, need := range codes {
		for _, have := range perms {
			if have == need {
				return true
			}
		}
	}
	return false
}

// RequirePermission 细粒度 gate（写 403 + ERROR 日志）
func RequirePermission(c *gin.Context, requiredCode string) bool {
	if HasPermission(c, requiredCode) {
		return true
	}
	username := c.GetString("username")
	utils.LogError("[RBAC] 权限不足: username=%s, need=%s, path=%s, ip=%s",
		username, requiredCode, c.Request.URL.Path, c.ClientIP())
	name := requiredCode
	if p, _ := database.GetPermissionByCode(requiredCode); p != nil {
		name = p.Name
	}
	utils.Error(c, http.StatusForbidden, "权限不足: 需要 "+name)
	return false
}

// RequireAnyPermission 多权限码 OR（任意一个命中即通过）
func RequireAnyPermission(c *gin.Context, codes ...string) bool {
	if HasAnyPermission(c, codes...) {
		return true
	}
	utils.LogError("[RBAC] 权限不足: username=%s, need_any=%v, path=%s",
		c.GetString("username"), codes, c.Request.URL.Path)
	utils.Error(c, http.StatusForbidden, "权限不足")
	return false
}

// APIGateMiddleware Gin 中间件：对每个受保护请求做 API 级权限 gate
// 用法：protected.Use(middleware.JWTAuth(jwt)); protected.Use(handlers.APIGateMiddleware())
func APIGateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.GetString("username")
		if username == "" {
			c.Next()
			return
		}
		if IsRBACAdmin(c) {
			c.Next()
			return
		}

		method := c.Request.Method
		path := c.Request.URL.Path
		requiredCodes, err := GetPermissionCodesForAPICached(method, path)
		if err != nil || len(requiredCodes) == 0 {
			// 没配置 = 不限制（向后兼容）
			c.Next()
			return
		}

		if HasAnyPermission(c, requiredCodes...) {
			c.Next()
			return
		}

		utils.LogError("[RBAC] API gate 拒绝: username=%s, method=%s, path=%s, need_any=%v",
			username, method, path, requiredCodes)
		utils.Error(c, http.StatusForbidden, "权限不足")
		c.Abort()
	}
}
