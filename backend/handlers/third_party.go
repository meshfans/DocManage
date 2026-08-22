package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"doc/config"
	"doc/database"
	"doc/middleware"
	"doc/models"
	"doc/services"
	"doc/utils"

	"github.com/gin-gonic/gin"
)

// ==================== ThirdPartyHandler ====================

// ThirdPartyHandler 第三方合同 9 个 API + 1 bulk 端点
// 路由前缀：/api/third-party/contracts
// 鉴权：受 JWTAuth 中间件保护
type ThirdPartyHandler struct {
	cfg *config.Config
}

func NewThirdPartyHandler(cfg *config.Config) *ThirdPartyHandler {
	return &ThirdPartyHandler{cfg: cfg}
}

// ==================== Helper ====================

// thirdPartyAccess 权限校验：所有登录用户可读；写操作限制 owner 或 admin
// 注：admin 通过 IsAdminUser(c) 判定（DB-backed）
// 非 admin 用户需同时满足：
//  1. owner == 当前用户，或
//  2. data_scope 允许访问（manager 看本部门+下级、custom 等）
func (h *ThirdPartyHandler) thirdPartyAccess(c *gin.Context, t *models.ThirdPartyContract) bool {
	if t == nil {
		return false
	}
	currentUserID := c.GetInt64("user_id")
	if t.CreatedBy == currentUserID || IsAdminUser(c) {
		return true
	}
	// 非 owner 非 admin：data_scope 二级校验
	return thirdPartyDataScopeAllows(c, t.CreatedBy, t.DepartmentID)
}

// thirdPartyDataScopeAllows 转调统一 helper。
// 保留薄壳函数名以最小化 handler 调用点改动；新代码请直接用 CheckDataScopeAccess。
func thirdPartyDataScopeAllows(c *gin.Context, ownerID, departmentID int64) bool {
	return CheckDataScopeAccess(c, ownerID, departmentID)
}

// generateContractNo 业务编号：TP-YYYYMMDD-snowid
// 用 snowid 后 6 位作为序号（保证 UNIQUE，修复原先 nano % 1e6 重复 bug）
func generateContractNo() string {
	datePrefix := time.Now().Format("20060102")
	// snowid 是全局唯一 ID，取后 6 位作为序号
	return fmt.Sprintf("TP-%s-%s", datePrefix, utils.NextSnowIDString())
}

// listThirdPartyContractsScoped 抽取 ListContracts / BulkDownload else 分支共用的 data_scope 拼接逻辑。
// 返回 (list, total, err)，调用方自行决定 HTTP 响应与日志。
//
// 规则：
//   - admin 或 data_scope=all：直接走 ListThirdPartyContracts（全量）
//   - 否则：BuildWhereSQL + ListThirdPartyContractsByDataScope（按 created_by/department_id 过滤）
func listThirdPartyContractsScoped(c *gin.Context, customerID int64, customerType, status, search string, page, pageSize int) ([]*models.ThirdPartyContract, int, error) {
	scope := middleware.GetDataScope(c)
	if scope == nil || scope.DataScope == "all" {
		return database.ListThirdPartyContracts(customerID, customerType, status, search, page, pageSize)
	}
	whereSQL, args, werr := database.BuildWhereSQL(scope, database.FilterOpts{
		OwnerCol:   "created_by",
		DeptCol:    "department_id",
		TableAlias: "tpc",
	})
	if werr != nil {
		return nil, 0, werr
	}
	return database.ListThirdPartyContractsByDataScope(customerID, customerType, status, search, page, pageSize, whereSQL, args)
}

// ==================== 9 个端点 ====================

