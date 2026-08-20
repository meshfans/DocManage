import { defineStore } from "pinia";
import {
  type appType,
  store,
  getConfig,
  storageLocal,
  deviceDetection,
  responsiveStorageNameSpace
} from "../utils";

/**
 * 解析 localStorage 中持久化的 sidebar 状态
 * 修复：之前依赖 responsiveStorageNameSpace()，但首次加载时该值可能未就绪，
 *       导致刷新后无法恢复折叠状态。这里用固定 fallback key + 多重 fallback：
 *       1) 当前 namespace 的 layout.sidebarStatus
 *       2) "responsive-layout" 硬编码 key
 *       3) localStorage 直接扫所有 "*-layout" key（兼容不同 namespace）
 *       4) getConfig() 默认值
 */
function loadPersistedSidebarStatus(): boolean {
  // 1) 优先用当前 namespace
  const ns = responsiveStorageNameSpace();
  if (ns) {
    const v = storageLocal().getItem<StorageConfigs>(`${ns}layout`)?.sidebarStatus;
    if (typeof v === "boolean") return v;
  }
  // 2) 硬编码 key fallback（默认值）
  const fallbackKeys = ["responsive-layout", "pure-layout"];
  for (const key of fallbackKeys) {
    const v = storageLocal().getItem<StorageConfigs>(key)?.sidebarStatus;
    if (typeof v === "boolean") return v;
  }
  // 3) 扫所有 localStorage key（兼容不同 namespace 命名）
  if (typeof window !== "undefined") {
    for (let i = 0; i < window.localStorage.length; i++) {
      const key = window.localStorage.key(i);
      if (key && key.endsWith("-layout") && key !== "responsive-storage-configure") {
        try {
          const raw = window.localStorage.getItem(key);
          if (raw) {
            const parsed = JSON.parse(raw) as StorageConfigs;
            if (typeof parsed?.sidebarStatus === "boolean") {
              return parsed.sidebarStatus;
            }
          }
        } catch {
          /* 忽略非 JSON 值 */
        }
      }
    }
  }
  // 4) 默认值
  return getConfig().SidebarStatus ?? true;
}

export const useAppStore = defineStore("pure-app", {
  state: (): appType => ({
    sidebar: {
      // P 修复：用 loadPersistedSidebarStatus() 多重 fallback，确保刷新后能恢复
      opened: loadPersistedSidebarStatus(),
      withoutAnimation: false,
      isClickCollapse: false
    },
    // 这里的layout用于监听容器拖拉后恢复对应的导航模式
    layout:
      storageLocal().getItem<StorageConfigs>(
        `${responsiveStorageNameSpace()}layout`
      )?.layout ?? getConfig().Layout,
    device: deviceDetection() ? "mobile" : "desktop",
    // 浏览器窗口的可视区域大小
    viewportSize: {
      width: document.documentElement.clientWidth,
      height: document.documentElement.clientHeight
    },
    isExperienceMode: false
  }),
  getters: {
    getSidebarStatus(state) {
      return state.sidebar.opened;
    },
    getDevice(state) {
      return state.device;
    },
    getViewportWidth(state) {
      return state.viewportSize.width;
    },
    getViewportHeight(state) {
      return state.viewportSize.height;
    }
  },
  actions: {
    TOGGLE_SIDEBAR(opened?: boolean, resize?: string) {
      const layout = storageLocal().getItem<StorageConfigs>(
        `${responsiveStorageNameSpace()}layout`
      );
      if (opened && resize) {
        this.sidebar.withoutAnimation = true;
        this.sidebar.opened = true;
        layout.sidebarStatus = true;
      } else if (!opened && resize) {
        this.sidebar.withoutAnimation = true;
        this.sidebar.opened = false;
        layout.sidebarStatus = false;
      } else if (!opened && !resize) {
        this.sidebar.withoutAnimation = false;
        this.sidebar.opened = !this.sidebar.opened;
        this.sidebar.isClickCollapse = !this.sidebar.opened;
        layout.sidebarStatus = this.sidebar.opened;
      }
      storageLocal().setItem(`${responsiveStorageNameSpace()}layout`, layout);
    },
    async toggleSideBar(opened?: boolean, resize?: string) {
      await this.TOGGLE_SIDEBAR(opened, resize);
    },
    toggleDevice(device: string) {
      this.device = device;
    },
    setLayout(layout) {
      this.layout = layout;
    },
    setViewportSize(size) {
      this.viewportSize = size;
    },
    async loadExperienceMode() {
      try {
        const res = await fetch("/api/license/client-info", { credentials: "same-origin" });
        if (res.ok) {
          const data = await res.json();
          if (data?.success && data?.data?.experience_mode === true) {
            this.isExperienceMode = true;
          } else {
            this.isExperienceMode = false;
          }
        }
      } catch {
        // 端点不可达（404 / 启动早期）默认 false，非体验模式
        this.isExperienceMode = false;
      }
    }
  }
});

export function useAppStoreHook() {
  return useAppStore(store);
}
