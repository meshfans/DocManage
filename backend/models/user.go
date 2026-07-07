package models

import "time"

type User struct {
	ID             int64     `json:"id"`
	Username       string    `json:"username"`
	Password       string    `json:"-"`
	Nickname       string    `json:"nickname"`
	Avatar         string    `json:"avatar"`
	Roles          []string  `json:"roles"`
	Permissions    []string  `json:"permissions"`
	RealName       string    `json:"real_name"`
	Email          string    `json:"email"`
	Phone          string    `json:"phone"`
	DepartmentID   *int64    `json:"department_id"`
	DepartmentName string    `json:"department_name,omitempty"`
	Position       string    `json:"position"`
	EmployeeNo     string    `json:"employee_no"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	RefreshToken string `json:"refreshToken" binding:"required"`
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