// ListContracts 列表 + 过滤 + 分页
//
//	GET /api/third-party/contracts
//	?customer_id=&customer_type=&status=&search=&page=&page_size=
//
// admin 看全部；非 admin 按 data_scope 过滤。
// 资源列：owner=tpc.created_by，dept=tpc.department_id。
func (h *ThirdPartyHandler) ListContracts(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:list") {
		return
	}
	customerID, _ := strconv.ParseInt(c.Query("customer_id"), 10, 64)
	customerType := c.Query("customer_type")
	if customerType != "" && customerType != "individual" && customerType != "enterprise" {
		customerType = ""
	}
	status := c.Query("status")
	search := strings.TrimSpace(c.Query("search"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var list []*models.ThirdPartyContract
	var total int
	var err error
	// data_scope 拼接逻辑抽到 listThirdPartyContractsScoped helper
	list, total, err = listThirdPartyContractsScoped(c, customerID, customerType, status, search, page, pageSize)
	if err != nil {
		utils.LogError("[third_party.ListContracts] 失败: customerID=%d err=%v", customerID, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	// 防御性兜底：list 永不为 nil（DB 层已保证）
	if list == nil {
		list = []*models.ThirdPartyContract{}
	}

	// 联表 customer：批查 + map 挂到每行（避免 N+1）
	customerMap, err := h.loadCustomersForList(list)
	if err != nil {
		// 联表失败不阻塞列表返回，只记 warn
		utils.Warn("[third_party] 联表 customer 失败: %v", err)
	}

	utils.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		// 客户信息 map（key=customer_id），前端用 row.customer_id 索引
		"customer_map": customerMap,
	})
}

// loadCustomersForList 批量查 list 中涉及的 customer，返回 map[id]customer_lite。
// 失败返回 (nil, err)。空 list 短路返回 (map{}, nil)。
// 返回的是简化字段（id/real_name/company_name/phone/customer_type），
// 不传全表所有字段，省序列化开销。
type customerLite struct {
	ID           int64  `json:"id"`
	CustomerType string `json:"customer_type"`
	RealName     string `json:"real_name"`
	CompanyName  string `json:"company_name"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
}

func (h *ThirdPartyHandler) loadCustomersForList(list []*models.ThirdPartyContract) (map[int64]customerLite, error) {
	if len(list) == 0 {
		return map[int64]customerLite{}, nil
	}
	// 收集去重的 customer_id
	ids := make([]int64, 0, len(list))
	seen := make(map[int64]struct{}, len(list))
	for _, c := range list {
		if c.CustomerID <= 0 {
			continue
		}
		if _, ok := seen[c.CustomerID]; ok {
			continue
		}
		seen[c.CustomerID] = struct{}{}
		ids = append(ids, c.CustomerID)
	}
	if len(ids) == 0 {
		return map[int64]customerLite{}, nil
	}
	full, err := database.GetCustomersByIDs(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]customerLite, len(full))
	for id, c := range full {
		out[id] = customerLite{
			ID:           c.ID,
			CustomerType: c.CustomerType,
			RealName:     c.RealName,
			CompanyName:  c.CompanyName,
			Phone:        c.Phone,
			Email:        c.Email,
		}
	}
	return out, nil
}

// CreateContract 创建
//
//	POST /api/third-party/contracts
//	body: { contract_no?, title, type?, status?, customer_id, amount?, currency?, sign_date?, start_date?, end_date?, file_path?, file_size?, file_hash?, file_sha256?, file_combined_hash?, remark? }
func (h *ThirdPartyHandler) CreateContract(c *gin.Context) {
	// HP3（2026-08-20）：显式 RequirePermission 兜底，避免 APIGateMiddleware 路径配置错误导致越权。
	// APIGate 也会校验，这里是双保险。
	if !RequirePermission(c, "thirdparty:create") {
		return
	}
	var req struct {
		Title      string  `json:"title"`
		Type       string  `json:"type"`
		Status     string  `json:"status"`
		CustomerID int64   `json:"customer_id"`
		Amount     float64 `json:"amount"`
		Currency   string  `json:"currency"`
		SignDate   int64   `json:"sign_date"`
		StartDate  int64   `json:"start_date"`
		EndDate    int64   `json:"end_date"`
		// 文件字段（v5 必填）
		FilePath         string `json:"file_path"`
		FileSize         int64  `json:"file_size"`
		FileSM3Hash      string `json:"file_sm3_hash"`
		FileSHA256Hash   string `json:"file_sha256_hash"`
		FileCombinedHash string `json:"file_combined_hash"`
		Remark           string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		utils.BadRequest(c, "title 不能为空")
		return
	}
	if req.CustomerID <= 0 {
		utils.BadRequest(c, "customer_id 不能为空")
		return
	}
	// 移除 FilePath/FileSize 强制校验，允许"先建档后补传"的工作流。
	// 文件补传走 POST /api/third-party/contracts/:id/upload 单独调用。
	if req.FilePath != "" && req.FileSize <= 0 {
		utils.BadRequest(c, "已提供 file_path 时 file_size 必须 > 0")
		return
	}
	// 校验 type
	tpType := req.Type
	if tpType == "" {
		tpType = models.TPContractTypePaper
	}
	if !models.IsValidTPType(tpType) {
		utils.BadRequest(c, "非法的 type: "+tpType)
		return
	}
	// 校验 status（默认 draft）
	tpStatus := req.Status
	if tpStatus == "" {
		tpStatus = models.TPStatusDraft
	}
	if !models.IsValidTPStatus(tpStatus) {
		utils.BadRequest(c, "非法的 status: "+tpStatus)
		return
	}
	// 校验 currency（默认 CNY）
	currency := req.Currency
	if currency == "" {
		currency = "CNY"
	}
	// 校验 customer 存在
	customer, err := database.GetCustomerByID(req.CustomerID)
	if err != nil {
		utils.LogError("[third_party.CreateContract] 查询客户失败 customerID=%d: %v", req.CustomerID, err)
		utils.Err(c, utils.CodeInternal, "查询客户失败: "+err.Error())
		return
	}
	if customer == nil {
		utils.BadRequest(c, "客户不存在")
		return
	}
	// 合同号独立生成（不复用 req.FilePath，避免语义混淆）
	contractNoFinal := generateContractNo()
	currentUserID := c.GetInt64("user_id")
	// 创建时快照当前用户主部门，供 data_scope 过滤使用。
	deptID, _ := database.GetUserMainDepartment(currentUserID)

	id, err := database.CreateThirdPartyContractV2(
		contractNoFinal, req.Title, tpType, tpStatus,
		req.CustomerID,
		req.Amount, currency,
		req.SignDate, req.StartDate, req.EndDate,
		req.FilePath, req.FileSize,
		req.FileSM3Hash, req.FileSHA256Hash, req.FileCombinedHash,
		req.Remark, currentUserID, deptID,
	)
	if err != nil {
		// UNIQUE 冲突：合同号重复（极端并发）
		utils.LogError("[third_party.CreateContract] 创建失败 title=%q customerID=%d: %v", req.Title, req.CustomerID, err)
		utils.Err(c, utils.CodeInternal, "创建失败: "+err.Error())
		return
	}
	// 返回详情（含联表）
	created, _ := database.GetThirdPartyContractByID(id)
	services.PublishEvent("thirdparty.contract.create")
	// 审计：第三方合同创建（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetThirdParty, id, "create", gin.H{
		"contract_no": created.ContractNo,
		"title":       req.Title,
		"customer_id": req.CustomerID,
		"type":        tpType,
		"status":      tpStatus,
		"amount":      req.Amount,
		"currency":    currency,
	})
	utils.Success(c, gin.H{"id": id, "data": created})
}

// GetContract 详情
//
//	GET /api/third-party/contracts/:id
//
// owner-or-admin 或 data_scope 允许才能查看。
func (h *ThirdPartyHandler) GetContract(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:detail") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.GetContract] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权访问此合同")
		return
	}
	utils.Success(c, t)
}

// UpdateContract 更新
//
//	POST /api/third-party/contracts/:id
//	body: 同 CreateContract（不含 customer_id 必填校验）
func (h *ThirdPartyHandler) UpdateContract(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:update") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.UpdateContract] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权修改")
		return
	}

	// WORM（Phase 1 Critical #1）：已上锁的 PDF 二进制不可被 UpdateContract 修改。
	// 这里只校验"是否被篡改"；UpdateContract 当前不修改 PDF 二进制，仅修改元数据。
	// 但若未来 UpdateContract 引入"重新上传 PDF"逻辑，应在重传后再 LockOnce（snowid 不变），
	// 此处 VerifyWorm 主要是"提早发现文件已被外部篡改"，给运维告警。
	if t.FilePath != "" {
		storage := services.GetThirdPartyStorage()
		if absExisting, pathErr := storage.GetAbsolutePath(t.FilePath); pathErr == nil {
			if locked, _, verifyErr := services.VerifyWorm(absExisting); verifyErr != nil && locked {
				// 已锁文件被篡改/删除 → 拒绝
				if errors.Is(verifyErr, services.ErrWormTampered) {
					utils.Err(c, utils.CodeContractLocked, "合同 PDF 已被锁定且检测到篡改，无法修改")
					return
				}
				// 其它 IO 错误 → 仅日志
				utils.Warn("[第三方合同] WORM VerifyWorm 异常: err=%v", verifyErr)
			}
		}
	}

	var req struct {
		Title      string  `json:"title"`
		Type       string  `json:"type"`
		CustomerID int64   `json:"customer_id"`
		Amount     float64 `json:"amount"`
		Currency   string  `json:"currency"`
		SignDate   int64   `json:"sign_date"`
		StartDate  int64   `json:"start_date"`
		EndDate    int64   `json:"end_date"`
		Remark     string  `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		utils.BadRequest(c, "title 不能为空")
		return
	}
	if req.CustomerID <= 0 {
		utils.BadRequest(c, "customer_id 不能为空")
		return
	}
	if !models.IsValidTPType(req.Type) {
		utils.BadRequest(c, "非法的 type: "+req.Type)
		return
	}

	if err := database.UpdateThirdPartyContractBasic(
		id,
		req.Title, req.Type, req.CustomerID,
		req.Amount, req.Currency,
		req.SignDate, req.StartDate, req.EndDate,
		req.Remark,
	); err != nil {
		utils.LogError("[third_party.UpdateContract] 更新失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "更新失败: "+err.Error())
		return
	}
	updated, _ := database.GetThirdPartyContractByID(id)
	services.PublishEvent("thirdparty.contract.update")
	// 审计：第三方合同更新（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetThirdParty, id, "update", gin.H{
		"contract_no": updated.ContractNo,
		"title":       req.Title,
		"customer_id": req.CustomerID,
	})
	utils.Success(c, gin.H{"data": updated})
}

