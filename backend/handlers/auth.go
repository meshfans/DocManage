package handlers

import (
	"net/http"
	"strings"
	"time"

	"doc/database"
	"doc/models"
	"doc/services"
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
			services.PublishEvent("auth.login.success")
		} else {
			services.PublishEvent("auth.login.failed")
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
		utils.Err(c, utils.CodeAuthInvalidCredentials, "用户名或密码错误")
		return
	}

	// J.6：登录前校验临时锁定（防 brute force）。
	if user.LockedUntil > time.Now().Unix() {
		utils.LogError("[Auth.Login] 用户被临时锁定: username=%s, locked_until=%d, ip=%s",
			user.Username, user.LockedUntil, c.ClientIP())
		database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.failed", gin.H{
			"username":     req.Username,
			"reason":       "account_locked",
			"locked_until": user.LockedUntil,
		})
		// K.6：埋业务事件（供 Prometheus 告警检测 brute force）
		services.PublishEvent("auth.login.locked")
		// L.4：返回 locked_until（unix 秒），前端可显示"账号 X 分钟后解锁"。
		utils.ErrWithExtras(c, utils.CodeAuthUserDisabled,
			"账号因登录失败次数过多被临时锁定，请稍后再试",
			gin.H{"locked_until": user.LockedUntil})
		return
	}

	if !utils.CheckPassword(req.Password, user.PasswordHash) {
		utils.Info("Login failed: wrong password - %s\n", req.Username)
		// 审计：登录失败（密码错误；actor 与 target 都是 user.ID）。
		database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.failed", gin.H{
			"username": req.Username,
			"reason":   "wrong_password",
		})
		// J.6：失败计数 + 触发锁定
		count, _, incErr := database.IncrementFailedLogin(user.ID)
		if incErr != nil {
			utils.Warn("[Auth.Login] IncrementFailedLogin 失败: user_id=%d, err=%v", user.ID, incErr)
		} else if count >= database.LoginMaxAttempts {
			if lockErr := database.LockUser(user.ID); lockErr != nil {
				utils.Warn("[Auth.Login] LockUser 失败: user_id=%d, err=%v", user.ID, lockErr)
			} else {
				utils.LogError("[Auth.Login] 用户触发锁定: username=%s, count=%d", user.Username, count)
				database.RecordAuditBy(c, user.ID, database.AuditTargetAuth, user.ID, "login.locked", gin.H{
					"username":      req.Username,
					"failed_count":  count,
					"locked_minutes": database.LoginLockMinutes,
				})
				// K.6：埋业务事件
				services.PublishEvent("auth.login.locked")
			}
		}
		// L.4：返回 remaining_attempts（含本次失败后剩余次数，>=0）；
		// 触发锁定时同步返回 locked_until（本次 LockUser 写入的截止时间）。
		extras := gin.H{}
		if incErr == nil {
			remaining := database.LoginMaxAttempts - count
			if remaining < 0 {
				remaining = 0
			}
			extras["remaining_attempts"] = remaining
			if count >= database.LoginMaxAttempts {
				// LockUser 已执行（lockErr == nil 路径），locked_until = now + LoginLockMinutes。
				// 不直接读 DB 避免 extra 查询；与 LockUser 内部计算保持一致。
				extras["locked_until"] = time.Now().Add(time.Duration(database.LoginLockMinutes) * time.Minute).Unix()
			}
		}
		utils.ErrWithExtras(c, utils.CodeAuthInvalidCredentials, "用户名或密码错误", extras)
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
		utils.Err(c, utils.CodeAuthUserDisabled, "账号已被禁用，请联系管理员")
		return
	}

	// J.6：登录成功 → 清零失败计数与锁定（兜底，前面 LockedUntil 已校验过）。
	if user.FailedLoginCount > 0 || user.LockedUntil > 0 {
		if resetErr := database.ResetFailedLogin(user.ID); resetErr != nil {
			utils.Warn("[Auth.Login] ResetFailedLogin 失败: user_id=%d, err=%v", user.ID, resetErr)
		} else {
			// K.6：埋业务事件，区分"无失败计数重置" vs "从锁定恢复"。
			// event label 不同便于监控告警（如"短期内大量 unlock"可能是误锁定激增）。
			if user.LockedUntil > 0 {
				services.PublishEvent("auth.login.unlocked")
				utils.Info("[Auth.Login] 用户从锁定恢复: username=%s", user.Username)
			} else {
				services.PublishEvent("auth.login.reset_counter")
			}
		}
	}

	accessToken, accessExpires, err := h.jwtUtils.GenerateAccessToken(user.ID, user.Username)
	if err != nil {
		utils.Info("Failed to generate access token: %v\n", err)
		utils.Err(c, utils.CodeInternal, "Failed to generate token")
		return
	}

	refreshToken, _, err := h.jwtUtils.GenerateRefreshToken(user.ID, user.Username)
	if err != nil {
		utils.Info("Failed to generate refresh token: %v\n", err)
		utils.Err(c, utils.CodeInternal, "Failed to generate token")
		return
	}

	// J.3：expires 字段 = access token 真正的 expires_at（不再是动态计算）。
	// 客户端可用 expires - now() 计算"距离刷新还剩多久"。
	expires := accessExpires.UnixNano() / 1000000

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
			services.PublishEvent("auth.refresh.success")
		} else {
			services.PublishEvent("auth.refresh.failed")
		}
	}()

	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Invalid request body")
		return
	}

	// 🛠 BUG-1 修复（2026-08-20）：J.5 Refresh Token Rotation 闭环。
	//
	// 旧 token 在下方「成功签发新 token」后会被 RevokeToken 加入黑名单，
	// 但旧 token 再次提交时 ValidateToken 不知道这件事（unauth 端点不走
	// JWTAuth 中间件 = 不走 IsTokenRevoked 检查）。如果不先查黑名单，
	// 攻击者拿到泄漏的 refresh token 后可无限续期。
	//
	// 修复：在 ValidateToken 之前先 IsTokenRevoked；命中黑名单 → 401
	// (auth.token_revoked) + 写 audit + 发布 auth.refresh.failed 事件。
	if req.RefreshToken != "" && utils.IsTokenRevoked(req.RefreshToken) {
		// 注意：actor_user_id 未知（unauth 端点），detail 用 token 指纹而非明文。
		database.RecordAuditBy(c, 0, database.AuditTargetAuth, 0, "refresh.failed", gin.H{
			"reason":         "rotated_or_revoked",
			"token_sha256_6": utils.TokenFingerprint(req.RefreshToken),
		})
		utils.Err(c, utils.CodeAuthTokenRevoked, "refresh token 已失效（已被轮换或吊销）")
		return
	}

	claims, err := h.jwtUtils.ValidateToken(req.RefreshToken)
	if err != nil {
		utils.Info("Refresh token validation failed: %v\n", err)
		// Issue #4：refresh 失败也要留痕，便于检测 token 暴力刷取
		database.RecordAuditBy(c, 0, database.AuditTargetAuth, 0, "refresh.failed", gin.H{
			"reason": "validate_failed",
			"err":    err.Error(),
		})
		utils.Err(c, utils.CodeAuthTokenInvalid, "refresh token 无效")
		return
	}

	if claims.TokenType != "refresh" {
		utils.Info("Invalid token type for refresh")
		// Issue #4：token type 不符视为 refresh.failed
		database.RecordAuditBy(c, 0, database.AuditTargetAuth, 0, "refresh.failed", gin.H{
			"reason":      "wrong_token_type",
			"token_type":  claims.TokenType,
		})
		utils.Err(c, utils.CodeAuthTokenInvalid, "refresh token 类型错误")
		return
	}

	accessToken, accessExpires, err := h.jwtUtils.GenerateAccessToken(claims.UserID, claims.Username)
	if err != nil {
		utils.Info("Failed to generate access token: %v\n", err)
		utils.Err(c, utils.CodeInternal, "Failed to generate token")
		return
	}

	// 🛠 BUG-1 修复（2026-08-20）：J.5 Refresh Token Rotation — 调整顺序。
	//
	// 旧顺序：RevokeToken(old) → Generate(new)。若 Generate 失败 → 旧 token
	// 已被吊销 → 用户被迫重新登录。颠倒后保证旧 token 在成功路径才失效。
	//
	// 新顺序：先 Generate(new) 成功 → 再 RevokeToken(old)。
	// 配合入口的 IsTokenRevoked 检查，攻击者拿旧 token 来 refresh 必 401。
	refreshToken, _, err := h.jwtUtils.GenerateRefreshToken(claims.UserID, claims.Username)
	if err != nil {
		utils.Info("Failed to generate refresh token: %v\n", err)
		utils.Err(c, utils.CodeInternal, "Failed to generate token")
		return
	}

	// 关键：成功签发后立即把旧 refresh token 加入黑名单。
	// 下一次有人拿旧 token 来 refresh → 上面 BUG-1 修复块命中黑名单 → 401。
	if req.RefreshToken != "" {
		utils.RevokeToken(req.RefreshToken, time.Now().Add(h.jwtUtils.GetRefreshExpire()).Unix())
	}

	expires := accessExpires.UnixNano() / 1000000

	response := models.TokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expires:      expires,
	}

	utils.Info("Token refreshed for user: %s\n", claims.Username)
	// Issue #4：refresh 是高敏感事件，模型字典已声明 refresh.success/failed。
	database.RecordAuditBy(c, claims.UserID, database.AuditTargetAuth, claims.UserID, "refresh.success", gin.H{
		"username": claims.Username,
	})
	utils.Success(c, response)
}

