package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"doc/database"
	"doc/models"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	jwtUtils *utils.JWTUtils
}

func NewAuthHandler(jwtUtils *utils.JWTUtils) *AuthHandler {
	return &AuthHandler{jwtUtils: jwtUtils}
}

func (h *AuthHandler) Login(c *gin.Context) {
	// P0 修复（2026-06-28）：登录结果上报给 LoginRateLimit 中间件。
	// defer 在 handler 返回前运行（早于 middleware 的 c.Next() 之后逻辑），
	// 根据 HTTP status 决定 success / failure。
	defer func() {
		if c.Writer.Status() == http.StatusOK {
			c.Set("__ratelimit_result", "success")
		} else {
			c.Set("__ratelimit_result", "failure")
		}
		// Round 16 业务事件埋点：登录成功/失败（低风险稳定事件，按 HTTP 状态分流）。
		if c.Writer.Status() == http.StatusOK {
			utils.IncBusinessEvent("auth.login.success")
		} else {
			utils.IncBusinessEvent("auth.login.failed")
		}
	}()

	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Invalid request body")
		return
	}

	user, err := database.GetUserByUsername(req.Username)
	if err != nil {
		utils.Info("Login failed: user not found - %s\n", req.Username)
		// 审计：登录失败（用户不存在，actor_user_id=0；target_id=0）。
		database.RecordAuditBy(c, 0, database.AuditTargetAuth, 0, "login.failed", gin.H{
			"username": req.Username,
			"reason":   "user_not_found",
		})
		utils.Unauthorized(c, "用户名或密码错误")
		return
	}

	if !utils.CheckPassword(req.Password, user.PasswordHash) {
		utils.Info("Login failed: wrong password - %s\n", req.Username)
		// 审计：登录失败（密码错误；actor 与 target 都是 user.ID）。
		database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.failed", gin.H{
			"username": req.Username,
			"reason":   "wrong_password",
		})
		utils.Unauthorized(c, "用户名或密码错误")
		return
	}

	// P0 修复（2026-06-28）：登录校验 user.Status == 'active'。
	// 之前只校验密码，禁用账号（status='disabled'）仍可登录获取 24h JWT。
	if user.Status != "active" {
		utils.LogError("[Auth.Login] 用户已禁用仍尝试登录: username=%s, status=%s, ip=%s",
			user.Username, user.Status, c.ClientIP())
		// 审计：登录失败（账号禁用）。
		database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.failed", gin.H{
			"username": req.Username,
			"reason":   "account_disabled",
			"status":   user.Status,
		})
		utils.Unauthorized(c, "账号已被禁用，请联系管理员")
		return
	}

	accessToken, err := h.jwtUtils.GenerateAccessToken(user.ID, user.Username)
	if err != nil {
		utils.Info("Failed to generate access token: %v\n", err)
		utils.Error(c, 500, "Failed to generate token")
		return
	}

	refreshToken, err := h.jwtUtils.GenerateRefreshToken(user.ID, user.Username)
	if err != nil {
		utils.Info("Failed to generate refresh token: %v\n", err)
		utils.Error(c, 500, "Failed to generate token")
		return
	}

	expiresTime := h.jwtUtils.GetExpirationTime()
	expires := expiresTime.UnixNano() / 1000000

	userAPI := user.ToAPIModel()
	effectivePerms, _ := database.GetEffectivePermissionsForUser(user.ID)
	if effectivePerms == nil {
		effectivePerms = []string{}
	}
	response := models.UserToken{
		Avatar:            userAPI.Avatar,
		Username:          userAPI.Username,
		Nickname:          userAPI.Nickname,
		Roles:             userAPI.Roles,
		Permissions:       effectivePerms,
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		Expires:           expires,
		PermissionVersion: getPermissionVersion(),
	}

	utils.Info("User logged in: %s\n", user.Username)
	// 审计：登录成功。/login 不走 JWTAuth 中间件，c.Get("user_id") 取不到 → 用 RecordAuditBy 显式传 actor_user_id。
	database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.success", gin.H{
		"username": user.Username,
	})
	utils.Success(c, response)
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	// P1 修复（2026-06-28）：上报 refresh-token 结果给 RefreshTokenRateLimit 中间件。
	defer func() {
		if c.Writer.Status() == http.StatusOK {
			c.Set("__ratelimit_result", "success")
		} else {
			c.Set("__ratelimit_result", "failure")
		}
		// Round 16 业务事件埋点：refresh 成功/失败（按 HTTP 状态分流，覆盖所有 return 路径）。
		if c.Writer.Status() == http.StatusOK {
			utils.IncBusinessEvent("auth.refresh.success")
		} else {
			utils.IncBusinessEvent("auth.refresh.failed")
		}
	}()

	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Invalid request body")
		return
	}

	claims, err := h.jwtUtils.ValidateToken(req.RefreshToken)
	if err != nil {
		utils.Info("Refresh token validation failed: %v\n", err)
		utils.Unauthorized(c)
		return
	}

	if claims.TokenType != "refresh" {
		utils.Info("Invalid token type for refresh")
		utils.Unauthorized(c)
		return
	}

	accessToken, err := h.jwtUtils.GenerateAccessToken(claims.UserID, claims.Username)
	if err != nil {
		utils.Info("Failed to generate access token: %v\n", err)
		utils.Error(c, 500, "Failed to generate token")
		return
	}

	refreshToken, err := h.jwtUtils.GenerateRefreshToken(claims.UserID, claims.Username)
	if err != nil {
		utils.Info("Failed to generate refresh token: %v\n", err)
		utils.Error(c, 500, "Failed to generate token")
		return
	}

	expiresTime := h.jwtUtils.GetExpirationTime()
	expires := expiresTime.UnixNano() / 1000000

	response := models.TokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expires:      expires,
	}

	utils.Info("Token refreshed for user: %s\n", claims.Username)
	utils.Success(c, response)
}

