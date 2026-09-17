import { http } from "@/utils/http";
import type { ApiEnvelope } from "./_envelope";

// 备份管理（第四阶段 Phase 4.3）前端 API。
// 对应后端 server/handlers/backup.go。

/** 备份清单（与后端 BackupManifest 对齐） */
export interface BackupManifest {
  id: number;
  snowid: string;
  /** full / incremental / restore_audit（恢复审计，2026-06-27 新增）*/
  type: "full" | "incremental" | "restore_audit";
  parent_id: number;
  status:
    | "pending"
    | "running"
    | "success"
    | "verified"
    | "corrupted"
    | "missing"
    | "failed";
  file_path: string;
  file_size: number;
  file_hash_sm3: string;
  file_hash_sha256: string;
  file_hash_combined: string;
  verified_at: number;
  verified_result: string;
  started_at: number;
  finished_at: number;
  duration_ms: number;
  keep_until: number;
  total_files: number;
  changed_files: number;
  wal_range_start: number;
  wal_range_end: number;
  error_msg: string;

  // 2026-07-06 round6 精简：删除 3 个加密字段
  //   - encrypted / encryption_algo / encrypt_passphrase
  //   - 后端 BackupManifest 同步下线（manifest 表少 4 列）
  //   - 备份永远明文，不再有加密相关字段
}

/** 审计记录识别（恢复操作产生的记录，非真实备份）*/
export function isAuditRecord(m: BackupManifest): boolean {
  // 2026-06-27：审计记录有独立 type=restore_audit，不再用 file_path 前缀识别
  //   旧逻辑（type=full + file_path 以 "restore:" 开头）已废弃
  //   兼容：保留 file_path 前缀判断用于旧版（2026-06-27 前的存量审计记录）
  return m.type === "restore_audit" || m.file_path.startsWith("restore:");
}

/** 列表响应 */
export interface BackupListResp {
  list: BackupManifest[];
  total: number;
  page: number;
  page_size: number;
  page_size_bytes: number;
  by_status: Record<string, number>;
}

/** 列表查询参数 */
export interface ListBackupsParams {
  type?: "full" | "incremental";
  status?: BackupManifest["status"];
  page?: number;
  page_size?: number;
}

/** GET /api/backup/list */
export function listBackups(params: ListBackupsParams = {}) {
  return http.request<ApiEnvelope<BackupListResp>>(
    "get",
    "/api/backup/list",
    { params }
  );
}

/** GET /api/backup/:id */
export function getBackup(id: number) {
  return http.request<ApiEnvelope<BackupManifest>>("get", `/api/backup/${id}`);
}

/** POST /api/backup/:id/delete */
export function deleteBackup(id: number) {
  return http.request<{ success: boolean; message?: string }>(
    "post",
    `/api/backup/${id}/delete`
  );
}

/**
 * 下载备份 zip 文件。
 *
 * 关键：用 axios 接收 blob（保留 Authorization header），再用 a[download] 触发浏览器保存。
 * 早期实现用 window.open 触发 GET 请求，**不会携带 axios 实例的 Authorization header**，
 * 导致后端 JWTAuth 中间件返回 401，下载功能 100% 不可用。
 */
export async function downloadBackup(id: number) {
  const res = await http.request<Blob>("get", `/api/backup/${id}/download`, {
    responseType: "blob"
  });
  // res.data 是 Blob 对象
  const blob = res as unknown as Blob;

  // 从 blob 或 headers 中取文件名（这里直接用 manifest id + 默认后缀，
  // 因为 axios blob 模式下读取自定义 header 较复杂）
  const filename = `backup_${id}.zip`;

  // 创建下载链接
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  window.URL.revokeObjectURL(url);
}

/** POST /api/system/backup-now 立即备份（手动） */
export function backupNow() {
  return http.request<{
    success: boolean;
    message?: string;
    data?: { manifest_id: number };
  }>("post", "/api/system/backup-now");
}

/** POST /api/system/restore 恢复（第四阶段 P0 维护模式自动开启） */
export interface RestorePayload {
  full_backup_id: number;
  incremental_ids?: number[];
  dry_run: boolean;
}
export function restoreBackup(payload: RestorePayload) {
  return http.request<{ success: boolean; message?: string }>(
    "post",
    "/api/system/restore",
    { data: payload }
  );
}
