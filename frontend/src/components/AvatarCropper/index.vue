<script setup lang="ts">
/**
 * 头像裁剪器
 * 基于通用 ImageCropper，输出 300x300 WebP 格式
 */
import ImageCropper from "@/components/ImageCropper/index.vue";

defineOptions({
  name: "AvatarCropper"
});

const emit = defineEmits<{
  (e: "confirm", blob: Blob): void;
}>();

const cropperRef = ref<InstanceType<typeof ImageCropper> | null>(null);

const open = () => {
  cropperRef.value?.open();
};

defineExpose({ open });

const onConfirm = (blob: Blob) => {
  emit("confirm", blob);
};
</script>

<template>
  <ImageCropper
    ref="cropperRef"
    title="裁剪头像"
    :output-size="300"
    output-format="webp"
    :quality="0.85"
    :max-size="5 * 1024 * 1024"
    accept="image/jpeg,image/png,image/gif,image/webp"
    :preview-size="380"
    @confirm="onConfirm"
  />
</template>
