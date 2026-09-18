<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount } from "vue";
import { streamLLM } from "@/api/llm";
import { ChatDotRound, Close, Promotion, Loading, Refresh, ChatLineSquare, Message, Document, ChatRound } from "@element-plus/icons-vue";
import MarkdownRenderer from "@/components/MarkdownRenderer/index.vue";

const visible = ref(false);
const loading = ref(false);
const input = ref("");
const messages = ref<Array<{ role: "user" | "assistant"; content: string; usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number } }>>([]);
const content = ref("");
const errorMsg = ref("");
const chatContainer = ref<HTMLElement | null>(null);

// Token 统计
const lastUsage = ref<{ prompt_tokens: number; completion_tokens: number; total_tokens: number } | null>(null);

// 快捷功能（带 context）
const quickActions = [
  {
    label: "总结通话",
    icon: ChatLineSquare,
    template: "call_summary",
    context: { scene: "通话" },
    placeholder: "粘贴通话记录内容，我来帮你生成结构化摘要..."
  },
  {
    label: "总结聊天",
    icon: ChatRound,
    template: "chat_summary",
    context: { scene: "聊天" },
    placeholder: "粘贴聊天记录内容，我来帮你生成结构化摘要..."
  },
  {
    label: "写跟进",
    icon: Message,
    template: "followup_message",
    context: {},
    placeholder: "描述客户情况和跟进目的，我来帮你写跟进消息..."
  },
  {
    label: "写邮件",
    icon: Promotion,
    template: "business_email",
    context: {},
    placeholder: "描述邮件目的和背景，我来帮你起草商务邮件..."
  },
  {
    label: "拜访纪要",
    icon: Document,
    template: "visit_report",
    context: {},
    placeholder: "粘贴拜访记录内容，我来帮你生成结构化纪要..."
  },
];

// 当前选中的快捷功能
const selectedAction = ref<typeof quickActions[0] | null>(null);

async function handleSend() {
  if (!input.value.trim() || loading.value) return;

  const userInput = input.value.trim();
  input.value = "";

  // 添加用户消息
  messages.value.push({ role: "user", content: userInput });
  scrollToBottom();

  loading.value = true;
  errorMsg.value = "";
  content.value = "";

  // 构建请求上下文
  const requestContext: Record<string, unknown> = {};
  let moduleName = "";
  if (selectedAction.value) {
    moduleName = selectedAction.value.template;
    Object.assign(requestContext, selectedAction.value.context);
  }

  try {
    const response = await streamLLM({
      input: userInput,
      context: requestContext,
      module: moduleName,
    });

    // 检查 HTTP 状态码
    if (!response.ok) {
      let errorText = "AI 服务处理失败";
      if (response.status === 401) {
        errorText = "AI 配置无效，请检查 API Key";
      } else if (response.status === 403) {
        errorText = "无权限访问 AI 服务";
      } else if (response.status === 429) {
        errorText = "AI 请求过于频繁，请稍后重试";
      } else if (response.status >= 500) {
        errorText = "AI 服务暂时不可用，请稍后重试";
      }
      errorMsg.value = errorText;
      loading.value = false;
      return;
    }

    // response.body 防御性检查
    if (!response.body) {
      errorMsg.value = "请求失败：响应体为空";
      loading.value = false;
      return;
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let fullContent = "";
    let buffer = "";
    let streamFinished = false;

    // processLine 在外层定义，避免每次 while 循环重复创建函数
    const processLine = (data: string): boolean => {
      if (data === "[DONE]") {
        return true;
      }

      try {
        const parsed = JSON.parse(data);
        if (parsed.delta) {
          fullContent += parsed.delta;
          content.value = fullContent;
        }
        if (parsed.error) {
          errorMsg.value = parsed.error;
        }
        if (parsed.done) {
          // 保存 token 统计
          if (parsed.usage) {
            lastUsage.value = parsed.usage;
          }
          return true;
        }
      } catch {
        // ignore parse error
      }
      return false;
    };

    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        // 处理 buffer 中残留的最后一行
        if (buffer.trim()) {
          processLine(buffer.trim());
        }
        break;
      }

      buffer += decoder.decode(value, { stream: true });
      // 按双换行切分 SSE 事件（标准格式）
      const events = buffer.split("\n\n");
      // 最后一段可能不完整，留到下次处理
      buffer = events.pop() || "";

      for (const event of events) {
        const line = event.trim();
        if (!line.startsWith("data: ")) continue;
        const dataContent = line.slice(6);
        if (!dataContent) continue;
        if (processLine(dataContent)) {
          streamFinished = true;
        }
      }

      if (streamFinished) break;
    }

    // 流结束后统一处理：把流式 content 提交为完整消息，重置 loading
    if (fullContent) {
      messages.value.push({ role: "assistant", content: fullContent, usage: lastUsage.value || undefined });
    }
    content.value = "";
    loading.value = false;
  } catch (err) {
    const error = err as Error;
    // 网络错误友好提示
    if (error.name === "TypeError" && error.message.includes("fetch")) {
      errorMsg.value = "网络连接失败，请检查网络后重试";
    } else if (error.message.includes("aborted")) {
      errorMsg.value = "请求被中断，请重试";
    } else {
      errorMsg.value = error.message || "请求失败，请稍后重试";
    }
  } finally {
    loading.value = false;
    content.value = "";
  }

  scrollToBottom();
}

