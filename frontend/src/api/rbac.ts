import { http } from "@/utils/http";

export interface Role {
  id: number;
  code: string;
  name: string;
  description: string;
  permissions: string[];
  is_system: boolean;
  status: string;
  // 2026-06-28 RBAC v3 P5：数据级权限
  // all = 全部 / dept_and_sub = 本部门+下属 / dept = 本部门
  // self_and_sub_dept = 自己+本部门+下属 / self = 自己 / custom = 自定义部门
  data_scope?: "all" | "dept_and_sub" | "dept" | "self_and_sub_dept" | "self" | "custom";
  // 2026-06-28 RBAC v3.1：custom 模式部门白名单（仅 data_scope=custom 时生效）
  // 后端存储为 JSON 数组；删除的部门自动过滤。
  custom_dept_ids?: number[];
  created_at?: number;
  updated_at?: number;
}

// 2026-06-28 RBAC v3 P5：data_scope 选项（前端展示用）
export const DATA_SCOPE_OPTIONS = [
  { value: "all", label: "全部数据", desc: "可看所有数据（admin 默认）" },
  { value: "dept_and_sub", label: "本部门+下级部门", desc: "含下级部门的树形数据" },
  { value: "dept", label: "本部门", desc: "仅本部门数据（manager 默认）" },
  { value: "self_and_sub_dept", label: "自己+本部门+下级", desc: "自己创建的 + 本部门 + 下级" },
  { value: "self", label: "仅自己", desc: "仅自己创建的数据（common 默认）" },
  { value: "custom", label: "自定义部门", desc: "指定部门白名单（需 P5 配置）" }
] as const;

export interface Permission {
  id: number;
  code: string;
  name: string;
  module: string;
  api_path: string;
  http_method: string;
  description: string;
  is_system: boolean;
  status: string;
  created_at?: number;
  updated_at?: number;
}

// ===== Roles =====

export function listRoles() {
  return http.request<{ list: Role[]; total: number }>(
    "get",
    "/api/rbac/roles"
  );
}

export function upsertRole(data: Partial<Role>) {
  return http.request("post", "/api/rbac/roles", { data });
}

export function deleteRole(code: string) {
  return http.request("post", `/api/rbac/roles/${code}/delete`);
}

// ===== Permissions =====

export function listPermissions() {
  return http.request<{ list: Permission[]; total: number }>(
    "get",
    "/api/rbac/permissions"
  );
}

export function upsertPermission(data: Partial<Permission>) {
  return http.request("post", "/api/rbac/permissions", { data });
}

export function deletePermission(code: string) {
  return http.request("post", `/api/rbac/permissions/${code}/delete`);
}

// ===== Check =====

export interface CheckRequest {
  username: string;
  required_codes: string[];
}

export interface CheckResponse {
  username: string;
  required_codes: string[];
  effective_permissions: string[];
  allowed: boolean;
}

export function checkPermission(data: CheckRequest) {
  return http.request<CheckResponse>("post", "/api/rbac/check", { data });
}