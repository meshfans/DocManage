import { defineStore } from "pinia";
import {
  type userType,
  store,
  router,
  resetRouter,
  routerArrays,
  storageLocal
} from "../utils";
import {
  type UserResult,
  type RefreshTokenResult,
  getLogin,
  getUserInfo,
  refreshTokenApi
} from "@/api/user";
import { useMultiTagsStoreHook } from "./multiTags";
import {
  type DataInfo,
  setToken,
  removeToken,
  setPermVersion,
  userKey
} from "@/utils/auth";

export const useUserStore = defineStore("pure-user", {
  state: (): userType => ({
    // 头像
    avatar: storageLocal().getItem<DataInfo<number>>(userKey)?.avatar ?? "",
    // 用户名
    username: storageLocal().getItem<DataInfo<number>>(userKey)?.username ?? "",
    // 昵称
    nickname: storageLocal().getItem<DataInfo<number>>(userKey)?.nickname ?? "",
    // 页面级别权限
    roles: storageLocal().getItem<DataInfo<number>>(userKey)?.roles ?? [],
    // 按钮级别权限
    permissions:
      storageLocal().getItem<DataInfo<number>>(userKey)?.permissions ?? [],
    // 是否勾选了登录页的免登录
    isRemembered: false,
    // 登录页的免登录存储几天，默认7天
    loginDay: 7
  }),
  actions: {
    /** 存储头像 */
    SET_AVATAR(avatar: string) {
      this.avatar = avatar;
    },
    /** 存储用户名 */
    SET_USERNAME(username: string) {
      this.username = username;
    },
    /** 存储昵称 */
    SET_NICKNAME(nickname: string) {
      this.nickname = nickname;
    },
    /** 存储角色 */
    SET_ROLES(roles: Array<string>) {
      this.roles = roles;
    },
    /** 存储按钮级别权限 */
    SET_PERMS(permissions: Array<string>) {
      this.permissions = permissions;
    },
    /** 存储是否勾选了登录页的免登录 */
    SET_ISREMEMBERED(bool: boolean) {
      this.isRemembered = bool;
    },
    /** 设置登录页的免登录存储几天 */
    SET_LOGINDAY(value: number) {
      this.loginDay = Number(value);
    },
    /** 登入 */
    async loginByUsername(data) {
      return new Promise<UserResult>((resolve, reject) => {
        getLogin(data)
          .then(data => {
            if (data?.success) setToken(data.data);
            resolve(data);
          })
          .catch(error => {
            reject(error);
          });
      });
    },
    /** 前端登出（不调用接口） */
    logOut() {
      this.username = "";
      this.roles = [];
      this.permissions = [];
      removeToken();
      useMultiTagsStoreHook().handleTags("equal", [...routerArrays]);
      resetRouter();
      router.push("/login");
    },
    /** 刷新`token` */
    async handRefreshToken(data) {
      return new Promise<RefreshTokenResult>((resolve, reject) => {
        refreshTokenApi(data)
          .then(data => {
            if (data) {
              setToken(data.data);
              resolve(data);
            }
          })
          .catch(error => {
            reject(error);
          });
      });
    },
    /**
     * 从后端重新拉取用户信息（含 roles / permissions / permissionVersion）
     * 用于 Plan B 权限版本变化时 reload 前刷新 localStorage，避免 reload 后旧数据生效
     * 2026-06-25 P1-7.1 修复
     */
    async refreshFromApi() {
      try {
        const res: any = await getUserInfo();
        if (res?.success && res.data) {
          const d = res.data;
          // 同步 store
          this.username = d.username ?? this.username;
          this.nickname = d.nickname ?? this.nickname;
          this.avatar = d.avatar ?? this.avatar;
          if (Array.isArray(d.roles)) this.roles = d.roles;
          if (Array.isArray(d.permissions)) this.permissions = d.permissions;
          // 同步 localStorage（让 reload 后保持一致）
          const stored = storageLocal().getItem<any>(userKey) ?? {};
          storageLocal().setItem(userKey, {
            ...stored,
            username: d.username ?? stored.username,
            nickname: d.nickname ?? stored.nickname,
            avatar: d.avatar ?? stored.avatar,
            roles: d.roles ?? stored.roles,
            permissions: d.permissions ?? stored.permissions
          });
          if (d.permissionVersion) {
            setPermVersion(d.permissionVersion);
          }
        }
      } catch (e) {
        // 静默失败：reload 仍能继续，保留旧数据不会更糟
        console.debug("[user store] refreshFromApi failed", e);
      }
    }
  }
});

export function useUserStoreHook() {
  return useUserStore(store);
}
