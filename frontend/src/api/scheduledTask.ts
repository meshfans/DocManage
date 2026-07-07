import { http } from "@/utils/http";

// ==================== 类型定义 ====================

export type ScheduledTask = {
  id: number;
  task_key: string;
  name: string;
  description: string;
  task_type: "built_in" | "business";
  cron_expr: string;
  handler_name: string;
  handler_params: string;
  is_active: boolean;
  is_concurrent: boolean;
  timeout_seconds: number;
  last_run_at: number;
  last_status: string;
  last_error: string;
  next_run_at: number;
  run_count: number;
  fail_count: number;
  created_by: number;
  created_at: string;
  updated_at: string;
};

export type ScheduledTaskLog = {
  id: number;
  task_id: number;
  task_key: string;
  started_at: number;
  finished_at: number;
  duration_ms: number;
  status: string;
  output: string;
  error: string;
  triggered_by: string;
  operator_id: number;
};

export type HandlerInfo = {
  name: string;
  description: string;
};

export type ListResult = {
  success: boolean;
  data: {
    list: ScheduledTask[];
    total: number;
  };
};

export type DetailResult = {
  success: boolean;
  data: ScheduledTask;
};

export type HandlersResult = {
  success: boolean;
  data: HandlerInfo[];
};

export type LogsResult = {
  success: boolean;
  data: {
    list: ScheduledTaskLog[];
    total: number;
    page: number;
    page_size: number;
  };
};

export type TaskRequest = {
  task_key: string;
  name: string;
  description?: string;
  cron_expr: string;
  handler_name: string;
  handler_params?: string;
  is_active?: boolean;
  is_concurrent?: boolean;
  timeout_seconds?: number;
};

export type TaskUpdateRequest = {
  name?: string;
  description?: string;
  cron_expr?: string;
  handler_name?: string;
  handler_params?: string;
  is_active?: boolean;
  is_concurrent?: boolean;
  timeout_seconds?: number;
};

// ==================== API ====================

// 1. 任务列表
export const listTasks = (params?: {
  keyword?: string;
  task_type?: string;
  is_active?: string;
}) => {
  const q: string[] = [];
  if (params?.keyword) q.push(`keyword=${encodeURIComponent(params.keyword)}`);
  if (params?.task_type) q.push(`task_type=${params.task_type}`);
  if (params?.is_active) q.push(`is_active=${params.is_active}`);
  const qs = q.length ? `?${q.join("&")}` : "";
  return http.request<ListResult>("get", `/api/scheduled-tasks${qs}`);
};

// 2. 任务详情
export const getTask = (id: number) => {
  return http.request<DetailResult>("get", `/api/scheduled-tasks/${id}`);
};

// 3. 新建任务
export const createTask = (data: TaskRequest) => {
  return http.post<DetailResult>("/api/scheduled-tasks", data);
};

// 4. 更新任务
export const updateTask = (id: number, data: TaskUpdateRequest) => {
  return http.post<DetailResult>(`/api/scheduled-tasks/${id}/update`, data);
};

// 5. 启停
export const toggleTask = (id: number) => {
  return http.post<DetailResult>(`/api/scheduled-tasks/${id}/toggle`);
};

// 6. 删除（仅业务任务）
export const deleteTask = (id: number) => {
  return http.post<{ success: boolean }>(`/api/scheduled-tasks/${id}/delete`);
};

// 7. 立即执行
export const runNow = (id: number) => {
  return http.post<{ success: boolean; data: { id: number } }>(
    `/api/scheduled-tasks/${id}/run-now`
  );
};

// 8. Handler 列表
export const listHandlers = () => {
  return http.request<HandlersResult>("get", "/api/scheduled-tasks/handlers");
};

// 9. 任务日志
export const getTaskLogs = (id: number, page = 1, pageSize = 20) => {
  return http.request<LogsResult>(
    "get",
    `/api/scheduled-tasks/${id}/logs?page=${page}&page_size=${pageSize}`
  );
};

// 10. 计算 cron 表达式下次触发时间（不依赖具体任务）
export const getNextRunTime = (expr: string) => {
  return http.request<{
    success: boolean;
    data: { expr: string; next_run: number; next_time: string };
  }>("get", `/api/scheduled-tasks/next-run?expr=${encodeURIComponent(expr)}`);
};

// ==================== 审计 ====================

export type ScheduledTaskAudit = {
  id: number;
  task_id: number;
  task_key: string;
  action: string;
  field_name: string;
  old_value: string;
  new_value: string;
  operator_id: number;
  operator_name: string;
  created_at: string;
};

export type AuditsResult = {
  success: boolean;
  data: {
    list: ScheduledTaskAudit[];
    total: number;
    page: number;
    page_size: number;
  };
};

// 11. 任务变更审计
export const getTaskAudits = (id: number, page = 1, pageSize = 20) => {
  return http.request<AuditsResult>(
    "get",
    `/api/scheduled-tasks/${id}/audits?page=${page}&page_size=${pageSize}`
  );
};

// ==================== 工具函数 ====================

// 格式化时间戳
export const formatUnix = (ts: number): string => {
  if (!ts || ts <= 0) return "-";
  const d = new Date(ts * 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

// 状态颜色映射
export const statusTypeMap: Record<string, "" | "success" | "warning" | "danger" | "info"> = {
  pending: "info",
  running: "warning",
  success: "success",
  failed: "danger",
  timeout: "danger",
  skipped: "info",
};

// 类型颜色
export const taskTypeMap: Record<string, "" | "success" | "info"> = {
  built_in: "success",
  business: "info",
};
