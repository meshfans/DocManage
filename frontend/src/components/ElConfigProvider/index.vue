<script setup lang="ts">
import { ElConfigProvider } from "element-plus";
import zhCn from "element-plus/es/locale/lang/zh-cn";

defineOptions({
  name: "ElConfigProvider",
  inheritAttrs: false
});
</script>

<template>
  <el-config-provider v-bind="$attrs" :locale="zhCn">
    <slot />
  </el-config-provider>
</template>