func (h *AuthHandler) GetUserInfo(c *gin.Context) {
	userID := c.GetInt64("user_id")
	username := c.GetString("username")

	user, err := database.GetUserByUsername(username)
	if err != nil {
		utils.Unauthorized(c)
		return
	}

	if user.ID != userID {
		utils.Unauthorized(c)
		return
	}

	expiresTime := h.jwtUtils.GetExpirationTime()
	expires := expiresTime.UnixNano() / 1000000

	userAPI := user.ToAPIModel()
	effectivePerms, _ := database.GetEffectivePermissionsForUser(user.ID)
	if effectivePerms == nil {
		effectivePerms = []string{}
	}
	response := models.UserToken{
		Avatar:            userAPI.Avatar,
		Username:          userAPI.Username,
		Nickname:          userAPI.Nickname,
		Roles:             userAPI.Roles,
		Permissions:       effectivePerms,
		Expires:           expires,
		PermissionVersion: getPermissionVersion(),
	}

	utils.Success(c, response)
}

// Logout 登出（P0 修复 2026-06-28：吊销当前 access token）。
//
//   - 历史实现只返回成功消息，token 本体未失效（24h 内仍可复用）
//   - 现在从 Authorization 头提取 token，加入进程内黑名单
//   - 同时尝试吊销 cookie / header 中可能存在的 refresh token（如果有）
//   - 客户端应清 localStorage（前端 userStore.logOut 已实现）
func (h *AuthHandler) Logout(c *gin.Context) {
	// 1. 吊销当前 access token
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		// 黑名单 TTL = access token 剩余有效期
		utils.RevokeToken(token, time.Now().Add(h.jwtUtils.GetAccessExpire()).Unix())
		utils.Info("[Auth.Logout] access token 已加入黑名单: user=%s, ip=%s",
			c.GetString("username"), c.ClientIP())
	}

	// 2. 兼容旧版：若 body 含 refresh_token 也吊销
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req) // body 可选
	if req.RefreshToken != "" {
		utils.RevokeToken(req.RefreshToken, time.Now().Add(h.jwtUtils.GetRefreshExpire()).Unix())
	}

	utils.Success(c, gin.H{
		"message": "Logout successful",
	})
}

// ChangePassword 修改当前登录用户的密码。
//   - 必须已登录（中间件校验）
//   - 必须传入旧密码并通过校验
//   - 新密码至少 6 位
//   - 新旧密码不能相同
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	// P0 修复（2026-06-28）：改密结果上报给 ChangePasswordRateLimit 中间件。
	// 与 Login 同样的 defer 模式：200 → success，其它 → failure。
	defer func() {
		if c.Writer.Status() == http.StatusOK {
			c.Set("__ratelimit_result", "success")
		} else {
			c.Set("__ratelimit_result", "failure")
		}
		// Round 16 业务事件埋点：修改密码成功/失败（按 HTTP 状态分流，覆盖所有 return 路径）。
		if c.Writer.Status() == http.StatusOK {
			utils.IncBusinessEvent("auth.password.change.success")
		} else {
			utils.IncBusinessEvent("auth.password.change.failed")
		}
	}()

	userID := c.GetInt64("user_id")
	username := c.GetString("username")

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 1. 强度校验
	if err := utils.ValidatePasswordStrength(req.NewPassword); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 2. 新旧密码不能相同
	if req.OldPassword == req.NewPassword {
		utils.BadRequest(c, "新密码不能与旧密码相同")
		return
	}

	// 3. 校验旧密码
	user, err := database.GetUserByUsername(username)
	if err != nil {
		utils.Unauthorized(c)
		return
	}
	if user.ID != userID {
		utils.Unauthorized(c)
		return
	}
	if !utils.CheckPassword(req.OldPassword, user.PasswordHash) {
		utils.BadRequest(c, "旧密码错误")
		return
	}

	// 4. 更新密码
	if err := database.UpdateUserPassword(userID, req.NewPassword); err != nil {
		utils.Error(c, http.StatusInternalServerError, "更新密码失败: "+err.Error())
		return
	}

	utils.Info("Password changed for user: %s\n", username)
	utils.Success(c, gin.H{"message": "密码修改成功"})
}

func (h *AuthHandler) Register(c *gin.Context) {
	// 2026-07-04：注册端点暂不开放（公网部署防滥用 + 后台手工建账号更可控）
	utils.Error(c, http.StatusForbidden, "注册功能暂未开放，请联系管理员创建账号")
}

func IsAdminUser(c *gin.Context) bool {
	username := c.GetString("username")
	if username == "" {
		return false
	}

	user, err := database.GetUserByUsername(username)
	if err != nil {
		return false
	}

	var roles []string
	if err := json.Unmarshal([]byte(user.Roles), &roles); err != nil {
		return false
	}

	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}
