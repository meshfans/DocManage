<script setup lang="ts">
/**
 * MediaGrid 主网格（照片 + 录像卡）
 * - 卡片：缩略图 / 类型徽章 / 3 哈希徽章 / 时间 / 操作按钮
 * - hover 浮起 + 3 按钮（预览 / 下载 / 删除）
 */
import { onMounted, onBeforeUnmount, ref, watch, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Picture,
  VideoPlay,
  Film,
  Download,
  View,
  CircleClose,
  RefreshRight,
  Loading
} from "@element-plus/icons-vue";
import type { Media, MediaBlobUrl } from "@/api/media";
import { deleteMedia, restoreMedia, fetchMediaFile, fetchMediaThumb } from "@/api/media";

const props = defineProps<{
  items: Media[];
  loading?: boolean;
  selectedIds?: number[];
  customerMap?: Record<number, { name: string; type: string }>;
}>();

// 获取客户显示名称
function getCustomerDisplay(customerId: number): string {
  if (!customerId || customerId <= 0) return "";
  const info = props.customerMap?.[customerId];
  if (!info) return `客户 #${customerId}`; // 未查到时显示ID
  return info.name;
}

const emit = defineEmits<{
  preview: [item: Media];
  detail: [item: Media];
  deleted: [];
  "select-change": [ids: number[]]; // 父组件传 selectedIds[] 来渲染选中态
}>();

// 当前是否处于"可选择"模式（只要有传入 selectedIds 就算）
const selectable = computed(() => Array.isArray(props.selectedIds));

function isSelected(m: Media): boolean {
  return selectable.value && (props.selectedIds ?? []).includes(m.id);
}

// 切换选中态（@change 传的是新 value（bool），不是 Event 对象）
// 父 <label @click.stop> 已阻止冒泡到卡片，无需手动 stopPropagation
function toggleSelect(m: Media, val: boolean) {
  if (!selectable.value) return;
  const cur = new Set(props.selectedIds ?? []);
  if (val) cur.add(m.id);
  else cur.delete(m.id);
  emit("select-change", Array.from(cur));
}

// 缩略图 URL 缓存（key=snowid）+ 对应 revoke 句柄
const thumbCache = ref<Record<string, string>>({});
const thumbRevoke = ref<Record<string, () => void>>({});

// 预制 tag → el-tag type 映射（与 MediaDetail 保持一致，让卡片展示与详情颜色统一）
const PRESET_TYPE_MAP: Record<string, string> = {
  身份证: "primary", 营业执照: "success", 人像: "info", 签名照: "warning",
  证件照: "primary", 合同页: "success", 印章: "danger", 公章: "danger",
  现场照片: "info", 票据: "warning", 手写: "info", 客户档案: "primary"
};

async function loadThumbs() {
  for (const m of props.items) {
    if (thumbCache.value[m.snowid]) continue;
    if (m.type !== "photo" && m.type !== "video") continue;
    try {
      let blobUrl: MediaBlobUrl | null = null;
      if (m.thumb_path) {
        // 优先用后端生成的缩略图（照片原图 / 视频首帧）
        blobUrl = await fetchMediaThumb(m.id);
      } else if (m.type === "photo") {
        // 无 thumb_path 时回退到原图；不传 download:true，后端不计入 download_count
        blobUrl = await fetchMediaFile(m.id);
      }
      if (blobUrl) {
        thumbCache.value = { ...thumbCache.value, [m.snowid]: blobUrl.url };
        thumbRevoke.value = { ...thumbRevoke.value, [m.snowid]: blobUrl.revoke };
      }
    } catch {
      // 静默失败
    }
  }
}

// 卸载时释放所有缩略图 ObjectURL（防止内存泄漏）
onBeforeUnmount(() => {
  for (const key of Object.keys(thumbRevoke.value)) {
    try { thumbRevoke.value[key](); } catch { /* ignore */ }
  }
  thumbCache.value = {};
  thumbRevoke.value = {};
});

onMounted(loadThumbs);
watch(() => props.items, loadThumbs, { deep: false });

function isImage(m: Media): boolean {
  return m.type === "photo";
}
function isVideo(m: Media): boolean {
  return m.type === "video";
}

