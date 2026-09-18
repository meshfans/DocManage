import { storeToRefs } from "pinia";
import { ref, watch, onBeforeUnmount, getCurrentInstance } from "vue";
import { getConfig } from "@/config";
import { emitter } from "@/utils/mitt";
import {
  resolveAvatarPath,
  fetchUserAvatarUrl,
  isUserAvatar
} from "@/utils/avatar";
import Logo from "@/assets/logo.png";
import { getTopMenu } from "@/router/utils";
import { useFullscreen } from "@vueuse/core";
import type { routeMetaType } from "../types";
import { useRouter, useRoute } from "vue-router";
import { router, remainingPaths } from "@/router";
import { computed, type CSSProperties } from "vue";
import { useAppStoreHook } from "@/store/modules/app";
import { useUserStoreHook } from "@/store/modules/user";
import { useGlobal, isAllEmpty } from "@pureadmin/utils";
import { usePermissionStoreHook } from "@/store/modules/permission";
import ExitFullscreen from "~icons/ri/fullscreen-exit-fill";
import Fullscreen from "~icons/ri/fullscreen-fill";

const errorInfo =
  "The current routing configuration is incorrect, please check the configuration";

// ============ 头像管理（单例）============
// 模块级 ref + revoke，所有 useNav() 调用共享同一份头像 URL。
// 这样 <img :src="userAvatar"> 只会有一个数据源，避免多组件竞争。
const avatarBlobUrl = ref<string>("");
let avatarRevoke: (() => void) | null = null;
let initialized = false;

function disposeAvatarUrl() {
  if (avatarRevoke) {
    try { avatarRevoke(); } catch { /* ignore */ }
    avatarRevoke = null;
  }
  avatarBlobUrl.value = "";
}

/**
 * 从 JWT token 解析 user_id
 *  - token 存在 Cookies "authorized-token" 里（key 由 TokenKey 常量定义）
 *  - 也兼容旧的 localStorage / sessionStorage 写法
 */
export function getUserIdFromToken(): number | null {
  // 1) Cookies 中查找（项目当前实现，参见 src/utils/auth.ts）
  try {
    const cookieToken = document.cookie
      .split("; ")
      .find((s) => s.startsWith("authorized-token="))
      ?.split("=")[1];
    if (cookieToken) {
      const decoded = decodeURIComponent(cookieToken);
      const parsed = JSON.parse(decoded);
      if (parsed?.accessToken) {
        const payload = JSON.parse(atob(parsed.accessToken.split(".")[1]));
        if (payload?.user_id) return payload.user_id;
      }
    }
  } catch {
    // ignore
  }

  // 2) localStorage / sessionStorage 兼容
  for (const storage of [localStorage, sessionStorage]) {
    for (const key of ["pure-user-token", "authorized-token", "user-info"]) {
      const raw = storage.getItem(key);
      if (!raw) continue;
      try {
        // 可能就是 token 字符串，也可能是 JSON 包装
        const tokenStr = raw.startsWith("{") ? JSON.parse(raw)?.accessToken ?? raw : raw;
        if (tokenStr.includes(".")) {
          const payload = JSON.parse(atob(tokenStr.split(".")[1]));
          if (payload?.user_id) return payload.user_id;
        }
      } catch {
        // ignore
      }
    }
  }

  return null;
}

/**
 * 加载用户头像
 */
async function loadAvatar(): Promise<void> {
  // 防止竞态：fetch 过程中又被调用先把旧 URL 释放
  disposeAvatarUrl();

  const avatar = useUserStoreHook().avatar;
  if (!isUserAvatar(avatar)) return;

  const userId = getUserIdFromToken();
  if (!userId) {
    console.warn("[avatar] no userId in token");
    return;
  }

  try {
    const result = await fetchUserAvatarUrl(userId);
    if (result) {
      avatarBlobUrl.value = result.url;
      avatarRevoke = result.revoke;
    }
  } catch (e) {
    console.warn("[avatar] load failed:", e);
  }
}

/**
 * 强制刷新头像（供外部组件在上传成功后调用）
 */
export function refreshNavAvatar() {
  disposeAvatarUrl();
  return loadAvatar();
}

/**
 * 模块级初始化：只注册一次 watch 和 emitter
 */
