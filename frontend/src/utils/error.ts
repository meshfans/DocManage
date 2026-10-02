/**
 * 错误码 i18n 映射（前端）
 *
 * 与 backend/utils/errors.go 的常量一一对应。
 * 维护规则：
 *   - 后端新增 ErrCode 常量 → 前端必须在 I18N.zh / I18N.en 加映射
 *   - 未知 code → 兜底显示后端 message（不回退到英文）
 *   - 前端不应直接用 `error.message` 解析（i18n 优先）
 *
 * 用法：
 *   import { translateError } from "@/utils/error";
 *   const text = translateError(code, "zh-CN"); // 返回中文文案
 *   ElMessage.error(text);
 */

export type Locale = "zh-CN" | "en-US";

/**
 * 错误码 → 多语言文案。
 *
 * 设计点：
 *   - 与后端 utils/errors.go 的 code 字符串完全一致
 *   - 未列出的 code（例如老后端版本）→ 兜底返回 null，调用方回退到 message
 *   - 不写兜底文案（"未知错误"等），避免误导
 */
const I18N: Record<string, Record<Locale, string>> = {
  // ===== common =====
  "common.internal": {
    "zh-CN": "服务器内部错误，请稍后重试",
    "en-US": "Internal server error, please try again later"
  },
  "common.invalid_param": {
    "zh-CN": "请求参数不合法",
    "en-US": "Invalid request parameters"
  },
  "common.unauthorized": {
    "zh-CN": "请先登录",
    "en-US": "Please sign in first"
  },
  "common.forbidden": {
    "zh-CN": "权限不足",
    "en-US": "Permission denied"
  },
  "common.not_found": {
    "zh-CN": "资源不存在",
    "en-US": "Resource not found"
  },
  // ===== 2026-10-01 C1 统一：utils.defaultCodeForStatus 的 HTTP 兜底码 =====
  // 后端 Response.Code 在未显式调 utils.Err(c, code, msg) 时按 HTTP 状态填这些码
  "common.conflict": {
    "zh-CN": "资源冲突",
    "en-US": "Resource conflict"
  },
  // ===== 2026-10-01 C1 统一：utils.defaultCodeForStatus 的 HTTP 兜底码 =====
  "common.unprocessable": {
    "zh-CN": "请求无法处理",
    "en-US": "Unprocessable request"
  },
  "common.too_many_requests": {
    "zh-CN": "请求过于频繁，请稍后重试",
    "en-US": "Too many requests, please try again later"
  },
  "internal.error": {
    "zh-CN": "服务器内部错误，请稍后重试",
    "en-US": "Internal server error, please try again later"
  },
  "internal.unavailable": {
    "zh-CN": "服务暂时不可用，请稍后重试",
    "en-US": "Service temporarily unavailable, please try again later"
  },
  "common.rate_limited": {
    "zh-CN": "请求过于频繁，请稍后再试",
    "en-US": "Too many requests, please try later"
  },
  "common.maintenance": {
    "zh-CN": "系统维护中，请稍后再试",
    "en-US": "System under maintenance, please try later"
  },
  "common.method_not_allowed": {
    "zh-CN": "请求方法不被允许",
    "en-US": "Method not allowed"
  },

  // ===== auth =====
  "auth.invalid_credentials": {
    "zh-CN": "用户名或密码错误",
    "en-US": "Invalid username or password"
  },
  "auth.user_not_found": {
    "zh-CN": "用户不存在",
    "en-US": "User not found"
  },
  "auth.user_disabled": {
    "zh-CN": "账号已停用，请联系管理员",
    "en-US": "Account disabled, please contact admin"
  },
  "auth.password_expired": {
    "zh-CN": "密码已过期，请修改密码",
    "en-US": "Password expired, please change it"
  },
  "auth.password_weak": {
    "zh-CN": "密码强度不足",
    "en-US": "Password is too weak"
  },
  "auth.password_reused": {
    "zh-CN": "新密码不能与最近 5 次相同",
    "en-US": "New password cannot match the last 5"
  },
  "auth.token_invalid": {
    "zh-CN": "登录凭证无效",
    "en-US": "Invalid token"
  },
  "auth.token_expired": {
    "zh-CN": "登录已过期，请重新登录",
    "en-US": "Session expired, please sign in again"
  },
  "auth.token_revoked": {
    "zh-CN": "登录已注销，请重新登录",
    "en-US": "Token revoked, please sign in again"
  },
  "auth.refresh_expired": {
    "zh-CN": "刷新凭证已过期，请重新登录",
    "en-US": "Refresh token expired, please sign in again"
  },
  "auth.totp_required": {
    "zh-CN": "需要输入二次验证码",
    "en-US": "Two-factor code required"
  },
  "auth.totp_invalid": {
    "zh-CN": "二次验证码错误",
    "en-US": "Invalid two-factor code"
  },
  "auth.signature_invalid": {
    "zh-CN": "API 签名错误",
    "en-US": "Invalid API signature"
  },
  "auth.signature_expired": {
    "zh-CN": "API 签名已过期",
    "en-US": "API signature expired"
  },

  // ===== rbac =====
  "rbac.role_not_found": {
    "zh-CN": "角色不存在",
    "en-US": "Role not found"
  },
  "rbac.role_exists": {
    "zh-CN": "角色已存在",
    "en-US": "Role already exists"
  },
  "rbac.permission_not_found": {
    "zh-CN": "权限不存在",
    "en-US": "Permission not found"
  },
  "rbac.permission_exists": {
    "zh-CN": "权限已存在",
    "en-US": "Permission already exists"
  },
  "rbac.user_binding_invalid": {
    "zh-CN": "用户角色绑定参数非法",
    "en-US": "Invalid user-role binding"
  },

  // ===== customer / contract =====
  "customer.not_found": {
    "zh-CN": "客户不存在",
    "en-US": "Customer not found"
  },
  "customer.exists": {
    "zh-CN": "客户已存在",
    "en-US": "Customer already exists"
  },
  "customer.invalid": {
    "zh-CN": "客户信息校验失败",
    "en-US": "Customer validation failed"
  },
  "customer.has_contracts": {
    "zh-CN": "客户下仍有合同，无法删除",
    "en-US": "Customer has contracts, cannot delete"
  },
  "contract.not_found": {
    "zh-CN": "合同不存在",
    "en-US": "Contract not found"
  },
  "contract.locked": {
    "zh-CN": "合同已锁定（PDF WORM），无法修改",
    "en-US": "Contract locked (PDF WORM), cannot modify"
  },
  "contract.expired": {
    "zh-CN": "合同已过期",
    "en-US": "Contract expired"
  },

  // ===== media / signature / seal =====
  "media.not_found": {
    "zh-CN": "媒体文件不存在",
    "en-US": "Media not found"
  },
  "media.too_large": {
    "zh-CN": "媒体文件超过大小限制",
    "en-US": "Media file too large"
  },
  "media.type_denied": {
    "zh-CN": "媒体类型不被允许",
    "en-US": "Media type not allowed"
  },
  "media.corrupted": {
    "zh-CN": "媒体文件已损坏",
    "en-US": "Media file corrupted"
  },
  "signature.not_found": {
    "zh-CN": "签名记录不存在",
    "en-US": "Signature not found"
  },
  "signature.invalid": {
    "zh-CN": "签名数据非法",
    "en-US": "Invalid signature"
  },
  "signature.replayed": {
    "zh-CN": "签名已被使用（防重放命中）",
    "en-US": "Signature replayed"
  },
  "seal.not_found": {
    "zh-CN": "印章不存在",
    "en-US": "Seal not found"
  },
  "seal.revoked": {
    "zh-CN": "印章已撤销",
    "en-US": "Seal revoked"
  },
  "seal.owner_invalid": {
    "zh-CN": "印章使用人非法",
    "en-US": "Invalid seal owner"
  },

  // ===== thirdparty =====
  "thirdparty.not_found": {
    "zh-CN": "第三方合同不存在",
    "en-US": "Third-party contract not found"
  },
  "thirdparty.status_change_invalid": {
    "zh-CN": "合同状态流转非法",
    "en-US": "Invalid status transition"
  },
  "thirdparty.upload_failed": {
    "zh-CN": "第三方合同上传失败",
    "en-US": "Third-party upload failed"
  },

  // ===== backup / audit =====
  "backup.not_found": {
    "zh-CN": "备份不存在",
    "en-US": "Backup not found"
  },
  "backup.corrupted": {
    "zh-CN": "备份已损坏",
    "en-US": "Backup corrupted"
  },
  "backup.missing": {
    "zh-CN": "备份文件缺失",
    "en-US": "Backup file missing"
  },
  "backup.in_progress": {
    "zh-CN": "已有备份任务在执行",
    "en-US": "Another backup is in progress"
  },
  "backup.restore_failed": {
    "zh-CN": "备份还原失败",
    "en-US": "Restore failed"
  },
  "backup.manifest_invalid": {
    "zh-CN": "备份清单解析失败",
    "en-US": "Invalid backup manifest"
  },
  "backup.integrity_failed": {
    "zh-CN": "备份完整性校验失败",
    "en-US": "Backup integrity check failed"
  },
  "audit.chain_broken": {
    "zh-CN": "审计链断裂，请检查详情",
    "en-US": "Audit chain broken, please check details"
  },
  "audit.write_failed": {
    "zh-CN": "审计写入失败（已 warn，不影响业务）",
    "en-US": "Audit write failed (warn only)"
  },
  "audit.reconcile_empty": {
    "zh-CN": "审计表为空",
    "en-US": "Audit table is empty"
  },

  // ===== system / scheduler =====
  "scheduler.running": {
    "zh-CN": "调度任务已在执行",
    "en-US": "Scheduled task already running"
  },
  "scheduler.full": {
    "zh-CN": "调度任务队列已满",
    "en-US": "Scheduler queue full"
  },
  "system.disk_full": {
    "zh-CN": "磁盘空间不足",
    "en-US": "Disk full"
  },
  "system.dependency_missing": {
    "zh-CN": "系统依赖缺失",
    "en-US": "System dependency missing"
  },

  // ===== reminder (Phase 3c 补全) =====
  "reminder.template_not_found": {
    "zh-CN": "提醒模板不存在",
    "en-US": "Reminder template not found"
  },
  "reminder.subscription_not_found": {
    "zh-CN": "提醒订阅不存在",
    "en-US": "Reminder subscription not found"
  },
  "reminder.permission_denied": {
    "zh-CN": "无权操作该提醒订阅",
    "en-US": "No permission for this reminder subscription"
  }
};

