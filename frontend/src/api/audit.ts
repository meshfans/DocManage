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
 *   - AUDIT_DICT 集中维护 17 种 target_type 的中文名 / Tag 颜色 / action 全集，
 *     供 audit.vue 筛选下拉 / 表格 Tag / 统计卡 三处复用，避免分散导致对不上。
 */
import { http } from "@/utils/http";
import type { ApiEnvelope } from "./_envelope";

// ==================== 类型 ====================

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

export interface ReconcileResult {
  ok: boolean;
  broken_at?: number;
  error?: string;
}

// ==================== 权威字典 AUDIT_DICT ====================
//
// 权威来源：backend/models/audit.go 顶部注释 + database/audit_log.go AuditTargetXxx 常量。
// 共 17 种 target_type：media / signature / seal / contract / flow / thirdparty /
// reminder / rbac_role / rbac_permission / rbac_user_binding / consent_letter /
// pdf_lock / customer / auth / backup / scheduled_task / system。
//
// 每种类型包含：label（中文名）、tag（el-tag 类型色）、actions（该类型下所有 action 枚举）。
//
// 注意：thirdparty 是 Trail 命名（无下划线）；third_party 是参考项目命名；
// 我们跟随后端常量 AuditTargetThirdParty = "thirdparty"，不再混用。

export type AuditTagType =
  | "primary"
  | "success"
  | "warning"
  | "info"
  | "danger";

export interface AuditActionDef {
  value: string;
  label: string;
}

export interface AuditTypeDef {
  label: string;
  tag: AuditTagType;
  actions: AuditActionDef[];
}

