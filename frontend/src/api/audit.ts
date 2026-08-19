/**
 * 审计日志 API（admin only）
 *
 * 后端：handlers/audit.go + database/audit_log.go
 *   GET  /api/audit            — 列表（分页 + 过滤）
 *   POST /api/audit/reconcile  — 触发哈希链验证
 *
 * 设计要点：
 *   - 仅 admin 可访问（后端 RequireAdmin 兜底，前端通过路由 meta 守卫）
 *   - 列表返回 envelope.data = { list, total, page, page_size }
 *   - 导出 CSV 由前端按 list 字段直接生成（无需后端再开一个端点）
 */
import { http } from "@/utils/http";
import type { ApiEnvelope } from "./_envelope";

export interface AuditRecord {
  id: number;
  target_type: string;
  target_id: number;
  action: string;
  actor_id: number;
  actor_ip?: string;
  user_agent?: string;
  detail?: string;
  hash_sm3: string;
  prev_hash: string;
  created_at: number;
}

export interface ListAuditParams {
  target_type?: string;
  target_id?: number;
  actor_id?: number;
  action?: string;
  /** unix seconds 起 */
  from?: number;
  /** unix seconds 止 */
  to?: number;
  page?: number;
  page_size?: number;
}

export interface ListAuditResult {
  list: AuditRecord[];
  total: number;
  page: number;
  page_size: number;
}

/**
 * 列出审计日志（admin only）。
 * target_type="" / target_id=0 / actor_id=0 / action="" 视为"全部"。
 */
export function listAudit(
  params: ListAuditParams = {}
): Promise<ApiEnvelope<ListAuditResult>> {
  return http.request<ApiEnvelope<ListAuditResult>>(
    "get",
    "/api/audit",
    { params }
  );
}

/**
 * 触发全局哈希链验证（admin only）。
 * 成功响应：{ success: true, data: { ok: true } }
 * 失败响应：HTTP 500 + header X-Audit-Broken-At 携带断点 id
 */
export function reconcileAuditChain(): Promise<ApiEnvelope<{ ok: boolean }>> {
  return http.request<ApiEnvelope<{ ok: boolean }>>(
    "post",
    "/api/audit/reconcile"
  );
}

/**
 * target_type 选项字典（与 backend AuditTargetType 常量一致）。
 * 写在这里而不是从后端取，因为 admin 页面只需要"全集 + 全部"两选项，过滤简单。
 */
export const TARGET_TYPE_OPTIONS: { label: string; value: string }[] = [
  { label: "全部", value: "" },
  { label: "media（媒体）", value: "media" },
  { label: "signature（签名）", value: "signature" },
  { label: "seal（印章）", value: "seal" },
  { label: "contract（合同表单）", value: "contract" },
  { label: "flow（流转）", value: "flow" },
  { label: "thirdparty（第三方合同）", value: "thirdparty" },
  { label: "reminder（提醒）", value: "reminder" },
  { label: "rbac_role / rbac_permission / rbac_user_binding", value: "rbac_role" },
  { label: "consent_letter（意愿书）", value: "consent_letter" },
  { label: "pdf_lock（合同 WORM）", value: "pdf_lock" },
  { label: "customer（客户）", value: "customer" },
  { label: "auth（认证）", value: "auth" },
  { label: "backup（备份）", value: "backup" },
  { label: "scheduled_task（调度）", value: "scheduled_task" },
  { label: "system（系统）", value: "system" }
];
