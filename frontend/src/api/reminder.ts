import { http } from "@/utils/http";

// 第十一阶段：提醒业务（模板 + 订阅 + 日志 + 扫描触发）

// ==================== 类型 ====================

export type ReminderTemplate = {
  id: number;
  template_key: string;
  name: string;
  description: string;
  rule_type: string; // 'contract_expiring' | future
  advance_days: number;
  is_active: boolean;
  is_system: boolean; // true = 系统预置不可删
  sort_order: number;
  created_at: number;
  updated_at: number;
};

export type LinkType = "customer" | "third_party_contract";

export const LINK_TYPE_OPTIONS: { value: LinkType; label: string; description: string }[] = [
  {
    value: "customer",
    label: "客户",
    description: "订阅该客户，自动展开为该客户所有文档"
  },
  {
    value: "third_party_contract",
    label: "文档",
    description: "订阅文档（third_party_contract 表）"
  }
];

export type ReminderSubscription = {
  id: number;
  template_id: number;
  // 第十三阶段 v4：通用多态关联
  link_type: LinkType;
  link_id: string; // CSV: "5" / "1,2,3"
  // 第十三阶段：接收人
  receiver_type: "admin" | "customer_owner" | "department" | "user";
  // 第十三阶段 v2：receiver_id 改为 CSV 字符串（支持多 ID，如 "1,2,3"）
  receiver_id: string;
  is_active: boolean;
  remark: string;
  created_by: number;
  created_at: number;
  updated_at: number;
  // 联表冗余字段
  template_name?: string;
  template_key?: string;
  template_advance_days?: number;
  // v4：link_* 显示用名（按 link_type 不同表查询）
  link_display_name?: string;
  link_sub_info?: string;
};

// 第十三阶段：receiver_type 合法枚举
export const RECEIVER_TYPE_OPTIONS: {
  value: ReminderSubscription["receiver_type"];
  label: string;
  description: string;
}[] = [
  {
    value: "admin",
    label: "管理员",
    description: "发到第一个 admin 用户"
  },
  {
    value: "customer_owner",
    label: "客户主负责人",
    description: "发到 customer.owner_user_id"
  },
  {
    value: "department",
    label: "部门内所有人",
    description: "receiver_id = 部门 ID，部门内所有 active 用户都收到"
  },
  {
    value: "user",
    label: "指定用户",
    description: "receiver_id = 用户 ID"
  }
];

export type ReminderLog = {
  id: number;
  template_id: number;
  subscription_id: number | null;
  customer_id: number | null;
  contract_id: number | null;
  contract_no: string;
  contract_title: string;
  customer_name: string;
  trigger_date: string; // YYYY-MM-DD
  days_before: number;
  message_id: number | null;
  delivery_status: "sent" | "failed";
  error_msg: string;
  triggered_by: "scheduler" | "manual";
  created_at: number;
};

export type ReminderScanStats = {
  templates: number;
  subscriptions: number;
  contracts_scanned: number;
  matched: number;
  sent: number;
  skipped: number;
  failed: number;
};

// ==================== 模板 API ====================

export const listReminderTemplates = () => {
  return http.request<{
    success: boolean;
    data: { list: ReminderTemplate[]; total: number };
  }>("get", "/api/reminders/templates");
};

export const createReminderTemplate = (data: {
  template_key: string;
  name: string;
  description?: string;
  rule_type?: string;
  advance_days: number;
  is_active?: boolean;
  sort_order?: number;
}) => {
  return http.request<{ success: boolean; data: { id: number } }>(
    "post",
    "/api/reminders/templates",
    { data }
  );
};

export const updateReminderTemplate = (
  id: number,
  data: {
    name: string;
    description?: string;
    rule_type?: string;
    advance_days: number;
    is_active: boolean;
    sort_order: number;
  }
) => {
  return http.request<{ success: boolean; data: { message: string } }>(
    "post",
    `/api/reminders/templates/${id}`,
    { data }
  );
};

export const deleteReminderTemplate = (id: number) => {
  return http.request<{ success: boolean; data: { message: string } }>(
    "post",
    `/api/reminders/templates/${id}/delete`
  );
};

// ==================== 订阅 API ====================

export const listReminderSubscriptions = (params: {
  template_id?: number;
  link_type?: LinkType;
  link_id?: number;
  is_active?: boolean;
  page?: number;
  page_size?: number;
}) => {
  return http.request<{
    success: boolean;
    data: { list: ReminderSubscription[]; total: number; page: number; page_size: number };
  }>("get", "/api/reminders/subscriptions", { params });
};

export const createReminderSubscription = (data: {
  template_id: number;
  // 第十三阶段 v4：通用多态关联
  link_type: LinkType;
  link_id: string; // CSV: "5" / "1,2,3"
  // 第十三阶段：接收人（缺省 = admin，v1 行为）
  receiver_type?: ReminderSubscription["receiver_type"];
  // 第十三阶段 v2：receiver_id 是 CSV 字符串（支持多 ID）
  receiver_id?: string;
  remark?: string;
}) => {
  return http.request<{ success: boolean; data: { id: number } }>(
    "post",
    "/api/reminders/subscriptions",
    { data }
  );
};

export const updateReminderSubscription = (
  id: number,
  data: { is_active?: boolean; remark?: string }
) => {
  return http.request<{ success: boolean; data: { message: string } }>(
    "post",
    `/api/reminders/subscriptions/${id}`,
    { data }
  );
};

export const deleteReminderSubscription = (id: number) => {
  return http.request<{ success: boolean; data: { message: string } }>(
    "post",
    `/api/reminders/subscriptions/${id}/delete`
  );
};

// ==================== 日志 API ====================

export const listReminderLogs = (params: {
  template_id?: number;
  customer_id?: number;
  contract_id?: number;
  trigger_date?: string;
  triggered_by?: string;
  delivery_status?: string;
  page?: number;
  page_size?: number;
}) => {
  return http.request<{
    success: boolean;
    data: { list: ReminderLog[]; total: number; page: number; page_size: number };
  }>("get", "/api/reminders/logs", { params });
};

// ==================== 扫描触发 API ====================

export const triggerReminderScan = () => {
  return http.request<{ success: boolean; data: { message: string; stats: ReminderScanStats } }>(
    "post",
    "/api/reminders/scan"
  );
};
