import Axios, {
  type AxiosInstance,
  type AxiosRequestConfig,
  type CustomParamsSerializer
} from "axios";
import type {
  PureHttpError,
  RequestMethods,
  PureHttpResponse,
  PureHttpRequestConfig
} from "./types.d";
import { stringify } from "qs";
import { ElNotification } from "element-plus";
import { getToken, formatToken } from "@/utils/auth";
import { useUserStoreHook } from "@/store/modules/user";
import { getConfig } from "@/config";
import { isDocClient } from "@/utils/isDocClient";

// ==================== 全局 403 handler（RBAC v1.1，2026-06-26）====================
//
// 设计要点：
//   1. 防抖：5s 内多次 403 只弹一次通知（避免刷新页面后连串 403 弹出多个 Notification）
//   2. 加速 Plan B：refreshFromApi() 立即拉新 roles/permissions（不等 60s 轮询）
//   3. 不强制刷新页面：保留用户当前操作（避免突兀 reload）
//   4. 不影响其它 catch：仍 return Promise.reject(error)，各组件可继续捕获
//
// 白名单：登录失败 / 注册失败 等业务 403 不应触发（如密码错误返回 403）
//   - 当前实现：所有 403 都提示。若误报严重，可加 url 黑名单。
//
// === 403 提示策略：5s 防抖 + refreshFromApi() 异步拉新权限 ===
// 当前为静默处理（不弹窗）。如需恢复，把下方被注释的 ElNotification 块解开即可。
let last403NotifyAt = 0;
const FORBIDDEN_NOTIFY_DEBOUNCE_MS = 5000;
let refreshScheduled = false;

function handleGlobal403(error: PureHttpError): void {
  const now = Date.now();

  // 防抖：避免短时间内大量 403 触发连串通知
  if (now - last403NotifyAt < FORBIDDEN_NOTIFY_DEBOUNCE_MS) {
    return;
  }
  last403NotifyAt = now;

  // 1. 友好提示（2026-06-28 临时隐藏——见上方注释）
  // const message =
  //   (error?.response?.data as { message?: string })?.message ||
  //   "您的权限不足，请联系管理员";
  // ElNotification({
  //   title: "权限不足",
  //   message: `${message}（页面将自动同步最新权限）`,
  //   type: "warning",
  //   duration: 4000,
  //   position: "bottom-right"
  // });
  console.debug(
    "[403] suppressed notification (2026-06-28):",
    (error?.response?.data as { message?: string })?.message || error?.message
  );

  // 2. 加速 Plan B 失效检测：拉新 roles/permissions（不等 60s 轮询）
  //    防重复：5s 内多次 403 只调度一次 refresh
  if (!refreshScheduled) {
    refreshScheduled = true;
    setTimeout(() => {
      refreshScheduled = false;
      useUserStoreHook()
        .refreshFromApi()
        .catch(e => console.debug("[403 handler] refreshFromApi failed", e));
    }, 500);
  }
}

// 同时兼容浏览器（vite proxy）和 DocManage 客户端（127.0.0.1:18080 内嵌代理）：
//   - DocManage Client：WebView 加载 127.0.0.1:18080，内嵌代理转发 /api/* → 后端
//   - 浏览器：vite dev server 同源，server.proxy 转发 /api/* → 后端
function getBaseUrl(): string {
  if (isDocClient()) {
    const platformConfig = getConfig();
    return platformConfig?.ApiBaseUrl || "http://127.0.0.1:18080";
  }
  return import.meta.env.VITE_API_BASE_URL || "";
}

// 相关配置请参考：www.axios-js.com/zh-cn/docs/#axios-request-config-1
function getDefaultConfig(): AxiosRequestConfig {
  return {
    // 默认 30s：兼容媒体上传（500MB 视频流式上传可能需要更长时间）
    // 上传接口应在调用方显式覆盖 timeout 为更高值（如 5 分钟）
    timeout: 30000,
    baseURL: getBaseUrl(),
    headers: {
      Accept: "application/json, text/plain, */*",
      "Content-Type": "application/json",
      "X-Requested-With": "XMLHttpRequest"
    },
    // qs 默认 arrayFormat=indices → ?tag_names[0]=a&tag_names[1]=b
    // Gin c.QueryArray 期望 ?tag_names=a&tag_names=b（repeat 格式）
    // 统一为 repeat，前后端数组参数才能对得上
    paramsSerializer: {
      serialize: ((params: any) =>
        stringify(params, { arrayFormat: "repeat" })) as unknown as CustomParamsSerializer
    }
  };
}

const defaultConfig: AxiosRequestConfig = getDefaultConfig();

class PureHttp {
  constructor() {
    this.httpInterceptorsRequest();
    this.httpInterceptorsResponse();
  }

  /** `token`过期后，暂存待执行的请求 */
  private static requests: Array<(token: string) => void> = [];

  /** 防止重复刷新`token` */
  private static isRefreshing = false;

  /** 刷新 Token 的 Promise 缓存 */
  private static refreshPromise: Promise<string> | null = null;

  /** 初始化配置对象 */
  private static initConfig: PureHttpRequestConfig = {};

  /** 保存当前`Axios`实例对象 */
  private static axiosInstance: AxiosInstance = Axios.create(defaultConfig);

