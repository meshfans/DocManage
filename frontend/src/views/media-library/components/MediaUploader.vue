<script setup lang="ts">
/**
 * MediaUploader 拖拽上传组件
 * - 拖拽高亮 + 进度 + 3 哈希计算（前端预览）
 * - 上传后通过 emit('uploaded') 通知父组件刷新列表
 * - 视频文件：抽帧作为 thumbnail（extractVideoThumb 工具），
 *             同时读取 width/height/duration 一并传入后端
 *
 * 2026-06-27 Bug #5 修复：新增可选 prop `customerId`，
 *   - > 0 时随 multipart 一并 append `customer_id` 字段（后端 media.go:UploadMedia 已支持）
 *   - 父组件（/media/library 来自 URL ?customer_id=）传入即自动关联
 */
import { ref, computed } from "vue";
import { ElMessage } from "element-plus";
import { Upload, Document } from "@element-plus/icons-vue";
import { uploadMedia, checkMediaHash } from "@/api/media";

// Props：
//   - customerId：当前页面作用域的客户 ID
//     · > 0 → 上传时自动 append 到 FormData（后端会写入 media.customer_id）
//     · 0 / undefined → 不 append，与未传等价
const props = defineProps<{
  customerId?: number;
}>();

const emit = defineEmits<{
  uploaded: [];
}>();

const dragging = ref(false);
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);

// 拖拽事件
function onDragEnter(e: DragEvent) {
  e.preventDefault();
  dragging.value = true;
}
function onDragOver(e: DragEvent) {
  e.preventDefault();
  dragging.value = true;
}
function onDragLeave(e: DragEvent) {
  e.preventDefault();
  // 防止子元素触发
  if (e.target === e.currentTarget) dragging.value = false;
}
function onDrop(e: DragEvent) {
  e.preventDefault();
  dragging.value = false;
  const files = e.dataTransfer?.files;
  if (files && files.length) {
    handleFiles(Array.from(files));
  }
}

// 点击选择
function openPicker() {
  fileInput.value?.click();
}
function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement;
  if (input.files) {
    handleFiles(Array.from(input.files));
    input.value = ""; // 重置
  }
}

/**
 * 从视频 Blob 抽第一帧，返回 JPEG 缩略图 Blob。
 *   - 取 t = min(0.5s, duration*0.1) 避开黑帧
 *   - 最大 320×240，JPEG Q=0.75
 *   - 30s 超时
 *   - 失败返回 null（不阻塞上传）
 */
async function extractVideoThumb(blob: Blob): Promise<Blob | null> {
  return new Promise(resolve => {
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    const url = URL.createObjectURL(blob);
    let cleaned = false;
    const cleanup = () => {
      if (cleaned) return;
      cleaned = true;
      try { URL.revokeObjectURL(url); } catch { /* ignore */ }
      video.remove();
    };
    const fail = () => { cleanup(); resolve(null); };
    const timer = window.setTimeout(fail, 30000);
    video.onloadedmetadata = () => {
      const dur = isFinite(video.duration) && video.duration > 0 ? video.duration : 1;
      const t = Math.min(0.5, dur * 0.1);
      try { video.currentTime = t; } catch { fail(); window.clearTimeout(timer); }
    };
    video.onseeked = () => {
      try {
        const vw = video.videoWidth || 320;
        const vh = video.videoHeight || 240;
        const maxW = 320, maxH = 240;
        const scale = Math.min(maxW / vw, maxH / vh, 1);
        const cw = Math.max(1, Math.round(vw * scale));
        const ch = Math.max(1, Math.round(vh * scale));
        const canvas = document.createElement("canvas");
        canvas.width = cw;
        canvas.height = ch;
        const ctx = canvas.getContext("2d");
        if (!ctx) { fail(); window.clearTimeout(timer); return; }
        ctx.drawImage(video, 0, 0, cw, ch);
        canvas.toBlob(
          b => { window.clearTimeout(timer); cleanup(); resolve(b || null); },
          "image/jpeg",
          0.75
        );
      } catch { fail(); window.clearTimeout(timer); }
    };
    video.onerror = () => { fail(); window.clearTimeout(timer); };
    video.src = url;
  });
}

/** 探测视频元数据：{ width, height, duration }；失败返回 null */
function probeVideoMeta(blob: Blob): Promise<{ width: number; height: number; duration: number } | null> {
  return new Promise(resolve => {
    const video = document.createElement("video");
    video.preload = "metadata";
    const url = URL.createObjectURL(blob);
    let cleaned = false;
    const cleanup = () => {
      if (cleaned) return;
      cleaned = true;
      try { URL.revokeObjectURL(url); } catch { /* ignore */ }
      video.src = "";
      video.remove();
    };
    const timer = window.setTimeout(() => {
      cleanup();
      resolve(null);
    }, 15000); // 增加超时时间，seek策略可能需要更长时间

    video.onloadedmetadata = () => {
      const width = video.videoWidth || 0;
      const height = video.videoHeight || 0;
      let duration = isFinite(video.duration) ? Math.floor(video.duration) : 0;

      // 某些webm格式duration在metadata中不可用，需要seek到末尾获取
      if (duration === 0) {
        video.currentTime = 1e10; // seek到末尾附近
        video.onseeked = () => {
          window.clearTimeout(timer);
          duration = isFinite(video.duration) ? Math.floor(video.duration) : 0;
          cleanup();
          resolve({ width, height, duration });
        };
        video.onerror = () => {
          window.clearTimeout(timer);
          cleanup();
          resolve({ width, height, duration: 0 });
        };
      } else {
        window.clearTimeout(timer);
        cleanup();
        resolve({ width, height, duration });
      }
    };

    video.onerror = () => {
      window.clearTimeout(timer);
      cleanup();
      resolve(null);
    };

    video.src = url;
  });
}

