<script setup lang="ts">
import { ListItem } from "../data";
import { ref, nextTick } from "vue";
import { useNav } from "@/layout/hooks/useNav";
import { deviceDetection } from "@pureadmin/utils";

const props = defineProps<{
  noticeItem: ListItem;
}>();

const emit = defineEmits<{
  click: [item: ListItem];
}>();

const titleRef = ref<HTMLElement | null>(null);
const titleTooltip = ref(false);
const descriptionRef = ref<HTMLElement | null>(null);
const descriptionTooltip = ref(false);
const { tooltipEffect } = useNav();
const isMobile = deviceDetection();

function hoverTitle() {
  nextTick(() => {
    if (titleRef.value) {
      titleRef.value.scrollWidth > titleRef.value.clientWidth
        ? (titleTooltip.value = true)
        : (titleTooltip.value = false);
    }
  });
}

function hoverDescription(event: MouseEvent) {
  const tempTag = document.createElement("span");
  tempTag.textContent = props.noticeItem.description;
  tempTag.className = "getDescriptionWidth";
  document.querySelector("body")?.appendChild(tempTag);
  const currentWidth = (
    document.querySelector(".getDescriptionWidth") as HTMLElement
  )?.offsetWidth ?? 0;
  document.querySelector(".getDescriptionWidth")?.remove();

  const cellWidth = (event.target as HTMLElement)?.offsetWidth ?? 0;
  currentWidth > 2 * cellWidth
    ? (descriptionTooltip.value = true)
    : (descriptionTooltip.value = false);
}

function handleClick() {
  if (!props.noticeItem.read) {
    emit("click", props.noticeItem);
  }
}
</script>

<template>
  <div
    class="notice-container border-0 border-b-[1px] border-solid border-[#f0f0f0] dark:border-[#303030] cursor-pointer"
    :class="{ 'opacity-60': noticeItem.read }"
    @click="handleClick"
  >
    <el-avatar
      v-if="noticeItem.avatar"
      :size="30"
      :src="noticeItem.avatar"
      class="notice-container-avatar"
    />
    <div class="notice-container-text">
      <div class="notice-text-title text-[#000000d9] dark:text-white">
        <el-tooltip
          popper-class="notice-title-popper"
          :effect="tooltipEffect"
          :disabled="!titleTooltip"
          :content="noticeItem.title"
          placement="top-start"
          :enterable="!isMobile"
        >
          <div
            ref="titleRef"
            class="notice-title-content"
            @mouseover="hoverTitle"
          >
            {{ noticeItem.title }}
          </div>
        </el-tooltip>
        <el-tag
          v-if="noticeItem?.extra"
          :type="noticeItem?.status"
          size="small"
          class="notice-title-extra"
        >
          {{ noticeItem?.extra }}
        </el-tag>
      </div>

      <el-tooltip
        popper-class="notice-title-popper"
        :effect="tooltipEffect"
        :disabled="!descriptionTooltip"
        :content="noticeItem.description"
        placement="top-start"
      >
        <div
          ref="descriptionRef"
          class="notice-text-description"
          @mouseover="hoverDescription"
        >
          {{ noticeItem.description }}
        </div>
      </el-tooltip>
      <div class="notice-text-datetime text-[#00000073] dark:text-white">
        {{ noticeItem.datetime }}
      </div>
    </div>
  </div>
</template>

<style>
.notice-title-popper {
  max-width: 238px;
}
</style>
<style lang="scss" scoped>
.notice-container {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 12px 20px;

  // border-bottom: 1px solid #f0f0f0;

  .notice-container-avatar {
    margin-right: 16px;
    background: #fff;
  }

  .notice-container-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    justify-content: space-between;

    .notice-text-title {
      display: flex;
      margin-bottom: 8px;
      font-size: 14px;
      font-weight: 400;
      line-height: 1.5715;
      cursor: pointer;

      .notice-title-content {
        flex: 1;
        width: 200px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .notice-title-extra {
        float: right;
        margin-top: -1.5px;
        font-weight: 400;
      }
    }

    .notice-text-description,
    .notice-text-datetime {
      font-size: 12px;
      line-height: 1.5715;
    }

    .notice-text-description {
      display: -webkit-box;
      overflow: hidden;
      text-overflow: ellipsis;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
    }

    .notice-text-datetime {
      margin-top: 4px;
    }
  }
}
</style>
