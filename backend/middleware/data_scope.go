package middleware

import (
	"strconv"
	"sync"
	"time"

	"doc/database"
	"doc/utils"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

// 本文件：RBAC v3 数据级权限的中间件。
// 2026-06-28 P2：把 PoC 阶段的 handler 内联 LoadUserDataScope 提到中间件层，
// 30s 进程内缓存（与 permsCache 同模式），handler 通过 GetDataScope(c) 拿。
//
// 设计要点：
//   1. 单写多读：DataScopeMiddleware 一次性加载并 c.Set("data_scope", *scope)
//   2. 缓存粒度：per-user（permsCache 是同模式），key = userID
//   3. 缓存值：完整的 *database.UserDataScope（含 SubDeptIDs）
//   4. 失效：role/permission/user.roles 变更时调 InvalidateDataScopeCache(userID)
//
// 为什么不放在 c.GetInt64("user_id") 之后立刻 c.Get("data_scope")：
//   业务 handler 经常要用 scope 拼 WHERE，希望直接拿对象而不是字符串。
//   走 c.Get 拿 interface{} 需断言，不如直接一个类型化函数 GetDataScope(c)。

// cachedDataScope 单条缓存项。
type cachedDataScope struct {
	scope     *database.UserDataScope
	expiresAt time.Time
}

var (
	dataScopeCache sync.Map
	// dataScopeCacheTTL 缓存有效期。
	// 2026-06-28 调整：从 30s 延长到 5min。理由：
	//   - 主动失效已覆盖所有已知变更路径（role / user / dept 写操作触发 invalidate）
	//   - 30s TTL 主要是"防御性兜底"，但代价是 200 用户下每秒 13-33 次无意义 DB 查询
	//   - 5min 是业务可接受的"理论最坏延迟"（紧急场景用 JWT 黑名单 + maintenance_mode）
	//   - 与 permsCache / apiPermsCache（30s / 60s）保持类似量级
	dataScopeCacheTTL = 5 * time.Minute
	// loadScopeGroup 合并同 userID 的并发 miss 请求为 1 次 DB 查询。
	// 2026-06-28 B4 修复：防止缓存过期瞬间 N 个并发请求触发 N 次 DB 查询（thundering herd）。
	// 即使 TTL 延长到 5min，缓存过期瞬间仍可能有并发请求（如前端轮询 + 用户操作同时）。
	loadScopeGroup singleflight.Group
)

// DataScopeMiddleware 数据级权限中间件。
// 必须在 JWTAuth 之后挂载（依赖 c.GetInt64("user_id") 和 c.GetString("username")）。
// 加载失败时：warn + 注入 nil scope（handler 内 fallback 到 self）。
func DataScopeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		username := c.GetString("username")
		if userID == 0 {
			c.Next()
			return
		}
		scope, err := loadDataScopeCached(userID, username)
		if err != nil {
			utils.Warn("[data_scope] 加载 user=%d scope 失败: %v", userID, err)
			// 注入 nil，handler 内走 fallback
			c.Set("data_scope", (*database.UserDataScope)(nil))
			c.Next()
			return
		}
		c.Set("data_scope", scope)
		c.Next()
	}
}

