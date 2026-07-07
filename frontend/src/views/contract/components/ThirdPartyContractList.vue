<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { formatTimestampLang } from "@/utils/date";
import {
  listThirdPartyContracts,
  deleteThirdPartyContract,
  changeThirdPartyContractStatus,
  downloadThirdPartyContractPdf,
  bulkDownloadThirdPartyContracts,
  TP_STATUS_OPTIONS,
  canTPTransition,
  canTPDelete,
  type ThirdPartyContract,
  type CustomerLite
} from "@/api/third_party";
// 第十三阶段 v4：提醒订阅入口（列表层）
import SubscriptionDialog from "@/views/system/reminder/components/SubscriptionDialog.vue";

defineOptions({
  name: "ThirdPartyContractList"
});

const props = defineProps<{
  // 客户视图下传入：仅显示该客户下的文档
  customerId?: number;
}>();

const router = useRouter();
const loading = ref(false);
const contracts = ref<ThirdPartyContract[]>([]);
const customerMap = ref<Record<string, CustomerLite>>({});
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const searchKeyword = ref("");
const statusFilter = ref("");
const typeFilter = ref<"" | "individual" | "enterprise">("");

async function loadList() {
  loading.value = true;
  try {
    // props.customerId 无效（0 / NaN / undefined）→ customer_id: undefined
    // 后端 `customerID > 0` 判断自动失效，走"全量获取"分支（错误处理）。
    const customer_id =
      typeof props.customerId === "number" &&
      Number.isFinite(props.customerId) &&
      props.customerId > 0
        ? props.customerId
        : undefined;
    const res = await listThirdPartyContracts({
      status: statusFilter.value || undefined,
      customer_type: typeFilter.value || undefined,
      search: searchKeyword.value.trim() || undefined,
      customer_id,
      page: page.value,
      page_size: pageSize.value
    });
    if (res.success) {
      contracts.value = res.data.list || [];
      customerMap.value = res.data.customer_map || {};
      total.value = res.data.total;
    } else {
      ElMessage.error("加载失败");
    }
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "请求失败");
  } finally {
    loading.value = false;
  }
}

// ========== 对方列显示（来自联表 customer_map） ==========
// 优先级：企业公司名 > 个人真实名 > 电话 > id
function displayCustomer(row: ThirdPartyContract): string {
  const c = customerMap.value[String(row.customer_id)];
  if (!c) return `客户 #${row.customer_id}`;
  return c.company_name || c.real_name || c.phone || `客户 #${c.id}`;
}
function customerPhone(row: ThirdPartyContract): string {
  return customerMap.value[String(row.customer_id)]?.phone || "";
}
type CustomerTypeTag = "warning" | "success" | "info";
function customerTypeTag(row: ThirdPartyContract): { type: CustomerTypeTag; label: string } {
  const c = customerMap.value[String(row.customer_id)];
  if (!c) return { type: "info", label: "—" };
  return c.customer_type === "enterprise"
    ? { type: "warning", label: "企业" }
    : { type: "success", label: "个人" };
}

onMounted(loadList);

function handleSearch() {
  page.value = 1;
  loadList();
}
function handleStatusChange() {
  page.value = 1;
  loadList();
}
function handleTypeChange() {
  page.value = 1;
  loadList();
}
function handlePageChange(p: number) {
  page.value = p;
  loadList();
}
function handleSizeChange(s: number) {
  pageSize.value = s;
  page.value = 1;
  loadList();
}

// ========== 状态机操作 ==========
async function handleStatus(row: ThirdPartyContract, to: string) {
  if (!canTPTransition(row.status, to)) {
    ElMessage.warning(`非法状态转换：${row.status} → ${to}`);
    return;
  }
  const labels: Record<string, string> = {
    pending: "提交",
    signed: "标记已签",
    archived: "归档",
    cancelled: "取消"
  };
  try {
    await ElMessageBox.confirm(
      `确认「${labels[to]}」「${row.title}」？`,
      `${labels[to]}确认`,
      { type: "info" }
    );
  } catch {
    return;
  }
  const res: any = await changeThirdPartyContractStatus(row.id, to);
  if (res.success) {
    ElMessage.success("状态已更新");
    loadList();
  } else {
    ElMessage.error(res.message || "状态更新失败");
  }
}

