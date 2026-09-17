<script setup lang="ts">
import { ref, watch } from "vue";
import { TP_STATUS_OPTIONS } from "@/api/third_party";
import ThirdPartyContractList from "./components/ThirdPartyContractList.vue";

defineOptions({ name: "ContractManagement" });

/**
 * 2026-07-06 精简：原 index.vue 包含 "mine" / "third" 两个 Tab。
 *   主合同（mine / 在线文档）已下线，整页改为只显示第三方合同列表。
 * 2026-07-07 "三方文档"→"文档列表"用户可见文案统一
 * 2026-09-17 重命名为"文档库"（隶属于"资料库"顶级菜单）
 */

// 暴露 ref 给子组件，用于刷新
const listRef = ref<InstanceType<typeof ThirdPartyContractList>>();

// 筛选工具栏状态（同步给子组件）
const searchKeyword = ref("");
const statusFilter = ref("");
const typeFilter = ref<"" | "individual" | "enterprise">("");

// 触发搜索
function handleSearch() {
  listRef.value?.loadList();
}
</script>

<template>
  <div class="contract-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="page-title">文档库</span>
          <!-- 筛选工具栏 -->
          <div class="header-toolbar">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索合同号/标题/客户名/企业名/电话"
              style="width: 280px; margin-right: 10px"
              clearable
              @keyup.enter="handleSearch"
            />
            <el-select
              v-model="typeFilter"
              placeholder="客户类型"
              style="width: 110px; margin-right: 10px"
              clearable
              @change="handleSearch"
            >
              <el-option label="个人" value="individual" />
              <el-option label="企业" value="enterprise" />
            </el-select>
            <el-select
              v-model="statusFilter"
              placeholder="状态"
              style="width: 110px; margin-right: 10px"
              clearable
              @change="handleSearch"
            >
              <el-option
                v-for="o in TP_STATUS_OPTIONS"
                :key="o.value"
                :label="o.label"
                :value="o.value"
              />
            </el-select>
            <el-button type="primary" @click="handleSearch">
              <i class="ri-search-line" style="margin-right: 4px"></i>
              搜索
            </el-button>
            <el-button @click="handleSearch">
              <i class="ri-refresh-line" style="margin-right: 4px"></i>
              刷新
            </el-button>
          </div>
        </div>
      </template>

      <ThirdPartyContractList
        ref="listRef"
        hide-toolbar
        :search-keyword="searchKeyword"
        :status-filter="statusFilter"
        :type-filter="typeFilter"
      />
    </el-card>
  </div>
</template>

<style scoped>
.contract-container {
  padding: 20px;
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