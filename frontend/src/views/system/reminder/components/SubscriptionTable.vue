<script setup lang="ts">
// 第十二阶段：提醒订阅列表（可复用）
// 场景：
//   1. /system/reminder 订阅 Tab（无过滤，全量）
//   2. /customer/contracts?tab=subscriptions 客户维度（customerId 过滤）
//   3. /contract/edit?id=X 合同详情页弹窗（contractId 过滤）
//   4. /third-party-contract/detail?thirdPartyId=X 同上

import { ref, watch, computed, onMounted } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  listReminderSubscriptions,
  updateReminderSubscription,
  deleteReminderSubscription,
  type ReminderSubscription
} from "@/api/reminder";
import { formatTimestampLang } from "@/utils/date";

const props = defineProps<{
  // 第十三阶段 v4：link_type + link_id 替代 customerId / contractId
  linkType?: "customer" | "contract" | "third_party_contract";
  linkId?: number;
  // 第十三阶段 v4：是否隐藏新增按钮
  // （SubscriptionDialog 内嵌时 = true；/system/reminder 全量管理 = false，父组件自带 dialog）
  // 保留兼容性：deprecated，用父组件 emit 'create' 代替
  hideCreate?: boolean;
  // 优化 2：是否隐藏"当前查看：xxx #N 的所有订阅"提示（dialog 内嵌时 = true，避免重复）
  hideHeaderHint?: boolean;
}>();

const emit = defineEmits<{
  (e: "refresh"): void;
  (e: "create"): void;  // 第十三阶段 v4：通知父组件打开"新建订阅" Dialog
}>();

// 第十三阶段 v4：link_type 标签辅助
function linkTypeLabel(t?: string): string {
  switch (t) {
    case "customer": return "客户";
    case "contract": return "主合同";
    case "third_party_contract": return "文档";
    default: return "客户";
  }
}
function linkTypeTagType(t?: string): "primary" | "success" | "warning" | "info" {
  switch (t) {
    case "customer": return "info";
    case "contract": return "warning";
    case "third_party_contract": return "primary";
    default: return "info";
  }
}
// 接收人标签辅助（保留用于"接收人"列）
function receiverTypeLabel(t?: string): string {
  switch (t) {
    case "admin": return "管理员";
    case "contract_owner": return "合同创建人";
    case "customer_owner": return "客户主负责人";
    case "department": return "部门内全员";
    case "user": return "指定用户";
    default: return "管理员";
  }
}
function receiverTypeTagType(t?: string): "primary" | "success" | "warning" | "info" {
  switch (t) {
    case "admin": return "info";
    case "contract_owner": return "warning";
    case "customer_owner": return "warning";
    case "department": return "success";
    case "user": return "primary";
    default: return "info";
  }
}
// 第十三阶段 v2：解析 receiver_id CSV 字符串为 ID 数组 + 拼接展示
function formatReceiverIDs(csv: string): string {
  if (!csv || csv === "0") return "";
  return csv
    .split(",")
    .map(s => s.trim())
    .filter(s => s)
    .map(s => `#${s}`)
    .join(", ");
}
function receiverIDCount(csv: string): number {
  if (!csv || csv === "0") return 0;
  return csv.split(",").filter(s => s.trim()).length;
}
// 第十三阶段 v4：link_id 同理
function formatLinkIDs(csv: string): string {
  if (!csv || csv === "0") return "";
  return csv
    .split(",")
    .map(s => s.trim())
    .filter(s => s)
    .map(s => `#${s}`)
    .join(", ");
}
function linkIDCount(csv: string): number {
  if (!csv || csv === "0") return 0;
  return csv.split(",").filter(s => s.trim()).length;
}

const list = ref<ReminderSubscription[]>([]);
const loading = ref(false);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

// ==================== 过滤 ====================
const filterParams = computed(() => {
  const p: Record<string, number | string> = {};
  if (props.linkType && props.linkId && props.linkId > 0) {
    p.link_type = props.linkType;
    p.link_id = props.linkId;
  }
  return p;
});