// ========== 删除 ==========
async function handleDelete(row: ThirdPartyContract) {
  if (!canTPDelete(row.status)) {
    ElMessage.warning("只有草稿 / 已取消状态可删除");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确认删除「${row.title}」？删除后无法恢复。`,
      "删除确认",
      { type: "warning", confirmButtonText: "确定删除" }
    );
  } catch {
    return;
  }
  const res: any = await deleteThirdPartyContract(row.id);
  if (res.success) {
    ElMessage.success("已删除");
    loadList();
  } else {
    ElMessage.error(res.message || "删除失败");
  }
}

// ========== 详情 / 下载 ==========
function handleView(row: ThirdPartyContract) {
  router.push(`/contract/third-party-detail?thirdPartyId=${row.id}`);
}

// ==================== 第十三阶段 v4：提醒订阅入口（列表层） ====================
const reminderDialogVisible = ref(false);
const reminderLinkID = ref<number | null>(null);
function openReminderDialog(row: ThirdPartyContract) {
  if (!row.id || row.id <= 0) {
    ElMessage.warning("文档 ID 无效");
    return;
  }
  reminderLinkID.value = row.id;
  reminderDialogVisible.value = true;
}
function closeReminderDialog() {
  reminderDialogVisible.value = false;
  reminderLinkID.value = null;
}

async function handleDownload(row: ThirdPartyContract) {
  if (!row.file_path || row.file_size <= 0) {
    ElMessage.warning("该合同尚未上传 PDF 文件");
    return;
  }
  try {
    const blob = (await downloadThirdPartyContractPdf(row.id)) as Blob;
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${row.contract_no}.pdf`;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
    ElMessage.success("下载成功");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "下载失败");
  }
}

// ========== 第十阶段 5% 收尾：批量下载（ZIP） ==========
// 后端 BulkDownload 端点已存在（POST /api/third-party/contracts/bulk-download），
// 此处仅补齐 UI：selection 列 + 工具栏 + 下载触发。
const tableRef = ref<any>(null);
const multipleSelection = ref<ThirdPartyContract[]>([]);
const bulkDownloading = ref(false);

function handleSelectionChange(rows: ThirdPartyContract[]) {
  multipleSelection.value = rows;
}
function clearSelection() {
  multipleSelection.value = [];
  // el-table 的 clearSelection 需要通过 ref 调用
  if (tableRef.value && typeof tableRef.value.clearSelection === "function") {
    tableRef.value.clearSelection();
  }
}
function selectableForDownload(row: ThirdPartyContract) {
  // 未上传 PDF 的行不能勾选（后端 BulkDownload 也会跳过）
  return !!(row.file_path && row.file_size > 0);
}

async function handleBulkDownload() {
  if (multipleSelection.value.length === 0) {
    ElMessage.warning("请先勾选要下载的合同");
    return;
  }
  // 前置校验：避免任何 race 漏网（理论上 selectable 已过滤）
  const ids: number[] = [];
  for (const r of multipleSelection.value) {
    if (r.file_path && r.file_size > 0) {
      ids.push(r.id);
    }
  }
  if (ids.length === 0) {
    ElMessage.warning("所选合同均未上传 PDF，无法打包");
    return;
  }
  bulkDownloading.value = true;
  try {
    const blob = (await bulkDownloadThirdPartyContracts({ ids })) as Blob;
    if (!blob || blob.size === 0) {
      ElMessage.error("服务端返回空 ZIP");
      return;
    }
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `third_party_${new Date().toISOString().replace(/[-:T]/g, "").slice(0, 15)}.zip`;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
    ElMessage.success(`已下载 ${ids.length} 个合同（共 ${(blob.size / 1024).toFixed(1)} KB）`);
    clearSelection();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "批量下载失败");
  } finally {
    bulkDownloading.value = false;
  }
}

// ========== 状态 tag 颜色映射 ==========
function statusType(s: string) {
  return TP_STATUS_OPTIONS.find(o => o.value === s)?.type || "info";
}
function statusLabel(s: string) {
  return TP_STATUS_OPTIONS.find(o => o.value === s)?.label || s;
}

// ========== 类型 tag ==========
const TYPE_LABELS: Record<string, string> = {
  paper: "纸质",
  electronic: "电子"
};
function typeLabel(t: string) {
  return TYPE_LABELS[t] || t;
}

// ========== 状态机按钮组（按当前状态显示合法下一态） ==========
function nextStatusOptions(s: string) {
  const allowed: Record<string, string[]> = {
    draft: ["pending", "cancelled"],
    pending: ["signed", "cancelled"],
    signed: ["archived"],
    archived: [],
    cancelled: []
  };
  return (allowed[s] || []).map(v => ({ value: v, label: statusLabel(v) }));
}
</script>