function handleQuickAction(action: typeof quickActions[0]) {
  selectedAction.value = action;
  input.value = "";
  // 清空对话，准备新的话题
  messages.value = [];
  content.value = "";
}

// 滚动节流：流式输出时高频调用，避免抖动
let scrollTimer: number | null = null;
function scrollToBottom() {
  if (scrollTimer !== null) return;
  scrollTimer = window.setTimeout(() => {
    scrollTimer = null;
    nextTick(() => {
      if (chatContainer.value) {
        chatContainer.value.scrollTop = chatContainer.value.scrollHeight;
      }
    });
  }, 16); // 约 60fps
}

function clearChat() {
  messages.value = [];
  content.value = "";
  errorMsg.value = "";
  lastUsage.value = null;
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    handleSend();
  }
}

// ESC 关闭
function handleEsc(e: KeyboardEvent) {
  if (e.key === "Escape" && visible.value) {
    visible.value = false;
  }
}

onMounted(() => {
  document.addEventListener("keydown", handleEsc);
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", handleEsc);
});
</script>

<template>
  <div>
    <!-- 浮动按钮 -->
    <div class="copilot-fab" @click="visible = !visible">
      <el-icon v-if="!visible" size="24"><ChatDotRound /></el-icon>
      <el-icon v-else size="20"><Close /></el-icon>
    </div>

    <!-- Copilot 面板 -->
    <Transition name="slide-fade">
      <div v-if="visible" class="copilot-panel">
        <div class="panel-header">
          <div class="header-title">
            <el-icon size="18" color="var(--el-color-primary)"><ChatDotRound /></el-icon>
            <span>AI 助手</span>
          </div>
          <el-button link size="small" @click="clearChat">
            <el-icon size="14"><Refresh /></el-icon>
            清空
          </el-button>
        </div>

        <!-- 消息区域 -->
        <div ref="chatContainer" class="chat-container">
          <div v-if="messages.length === 0 && !content" class="welcome">
            <p>你好，我是 AI 助手</p>
            <p style="font-size: 13px; color: var(--el-text-color-secondary)">
              我可以帮你：
            </p>
            <div class="quick-actions">
              <template v-for="(qa, idx) in quickActions" :key="qa.label">
                <button
                  v-if="idx % 2 === 0"
                  class="quick-btn"
                  :class="{ active: selectedAction?.label === qa.label }"
                  @click="handleQuickAction(qa)"
                >
                  <el-icon size="16"><component :is="qa.icon" /></el-icon>
                  {{ qa.label }}
                </button>
                <button
                  v-if="idx % 2 === 0 && quickActions[idx + 1]"
                  class="quick-btn"
                  :class="{ active: selectedAction?.label === quickActions[idx + 1]?.label }"
                  @click="handleQuickAction(quickActions[idx + 1])"
                >
                  <el-icon size="16"><component :is="quickActions[idx + 1]?.icon" /></el-icon>
                  {{ quickActions[idx + 1]?.label }}
                </button>
              </template>
            </div>
            <!-- 选中功能的提示 -->
            <div v-if="selectedAction" class="action-hint">
              <p>{{ selectedAction.placeholder }}</p>
            </div>
          </div>

          <div v-for="(msg, idx) in messages" :key="idx" :class="['message', msg.role]">
            <div class="message-content">
              <MarkdownRenderer :content="msg.content" />
              <div v-if="msg.role === 'assistant' && msg.usage" class="usage-info">
                <span>消耗 {{ msg.usage.total_tokens }} tokens</span>
              </div>
            </div>
          </div>

          <!-- 正在生成的内容 -->
          <div v-if="content" class="message assistant">
            <div class="message-content">
              <MarkdownRenderer :content="content" />
            </div>
          </div>

          <!-- 加载中 -->
          <div v-if="loading && !content" class="message assistant">
            <div class="message-content loading">
              <el-icon class="is-loading" size="16"><Loading /></el-icon>
              思考中...
            </div>
          </div>

          <!-- 错误 -->
          <div v-if="errorMsg" class="message error">
            <div class="message-content">{{ errorMsg }}</div>
          </div>
        </div>

        <!-- 输入区域 -->
        <div class="input-area">
          <el-input
            v-model="input"
            type="textarea"
            :rows="2"
            resize="none"
            placeholder="输入问题，按 Enter 发送..."
            :disabled="loading"
            @keydown="handleKeydown"
          />
          <div class="input-toolbar">
            <div class="toolbar-left">
              <el-tooltip content="新对话" placement="top">
                <el-button link size="small" @click="clearChat">
                  <el-icon size="18"><ChatLineSquare /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
            <div class="toolbar-right">
              <el-button
                type="primary"
                circle
                :disabled="!input.trim() || loading"
                @click="handleSend"
              >
                <el-icon><Promotion /></el-icon>
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.copilot-fab {
  position: fixed;
  right: 24px;
  bottom: 24px;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--el-color-primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  z-index: 1000;
  transition: all 0.3s;
}

