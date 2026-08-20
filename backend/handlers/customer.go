package handlers

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"doc/config"
	"doc/database"
	"doc/middleware"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

type CustomerHandler struct {
	cfg *config.Config
}

func NewCustomerHandler(cfg *config.Config) *CustomerHandler {
	return &CustomerHandler{cfg: cfg}
}

func (h *CustomerHandler) CreateCustomer(c *gin.Context) {
	// 2026-07-07 round11：与 manager 角色 seed 保持一致——manager 已持有
	// customer:create 权限码；改用 RequirePermission 让 manager 可写客户。
	if !RequirePermission(c, "customer:create") {
		return
	}

	var req struct {
		RealName  string `json:"real_name"`
		Phone     string `json:"phone"`
		IDCard    string `json:"id_card"`
		Address   string `json:"address"`
		Email     string `json:"email"`
		Gender    string `json:"gender"`
		BirthDate string `json:"birth_date"`
		Remarks   string `json:"remarks"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}

	snowid := utils.NextSnowIDString()

	// 2026-06-28 RBAC v3 P3：owner_user_id = 当前用户，department_id = 当前用户主部门。
	// admin 创建时也设置（admin 短路 data_scope='all'，不影响列表）。
	ownerUserID := c.GetInt64("user_id")
	deptID, _ := database.GetUserMainDepartment(ownerUserID)
	id, err := database.CreateCustomerV3(snowid, req.RealName, req.Phone, req.IDCard, req.Address, req.Email, req.Gender, req.BirthDate, req.Remarks, ownerUserID, deptID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "创建客户失败: "+err.Error())
		return
	}

	services.PublishEvent("customer.create")
	// 审计：customer create（detail 仅记字段名 + 长度，避免存敏感数据）。
	database.RecordAudit(c, database.AuditTargetCustomer, id, "create", gin.H{
		"snowid":        snowid,
		"real_name_len": len(req.RealName),
		"phone_len":     len(req.Phone),
		"id_card_len":   len(req.IDCard),
		"owner_user_id": ownerUserID,
		"department_id": deptID,
	})
	utils.Success(c, gin.H{
		"id":      id,
		"snowid":  snowid,
		"message": "创建成功",
	})
}

func (h *CustomerHandler) GetCustomerList(c *gin.Context) {
	// 2026-06-28 RBAC v3 C1 修复：admin gate 改用 IsAdminUser（DB-backed）。
	// 第一轮 B3 用了 IsAdmin（JWT-only username 检查），会误放 username="admin" 但 roles 错的用户。
	// IsAdminUser 查 DB users.roles 含 "admin" 才放行，与其他 handler 一致。
	//
	// 2026-06-28 修复 Issue #2：gate 改为「admin 或持有 customer:list 权限码」。
	// 原实现 `if !IsAdminUser(c) { 403 }` 直接拒绝所有非 admin，但下方 107-125 的 data_scope 过滤
	// 分支本意是支持非 admin manager 查本部门客户，导致非 admin 路径变成死代码。
	// APIGateMiddleware 已做权限码前置 gate，这里再做一次双保险（性能可忽略）。
	//
	// 流程：
	//   - admin（DB roles 含 admin）→ 进入 handler，scope.DataScope == "all" → 全量
	//   - 非 admin 持有 customer:list 权限码 → 进入 handler，scope != "all" → data_scope 过滤
	//   - 无权限用户 → APIGateMiddleware 已 403，到不了这里
	if !IsAdminUser(c) && !HasPermission(c, "customer:list") {
		utils.Error(c, http.StatusForbidden, "需要管理员或 customer:list 权限")
		return
	}

	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "10")
	keyword := c.Query("keyword")

	pageInt, _ := strconv.Atoi(page)
	pageSizeInt, _ := strconv.Atoi(pageSize)

	if pageInt < 1 {
		pageInt = 1
	}
	if pageSizeInt < 1 || pageSizeInt > 100 {
		pageSizeInt = 10
	}

	// 2026-06-28 RBAC v3 P3：admin 看全部；非 admin（manager with data_scope=dept 等）按 scope 过滤。
	// customer 表的 owner 列是 owner_user_id。
	scope := middleware.GetDataScope(c)
	var customers []database.Customer
	var total int
	var err error
	if scope == nil || scope.DataScope == "all" {
		customers, total, err = database.GetCustomersWithPaginationAndSearch(pageInt, pageSizeInt, keyword)
	} else {
		whereSQL, args, werr := database.BuildWhereSQL(scope, database.FilterOpts{
			OwnerCol: "owner_user_id",
			DeptCol:  "department_id",
		})
		if werr != nil {
			utils.Error(c, http.StatusInternalServerError, "data_scope 过滤失败: "+werr.Error())
			return
		}
		customers, total, err = database.ListCustomersByDataScope(pageInt, pageSizeInt, keyword, whereSQL, args)
	}
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取客户列表失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"list":      customers,
		"total":     total,
		"page":      pageInt,
		"page_size": pageSizeInt,
	})
}

func (h *CustomerHandler) GetCustomerByID(c *gin.Context) {
	// 2026-06-28 修复 Issue #4：与 GetCustomerList 对齐，允许非 admin 持有 customer:list
	// 权限码的用户按 data_scope 访问单条详情。
	// 原 RequireAdmin 会导致「列表可见、点开 403」的 UX 问题。
	// 2026-07-07 round11：Create/Update/Delete 同步改为 RequirePermission("customer:create"/"customer:update"/"customer:delete")，
	// 与 manager 角色 seed 中的权限码一致。
	if !IsAdminUser(c) && !HasPermission(c, "customer:list") {
		utils.Error(c, http.StatusForbidden, "需要管理员或 customer:list 权限")
		return
	}

	idStr := c.Query("id")
	if idStr == "" {
		utils.BadRequest(c, "缺少客户ID参数")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的客户ID")
		return
	}

	customer, err := database.GetCustomerByID(id)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "客户不存在")
		return
	}

	// 非 admin 用户走 data_scope 二次校验
	if !IsAdminUser(c) {
		if !customerDataScopeAllows(c, customer.OwnerUserID, customer.DepartmentID) {
			utils.Error(c, http.StatusForbidden, "无权查看此客户")
			return
		}
	}

	utils.Success(c, customer)
}

// customerDataScopeAllows 2026-06-29 RBAC v3 P2 重构：转调统一 helper。
// 保留薄壳函数名以最小化 handler 调用点改动；新代码请直接用 CheckDataScopeAccess。
func customerDataScopeAllows(c *gin.Context, ownerID, departmentID int64) bool {
	return CheckDataScopeAccess(c, ownerID, departmentID)
}

func (h *CustomerHandler) UpdateCustomer(c *gin.Context) {
	// 2026-07-07 round11：与 CreateCustomer 对齐，manager 持有 customer:update 即可写。
	if !RequirePermission(c, "customer:update") {
		return
	}

	var req struct {
		ID        int64  `json:"id"`
		RealName  string `json:"real_name"`
		Phone     string `json:"phone"`
		IDCard    string `json:"id_card"`
		Address   string `json:"address"`
		Email     string `json:"email"`
		Gender    string `json:"gender"`
		BirthDate string `json:"birth_date"`
		Remarks   string `json:"remarks"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.ID <= 0 {
		utils.BadRequest(c, "无效的客户ID")
		return
	}

	err := database.UpdateCustomer(req.ID, req.RealName, req.Phone, req.IDCard, req.Address, req.Email, req.Gender, req.BirthDate, req.Remarks)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "更新客户信息失败")
		return
	}

	services.PublishEvent("customer.update")
	utils.Success(c, gin.H{
		"message": "更新成功",
	})
}