// ChangeStatus 状态切换
//
//	POST /api/third-party/contracts/:id/status
//	body: { status }
func (h *ThirdPartyHandler) ChangeStatus(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:status") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.ChangeStatus] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权操作")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if !models.IsValidTPStatus(req.Status) {
		utils.BadRequest(c, "非法的 status: "+req.Status)
		return
	}
	ok, reason := models.TPStatusCanTransition(t.Status, req.Status)
	if !ok {
		utils.Err(c, utils.CodeConflict, "状态转换失败: "+reason)
		return
	}
	if err := database.UpdateThirdPartyContractStatus(id, req.Status); err != nil {
		utils.LogError("[third_party.ChangeStatus] 更新状态失败 id=%d status=%s: %v", id, req.Status, err)
		utils.Err(c, utils.CodeInternal, "更新状态失败: "+err.Error())
		return
	}
	services.PublishEvent("thirdparty.contract.status.change")
	// 审计：状态转换（HP2 补全）。
	database.RecordAudit(c, database.AuditTargetThirdParty, id, "status.change", gin.H{
		"contract_no": t.ContractNo,
		"old_status":  t.Status,
		"new_status":  req.Status,
	})
	utils.Success(c, gin.H{"message": "状态已更新", "status": req.Status})
}

// DeleteContract 删除（仅 draft / cancelled）
//
//	POST /api/third-party/contracts/:id/delete
func (h *ThirdPartyHandler) DeleteContract(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:delete") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.DeleteContract] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权删除")
		return
	}
	if !models.TPStatusCanDelete(t.Status) {
		utils.Err(c, utils.CodeConflict, "只有草稿/已取消状态可删除")
		return
	}
	// 先删文件
	storage := services.GetThirdPartyStorage()
	if err := storage.DeleteFile(t.FilePath); err != nil {
		utils.Warn("[third_party] 删除文件失败（继续删记录）: %v", err)
	}
	if err := database.DeleteThirdPartyContract(id); err != nil {
		utils.LogError("[third_party.DeleteContract] 删除失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "删除失败: "+err.Error())
		return
	}
	services.PublishEvent("thirdparty.contract.delete")
	// 审计：thirdparty.contract.delete。
	database.RecordAudit(c, database.AuditTargetThirdParty, id, "delete", nil)
	utils.Success(c, gin.H{"message": "已删除"})
}

