<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick, computed } from "vue";
import { ElMessage } from "element-plus";
import { ZoomIn, ZoomOut, RefreshRight } from "@element-plus/icons-vue";

const props = withDefaults(defineProps<{
  /** 输出图片尺寸（宽高），默认 300 */
  outputSize?: number;
  /** 输出格式，默认 webp */
  outputFormat?: "webp" | "jpeg" | "png";
  /** 输出质量（0-1），仅 webp/jpeg 有效，默认 0.85 */
  quality?: number;
  /** 最大文件大小（字节），默认 5MB */
  maxSize?: number;
  /** 允许的文件类型，默认 image/* */
  accept?: string;
  /** 弹窗标题，默认"裁剪图片" */
  title?: string;
  /** 预览画布尺寸，默认 380 */
  previewSize?: number;
}>(), {
  outputSize: 300,
  outputFormat: "webp",
  quality: 0.85,
  maxSize: 5 * 1024 * 1024,
  accept: "image/*",
  title: "裁剪图片",
  previewSize: 380
});

const emit = defineEmits<{
  (e: "confirm", blob: Blob): void;
}>();

const fileInputRef = ref<HTMLInputElement | null>(null);

const dialogVisible = ref(false);
const canvasRef = ref<HTMLCanvasElement | null>(null);
const submitting = ref(false);

// 状态
const img = ref<HTMLImageElement | null>(null);
const imgScale = ref(1); // 用户缩放系数
const imgOffset = ref({ x: 0, y: 0 }); // 图片平移（相对裁剪框中心）
const dragging = ref(false);
const lastPos = ref({ x: 0, y: 0 });

// 计算 MIME 类型
const outputMimeType = computed(() => {
  switch (props.outputFormat) {
    case "jpeg": return "image/jpeg";
    case "png": return "image/png";
    default: return "image/webp";
  }
});

const open = () => {
  fileInputRef.value?.click();
};

defineExpose({ open });

const onFileChange = (e: Event) => {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;

  if (!file.type.startsWith("image/")) {
    ElMessage.error("只能选择图片文件");
    return;
  }
  if (file.size > props.maxSize) {
    ElMessage.error(`图片不能超过 ${Math.round(props.maxSize / 1024 / 1024)}MB`);
    return;
  }

  const reader = new FileReader();
  reader.onload = (ev) => {
    const src = ev.target?.result as string;
    const image = new Image();
    image.onload = () => {
      img.value = image;
      // 初始缩放：图片完整显示在裁剪框内
      const scale = Math.max(props.outputSize / image.width, props.outputSize / image.height);
      imgScale.value = scale;
      imgOffset.value = { x: 0, y: 0 };
      dialogVisible.value = true;
      nextTick(draw);
    };
    image.onerror = () => ElMessage.error("图片加载失败");
    image.src = src;
  };
  reader.readAsDataURL(file);
};

// 绘制
const draw = () => {
  const canvas = canvasRef.value;
  const image = img.value;
  if (!canvas || !image) return;

  const ctx = canvas.getContext("2d");
  if (!ctx) return;

  const size = props.outputSize;

  // 清空，画背景
  ctx.fillStyle = "#1f2329";
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  // 计算图片绘制尺寸
  const w = image.width * imgScale.value;
  const h = image.height * imgScale.value;
  const cx = canvas.width / 2 + imgOffset.value.x;
  const cy = canvas.height / 2 + imgOffset.value.y;
  const x = cx - w / 2;
  const y = cy - h / 2;

  // 绘制图片
  ctx.drawImage(image, x, y, w, h);

  // 绘制半透明遮罩（裁剪框外的区域）
  ctx.fillStyle = "rgba(0, 0, 0, 0.5)";
  const cutX = (canvas.width - size) / 2;
  const cutY = (canvas.height - size) / 2;
  // 上
  ctx.fillRect(0, 0, canvas.width, cutY);
  // 下
  ctx.fillRect(0, cutY + size, canvas.width, canvas.height - cutY - size);
  // 左
  ctx.fillRect(0, cutY, cutX, size);
  // 右
  ctx.fillRect(cutX + size, cutY, canvas.width - cutX - size, size);

  // 绘制裁剪框边框
  ctx.strokeStyle = "#409eff";
  ctx.lineWidth = 2;
  ctx.strokeRect(cutX, cutY, size, size);

  // 9 宫格引导线
  ctx.strokeStyle = "rgba(255, 255, 255, 0.4)";
  ctx.lineWidth = 1;
  for (let i = 1; i < 3; i++) {
    ctx.beginPath();
    ctx.moveTo(cutX + (size * i) / 3, cutY);
    ctx.lineTo(cutX + (size * i) / 3, cutY + size);
    ctx.stroke();
    ctx.beginPath();
    ctx.moveTo(cutX, cutY + (size * i) / 3);
    ctx.lineTo(cutX + size, cutY + (size * i) / 3);
    ctx.stroke();
  }
};

// 拖动
const onMouseDown = (e: MouseEvent) => {
  dragging.value = true;
  lastPos.value = { x: e.clientX, y: e.clientY };
};

