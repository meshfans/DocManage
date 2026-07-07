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

export type DepartmentResult = {
  success: boolean;
  data: Department | DepartmentTree[];
};

export type DepartmentMessageResult = {
  success: boolean;
  message?: string;
};

export const getDepartmentTree = (fields?: string) => {
  if (fields) {
    return http.request<DepartmentResult>("get", "/api/departments", {
      params: { fields }
    });
  }
  return http.request<DepartmentResult>("get", "/api/departments");
};

export const getDepartment = (id: number) => {
  return http.request<DepartmentResult>("get", `/api/departments/${id}`);
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
  return http.request<DepartmentResult>("get", `/api/departments/${id}/users`);
};