/**
 * 翻译错误码。
 *
 * @param code  后端响应里的 code（字符串，可选 undefined）
 * @param locale 语言，默认 zh-CN
 * @returns 找到的多语言文案；未找到返回 null（调用方应 fallback 到 message）
 */
export function translateError(
  code: string | undefined,
  locale: Locale = "zh-CN"
): string | null {
  if (!code) return null;
  const entry = I18N[code];
  if (!entry) return null;
  return entry[locale] ?? entry["zh-CN"] ?? null;
}

/**
 * 从 axios 错误对象里提取友好文案。
 * 优先按 code 翻译；翻译失败时再回退到后端 message。
 *
 * ⚠️ 2026-08-19 M-E4 修复后契约：
 *   - 后端响应 envelope = { success, message, error, code, detail }
 *   - error / detail = wrapped error 字符串（给开发者看）
 *   - code         = 归一化错误码（给前端 i18n 翻译用）
 *   - message      = 业务文案（兜底）
 *
 * 用法：
 *   try { await api.x() } catch(e) { ElMessage.error(extractErrorMessage(e)) }
 */
export function extractErrorMessage(
  err: any,
  locale: Locale = "zh-CN"
): string {
  // axios 错误：err.response.data = { success, code, message, detail, error }
  const data = err?.response?.data;
  if (data) {
    // 优先用 code 字段（后端标准化错误码）；若后端未迁移（极少数老端点），
    // 兜底用 error 字段——但在 M-E4 修复后，error 字段是 wrapped error 字符串，
    // 大概率 i18n 查不到，所以这里"data.error"fallback 仅作为"老后端"兼容。
    const code = data.code || data.error;
    const translated = translateError(code, locale);
    if (translated) return translated;
    if (data.message) return data.message;
  }
  // 非 axios 错误（网络断开、CORS 等）
  if (err?.message) return err.message;
  return locale === "zh-CN" ? "请求失败" : "Request failed";
}
