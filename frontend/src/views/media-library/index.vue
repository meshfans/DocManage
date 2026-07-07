<script setup lang="ts">
/**
 * 媒体库主页（v2 单表 + 3 哈希）
 * - 集成：MediaFilter / MediaUploader / MediaGrid / MediaDetail / MediaLightbox
 * - 替换之前的 placeholder
 */
import { ref, onMounted, watch, computed } from "vue";
import { useRouter, useRoute } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Picture,
  Refresh,
  Close,
  Delete,
  CollectionTag,
  User
} from "@element-plus/icons-vue";
import {
  listMedia,
  bulkAddTag,
  bulkDelete,
  bulkSetCustomer,
  type Media,
  type ListMediaParams
} from "@/api/media";
import { getCustomerById } from "@/api/customer";
import MediaFilter from "./components/MediaFilter.vue";
import MediaUploader from "./components/MediaUploader.vue";
import MediaGrid from "./components/MediaGrid.vue";
import MediaDetail from "./components/MediaDetail.vue";
import MediaLightbox from "./components/MediaLightbox.vue";

defineOptions({ name: "MediaLibrary" });

const router = useRouter();
const route = useRoute();

// URL ?customer_id=N → 锁定到该客户的媒体视图
// 例：从客户档案页跳过来 → /media/library?customer_id=12&customer_name=xxx
const scopedCustomerID = computed(() => {
  const v = Number(route.query.customer_id);
  return Number.isFinite(v) && v > 0 ? v : 0;
});
const scopedCustomerName = computed(() => {
  return (route.query.customer_name as string) || "";
});
// 监听 route 变化，同步 customer_id 过滤
// P2 修复：正确处理清空场景（n=0 或 undefined 时也要更新 filter，避免 URL 移除参数后过滤不重置）
watch(
  () => route.query.customer_id,
  v => {
    const n = Number(v);
    filter.value.customer_id = (Number.isFinite(n) && n > 0) ? n : undefined;
  }
);

// 过滤 + 列表
const filter = ref<ListMediaParams>({
  page: 1,
  page_size: 20,
  status: "active", // 默认 active：只看未删；用户在过滤器选"全部"或"deleted"可切换
  customer_id: scopedCustomerID.value || undefined
});
const items = ref<Media[]>([]);
const total = ref(0);
const loading = ref(false);
// 客户信息缓存 { customerId: { name, type, ts } }
// P1 修复：添加 TTL 失效（5 分钟），避免客户改名/删除后显示陈旧数据
const CUSTOMER_CACHE_TTL = 5 * 60 * 1000;
const customerMap = ref<Record<number, { name: string; type: string; ts: number }>>({});

