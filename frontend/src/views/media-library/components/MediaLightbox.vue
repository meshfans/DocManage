<script setup lang="ts">
/**
 * MediaLightbox 全屏预览（与摄像头页 lightbox 同款，可独立复用）
 * - 点缩略图 → 全屏，Esc/←/→ 导航
 */
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { ArrowLeft, ArrowRight, CircleClose, Download } from "@element-plus/icons-vue";
import type { Media, MediaBlobUrl } from "@/api/media";
import { fetchMediaFile } from "@/api/media";

const props = defineProps<{
  items: Media[];
  current: Media | null;
}>();

const emit = defineEmits<{
  close: [];
  changed: [item: Media];
}>();

const currentIndex = computed(() => {
  if (!props.current) return -1;
  return props.items.findIndex(m => m.id === props.current!.id);
});

// URL 缓存：id → { url, revoke }
// 注意：组件卸载时统一 revoke，否则会泄漏
const urlCache = new Map<number, MediaBlobUrl>();

async function getUrl(m: Media): Promise<string> {
  if (urlCache.has(m.id)) return urlCache.get(m.id)!.url;
  const blobUrl = await fetchMediaFile(m.id);
  urlCache.set(m.id, blobUrl);
  return blobUrl.url;
}

const photoUrl = ref<string>("");
watch(
  () => props.current,
  async cur => {
    if (!cur) {
      photoUrl.value = "";
      return;
    }
    try {
      photoUrl.value = await getUrl(cur);
    } catch {
      photoUrl.value = "";
    }
  },
  { immediate: true }
);

function nav(delta: number) {
  if (currentIndex.value < 0) return;
  const items = props.items;
  const next = ((currentIndex.value + delta) % items.length + items.length) % items.length;
  const newItem = items[next];
  if (newItem) emit("changed", newItem);
}

function close() {
  emit("close");
}

/**
 * 把 mime_type 缩为短标签显示用。
 *   - "image/png"   → "PNG"
 *   - "image/jpeg"  → "JPEG"
 *   - "video/mp4"   → "MP4"
 *   - "video/webm"  → "WEBM"
 *   - "audio/mpeg"  → "MP3"
 *   - 其他          → 原值去 "image/" 前缀
 *
 * P0 修复（2026-06-14）：修复硬编码 "JPEG" 显示 bug。
 */
function formatMimeShort(mime?: string | null): string {
  if (!mime) return "—";
  const m = mime.toLowerCase();
  if (m === "image/png") return "PNG";
  if (m === "image/jpeg" || m === "image/jpg") return "JPEG";
  if (m === "image/gif") return "GIF";
  if (m === "image/webp") return "WEBP";
  if (m === "video/mp4") return "MP4";
  if (m === "video/webm") return "WEBM";
  if (m === "audio/mpeg" || m === "audio/mp3") return "MP3";
  if (m === "audio/wav") return "WAV";
  return mime.replace(/^[a-z]+\//, "");
}

async function download() {
  if (!props.current) return;
  try {
    // fetchMediaFile 现在返回 MediaBlobUrl（包含 revoke 句柄）
    const blobUrl = await fetchMediaFile(props.current.id, { download: true });
    const a = document.createElement("a");
    a.href = blobUrl.url;
    const ext = props.current.file_path.split(".").pop() ?? "bin";
    a.download = `${props.current.snowid}.${ext}`;
    a.click();
    // 延迟 revoke，避免某些浏览器下载延迟导致文件丢失
    setTimeout(() => blobUrl.revoke(), 5000);
  } catch (err) {
    console.error("下载失败", err);
  }
}

function onKey(e: KeyboardEvent) {
  if (!props.current) return;
  if (e.key === "Escape") {
    e.preventDefault();
    close();
  } else if (e.key === "ArrowLeft") {
    e.preventDefault();
    nav(-1);
  } else if (e.key === "ArrowRight") {
    e.preventDefault();
    nav(1);
  }
}

onMounted(() => window.addEventListener("keydown", onKey));

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
  // 清理所有缓存的 ObjectURL（防止内存泄漏）
  for (const blobUrl of urlCache.values()) {
    try { blobUrl.revoke(); } catch { /* ignore */ }
  }
  urlCache.clear();
});
</script>

