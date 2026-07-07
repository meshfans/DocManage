<script setup lang="ts">
import { ListItem } from "../data";
import NoticeItem from "./NoticeItem.vue";

const props = defineProps<{
  list: ListItem[];
  emptyText?: string;
}>();

const emit = defineEmits<{
  click: [item: ListItem];
}>();
</script>

<template>
  <div v-if="props.list.length">
    <NoticeItem
      v-for="(item, index) in props.list"
      :key="index"
      :noticeItem="item"
      @click="emit('click', item)"
    />
  </div>
  <el-empty v-else :description="props.emptyText || '暂无消息'" />
</template>
