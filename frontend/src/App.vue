<template>
  <ElConfigProvider>
    <router-view />
    <ReDialog />
  </ElConfigProvider>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import { ReDialog } from "@/components/ReDialog";
import ElConfigProvider from "@/components/ElConfigProvider/index.vue";

export default defineComponent({
  name: "app",
  inheritAttrs: false,
  components: {
    ElConfigProvider,
    ReDialog
  }
});
</script>