  /** 请求白名单（精确匹配） */
  private static readonly WHITE_LIST = ["/refresh-token", "/login"];

  /** 检查URL是否在白名单中 */
  private static isWhiteListed(url: string): boolean {
    const path = url.startsWith("/api") ? url : `/api${url}`;
    return PureHttp.WHITE_LIST.some(
      whitePath => path === whitePath || path === `/api${whitePath}`
    );
  }

  /** 刷新 Token 并返回新的 accessToken */
  private static async refreshAccessToken(refreshToken: string): Promise<string> {
    if (PureHttp.refreshPromise) {
      return PureHttp.refreshPromise;
    }

    PureHttp.refreshPromise = useUserStoreHook()
      .handRefreshToken({ refreshToken })
      .then(res => {
        const token = res.data.accessToken;
        PureHttp.requests.forEach(cb => cb(token));
        PureHttp.requests = [];
        return token;
      })
      .catch(err => {
        PureHttp.requests = [];
        throw err;
      })
      .finally(() => {
        PureHttp.refreshPromise = null;
        PureHttp.isRefreshing = false;
      });

    return PureHttp.refreshPromise;
  }

  /** 请求拦截 */
  private httpInterceptorsRequest(): void {
    PureHttp.axiosInstance.interceptors.request.use(
      async (config: PureHttpRequestConfig): Promise<any> => {
        if (typeof config.beforeRequestCallback === "function") {
          config.beforeRequestCallback(config);
          return config;
        }
        if (PureHttp.initConfig.beforeRequestCallback) {
          PureHttp.initConfig.beforeRequestCallback(config);
          return config;
        }
        
        const url = config.url || "";
        if (PureHttp.isWhiteListed(url)) {
          return config;
        }

        const data = getToken();
        if (!data) {
          return config;
        }

        const now = new Date().getTime();
        const expired = parseInt(data.expires) - now <= 0;
        
        if (expired) {
          if (!PureHttp.isRefreshing) {
            PureHttp.isRefreshing = true;
          }
          
          try {
            const newToken = await PureHttp.refreshAccessToken(data.refreshToken);
            if (newToken) {
              config.headers["Authorization"] = formatToken(newToken);
            }
          } catch {
            // 刷新失败：返回被拒绝的 Promise，让外层 catch 处理（避免 401 重试）
            return Promise.reject({
              message: "Token 刷新失败，请重新登录",
              isCancelRequest: false
            });
          }
        } else {
          config.headers["Authorization"] = formatToken(data.accessToken);
        }

        return config;
      },
      error => {
        return Promise.reject(error);
      }
    );
  }

  /** 响应拦截 */
  private httpInterceptorsResponse(): void {
    const instance = PureHttp.axiosInstance;
    instance.interceptors.response.use(
      (response: PureHttpResponse) => {
        const $config = response.config;
        // 优先判断post/get等方法是否传入回调，否则执行初始化设置等回调
        if (typeof $config.beforeResponseCallback === "function") {
          $config.beforeResponseCallback(response);
          return response.data;
        }
        if (PureHttp.initConfig.beforeResponseCallback) {
          PureHttp.initConfig.beforeResponseCallback(response);
          return response.data;
        }
        return response.data;
      },
      (error: PureHttpError) => {
        const $error = error;
        $error.isCancelRequest = Axios.isCancel($error);
        // 提取后端返回的 message 字段，挂到 error.message 上
        // 后端 utils.Error / utils.ErrorWithDetail / utils.BadRequest 都返回
        //   { success: false, message: "xxx" } 或 { success: false, message, error }
        // 这样调用处 e?.message 就能拿到后端的真实提示
        const data = $error?.response?.data as
          | { success?: boolean; message?: string; error?: string }
          | undefined;
        if (data) {
          if (data.message) {
            $error.message = data.message;
          } else if (data.error) {
            $error.message = data.error;
          }
        }
        // 全局 403 handler（RBAC 第七阶段 v1.1，2026-06-26）
        // 用途：兜底 UX（v-perms 已隐藏按钮，但若 Plan B 60s 轮询间隔期内被改权限，
        //   用户点旧页面的某个按钮仍会触发 403）。同时加速 Plan B 失效检测：
        //   refreshFromApi 立即拉新 roles/permissions（不等 60s 轮询）。
        if ($error?.response?.status === 403) {
          handleGlobal403($error);
        }
        return Promise.reject($error);
      }
    );
  }

  /** 通用请求工具函数 */
  public request<T>(
    method: RequestMethods,
    url: string,
    param?: AxiosRequestConfig,
    axiosConfig?: PureHttpRequestConfig
  ): Promise<T> {
    const config = {
      method,
      url,
      baseURL: getBaseUrl(),
      ...param,
      ...axiosConfig
    } as PureHttpRequestConfig;

    // 单独处理自定义请求/响应回调
    return new Promise((resolve, reject) => {
      PureHttp.axiosInstance
        .request(config)
        .then((response: undefined) => {
          resolve(response);
        })
        .catch(error => {
          reject(error);
        });
    });
  }

  /** 单独抽离的`post`工具函数 */
  public post<T, P>(
    url: string,
    data?: P,
    config?: PureHttpRequestConfig
  ): Promise<T> {
    return this.request<T>("post", url, { data }, config);
  }
}

export const http = new PureHttp();