// ==================== PDF 上传/下载 ====================

// UploadFile 上传 PDF（覆盖式：先存盘 → 算 3 哈希 → 更新文件字段；旧文件被删）
//
//	POST /api/third-party/contracts/:id/upload
//	body: { pdf_base64: "data:application/pdf;base64,..." | 纯 base64 }
//	返回：{ file_size, file_hash, file_sha256, file_combined_hash, file_path }
func (h *ThirdPartyHandler) UploadFile(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:upload") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.UploadFile] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权上传")
		return
	}

	var req struct {
		PDFBase64 string `json:"pdf_base64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}
	if req.PDFBase64 == "" {
		utils.BadRequest(c, "pdf_base64 不能为空")
		return
	}
	// 1. 解析 base64
	pdfBytes, err := decodePDFBase64(req.PDFBase64)
	if err != nil {
		utils.BadRequest(c, "pdf_base64 解析失败: "+err.Error())
		return
	}
	// 2. 限大小（Plan B：用 cfg.Upload.MaxSize 替代硬编码 20MB）
	var maxSize int64 = 20 * 1024 * 1024 // 兜底默认 20MB
	if config.GlobalConfig != nil && config.GlobalConfig.Upload.MaxSize > 0 {
		maxSize = config.GlobalConfig.Upload.MaxSize
	}
	if int64(len(pdfBytes)) > maxSize {
		utils.Err(c, utils.CodeMediaTooLarge,
			fmt.Sprintf("PDF 超过 %d MB（cfg.Upload.MaxSize）", maxSize/1024/1024))
		return
	}
	// 3. 校验 magic number：%PDF-
	if len(pdfBytes) < 5 || string(pdfBytes[:5]) != "%PDF-" {
		utils.BadRequest(c, "不是合法的 PDF 文件（magic number 不匹配）")
		return
	}
	// 4. 存盘
	storage := services.GetThirdPartyStorage()
	// 如果已有旧文件，先删
	if t.FilePath != "" {
		_ = storage.DeleteFile(t.FilePath)
	}
	// 用现有 snowid-like 路径（这里没 snowid，用 contract id 当路径片段）
	// 用 t.ContractNo 作为目录子项，便于按合同号浏览
	snowid := strings.TrimPrefix(t.ContractNo, "TP-")
	_, relPath, err := storage.SaveThirdPartyAsset(pdfBytes, snowid)
	if err != nil {
		utils.LogError("[third_party.UploadFile] 保存文件失败 id=%d snowid=%s: %v", id, snowid, err)
		utils.Err(c, utils.CodeInternal, "保存文件失败: "+err.Error())
		return
	}
	// 5. 算 3 哈希（写文件后直接用文件路径）
	absPath, pathErr := storage.GetAbsolutePath(relPath)
	if pathErr != nil {
		// P0 修复（2026-06-28）：路径非法 → 500（理论不可能，relPath 由 SaveThirdPartyAsset 生成）
		utils.LogError("[third_party.Upload] 路径非法 id=%d: %v", id, pathErr)
		utils.Err(c, utils.CodeInternal, "保存路径异常")
		return
	}
	hashService := services.NewHashService(true, true)
	hash, err := hashService.CalculateFileDualHash(absPath)
	if err != nil {
		// 文件已存但算 hash 失败：回滚
		_ = storage.DeleteFile(relPath)
		utils.LogError("[third_party.UploadFile] 算哈希失败 id=%d absPath=%s: %v", id, absPath, err)
		utils.Err(c, utils.CodeInternal, "算哈希失败: "+err.Error())
		return
	}
	// 6. 更新 DB
	if err := database.UpdateThirdPartyContractFile(
		id, relPath, int64(len(pdfBytes)),
		hash.SM3Hash, hash.SHA256Hash, hash.CombinedHash,
	); err != nil {
		_ = storage.DeleteFile(relPath)
		utils.LogError("[third_party.UploadFile] 更新文件字段失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "更新文件字段失败: "+err.Error())
		return
	}

	// 7. WORM（Phase 1 Critical #1）：PDF 落盘后立即锁定。
	// 失败不阻断业务流：PDF 已入库 + 落盘，锁失败属运维事件。
	// K.1：失败时写审计 + 上报业务事件指标 worm.lock.failed，便于告警 + 追溯缺锁文件。
	userID := c.GetInt64("user_id")
	if _, lockErr := services.LockOnce(absPath, snowid, userID, "thirdparty.contract.upload"); lockErr != nil {
		if errors.Is(lockErr, database.ErrAlreadyLocked) {
			utils.Warn("[第三方合同] WORM 已存在锁（重复上传）: snowid=%s", snowid)
		} else {
			utils.Warn("[第三方合同] WORM LockOnce 失败: snowid=%s, err=%v", snowid, lockErr)
			services.PublishEvent("worm.lock.failed")
			database.RecordAudit(c, database.AuditTargetPDFLock, id, "lock.failed", gin.H{
				"snowid":   snowid,
				"abs_path": absPath,
				"err":      lockErr.Error(),
				"reason":   "post_upload_lock_failed",
			})
		}
	}
	// Phase 6 (Critical #8)：pdf_lock 审计（WORM LockOnce 成功的事件记录）。
	// 即使 LockOnce 失败也尝试记 audit，便于事后追溯哪些 PDF 缺锁。
	database.RecordAudit(c, database.AuditTargetPDFLock, id, "lock", gin.H{
		"snowid":   snowid,
		"abs_path": absPath,
		"size":     len(pdfBytes),
		"sm3":      hash.SM3Hash,
		"sha256":   hash.SHA256Hash,
		"contract_id": id,
	})
	utils.Success(c, gin.H{
		"file_size":          len(pdfBytes),
		"file_sm3_hash":      hash.SM3Hash,
		"file_sha256_hash":   hash.SHA256Hash,
		"file_combined_hash": hash.CombinedHash,
		"file_path":          relPath,
	})
}

// DownloadFile 单文件下载（流式）
//
//	GET /api/third-party/contracts/:id/download
//
// owner-or-admin / data_scope 校验：非 owner 且 data_scope 不允许则 403。
func (h *ThirdPartyHandler) DownloadFile(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:download") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		utils.BadRequest(c, "无效的 id")
		return
	}
	t, err := database.GetThirdPartyContractByID(id)
	if err != nil {
		utils.LogError("[third_party.DownloadFile] 查询失败 id=%d: %v", id, err)
		utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
		return
	}
	if t == nil {
		utils.Err(c, utils.CodeContractNotFound, "合同不存在")
		return
	}
	if !h.thirdPartyAccess(c, t) {
		utils.Err(c, utils.CodeForbidden, "无权下载此合同")
		return
	}

	if t.FilePath == "" {
		utils.Err(c, utils.CodeNotFound, "该合同尚未上传文件")
		return
	}
	storage := services.GetThirdPartyStorage()
	absPath, err := storage.GetAbsolutePath(t.FilePath)
	if err != nil {
		// P0 修复（2026-06-28）：路径非法 → 404 不暴露内部细节
		utils.Warn("[third_party.Download] 路径非法 id=%d: %v", id, err)
		utils.Err(c, utils.CodeMediaNotFound, "文件不存在")
		return
	}
	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		utils.Err(c, utils.CodeMediaNotFound, "文件不存在")
		return
	}
	// 用 t.ContractNo 作为下载文件名
	filename := t.ContractNo + ".pdf"
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.File(absPath)
}

// BulkDownload 批量下载（流式 ZIP）
//
//	POST /api/third-party/contracts/bulk-download
//	body: { ids?: [], search?, status?, customer_id? }
//	返回：application/zip 流
func (h *ThirdPartyHandler) BulkDownload(c *gin.Context) {
	if !RequirePermission(c, "thirdparty:bulk-download") {
		return
	}
	var req struct {
		IDs        []int64 `json:"ids"`
		Search     string  `json:"search"`
		Status     string  `json:"status"`
		CustomerID int64   `json:"customer_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "无效的请求数据: "+err.Error())
		return
	}

	var ids []int64
	if len(req.IDs) > 0 {
		// 即便前端给了 ids 列表，也需二次校验：每个 id 都必须 data_scope 允许。
		// 否则非 admin 用户可绕过 list filter 直接拉指定 id 的 PDF。
		// 用 GetThirdPartyContractsByIDs 批量查（N+1 防护）；任何 id 不存在 / 权限拒绝 → 403。
		contracts, terr := database.GetThirdPartyContractsByIDs(req.IDs)
		if terr != nil {
			utils.LogError("[third_party.BulkDownload] GetThirdPartyContractsByIDs 失败 ids=%v: %v", req.IDs, terr)
			utils.Err(c, utils.CodeInternal, "查询失败: "+terr.Error())
			return
		}
		// 用 id 索引方便 O(1) 查找
		byID := make(map[int64]*models.ThirdPartyContract, len(contracts))
		for _, t := range contracts {
			byID[t.ID] = t
		}
		var deniedIDs []int64
		for _, id := range req.IDs {
			t, ok := byID[id]
			if !ok {
				utils.Warn("[third_party.BulkDownload] id 不存在或未查到 id=%d", id)
				deniedIDs = append(deniedIDs, id)
				continue
			}
			if !h.thirdPartyAccess(c, t) {
				deniedIDs = append(deniedIDs, id)
			}
		}
		if len(deniedIDs) > 0 {
			utils.Warn("[third_party.BulkDownload] 部分 id 拒绝访问 user_id=%d denied=%v allowed=%v",
				c.GetInt64("user_id"), deniedIDs, len(req.IDs)-len(deniedIDs))
			utils.Err(c, utils.CodeForbidden,
				fmt.Sprintf("无权访问 %d/%d 个合同 (被拒 ids=%v)", len(deniedIDs), len(req.IDs), deniedIDs))
			return
		}
		ids = req.IDs
	} else {
		// 按 search/status/customer_id 查时也走 data_scope 过滤，
		// 避免非 admin 用户用搜索条件拉走其他部门合同。
		// data_scope 拼接逻辑复用 ListContracts 的 listThirdPartyContractsScoped helper
		var list []*models.ThirdPartyContract
		var err error
		list, _, err = listThirdPartyContractsScoped(c, req.CustomerID, "", req.Status, req.Search, 1, 1000)
		if err != nil {
			utils.LogError("[third_party.BulkDownload] listThirdPartyContractsScoped 失败 customerID=%d status=%q search=%q: %v", req.CustomerID, req.Status, req.Search, err)
			utils.Err(c, utils.CodeInternal, "查询失败: "+err.Error())
			return
		}
		for _, t := range list {
			ids = append(ids, t.ID)
		}
	}

	if len(ids) == 0 {
		utils.Err(c, utils.CodeNotFound, "没有可下载的合同")
		return
	}

	// 拿文件信息
	files, err := database.ListThirdPartyContractFiles(ids)
	if err != nil {
		utils.LogError("[third_party.BulkDownload] ListThirdPartyContractFiles 失败 ids=%v: %v", ids, err)
		utils.Err(c, utils.CodeInternal, "查询文件失败: "+err.Error())
		return
	}
	if len(files) == 0 {
		utils.Err(c, utils.CodeNotFound, "没有可下载的文件")
		return
	}

	// 流式 ZIP
	storage := services.GetThirdPartyStorage()
	c.Header("Content-Type", "application/zip")
	filename := fmt.Sprintf("third_party_contracts_%s.zip", time.Now().Format("20060102_150405"))
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)

	// 简单的非流式 ZIP（文件数 < 100 时足够用；大文件场景可优化为 archive/zip 流式 writer）
	zipData, skipped, err := buildZipFromFiles(storage, files)
	if err != nil {
		utils.LogError("[third_party.BulkDownload] buildZipFromFiles 失败 files=%d: %v", len(files), err)
		utils.Err(c, utils.CodeInternal, "打包失败: "+err.Error())
		return
	}
	// P1 修复（2026-06-28）：若部分文件失败（路径非法 / 文件丢失），告知用户。
	//   - HTTP status 仍 200（zip 已返回，部分文件可用）
	//   - 用 X-Skipped-Ids 自定义 header 列出失败文件 id（前端实时提示）
	//   - skipped 元数据嵌入 ZIP 标准 EOCD comment 字段（不影响解压兼容性）
	//     客户端可解压后用 unzip -z / Python zipfile.comment 读取
	if len(skipped) > 0 {
		c.Header("X-Skipped-Ids", fmt.Sprintf("%v", skipped))
		utils.Warn("[third_party.BulkDownload] 部分文件跳过: ids=%v (已嵌入 EOCD comment)", skipped)
	}
	_, _ = io.Copy(c.Writer, strings.NewReader(string(zipData)))
}

