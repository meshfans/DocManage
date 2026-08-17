import { http } from "@/utils/http";

export type UserResult = {
  success: boolean;
  /** 业务消息（登录失败时由后端返回） */
  message?: string;
  data: {
    /** 头像 */
    avatar: string;
    /** 用户名 */
    username: string;
    /** 昵称 */
    nickname: string;
    /** 当前登录用户的角色 */
    roles: Array<string>;
    /** 按钮级别权限 */
    permissions: Array<string>;
    /** `token` */
    accessToken: string;
    /** 用于调用刷新`accessToken`的接口时所需的`token` */
    refreshToken: string;
    /** `accessToken`的过期时间（格式'xxxx/xx/xx xx:xx:xx'） */
    expires: Date;
  };
};

export type RefreshTokenResult = {
  success: boolean;
  data: {
    /** `token` */
    accessToken: string;
    /** 用于调用刷新`accessToken`的接口时所需的`token` */
    refreshToken: string;
    /** `accessToken`的过期时间（格式'xxxx/xx/xx xx:xx:xx'） */
    expires: Date;
  };
};

/** 登录 */
export const getLogin = (data?: object) => {
  return http.request<UserResult>("post", "/api/login", { data });
};

/**
 * 获取当前登录用户信息（含 roles / permissions / permissionVersion）
 * 2026-06-25 P1-7.1：用于 Plan B 权限版本变化时 reload 前刷新 localStorage
 */
export const getUserInfo = () => {
  return http.request<UserResult>("get", "/api/user/info");
};

/** 刷新`token` */
export const refreshTokenApi = (data?: object) => {
  return http.request<RefreshTokenResult>("post", "/api/refresh-token", { data });
};

/** 修改自己的密码 */
export const changePassword = (data: {
  old_password: string;
  new_password: string;
}) => {
  return http.request<{ success: boolean; message?: string }>(
    "post",
    "/api/change-password",
    { data }
  );
};
