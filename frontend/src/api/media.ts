import { http } from "@/utils/http";

// ==================== 媒体（v2 单表 + 3 哈希）====================
// 12 个端点对应后端 handlers/media.go

// ---------- 类型 ----------

export type MediaType = "photo" | "video" | "audio";
export type MediaSource = "camera" | "upload";
export type MediaStatus = "active" | "archived" | "deleted";

export type MediaTag = {
  name: string;
  color?: string;
};

export type MediaBinding = {
  target_type: "third_party";
  target_id: number;
  role: "attachment" | "evidence" | "original" | "copy";
  remark?: string;
};

export type MediaAuditEntry = {
  action: "view" | "download" | "delete" | "restore" | "upload" | "reused" | "update" | "create" | "bind" | "unbind";
  actor_id: number;
  ts: number; // unix seconds
};

export type MediaHashes = {
  sm3: string;
  sha256: string;
  combined: string;
  algorithm: string;
  data_length: number;
  calculated_at: number;
};

export type Media = {
  id: number;
  snowid: string;
  type: MediaType;
  name: string;
  original_name?: string;
  mime_type?: string;
  file_path: string;
  thumb_path?: string;
  file_size: number;
  width: number;
  height: number;
  duration: number;
  /** 3 种哈希（核心） */
  hash_sm3: string;
  hash_sha256: string;
  hash_combined: string;
  source: MediaSource;
  source_ref?: string;
  watermark_text?: string;
  taken_at: number;
  taken_by: number;
  /** v2.1：客户关联（主索引） + 用户备用字段 */
  customer_id: number;
  user_id: number;
  /** JSON 列：标签 */
  tags: MediaTag[];
  /** JSON 列：绑定 */
  bindings: MediaBinding[];
  /** JSON 列：审计（环形缓冲 50 条） */
  audit?: MediaAuditEntry[];
  view_count: number;
  download_count: number;
  remark?: string;
  status: MediaStatus;
  created_by: number;
  created_at: number;
  updated_at: number;
  deleted_at: number;
};

// ---------- 列表查询参数 ----------

export type ListMediaParams = {
  /** 类型过滤：photo / video / audio */
  type?: MediaType;
  /** 来源过滤：camera / upload */
  source?: MediaSource;
  /**
   * 状态过滤：
   *   - 缺省（""）→ 仅 active
   *   - "all"     → 不过滤 status
   *   - 其它       → 精确匹配（active / archived / deleted）
   */
  status?: MediaStatus | "all" | "";
  /** 拍摄人 user_id 过滤（0 = 不限） */
  taken_by?: number;
  /** 客户关联过滤：仅看该客户关联的媒体（0 = 不限）。主索引命中，性能 OK。 */
  customer_id?: number;
  /** 用户备用字段过滤（0 = 不限） */
  user_id?: number;
  /** taken_at 区间起点（unix seconds，0 = 不限） */
  from?: number;
  /** taken_at 区间终点（unix seconds，0 = 不限） */
  to?: number;
  /** 模糊搜索 name / original_name */
  q?: string;
  /** 按 tag 名过滤（多选 OR 关系；空 = 不限） */
  tag_names?: string[];
  page?: number;
  page_size?: number;
};

// ---------- API 响应包装 ----------

export type ListMediaResult = {
  success: boolean;
  data: {
    list: Media[];
    total: number;
    page: number;
    page_size: number;
  };
};

export type GetMediaResult = {
  success: boolean;
  data: Media;
};

export type UploadMediaResult = {
  success: boolean;
  data: {
    id: number;
    snowid: string;
    hash: MediaHashes;
    reused: boolean;
    data?: Media;
    message: string;
  };
};

export type CheckMediaHashResult = {
  success: boolean;
  data: {
    exists: boolean;
    id?: number;
    snowid?: string;
    file_path?: string;
    name?: string;
  };
};

export type VerifyMediaResult = {
  success: boolean;
  data: {
    valid: boolean;
    sm3_match: boolean;
    sha256_match: boolean;
    combined_match: boolean;
    stored: {
      sm3_hash: string;
      sha256_hash: string;
      combined_hash: string;
    };
    current: MediaHashes;
  };
};

// ==================== 12 个端点函数 ====================

// 1. 列表（分页 + 过滤）
export const listMedia = (params: ListMediaParams = {}) => {
  return http.request<ListMediaResult>("get", "/api/media", { params });
};

// 2. 按 target 反查（绑定维度的反向查询）
export const listMediaByTarget = (params: {
  target_type: string;
  target_id: number;
}) => {
  return http.request<ListMediaResult>("get", "/api/media/by-target", { params });
};

// 3. 详情（自动 view_count++ + audit push）
export const getMedia = (id: number) => {
  return http.request<GetMediaResult>("get", `/api/media/${id}`);
};

// 4. 创建元数据（一般用 upload 一站式；保留给"前端已上传到 CDN"场景）
export const createMedia = (data: Partial<Media> & { snowid: string; file_path: string; hash_sm3: string; hash_sha256: string; hash_combined: string }) => {
  return http.request<{ success: boolean; data: { id: number; snowid: string; data: Media; message: string } }>(
    "post",
    "/api/media/create",
    { data }
  );
};