func (h *AuthHandler) GetUserInfo(c *gin.Context) {
	userID := c.GetInt64("user_id")
	username := c.GetString("username")

	user, err := database.GetUserByUsername(username)
	if err != nil {
		utils.Err(c, utils.CodeAuthTokenInvalid, "用户不存在或凭据失效")
		return
	}

	if user.ID != userID {
		utils.Err(c, utils.CodeAuthTokenInvalid, "用户不存在或凭据失效")
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

	// Issue #4：logout 是 token 吊销事件，留痕便于追溯凭据撤销。
	userID := c.GetInt64("user_id")
	username := c.GetString("username")
	// Issue M-1：旧版写 token 前 8 字符（JWT header 可还原 alg/typ 元信息），
	// 改为 sha256 不可逆指纹 + token 长度，避免泄漏签名算法与凭据片段。
	database.RecordAuditBy(c, userID, database.AuditTargetAuth, userID, "logout.success", gin.H{
		"username":            username,
		"revoked_token_kind":  classifyToken(authHeader),
		"revoked_token_hash":  tokenFingerprint(authHeader),
	})
}

// classifyToken 仅返回 token 类型（access / refresh / unknown），不取 prefix。
// 让审计员能区分吊销事件属于哪个生命周期，但不暴露凭据片段。
func classifyToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "unknown"
	}
	// 简化判断：JWT access token 较短，refresh token 一般 250+ 字符。
	// 当前 DocManageTrail 的 access/refresh token 长度差异显著，此处仅作分类。
	tok := header[len(prefix):]
	switch {
	case len(tok) > 200:
		return "refresh"
	case len(tok) > 50:
		return "access"
	default:
		return "unknown"
	}
}