// loadDataScopeCached 读缓存，miss 时查 DB 并写回。
// 缓存 key 用 userID 而不是 username：username 可能改名但 ID 不会变。
//
// 2026-06-28 B4 修复：用 singleflight 合并同 userID 的并发 miss 请求。
// 场景：缓存过期瞬间，N 个并发请求都触发 miss → singleflight 保证只有 1 个真正查 DB，
// 其他 N-1 个共享结果。避免 DB 瞬时压力尖刺。
func loadDataScopeCached(userID int64, username string) (*database.UserDataScope, error) {
	if v, ok := dataScopeCache.Load(userID); ok {
		c := v.(*cachedDataScope)
		if time.Now().Before(c.expiresAt) {
			return c.scope, nil
		}
	}
	key := strconv.FormatInt(userID, 10)
	v, err, _ := loadScopeGroup.Do(key, func() (interface{}, error) {
		// 双重检查：并发场景下，前一个 singleflight 调用可能已写回缓存
		if v2, ok := dataScopeCache.Load(userID); ok {
			c := v2.(*cachedDataScope)
			if time.Now().Before(c.expiresAt) {
				return c.scope, nil
			}
		}
		scope, err := database.LoadUserDataScope(userID, username)
		if err != nil {
			return nil, err
		}
		dataScopeCache.Store(userID, &cachedDataScope{
			scope:     scope,
			expiresAt: time.Now().Add(dataScopeCacheTTL),
		})
		return scope, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*database.UserDataScope), nil
}

// GetDataScope 从 context 拿 data_scope，未注入时返回 nil。
// 失败 fallback：handler 可用 `scope == nil` 走 self 过滤。
func GetDataScope(c *gin.Context) *database.UserDataScope {
	v, ok := c.Get("data_scope")
	if !ok {
		return nil
	}
	return v.(*database.UserDataScope)
}

// InvalidateDataScopeCache 清指定用户的 data_scope 缓存。
// 调用时机：
//   - admin 改 users.department_id / users.roles（直接用 userID）
//   - admin 改 department.parent_id（用 InvalidateDataScopeCacheByDeptSubtree）
//   - admin 改 role.data_scope（用 InvalidateDataScopeCacheByRole）
func InvalidateDataScopeCache(userID int64) {
	dataScopeCache.Delete(userID)
}

// InvalidateDataScopeCacheByRole 2026-06-29 RBAC v3 P5：精准失效某 role 持有者的缓存。
// 替代 InvalidateAllDataScopeCache 用于 role.data_scope / role.permissions 变更场景。
// 200 用户 × 5min TTL × N role 变更：减少 N×200 次无意义 DB 查询。
func InvalidateDataScopeCacheByRole(roleCode string) {
	if roleCode == "" {
		return
	}
	userIDs, err := database.GetUserIDsByRole(roleCode)
	if err != nil {
		utils.Warn("[data_scope] 查 role=%s 用户失败: %v，回退全清", roleCode, err)
		InvalidateAllDataScopeCache()
		return
	}
	for _, uid := range userIDs {
		dataScopeCache.Delete(uid)
	}
	if len(userIDs) > 0 {
		utils.Info("[data_scope] role=%s 缓存失效：%d 个用户", roleCode, len(userIDs))
	}
}

// InvalidateDataScopeCacheByDeptSubtree 2026-06-29 RBAC v3 P5：精准失效某部门子树用户的缓存。
// 替代 InvalidateAllDataScopeCache 用于 department.parent_id / department.status 变更场景。
// 单查询（CTE）获取子树 + 用户，避免 200 用户的全量失效。
func InvalidateDataScopeCacheByDeptSubtree(rootDeptID int64) {
	if rootDeptID <= 0 {
		InvalidateAllDataScopeCache()
		return
	}
	userIDs, err := database.GetUserIDsByDepartmentSubtree(rootDeptID)
	if err != nil {
		utils.Warn("[data_scope] 查 dept=%d 子树用户失败: %v，回退全清", rootDeptID, err)
		InvalidateAllDataScopeCache()
		return
	}
	for _, uid := range userIDs {
		dataScopeCache.Delete(uid)
	}
	if len(userIDs) > 0 {
		utils.Info("[data_scope] dept=%d 子树缓存失效：%d 个用户", rootDeptID, len(userIDs))
	}
}

// InvalidateAllDataScopeCache 清全部缓存（兜底 + 旧调用方）。
// 2026-06-28 P2：和 InvalidateAllPermsCache 配套使用。
// 2026-06-29 P5：精细化失效（ByRole / ByDeptSubtree）已覆盖大部分场景，本函数仅作兜底。
func InvalidateAllDataScopeCache() {
	dataScopeCache.Range(func(k, v interface{}) bool {
		dataScopeCache.Delete(k)
		return true
	})
}