func (h *CustomerHandler) DeleteCustomer(c *gin.Context) {
	// 2026-07-07 round11：与 Create/Update 对齐，manager 持有 customer:delete 即可删。
	if !RequirePermission(c, "customer:delete") {
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.ID <= 0 {
		utils.BadRequest(c, "无效的客户ID")
		return
	}

	err := database.DeleteCustomer(req.ID)
	if err != nil {
		// 业务约束（存在进行中合同/流转）→ 409 Conflict；
		// DB 错误 → 500。错误信息透传给前端用于提示。
		if errors.Is(err, database.ErrDeleteCustomerBlocked) {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		utils.Error(c, http.StatusInternalServerError, "删除客户失败: "+err.Error())
		return
	}

	services.PublishEvent("customer.delete")
	// 审计：customer delete。
	database.RecordAudit(c, database.AuditTargetCustomer, req.ID, "delete", nil)
	utils.Success(c, gin.H{
		"message": "删除成功",
	})
}

func (h *CustomerHandler) UploadSignature(c *gin.Context) {
	var req struct {
		CustomerID int64  `json:"customer_id" binding:"required"`
		Signature  string `json:"signature" binding:"required"`
		Date       string `json:"date"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据")
		return
	}

	if req.CustomerID <= 0 {
		utils.BadRequest(c, "无效的客户ID")
		return
	}

	customer, err := database.GetCustomerByID(req.CustomerID)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "客户不存在")
		return
	}

	signatureData := req.Signature
	if len(signatureData) > 22 && signatureData[:22] == "data:image/png;base64," {
		signatureData = signatureData[22:]
	} else if len(signatureData) > 20 && signatureData[:20] == "data:image/jpeg;base64," {
		signatureData = signatureData[20:]
	}

	decodedSignature, err := base64.StdEncoding.DecodeString(signatureData)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "无效的签名数据")
		return
	}

	now := time.Now()
	yearMonth := now.Format("200601")
	signDir := filepath.Join(h.cfg.Upload.Dir, "sign", yearMonth)
	if err := os.MkdirAll(signDir, 0755); err != nil {
		utils.Error(c, http.StatusInternalServerError, "创建目录失败")
		return
	}

	var snowid string
	var recordID int64
	var lastFilePath string

	saveSignatureRecord := func(sigType string, data []byte) (int64, string, error) {
		snowid = utils.NextSnowIDString()
		filename := fmt.Sprintf("%s_%s.png", snowid, sigType)
		filePath := filepath.Join(signDir, filename)

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return 0, "", err
		}

		hashService := services.NewHashService(true, true)
		dualHash, err := hashService.CalculateDualHash(data)
		if err != nil {
			utils.Warn("[签署] 计算签名哈希失败: %v", err)
			dualHash = &services.DualHash{}
		}

		recID, err := database.CreateSignatureRecord(snowid, req.CustomerID, sigType, filePath, dualHash.SM3Hash, dualHash.SHA256Hash, dualHash.CombinedHash)
		return recID, filePath, err
	}

	signatureID, signatureFilePath, err := saveSignatureRecord("signature", decodedSignature)
	lastFilePath = signatureFilePath
	recordID = signatureID
	if err != nil {
		utils.Info("[签署] 创建签名记录失败: snowid=%s, customerID=%d, err=%v", snowid, req.CustomerID, err)
		utils.Error(c, http.StatusInternalServerError, "创建签名记录失败")
		return
	}

	// WORM（Phase 1 Critical #1）：签名图上传成功后立即锁定。
	// 失败不阻断业务流：签名图已入库 + 落盘，锁失败属运维事件。
	absSigPath := signatureFilePath
	if !filepath.IsAbs(absSigPath) {
		absSigPath = filepath.Join(h.cfg.Upload.Dir, absSigPath)
	}
	userID := c.GetInt64("user_id")
	if _, lockErr := services.LockOnce(absSigPath, snowid, userID, "customer.signature.upload"); lockErr != nil {
		if errors.Is(lockErr, database.ErrAlreadyLocked) {
			utils.Warn("[签署] WORM 已存在锁（重复上传）: snowid=%s", snowid)
		} else {
			utils.Warn("[签署] WORM LockOnce 失败: snowid=%s, err=%v", snowid, lockErr)
		}
	}

	if req.Date != "" {
		dateData := req.Date
		if len(dateData) > 22 && dateData[:22] == "data:image/png;base64," {
			dateData = dateData[22:]
		} else if len(dateData) > 20 && dateData[:20] == "data:image/jpeg;base64," {
			dateData = dateData[20:]
		}

		decodedDate, err := base64.StdEncoding.DecodeString(dateData)
		if err == nil && len(decodedDate) > 0 {
			_, dateFilePath, derr := saveSignatureRecord("date", decodedDate)
			if derr != nil {
				utils.Info("[签署] 创建日期记录失败: err=%v", derr)
			} else {
				lastFilePath = dateFilePath
			}
		}
	}

	adminUser, _ := database.GetUserByUsername("admin")
	if adminUser != nil {
		msg := fmt.Sprintf("%s 上传了签名照片", customer.RealName)
		_, _ = database.CreateMessage(adminUser.ID, req.CustomerID, "签名上传通知", msg, "signature")
	}

	services.PublishEvent("customer.signature.upload")
	// 审计：customer signature upload（仅记录文件名 + 字节数，不落二进制）。
	database.RecordAudit(c, database.AuditTargetCustomer, req.CustomerID, "signature.upload", gin.H{
		"signature_snowid": snowid,
		"file_path":        filepath.Base(lastFilePath),
		"record_id":        recordID,
	})
	utils.Success(c, gin.H{
		"message":          "签名上传成功",
		"signature_snowid": snowid,
		"file_path":        lastFilePath,
		"record_id":        recordID,
	})
}

func (h *CustomerHandler) GetSignature(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的签名ID")
		return
	}

	record, err := database.GetSignatureRecordByID(id)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "签名不存在")
		return
	}

	absPath, err := filepath.Abs(record.FilePath)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "文件路径错误")
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		utils.Error(c, http.StatusNotFound, "签名文件不存在")
		return
	}

	c.Header("Content-Type", "image/png")
	c.Header("Content-Disposition", "inline; filename=signature.png")
	c.File(absPath)
}

// ==================== Individual / Enterprise Customer APIs ====================
// The 4 handlers below are additive: the original Create / GetCustomerList /
// GetCustomerByID / Update / DeleteCustomer are kept unchanged so existing
// callers continue to work. New endpoints:
//   POST /api/customer/create-ext         -> CreateCustomerExt
//   POST /api/customer/update-ext         -> UpdateCustomerExt
//   GET  /api/customer/list-by-type       -> GetCustomersByTypeList
//   GET  /api/customer/search-by-type     -> SearchCustomersByTypeList

// CreateCustomerExt supports both individual and enterprise customers.
// Frontend should call this for new customer management (个人/企业 tab).
func (h *CustomerHandler) CreateCustomerExt(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	var in database.CustomerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}

	// 基础校验
	if in.Phone == "" {
		utils.BadRequest(c, "联系电话不能为空")
		return
	}
	if in.CustomerType == "" {
		in.CustomerType = "individual"
	}
	if in.CustomerType != "individual" && in.CustomerType != "enterprise" {
		utils.BadRequest(c, "客户类型必须是 individual 或 enterprise")
		return
	}
	// 按类型强校验关键字段
	if in.CustomerType == "individual" && in.RealName == "" {
		utils.BadRequest(c, "个人客户的真实姓名不能为空")
		return
	}
	if in.CustomerType == "enterprise" && in.CompanyName == "" {
		utils.BadRequest(c, "企业客户的企业名称不能为空")
		return
	}
	if in.CustomerType == "enterprise" && in.USCC != "" {
		if err := validateUSCC(in.USCC); err != nil {
			utils.BadRequest(c, "统一社会信用代码无效: "+err.Error())
			return
		}
	}

	in.SnowID = utils.NextSnowIDString()

	id, err := database.CreateCustomerExt(&in)
	if err != nil {
		// 部分唯一索引冲突：统一社会信用代码已存在
		if isUniqueConstraintError(err, "idx_customer_uscc") {
			utils.Error(c, http.StatusConflict, "该统一社会信用代码已存在")
			return
		}
		utils.Error(c, http.StatusInternalServerError, "创建客户失败: "+err.Error())
		return
	}

	services.PublishEvent("customer.create")
	utils.Success(c, gin.H{
		"id":            id,
		"snowid":        in.SnowID,
		"customer_type": in.CustomerType,
		"message":       "创建成功",
	})
}

// UpdateCustomerExt updates a customer (individual or enterprise) by id.
// Uses Go struct embedding to fold `id` and `CustomerInput` into a single
// anonymous struct, so a single ShouldBindJSON call parses everything
// (the alternative of parsing twice with json.Unmarshal is more error-prone
// because c.Request.Body can only be read once).
func (h *CustomerHandler) UpdateCustomerExt(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	var req struct {
		ID int64 `json:"id"`
		database.CustomerInput
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if req.ID <= 0 {
		utils.BadRequest(c, "无效的客户ID")
		return
	}

	// 字段验证（访问嵌入字段时直接用名字，会被 Go 自动"提升"）
	if req.CustomerType != "" && req.CustomerType != "individual" && req.CustomerType != "enterprise" {
		utils.BadRequest(c, "客户类型必须是 individual 或 enterprise")
		return
	}
	// 按类型校验必填字段（与 CreateCustomerExt 保持一致）
	if req.CustomerType == "individual" && req.RealName == "" {
		utils.BadRequest(c, "个人客户的真实姓名不能为空")
		return
	}
	if req.CustomerType == "enterprise" && req.CompanyName == "" {
		utils.BadRequest(c, "企业客户的企业名称不能为空")
		return
	}
	if req.CustomerType == "enterprise" && req.USCC != "" {
		if err := validateUSCC(req.USCC); err != nil {
			utils.BadRequest(c, "统一社会信用代码无效: "+err.Error())
			return
		}
	}

	if err := database.UpdateCustomerExt(req.ID, &req.CustomerInput); err != nil {
		if isUniqueConstraintError(err, "idx_customer_uscc") {
			utils.Error(c, http.StatusConflict, "该统一社会信用代码已存在")
			return
		}
		utils.Error(c, http.StatusInternalServerError, "更新客户信息失败: "+err.Error())
		return
	}

	services.PublishEvent("customer.update")
	utils.Success(c, gin.H{
		"id":      req.ID,
		"message": "更新成功",
	})
}

// GetCustomersByTypeList returns customers filtered by customer_type.
//
//	GET /api/customer/list-by-type?type=individual|enterprise&page=1&page_size=10
func (h *CustomerHandler) GetCustomersByTypeList(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	ctype := c.Query("type")
	if ctype != "" && ctype != "individual" && ctype != "enterprise" {
		utils.BadRequest(c, "type 必须是 individual 或 enterprise")
		return
	}

	pageInt, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSizeInt, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if pageInt < 1 {
		pageInt = 1
	}
	if pageSizeInt < 1 || pageSizeInt > 100 {
		pageSizeInt = 10
	}

	customers, total, err := database.GetCustomersByType(pageInt, pageSizeInt, ctype)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取客户列表失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"list":      customers,
		"total":     total,
		"page":      pageInt,
		"page_size": pageSizeInt,
		"type":      ctype,
	})
}

// SearchCustomersByTypeList paginates and searches customers filtered by type.
//
//	GET /api/customer/search-by-type?type=individual|enterprise&keyword=&page=1&page_size=10
func (h *CustomerHandler) SearchCustomersByTypeList(c *gin.Context) {
	if !RequireAdmin(c) {
		return
	}

	ctype := c.Query("type")
	if ctype != "" && ctype != "individual" && ctype != "enterprise" {
		utils.BadRequest(c, "type 必须是 individual 或 enterprise")
		return
	}
	keyword := c.Query("keyword")

	pageInt, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSizeInt, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if pageInt < 1 {
		pageInt = 1
	}
	if pageSizeInt < 1 || pageSizeInt > 100 {
		pageSizeInt = 10
	}

	customers, total, err := database.SearchCustomersByType(pageInt, pageSizeInt, keyword, ctype)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "搜索客户失败: "+err.Error())
		return
	}

	utils.Success(c, gin.H{
		"list":      customers,
		"total":     total,
		"page":      pageInt,
		"page_size": pageSizeInt,
		"type":      ctype,
		"keyword":   keyword,
	})
}
