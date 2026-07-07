// avatar 解析工具
// 第十三阶段 v4：DB 存的是字符串，需要映射到前端可用的 URL
// 特殊值：
//   - "" / null / undefined → 返回默认头像 user.jpg
//   - "logo.png"              → 返回本地 logo 资源
//   - "http://..." / "https://..." / "/" 开头 → 原样返回
//   - 其他（如 "/uploads/xxx.png"）→ 原样返回

import DefaultAvatar from "@/assets/logo.png";
import LogoAsset from "@/assets/logo.png";

/**
 * 解析 avatar 字段为可用的 URL。
 * @param avatar DB 存的 avatar 字符串
 * @returns 浏览器可直接加载的 URL
 */
export function resolveAvatar(avatar: string | null | undefined): string {
  if (!avatar || avatar.trim() === "") {
    return DefaultAvatar;
  }
  // 特殊关键字：DB 存的 "logo.png" → 映射到 import 后的资源
  if (avatar === "logo.png") {
    return LogoAsset;
  }
  // URL / 绝对路径 / data URI / 相对路径：原样返回
  return avatar;
}