async function loadList() {
  loading.value = true;
  try {
    const res = await listMedia(filter.value);
    if (res.success) {
      items.value = res.data.list;
      total.value = res.data.total;
      // 收集所有出现的customer_id并查询客户名称
      await loadCustomerNames(res.data.list);
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  } finally {
    loading.value = false;
  }
}

// 查询并缓存客户名称
// P1 修复：
//   1) 添加 TTL 失效检查
//   2) 收集失败 ID 并在控制台告警（不再静默失败）
async function loadCustomerNames(mediaList: Media[]) {
  const now = Date.now();
  const ids = [...new Set(mediaList.map(m => m.customer_id).filter(id => id && id > 0))];
  if (ids.length === 0) return;

  // 失效检查：超过 TTL 的缓存项视为未缓存
  const uncachedIds = ids.filter(id => {
    const cached = customerMap.value[id];
    return !cached || (now - cached.ts) > CUSTOMER_CACHE_TTL;
  });
  if (uncachedIds.length === 0) return;

  // 批量查询未缓存的客户
  const failed: number[] = [];
  await Promise.all(uncachedIds.map(async (id) => {
    try {
      const res = await getCustomerById(id);
      if (res.success && res.data) {
        const c = res.data;
        customerMap.value[id] = {
          name: c.customer_type === "individual" ? c.real_name : c.company_name,
          type: c.customer_type,
          ts: Date.now()
        };
      } else {
        failed.push(id);
      }
    } catch {
      failed.push(id);
    }
  }));
  if (failed.length > 0) {
    console.warn(`[customerMap] ${failed.length} 个客户名查询失败:`, failed);
  }
}

// 失效缓存：上传/更新/删除后调用
function invalidateCustomerCache(customerIds: number[]) {
  for (const id of customerIds) {
    if (customerMap.value[id]) {
      delete customerMap.value[id];
    }
  }
}

onMounted(loadList);
watch(filter, loadList, { deep: true });

// 详情抽屉
const detailId = ref<number | null>(null);
function openDetail(m: Media) {
  detailId.value = m.id;
}

// 灯箱
const lightboxItem = ref<Media | null>(null);
function openLightbox(m: Media) {
  lightboxItem.value = m;
}

// 批量选择
const selectedIds = ref<number[]>([]);
function onSelectChange(ids: number[]) {
  selectedIds.value = ids;
}
function clearSelection() {
  selectedIds.value = [];
}

// 批量打 tag
//   颜色选择器已去掉：MediaGrid 卡片显示 tag 时只读 PRESET_TYPE_MAP[t.name]，
//   不读 t.color。所以颜色字段虽然存了，但显示时是按 name 查预制颜色。
//   去掉颜色选择器避免给用户"选了没用"的错觉。
const tagDialogOpen = ref(false);
const tagDialogName = ref("");
const tagDialogLoading = ref(false);

// 预制 tag 词典（与 MediaFilter / MediaDetail 保持一致）
const PRESET_TAGS: { name: string; type: string }[] = [
  { name: "身份证",      type: "primary" },
  { name: "营业执照",    type: "success" },
  { name: "人像",        type: "info"    },
  { name: "签名照",      type: "warning" },
  { name: "证件照",      type: "primary" },
  { name: "合同页",      type: "success" },
  { name: "印章",        type: "danger"  },
  { name: "公章",        type: "danger"  },
  { name: "现场照片",    type: "info"    },
  { name: "票据",        type: "warning" },
  { name: "手写",        type: "info"    },
  { name: "客户档案",    type: "primary" }
];
// 预制 tag → 默认颜色（自动跟着预制走）
const PRESET_COLOR_MAP: Record<string, string> = Object.fromEntries(
  PRESET_TAGS.map(t => [t.name, t.type])
);
// 颜色字典（el-tag type → 真实 CSS 色值，仅后端入库用）
//   用户批量对话框已不再让用户选色（见 submitBulkTag 注释），
//   所以 COLOR_SWATCH / COLOR_LABELS 暂保留供后端 / 未来扩展使用，不在 UI 渲染。
const COLOR_SWATCH: Record<string, string> = {
  primary: "#409eff",
  success: "#67c23a",
  warning: "#e6a23c",
  danger:  "#f56c6c",
  info:    "#909399"
};
const COLOR_LABELS: { value: string; label: string }[] = [
  { value: "primary", label: "primary 蓝" },
  { value: "success", label: "success 绿" },
  { value: "warning", label: "warning 黄" },
  { value: "danger",  label: "danger  红" },
  { value: "info",    label: "info  灰" }
];

function pickPresetTag(name: string) {
  tagDialogName.value = name;
}

function openBulkTagDialog() {
  if (selectedIds.value.length === 0) {
    ElMessage.warning("请先选择媒体");
    return;
  }
  tagDialogName.value = "";
  tagDialogOpen.value = true;
}
async function submitBulkTag() {
  const name = tagDialogName.value.trim();
  if (!name) {
    ElMessage.warning("请输入标签名");
    return;
  }
  tagDialogLoading.value = true;
  try {
    // 不再传 color：MediaGrid 卡片显示 tag 时只按 name 查 PRESET_TYPE_MAP，不读 color 字段
    const res = await bulkAddTag({
      ids: selectedIds.value,
      tag_name: name
    });
    if (res.success) {
      ElMessage.success(res.data.message ?? "已添加");
      tagDialogOpen.value = false;
      clearSelection();
      loadList();
    } else {
      ElMessage.error(res.message ?? "失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "失败");
  } finally {
    tagDialogLoading.value = false;
  }
}

// 批量删除
async function bulkDeleteSelected() {
  if (selectedIds.value.length === 0) return;
  try {
    await ElMessageBox.confirm(
      `确定将选中的 ${selectedIds.value.length} 项移到回收站？`,
      "批量软删除（可恢复）",
      {
        type: "warning",
        confirmButtonText: `移到回收站（${selectedIds.value.length}）`,
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );
  } catch {
    return;
  }
  try {
    const res = await bulkDelete(selectedIds.value);
    if (res.success) {
      ElMessage.success(res.data.message ?? "已删除");
      clearSelection();
      loadList();
    } else {
      ElMessage.error(res.message ?? "失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "失败");
  }
}

// 批量关联客户
const bulkCustomerInput = ref("");
async function bulkSetCustomerSubmit() {
  if (selectedIds.value.length === 0) return;
  const n = Number(bulkCustomerInput.value);
  if (!Number.isFinite(n) || n < 0) {
    ElMessage.warning("请输入合法的客户 ID（0 = 解除关联）");
    return;
  }

  // 解除关联（customer_id=0）不需要校验
  if (n === 0) {
    try {
      await ElMessageBox.confirm(
        `确定解除选中的 ${selectedIds.value.length} 项媒体的客户关联？`,
        "解除关联",
        {
          type: "warning",
          confirmButtonText: "解除",
          cancelButtonText: "取消",
          customClass: "media-confirm-msg"
        }
      );
      const res = await bulkSetCustomer({
        ids: selectedIds.value,
        customer_id: 0
      });
      if (res.success) {
        ElMessage.success(res.data.message ?? "已解除关联");
        bulkCustomerInput.value = "";
        clearSelection();
        loadList();
      } else {
        ElMessage.error(res.message ?? "失败");
      }
    } catch {
      // 用户取消
    }
    return;
  }

  // 关联客户：先校验客户ID是否存在
  try {
    const custRes = await getCustomerById(n);
    if (!custRes.success || !custRes.data) {
      ElMessage.warning(`客户 #${n} 不存在，请检查客户 ID`);
      return;
    }
    const cust = custRes.data;
    // 个人客户用 real_name，企业客户用 company_name
    const custName = cust.customer_type === "individual"
      ? cust.real_name
      : cust.company_name;

    // 弹出确认框
    await ElMessageBox.confirm(
      `确定将选中的 ${selectedIds.value.length} 项媒体关联到「${custName}」（客户 #${n}）？`,
      "关联客户",
      {
        type: "info",
        confirmButtonText: "确认关联",
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );

    // 用户确认后执行关联
    const res = await bulkSetCustomer({
      ids: selectedIds.value,
      customer_id: n
    });
    if (res.success) {
      ElMessage.success(res.data.message ?? "已关联");
      bulkCustomerInput.value = "";
      clearSelection();
      loadList();
    } else {
      ElMessage.error(res.message ?? "失败");
    }
  } catch (e: any) {
    // 用户取消或API错误
    if (e !== "cancel" && e !== "close") {
      ElMessage.error(e?.response?.data?.message || e?.message || e || "失败");
    }
  }
}

// 操作回调
function onUploaded() {
  loadList();
}
function onDeleted() {
  detailId.value = null;
  loadList();
}
function onFilterReset() {
  filter.value = { page: 1, page_size: filter.value.page_size ?? 20 };
}

// 清除客户过滤（从 URL 移除 ?customer_id=）
function clearCustomerFilter() {
  router.push({ name: "MediaLibrary", query: {} });
  filter.value.customer_id = undefined;
}

const stats = ref({ photo: 0, video: 0, audio: 0, total: 0 });
async function loadStats() {
  // Bug #6 修复：媒体库顶栏的"照片/录像/音频/总"统计需要随 customer_id 过滤。
  //   - /media/library          → 全局统计（与 URL ?customer_id= 无关）
  //   - /media/library?customer_id=17 → 仅统计该客户的媒体（4 个数字都收窄）
  // 视频统计：之前 `page_size: 1` 是为了避免大列表传输，但 total 是 count(*) 不受影响。
  // 现状 status 默认 "active"（继承自 filter.status），与主页列表保持一致；
  // 录像统计从 bug 报告里观察到 "为 0" 是因为根本没传 customer_id，与底层录像缺失无关。
  const customerScope = scopedCustomerID.value || 0;
  const baseParams: ListMediaParams = { status: filter.value.status };
  if (customerScope > 0) baseParams.customer_id = customerScope;
  const [p, v, a] = await Promise.all([
    listMedia({ ...baseParams, type: "photo", page_size: 1 }),
    listMedia({ ...baseParams, type: "video", page_size: 1 }),
    listMedia({ ...baseParams, type: "audio", page_size: 1 })
  ]);
  stats.value = {
    photo: p.data?.total ?? 0,
    video: v.data?.total ?? 0,
    audio: a.data?.total ?? 0,
    total: (p.data?.total ?? 0) + (v.data?.total ?? 0) + (a.data?.total ?? 0)
  };
}
// 监听 customer_id / status 变化，重新拉统计
watch(
  [() => scopedCustomerID.value, () => filter.value.status],
  () => { loadStats(); }
);
onMounted(loadStats);
</script>

<template>
  <div class="media-library">
    <!-- 顶栏 -->
    <header class="library-topbar">
      <div class="topbar-left">
        <div class="brand-mark">
          <el-icon :size="18"><Picture /></el-icon>
        </div>
        <div class="brand-text">
          <h1 class="brand-title">媒体库</h1>
          <p class="brand-sub">Media Library</p>
        </div>
        <span class="env-pill" data-tone="info">
          <span class="env-pill-dot" />
          3 哈希校验
        </span>
      </div>

      <div class="topbar-stats">
        <div class="stat">
          <span class="stat-num">{{ stats.photo }}</span>
          <span class="stat-label">照片</span>
        </div>
        <div class="stat">
          <span class="stat-num">{{ stats.video }}</span>
          <span class="stat-label">录像</span>
        </div>
        <div class="stat">
          <span class="stat-num">{{ stats.audio }}</span>
          <span class="stat-label">音频</span>
        </div>
        <div class="stat-divider" />
        <div class="stat stat--total">
          <span class="stat-num">{{ stats.total }}</span>
          <span class="stat-label">总</span>
        </div>
      </div>

      <div class="topbar-actions">
        <el-button :icon="Refresh" @click="loadList">刷新</el-button>
      </div>
    </header>

    <!-- 客户档案过滤 banner（仅 URL ?customer_id=X 时显示） -->
    <div v-if="scopedCustomerID > 0" class="customer-banner">
      <el-icon :size="16" color="#409eff"><User /></el-icon>
      <span class="banner-text">
        正在查看客户 <b>#{{ scopedCustomerID }}</b>
        <template v-if="scopedCustomerName">
          <span class="banner-customer-name">「{{ scopedCustomerName }}」</span>
        </template>
        的媒体
      </span>
      <el-button
        link
        type="primary"
        @click="clearCustomerFilter"
      >清除过滤</el-button>
    </div>

    <!-- 批量操作工具栏（仅选中时显示，sticky 在页面顶部） -->
    <transition name="bulkbar-fade">
      <div v-if="selectedIds.length > 0" class="bulk-toolbar">
        <span class="bulk-count">
          已选 <b>{{ selectedIds.length }}</b> 项
        </span>
        <el-button
          type="primary"
          :icon="CollectionTag"
          @click="openBulkTagDialog"
        >批量打标签</el-button>
        <el-input
          v-model="bulkCustomerInput"
          placeholder="客户 ID（关联）"
          style="width: 160px"
          @keyup.enter="bulkSetCustomerSubmit"
        />
        <el-button @click="bulkSetCustomerSubmit">关联客户</el-button>
        <el-button
          type="danger"
          :icon="Delete"
          @click="bulkDeleteSelected"
        >批量移到回收站</el-button>
        <div class="bulk-spacer" />
        <el-button :icon="Close" link @click="clearSelection">清空选择</el-button>
      </div>
    </transition>

    <!-- 过滤 -->
    <MediaFilter v-model="filter" :total="total" @reset="onFilterReset" />

    <!-- 上传 -->
    <!-- Bug #5 修复：传入 scopedCustomerID（来自 URL ?customer_id=），
         MediaUploader 会把 customer_id append 到 multipart。
         上传完成后立即能在当前 ?customer_id= 过滤视图下看到。 -->
    <MediaUploader
      :customer-id="scopedCustomerID"
      @uploaded="onUploaded"
    />

    <!-- 网格 -->
    <MediaGrid
      :items="items"
      :loading="loading"
      :selected-ids="selectedIds"
      :customer-map="customerMap"
      @preview="openLightbox"
      @detail="openDetail"
      @deleted="onDeleted"
      @select-change="onSelectChange"
    />

    <!-- 分页 -->
    <el-pagination
      v-if="total > 0"
      v-model:current-page="filter.page"
      v-model:page-size="filter.page_size"
      :total="total"
      :page-sizes="[20, 50, 100]"
      layout="prev, pager, next, sizes, total"
      class="library-pagination"
      @current-change="loadList"
      @size-change="loadList"
    />

    <!-- 详情抽屉 -->
    <!--
      :key="detailId" 关键：每次开 detail 都强制重新挂载，保证标签 / 哈希 / 缩略图
      都是从后端拉取的最新值（避免父组件 items 缓存或组件复用导致数据陈旧）
    -->
    <MediaDetail
      :key="detailId ?? 'closed'"
      :media-id="detailId"
      @close="detailId = null"
      @updated="loadList"
      @deleted="onDeleted"
    />

    <!-- 批量打标签 dialog -->
    <el-dialog
      v-model="tagDialogOpen"
      title="批量打标签"
      width="520px"
      :close-on-click-modal="false"
    >
      <p class="dialog-tip">
        将为 <b>{{ selectedIds.length }}</b> 项媒体添加 tag（已存在则跳过）。
      </p>
      <el-form label-position="top">
        <!-- 预制 tag 一键选择 -->
        <el-form-item label="预制标签（一键选择）">
          <div class="preset-chips">
            <el-tag
              v-for="t in PRESET_TAGS"
              :key="t.name"
              :type="(t.type as any)"
              effect="plain"
              class="preset-chip"
              :class="{ 'is-active': tagDialogName === t.name }"
              @click="pickPresetTag(t.name)"
            >
              {{ t.name }}
            </el-tag>
          </div>
        </el-form-item>
        <!-- 标签名（可手写） -->
        <el-form-item label="标签名（可手写）">
          <el-input
            v-model="tagDialogName"
            placeholder="例如：客户档案 / 现场照片 / 自定义..."
            clearable
            @keyup.enter="submitBulkTag"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tagDialogOpen = false">取消</el-button>
        <el-button
          type="primary"
          :loading="tagDialogLoading"
          @click="submitBulkTag"
        >添加</el-button>
      </template>
    </el-dialog>

    <!-- 灯箱 -->
    <MediaLightbox
      :items="items"
      :current="lightboxItem"
      @close="lightboxItem = null"
      @changed="m => (lightboxItem = m)"
    />
  </div>
</template>

<style scoped>
.media-library {
  padding: 24px 28px 48px;
  min-height: 100%;
  background: var(--el-bg-color-page, #f7f8fa);
  color: var(--el-text-color-primary, #1f2329);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
}

/* 顶栏 */
.library-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.topbar-left {
  display: flex;
  align-items: center;
  gap: 14px;
}
.brand-mark {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2937 0%, #111827 100%);
  color: #fff;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04), 0 1px 3px rgba(15, 23, 42, 0.06);
}
.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.brand-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -0.2px;
}
.brand-sub {
  margin: 2px 0 0;
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--el-text-color-placeholder, #a8abb2);
}

.env-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  background: var(--el-fill-color-light, #f5f7fa);
  color: var(--el-text-color-secondary, #606266);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  margin-left: 6px;
}
.env-pill-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.7;
}
.env-pill[data-tone="info"] {
  color: #2563eb;
  background: rgba(37, 99, 235, 0.08);
  border-color: rgba(37, 99, 235, 0.2);
}

.topbar-stats {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 999px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}
.stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 36px;
}
.stat-num {
  font-size: 18px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-primary, #1f2329);
  line-height: 1.1;
}
.stat-label {
  font-size: 10px;
  color: var(--el-text-color-secondary, #606266);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-top: 2px;
}
.stat--total .stat-num {
  color: var(--el-color-primary, #409eff);
}
.stat-divider {
  width: 1px;
  height: 24px;
  background: var(--el-border-color-lighter, #ebeef5);
}

.topbar-actions {
  display: inline-flex;
  gap: 8px;
}

/* 分页 */
.library-pagination {
  display: flex;
  justify-content: center;
  margin-top: 20px;
  padding: 12px 0;
}

/* ===== 客户档案过滤 banner ===== */
.customer-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  margin-bottom: 12px;
  background: linear-gradient(90deg, rgba(64, 158, 255, 0.08), rgba(64, 158, 255, 0.02));
  border: 1px solid rgba(64, 158, 255, 0.2);
  border-radius: 10px;
  color: var(--el-text-color-primary, #1f2329);
  font-size: 13px;
}
.customer-banner .banner-text {
  flex: 1;
  font-weight: 500;
}
.customer-banner .banner-text b {
  color: var(--el-color-primary, #409eff);
  font-weight: 700;
  margin: 0 2px;
}

/* ===== 批量操作工具栏 ===== */
.bulk-toolbar {
  position: sticky;
  top: 8px;
  z-index: 50;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  margin-bottom: 12px;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color-lighter, #dcdfe6);
  border-radius: 10px;
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.08);
  flex-wrap: wrap;
}
.bulk-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary, #1f2329);
}
.bulk-count b {
  color: var(--el-color-primary, #409eff);
  font-weight: 700;
  margin: 0 2px;
  font-variant-numeric: tabular-nums;
}
.bulk-spacer {
  flex: 1;
}

.bulkbar-fade-enter-active,
.bulkbar-fade-leave-active {
  transition: opacity 200ms ease, transform 200ms ease;
}
.bulkbar-fade-enter-from,
.bulkbar-fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

/* 批量 tag dialog */
.dialog-tip {
  margin: 0 0 16px;
  padding: 8px 12px;
  background: var(--el-fill-color-light, #f5f7fa);
  border-radius: 6px;
  font-size: 13px;
  color: var(--el-text-color-regular, #606266);
}
.dialog-tip b {
  color: var(--el-color-primary, #409eff);
  font-weight: 700;
  margin: 0 2px;
}

/* 预制 tag chips */
.preset-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.preset-chip {
  cursor: pointer;
  user-select: none;
  transition: transform 120ms ease, box-shadow 120ms ease;
}
.preset-chip:hover {
  transform: translateY(-1px);
  box-shadow: 0 2px 6px rgba(15, 23, 42, 0.1);
}
.preset-chip.is-active {
  outline: 2px solid var(--el-color-primary, #409eff);
  outline-offset: 1px;
}

/* 旧的 color-radios / color-swatch 样式已无用（颜色选择器已删），
   COLOR_SWATCH / COLOR_LABELS 暂保留供后端 / 未来扩展使用 */

/* 暗色模式 */
@media (prefers-color-scheme: dark) {
  .media-library {
    --el-bg-color-page: #0b0d10;
    --el-bg-color: #15181d;
    --el-fill-color-light: #1c2026;
    --el-border-color-lighter: #2a2f37;
    --el-text-color-primary: #e5e7eb;
    --el-text-color-secondary: #9ca3af;
    --el-text-color-placeholder: #6b7280;
  }
  .topbar-stats, .brand-mark {
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
  }
}
</style>