// 5. 更新元数据（name / remark / status / tags / bindings / customer_id / user_id）
// 空白字段表示"不改"；customer_id / user_id 设 0 表示清除
export const updateMedia = (id: number, data: {
  name?: string;
  remark?: string;
  status?: MediaStatus;
  customer_id?: number;
  user_id?: number;
  tags?: MediaTag[];
  bindings?: MediaBinding[];
}) => {
  return http.request<{ success: boolean; message: string }>(
    "post",
    `/api/media/${id}`,
    { data }
  );
};

// 6. 软删
export const deleteMedia = (id: number) => {
  return http.request<{ success: boolean; message: string }>(
    "post",
    `/api/media/${id}/delete`
  );
};

// 7. 恢复
export const restoreMedia = (id: number) => {
  return http.request<{ success: boolean; message: string }>(
    "post",
    `/api/media/${id}/restore`
  );
};

// 8. 获取原文件（流式 → Blob）
// 参考 fetchSealImage 的做法：http.request + responseType: blob
// 因为浏览器原生 <a> 不会自动加 Authorization header，会 401。
//   options.download: 显式声明这是"实际下载"（后端 ?download=true 时才计入 download_count）
//                     预览 / 缩略图回退场景不要传 true，避免污染下载统计。
// 返回值：ObjectURL 字符串 + revoke 函数。
//   调用方应通过返回值直接渲染，或显式调用 .revoke() 释放内存。
//   为避免泄漏，建议在 img/视频元素 unmount 时调用 revokeMediaUrl。
export const fetchMediaFile = async (
  id: number,
  options?: { download?: boolean }
): Promise<MediaBlobUrl> => {
  const url = options?.download
    ? `/api/media/${id}/file?download=true`
    : `/api/media/${id}/file`;
  const blob = (await http.request("get", url, {
    responseType: "blob"
  })) as Blob;
  const objectUrl = URL.createObjectURL(blob);
  return {
    url: objectUrl,
    revoke: () => URL.revokeObjectURL(objectUrl)
  };
};

// 9. 下载缩略图（返回 ObjectURL + revoke 助手）
export const fetchMediaThumb = async (id: number): Promise<MediaBlobUrl> => {
  const blob = (await http.request("get", `/api/media/${id}/thumb`, {
    responseType: "blob"
  })) as Blob;
  const objectUrl = URL.createObjectURL(blob);
  return {
    url: objectUrl,
    revoke: () => URL.revokeObjectURL(objectUrl)
  };
};

/** fetchMediaFile / fetchMediaThumb 返回值类型。调用方应调用 revoke() 释放内存 */
export type MediaBlobUrl = {
  url: string;
  revoke: () => void;
};

/** 释放已创建的 ObjectURL（不抛错） */
export const revokeMediaUrl = (url: string | null | undefined) => {
  if (!url) return;
  try {
    URL.revokeObjectURL(url);
  } catch {
    /* 忽略：可能已被其他流程释放 */
  }
};

// 10. 上传（multipart + 3 哈希 + 自动去重）
//   - 同一文件二次上传返回 reused: true
//   - 第二次起 file 可以是 FormData 中的 file 字段
//   - 超时设为 5 分钟：兼容大文件（500MB 视频）上传
export const uploadMedia = (form: FormData) => {
  return http.request<UploadMediaResult>("post", "/api/media/upload", {
    data: form,
    headers: { "Content-Type": "multipart/form-data" },
    timeout: 5 * 60 * 1000
  });
};

// 11. 上传前查重（避免不必要的上传）
  // 后端 CheckHash handler 读 c.Query("hash_sha256")（**带下划线**）
  export const checkMediaHash = (hashSha256: string) => {
    return http.request<CheckMediaHashResult>(
      "get",
      "/api/media/check-hash",
      { params: { hash_sha256: hashSha256 } }
    );
  };

// 12. 三哈希校验（读文件重算，对比 DB 值）
export const verifyMedia = (id: number) => {
  return http.request<VerifyMediaResult>("get", `/api/media/${id}/verify`);
};

// 13. 列出所有出现过的 tag 名（去重，按字母序；用于过滤下拉）
export const listAllMediaTags = () => {
  return http.request<{ success: boolean; data: { tags: string[] } }>(
    "get",
    "/api/media/tags"
  );
};

// 14. 批量加 tag
export const bulkAddTag = (data: {
  ids: number[];
  tag_name: string;
  color?: string;
}) => {
  return http.request<{
    success: boolean;
    message?: string;
    data: { added: number; total: number; message: string };
  }>(
    "post",
    "/api/media/bulk-tag",
    { data }
  );
};

// 15. 批量软删除（与单条 Delete 一致：仅软标记，可恢复）
export const bulkDelete = (ids: number[]) => {
  return http.request<{
    success: boolean;
    message?: string;
    data: { deleted: number; total: number; message: string };
  }>(
    "post",
    "/api/media/bulk-delete",
    { data: { ids } }
  );
};

// 16. 批量关联客户
export const bulkSetCustomer = (data: {
  ids: number[];
  customer_id: number;
}) => {
  return http.request<{
    success: boolean;
    message?: string;
    data: { updated: number; total: number; message: string };
  }>(
    "post",
    "/api/media/bulk-customer",
    { data }
  );
};

// 2026-07-06 round3 精简：通用审计 API（GET /api/audit + POST /api/audit/reconcile）已下线。
// 后端仍保留 database.AppendAudit（其他业务写审计用），audit_log 表 DDL 不动
