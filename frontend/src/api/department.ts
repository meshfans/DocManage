import { http } from "@/utils/http";

// 后端 SQLite 中 created_at/updated_at 存为 INTEGER（Unix 秒），
// 实际通过 Go struct 序列化为 JSON 数字；后端代码改造时统一转 ISO 字符串。
export type Department = {
  id: number;
  name: string;
  parent_id: number;
  level: number;
  sort_order: number;
  status: string;
  created_at: number | string;
  updated_at: number | string;
};

export type DepartmentTree = Department & {
  children: DepartmentTree[];
};

/** GET /api/departments 返回：{ success, data: DepartmentTree[] } 包络 */
export type DepartmentListResult = {
  success?: boolean;
  data?: DepartmentTree[];
  list?: DepartmentTree[];
  total?: number;
};

/** GET /api/departments/:id 返回：单条 Department 对象 */
export type DepartmentDetailResult = {
  success?: boolean;
  department?: Department;
};

/** GET /api/departments/:id/users 返回：{ list: User[], total: number } */
export type DepartmentUsersResult = {
  success?: boolean;
  list?: Array<Record<string, unknown>>;
  total?: number;
};

/** 写操作（创建/更新/删除）共用的 message-only 返回 */
export type DepartmentMessageResult = {
  success: boolean;
  message?: string;
};

export const getDepartmentTree = (fields?: string) => {
  if (fields) {
    return http.request<DepartmentListResult>("get", "/api/departments", {
      params: { fields }
    });
  }
  return http.request<DepartmentListResult>("get", "/api/departments");
};

export const getDepartment = (id: number) => {
  return http.request<DepartmentDetailResult>("get", `/api/departments/${id}`);
};

export const createDepartment = (data: {
  name: string;
  parent_id?: number;
  sort_order?: number;
  status?: string;
}) => {
  return http.post("/api/departments", data);
};

export const updateDepartment = (
  id: number,
  data: {
    name: string;
    parent_id?: number;
    sort_order?: number;
    status?: string;
  }
) => {
  return http.post(`/api/departments/${id}/update`, data);
};

export const deleteDepartment = (id: number) => {
  return http.post<DepartmentMessageResult>(`/api/departments/${id}/delete`);
};

export const getDepartmentUsers = (id: number) => {
  return http.request<DepartmentUsersResult>("get", `/api/departments/${id}/users`);
};