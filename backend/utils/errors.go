package utils

// ==================== 统一错误码字典（Round 18）====================
//
// 设计目标：
//  1. 业务层调 ErrXxx(c, err) 时，前端按 code 文案翻译，统一处理
//  2. code 命名规范：<module>.<semantic>，避免散落字符串
//  3. 与现有 utils.Error / utils.ErrorWithDetail 并存：
//     - 通用错误（参数缺失、解析失败）仍走 utils.Error(message)
//     - 业务语义错误（密码错误、权限不足）走 utils.Err(c, CodeXxx, msg)
//
// 命名规范：
//   - 顶层 = 模块分类：auth / rbac / customer / contract / media / signature /
//                          seal / backup / audit / thirdparty / system / common
//   - 二级 = 语义：not_found / invalid / exists / locked / forbidden / expired / ...
//   - 例：auth.password.invalid / rbac.role.not_found / customer.exists
//
// 与 i18n 配套：
//   - 前端 `frontend/src/utils/error.ts` 维护一张 map：code → zh-CN / en-US 文案
//   - 后端不在响应里塞"英文硬编码"，只发 code（必要时附 detail）
//
// 演进规则：
//   - 新增 code 必须在该文件追加，附简短注释（业务场景）
//   - 删除/重命名 code 必须走 ADR 或本文件留下 Deprecated 标记
//   - 命名 level 用小写 + 点号（与 prom label 风格一致）
// ----------------------------------------------------------------------------

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrCode 错误码字符串类型。
// 选 string 而非 int 的原因：
//   - 可读性：日志 / 调试一眼能看懂
//   - 演进性：新增 code 不挤占旧 code 编号
//   - 跨系统：前端 / 移动端 / 第三方集成对字符串友好
type ErrCode string

// ==================== 通用（common）====================

const (
	CodeInternal         ErrCode = "common.internal"
	CodeInvalidParam      ErrCode = "common.invalid_param"
	CodeUnauthorized      ErrCode = "common.unauthorized"
	CodeForbidden         ErrCode = "common.forbidden"
	CodeNotFound          ErrCode = "common.not_found"
	CodeConflict          ErrCode = "common.conflict"
	CodeRateLimited       ErrCode = "common.rate_limited"
	CodeMaintenance       ErrCode = "common.maintenance"
	CodeMethodNotAllowed  ErrCode = "common.method_not_allowed"
)

// ==================== 认证（auth）====================

const (
	CodeAuthInvalidCredentials ErrCode = "auth.invalid_credentials"
	CodeAuthUserNotFound       ErrCode = "auth.user_not_found"
	CodeAuthUserDisabled       ErrCode = "auth.user_disabled"
	CodeAuthPasswordExpired    ErrCode = "auth.password_expired"
	CodeAuthPasswordWeak       ErrCode = "auth.password_weak"
	CodeAuthPasswordReused     ErrCode = "auth.password_reused"
	CodeAuthTokenInvalid       ErrCode = "auth.token_invalid"
	CodeAuthTokenExpired       ErrCode = "auth.token_expired"
	CodeAuthTokenRevoked       ErrCode = "auth.token_revoked"
	CodeAuthRefreshExpired     ErrCode = "auth.refresh_expired"
	CodeAuthTOTPRequired       ErrCode = "auth.totp_required"
	CodeAuthTOTPInvalid        ErrCode = "auth.totp_invalid"
	CodeAuthSignatureInvalid   ErrCode = "auth.signature_invalid"
	CodeAuthSignatureExpired   ErrCode = "auth.signature_expired"
)

// ==================== RBAC（rbac）====================

const (
	CodeRBACRoleNotFound       ErrCode = "rbac.role_not_found"
	CodeRBACRoleExists         ErrCode = "rbac.role_exists"
	CodeRBACPermissionNotFound ErrCode = "rbac.permission_not_found"
	CodeRBACPermissionExists   ErrCode = "rbac.permission_exists"
	CodeRBACUserBindingInvalid ErrCode = "rbac.user_binding_invalid"
)

// ==================== 客户 / 合同 / 媒体 / 签名 / 印章 / 第三方 ====================

