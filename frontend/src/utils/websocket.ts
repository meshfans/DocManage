import { ref } from "vue";
import { getConfig } from "@/config";
import { isDocClient } from "@/utils/isDocClient";

export interface WebSocketMessage {
  type: string;
  content: any;
  timestamp?: string;
}

// 多标签页共享：使用 BroadcastChannel 协调
const WS_CHANNEL_NAME = "docmanage-websocket-coordinator";
const LEAD_ELECTION_TIMEOUT = 2000; // 等待 leader 选举的超时

class WebSocketService {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 10;
  private reconnectDelay = 3000;
  private maxReconnectDelay = 30000;
  private heartbeatInterval: number | null = null;
  private pingTimeout: number | null = null;
  private authToken: string = "";
  private isAuthenticated = false;
  private explicitlyClosed = false;

  // 多标签页共享
  private channel: BroadcastChannel | null = null;
  private isLeader: boolean = false;
  private leaderId: string = "";
  private instanceId: string = "";
  private pendingMessages: WebSocketMessage[] = [];

  public onMessage: (data: WebSocketMessage) => void = () => {};
  public onConnect: () => void = () => {};
  public onDisconnect: () => void = () => {};
  public onAuthSuccess: () => void = () => {};
  public onAuthError: (error: string) => void = () => {};
  public isConnected = ref(false);

  private readonly HEARTBEAT_INTERVAL = 30000;
  private readonly PING_TIMEOUT = 10000;
  private initDelay: number = 0;

  constructor() {
    // 生成唯一实例 ID
    this.instanceId = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
    // 随机延迟 0-500ms，减少多标签页同时连接的压力
    this.initDelay = Math.random() * 500;
  }

  /**
   * 建立 WebSocket 连接。
   * 多标签页协调：使用 BroadcastChannel
   * - 首个标签页成为 leader，持有 WebSocket 连接
   * - 其他标签页通过 BroadcastChannel 接收 leader 转发的消息
   */
  connect(token: string) {
    if (!token) {
      console.error("[WS] connect: token is empty");
      return;
    }

    // 已有 OPEN/CONNECTING 连接则不重连
    if (
      this.ws &&
      (this.ws.readyState === WebSocket.OPEN ||
        this.ws.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    this.authToken = token;
    this.isAuthenticated = false;
    this.explicitlyClosed = false;

    // 初始化 BroadcastChannel
    this.initBroadcastChannel();
  }

  /**
   * 初始化 BroadcastChannel 用于多标签页协调
   */
  private initBroadcastChannel() {
    if (this.channel) {
      return;
    }

    try {
      this.channel = new BroadcastChannel(WS_CHANNEL_NAME);

      // 接收来自其他标签页的消息
      this.channel.onmessage = (event) => {
        const msg = event.data;

        if (msg.type === "leader-announce") {
          // 收到 leader 公告
          this.leaderId = msg.leaderId;
          if (this.instanceId !== msg.leaderId) {
            this.isLeader = false;
            console.log(`[WS] Non-leader (${this.instanceId}), leader is ${msg.leaderId}`);
          }
          return;
        }

        if (msg.type === "leader-elect") {
          // 收到 leader 选举请求
          if (this.instanceId > msg.candidateId) {
            // 当前实例 ID 更大，成为 leader
            this.becomeLeader();
          }
          return;
        }

        if (msg.type === "message-forward") {
          // 收到转发的消息
          if (msg.instanceId !== this.instanceId) {
            console.log("[WS] Received forwarded message:", msg.message.type);
            this.onMessage(msg.message);
          }
          return;
        }

        if (msg.type === "send-request") {
          // 收到发送消息请求（从非 leader 标签页发来）
          if (this.isLeader && this.ws?.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(msg.message));
          }
          return;
        }

        if (msg.type === "leader-disconnect") {
          // Leader 断开，开始选举
          if (this.leaderId === msg.instanceId) {
            this.startLeaderElection();
          }
          return;
        }
      };

      // 尝试成为 leader 或等待 leader
      this.tryBecomeOrWaitForLeader();

    } catch (error) {
      console.error("[WS] BroadcastChannel init failed, fallback to single mode:", error);
      this.connectInternal();
    }
  }

  /**
   * 尝试成为 leader 或等待现有 leader
   */
  private tryBecomeOrWaitForLeader() {
    // 随机延迟后发送选举请求，减少同时连接压力
    setTimeout(() => {
      // 先发送选举请求，等待一小段时间让其他标签页响应
      this.channel?.postMessage({
        type: "leader-elect",
        candidateId: this.instanceId
      });

      // 延迟后检查是否有 leader，没有则成为 leader
      setTimeout(() => {
        if (!this.leaderId) {
          this.becomeLeader();
        }
      }, LEAD_ELECTION_TIMEOUT);
    }, this.initDelay);
  }

  /**
   * 开始 leader 选举
   */
  private startLeaderElection() {
    this.isLeader = false;
    this.leaderId = "";
    this.disconnectWs();
    this.tryBecomeOrWaitForLeader();
  }

  /**
   * 成为 leader，建立 WebSocket 连接
   */
  private becomeLeader() {
    this.isLeader = true;
    this.leaderId = this.instanceId;
    console.log(`[WS] I am the leader (${this.instanceId})`);

    // 广播我是 leader
    this.channel?.postMessage({
      type: "leader-announce",
      leaderId: this.instanceId
    });

    // 建立 WebSocket 连接
    this.connectInternal();
  }

  /**
   * 内部建立 WebSocket 连接（由 leader 调用）
   */
  private connectInternal() {
    // 防止重复建立连接
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      console.log("[WS] 连接已存在，跳过");
      return;
    }

    const wsBaseUrl = this.resolveWsBaseUrl();
    const url = new URL(wsBaseUrl);
    url.searchParams.set("token", this.authToken);
    const wsUrl = url.toString();

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        try {
          this.ws!.send(JSON.stringify({ type: "auth", content: this.authToken }));
        } catch (e) {
          console.error("[WS] send auth 失败:", e);
        }
      };

