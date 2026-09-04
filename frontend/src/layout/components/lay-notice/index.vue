<script setup lang="ts">
/**
 * 顶部铃铛通知（lay-notice）。
 *
 * 重构于 2026-09-04：所有计数 / 拉取 / WS 订阅逻辑已抽离到 useMessageNotice composable。
 * 本组件只负责渲染：铃铛 + 角标 + 消息列表 tab + 已读 / 全部已读交互。
 */
import { computed } from "vue";
import { ElMessage } from "element-plus";
import NoticeList from "./components/NoticeList.vue";
import BellIcon from "~icons/ep/bell";
import { useMessageNotice } from "@/composables/useMessageNotice";
import { listItemFromMessage, type ListItem } from "./data";

const {
  noticesNum,
  messages,
  loading,
  refreshMessages,
  markRead,
  markAllRead
} = useMessageNotice();

// "消息" tab 列表（从 Message 适配到 ListItem）
const messageList = computed<ListItem[]>(() => messages.value.map(listItemFromMessage));

async function handleNoticeClick(item: ListItem) {
  if (!item.id || item.read) return;
  try {
    await markRead(item.id);
  } catch (e) {
    console.error("[lay-notice] markRead failed:", e);
    ElMessage.error("标记已读失败");
  }
}

async function handleMarkAllAsRead() {
  try {
    await markAllRead();
  } catch (e) {
    console.error("[lay-notice] markAllRead failed:", e);
    ElMessage.error("标记全部已读失败");
  }
}
</script>

<template>
  <el-dropdown
    trigger="click"
    placement="bottom-end"
    @visible-change="(visible: boolean) => visible && refreshMessages()"
  >
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
        <div class="w-[330px]">
          <div
            class="flex justify-between items-center px-5 py-2 border-b border-gray-200 dark:border-gray-700"
          >
            <span class="text-sm text-gray-500">共 {{ messageList.length }} 条消息</span>
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
                :list="messageList"
                emptyText="暂无消息"
                @click="handleNoticeClick"
              />
            </div>
          </el-scrollbar>
        </div>
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

.noticeList-container {
  padding: 0;
}
</style>