const (
	CodeCustomerNotFound     ErrCode = "customer.not_found"
	CodeCustomerExists       ErrCode = "customer.exists"
	CodeCustomerInvalid      ErrCode = "customer.invalid"
	CodeCustomerHasContracts ErrCode = "customer.has_contracts"

	CodeContractNotFound ErrCode = "contract.not_found"
	CodeContractLocked   ErrCode = "contract.locked"
	CodeContractExpired  ErrCode = "contract.expired"

	CodeMediaNotFound  ErrCode = "media.not_found"
	CodeMediaTooLarge  ErrCode = "media.too_large"
	CodeMediaTypeDeny  ErrCode = "media.type_denied"
	CodeMediaCorrupted ErrCode = "media.corrupted"

	CodeSignatureNotFound ErrCode = "signature.not_found"
	CodeSignatureInvalid  ErrCode = "signature.invalid"
	CodeSignatureReplayed ErrCode = "signature.replayed"

	CodeSealNotFound     ErrCode = "seal.not_found"
	CodeSealRevoked      ErrCode = "seal.revoked"
	CodeSealOwnerInvalid ErrCode = "seal.owner_invalid"

	CodeThirdPartyNotFound     ErrCode = "thirdparty.not_found"
	CodeThirdPartyStatusChange ErrCode = "thirdparty.status_change_invalid"
	CodeThirdPartyUploadFailed ErrCode = "thirdparty.upload_failed"
)

// ==================== 备份 / 还原 / 审计 ====================

const (
	CodeBackupNotFound      ErrCode = "backup.not_found"
	CodeBackupCorrupted     ErrCode = "backup.corrupted"
	CodeBackupMissing       ErrCode = "backup.missing"
	CodeBackupInProgress    ErrCode = "backup.in_progress"
	CodeRestoreFailed       ErrCode = "backup.restore_failed"
	CodeRestoreManifest     ErrCode = "backup.manifest_invalid"
	CodeRestoreIntegrity    ErrCode = "backup.integrity_failed"
	CodeAuditChainBroken    ErrCode = "audit.chain_broken"
	CodeAuditWriteFailed    ErrCode = "audit.write_failed"
	CodeAuditReconcileEmpty ErrCode = "audit.reconcile_empty"
)

// ==================== 提醒（reminder）====================
// Phase 3c (Critical #6) 补全。
const (
	CodeReminderTemplateNotFound     ErrCode = "reminder.template_not_found"
	CodeReminderSubscriptionNotFound ErrCode = "reminder.subscription_not_found"
	CodeReminderPermissionDenied     ErrCode = "reminder.permission_denied"
)

// ==================== 系统 / 调度 / 限流 ====================

const (
	CodeSchedulerRunning ErrCode = "scheduler.running"
	CodeSchedulerFull    ErrCode = "scheduler.full"
	CodeSystemDiskFull   ErrCode = "system.disk_full"
	CodeSystemDepMissing ErrCode = "system.dependency_missing"
)

// ==================== License / 模块门禁（2026-09-28）====================
//
// 设计：RAG / OCR 等"插件型"模块在 license 未签发对应 feature 时，
// 中间件 FeatureGate 直接返 403 + CodeLicenseFeatureNotLicensed，
// 前端 i18n 按此 code 翻译（"当前 license 未授权此模块"，不暴露 license 字眼）。
const (
	// CodeLicenseFeatureNotLicensed license 未签发此模块 feature。
	// 用途：Gin 中间件 FeatureGate(key) 在 sdk.GlobalHasFeature(key)=false 时返。
	// 配套模块 key：rag / ocr（仅业务实际消费的 key；payload features 数组里的元素仅作为白名单精确匹配）。
	CodeLicenseFeatureNotLicensed ErrCode = "license.feature_not_licensed"
)