      this.ws.onmessage = (event) => {
        try {
          const data: WebSocketMessage = JSON.parse(event.data);

          if (data.type === "auth") {
            if ((data.content as any)?.status === "ok") {
              this.isAuthenticated = true;
              this.reconnectAttempts = 0;
              this.isConnected.value = true;
              this.onAuthSuccess();
              this.startHeartbeat();
            } else {
              this.isAuthenticated = false;
              this.onAuthError((data.content as any)?.message || "认证失败");
              this.ws?.close();
            }
            return;
          }

          if (data.type === "error") {
            console.warn("[WS] server error:", data.content);
            return;
          }

          if (data.type === "new_message") {
            this.onMessage(data);
            // 转发给其他标签页
            this.forwardToSlaves(data);
          }

          if (data.type === "pong") {
            this.clearPingTimeout();
          }

          this.resetHeartbeat();
        } catch (e) {
          console.error("[WS] 解析消息失败:", e);
        }
      };

      this.ws.onclose = (event) => {
        this.isConnected.value = false;
        this.isAuthenticated = false;
        this.stopHeartbeat();
        this.onDisconnect();

        if (event.code === 1008 || event.code === 4401 || event.code === 4403) {
          console.warn("[WS] auth rejected, stop reconnect. code=", event.code);
          this.explicitlyClosed = true;
          return;
        }

        if (this.explicitlyClosed) {
          console.log("[WS] explicitly closed, skip reconnect");
          return;
        }

        // 通知其他标签页 leader 断开（如果自己是 leader）
        if (this.isLeader) {
          this.channel?.postMessage({
            type: "leader-disconnect",
            instanceId: this.instanceId
          });
          // 兑底：如果 leader-disconnect 消息因 BroadcastChannel 异常（跨 origin / Worker context）丢失，
          // 其他 tab 不会触发选举，本 tab 也不会自愈。给 leader 自己额外挂一个重连定时器，
          // 即使没有 follower 响应也能恢复连接。
          this.attemptReconnect();
        }
      };

