<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Timer, Bell } from "@element-plus/icons-vue";
import { useUserStore } from "@/store/modules/user";
import {
  listReminderTemplates,
  createReminderTemplate,
  updateReminderTemplate,
  deleteReminderTemplate,
  listReminderLogs,
  triggerReminderScan,
  type ReminderTemplate,
  type ReminderLog
} from "@/api/reminder";
import { formatTimestampLang } from "@/utils/date";
import { hasPerms } from "@/utils/auth";
import SubscriptionTable from "./components/SubscriptionTable.vue";
import SubscriptionDialog from "./components/SubscriptionDialog.vue";

defineOptions({ name: "ReminderManagement" });

const userStore = useUserStore();
const isAdmin = computed(() => userStore.roles?.includes("admin") ?? false);

// 规则类型下拉选项（v1 仅支持 contract_expiring；v2 扩展时加新选项）
const RULE_TYPE_OPTIONS: { value: string; label: string; description: string }[] = [
  {
    value: "contract_expiring",
    label: "合同到期前 N 天（v1）",
    description: "合同 end_date 之前 N 天提醒（advance_days）"
  },
  // v2 候选：
  // { value: "contract_overdue", label: "合同已过期 N 天" },
  // { value: "customer_birthday", label: "客户生日前 N 天" },
  // { value: "contract_renewal", label: "续签前 N 天" },
];

// ==================== Tab 状态 ====================
const activeTab = ref<"templates" | "subscriptions" | "logs">("templates");

// ==================== 模板 Tab ====================
const templateList = ref<ReminderTemplate[]>([]);
const templateLoading = ref(false);
const templateDialogVisible = ref(false);
const templateEditing = ref<ReminderTemplate | null>(null);
const templateForm = ref<{
  template_key: string;
  name: string;
  description: string;
  rule_type: string;
  advance_days: number;
  is_active: boolean;
  sort_order: number;
}>({
  template_key: "",
  name: "",
  description: "",
  rule_type: "contract_expiring",
  advance_days: 30,
  is_active: true,
  sort_order: 0
});

async function loadTemplates() {
  templateLoading.value = true;
  try {
    const res: any = await listReminderTemplates();
    if (res.success) templateList.value = res.data.list || [];
    else ElMessage.error("加载模板失败");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  } finally {
    templateLoading.value = false;
  }
}

function handleNewTemplate() {
  // 2026-06-25 P1-7.2：按钮已用 v-perms="'reminder:templates:create'" 隐藏，
  // 此处保留防御性检查（防止控制台绕过 UI）
  if (!hasPerms("reminder:templates:create")) {
    ElMessage.warning("无权限：缺少 reminder:templates:create");
    return;
  }
  templateEditing.value = null;
  templateForm.value = {
    template_key: "",
    name: "",
    description: "",
    rule_type: "contract_expiring",
    advance_days: 30,
    is_active: true,
    sort_order: 0
  };
  templateDialogVisible.value = true;
}

function handleEditTemplate(row: ReminderTemplate) {
  // 2026-06-25 P1-7.2：按钮已用 v-perms="'reminder:templates:update'" 隐藏
  if (!hasPerms("reminder:templates:update")) {
    ElMessage.warning("无权限：缺少 reminder:templates:update");
    return;
  }
  if (row.is_system) {
    ElMessage.warning("系统预置模板不可修改");
    return;
  }
  templateEditing.value = row;
  templateForm.value = {
    template_key: row.template_key,
    name: row.name,
    description: row.description,
    rule_type: row.rule_type,
    advance_days: row.advance_days,
    is_active: row.is_active,
    sort_order: row.sort_order
  };
  templateDialogVisible.value = true;
}

async function handleSaveTemplate() {
  if (!templateForm.value.template_key.trim() || !templateForm.value.name.trim()) {
    ElMessage.warning("template_key 和 name 必填");
    return;
  }
  if (templateForm.value.advance_days < 0 || templateForm.value.advance_days > 365) {
    ElMessage.warning("advance_days 应在 [0, 365]");
    return;
  }
  try {
    if (templateEditing.value) {
      const res: any = await updateReminderTemplate(templateEditing.value.id, templateForm.value);
      if (res.success) ElMessage.success("已更新");
      else ElMessage.error(res.message || "更新失败");
    } else {
      const res: any = await createReminderTemplate(templateForm.value);
      if (res.success) ElMessage.success("已创建");
      else ElMessage.error(res.message || "创建失败");
    }
    templateDialogVisible.value = false;
    loadTemplates();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "保存失败");
  }
}