// tokenFingerprint 返回 token 的 sha256 前 6 字节（hex 12 字符）作为不可逆指纹。
// 同一 token 多次吊销能 dedupe，但无法从指纹还原 token 内容。
//
// 🛠 BUG-1 修复（2026-08-20）：改为委派 utils.TokenFingerprint，避免重复实现。
// utils.TokenFingerprint 直接吃 raw token 字符串；这里负责从 Authorization
// header 里剥 "Bearer " 前缀（与 classifyToken 行为对齐）。
func tokenFingerprint(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	tok := header[len(prefix):]
	if tok == "" {
		return ""
	}
	return utils.TokenFingerprint(tok)
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
		// Round 19 #1：改用 services.PublishEvent 走事件总线，便于未来接入 webhook / ws push。
		if c.Writer.Status() == http.StatusOK {
			services.PublishEvent("auth.password.change.success")
		} else {
			services.PublishEvent("auth.password.change.failed")
		}
		// Issue #4：密码变更是 L4 取证必查项，模型字典已声明
		// password.change.success / password.change.failed，必须留痕。
		// 注意：detail 不能包含明文密码 / 哈希。
		uid := c.GetInt64("user_id")
		uname := c.GetString("username")
		if c.Writer.Status() == http.StatusOK {
			services.PublishEvent("auth.password.change.success")
			database.RecordAudit(c, database.AuditTargetAuth, uid, "password.change.success", gin.H{
				"username": uname,
			})
		} else {
			services.PublishEvent("auth.password.change.failed")
			database.RecordAudit(c, database.AuditTargetAuth, uid, "password.change.failed", gin.H{
				"username": uname,
				"reason":   "see_response_body",
			})
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
		utils.Err(c, utils.CodeAuthPasswordWeak, err.Error())
		return
	}

	// 2. 新旧密码不能相同
	if req.OldPassword == req.NewPassword {
		utils.Err(c, utils.CodeAuthPasswordReused, "新密码不能与旧密码相同")
		return
	}

	// 3. 校验旧密码
	user, err := database.GetUserByUsername(username)
	if err != nil {
		utils.Err(c, utils.CodeAuthTokenInvalid, "用户不存在或凭据失效")
		return
	}
	if user.ID != userID {
		utils.Err(c, utils.CodeAuthTokenInvalid, "用户不存在或凭据失效")
		return
	}
	if !utils.CheckPassword(req.OldPassword, user.PasswordHash) {
		utils.Err(c, utils.CodeAuthInvalidCredentials, "旧密码错误")
		return
	}

	// 4. 更新密码
	if err := database.UpdateUserPassword(userID, req.NewPassword); err != nil {
		utils.Err(c, utils.CodeInternal, "更新密码失败: "+err.Error())
		return
	}

	utils.Info("Password changed for user: %s\n", username)
	utils.Success(c, gin.H{"message": "密码修改成功"})
}

func (h *AuthHandler) Register(c *gin.Context) {
	// 2026-07-04：注册端点暂不开放（公网部署防滥用 + 后台手工建账号更可控）
	utils.Err(c, utils.CodeForbidden, "注册功能暂未开放，请联系管理员创建账号")
}

// IsAdminUser 判断当前用户是否为 admin。
//
// 🛠 P1 BUG-6 修复（2026-08-20）：复用 GetEffectivePermissionsCached 缓存，
// 不再每请求额外查一次 DB（IsAdminUser 在 APIGateMiddleware 中被频繁调用）。
// GetEffectivePermissionsCached 内部已走 permsCache（5min TTL），省掉一次 DB。
func IsAdminUser(c *gin.Context) bool {
	username := c.GetString("username")
	if username == "" {
		return false
	}

	perms, err := GetEffectivePermissionsCached(username)
	if err != nil || perms == nil {
		return false
	}

	// admin 角色命中条件：持有通配符权限 *:* 或 roles 含 "admin"
	for _, p := range perms {
		if p == "*:*" || p == "admin" {
			return true
		}
	}
	return false
}