<template>
  <transition name="lightbox-fade">
    <div
      v-if="current"
      class="lightbox"
      role="dialog"
      aria-modal="true"
      @click.self="close"
    >
      <button class="lb-btn lb-btn--close" aria-label="关闭" @click="close">
        <el-icon :size="22"><CircleClose /></el-icon>
      </button>

      <button
        v-if="items.length > 1"
        class="lb-btn lb-btn--nav lb-btn--prev"
        aria-label="上一张"
        @click="nav(-1)"
      >
        <el-icon :size="26"><ArrowLeft /></el-icon>
      </button>
      <button
        v-if="items.length > 1"
        class="lb-btn lb-btn--nav lb-btn--next"
        aria-label="下一张"
        @click="nav(1)"
      >
        <el-icon :size="26"><ArrowRight /></el-icon>
      </button>

      <figure class="lb-stage">
        <img
          v-if="current.type === 'photo'"
          :src="photoUrl"
          :alt="current.name"
          class="lb-img"
          @click.stop
        />
        <video
          v-else-if="current.type === 'video'"
          :src="photoUrl"
          class="lb-img"
          controls
          autoplay
          @click.stop
        />
        <div v-else class="lb-audio" @click.stop>
          <audio :src="photoUrl" controls autoplay />
        </div>
      </figure>

      <footer class="lb-info">
        <div class="lb-left">
          <span class="lb-badge" :data-type="current.type">
            {{ current.type === "photo" ? "照片" : current.type === "video" ? "录像" : "音频" }}
          </span>
          <span class="lb-time">{{ new Date(current.taken_at * 1000).toLocaleString("zh-CN") }}</span>
          <!-- P0 修复（2026-06-14）：硬编码 JPEG 改为按 mime_type 动态显示 -->
          <span class="lb-extra">
            {{ formatMimeShort(current.mime_type) }} · {{ current.width }}×{{ current.height }}
          </span>
        </div>
        <div class="lb-right">
          <span v-if="items.length > 1" class="lb-counter">
            {{ currentIndex + 1 }} / {{ items.length }}
          </span>
          <button class="lb-action" @click="download">
            <el-icon :size="14"><Download /></el-icon>
            <span>下载</span>
          </button>
        </div>
      </footer>
    </div>
  </transition>
</template>

<style scoped>
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0, 0, 0, 0.92);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 56px 80px;
}

.lb-stage {
  margin: 0;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.lb-img {
  display: block;
  max-width: 100%;
  max-height: 100%;
  width: auto;
  height: auto;
  object-fit: contain;
  border-radius: 4px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  animation: lb-in 240ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
@keyframes lb-in {
  from { opacity: 0; transform: scale(0.96); }
  to { opacity: 1; transform: scale(1); }
}
.lb-audio {
  width: 100%;
  max-width: 480px;
  padding: 24px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.lb-audio audio {
  width: 100%;
}

.lb-btn {
  position: absolute;
  appearance: none;
  border: 0;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.12);
  transition: background 160ms ease, transform 160ms ease, border-color 160ms ease;
  z-index: 2;
}
.lb-btn:hover {
  background: rgba(255, 255, 255, 0.16);
  border-color: rgba(255, 255, 255, 0.24);
  transform: scale(1.05);
}
.lb-btn--close {
  top: 20px;
  right: 20px;
}
.lb-btn--nav {
  top: 50%;
  transform: translateY(-50%);
  width: 48px;
  height: 48px;
}
.lb-btn--nav:hover {
  transform: translateY(-50%) scale(1.05);
}
.lb-btn--prev { left: 20px; }
.lb-btn--next { right: 20px; }

.lb-info {
  position: absolute;
  left: 50%;
  bottom: 20px;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  min-width: 480px;
  max-width: calc(100vw - 160px);
  padding: 10px 16px;
  background: rgba(20, 20, 20, 0.55);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 999px;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  color: rgba(255, 255, 255, 0.92);
  font-size: 12px;
}
.lb-left, .lb-right {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
.lb-badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  background: rgba(64, 158, 255, 0.18);
  color: #93c5fd;
}
.lb-badge[data-type="video"] { background: rgba(239, 68, 68, 0.18); color: #fca5a5; }
.lb-badge[data-type="audio"] { background: rgba(96, 105, 122, 0.18); color: #d1d5db; }
.lb-time {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-variant-numeric: tabular-nums;
  color: rgba(255, 255, 255, 0.88);
}
.lb-extra {
  color: rgba(255, 255, 255, 0.55);
  font-size: 11px;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}
.lb-counter {
  color: rgba(255, 255, 255, 0.7);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-variant-numeric: tabular-nums;
  font-size: 11px;
}
.lb-action {
  appearance: none;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  transition: background 160ms ease, border-color 160ms ease, transform 160ms ease;
}
.lb-action:hover {
  background: rgba(255, 255, 255, 0.18);
  border-color: rgba(255, 255, 255, 0.32);
  transform: translateY(-1px);
}

.lightbox-fade-enter-active,
.lightbox-fade-leave-active {
  transition: opacity 220ms ease;
}
.lightbox-fade-enter-from,
.lightbox-fade-leave-to {
  opacity: 0;
}
</style>
