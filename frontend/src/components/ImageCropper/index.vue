<script setup lang="ts">
/**
 * ImageCropper - 通用图片裁剪组件
 * 支持任意宽高比、旋转、圆形裁剪等功能
 */
import { ref, onMounted, onBeforeUnmount, nextTick, computed } from "vue";
import { ElMessage } from "element-plus";
import { ZoomIn, ZoomOut, RefreshRight } from "@element-plus/icons-vue";
import type { CropperOptions } from "./types";
import { fullDefaultOptions } from "./types";

const props = withDefaults(defineProps<CropperOptions>(), {
  outputWidth: 300,
  outputHeight: 300,
  outputFormat: "webp",
  quality: 0.85,
  maxSize: 5 * 1024 * 1024,
  accept: "image/*",
  aspectRatio: 0,
  backgroundColor: '#1f2329',
  maskColor: 'rgba(0, 0, 0, 0.5)',
  borderColor: '#409eff',
  guideColor: 'rgba(255, 255, 255, 0.4)',
  title: "裁剪图片",
  previewSize: 380,
  showGuideGrid: true,
  roundedCrop: false,
  minScale: 0.1,
  maxScale: 5,
  boundToCrop: true,
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
const imgScale = ref(1);
const imgOffset = ref({ x: 0, y: 0 });
const imgRotation = ref(0);
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

// 计算裁剪区域
const cropRegion = computed(() => {
  const size = props.previewSize;
  const aspectRatio = props.aspectRatio ?? 0;
  
  let cropW: number;
  let cropH: number;
  
  if (aspectRatio === 0) {
    // 自由裁剪，使用输出尺寸比例
    cropW = props.outputWidth ?? 300;
    cropH = props.outputHeight ?? 300;
  } else {
    // 固定宽高比
    cropW = size;
    cropH = size / aspectRatio;
    if (cropH > size) {
      cropH = size;
      cropW = size * aspectRatio;
    }
  }
  
  return {
    x: (size - cropW) / 2,
    y: (size - cropH) / 2,
    width: cropW,
    height: cropH,
  };
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
  if (file.size > (props.maxSize ?? 5 * 1024 * 1024)) {
    ElMessage.error(`图片不能超过 ${Math.round((props.maxSize ?? 5 * 1024 * 1024) / 1024 / 1024)}MB`);
    return;
  }

  const reader = new FileReader();
  reader.onload = (ev) => {
    const src = ev.target?.result as string;
    const image = new Image();
    image.onload = () => {
      img.value = image;
      // 初始缩放：图片完整显示在裁剪框内
      const crop = cropRegion.value;
      const scale = Math.min(crop.width / image.width, crop.height / image.height);
      imgScale.value = scale;
      imgOffset.value = { x: 0, y: 0 };
      imgRotation.value = 0;
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

  const size = props.previewSize ?? 380;
  const crop = cropRegion.value;
  const rounded = props.roundedCrop ?? false;

  // 清空，画背景
  ctx.fillStyle = props.backgroundColor ?? '#1f2329';
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  // 保存状态
  ctx.save();

  // 移动到中心
  const centerX = size / 2;
  const centerY = size / 2;
  ctx.translate(centerX, centerY);

  // 应用旋转
  if (imgRotation.value !== 0) {
    ctx.rotate((imgRotation.value * Math.PI) / 180);
  }

  // 计算图片尺寸
  const w = image.width * imgScale.value;
  const h = image.height * imgScale.value;
  const x = -w / 2 + imgOffset.value.x;
  const y = -h / 2 + imgOffset.value.y;

  // 圆形裁剪
  if (rounded) {
    const radius = Math.min(crop.width, crop.height) / 2;
    const cx = crop.x + crop.width / 2;
    const cy = crop.y + crop.height / 2;
    ctx.beginPath();
    ctx.arc(cx - centerX, cy - centerY, radius, 0, Math.PI * 2);
    ctx.clip();
  }

  // 绘制图片
  ctx.drawImage(image, x, y, w, h);

  // 恢复状态
  ctx.restore();

  // 绘制遮罩
  ctx.fillStyle = props.maskColor ?? 'rgba(0, 0, 0, 0.5)';
  
  if (rounded) {
    // 圆形遮罩
    const radius = Math.min(crop.width, crop.height) / 2;
    const cx = crop.x + crop.width / 2;
    const cy = crop.y + crop.height / 2;
    
    ctx.fillStyle = props.maskColor ?? 'rgba(0, 0, 0, 0.5)';
    ctx.beginPath();
    ctx.rect(0, 0, size, size);
    ctx.arc(cx, cy, radius, 0, Math.PI * 2, true);
    ctx.fill();
  } else {
    // 上
    ctx.fillRect(0, 0, size, crop.y);
    // 下
    ctx.fillRect(0, crop.y + crop.height, size, size - crop.y - crop.height);
    // 左
    ctx.fillRect(0, crop.y, crop.x, crop.height);
    // 右
    ctx.fillRect(crop.x + crop.width, crop.y, size - crop.x - crop.width, crop.height);
  }

  // 绘制裁剪框边框
  ctx.strokeStyle = props.borderColor ?? '#409eff';
  ctx.lineWidth = 2;
  
  if (rounded) {
    const radius = Math.min(crop.width, crop.height) / 2;
    const cx = crop.x + crop.width / 2;
    const cy = crop.y + crop.height / 2;
    ctx.beginPath();
    ctx.arc(cx, cy, radius, 0, Math.PI * 2);
    ctx.stroke();
  } else {
    ctx.strokeRect(crop.x, crop.y, crop.width, crop.height);
  }

  // 9 宫格引导线
  if ((props.showGuideGrid ?? true) && !rounded) {
    ctx.strokeStyle = props.guideColor ?? 'rgba(255, 255, 255, 0.4)';
    ctx.lineWidth = 1;
    const { x, y, width, height } = crop;
    
    for (let i = 1; i < 3; i++) {
      ctx.beginPath();
      ctx.moveTo(x + width * i / 3, y);
      ctx.lineTo(x + width * i / 3, y + height);
      ctx.stroke();
    }
    for (let i = 1; i < 3; i++) {
      ctx.beginPath();
      ctx.moveTo(x, y + height * i / 3);
      ctx.lineTo(x + width, y + height * i / 3);
      ctx.stroke();
    }
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
  
  const crop = cropRegion.value;
  const w = (img.value?.width ?? 0) * imgScale.value;
  const h = (img.value?.height ?? 0) * imgScale.value;
  const maxOffsetX = Math.max(0, (w - crop.width) / 2);
  const maxOffsetY = Math.max(0, (h - crop.height) / 2);
  
  if (props.boundToCrop ?? true) {
    imgOffset.value = {
      x: Math.max(-maxOffsetX, Math.min(maxOffsetX, imgOffset.value.x + dx)),
      y: Math.max(-maxOffsetY, Math.min(maxOffsetY, imgOffset.value.y + dy)),
    };
  } else {
    imgOffset.value = {
      x: imgOffset.value.x + dx,
      y: imgOffset.value.y + dy,
    };
  }
  
  draw();
};

const onMouseUp = () => {
  dragging.value = false;
};

// 滚轮缩放
const onWheel = (e: WheelEvent) => {
  e.preventDefault();
  const delta = -e.deltaY * 0.001;
  const minScale = props.minScale ?? 0.1;
  const maxScale = props.maxScale ?? 5;
  const newScale = Math.max(minScale, Math.min(maxScale, imgScale.value + delta));
  imgScale.value = newScale;
  draw();
};

// 缩放按钮
const zoomIn = () => {
  const maxScale = props.maxScale ?? 5;
  imgScale.value = Math.min(maxScale, imgScale.value + 0.1);
  draw();
};

const zoomOut = () => {
  const minScale = props.minScale ?? 0.1;
  imgScale.value = Math.max(minScale, imgScale.value - 0.1);
  draw();
};

// 旋转
const rotate = () => {
  imgRotation.value = (imgRotation.value + 90) % 360;
  if (img.value) {
    const crop = cropRegion.value;
    const rotatedW = imgRotation.value === 90 || imgRotation.value === 270 
      ? img.value.height : img.value.width;
    const rotatedH = imgRotation.value === 90 || imgRotation.value === 270 
      ? img.value.width : img.value.height;
    const scale = Math.min(crop.width / rotatedW, crop.height / rotatedH);
    imgScale.value = scale;
    imgOffset.value = { x: 0, y: 0 };
  }
  draw();
};

// 重置
const reset = () => {
  if (!img.value) return;
  const crop = cropRegion.value;
  imgScale.value = Math.min(crop.width / img.value.width, crop.height / img.value.height);
  imgOffset.value = { x: 0, y: 0 };
  imgRotation.value = 0;
  draw();
};

// 确认裁剪
const onConfirm = () => {
  if (!img.value || !canvasRef.value) {
    ElMessage.warning("请先选择图片");
    return;
  }
  submitting.value = true;

  try {
    const canvas = document.createElement("canvas");
    const outputW = props.outputWidth ?? 300;
    const outputH = props.outputHeight ?? 300;
    canvas.width = outputW;
    canvas.height = outputH;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      ElMessage.error("浏览器不支持 canvas");
      submitting.value = false;
      return;
    }

    const crop = cropRegion.value;
    const previewSize = props.previewSize ?? 380;
    const scaleX = previewSize / canvasRef.value.width;
    const scaleY = previewSize / canvasRef.value.height;

    // 绘制裁剪区域到输出 canvas
    ctx.drawImage(
      canvasRef.value,
      crop.x * scaleX, crop.y * scaleY, crop.width * scaleX, crop.height * scaleY,
      0, 0, outputW, outputH
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
      props.quality ?? 0.85
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
        <el-button @click="rotate" title="旋转90°">
          <el-icon><RefreshRight /></el-icon>
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
