import type { RouteRecordName } from "vue-router";

export type cacheType = {
  mode: string;
  name?: RouteRecordName;
};

export type positionType = {
  startIndex?: number;
  length?: number;
};

export type appType = {
  sidebar: {
    opened: boolean;
    withoutAnimation: boolean;
    // 判断是否手动点击Collapse
    isClickCollapse: boolean;
  };
  layout: string;
  device: string;
  viewportSize: { width: number; height: number };
  // 体验模式（database.mode == "experience"）：后端拦截写请求，前端显示横幅、禁用提交按钮。
  // 登录成功后从 GET /api/license/client-info 加载。
  isExperienceMode: boolean;
};

export type multiType = {
  path: string;
  name: string;
  meta: any;
  query?: object;
  params?: object;
};

export type setType = {
  title: string;
  fixedHeader: boolean;
  hiddenSideBar: boolean;
};

export type userType = {
  avatar?: string;
  username?: string;
  nickname?: string;
  roles?: Array<string>;
  permissions?: Array<string>;
  /**
   * 当前 license 启用的 module key 集合（2026-10-01 C1/C2 统一）。
   *
   * 数据来源：登录后 GET /api/license/features 响应里的 module_keys 字段
   * （后端 handlers/license_features.go 从 sdk.GlobalOutcome 派生，
   *   白名单见 backend/pkg/license/sdk/feature.go 的 BusinessModuleKeys）。
   *
   * 当前白名单：rag / ocr / ai。
   *   - "ai" 决定 /system/ai-config 页面是否显示；
   *     无 "ai" 时隐藏该页，系统默认使用 meshfans 提供的 LLM 能力。
   *
   * 未登录或拉取失败时为空数组 → 所有 requiredFeature 页面保守隐藏。
   *
   * hasFeature(key) 用精确字符串相等匹配（区分大小写、不 trim、不子串）。
   */
  moduleKeys?: Array<string>;  isRemembered?: boolean;
  loginDay?: number;
};