// decodePDFBase64 解析 data:application/pdf;base64,xxx 或 纯 base64
func decodePDFBase64(s string) ([]byte, error) {
	const prefix = "data:application/pdf;base64,"
	if strings.HasPrefix(s, prefix) {
		s = s[len(prefix):]
	} else if strings.HasPrefix(s, "data:") {
		// 其它 data: 前缀（极少见），先去掉 ;base64,xxx
		if idx := strings.Index(s, ";base64,"); idx >= 0 {
			s = s[idx+len(";base64,"):]
		}
	}
	return base64.StdEncoding.DecodeString(s)
}

// ==================== 辅助 ====================

// buildZipEntry 构造一个 ZIP 局部文件头 + 文件数据 + 中央目录条目
// 用 reflect/zip 太重；这里手写一个极简版（不压缩，只 STORE）
type zipEntry struct {
	Name    string
	Data    []byte
	ModTime time.Time
}

func buildZipFromFiles(storage *services.ThirdPartyStorage, files []struct {
	ID         int64
	RelPath    string
	ContractNo string
}) ([]byte, []int64, error) {
	entries := make([]zipEntry, 0, len(files))
	// P1 修复（2026-06-28）：记录跳过的文件 id 列表，让调用方告知用户。
	var skipped []int64
	for _, f := range files {
		absPath, pathErr := storage.GetAbsolutePath(f.RelPath)
		if pathErr != nil {
			// P0 修复（2026-06-28）：路径非法（DB 篡改或损坏）→ 跳过该文件，不让批量下载失败
			utils.Warn("[third_party.buildZipFromFiles] 路径非法 id=%d: %v", f.ID, pathErr)
			skipped = append(skipped, f.ID)
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			utils.Warn("[third_party] 读取文件失败，跳过 id=%d (%s): %v", f.ID, absPath, err)
			skipped = append(skipped, f.ID)
			continue
		}
		entries = append(entries, zipEntry{
			Name:    f.ContractNo + ".pdf",
			Data:    data,
			ModTime: time.Now(),
		})
	}
	if len(entries) == 0 {
		return nil, skipped, fmt.Errorf("没有可打包的文件")
	}

	// P1 修复（2026-06-28）：若有跳过文件，把 skipped 列表序列化进 EOCD comment 字段。
	//   - 不在 ZIP body 末尾追加 JSON（会被部分严格解析器视为 ZIP 损坏）
	//   - EOCD comment 字段（最大 65535 字节）是 ZIP 标准定义的合法位置
	//   - 调用方（前端）可解压后从 ZIP comment 字段读取，或直接读 X-Skipped-Ids header
	var comment []byte
	if len(skipped) > 0 {
		commentJSON, err := json.Marshal(map[string]interface{}{
			"warning": "以下文件因路径异常或文件丢失未能包含",
			"ids":     skipped,
		})
		if err == nil {
			comment = commentJSON
		}
	}

	zipData, err := writeZip(entries, comment)
	return zipData, skipped, err
}

