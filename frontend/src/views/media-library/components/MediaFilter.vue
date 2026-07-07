<script setup lang="ts">
/**
 * MediaFilter 顶部过滤栏
 * 媒体库 v2 单表设计 — Phase 5
 */
import { ref, watch, onMounted } from "vue";
import { Search } from "@element-plus/icons-vue";
import { listAllMediaTags, type ListMediaParams } from "@/api/media";

const props = defineProps<{
  modelValue: ListMediaParams;
  total: number;
}>();

const emit = defineEmits<{
  "update:modelValue": [params: ListMediaParams];
  reset: [];
}>();

// 本地副本（避免直接改 prop）
const local = ref<ListMediaParams>({ ...props.modelValue });

watch(
  () => props.modelValue,
  v => {
    local.value = { ...v };
  },
  { deep: true }
);

function apply() {
  // 防御：空数组 tag_names 不发送（避免后端误解为"过滤空字符串"）
  const out: ListMediaParams = { ...local.value, page: 1 };
  if (Array.isArray(out.tag_names) && out.tag_names.length === 0) {
    delete out.tag_names;
  }
  emit("update:modelValue", out);
}

function reset() {
  local.value = {
    page: 1,
    page_size: props.modelValue.page_size ?? 20,
    status: "active" // 重置后默认 active
  };
  emit("reset");
}

// 多选 tag 过滤：tag_names 是数组（OR 关系）
//  - 监视 local.tag_names 变化，应用时直接发数组
//  - 当用户清空选择时，传 undefined（不限）

// 防抖搜索
let timer: number | undefined;
function onQInput() {
  if (timer) window.clearTimeout(timer);
  timer = window.setTimeout(() => {
    apply();
  }, 350);
}

// ===== Tag 过滤 =====
// 预制 tag 词典（与 MediaDetail / MediaGrid 保持一致，始终在下拉显示）
// 后端 listAllMediaTags 只返回 DB 中已用过的 tag —— 新加预制 tag 的话 DB 还没有数据
// 所以下拉要"预制 + DB 去重合并"，避免用户找不到 营业执照 等常用预制
const PRESET_TAGS: string[] = [
  "身份证", "营业执照", "人像", "签名照",
  "证件照", "合同页", "印章", "公章",
  "现场照片", "票据", "手写", "客户档案"
];
const availableTags = ref<string[]>([]);
async function loadTags() {
  try {
    const res = await listAllMediaTags();
    const dbTags = res.success ? (res.data.tags ?? []) : [];
    // 预制优先（按 PRESET_TAGS 顺序），再追加 DB 中"非预制"的手打 tag
    const seen = new Set<string>();
    const merged: string[] = [];
    for (const t of PRESET_TAGS) {
      if (!seen.has(t)) {
        seen.add(t);
        merged.push(t);
      }
    }
    for (const t of dbTags) {
      if (!seen.has(t)) {
        seen.add(t);
        merged.push(t);
      }
    }
    availableTags.value = merged;
  } catch {
    /* 静默失败：仅显示预制 tag */
    availableTags.value = [...PRESET_TAGS];
  }
}
onMounted(loadTags);
</script>

<template>
  <div class="media-filter">
    <!-- 类型 chips -->
    <div class="filter-group">
      <span class="filter-label">类型</span>
      <el-radio-group
        v-model="local.type"
        size="default"
        @change="apply"
      >
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="photo">照片</el-radio-button>
        <el-radio-button value="video">录像</el-radio-button>
        <el-radio-button value="audio">音频</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 来源 chips -->
    <div class="filter-group">
      <span class="filter-label">来源</span>
      <el-radio-group
        v-model="local.source"
        size="default"
        @change="apply"
      >
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="camera">摄像头</el-radio-button>
        <el-radio-button value="upload">上传</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 标签多选（预制 + 手打） -->
    <div class="filter-group">
      <span class="filter-label">标签</span>
      <el-select
        v-model="local.tag_names"
        multiple
        collapse-tags
        collapse-tags-tooltip
        :max-collapse-tags="3"
        placeholder="选择或输入标签名（多选，OR 关系）"
        clearable
        filterable
        allow-create
        default-first-option
        style="min-width: 280px; max-width: 480px"
        @change="apply"
      >
        <el-option
          v-for="t in availableTags"
          :key="t"
          :label="t"
          :value="t"
        />
      </el-select>
    </div>

    <!-- 状态 + 关键字 + 操作 -->
    <div class="filter-row">
      <el-select
        v-model="local.status"
        size="default"
        style="width: 140px"
        @change="apply"
      >
        <el-option label="活跃" value="active" />
        <el-option label="已删" value="deleted" />
        <el-option label="全部" value="all" />
      </el-select>

      <el-input
        v-model="local.q"
        placeholder="搜索文件名 / 原始名"
        clearable
        size="default"
        style="flex: 1; max-width: 360px"
        @input="onQInput"
        @clear="apply"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>

      <el-button @click="reset">重置</el-button>

      <div class="filter-spacer" />

      <span class="filter-total">
        共 <b>{{ total }}</b> 项
      </span>
    </div>
  </div>
</template>

<style scoped>
.media-filter {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 20px;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 12px;
  margin-bottom: 16px;
}

.filter-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.filter-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary, #606266);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  min-width: 32px;
}
.filter-hint {
  font-size: 11px;
  color: var(--el-text-color-placeholder, #a8abb2);
  margin-left: 4px;
}

.filter-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.filter-spacer {
  flex: 1;
}

.filter-total {
  font-size: 12px;
  color: var(--el-text-color-secondary, #606266);
  font-variant-numeric: tabular-nums;
}
.filter-total b {
  color: var(--el-color-primary, #409eff);
  font-weight: 700;
  font-size: 14px;
  margin: 0 2px;
}
</style>
