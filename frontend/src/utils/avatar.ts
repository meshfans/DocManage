// avatar 解析工具
// 参考 /library/media 的 fetchMediaThumb 模式：
//   - 后端存路径 → 前端通过 fetchUserAvatar(id) 获取 blob URL 显示
//   - 调用方负责在 unmount 时 revoke 释放内存
//
// 特殊值：
//   - "" / null / undefined → 返回默认头像
//   - "logo.png" → 返回本地 logo 资源
//   - "avatar/{id}.{ext}" 或 "uploads/avatar/{id}.{ext}" → 异步获取 blob URL
//   - "http://..." / "https://..." / "/" 开头 → 原样返回

import DefaultAvatar from "@/assets/logo.png";
import { fetchUserAvatar as apiFetchUserAvatar } from "@/api/user_extended";

/**
 * 检查是否是用户头像路径（avatar/{id}.{ext} 或 uploads/avatar/{id}.{ext}）
 */
export function isUserAvatar(avatar: string | null | undefined): boolean {
  if (!avatar) return false;
  return /^(uploads[/\\])?avatar[/\\]\d+\.\w+$/i.test(avatar);
}

/**
 * 同步解析 avatar 路径（用于获取原始路径）
 * 注意：用户头像路径（avatar/xxx.webp）不直接作为 src 返回，
 *       而是由 loadAvatar() 加载 blob URL 后显示，避免刷新时先显示破碎图片
 */
export function resolveAvatarPath(avatar: string | null | undefined): string {
  if (!avatar || avatar.trim() === "") {
    return DefaultAvatar;
  }
  if (avatar === "logo.png") {
    return DefaultAvatar;
  }
  // 用户头像路径由 blob URL 处理，这里返回默认头像
  if (isUserAvatar(avatar)) {
    return DefaultAvatar;
  }
  return avatar;
}

/**
 * 获取用户头像的 blob URL（推荐，与媒体库一致）
 */
export async function fetchUserAvatarUrl(
  userId: number
): Promise<{ url: string; revoke: () => void } | null> {
  try {
    return await apiFetchUserAvatar(userId);
  } catch {
    return null;
  }
}

/**
 * 清除头像缓存（保留接口兼容）
 */
export function clearAvatarCache(): void {
  // 现在头像 URL 不再缓存，调用方自己管理 revoke
}