// ==================== CRM（DocCRM MVP 2026-09-15 接入）====================
//
// 2026-10-02 L-3 注释：以下 CRM 错误码为 DocCRM 同步保留，本项目（DocManageTrail）
// 不会触发。保留原因：
//   - 多项目共用 utils 包，便于上游中间件 / 跨项目引用对齐
//   - future-proof：若后续 DocManageTrail 接入 CRM 模块可直接复用
//   - i18n 表（frontend/src/utils/error.ts）按 code 命中，未命中的 code 不影响前端
//
// 命名规范：crm.<module>.<semantic>
//   - module：contact / lead / opportunity / activity / customer_meta / dashboard
//   - semantic：not_found / invalid / stage_invalid / recycled / already_converted / ...
const (
	// 联系人
	CodeCRMContactNotFound ErrCode = "crm.contact.not_found"
	CodeCRMContactInvalid  ErrCode = "crm.contact.invalid"

	// 线索
	CodeCRMLeadNotFound         ErrCode = "crm.lead.not_found"
	CodeCRMLeadInvalid          ErrCode = "crm.lead.invalid"
	CodeCRMLeadRecycled         ErrCode = "crm.lead.recycled"
	CodeCRMLeadAlreadyConverted ErrCode = "crm.lead.already_converted"
	CodeCRMLeadNotInPool        ErrCode = "crm.lead.not_in_pool"
	CodeCRMLeadProtected        ErrCode = "crm.lead.protected"

	// 商机
	CodeCRMOpportunityNotFound    ErrCode = "crm.opportunity.not_found"
	CodeCRMOpportunityInvalid     ErrCode = "crm.opportunity.invalid"
	CodeCRMOpportunityStageInvalid ErrCode = "crm.opportunity.stage_invalid"
	CodeCRMOpportunityClosed      ErrCode = "crm.opportunity.closed"

	// 活动
	CodeCRMActivityNotFound  ErrCode = "crm.activity.not_found"
	CodeCRMActivityInvalid   ErrCode = "crm.activity.invalid"
	CodeCRMActivityTargetInvalid ErrCode = "crm.activity.target_invalid"

	// 客户 CRM 元数据
	CodeCRMCustomerMetaNotFound ErrCode = "crm.customer_meta.not_found"
	CodeCRMCustomerMetaInvalid  ErrCode = "crm.customer_meta.invalid"
)

// ==================== 错误码 → HTTP 状态 ====================

// httpStatusFor 将错误码映射到 HTTP 状态码。
// 业务设计：
//   - 401：未登录 / 凭证错 / token 问题
//   - 403：已登录但权限不足
//   - 404：资源不存在
//   - 409：资源冲突（重复）
//   - 410：资源已锁定 / 已过期 / 已撤销（语义性 Gone）
//   - 429：限流
//   - 503：维护模式
//   - 500：服务器侧错误（DB / 内部 panic / 审计链断裂 / 备份损坏）
//   - 400：参数/业务校验失败
func httpStatusFor(code ErrCode) int {
	switch code {
	case CodeInternal, CodeSystemDepMissing, CodeSystemDiskFull,
		CodeAuditWriteFailed, CodeAuditChainBroken,
		CodeBackupCorrupted, CodeBackupMissing, CodeRestoreFailed,
		CodeRestoreManifest, CodeRestoreIntegrity, CodeThirdPartyUploadFailed:
		return http.StatusInternalServerError
	case CodeInvalidParam, CodeAuthPasswordWeak, CodeAuthPasswordReused,
		CodeAuthSignatureInvalid, CodeAuthSignatureExpired,
		CodeAuthTOTPRequired, CodeAuthTOTPInvalid,
		CodeMediaTooLarge, CodeMediaTypeDeny,
		CodeSignatureInvalid, CodeSignatureReplayed,
		CodeSealOwnerInvalid, CodeThirdPartyStatusChange,
		CodeRBACUserBindingInvalid, CodeCustomerInvalid,
		CodeCustomerHasContracts, CodeBackupInProgress,
		CodeSchedulerRunning, CodeSchedulerFull:
		return http.StatusBadRequest
	case CodeUnauthorized, CodeAuthInvalidCredentials, CodeAuthUserNotFound,
		CodeAuthUserDisabled, CodeAuthPasswordExpired,
		CodeAuthTokenInvalid, CodeAuthTokenExpired, CodeAuthTokenRevoked,
		CodeAuthRefreshExpired:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeLicenseFeatureNotLicensed:
		// FeatureGate 中间件失败：license 未签发此模块。
		// 已登录、有权限，但 license 不带该 feature → 403。
		return http.StatusForbidden
	case CodeNotFound, CodeCustomerNotFound, CodeContractNotFound,
		CodeMediaNotFound, CodeMediaCorrupted, CodeSignatureNotFound,
		CodeSealNotFound, CodeThirdPartyNotFound,
		CodeBackupNotFound, CodeAuditReconcileEmpty,
		CodeRBACRoleNotFound, CodeRBACPermissionNotFound,
		// Phase 3c 补全（reminder 模块）
		CodeReminderTemplateNotFound, CodeReminderSubscriptionNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeCustomerExists,
		CodeRBACRoleExists, CodeRBACPermissionExists:
		return http.StatusConflict
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeMaintenance:
		return http.StatusServiceUnavailable
	case CodeContractLocked, CodeContractExpired, CodeSealRevoked:
		return http.StatusGone
	case CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	default:
		return http.StatusBadRequest
	}
}

