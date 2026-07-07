package handlers

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"doc/database"
	"doc/models"
	"doc/utils"
)

type RoutesHandler struct{}

func NewRoutesHandler() *RoutesHandler {
	return &RoutesHandler{}
}

// GetAsyncRoutes GET /api/get-async-routes
// PR-9：根据用户 effective_permissions 动态构建菜单树
// 替代前端硬编码的 meta.roles 过滤逻辑
func (h *RoutesHandler) GetAsyncRoutes(c *gin.Context) {
	userID := c.GetInt64("user_id")
	username := c.GetString("username")

	user, err := database.GetUserByUsername(username)
	if err != nil || user.ID != userID {
		utils.Unauthorized(c)
		return
	}

	// admin 旁路
	isAdmin := IsRBACAdmin(c)

	// 取有效权限（带 30s 缓存）
	perms, _ := GetEffectivePermissionsCached(username)

	// 构建菜单树
	menuTree := database.BuildMenuTree(perms, isAdmin)

	// 转为前端 vue-router 期望的 RouteNode 格式
	routes := convertMenuToRoutes(menuTree)

	// 返回时附带 permission_version，前端用此判断是否需要重新拉取
	utils.Success(c, gin.H{
		"list": routes,
		"permission_version": getPermissionVersion(),
	})
}

// GetPermissionVersion GET /api/rbac/permission-version
// 轻量级版本号查询（前端用于判断是否需要重新拉取菜单）
// 5s 缓存：1000 用户 × 12 次/分钟 = 12000 QPS → 加缓存后 12 QPS
//
// 2026-06-29 RBAC v3 P2 重构（handover §11 C4 跳过项）：加 sync.RWMutex 保护并发读写。
// 此前 permVerCache / permVerCacheExp 是裸全局变量，并发请求下 go race detector 会报数据竞争。
// 用 RWMutex 替代 sync.Mutex：getPermissionVersionCached 是高频读路径，写仅发生在 5s TTL 到期。
var (
	permVerCacheMu  sync.RWMutex
	permVerCache    string
	permVerCacheExp int64
	permVerCacheTTL = int64(5) // 5s
)

func getPermissionVersionCached() string {
	now := time.Now().Unix()

	// 快速路径：读锁（高频）
	permVerCacheMu.RLock()
	if permVerCache != "" && now < permVerCacheExp {
		v := permVerCache
		permVerCacheMu.RUnlock()
		return v
	}
	permVerCacheMu.RUnlock()

	// 慢速路径：TTL 到期或首次访问，查 DB 后写锁刷新
	v := getPermissionVersion()
	permVerCacheMu.Lock()
	// 双重检查：避免多个 goroutine 同时查 DB 后重复写
	if permVerCache != "" && now < permVerCacheExp {
		existing := permVerCache
		permVerCacheMu.Unlock()
		return existing
	}
	permVerCache = v
	permVerCacheExp = now + permVerCacheTTL
	permVerCacheMu.Unlock()
	return v
}

func (h *RoutesHandler) GetPermissionVersion(c *gin.Context) {
	utils.Success(c, gin.H{"version": getPermissionVersionCached()})
}

// InvalidatePermissionVersionCache 清空 permVersion 缓存
// 在 role/permission 写操作后调用（让前端轮询能立即看到版本变化）
func InvalidatePermissionVersionCache() {
	permVerCacheMu.Lock()
	permVerCache = ""
	permVerCacheExp = 0
	permVerCacheMu.Unlock()
}

// convertMenuToRoutes 把 MenuItem 树转成前端 RouteNode 树
func convertMenuToRoutes(items []*database.MenuItem) []*models.RouteNode {
	out := make([]*models.RouteNode, 0, len(items))
	for _, item := range items {
		node := &models.RouteNode{
			Path: item.Path,
			Name: item.Name,
			Meta: &models.RouteMeta{
				Title: item.Title,
				Icon:  item.Icon,
				Rank:  item.Rank,
			},
		}
		if item.Component != "" {
			node.Component = item.Component
		}
		if len(item.Children) > 0 {
			node.Children = convertMenuToRoutes(item.Children)
		}
		out = append(out, node)
	}
	return out
}

// getPermissionVersion 计算当前 permission/role 的版本号
// 实现：基于 permission 表 count + role 表 max(updated_at) + 当前 unix 时间戳
// 当 admin 改 role/permission 时会同步失效 cache，version 也会变
func getPermissionVersion() string {
	// 取 role 表最新 updated_at + permission 数
	var maxUpdated int64
	row := database.DB.QueryRow("SELECT COALESCE(MAX(updated_at), 0) FROM role")
	_ = row.Scan(&maxUpdated)
	var permCount int
	row2 := database.DB.QueryRow("SELECT COUNT(*) FROM permission")
	_ = row2.Scan(&permCount)
	return formatVersion(maxUpdated, permCount)
}

func formatVersion(maxUpdated int64, permCount int) string {
	// 简化版：maxUpdated-permCount
	return time.Unix(maxUpdated, 0).Format("20060102150405") + "-" + itoa(permCount)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}