async function handleFiles(files: File[]) {
  if (uploading.value) {
    ElMessage.warning("正在上传中，请稍候");
    return;
  }

  uploading.value = true;
  try {
    for (const file of files) {
      // 1) 上传前查重（按 SHA-256）
      try {
        const buf = await file.arrayBuffer();
        const hash = await sha256Hex(buf);
        const dup = await checkMediaHash(hash);
        if (dup.success && dup.data.exists) {
          ElMessage.warning(
            `「${file.name}」是已存在的媒体（#${dup.data.id}），跳过上传`
          );
          continue;
        }
      } catch (e) {
        // 查重失败不阻塞上传
        console.warn("[uploader] check-hash failed", e);
      }

      // 2) multipart 上传（视频抽帧 + 元数据）
      const form = new FormData();
      form.append("file", file);
      form.append("name", file.name);
      form.append("source", "upload");
      form.append("file_size", String(file.size));
      // P 修复：传 taken_at（秒）让后端入库时使用文件真实创建时间，
      // 避免"上传时间"被错误地记为"拍摄时间"
      // file.lastModified 是毫秒，/1000 转为秒
      const takenAt = Math.floor(file.lastModified / 1000);
      if (takenAt > 0) form.append("taken_at", String(takenAt));
      // Bug #5 修复：当父组件传入 customerId > 0（来自 URL ?customer_id= 或手动选择）
      // 时，把 customer_id append 到 multipart。后端 media.go:UploadMedia 会写入
      // media.customer_id，从而在 /media/library?customer_id= 的过滤视图下立即可见。
      if (props.customerId && props.customerId > 0) {
        form.append("customer_id", String(props.customerId));
      }
      // 视频：抽帧 + 元数据 → 后端 SaveThumbnail + 写入
      const isVideo = file.type.startsWith("video/") || /\.(mp4|webm|mov|avi)$/i.test(file.name);
      if (isVideo) {
        // 元数据探测
        try {
          const meta = await probeVideoMeta(file);
          if (meta) {
            if (meta.width > 0) form.append("width", String(meta.width));
            if (meta.height > 0) form.append("height", String(meta.height));
            if (meta.duration > 0) form.append("duration", String(meta.duration));
          }
        } catch (e) {
          console.warn("[uploader] probe video meta failed", e);
        }
        // 抽帧（最多等 5s）
        try {
          const thumbPromise = extractVideoThumb(file);
          const thumb = await Promise.race([
            thumbPromise,
            new Promise<Blob | null>(resolve => setTimeout(() => resolve(null), 5000))
          ]);
          if (thumb) form.append("thumbnail", thumb, "thumb.jpg");
        } catch (e) {
          console.warn("[uploader] extract video thumb failed", e);
        }
      }

      const res = await uploadMedia(form);
      if (res.success) {
        const d = res.data;
        if (d.reused) {
          ElMessage.success(`「${file.name}」复用 #${d.id}`);
        } else {
          ElMessage.success(
            `「${file.name}」已保存 #${d.id}（SM3 ${d.hash.sm3.slice(0, 8)}… / SHA256 ${d.hash.sha256.slice(0, 8)}…）`
          );
        }
      }
    }
    emit("uploaded");
  } catch (e: any) {
    ElMessage.error(`上传失败：${e?.message ?? e}`);
  } finally {
    uploading.value = false;
  }
}

// SHA-256 (Web Crypto)
async function sha256Hex(buf: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", buf);
  return Array.from(new Uint8Array(digest))
    .map(b => b.toString(16).padStart(2, "0"))
    .join("");
}
</script>

<template>
  <div
    class="media-uploader"
    :class="{ 'is-dragging': dragging, 'is-uploading': uploading }"
    @dragenter="onDragEnter"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
    @click="openPicker"
  >
    <input
      ref="fileInput"
      type="file"
      accept="image/*,video/*,audio/*"
      multiple
      style="display: none"
      @change="onFileChange"
    />
    <div class="uploader-inner">
      <el-icon class="uploader-icon" :size="36">
        <Upload />
      </el-icon>
      <p class="uploader-title">
        {{ uploading ? "上传中…" : "拖拽文件到这里，或点击选择" }}
      </p>
      <p class="uploader-hint">
        支持图片 / 视频 / 音频 · 同一文件自动复用（SM3 + SHA-256 查重）
      </p>
      <div v-if="uploading" class="uploader-progress">
        <el-progress :percentage="50" :indeterminate="true" :show-text="false" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.media-uploader {
  position: relative;
  padding: 24px;
  background: var(--el-bg-color, #fff);
  border: 2px dashed var(--el-border-color, #dcdfe6);
  border-radius: 12px;
  cursor: pointer;
  transition: border-color 200ms ease, background 200ms ease;
  margin-bottom: 16px;
}
.media-uploader:hover {
  border-color: var(--el-color-primary, #409eff);
  background: rgba(64, 158, 255, 0.02);
}
.media-uploader.is-dragging {
  border-color: var(--el-color-primary, #409eff);
  background: rgba(64, 158, 255, 0.06);
}
.media-uploader.is-uploading {
  pointer-events: none;
  opacity: 0.7;
}

.uploader-inner {
  text-align: center;
}

.uploader-icon {
  color: var(--el-color-primary, #409eff);
  margin-bottom: 8px;
}

.uploader-title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary, #1f2329);
}

.uploader-hint {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary, #606266);
  line-height: 1.5;
}

.uploader-progress {
  margin-top: 12px;
  max-width: 360px;
  margin-left: auto;
  margin-right: auto;
}
</style>