<template>
  <div class="third-party-list-container">
    <div class="list-header">
      <span class="list-title"></span>
      <div class="header-actions">
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
          @change="handleTypeChange"
        >
          <el-option label="个人" value="individual" />
          <el-option label="企业" value="enterprise" />
        </el-select>
        <el-select
          v-model="statusFilter"
          placeholder="状态"
          style="width: 110px; margin-right: 10px"
          clearable
          @change="handleStatusChange"
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
        <el-button @click="loadList">
          <i class="ri-refresh-line" style="margin-right: 4px"></i>
          刷新
        </el-button>
      </div>
    </div>

    <!-- 第十阶段 5% 收尾：批量下载工具栏（仅选中时显示） -->
    <transition name="el-fade-in-linear">
      <div v-if="multipleSelection.length > 0" class="bulk-bar">
        <div class="bulk-bar-left">
          <i class="ri-checkbox-multiple-line" style="font-size: 16px; color: #409eff"></i>
          <span>已选 <b class="bulk-count">{{ multipleSelection.length }}</b> 项</span>
        </div>
        <div class="bulk-bar-right">
          <el-button
            type="primary"
            :loading="bulkDownloading"
            @click="handleBulkDownload"
          >
            <i class="ri-download-2-line" style="margin-right: 4px"></i>
            批量下载 ZIP
          </el-button>
          <el-button text @click="clearSelection">
            <i class="ri-close-line" style="margin-right: 4px"></i>
            清空选择
          </el-button>
        </div>
      </div>
    </transition>

    <el-table
      ref="tableRef"
      v-loading="loading"
      :data="contracts"
      border
      style="width: 100%"
      @selection-change="handleSelectionChange"
    >
        <el-table-column
          type="selection"
          width="50"
          align="center"
          :selectable="selectableForDownload"
        />
        <el-table-column
          prop="id"
          label="ID"
          width="80"
          align="center"
        />
        <el-table-column
          prop="contract_no"
          label="合同号"
          width="180"
          align="center"
        />
        <el-table-column
          prop="title"
          label="标题"
          min-width="180"
          align="center"
          show-overflow-tooltip
        />
        <el-table-column label="类型" width="80" align="center">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">
              {{ typeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="客户类型" width="90" align="center">
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="customerTypeTag(row).type"
              effect="plain"
            >
              {{ customerTypeTag(row).label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="客户" min-width="140" align="center">
          <template #default="{ row }">
            {{ displayCustomer(row) }}
          </template>
        </el-table-column>
        <el-table-column label="电话" width="140" align="center">
          <template #default="{ row }">
            <span v-if="customerPhone(row)">{{ customerPhone(row) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="金额" width="140" align="center">
          <template #default="{ row }">
            {{ row.amount.toFixed(2) }} {{ row.currency }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="PDF" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.file_size > 0" type="success" size="small">已上传</el-tag>
            <el-tag v-else type="warning" size="small">未上传</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180" align="center">
          <template #default="{ row }">
            {{ formatTimestampLang(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleDownload(row)">
              下载
            </el-button>
            <el-button link type="primary" size="small" @click="handleView(row)">
              查看
            </el-button>
            <el-button link type="warning" size="small" @click="openReminderDialog(row)">
              提醒
            </el-button>
            <el-button
              v-if="canTPDelete(row.status)"
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

      <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="handlePageChange"
        @size-change="handleSizeChange"
      />
    </div>

    <!-- 第十三阶段 v4：提醒订阅 Dialog（列表层入口） -->
    <SubscriptionDialog
      v-model:visible="reminderDialogVisible"
      link-type="third_party_contract"
      :link-id="reminderLinkID || 0"
      @created="closeReminderDialog"
    />
  </div>
</template>

<style scoped>
.third-party-list-container {
  /* 直接渲染在父级 el-tab-pane，不套 el-card */
}
.list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.list-title {
  font-weight: 600;
  font-size: 15px;
}
.header-actions {
  display: flex;
  align-items: center;
}
.pagination-wrapper {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
:deep(.el-table) {
  font-size: 14px;
}
:deep(.el-button + .el-button) {
  margin-left: 8px;
}
.customer-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: center;
  flex-wrap: wrap;
}

/* 第十阶段 5% 收尾：批量下载工具栏 */
.bulk-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  margin-bottom: 12px;
  background: linear-gradient(90deg, #ecf5ff 0%, #f5f7fa 100%);
  border: 1px solid #d9ecff;
  border-radius: 6px;
  box-shadow: 0 1px 4px rgba(64, 158, 255, 0.08);
}
.bulk-bar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: #303133;
}
.bulk-count {
  color: #409eff;
  font-size: 16px;
  font-weight: 600;
  margin: 0 2px;
}
.bulk-bar-right {
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