async function loadList() {
  loading.value = true;
  try {
    const res: any = await listReminderSubscriptions({
      ...filterParams.value,
      page: page.value,
      page_size: pageSize.value
    });
    if (res.success) {
      list.value = res.data.list || [];
      total.value = res.data.total || 0;
    } else {
      ElMessage.error(res.message || "加载订阅失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  } finally {
    loading.value = false;
  }
}

watch(
  () => [props.linkType, props.linkId],
  () => {
    page.value = 1;
    loadList();
  }
);
onMounted(loadList);

// ==================== 操作 ====================
async function handleToggle(row: ReminderSubscription) {
  try {
    const res: any = await updateReminderSubscription(row.id, {
      is_active: !row.is_active
    });
    if (res.success) {
      ElMessage.success(row.is_active ? "已停用" : "已启用");
      loadList();
      emit("refresh");
    } else {
      ElMessage.error(res.message || "更新失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "更新失败");
  }
}

async function handleDelete(row: ReminderSubscription) {
  try {
    await ElMessageBox.confirm(
      `确认删除订阅「${row.template_name || row.template_key || row.id}」？`,
      "删除订阅",
      {
        type: "warning",
        confirmButtonText: "确定删除",
        cancelButtonText: "取消"
      }
    );
  } catch {
    return;
  }
  try {
    const res: any = await deleteReminderSubscription(row.id);
    if (res.success) {
      ElMessage.success("已删除");
      loadList();
      emit("refresh");
    } else {
      ElMessage.error(res.message || "删除失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "删除失败");
  }
}

// ==================== 新建订阅（emit 给父组件，避免循环引用） ====================
function handleCreate() {
  // 第十三阶段 v4：不再内嵌 SubscriptionDialog（避免循环引用）
  // 改为通知父组件打开 Dialog
  emit("create");
}
</script>

<template>
  <div class="subscription-table">
    <div class="table-toolbar">
      <!-- "+ 新建订阅" 按钮已移除（父组件 /system/reminder 顶部自带，SubscriptionDialog 内嵌时不显示） -->
      <span v-if="!hideHeaderHint" class="hint">
        <template v-if="linkType && linkId && linkId > 0">
          当前查看：{{ linkType }} #{{ linkId }} 的所有订阅
        </template>
      </span>
    </div>

    <el-table
      v-loading="loading"
      :data="list"
      stripe
      style="width: 100%"
      empty-text="暂无订阅"
    >
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="template_name" label="提醒规则" min-width="180">
        <template #default="{ row }">
          <div>{{ row.template_name || row.template_key || "-" }}</div>
          <div class="sub">
            <el-tag
              v-if="row.template_advance_days !== undefined"
              size="small"
              type="info"
            >
              提前 {{ row.template_advance_days }} 天
            </el-tag>
          </div>
        </template>
      </el-table-column>
      <!-- 第十三阶段 v4：link_type + link_id 替代 customer_id + contract_id -->
      <el-table-column label="订阅对象" min-width="220">
        <template #default="{ row }">
          <el-tag
            :type="linkTypeTagType(row.link_type)"
            size="small"
            effect="plain"
          >
            {{ linkTypeLabel(row.link_type) }}
          </el-tag>
          <span
            v-if="row.link_id"
            class="ml-1 ids"
            :title="`${linkIDCount(row.link_id)} 个对象`"
          >
            {{ formatLinkIDs(row.link_id) }}
          </span>
        </template>
      </el-table-column>
      <!-- 第十三阶段：接收人列 -->
      <el-table-column label="接收人" min-width="220">
        <template #default="{ row }">
          <el-tag
            :type="receiverTypeTagType(row.receiver_type)"
            size="small"
            effect="plain"
          >
            {{ receiverTypeLabel(row.receiver_type) }}
          </el-tag>
          <span
            v-if="row.receiver_type === 'department' || row.receiver_type === 'user'"
            class="ml-1 ids"
            :title="`${receiverIDCount(row.receiver_id)} 个接收人`"
          >
            {{ formatReceiverIDs(row.receiver_id) }}
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
      <el-table-column label="状态" width="90" align="center">
        <template #default="{ row }">
          <el-tag
            :type="row.is_active ? 'success' : 'info'"
            size="small"
          >
            {{ row.is_active ? "启用" : "停用" }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="160">
        <template #default="{ row }">
          {{ formatTimestampLang(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <el-button
            v-perms="'reminder:subscriptions:update'"
            link
            type="primary"
            size="small"
            @click="handleToggle(row)"
          >
            {{ row.is_active ? "停用" : "启用" }}
          </el-button>
          <el-button
            v-perms="'reminder:subscriptions:delete'"
            link
            type="danger"
            size="small"
            @click="handleDelete(row)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="page"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[10, 20, 50]"
      layout="total, sizes, prev, pager, next, jumper"
      class="pagination"
      @current-change="loadList"
      @size-change="loadList"
    />
  </div>
</template>

<style scoped>
.subscription-table {
  padding: 0;
}
.table-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sub {
  margin-top: 2px;
  font-size: 12px;
}
.ml-1 {
  margin-left: 6px;
}
.ids {
  font-family: monospace;
  font-size: 12px;
  color: #666;
  word-break: break-all;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
