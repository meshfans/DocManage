<script setup lang="ts">
import ThirdPartyContractList from "./components/ThirdPartyContractList.vue";

defineOptions({ name: "ContractManagement" });

/**
 * 2026-07-06 精简：原 index.vue 包含 "mine" / "third" 两个 Tab。
 *   主合同（mine / 在线文档）已下线，整页改为只显示第三方合同列表。
 * 2026-07-07 "三方文档"→"文档列表"用户可见文案统一
 */
</script>

<template>
  <div class="contract-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="page-title">文档列表</span>
        </div>
      </template>

      <ThirdPartyContractList />
    </el-card>
  </div>
</template>

<style scoped>
.contract-container {
  padding: 0;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.page-title {
  font-size: 16px;
  font-weight: 600;
}
</style>