const onMouseMove = (e: MouseEvent) => {
  if (!dragging.value) return;
  const dx = e.clientX - lastPos.value.x;
  const dy = e.clientY - lastPos.value.y;
  lastPos.value = { x: e.clientX, y: e.clientY };
  imgOffset.value = {
    x: imgOffset.value.x + dx,
    y: imgOffset.value.y + dy
  };
  draw();
};

const onMouseUp = () => {
  dragging.value = false;
};

// 滚轮缩放
const onWheel = (e: WheelEvent) => {
  e.preventDefault();
  const delta = e.deltaY > 0 ? -0.05 : 0.05;
  const newScale = Math.max(0.1, Math.min(5, imgScale.value + delta));
  imgScale.value = newScale;
  draw();
};

// 缩放按钮
const zoomIn = () => {
  imgScale.value = Math.min(5, imgScale.value + 0.1);
  draw();
};
const zoomOut = () => {
  imgScale.value = Math.max(0.1, imgScale.value - 0.1);
  draw();
};
const reset = () => {
  if (!img.value) return;
  imgScale.value = Math.max(props.outputSize / img.value.width, props.outputSize / img.value.height);
  imgOffset.value = { x: 0, y: 0 };
  draw();
};

// 确认裁剪
const onConfirm = () => {
  if (!img.value) {
    ElMessage.warning("请先选择图片");
    return;
  }
  submitting.value = true;

  try {
    const canvas = document.createElement("canvas");
    canvas.width = props.outputSize;
    canvas.height = props.outputSize;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      ElMessage.error("浏览器不支持 canvas");
      submitting.value = false;
      return;
    }

    const image = img.value;
    // canvas 中图片的绘制参数
    const w = image.width * imgScale.value;
    const h = image.height * imgScale.value;
    const cx = canvasRef.value!.width / 2 + imgOffset.value.x;
    const cy = canvasRef.value!.height / 2 + imgOffset.value.y;
    const imgX = cx - w / 2;
    const imgY = cy - h / 2;

    // 裁剪框在 canvas 上的位置
    const cutX = (canvasRef.value!.width - props.outputSize) / 2;
    const cutY = (canvasRef.value!.height - props.outputSize) / 2;

    // 将裁剪框区域绘制到输出 canvas
    ctx.drawImage(
      canvasRef.value!, // 源 canvas（含图片）
      cutX, cutY, props.outputSize, props.outputSize, // 源裁剪区域
      0, 0, props.outputSize, props.outputSize // 目标区域
    );

    canvas.toBlob(
      (blob) => {
        submitting.value = false;
        if (!blob) {
          ElMessage.error("裁剪失败，请重试");
          return;
        }
        emit("confirm", blob);
        dialogVisible.value = false;
      },
      outputMimeType.value,
      props.quality
    );
  } catch (e: any) {
    submitting.value = false;
    ElMessage.error(e?.message || "裁剪失败");
  }
};

const onCancel = () => {
  dialogVisible.value = false;
};

const onClose = () => {
  dialogVisible.value = false;
};

// 全局监听（避免鼠标移出 canvas 后丢失）
onMounted(() => {
  window.addEventListener("mousemove", onMouseMove);
  window.addEventListener("mouseup", onMouseUp);
});

onBeforeUnmount(() => {
  window.removeEventListener("mousemove", onMouseMove);
  window.removeEventListener("mouseup", onMouseUp);
});
</script>

<template>
  <input
    ref="fileInputRef"
    type="file"
    :accept="accept"
    style="display: none"
    @change="onFileChange"
  />

  <el-dialog
    v-model="dialogVisible"
    :title="title"
    width="500px"
    :close-on-click-modal="false"
    @close="onClose"
  >
    <div class="cropper-stage">
      <canvas
        ref="canvasRef"
        :width="previewSize"
        :height="previewSize"
        @mousedown="onMouseDown"
        @wheel="onWheel"
      />
      <div class="cropper-tip">
        滚轮缩放 · 拖动平移
      </div>
    </div>

    <div class="cropper-toolbar">
      <el-button-group>
        <el-button @click="zoomOut" title="缩小">
          <el-icon><ZoomOut /></el-icon>
        </el-button>
        <el-button @click="zoomIn" title="放大">
          <el-icon><ZoomIn /></el-icon>
        </el-button>
        <el-button @click="reset" title="重置">
          <el-icon><RefreshRight /></el-icon>
        </el-button>
      </el-button-group>
    </div>

    <template #footer>
      <el-button @click="onCancel">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="onConfirm">
        确认裁剪
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.cropper-stage {
  display: flex;
  flex-direction: column;
  align-items: center;
  background: #fafafa;
  border-radius: 4px;
  padding: 10px;
}

.cropper-stage canvas {
  display: block;
  cursor: move;
  user-select: none;
  border-radius: 4px;
}

.cropper-tip {
  margin-top: 8px;
  font-size: 12px;
  color: #909399;
}

.cropper-toolbar {
  display: flex;
  justify-content: center;
  margin-top: 16px;
}
</style>