// writeZip 极简 ZIP 编码（STORE 模式，不压缩）
// 格式：local file header + file data + central directory + EOCD [+ comment]
// 参考：https://en.wikipedia.org/wiki/ZIP_(file_format)
//
// 字段偏移严格按 PKWARE APPNOTE 6.3.x：
//
//	Local File Header (LFH) 30 bytes + name
//	Central Directory Header (CDH) 46 bytes + name
//	EOCD 22 bytes + comment (0-65535 bytes)
//
// ⚠️ 注意：之前版本所有 putUint16/putUint32 调用偏移错了 2-4 字节（漏了 compression method），
// 导致 LFH/CDH 字段全部错位。Windows 资源管理器/7-Zip 容错读取还能解压，
// 但 .NET ZipFile、macOS Archive Utility 等严格解析器会失败（entries 数为 0 或字段全 0）。
// 详见 2026-06-26 phase 15.5 E2E 验证发现的 server-side bug fix。
//
// 参数 comment（可选）：嵌入 EOCD 的 archive comment 字段，最大 65535 字节。
//   - P1 修复（2026-06-28）：用于嵌入 skipped 文件列表的 JSON 元数据，
//     避免在 body 末尾追加导致部分严格解析器失败。
//   - 主流解压工具都能正确读取 comment（unzip -z、Python zipfile.comment、Windows 资源管理器属性）
func writeZip(entries []zipEntry, comment []byte) ([]byte, error) {
	const (
		sigLFH  = 0x04034b50 // local file header signature
		sigCDH  = 0x02014b50 // central directory header signature
		sigEOCD = 0x06054b50 // end of central directory signature
	)
	var buf []byte
	centralDir := []byte{}

	for _, e := range entries {
		nameBytes := []byte(e.Name)
		dosTime, dosDate := timeToDos(e.ModTime)
		dataLen := uint32(len(e.Data))
		crc := crc32IEEE(e.Data)
		// local header offset = len(buf) at this point (before appending LFH + data)
		relativeOffset := uint32(len(buf))

		// ============ Local File Header (30 bytes + name) ============
		// 0-3   signature
		// 4-5   version needed (2.0)
		// 6-7   flags (0)
		// 8-9   compression method (0 = STORE)
		// 10-11 last mod file time
		// 12-13 last mod file date
		// 14-17 CRC-32
		// 18-21 compressed size
		// 22-25 uncompressed size
		// 26-27 file name length
		// 28-29 extra field length (0)
		// 30+   file name
		lfh := make([]byte, 30+len(nameBytes))
		putUint32(lfh[0:], sigLFH)
		putUint16(lfh[4:], 20)
		putUint16(lfh[6:], 0)
		putUint16(lfh[8:], 0)
		putUint16(lfh[10:], dosTime)
		putUint16(lfh[12:], dosDate)
		putUint32(lfh[14:], crc)
		putUint32(lfh[18:], dataLen)
		putUint32(lfh[22:], dataLen)
		putUint16(lfh[26:], uint16(len(nameBytes)))
		putUint16(lfh[28:], 0)
		copy(lfh[30:], nameBytes)
		buf = append(buf, lfh...)
		buf = append(buf, e.Data...)

		// ============ Central Directory Header (46 bytes + name) ============
		// 0-3    signature
		// 4-5    version made by (20 = MS-DOS)
		// 6-7    version needed (20 = 2.0)
		// 8-9    flags (0)
		// 10-11  compression method (0 = STORE)
		// 12-13  last mod file time
		// 14-15  last mod file date
		// 16-19  CRC-32
		// 20-23  compressed size
		// 24-27  uncompressed size
		// 28-29  file name length
		// 30-31  extra field length (0)
		// 32-33  file comment length (0)
		// 34-35  disk number start (0)
		// 36-37  internal file attributes (0)
		// 38-41  external file attributes (0)
		// 42-45  relative offset of local header
		// 46+    file name
		cdh := make([]byte, 46+len(nameBytes))
		putUint32(cdh[0:], sigCDH)
		putUint16(cdh[4:], 20)
		putUint16(cdh[6:], 20)
		putUint16(cdh[8:], 0)
		putUint16(cdh[10:], 0)
		putUint16(cdh[12:], dosTime)
		putUint16(cdh[14:], dosDate)
		putUint32(cdh[16:], crc)
		putUint32(cdh[20:], dataLen)
		putUint32(cdh[24:], dataLen)
		putUint16(cdh[28:], uint16(len(nameBytes)))
		putUint16(cdh[30:], 0)
		putUint16(cdh[32:], 0)
		putUint16(cdh[34:], 0)
		putUint16(cdh[36:], 0)
		putUint32(cdh[38:], 0)
		putUint32(cdh[42:], relativeOffset)
		copy(cdh[46:], nameBytes)
		centralDir = append(centralDir, cdh...)
	}

	// ============ End of Central Directory Record (EOCD, 22 bytes + comment) ============
	// EOCD comment 字段（最大 65535 字节，ZIP 标准合法位置）。
	// 用于嵌入 skipped 文件列表等元数据，不影响 ZIP 解压兼容性。
	commentLen := 0
	if len(comment) > 0 {
		// ZIP 规范：comment 长度字段为 uint16，最大 65535
		if len(comment) > 0xFFFF {
			utils.Warn("[writeZip] comment 超长 (%d bytes)，截断到 65535", len(comment))
			comment = comment[:0xFFFF]
		}
		commentLen = len(comment)
	}
	eocd := make([]byte, 22+commentLen)
	putUint32(eocd[0:], sigEOCD)
	putUint16(eocd[4:], 0)
	putUint16(eocd[6:], 0)
	putUint16(eocd[8:], uint16(len(entries)))
	putUint16(eocd[10:], uint16(len(entries)))
	putUint32(eocd[12:], uint32(len(centralDir)))
	putUint32(eocd[16:], uint32(len(buf)))
	putUint16(eocd[20:], uint16(commentLen))
	if commentLen > 0 {
		copy(eocd[22:], comment)
	}
	buf = append(buf, centralDir...)
	buf = append(buf, eocd...)
	return buf, nil
}