// ==================== 业务层 API ====================

// Err 写入一个业务语义错误（带 error code）。
//
// code：见上方常量
// message：人类可读描述（中文或英文，前端 i18n 优先按 code 翻译）
// detail：可选的具体错误（例：wrapped error），用于日志 / 调试
//
// 响应体格式：
//
//	{
//	  "success": false,
//	  "message": "密码错误",
//	  "error":   "bcrypt mismatch",        // 兼容 ErrorWithDetail：底层 wrapped error
//	  "code":    "auth.invalid_credentials",// 新增：归一化错误码，供前端 i18n 翻译
//	  "detail":  "bcrypt mismatch"          // 同 error（保留以便日志/调试分开使用）
//	}
//
// 兼容既有 envelope：
//   - `error` 字段语义 = wrapped error 字符串（与 utils.ErrorWithDetail 一致）
//   - 新增 `code` 字段 = 归一化错误码（前端 i18n 优先用这个）
//   - `detail` 字段 = 同 error，进一步冗余（与 utils.ErrorWithDetail 同义）
// ⚠️ 2026-08-19 M-E4 修复：之前误把 code 字符串塞进 `error` 字段，
//    会导致前端显示 "auth.invalid_credentials" 而非业务文案。
//    修正：`error` 回到 wrapped error 字符串；code 单独存。
func Err(c *gin.Context, code ErrCode, message string, detail ...error) {
	var d string
	if len(detail) > 0 && detail[0] != nil {
		d = detail[0].Error()
	}
	status := httpStatusFor(code)
	c.JSON(status, gin.H{
		"success": false,
		"message": message,
		"error":   d,                       // wrapped error 字符串（兼容 ErrorWithDetail）
		"code":    string(code),            // 归一化错误码（前端 i18n）
		"detail":  d,                       // 冗余：与 error 同义，便于日志 / 调试拆分
	})
}

// ErrWithExtras 在 utils.Err 响应包络基础上追加额外字段。
//
// L.4 新增：登录失败响应需要带 remaining_attempts / locked_until 给前端展示
// 剩余尝试次数与解锁时间，但 utils.Err 签名固定不能塞业务字段。
// extras 中的 key/value 会合并到响应根级（与 code/message 同层），覆盖同名默认键。
//
// 兼容：未传 extras（nil 或空 map）时响应与 utils.Err 完全一致。
func ErrWithExtras(c *gin.Context, code ErrCode, message string, extras gin.H, detail ...error) {
	var d string
	if len(detail) > 0 && detail[0] != nil {
		d = detail[0].Error()
	}
	status := httpStatusFor(code)
	resp := gin.H{
		"success": false,
		"message": message,
		"error":   d,
		"code":    string(code),
		"detail":  d,
	}
	for k, v := range extras {
		resp[k] = v
	}
	c.JSON(status, resp)
}

// ErrInternal 简化调用：code 必传；message 缺省时用 code 字符串。
func ErrInternal(c *gin.Context, code ErrCode, detail ...error) {
	msg := string(code)
	if len(detail) > 0 && detail[0] != nil {
		msg = detail[0].Error()
	}
	Err(c, code, msg, detail...)
}
