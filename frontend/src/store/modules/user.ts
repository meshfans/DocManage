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
  // 2026-10-01 C1/C2 统一：拉取 license 能力（module_keys），决定 requiredFeature 页面显隐
  import { getLicenseFeatures } from "@/api/license";
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
    // 2026-10-01 C1/C2 统一：当前 license 启用的 module key 集合。
    // 登录后由 loginByUsername / refreshFromApi fetch /api/license/features 填充。
    // "ai" 决定 /system/ai-config 页面是否显示（无则默认用 meshfans 的 LLM 能力）。
    moduleKeys: [],    isRemembered: false,
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
    /** 2026-10-01 C1/C2 统一：存 license 启用的 module key 集合 */
    SET_MODULE_KEYS(keys: Array<string>) {
      this.moduleKeys = keys;
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
    async loginByUsername(credentials) {
      try {
        const res = await getLogin(credentials);
        if (res?.success) {
          setToken(res.data);
          // 2026-08-20 自愈：登录成功后强制从 /api/user/info 重新拉取权威
          // roles/permissions/permissionVersion 并同步 store + localStorage。
          // 背景：login 响应里虽然带了 permissions，但若浏览器 localStorage
          // 残留了之前 dirty 的 user-info（升级 / 角色变更 / 缓存漂移等场景），
          // 新标签页 store 初始化时会从 localStorage 反查旧值，导致"0/4 空模块"。
          // 多打一次 user/info 代价极小（约 30ms），换来自愈能力，不必再让用户手动清缓存。
          try {
            await this.refreshFromApi();
          } catch (e) {
            console.debug("[user store] post-login refreshFromApi failed", e);
          }
          // 2026-10-01 C1/C2 统一：登录成功后立即拉 license 能力。
          // 必须在路由装配（initDynamicRouter → filterTree）之前完成，
          // 否则 AI 配置页会因 moduleKeys 尚为空而被误隐藏。
          try {
            await this.refreshLicenseFeatures();
          } catch (e) {
            console.debug("[user store] post-login refreshLicenseFeatures failed", e);
          }
        }
        return res;
      } catch (error) {
        throw error;
      }
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
    /**
     * 2026-10-01 C1/C2 统一：拉取 license 能力，写入 moduleKeys。
     *
     * 数据源：GET /api/license/features 的 module_keys 字段
     *        （后端 handlers/license_features.go 从 sdk.GlobalOutcome 派生，
     *          白名单见 pkg/license/sdk/feature.go 的 BusinessModuleKeys）
     * 当前白名单：rag / ocr / ai。其中 "ai" 控制 AI 配置页显隐。
     *
     * 失败时不抛：moduleKeys 保持空数组 → hasFeature 全返 false
     *              → AI 配置页等 requiredFeature 页面保守隐藏。
     *
     * 2026-10-02 M-6 修复：失败时重试 1 次（间隔 1s）。
     *   场景：登录后端短暂 502 / 网络抖动 → moduleKeys 永久空 → AI 配置页
     *   永久不可见，必须刷页。新增 retry 兜底：单次抖动场景自动恢复。
     */
    async refreshLicenseFeatures() {
      const tryOnce = async () => {
        try {
          const res = await getLicenseFeatures();
          if (res?.success && res.data && Array.isArray(res.data.module_keys)) {
            this.SET_MODULE_KEYS(res.data.module_keys);
            return true;
          }
          this.SET_MODULE_KEYS([]);
          return false;
        } catch (e) {
          console.debug("[user store] getLicenseFeatures failed", e);
          this.SET_MODULE_KEYS([]);
          return false;
        }
      };

      if (await tryOnce()) return;
      // 单次重试：1s 后再拉一次。两次都失败 → 保守置空（与历史行为一致）
      await new Promise(resolve => setTimeout(resolve, 1000));
      await tryOnce();
    },    async refreshFromApi() {
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
