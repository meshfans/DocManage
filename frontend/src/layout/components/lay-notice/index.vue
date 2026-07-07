<script setup lang="ts">
/**
 * 顶部铃铛通知（lay-notice）
 *
 * 2026-07-06 round2 精简：流转模块下线后，移除以下内容：
 *   - import { getMyPendingTasks } from "@/api/flow_instance"（api 已删除）
 *   - fetchTasks() 函数（拉取 /api/flows/my-pending）
 *   - WS 收到 flow_task / new_flow_task 时的刷新
 *   - tab "3"（流转待办）查找逻辑
 *   - 角标累计任务数逻辑
 *   - 点击 flow_task 跳 /flow/pending 的路由
 *   - noticesData 中第三个 tab 项
 *
 * 2026-07-06 round4 精简：WebSocket 模块下线后，移除以下内容：
 *   - import { wsService } from "@/utils/websocket"（utils/websocket.ts 已删）
 *   - import { getToken } from "@/utils/auth"（WS 鉴权用，删除）
 *   - connectWebSocket() 整个函数
 *   - onMounted 中的 connectWebSocket() 调用
 *   - onUnmounted 中的 wsService.disconnect() 调用
 *   - import { onUnmounted } 也随之未用
 *
 * 业务影响：实时推送下线，新消息需用户手动展开铃铛或刷新页面才可见
 *   （getMessages / getUnreadCount 在用户每次打开铃铛时拉取一次）
 */
import { ref, computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { noticesData, ListItem } from "./data";
import NoticeList from "./components/NoticeList.vue";
import BellIcon from "~icons/ep/bell";
import { getMessages, getUnreadCount, markAsRead, markAllAsRead } from "@/api/message";
import { ElMessage } from "element-plus";

const router = useRouter();

const noticesNum = ref(0);
const messageUnread = ref(0); // 消息未读数（独立计数）
const notices = ref(noticesData);
const activeKey = ref(noticesData[0]?.key);
const loading = ref(false);

const fetchMessages = async () => {
  loading.value = true;
  try {
    const res = await getMessages(50, 0);
    if (res.data?.messages) {
      const messages = res.data.messages.map((msg: any) => ({
        avatar: "",
        title: msg.title || "系统消息",
        datetime: formatDate(msg.created_at),
        type: msg.type || "system",
        description: msg.content || "",
        read: msg.read,
        id: msg.id
      }));

      const messageTab = notices.value.find((n: any) => n.key === "2");
      if (messageTab) {
        messageTab.list = messages;
      }

      // 角标 = 消息未读数（2026-07-06 round2：移除任务数累加）
      noticesNum.value = messages.filter((m: any) => !m.read).length;
    }
  } catch (error: any) {
    console.error("获取消息失败", error);
  } finally {
    loading.value = false;
  }
};

const fetchUnreadCount = async () => {
  try {
    const res = await getUnreadCount();
    if (res.data?.count !== undefined) {
      messageUnread.value = res.data.count;
      // 角标 = 消息未读数（2026-07-06 round2：移除任务数累加）
      noticesNum.value = messageUnread.value;
    }
  } catch (error: any) {
    console.error("获取未读消息数失败:", error);
  }
};

const handleNoticeClick = async (item: ListItem) => {
  // 2026-07-06 round2：移除 "if (item.type === 'flow_task')" 分支（流转模块下线）

  if (!item.id || item.read) return;

  try {
    await markAsRead(item.id);
    item.read = true;
    noticesNum.value = Math.max(0, noticesNum.value - 1);

    const messageTab = notices.value.find((n: any) => n.key === "2");
    if (messageTab) {
      const msg = messageTab.list.find((m: any) => m.id === item.id);
      if (msg) {
        msg.read = true;
      }
    }
  } catch (error) {
    console.error("标记已读失败", error);
    ElMessage.error("标记已读失败");
  }
};

const handleMarkAllAsRead = async () => {
  try {
    await markAllAsRead();
    noticesNum.value = 0;

    const messageTab = notices.value.find((n: any) => n.key === "2");
    if (messageTab) {
      messageTab.list.forEach((msg: any) => {
        msg.read = true;
      });
    }
  } catch (error) {
    console.error("标记全部已读失败", error);
    ElMessage.error("标记全部已读失败");
  }
};

onMounted(() => {
  fetchMessages();
  // 2026-07-06 round2：移除 fetchTasks() 调用
  // 2026-07-06 round4：移除 connectWebSocket() 调用（WS 模块下线）
});

const formatDate = (timestamp: string): string => {
  if (!timestamp) return "";
  const ts = parseInt(timestamp);
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
};

const getLabel = computed(
  () => (item: any) => {
    const unreadCount = item.list?.filter((m: any) => !m.read).length || 0;
    return item.name + (unreadCount > 0 ? `(${unreadCount})` : "");
  }
);

defineExpose({
  fetchMessages,
  fetchUnreadCount
});
</script>

<template>
  <el-dropdown trigger="click" placement="bottom-end" @visible-change="(visible: boolean) => visible && fetchMessages()">
    <span
      :class="[
        'dropdown-badge',
        'navbar-bg-hover',
        'select-none',
        Number(noticesNum) !== 0 && 'mr-[10px]'
      ]"
    >
      <el-badge :value="Number(noticesNum) === 0 ? '' : noticesNum" :max="99">
        <span class="header-notice-icon">
          <IconifyIconOffline :icon="BellIcon" />
        </span>
      </el-badge>
    </span>
    <template #dropdown>
      <el-dropdown-menu>
        <el-tabs
          v-model="activeKey"
          :stretch="true"
          class="dropdown-tabs"
          :style="{ width: notices.length === 0 ? '200px' : '330px' }"
        >
          <el-empty
            v-if="notices.length === 0"
            description="暂无消息"
            :image-size="60"
          />
          <span v-else>
            <template v-for="item in notices" :key="item.key">
              <el-tab-pane :label="getLabel(item)" :name="`${item.key}`">
                <div class="flex justify-between items-center px-5 py-2 border-b border-gray-200 dark:border-gray-700">
                  <span class="text-sm text-gray-500">共 {{ item.list?.length || 0 }} 条消息</span>
                  <el-button
                    v-if="noticesNum > 0"
                    type="primary"
                    link
                    size="small"
                    @click="handleMarkAllAsRead"
                  >
                    全部已读
                  </el-button>
                </div>
                <el-scrollbar max-height="280px" v-loading="loading">
                  <div class="noticeList-container">
                    <NoticeList
                      :list="item.list || []"
                      :emptyText="item.emptyText"
                      @click="handleNoticeClick"
                    />
                  </div>
                </el-scrollbar>
              </el-tab-pane>
            </template>
          </span>
        </el-tabs>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<style lang="scss" scoped>
.dropdown-badge {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 48px;
  cursor: pointer;

  .header-notice-icon {
    font-size: 18px;
  }
}

.dropdown-tabs {
  .noticeList-container {
    padding: 0;
  }

  :deep(.el-tabs__header) {
    margin: 0;
  }

  :deep(.el-tabs__nav-wrap)::after {
    height: 1px;
  }

  :deep(.el-tabs__nav-wrap) {
    padding: 0 36px;
  }
}
</style>