async function handleDeleteTemplate(row: ReminderTemplate) {
  // 2026-06-25 P1-7.2：按钮已用 v-perms="'reminder:templates:delete'" 隐藏
  if (!hasPerms("reminder:templates:delete")) {
    ElMessage.warning("无权限：缺少 reminder:templates:delete");
    return;
  }
  if (row.is_system) {
    ElMessage.warning("系统预置模板不可删除");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确认删除模板「${row.name}」？\n\n注意：仅当该模板下无启用订阅时才允许删除。`,
      "删除模板",
      { type: "warning", confirmButtonText: "确定删除", cancelButtonText: "取消" }
    );
  } catch {
    return;
  }
  try {
    const res: any = await deleteReminderTemplate(row.id);
    if (res.success) {
      ElMessage.success("已删除");
      loadTemplates();
    } else {
      ElMessage.error(res.message || "删除失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "删除失败");
  }
}

// ==================== 订阅 Tab ====================
// 第十二阶段重构：订阅 Tab 改用公共组件 SubscriptionTable
// 第十三阶段 v4：SubscriptionTable 通过 emit('create') 通知父组件打开 Dialog
const createSubDialogVisible = ref(false);
function handleOpenCreate() {
  createSubDialogVisible.value = true;
}
function onSubCreated() {
  createSubDialogVisible.value = false;
  // 提示：列表由 SubscriptionTable 自身管理（emit refresh），父组件无需手动刷新
}

// ==================== 日志 Tab ====================
const logList = ref<ReminderLog[]>([]);
const logLoading = ref(false);
const logTotal = ref(0);
const logPage = ref(1);
const logPageSize = ref(20);
const logFilter = ref<{ trigger_date?: string; triggered_by?: string; delivery_status?: string }>({});

async function loadLogs() {
  logLoading.value = true;
  try {
    const res: any = await listReminderLogs({
      ...logFilter.value,
      page: logPage.value,
      page_size: logPageSize.value
    });
    if (res.success) {
      logList.value = res.data.list || [];
      logTotal.value = res.data.total;
    } else {
      ElMessage.error("加载日志失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  } finally {
    logLoading.value = false;
  }
}

// ==================== 立即扫描 ====================
const scanning = ref(false);
async function handleTriggerScan() {
  // 2026-06-25 P1-7.2：按钮已用 v-perms="'reminder:scan'" 隐藏
  if (!hasPerms("reminder:scan")) {
    ElMessage.warning("无权限：缺少 reminder:scan");
    return;
  }
  try {
    await ElMessageBox.confirm(
      "立即执行一次提醒扫描？\n\n将扫描所有 active 订阅，命中规则后通过站内信通知。",
      "手动扫描",
      { type: "info", confirmButtonText: "开始扫描", cancelButtonText: "取消" }
    );
  } catch {
    return;
  }
  scanning.value = true;
  try {
    const res: any = await triggerReminderScan();
    if (res.success) {
      const s = res.data.stats;
      ElMessage.success(
        `扫描完成：模板 ${s.templates} / 订阅 ${s.subscriptions} / 扫描 ${s.contracts_scanned} / 命中 ${s.matched} / 发送 ${s.sent} / 跳过 ${s.skipped} / 失败 ${s.failed}`
      );
      if (activeTab.value === "logs") loadLogs();
    } else {
      ElMessage.error(res.message || "扫描失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "扫描失败");
  } finally {
    scanning.value = false;
  }
}

function formatDaysBefore(n: number) {
  if (n === 0) return "今天到期";
  if (n === 1) return "明天到期";
  return `${n} 天后到期`;
}

onMounted(() => {
  loadTemplates();
});
</script>

<template>
  <div class="reminder-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><Bell /></el-icon>
            <span>提醒管理</span>
          </div>
          <div>
            <!-- 优化 3：顶部"+ 新建订阅"按钮（与"立即扫描"并列） -->
            <el-button
              v-if="activeTab === 'subscriptions'"
              type="success"
              :icon="undefined as any"
              @click="handleOpenCreate"
            >
              新建订阅
            </el-button>
            <el-button
              v-perms="'reminder:scan'"
              type="primary"
              :icon="Timer"
              :loading="scanning"
              @click="handleTriggerScan"
            >
              立即扫描
            </el-button>
          </div>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
      >
        <template #title>
          提醒业务采用"模板 + 订阅"分层模式：模板预置规则（如"合同到期前 30 天"），客户/合同订阅适用模板。
          扫描器每天 09:00 自动运行（scheduler），命中规则后通过站内信通知。
        </template>
      </el-alert>

      <el-tabs v-model="activeTab">
        <!-- 模板 Tab -->
        <el-tab-pane label="提醒模板" name="templates">
          <div style="margin-bottom: 12px">
            <el-button v-perms="'reminder:templates:create'" type="primary" @click="handleNewTemplate">
              + 新建模板
            </el-button>
          </div>
          <el-table :data="templateList" v-loading="templateLoading" stripe>
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column label="名称" min-width="180">
              <template #default="{ row }">
                <div style="font-weight: 600">{{ row.name }}</div>
                <div style="color: #999; font-size: 12px"><code>{{ row.template_key }}</code></div>
              </template>
            </el-table-column>
            <el-table-column label="规则" width="140">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.rule_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="advance_days" label="提前天数" width="100" align="center" />
            <el-table-column label="类型" width="80" align="center">
              <template #default="{ row }">
                <el-tag v-if="row.is_system" type="info" size="small">系统</el-tag>
                <el-tag v-else size="small" effect="plain">业务</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="启用" width="80" align="center">
              <template #default="{ row }">
                <el-tag :type="row.is_active ? 'success' : 'info'" size="small">
                  {{ row.is_active ? "是" : "否" }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="sort_order" label="排序" width="70" align="center" />
            <el-table-column label="操作" width="100" fixed="right" align="center">
              <template #default="{ row }">
                <el-button
                  v-perms="'reminder:templates:update'"
                  link
                  type="primary"
                  size="small"
                  :disabled="row.is_system"
                  @click="handleEditTemplate(row)"
                >
                  编辑
                </el-button>
                <el-button
                  v-perms="'reminder:templates:delete'"
                  link
                  type="danger"
                  size="small"
                  :disabled="row.is_system"
                  @click="handleDeleteTemplate(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- 订阅 Tab -->
        <el-tab-pane label="订阅管理" name="subscriptions">
          <SubscriptionTable @create="handleOpenCreate" />
        </el-tab-pane>

        <!-- 日志 Tab -->
        <el-tab-pane label="发送日志" name="logs">
          <el-table :data="logList" v-loading="logLoading" stripe>
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="trigger_date" label="触发日期" width="120" />
            <el-table-column label="合同" min-width="200">
              <template #default="{ row }">
                <div>{{ row.contract_title }}</div>
                <div style="color: #999; font-size: 12px">{{ row.contract_no }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="customer_name" label="客户" min-width="120" />
            <el-table-column label="提前天数" width="120" align="center">
              <template #default="{ row }">
                <el-tag :type="row.days_before === 0 ? 'danger' : (row.days_before === 1 ? 'warning' : 'info')" size="small">
                  {{ formatDaysBefore(row.days_before) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="row.delivery_status === 'sent' ? 'success' : 'danger'" size="small">
                  {{ row.delivery_status === "sent" ? "已发送" : "失败" }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="触发方式" width="100" align="center">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">
                  {{ row.triggered_by === "scheduler" ? "定时" : "手动" }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="170">
              <template #default="{ row }">
                {{ formatTimestampLang(row.created_at) }}
              </template>
            </el-table-column>
          </el-table>
          <div style="margin-top: 12px; text-align: right">
            <el-pagination
              v-model:current-page="logPage"
              v-model:page-size="logPageSize"
              :total="logTotal"
              layout="total, sizes, prev, pager, next"
              :page-sizes="[10, 20, 50, 100]"
              @current-change="loadLogs"
              @size-change="loadLogs"
            />
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 模板编辑对话框 -->
    <el-dialog
      v-model="templateDialogVisible"
      :title="templateEditing ? '编辑模板' : '新建模板'"
      width="600px"
      :close-on-click-modal="false"
    >
      <el-form label-width="120px">
        <el-form-item label="template_key" required>
          <el-input
            v-model="templateForm.template_key"
            :disabled="!!templateEditing"
            placeholder="蛇形命名，如 contract_expiring_30d"
          />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="templateForm.name" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="templateForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="规则类型">
          <el-select v-model="templateForm.rule_type" placeholder="选择规则类型" style="width: 100%">
            <el-option
              v-for="opt in RULE_TYPE_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="提前天数" required>
          <el-input-number v-model="templateForm.advance_days" :min="0" :max="365" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="templateForm.is_active" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="templateForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="templateDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveTemplate">保存</el-button>
      </template>
    </el-dialog>

    <!-- 第十三阶段 v4：父组件提供"新建订阅" Dialog（由 SubscriptionTable emit('create') 触发） -->
    <SubscriptionDialog
      v-model:visible="createSubDialogVisible"
      @created="onSubCreated"
    />
  </div>
</template>

<style scoped>
.reminder-container {
  padding: 16px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.card-title {
  display: flex;
  gap: 8px;
  align-items: center;
  font-weight: 600;
  font-size: 16px;
}
</style>