function formatTime(ts: number): string {
  const d = new Date(ts * 1000);
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) {
    return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  }
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatDuration(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

async function onDownload(m: Media, e: Event) {
  e.stopPropagation();
  try {
    // fetchMediaFile 返回 MediaBlobUrl { url, revoke }
    const blobUrl = await fetchMediaFile(m.id, { download: true });
    const a = document.createElement("a");
    a.href = blobUrl.url;
    const ext = m.file_path.split(".").pop() ?? "bin";
    a.download = `${m.snowid}.${ext}`;
    a.click();
    // 延迟 revoke，避免某些浏览器下载延迟导致文件丢失
    setTimeout(() => blobUrl.revoke(), 5000);
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "下载失败");
  }
}

async function onDelete(m: Media, e: Event) {
  e.stopPropagation();
  try {
    await ElMessageBox.confirm(
      `确定删除「${m.name}」？此操作可恢复。`,
      "软删除",
      {
        type: "warning",
        confirmButtonText: "删除",
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );
    const res = await deleteMedia(m.id);
    if (res.success) {
      ElMessage.success("已删除");
      emit("deleted");
    }
  } catch {
    /* 用户取消 */
  }
}

// Bug #6 修复：列表项 status === "deleted" 时，把"删除"按钮换成"恢复"按钮。
// 走同一份 fetchMediaFile / fetchMediaThumb：后端已改用 GetMediaByIDIncludeDeleted，
// 已删项也能正常加载缩略图 / 原图（虽然列表里默认只显示"已删"过滤器下的项，
// 但 thumb_cache 已存在复用机制，恢复后无需重新拉图）。
async function onRestore(m: Media, e: Event) {
  e.stopPropagation();
  try {
    await ElMessageBox.confirm(
      `确定恢复「${m.name}」？恢复后会重新出现在默认列表。`,
      "恢复",
      {
        type: "info",
        confirmButtonText: "恢复",
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );
    const res = await restoreMedia(m.id);
    if (res.success) {
      ElMessage.success("已恢复");
      emit("deleted"); // 复用同一事件，父组件统一 reloadList
    }
  } catch {
    /* 用户取消 */
  }
}
</script>

<template>
  <div class="media-grid-wrapper">
    <div v-if="loading" class="grid-state">
      <el-icon :size="24"><Loading /></el-icon>
      加载中…
    </div>

    <div v-else-if="items.length === 0" class="grid-state">
      <el-icon :size="48" color="#d1d5db"><Film /></el-icon>
      <p>暂无媒体</p>
    </div>

    <div v-else class="media-grid">
      <article
        v-for="m in items"
        :key="m.id"
        class="media-card"
        :data-type="m.type"
        :class="{
          'is-selected': isSelected(m),
          'is-selectable': selectable,
          'is-deleted': m.status === 'deleted'
        }"
        @click="emit('detail', m)"
      >
        <!-- 缩略图区 -->
        <div class="card-thumb">
          <img
            v-if="(isImage(m) || isVideo(m)) && thumbCache[m.snowid]"
            :src="thumbCache[m.snowid]"
            :alt="m.name"
            class="thumb-img"
          />
          <video
            v-else-if="isVideo(m)"
            :src="m.file_path"
            class="thumb-img thumb-video"
            preload="metadata"
            muted
          />
          <div v-else-if="m.type === 'audio'" class="thumb-audio">
            <el-icon :size="40"><Film /></el-icon>
          </div>
          <div v-else class="thumb-placeholder">
            <el-icon :size="32" color="#9ca3af"><Picture /></el-icon>
          </div>

          <div class="card-type-badge" :data-type="m.type">
            <el-icon :size="11">
              <Picture v-if="m.type === 'photo'" />
              <VideoPlay v-else-if="m.type === 'video'" />
              <Film v-else />
            </el-icon>
            {{ m.type === "photo" ? "照片" : m.type === "video" ? "录像" : "音频" }}
          </div>
          <!-- Bug #6 修复：已删项显式标识，避免误以为是空状态 -->
          <span v-if="m.status === 'deleted'" class="card-deleted-badge">已删</span>
          <span v-if="m.type === 'video' && m.duration" class="card-duration">
            {{ formatDuration(m.duration) }}
          </span>
        </div>

        <!-- 主体信息 -->
        <div class="card-body">
          <!-- 名称行：可选择时 checkbox 放名称左边，点击名称 = 切换选择 -->
          <div class="card-name-row">
            <el-checkbox
              v-if="selectable"
              class="card-name-check"
              :model-value="isSelected(m)"
              @change="(val) => toggleSelect(m, val as boolean)"
              @click.stop
            />
            <h4
              class="card-name"
              :class="{ 'card-name-clickable': selectable }"
              :title="m.name"
              @click.stop="selectable && toggleSelect(m, !isSelected(m))"
            >{{ m.name }}</h4>
          </div>
          <!-- 关联客户 -->
          <div v-if="m.customer_id && m.customer_id > 0" class="card-customer">
            <span class="customer-label">客户：</span>
            <span class="customer-name" :title="`客户 #${m.customer_id}`">{{ getCustomerDisplay(m.customer_id) }}</span>
          </div>
          <div class="card-meta">
            <span class="card-time">{{ formatTime(m.taken_at) }}</span>
            <span class="card-size">{{ formatSize(m.file_size) }}</span>
          </div>
          <div class="card-hashes">
            <span v-if="m.hash_sm3" class="hash-chip" title="SM3 校验通过">SM3 ✓</span>
            <span v-if="m.hash_sha256" class="hash-chip" title="SHA-256 校验通过">SHA256 ✓</span>
            <span v-if="m.hash_combined" class="hash-chip" title="Combined 校验通过">Combined ✓</span>
          </div>
          <!-- 标签（最多显示 3 个；更多用 +N 提示） -->
          <div v-if="(m.tags ?? []).length > 0" class="card-tags">
            <el-tag
              v-for="t in (m.tags ?? []).slice(0, 3)"
              :key="t.name"
              size="small"
              effect="plain"
              :type="(PRESET_TYPE_MAP[t.name] as any) || 'info'"
            >{{ t.name }}</el-tag>
            <span v-if="(m.tags ?? []).length > 3" class="card-tags-more">
              +{{ (m.tags ?? []).length - 3 }}
            </span>
          </div>
        </div>

        <!-- hover 操作 -->
        <div class="card-actions">
          <button
            v-if="isImage(m) || isVideo(m)"
            class="action-btn"
            title="预览"
            @click.stop="emit('preview', m)"
          >
            <el-icon><View /></el-icon>
          </button>
          <button class="action-btn" title="下载" @click.stop="onDownload(m, $event)">
            <el-icon><Download /></el-icon>
          </button>
          <!-- Bug #6 修复：已删项显示"恢复"按钮；其余显示"删除"按钮 -->
          <button
            v-if="m.status === 'deleted'"
            class="action-btn action-btn--restore"
            title="恢复"
            @click.stop="onRestore(m, $event)"
          >
            <el-icon><RefreshRight /></el-icon>
          </button>
          <button
            v-else
            class="action-btn action-btn--danger"
            title="删除"
            @click.stop="onDelete(m, $event)"
          >
            <el-icon><CircleClose /></el-icon>
          </button>
        </div>
      </article>
    </div>
  </div>
</template>

<style scoped>
.media-grid-wrapper {
  min-height: 200px;
}

.grid-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 60px 20px;
  color: var(--el-text-color-secondary, #606266);
  font-size: 13px;
}

.media-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 14px;
}

