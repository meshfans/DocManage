<template>
  <div class="scheduled-task-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><Timer /></el-icon>
            <span>定时任务管理</span>
          </div>
          <div>
            <el-input
              v-model="filterKeyword"
              placeholder="搜索任务名/Key"
              clearable
              style="width: 200px; margin-right: 8px"
              @keyup.enter="loadList"
              @clear="loadList"
            />
            <el-select
              v-model="filterType"
              placeholder="类型"
              clearable
              style="width: 110px; margin-right: 8px"
              @change="loadList"
            >
              <el-option label="内置" value="built_in" />
              <el-option label="业务" value="business" />
            </el-select>
            <el-select
              v-model="filterActive"
              placeholder="状态"
              clearable
              style="width: 110px; margin-right: 8px"
              @change="loadList"
            >
              <el-option label="启用" value="1" />
              <el-option label="停用" value="0" />
            </el-select>
            <el-button @click="loadList">查询</el-button>
            <el-button type="primary" @click="handleCreate">新建任务</el-button>
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
          5 段 cron：分 时 日 月 周（如 <code>0 2 * * *</code> 每日凌晨 2 点）<br />
          6 段 cron：秒 分 时 日 月 周（如 <code>0 0 9 * * *</code> 每日 9 点整）
        </template>
      </el-alert>

      <el-table :data="filteredList" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="任务" min-width="200">
          <template #default="{ row }">
            <div style="font-weight: 600">{{ row.name }}</div>
            <div style="color: #999; font-size: 12px">
              <code>{{ row.task_key }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="taskTypeMap[row.task_type] || ''">
              {{ row.task_type === "built_in" ? "内置" : "业务" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Cron" min-width="160">
          <template #default="{ row }">
            <code style="font-size: 12px">{{ row.cron_expr }}</code>
          </template>
        </el-table-column>
        <el-table-column label="Handler" min-width="140">
          <template #default="{ row }">
            <code style="font-size: 12px">{{ row.handler_name }}</code>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-switch
              v-model="row.is_active"
              :disabled="!canEdit(row)"
              @change="(val: boolean) => handleToggle(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column label="上次执行" width="180">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTypeMap[row.last_status] || 'info'">
              {{ statusLabel(row.last_status) }}
            </el-tag>
            <div style="color: #999; font-size: 12px; margin-top: 4px">
              {{ formatUnix(row.last_run_at) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="下次执行" width="170">
          <template #default="{ row }">
            <span v-if="row.next_run_at > 0" style="font-size: 12px">
              {{ formatUnix(row.next_run_at) }}
            </span>
            <span v-else style="color: #ccc">-</span>
          </template>
        </el-table-column>
        <el-table-column label="成功/失败" width="100">
          <template #default="{ row }">
            <span style="color: #67c23a">{{ row.run_count }}</span>
            <span style="color: #999"> / </span>
            <span style="color: #f56c6c">{{ row.fail_count }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleViewLogs(row)">
              日志
            </el-button>
            <el-button link type="primary" size="small" @click="handleViewAudits(row)">
              审计
            </el-button>
            <el-button link type="primary" size="small" @click="handleRunNow(row)">
              立即执行
            </el-button>
            <el-button link type="primary" size="small" @click="handleEdit(row)">
              编辑
            </el-button>
            <el-button
              v-if="row.task_type === 'business'"
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
    </el-card>

    <!-- 编辑/新建弹窗 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="640px"
      @close="resetForm"
    >
      <el-form :model="formData" label-width="100px" :rules="formRules" ref="formRef">
        <el-alert
          v-if="isEdit && isBuiltIn"
          type="warning"
          :closable="false"
          show-icon
          style="margin-bottom: 16px"
        >
          <template #title>
            • 正在修改<strong>内置任务</strong>。修改后立即生效（无需重启）：
            <br />• Cron 表达式：变更后下一次触发按新表达式
            <br />• Handler：<strong style="color: #f56c6c">不允许修改</strong>（如需自定义请新建业务任务）
            <br />• 建议先点"测试"按钮确认下次触发时间
          </template>
        </el-alert>

        <el-form-item label="任务Key" prop="task_key">
          <el-input
            v-model="formData.task_key"
            placeholder="例如: contract_daily_remind"
            :disabled="isEdit"
          />
        </el-form-item>

        <el-form-item label="任务名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入任务名称" />
        </el-form-item>

        <el-form-item label="Handler" prop="handler_name">
          <el-select
            v-model="formData.handler_name"
            placeholder="选择已注册的 handler"
            style="width: 100%"
            :disabled="isEdit && isBuiltIn"
            @change="onHandlerChange"
          >
            <el-option
              v-for="h in handlers"
              :key="h.name"
              :label="h.name"
              :value="h.name"
            />
          </el-select>
          <span v-if="isEdit && isBuiltIn" style="color: #999; font-size: 12px">
            内置任务的 Handler 不允许修改
          </span>
        </el-form-item>

        <el-form-item label="Cron 表达式" prop="cron_expr">
          <el-input
            v-model="formData.cron_expr"
            placeholder="5段或6段，如 0 0 2 * * *"
          />
          <div class="cron-actions">
            <el-button
              type="primary"
              link
              :loading="testingCron"
              @click="testCronExpr"
            >
              {{ testingCron ? "计算中..." : "测试" }}
            </el-button>
            <el-dropdown @command="applyCronTemplate" trigger="click">
              <el-button type="primary" link>
                模板
                <el-icon style="margin-left: 2px"><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="0 * * * * *">每分钟</el-dropdown-item>
                  <el-dropdown-item command="0 0 * * * *">每小时</el-dropdown-item>
                  <el-dropdown-item command="0 0 0 * * *">每天 0 点</el-dropdown-item>
                  <el-dropdown-item command="0 0 2 * * *">每天 2 点</el-dropdown-item>
                  <el-dropdown-item command="0 0 9 * * *">每天 9 点</el-dropdown-item>
                  <el-dropdown-item command="0 0 0 * * 1">每周一 0 点</el-dropdown-item>
                  <el-dropdown-item command="0 0 0 1 * *">每月 1 号 0 点</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
          <div v-if="nextRunInfo" style="font-size: 12px; color: #67c23a; margin-top: 4px">
            ✓ 下次触发：<strong>{{ nextRunInfo }}</strong>
          </div>
          <div v-if="cronTestError" style="font-size: 12px; color: #f56c6c; margin-top: 4px">
            ✗ {{ cronTestError }}
          </div>
        </el-form-item>

        <el-form-item label="参数 (JSON)">
          <el-input
            v-model="formData.handler_params"
            type="textarea"
            :rows="2"
            placeholder='例如: {"days": 30}'
          />
        </el-form-item>

        <el-form-item label="描述">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="2"
            placeholder="任务描述（可选）"
          />
        </el-form-item>

        <el-form-item label="超时（秒）">
          <el-input-number
            v-model="formData.timeout_seconds"
            :min="60"
            :max="86400"
            :step="60"
          />
        </el-form-item>

        <el-form-item v-if="!isEdit" label="允许并发">
          <el-switch v-model="formData.is_concurrent" />
          <span style="color: #999; margin-left: 8px; font-size: 12px">
            默认串行防重入：上次未结束则跳过本次
          </span>
        </el-form-item>

        <el-form-item v-if="isEdit && isBuiltIn" label="启用">
          <el-switch v-model="formData.is_active" />
          <span style="color: #999; margin-left: 8px; font-size: 12px">
            内置任务：可改描述、Cron、超时与启停（Handler 锁定）
          </span>
        </el-form-item>

        <el-form-item v-if="isEdit && !isBuiltIn" label="启用">
          <el-switch v-model="formData.is_active" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">
          确定
        </el-button>
      </template>
    </el-dialog>

    <!-- 日志抽屉 -->
    <el-drawer
      v-model="logsVisible"
      :title="`执行日志：${currentTask?.name || ''}`"
      size="780px"
      direction="rtl"
    >
      <el-table :data="logs" v-loading="logsLoading" stripe size="small">
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTypeMap[row.status] || 'info'">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="触发方式" width="90">
          <template #default="{ row }">
            {{ row.triggered_by === "manual" ? "手动" : "调度" }}
          </template>
        </el-table-column>
        <el-table-column label="开始时间" width="160">
          <template #default="{ row }">
            <span style="font-size: 12px">{{ formatUnix(row.started_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="80">
          <template #default="{ row }">
            <span v-if="row.duration_ms > 0">{{ row.duration_ms }} ms</span>
            <span v-else style="color: #ccc">-</span>
          </template>
        </el-table-column>
        <el-table-column label="输出/错误" min-width="240">
          <template #default="{ row }">
            <div v-if="row.output" style="font-size: 12px; color: #67c23a; white-space: pre-wrap; word-break: break-all">
              {{ row.output }}
            </div>
            <div v-if="row.error" style="font-size: 12px; color: #f56c6c; white-space: pre-wrap; word-break: break-all">
              {{ row.error }}
            </div>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <!-- 审计抽屉 -->
    <el-drawer
      v-model="auditsVisible"
      :title="`变更审计：${currentTask?.name || ''}`"
      size="780px"
      direction="rtl"
    >
      <el-table :data="audits" v-loading="auditsLoading" stripe size="small">
        <el-table-column label="字段" width="140">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ row.field_name }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="原值" min-width="180">
          <template #default="{ row }">
            <code style="font-size: 12px; color: #f56c6c">{{ row.old_value || "(空)" }}</code>
          </template>
        </el-table-column>
        <el-table-column label="→" width="40" align="center">
          <template #default>
            <el-icon><Right /></el-icon>
          </template>
        </el-table-column>
        <el-table-column label="新值" min-width="180">
          <template #default="{ row }">
            <code style="font-size: 12px; color: #67c23a">{{ row.new_value || "(空)" }}</code>
          </template>
        </el-table-column>
        <el-table-column label="操作人" width="120">
          <template #default="{ row }">
            {{ row.operator_name || `ID:${row.operator_id}` }}
          </template>
        </el-table-column>
        <el-table-column label="时间" width="160">
          <template #default="{ row }">
            <span style="font-size: 12px">{{ formatUnix(parseInt(row.created_at) || 0) }}</span>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <!-- Diff 确认弹窗 -->
    <el-dialog v-model="diffVisible" title="确认变更" width="640px">
      <el-alert
        v-if="hasRiskChange"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
      >
        <template #title>
          ⚠️ 检测到<strong>高风险变更</strong>，请确认是否继续
        </template>
      </el-alert>
      <el-descriptions :column="1" border>
        <el-descriptions-item
          v-for="(change, idx) in diffChanges"
          :key="idx"
          :label="change.label"
        >
          <div style="display: flex; align-items: center; gap: 8px">
            <code style="color: #f56c6c; background: #fef0f0; padding: 2px 6px; border-radius: 3px">
              {{ change.oldVal || "(空)" }}
            </code>
            <el-icon><Right /></el-icon>
            <code style="color: #67c23a; background: #f0f9eb; padding: 2px 6px; border-radius: 3px">
              {{ change.newVal || "(空)" }}
            </code>
          </div>
        </el-descriptions-item>
      </el-descriptions>
      <div v-if="diffChanges.length === 0" style="text-align: center; color: #999; padding: 20px">
        无变更
      </div>
      <el-alert
        v-if="!isNewTask && diffChanges.some((c) => c.field === 'cron_expr')"
        type="info"
        :closable="false"
        style="margin-top: 12px"
      >
        <template #title>
          Cron 表达式变更将<strong>立即生效</strong>（无需重启服务）
        </template>
      </el-alert>
      <template #footer>
        <el-button @click="diffVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="confirmSubmit">
          确认修改
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from "element-plus";
import { ArrowDown, Timer, Right } from "@element-plus/icons-vue";
import {
  listTasks,
  getTaskLogs,
  createTask,
  updateTask,
  toggleTask,
  deleteTask,
  runNow,
  listHandlers,
  getNextRunTime,
  getTaskAudits,
  formatUnix,
  statusTypeMap,
  taskTypeMap,
  type ScheduledTask,
  type ScheduledTaskLog,
  type ScheduledTaskAudit,
  type HandlerInfo,
} from "@/api/scheduledTask";

// ==================== 状态 ====================
const loading = ref(false);
const list = ref<ScheduledTask[]>([]);
const filterKeyword = ref("");
const filterType = ref<string>("");
const filterActive = ref<string>("");

const dialogVisible = ref(false);
const isEdit = ref(false);
const isBuiltIn = ref(false);
const currentId = ref(0);
const submitting = ref(false);
const formRef = ref<FormInstance>();

const handlers = ref<HandlerInfo[]>([]);
const initialForm = () => ({
  task_key: "",
  name: "",
  description: "",
  handler_name: "",
  handler_params: "{}",
  cron_expr: "0 0 2 * * *",
  is_active: true,
  is_concurrent: false,
  timeout_seconds: 3600,
});
const formData = ref(initialForm());

const formRules: FormRules = {
  task_key: [{ required: true, message: "请输入任务Key", trigger: "blur" }],
  name: [{ required: true, message: "请输入任务名称", trigger: "blur" }],
  handler_name: [{ required: true, message: "请选择 Handler", trigger: "change" }],
  cron_expr: [
    { required: true, message: "请输入 cron 表达式", trigger: "blur" },
    {
      validator: (_rule, value, callback) => {
        if (!value) return callback();
        const fields = value.trim().split(/\s+/);
        if (fields.length !== 5 && fields.length !== 6) {
          callback(new Error("cron 表达式必须是 5 段或 6 段"));
        } else {
          callback();
        }
      },
      trigger: "blur",
    },
  ],
};

// 日志抽屉
const logsVisible = ref(false);
const logsLoading = ref(false);
const logs = ref<ScheduledTaskLog[]>([]);
const currentTask = ref<ScheduledTask | null>(null);

// 审计抽屉
const auditsVisible = ref(false);
const auditsLoading = ref(false);
const audits = ref<ScheduledTaskAudit[]>([]);

// Diff 弹窗
const diffVisible = ref(false);
const isNewTask = ref(false);
const oldData = ref<Record<string, any> | null>(null);
interface DiffChange {
  field: string;
  label: string;
  oldVal: string;
  newVal: string;
}
const diffChanges = ref<DiffChange[]>([]);
const hasRiskChange = computed(() =>
  diffChanges.value.some((c) =>
    c.field === "is_active" ||
    c.field === "handler_name" ||
    c.field === "cron_expr"
  )
);

// Cron 测试
const nextRunInfo = ref("");
const testingCron = ref(false);
const cronTestError = ref("");

// ==================== 计算属性 ====================
const dialogTitle = computed(() => (isEdit.value ? "编辑任务" : "新建任务"));
const filteredList = computed(() => list.value);

// ==================== 工具 ====================
const statusLabel = (s: string) => {
  const map: Record<string, string> = {
    pending: "未执行",
    running: "执行中",
    success: "成功",
    failed: "失败",
    timeout: "超时",
    skipped: "跳过",
  };
  return map[s] || s;
};
const canEdit = (row: ScheduledTask) => row.task_type === "business" || true;
// 内置任务也允许启停；其他字段在 handleSubmit 中做限制

// ==================== 加载 ====================
const loadList = async () => {
  loading.value = true;
  try {
    const res: any = await listTasks({
      keyword: filterKeyword.value || undefined,
      task_type: filterType.value || undefined,
      is_active: filterActive.value || undefined,
    });
    list.value = res.data?.list || [];
  } catch (e) {
    list.value = [];
  } finally {
    loading.value = false;
  }
};

const loadHandlers = async () => {
  try {
    const res: any = await listHandlers();
    handlers.value = res.data || [];
  } catch (e) {
    handlers.value = [];
  }
};

// ==================== 行为 ====================
const handleCreate = () => {
  isEdit.value = false;
  isBuiltIn.value = false;
  isNewTask.value = true;
  currentId.value = 0;
  oldData.value = null;
  formData.value = initialForm();
  dialogVisible.value = true;
  // 清理 diff/test 状态
  diffChanges.value = [];
  nextRunInfo.value = "";
  cronTestError.value = "";
};

const handleEdit = (row: ScheduledTask) => {
  isEdit.value = true;
  isBuiltIn.value = row.task_type === "built_in";
  isNewTask.value = false;
  currentId.value = row.id;
  formData.value = {
    task_key: row.task_key,
    name: row.name,
    description: row.description,
    handler_name: row.handler_name,
    handler_params: row.handler_params || "{}",
    cron_expr: row.cron_expr,
    is_active: row.is_active,
    is_concurrent: row.is_concurrent,
    timeout_seconds: row.timeout_seconds || 3600,
  };
  // 记录原值用于 diff
  oldData.value = { ...formData.value };
  diffChanges.value = [];
  nextRunInfo.value = "";
  cronTestError.value = "";
  dialogVisible.value = true;
};

const handleSubmit = async () => {
  if (!formRef.value) return;
  try {
    await formRef.value.validate();
  } catch {
    return;
  }

  // 计算 diff
  diffChanges.value = computeDiff();
  if (isNewTask.value) {
    // 新建任务：把"所有字段"都视为新增
    diffChanges.value = [
      { field: "name", label: "任务名", oldVal: "", newVal: formData.value.name },
      { field: "task_key", label: "任务Key", oldVal: "", newVal: formData.value.task_key },
      { field: "cron_expr", label: "Cron", oldVal: "", newVal: formData.value.cron_expr },
      { field: "handler_name", label: "Handler", oldVal: "", newVal: formData.value.handler_name },
    ];
  }

  // 弹出 diff 确认
  diffVisible.value = true;
};

// 真正提交（用户点"确认修改"后）
const confirmSubmit = async () => {
  submitting.value = true;
  try {
    if (isEdit.value) {
      // 编辑：只提交变更字段（保留后端字段命名）
      const payload: any = {};
      // 内置任务：handler 不允许改
      const fields: (keyof typeof formData.value)[] = [
        "name", "description", "cron_expr", "handler_params", "is_concurrent", "is_active", "timeout_seconds"
      ];
      for (const f of fields) {
        const newVal = (formData.value as any)[f];
        const oldVal = oldData.value ? (oldData.value as any)[f] : undefined;
        if (oldVal !== undefined && newVal !== oldVal) {
          payload[f] = newVal;
        }
      }
      if (!isBuiltIn.value) {
        const newHandler = formData.value.handler_name;
        const oldHandler = oldData.value?.handler_name;
        if (newHandler !== oldHandler) {
          payload.handler_name = newHandler;
        }
      }
      await updateTask(currentId.value, payload);
      ElMessage.success("已更新，立即生效");
    } else {
      await createTask(formData.value);
      ElMessage.success("已创建");
    }
    diffVisible.value = false;
    dialogVisible.value = false;
    await loadList();
  } catch (e: any) {
    ElMessage.error(e?.message || "操作失败");
  } finally {
    submitting.value = false;
  }
};

// 计算 old → new 的字段变更
const computeDiff = (): DiffChange[] => {
  if (!oldData.value) return [];
  const changes: DiffChange[] = [];
  const fieldLabels: Record<string, string> = {
    name: "任务名",
    description: "描述",
    cron_expr: "Cron 表达式",
    handler_name: "Handler",
    handler_params: "参数",
    is_active: "启用",
    is_concurrent: "允许并发",
    timeout_seconds: "超时（秒）",
  };
  const checks: { key: string; format?: (v: any) => string }[] = [
    { key: "name" },
    { key: "description" },
    { key: "cron_expr" },
    { key: "handler_name" },
    { key: "handler_params" },
    { key: "is_active", format: (v) => (v ? "是" : "否") },
    { key: "is_concurrent", format: (v) => (v ? "是" : "否") },
    { key: "timeout_seconds", format: (v) => String(v) },
  ];
  for (const c of checks) {
    const oldV = (oldData.value as any)[c.key];
    const newV = (formData.value as any)[c.key];
    if (oldV === newV) continue;
    const fmt = c.format || ((v: any) => (v === "" || v === null || v === undefined ? "(空)" : String(v)));
    changes.push({
      field: c.key,
      label: fieldLabels[c.key] || c.key,
      oldVal: fmt(oldV),
      newVal: fmt(newV),
    });
  }
  return changes;
};

// ==================== Cron 测试 ====================
const testCronExpr = async () => {
  if (!formData.value.cron_expr) {
    cronTestError.value = "请先输入 cron 表达式";
    nextRunInfo.value = "";
    return;
  }
  testingCron.value = true;
  cronTestError.value = "";
  nextRunInfo.value = "";
  try {
    const res: any = await getNextRunTime(formData.value.cron_expr);
    nextRunInfo.value = res.data?.next_time || "-";
  } catch (e: any) {
    cronTestError.value = e?.message || "计算失败";
  } finally {
    testingCron.value = false;
  }
};

// ==================== 审计抽屉 ====================
const handleViewAudits = async (row: ScheduledTask) => {
  currentTask.value = row;
  auditsVisible.value = true;
  auditsLoading.value = true;
  try {
    const res: any = await getTaskAudits(row.id, 1, 100);
    audits.value = res.data?.list || [];
  } catch {
    audits.value = [];
  } finally {
    auditsLoading.value = false;
  }
};

const handleToggle = async (row: ScheduledTask, _val: boolean) => {
  try {
    const res: any = await toggleTask(row.id);
    row.is_active = res.data?.is_active ?? _val;
    ElMessage.success(row.is_active ? "已启用" : "已停用");
  } catch (e: any) {
    row.is_active = !_val;
    ElMessage.error(e?.message || "操作失败");
  }
};

const handleDelete = async (row: ScheduledTask) => {
  try {
    await ElMessageBox.confirm(
      `确认删除任务「${row.name}」？删除后无法恢复。`,
      "确认删除",
      { type: "warning" }
    );
  } catch {
    return;
  }
  try {
    await deleteTask(row.id);
    ElMessage.success("已删除");
    await loadList();
  } catch (e: any) {
    ElMessage.error(e?.message || "删除失败");
  }
};

const handleRunNow = async (row: ScheduledTask) => {
  try {
    await ElMessageBox.confirm(
      `确认立即执行任务「${row.name}」？`,
      "立即执行",
      { type: "info" }
    );
  } catch {
    return;
  }
  try {
    await runNow(row.id);
    ElMessage.success("已触发，可点击「日志」查看执行结果");
  } catch (e: any) {
    ElMessage.error(e?.message || "触发失败");
  }
};

const handleViewLogs = async (row: ScheduledTask) => {
  currentTask.value = row;
  logsVisible.value = true;
  logsLoading.value = true;
  try {
    const res: any = await getTaskLogs(row.id, 1, 50);
    logs.value = res.data?.list || [];
  } catch {
    logs.value = [];
  } finally {
    logsLoading.value = false;
  }
};

const applyCronTemplate = (expr: string) => {
  formData.value.cron_expr = expr;
};

const onHandlerChange = (val: string) => {
  if (val) {
    formData.value.handler_name = val;
  }
};

const resetForm = () => {
  formData.value = initialForm();
  isEdit.value = false;
  isBuiltIn.value = false;
  currentId.value = 0;
};

// ==================== 初始化 ====================
onMounted(() => {
  loadHandlers();
  loadList();
});
</script>

<style scoped>
.scheduled-task-container {
  padding: 16px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 15px;
}
.cron-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 6px;
}
code {
  font-family: "Consolas", "Monaco", monospace;
  background: #f5f7fa;
  padding: 1px 4px;
  border-radius: 3px;
  color: #d63384;
}
</style>
