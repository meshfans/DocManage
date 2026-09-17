import { http } from "@/utils/http";

export type User = {
  id: number;
  username: string;
  nickname: string;
  avatar: string;
  roles: string[];
  permissions: string[];
  real_name: string;
  email: string;
  phone: string;
  department_id: number | null;
  department_name: string;
  position: string;
  employee_no: string;
  status: string;
  created_at: string;
  updated_at: string;
};

export type UserListResult = {
  success: boolean;
  data: {
    list: User[];
    total: number;
    page: number;
    page_size: number;
    total_pages: number;
  };
};

export type UserResult = {
  success: boolean;
  data: User;
};

export interface GetUsersParams {
  fields?: string;
  page?: number;
  page_size?: number;
  search?: string;
}

export const getUsers = (params?: GetUsersParams) => {
  if (!params) {
    return http.request<UserListResult>("get", "/api/users");
  }

  const queryParams: Record<string, any> = {};
  if (params.page) queryParams.page = params.page;
  if (params.page_size) queryParams.page_size = params.page_size;
  if (params.search) queryParams.search = params.search;
  if (params.fields) queryParams.fields = params.fields;

  return http.request<UserListResult>("get", "/api/users", {
    params: queryParams
  });
};

export type UsernameCheckResult = {
  success: boolean;
  data: {
    exists: boolean;
  };
};

export const checkUsername = (username: string, excludeId?: number) => {
  const params: any = { username };
  if (excludeId !== undefined) {
    params.exclude_id = excludeId;
  }
  return http.request<UsernameCheckResult>("get", "/api/users/check-username", {
    params
  });
};

export type CreateUserData = {
  username: string;
  password: string;
  nickname?: string;
  real_name?: string;
  email?: string;
  phone?: string;
  position?: string;
  employee_no?: string;
  department_id?: number | null;
  status?: string;
};

export type CreateUserResult = {
  success: boolean;
  data?: {
    id: number;
    message?: string;
  };
  message?: string;
};

export const createUser = (data: CreateUserData) => {
  return http.post<CreateUserResult>("/api/users", data);
};

export const getUser = (id: number) => {
  return http.request<UserResult>("get", `/api/users/${id}`);
};

export const updateUser = (
  id: number,
  data: {
    username?: string;
    nickname?: string;
    real_name?: string;
    email?: string;
    phone?: string;
    position?: string;
    employee_no?: string;
    department_id?: number | null;
    status?: string;
  }
) => {
  return http.post(`/api/users/${id}/update`, data);
};

export const deleteUser = (id: number) => {
  return http.post(`/api/users/${id}/delete`);
};

export const updateUserDepartment = (
  id: number,
  data: {
    department_id: number | null;
  }
) => {
  return http.post(`/api/users/${id}/department`, data);
};

export const assignUserRoles = (id: number, data: { roles: string[] }) => {
  return http.post(`/api/users/${id}/roles`, data);
};

export const uploadAvatar = (id: number, file: File) => {
  const formData = new FormData();
  formData.append("file", file);
  return http.post(`/api/users/${id}/avatar`, formData, {
    headers: {
      "Content-Type": "multipart/form-data"
    }
  });
};

// 获取用户头像（返回 Blob URL）
export const fetchUserAvatar = async (id: number): Promise<{ url: string; revoke: () => void }> => {
  const blob = (await http.request("get", `/api/users/${id}/avatar`, {
    responseType: "blob"
  })) as Blob;
  const objectUrl = URL.createObjectURL(blob);
  return {
    url: objectUrl,
    revoke: () => URL.revokeObjectURL(objectUrl)
  };
};

export const getDepartmentUsers = (departmentId: number) => {
  return http.request<UserListResult>("get", `/api/users/department/${departmentId}`);
};