      this.ws.onerror = (error) => {
        console.error("[WS] 错误:", error);
      };
    } catch (error) {
      console.error("[WS] new WebSocket 失败:", error);
      if (this.isLeader) {
        this.attemptReconnect();
      }
    }
  }

  /**
   * 转发消息给非 leader 标签页
   */
  private forwardToSlaves(message: WebSocketMessage) {
    this.channel?.postMessage({
      type: "message-forward",
      instanceId: this.instanceId,
      message
    });
  }

  /**
   * 断开 WebSocket 连接
   */
  private disconnectWs() {
    if (this.ws) {
      try {
        this.ws.close();
      } catch (e) {
        // ignore
      }
      this.ws = null;
    }
  }

  /**
   * 发送消息（所有标签页都调用此方法）
   */
  send(message: { type: string; content?: any }) {
    const msgStr = JSON.stringify(message);

    if (this.isLeader && this.ws?.readyState === WebSocket.OPEN) {
      // Leader 直接发送
      try {
        this.ws.send(msgStr);
      } catch (e) {
        console.error("[WS] send 失败:", e);
      }
    } else {
      // 非 leader 通过 BroadcastChannel 请求 leader 发送
      this.channel?.postMessage({
        type: "send-request",
        instanceId: this.instanceId,
        message
      });
    }
  }

  /**
   * 拼出 ws/wss + host + /api/ws
   */
  private resolveWsBaseUrl(): string {
    if (isDocClient()) {
      const platformConfig = getConfig();
      if (platformConfig?.WSUrl) {
        return platformConfig.WSUrl;
      }
      const apiBaseUrl = platformConfig?.ApiBaseUrl || "http://127.0.0.1:18080";
      const wsProtocol = apiBaseUrl.startsWith("https") ? "wss:" : "ws:";
      const wsHost = apiBaseUrl.replace(/^https?:\/\//, "");
      return `${wsProtocol}//${wsHost}/api/ws`;
    }
    // 浏览器模式：相对路径走 vite dev proxy（ws 透传）
    const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${wsProtocol}//${window.location.host}/api/ws`;
  }

  private startHeartbeat() {
    this.stopHeartbeat();

    this.heartbeatInterval = window.setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN && this.isAuthenticated) {
        try {
          this.ws.send(JSON.stringify({ type: "ping" }));
        } catch (e) {
          console.error("[WS] send ping 失败:", e);
          return;
        }

        this.clearPingTimeout();
        this.pingTimeout = window.setTimeout(() => {
          console.warn("[WS] ping 超时，主动断开");
          this.ws?.close();
        }, this.PING_TIMEOUT);
      }
    }, this.HEARTBEAT_INTERVAL);
  }

  private resetHeartbeat() {
    if (this.isAuthenticated) {
      this.stopHeartbeat();
      this.startHeartbeat();
    }
  }

  private clearPingTimeout() {
    if (this.pingTimeout) {
      clearTimeout(this.pingTimeout);
      this.pingTimeout = null;
    }
  }

  private stopHeartbeat() {
    if (this.heartbeatInterval) {
      clearInterval(this.heartbeatInterval);
      this.heartbeatInterval = null;
    }
    this.clearPingTimeout();
  }

  private attemptReconnect() {
    if (this.explicitlyClosed) return;
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.warn("[WS] 已达最大重连次数，停止重连");
      return;
    }
    this.reconnectAttempts++;
    const delay = Math.min(
      this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1),
      this.maxReconnectDelay
    );

    setTimeout(() => {
      if (this.authToken) {
        this.connectInternal();
      }
    }, delay);
  }

  disconnect() {
    this.explicitlyClosed = true;
    this.stopHeartbeat();
    this.isAuthenticated = false;
    this.disconnectWs();

    // 通知其他标签页 leader 断开
    if (this.isLeader) {
      this.channel?.postMessage({
        type: "leader-disconnect",
        instanceId: this.instanceId
      });
    }

    // 关闭 BroadcastChannel
    if (this.channel) {
      this.channel.close();
      this.channel = null;
    }
  }
}

export const wsService = new WebSocketService();