.media-card {
  position: relative;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 10px;
  overflow: hidden;
  cursor: pointer;
  transition: border-color 200ms ease, transform 200ms ease, box-shadow 200ms ease;
}
.media-card:hover {
  border-color: var(--el-color-primary-light-5, #c6e2ff);
  transform: translateY(-2px);
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.08);
}
.media-card[data-type="video"] { border-left: 3px solid #ef4444; }
.media-card[data-type="photo"] { border-left: 3px solid #409eff; }
.media-card[data-type="audio"] { border-left: 3px solid #909399; }
.media-card.is-selected {
  border-color: var(--el-color-primary, #409eff);
  box-shadow: 0 0 0 2px rgba(64, 158, 255, 0.25);
}

/* 旧的 absolute 定位 checkbox 样式已废弃，名称行 checkbox 改用 .card-name-check */

.card-thumb {
  position: relative;
  aspect-ratio: 16 / 9;
  background: #0a0a0a;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.thumb-video {
  background: #000;
}
.thumb-audio {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #4b5563, #1f2937);
  color: #fff;
}
.thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #9ca3af;
}

.card-type-badge {
  position: absolute;
  top: 8px;
  left: 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  border-radius: 999px;
  color: #fff;
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.card-type-badge[data-type="photo"] { background: rgba(64, 158, 255, 0.85); }
.card-type-badge[data-type="video"] { background: rgba(239, 68, 68, 0.85); }
.card-type-badge[data-type="audio"] { background: rgba(96, 105, 122, 0.85); }

.card-duration {
  position: absolute;
  bottom: 8px;
  right: 8px;
  padding: 2px 6px;
  font-size: 11px;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  color: #fff;
  background: rgba(0, 0, 0, 0.7);
  border-radius: 4px;
}

.card-body {
  padding: 10px 12px 12px;
}
.card-name-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}
.card-name-check {
  flex-shrink: 0;
  margin-right: 0;
}
.card-name {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary, #1f2329);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
  min-width: 0;
}
.card-name-clickable {
  cursor: pointer;
  user-select: none;
  transition: color 140ms ease;
}
.card-name-clickable:hover {
  color: var(--el-color-primary, #409eff);
}
.card-customer {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--el-text-color-secondary, #909399);
  margin-bottom: 4px;
}
.customer-label {
  font-weight: 500;
  flex-shrink: 0;
}
.customer-name {
  color: var(--el-color-primary, #409eff);
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}
.card-meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--el-text-color-secondary, #606266);
  font-variant-numeric: tabular-nums;
  margin-bottom: 6px;
}
.card-hashes {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.hash-chip {
  padding: 1px 6px;
  font-size: 10px;
  font-weight: 600;
  color: #16a34a;
  background: rgba(22, 163, 74, 0.1);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

.card-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
  align-items: center;
}
.card-tags-more {
  font-size: 11px;
  color: var(--el-text-color-secondary, #909399);
  font-weight: 600;
  padding: 0 2px;
}

.card-actions {
  position: absolute;
  top: 8px;
  right: 8px;
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 180ms ease;
}
.media-card:hover .card-actions { opacity: 1; }
.action-btn {
  appearance: none;
  border: 0;
  cursor: pointer;
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.92);
  color: #1f2937;
  border-radius: 6px;
  font-size: 13px;
  transition: background 140ms ease, transform 140ms ease;
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.action-btn:hover {
  background: #fff;
  transform: scale(1.05);
}
.action-btn--danger {
  background: rgba(239, 68, 68, 0.92);
  color: #fff;
}
.action-btn--danger:hover {
  background: rgba(239, 68, 68, 1);
}
/* Bug #6 修复：恢复按钮（绿色系，与删除区分） */
.action-btn--restore {
  background: rgba(34, 197, 94, 0.92);
  color: #fff;
}
.action-btn--restore:hover {
  background: rgba(34, 197, 94, 1);
}
/* Bug #6 修复：已删卡片视觉降级 + 灰蒙版 */
.media-card.is-deleted {
  opacity: 0.85;
  filter: grayscale(0.35);
  border-color: rgba(239, 68, 68, 0.3);
}
.media-card.is-deleted:hover {
  filter: grayscale(0.15);
}
/* Bug #6 二次修复：把"已删"徽章挪到 bottom-left。
 * 原来在 top-right 与 hover 才显示的 .card-actions（恢复/下载）冲突。
 * bottom-right 是视频 .card-duration 占位，bottom-left 是唯一空位。
 * z-index 1 让它落在缩略图之上、但低于悬浮 action（.card-actions z 是 2）。 */
.card-deleted-badge {
  position: absolute;
  bottom: 8px;
  left: 8px;
  padding: 2px 8px;
  border-radius: 4px;
  background: rgba(239, 68, 68, 0.92);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.18);
  z-index: 1;
}
</style>
