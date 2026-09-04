import { ref, onMounted, onBeforeUnmount } from "vue";
import {
  getMessages,
  getUnreadCount,
  markAsRead,
  markAllAsRead,
  type Message
} from "@/api/message";
import { wsService, type WebSocketMessage } from "@/utils/websocket";
import { getToken } from "@/utils/auth";

/**
 * 顶部铃铛 / 消息提醒的统一 composable（2026-09-04 重构）。
 *
 * 设计目标：
 *  - 单一权威：noticesNum 只由 unread-count 接口写入，HTTP 拉 messages.list 不再覆盖角标。
 *  - WS 实时插入 + 重新拉 unread-count 校准（防 +1 漂移）。
 *  - 状态内聚：layout/components/lay-notice/index.vue 只渲染，不再持有计数 / 拉取逻辑。
 *  - 多实例友好：wsService.onSubscribe 解绑时移除回调；多 navbar 共用同一 ws 推送。
 *
 * 数据通道：
 *   - HTTP: GET /api/messages/list     → 列表（拉模式）
 *   - HTTP: GET /api/messages/unread-count → 角标（权威）
 *   - WS  : type="new_message"        → 插入列表头部 + 重新拉 unread-count 兜底
 *
 * 用法：
 *   const { noticesNum, messages, loading, refreshMessages, refreshUnread, markRead, markAllRead } = useMessageNotice();
 */
export function useMessageNotice() {
  // ---- state ----
  const noticesNum = ref(0); // 角标：未读消息数，唯一权威（来自 unread-count）
  const messages = ref<Message[]>([]); // "消息" tab 列表
  const loading = ref(false);

  // ---- HTTP: list ----
  async function refreshMessages(limit = 50, offset = 0) {
    loading.value = true;
    try {
      const res = await getMessages(limit, offset);
      messages.value = res.data?.messages ?? [];
    } catch (e) {
      console.error("[useMessageNotice] refreshMessages failed:", e);
    } finally {
      loading.value = false;
    }
  }

  // ---- HTTP: unread count（权威） ----
  async function refreshUnread() {
    try {
      const res = await getUnreadCount();
      if (res.data && typeof res.data.count === "number") {
        noticesNum.value = res.data.count;
      }
    } catch (e) {
      console.error("[useMessageNotice] refreshUnread failed:", e);
    }
  }

  // ---- HTTP: mark read ----
  async function markRead(id: number) {
    try {
      await markAsRead(id);
      // 本地乐观更新（角标与服务端会因已读而下降）
      const m = messages.value.find(x => x.id === id);
      if (m && !m.read) {
        m.read = true;
        noticesNum.value = Math.max(0, noticesNum.value - 1);
      }
    } catch (e) {
      console.error("[useMessageNotice] markRead failed:", e);
      throw e;
    }
  }

  async function markAllRead() {
    try {
      await markAllAsRead();
      messages.value.forEach(m => (m.read = true));
      noticesNum.value = 0;
    } catch (e) {
      console.error("[useMessageNotice] markAllRead failed:", e);
      throw e;
    }
  }

  // ---- WS: new_message 处理 ----
  // 后端 handlers/message.go wsNewMessagePayload 字段：
  //   { id, title, content, type, sender_id, user_id, created_at }
  // 旧实现用 content.id / content.created_at 兼容两种来源（hub 内部加 timestamp / 直推 payload）。
  function handleWsNewMessage(msg: WebSocketMessage) {
    const content = (msg.content ?? {}) as Partial<Message> & { created_at?: number | string };
    const newId = content.id;
    if (newId == null) return;

    // 去重：HTTP 拉过 / WS 已收过 的 id 不重复插入
    if (messages.value.some(m => m.id === newId)) return;

    messages.value.unshift({
      id: newId,
      user_id: content.user_id ?? 0,
      sender_id: content.sender_id ?? 0,
      title: content.title || "系统消息",
      content: content.content || "",
      type: content.type || "system",
      status: "unread",
      read: false,
      created_at: String(content.created_at ?? msg.timestamp ?? ""),
      updated_at: String(content.created_at ?? msg.timestamp ?? "")
    });
    // 角标交给 DB：每次 WS 推送后重新拉一次，与 DB 严格对齐
    refreshUnread();
  }

  // ---- lifecycle: 注册 / 解绑 WS 订阅 ----
  // 注意：wsService 在新版本（同步进行的 wsService.onMessage → 多订阅 Set 重构）下提供 onSubscribe(type, fn)。
  // 这里同时兼容旧的 onMessage 单值回调：通过 type 字段判断。
  let unsubscribe: (() => void) | null = null;

  onMounted(() => {
    // wsService 新接口：onSubscribe(type, handler) → 返回 unsubscribe
    const svc = wsService as unknown as {
      onSubscribe?: (type: string, fn: (msg: WebSocketMessage) => void) => () => void;
      onMessage?: (msg: WebSocketMessage) => void;
    };

    if (typeof svc.onSubscribe === "function") {
      unsubscribe = svc.onSubscribe("new_message", handleWsNewMessage);
    } else {
      // 兜底：旧 wsService 仍走单值 onMessage，多订阅器不冲突（仅一个订阅者场景）
      svc.onMessage = (data: WebSocketMessage) => {
        if (data.type === "new_message") handleWsNewMessage(data);
      };
      unsubscribe = () => {
        // 旧版没有反注册接口，靠组件卸载时整个 wsService.disconnect 由上层处理
      };
    }

    // 触发 WS 连接（如果 wsService 已处于 leader / 已连接会 no-op）。
    // 不在 wsService 内部自动 connect 是有意的：连接时机由"消息 composable"
    // 这种"谁订阅谁连"的契约来控制；切走路由 / 用户登出后 disconnect 由上层处理。
    const tokenInfo = getToken() as { accessToken?: string } | null;
    if (tokenInfo?.accessToken) {
      wsService.connect(tokenInfo.accessToken);
    }

    // 首屏拉一次：消息列表 + 未读数
    refreshMessages();
    refreshUnread();
  });

  onBeforeUnmount(() => {
    unsubscribe?.();
  });

  return {
    noticesNum,
    messages,
    loading,
    refreshMessages,
    refreshUnread,
    markRead,
    markAllRead
  };
}