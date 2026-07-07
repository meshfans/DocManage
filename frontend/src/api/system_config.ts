import { http } from "@/utils/http";

// 业务配置（system_config 表）前端 API。
// DEFAULT_CONFIGS 在后端，DB 覆盖值优先。

// 单条配置元数据（用于"业务配置"页表格展示）
export type ConfigMeta = {
  key: string;
  value: string;
  default_value: string;
  description: string;
  category: string;
  is_overridden: boolean;
};

// 公开端点：所有登录用户可读（前端启动时调，决策 UI 行为）
export const getPublicSystemConfigs = () => {
  return http.request<{ success: boolean; data: Record<string, string> }>(
    "get",
    "/api/system-config/public"
  );
};

// 管理端点：列出全部配置项（含元信息：默认值 / 描述 / 是否被覆盖）
// 仅 admin 可调用
export const listSystemConfigs = () => {
  return http.request<{ success: boolean; data: ConfigMeta[] }>(
    "get",
    "/api/system-config/list"
  );
};

// 修改/新增一个配置项
// 仅 admin 可调用
export const updateSystemConfig = (key: string, value: string) => {
  return http.request<{
    success: boolean;
    data: { key: string; value: string };
    message?: string;
  }>("post", "/api/system-config/update", { data: { key, value } });
};

// 删除一个 key（回退到 DEFAULT_CONFIGS 兜底）
// 仅 admin 可调用
export const deleteSystemConfig = (key: string) => {
  return http.request<{ success: boolean; message?: string }>(
    "post",
    `/api/system-config/${encodeURIComponent(key)}/delete`
  );
};