.copilot-fab:hover {
  transform: scale(1.1);
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.2);
}

.copilot-panel {
  position: fixed;
  right: 24px;
  bottom: 88px;
  width: 380px;
  height: 520px;
  background: var(--el-bg-color);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.15);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  z-index: 999;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-lightest);
}

.header-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.chat-container {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}

.welcome {
  text-align: center;
  padding: 20px 0;
  color: var(--el-text-color-primary);
}

.quick-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.quick-btn {
  flex: 1;
  min-width: calc(50% - 4px);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 8px;
  background: var(--el-fill-color-lightest);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.quick-btn:hover {
  background: var(--el-fill-color-light);
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.quick-btn.active {
  background: var(--el-color-primary-light-9);
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.action-hint {
  margin-top: 12px;
  padding: 10px 12px;
  background: var(--el-color-primary-light-9);
  border-radius: 8px;
  font-size: 13px;
  color: var(--el-color-primary);
}

.message {
  margin-bottom: 12px;
  display: flex;
}

.message.user {
  justify-content: flex-end;
}

.message.user .message-content {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.message.assistant .message-content {
  background: var(--el-fill-color-lightest);
  color: var(--el-text-color-primary);
}

.message.error .message-content {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
}

.message-content {
  max-width: 85%;
  padding: 10px 14px;
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.message.user .message-content {
  border-bottom-right-radius: 4px;
}

.message.assistant .message-content {
  border-bottom-left-radius: 4px;
}

.loading {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-secondary);
}

.usage-info {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  opacity: 0.8;
  margin-top: 4px;
}

.input-area {
  padding: 8px 12px 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-lightest);
}

.input-area :deep(.el-textarea__inner) {
  border-radius: 16px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  padding: 10px 12px;
}

.input-area :deep(.el-textarea__inner:focus) {
  border-color: var(--el-color-primary);
}

.input-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
  padding: 0 4px;
}

.toolbar-left {
  display: flex;
  gap: 4px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 过渡动画 */
.slide-fade-enter-active {
  transition: all 0.3s ease-out;
}

.slide-fade-leave-active {
  transition: all 0.2s ease-in;
}

.slide-fade-enter-from,
.slide-fade-leave-to {
  opacity: 0;
  transform: translateY(20px);
}
</style>
