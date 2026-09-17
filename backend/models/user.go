package models

import "time"

type User struct {
	ID                int64     `json:"id"`
	Username          string    `json:"username"`
	Password          string    `json:"-"`
	Nickname          string    `json:"nickname"`
	Avatar            string    `json:"avatar"`
	Roles             []string  `json:"roles"`
	Permissions       []string  `json:"permissions"`
	RealName          string    `json:"real_name"`
	Email             string    `json:"email"`
	Phone             string    `json:"phone"`
	DepartmentID      *int64    `json:"department_id"`
	DepartmentName    string    `json:"department_name,omitempty"`
	Position          string    `json:"position"`
	EmployeeNo        string    `json:"employee_no"`
	Status            string    `json:"status"`
	FailedLoginCount  int       `json:"failed_login_count,omitempty"`
	LockedUntil       int64     `json:"locked_until,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Success bool       `json:"success"`
	Data    *UserToken `json:"data,omitempty"`
}

type UserToken struct {
	ID                int64    `json:"id"`
	Avatar            string   `json:"avatar"`
	Username          string   `json:"username"`
	Nickname          string   `json:"nickname"`
	RealName          string   `json:"real_name"`
	DepartmentID      *int64   `json:"department_id"`
	Position          string   `json:"position"`
	Roles             []string `json:"roles"`
	Permissions       []string `json:"permissions"`
	AccessToken       string   `json:"accessToken"`
	RefreshToken      string   `json:"refreshToken"`
	Expires           int64    `json:"expires"`
	PermissionVersion string   `json:"permission_version,omitempty"`
}

type RefreshTokenRequest struct {
	// 🛠 BUG-3 修复（2026-08-20）：JSON tag 改为 snake_case。
	//
	// 之前用驼峰 `refreshToken`：
	//   - 与项目其他 snake_case 字段（old_password / new_password / smoke_audit_fix.ps1
	//     中 refresh_token）不一致
	//   - 让 smoke 脚本（08-19 起就有）永远 400 "Invalid request body"
	//   - 任何按老 API 文档用蛇形的客户端必失败
	//
	// 修复：tag 改 `refresh_token`，与 utils/errors.go 的 CodeAuthRefreshExpired、
	// audit action "refresh.success/failed" 等命名风格统一。
	//
	// 注意：响应 UserToken.RefreshToken 仍输出 `refreshToken`（驼峰，前端
	// store 用 `data.refreshToken` 取），与请求体命名不对称是历史遗留，
	// 暂不破坏前端解析。
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type RefreshTokenResponse struct {
	Success bool       `json:"success"`
	Data    *TokenData `json:"data,omitempty"`
}

type TokenData struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Expires      int64  `json:"expires"`
}

type RouteNode struct {
	Path      string       `json:"path"`
	Name      string       `json:"name,omitempty"`
	Meta      *RouteMeta   `json:"meta,omitempty"`
	Children  []*RouteNode `json:"children,omitempty"`
	Component string       `json:"component,omitempty"`
}

type RouteMeta struct {
	Title string   `json:"title,omitempty"`
	Icon  string   `json:"icon,omitempty"`
	Rank  int      `json:"rank,omitempty"`
	Roles []string `json:"roles,omitempty"`
	Auths []string `json:"auths,omitempty"`
}

type RoutesResponse struct {
	Success bool         `json:"success"`
	Data    []*RouteNode `json:"data,omitempty"`
}