function ensureInit() {
  if (initialized) return;
  initialized = true;

  // 监听 store 中头像路径变化，重新 fetch blob URL
  watch(
    () => useUserStoreHook().avatar,
    (newAvatar) => {
      if (newAvatar) {
        loadAvatar();
      } else {
        disposeAvatarUrl();
      }
    },
    { immediate: true }
  );
}

export function useNav() {
  ensureInit();

  const route = useRoute();
  const pureApp = useAppStoreHook();
  const routers = useRouter().options.routes;
  const { isFullscreen, toggle } = useFullscreen();
  const { wholeMenus } = storeToRefs(usePermissionStoreHook());
  const tooltipEffect = getConfig()?.TooltipEffect ?? "light";

  const getDivStyle = computed((): CSSProperties => {
    return {
      width: "100%",
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      overflow: "hidden"
    };
  });

  // 头像 URL：blob URL 优先（异步加载），fallback 到路径或默认
  // 静态 URL 追加时间戳 cache busting，确保上传后浏览器立即显示新头像
  const userAvatar = computed(() => {
    if (avatarBlobUrl.value) {
      // blob URL 不需要 cache busting（每次都是新 URL）
      if (avatarBlobUrl.value.startsWith("blob:")) return avatarBlobUrl.value;
      // 静态路径加 ?t={ms} 强制刷新
      return `${avatarBlobUrl.value}?t=${Date.now()}`;
    }
    return resolveAvatarPath(useUserStoreHook()?.avatar);
  });

  /** 昵称 */
  const username = computed(() => {
    return isAllEmpty(useUserStoreHook()?.nickname)
      ? useUserStoreHook()?.username
      : useUserStoreHook()?.nickname;
  });

  const avatarsStyle = computed(() => {
    return username.value ? { marginRight: "10px" } : "";
  });

  const isCollapse = computed(() => {
    return !pureApp.getSidebarStatus;
  });

  const device = computed(() => {
    return pureApp.getDevice;
  });

  const { $storage, $config } = useGlobal<GlobalPropertiesApi>();
  const layout = computed(() => {
    return $storage?.layout?.layout;
  });

  const title = computed(() => {
    return $config.Title;
  });

  function changeTitle(meta: routeMetaType) {
    const Title = getConfig().Title;
    if (Title) document.title = `${meta.title} | ${Title}`;
    else document.title = meta.title;
  }

  function logout() {
    disposeAvatarUrl();
    useUserStoreHook().logOut();
  }

  function backTopMenu() {
    router.push(getTopMenu()?.path);
  }

  function onPanel() {
    emitter.emit("openPanel");
  }

  function toggleSideBar() {
    pureApp.toggleSideBar();
  }

  function handleResize(menuRef) {
    menuRef?.handleResize();
  }

  function resolvePath(route) {
    if (!route.children) return console.error(errorInfo);
    const httpReg = /^http(s?):\/\//;
    const routeChildPath = route.children[0]?.path;
    if (httpReg.test(routeChildPath)) {
      return route.path + "/" + routeChildPath;
    } else {
      return routeChildPath;
    }
  }

  function menuSelect(indexPath: string) {
    if (wholeMenus.value.length === 0 || isRemaining(indexPath)) return;
    emitter.emit("changLayoutRoute", indexPath);
  }

  function isRemaining(path: string) {
    return remainingPaths.includes(path);
  }

  function getLogo() {
    return Logo;
  }

  // 组件卸载时不 revoke（全局共享，由 logout 释放）
  // 但为了保险，仅在 navbar 卸载时释放，避免泄漏
  if (getCurrentInstance()) {
    onBeforeUnmount(() => {
      // 不在所有 useNav 调用方卸载时都 revoke，
      // 仅在应用退出（logout）时统一清理
    });
  }

  return {
    route,
    title,
    device,
    layout,
    logout,
    routers,
    $storage,
    isFullscreen,
    Fullscreen,
    ExitFullscreen,
    toggle,
    backTopMenu,
    onPanel,
    getDivStyle,
    changeTitle,
    toggleSideBar,
    handleResize,
    resolvePath,
    getLogo,
    isCollapse,
    menuSelect,
    pureApp,
    username,
    userAvatar,
    avatarsStyle,
    tooltipEffect
  };
}