export const AUDIT_DICT: Record<string, AuditTypeDef> = {
  media: {
    label: "媒体",
    tag: "primary",
    actions: [
      { value: "view", label: "查看 (view)" },
      { value: "download", label: "下载 (download)" },
      { value: "upload", label: "上传 (upload)" },
      { value: "record", label: "摄像记录 (record)" },
      { value: "delete", label: "删除 (delete)" },
      { value: "restore", label: "恢复 (restore)" },
      { value: "bind", label: "绑定 (bind)" },
      { value: "unbind", label: "解绑 (unbind)" },
      { value: "tag-add", label: "加标签 (tag-add)" },
      { value: "tag-remove", label: "去标签 (tag-remove)" },
      { value: "bulk-delete", label: "批量删除 (bulk-delete)" }
    ]
  },
  signature: {
    label: "签名",
    tag: "success",
    actions: [
      { value: "sign", label: "手签-合同 (sign)" },
      { value: "sign-date", label: "日期-合同 (sign-date)" },
      { value: "sign-draft", label: "手签-草稿 (sign-draft)" },
      { value: "sign-date-draft", label: "日期-草稿 (sign-date-draft)" }
    ]
  },
  seal: {
    label: "印章",
    tag: "warning",
    actions: [
      { value: "create", label: "创建 (create)" },
      { value: "delete", label: "删除 (delete)" },
      { value: "restore", label: "恢复 (restore)" }
    ]
  },
  contract: {
    label: "合同",
    tag: "info",
    actions: [
      { value: "save-form", label: "保存表单 (save-form)" },
      { value: "status-change", label: "状态变更 (status-change)" }
    ]
  },
  flow: {
    label: "流转",
    tag: "primary",
    actions: [
      { value: "start", label: "启动 (start)" },
      { value: "submit-step", label: "提交步骤 (submit-step)" },
      { value: "reject", label: "驳回 (reject)" },
      { value: "cancel", label: "取消 (cancel)" },
      { value: "delete", label: "删除 (delete)" }
    ]
  },
  thirdparty: {
    label: "第三方合同",
    tag: "warning",
    actions: [
      { value: "create", label: "创建 (create)" },
      { value: "status-change", label: "状态变更 (status-change)" },
      { value: "delete", label: "删除 (delete)" }
    ]
  },
  reminder: {
    label: "提醒",
    tag: "info",
    actions: [
      { value: "create", label: "创建 (create)" },
      { value: "update", label: "更新 (update)" },
      { value: "delete", label: "删除 (delete)" },
      { value: "subscribe", label: "订阅 (subscribe)" },
      { value: "update-sub", label: "更新订阅 (update-sub)" },
      { value: "unsubscribe", label: "取消订阅 (unsubscribe)" },
      { value: "scan", label: "扫描触发 (scan)" }
    ]
  },
  rbac_role: {
    label: "RBAC 角色",
    tag: "success",
    actions: [
      { value: "upsert", label: "新建/更新 (upsert)" },
      { value: "delete", label: "删除 (delete)" }
    ]
  },
  rbac_permission: {
    label: "RBAC 权限",
    tag: "success",
    actions: [
      { value: "upsert", label: "新建/更新 (upsert)" },
      { value: "delete", label: "删除 (delete)" }
    ]
  },
  rbac_user_binding: {
    label: "RBAC 用户绑定",
    tag: "warning",
    actions: [
      { value: "assign_roles", label: "分配角色 (assign_roles)" },
      { value: "update_roles", label: "更新角色 (update_roles)" }
    ]
  },
  consent_letter: {
    label: "意愿确认书",
    tag: "info",
    actions: [{ value: "view", label: "阅读 (view)" }]
  },
  pdf_lock: {
    label: "PDF 锁定 (WORM)",
    tag: "danger",
    actions: [
      { value: "pdf_lock", label: "锁定 PDF (pdf_lock)" },
      { value: "pdf_verify", label: "校验通过 (pdf_verify)" },
      { value: "pdf_verify_failed", label: "校验失败 (pdf_verify_failed)" },
      { value: "pdf_download", label: "下载 PDF (pdf_download)" },
      { value: "evidence_export", label: "导出证据包 (evidence_export)" }
    ]
  },
  customer: {
    label: "客户",
    tag: "primary",
    actions: [
      { value: "create", label: "创建 (create)" },
      { value: "update", label: "更新 (update)" },
      { value: "delete", label: "删除 (delete)" },
      { value: "signature.upload", label: "上传签名 (signature.upload)" },
      { value: "signature.lock", label: "签名 WORM 锁定 (signature.lock)" }
    ]
  },
  auth: {
    label: "认证",
    tag: "success",
    actions: [
      { value: "login.success", label: "登录成功 (login.success)" },
      { value: "login.failed", label: "登录失败 (login.failed)" },
      { value: "refresh.success", label: "刷新成功 (refresh.success)" },
      { value: "refresh.failed", label: "刷新失败 (refresh.failed)" },
      { value: "password.change.success", label: "改密成功 (password.change.success)" },
      { value: "password.change.failed", label: "改密失败 (password.change.failed)" }
    ]
  },
  backup: {
    label: "备份",
    tag: "warning",
    actions: [
      { value: "manual.success", label: "手动备份成功 (manual.success)" },
      { value: "manual.failed", label: "手动备份失败 (manual.failed)" },
      { value: "restore.success", label: "恢复成功 (restore.success)" },
      { value: "restore.failed", label: "恢复失败 (restore.failed)" },
      { value: "verify.ok", label: "校验通过 (verify.ok)" },
      { value: "verify.corrupted", label: "校验损坏 (verify.corrupted)" },
      { value: "verify.missing", label: "校验缺失 (verify.missing)" }
    ]
  },
  scheduled_task: {
    label: "调度任务",
    tag: "info",
    actions: [
      { value: "run.success", label: "执行成功 (run.success)" },
      { value: "run.failed", label: "执行失败 (run.failed)" },
      { value: "run.timeout", label: "执行超时 (run.timeout)" },
      { value: "run.skipped", label: "跳过 (run.skipped)" }
    ]
  },
  system: {
    label: "系统",
    tag: "danger",
    actions: [
      { value: "maintenance.enable", label: "启用维护 (maintenance.enable)" },
      { value: "maintenance.disable", label: "关闭维护 (maintenance.disable)" }
    ]
  }
};

/** 目标类型下拉（含"全部类型"首项，按字典稳定顺序） */
export const TARGET_TYPE_OPTIONS: { label: string; value: string }[] = [
  { label: "全部类型", value: "" },
  ...Object.entries(AUDIT_DICT).map(([value, def]) => ({
    value,
    label: `${def.label} (${value})`
  }))
];

// ==================== API 函数 ====================

/**
 * 列出审计日志（admin only）。
 * target_type="" / target_id=0 / actor_id=0 / action="" 视为"全部"。
 */
export function listAudit(
  params: ListAuditParams = {}
): Promise<ApiEnvelope<ListAuditResult>> {
  return http.request<ApiEnvelope<ListAuditResult>>("get", "/api/audit", {
    params
  });
}

/**
 * 触发全局哈希链验证（admin only）。
 * 成功响应：{ success: true, data: { ok: true } }
 * 失败响应：HTTP 500 + header X-Audit-Broken-At 携带断点 id
 *          body 走 utils.ErrorWithDetail 标准格式。
 *
 * ⚠️ 与参考项目 DocManage 不同：Trail 后端走 ErrorWithDetail（5xx + Header），
 * 不再把 broken_at 塞进 data.ok=false 的 body。前端需读 X-Audit-Broken-At header。
 */
export function reconcileAuditChain(): Promise<ApiEnvelope<ReconcileResult>> {
  return http.request<ApiEnvelope<ReconcileResult>>(
    "post",
    "/api/audit/reconcile"
  );
}