func putUint16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func putUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// timeToDos 把 time.Time 转为 MS-DOS 日期/时间
func timeToDos(t time.Time) (uint16, uint16) {
	// DOS 时间：5 小时位 | 6 分钟位 | 5 秒/2 位
	dosTime := uint16(t.Hour()<<11) | uint16(t.Minute()<<5) | uint16(t.Second()/2)
	// DOS 日期：7 年-1980 位 | 4 月位 | 5 日位
	year := t.Year() - 1980
	if year < 0 {
		year = 0
	}
	if year > 127 {
		year = 127
	}
	dosDate := uint16(year<<9) | uint16(t.Month()<<5) | uint16(t.Day())
	return dosTime, dosDate
}

// crc32IEEE 标准 CRC-32（IEEE 802.3 多项式 0xEDB88320）
var crc32Table = makeCrc32Table()

func makeCrc32Table() [256]uint32 {
	const poly = 0xEDB88320
	var t [256]uint32
	for i := 0; i < 256; i++ {
		c := uint32(i)
		for j := 0; j < 8; j++ {
			if c&1 != 0 {
				c = poly ^ (c >> 1)
			} else {
				c = c >> 1
			}
		}
		t[i] = c
	}
	return t
}

func crc32IEEE(data []byte) uint32 {
	c := uint32(0xFFFFFFFF)
	for _, b := range data {
		c = crc32Table[byte(c)^b] ^ (c >> 8)
	}
	return c ^ 0xFFFFFFFF
}

// 避免 goimports 误删 json import（用于 body 解析）
var _ = json.Marshal
