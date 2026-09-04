import type { Message } from "@/api/message";

export interface ListItem {
  id?: number;
  avatar: string;
  title: string;
  datetime: string;
  type: string;
  description: string;
  status?: "primary" | "success" | "warning" | "info" | "danger";
  extra?: string;
  read?: boolean;
}

export interface TabItem {
  key: string;
  name: string;
  list: ListItem[];
  emptyText: string;
}

// 2026-09-04 重构：lay-notice 只渲染 "消息" tab，"通知" / "任务" 已下线。
// 见 .trae/documents/.../ 规划文档；后续若加新 tab，按 TabItem[] 形式扩展。
export const noticesData: TabItem[] = [
  {
    key: "2",
    name: "消息",
    list: [],
    emptyText: "暂无消息"
  }
];

// 把后端 Message 适配为 ListItem（UI 渲染用）。
// 重构前内联在 lay-notice/index.vue 的 fetchMessages / handleWsNewMessage 里，
// 现在统一收敛到一处。
export function listItemFromMessage(msg: Message): ListItem {
  return {
    id: msg.id,
    avatar: "",
    title: msg.title || "系统消息",
    datetime: formatMessageDate(msg.created_at),
    type: msg.type || "system",
    description: msg.content || "",
    read: msg.read
  };
}

// 把 created_at（秒或毫秒字符串）格式化为 "今天 / 昨天 / M-D"。
function formatMessageDate(timestamp: string | number): string {
  if (!timestamp) return "";
  const ts = typeof timestamp === "string" ? parseInt(timestamp) : timestamp;
  let date: Date;
  if (ts > 1e12) {
    date = new Date(ts);
  } else if (ts > 1e9) {
    date = new Date(ts * 1000);
  } else {
    return "";
  }
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const oneDay = 24 * 60 * 60 * 1000;
  if (diff < oneDay && date.getDate() === now.getDate()) {
    return "今天";
  } else if (diff < 2 * oneDay && date.getDate() === now.getDate() - 1) {
    return "昨天";
  } else {
    return `${date.getMonth() + 1}-${date.getDate()}`;
  }
}