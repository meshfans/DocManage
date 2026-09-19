<script setup lang="ts">
/**
 * 审计日志查询 UI（admin only）
 *
 * 参考 F:\Code\DocManageTrail\frontend\src\views\system\audit.vue 完善：
 *   - AUDIT_DICT 集中字典：12 种 target_type × 分级 tag 色 × 各类型 action 全集
 *   - 统计卡（按类型实时统计本页数量）
 *   - 链状态徽章 + 验证结果 Alert 横幅
 *   - 级联筛选：target_type → action 下拉联动，切类型自动清空不适用 action
 *   - 表格 Tag 着色（target_type / action 均按语义分级）
 *
 * 同时保留 DocManageTrail 原有能力：
 *   - actor_id 筛选 + 时间区间（daterange → unix seconds）
 *   - CSV 导出按筛选全量（翻页最多 100 页 × 200 = 20000 条防护）
 *   - csvEscape RFC 4180 转义（user_agent/detail 含 , " \n 不破坏 CSV）
 *   - 哈希 / detail 字段截断 + tooltip（节省列宽，悬停看完整）
 *   - extractErrorMessage i18n 翻译 reconcile 失败（非断点场景）
 *   - X-Audit-Broken-At header 读断点（Trail 后端 ErrorWithDetail + Header 协议）
 */
import { ref, reactive, onMounted, computed, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Refresh,
  Check,
  Download,
  Histogram
} from "@element-plus/icons-vue";
import dayjs from "dayjs";
import {
  listAudit,
  reconcileAuditChain,
  AUDIT_DICT,
  TARGET_TYPE_OPTIONS,
  type AuditRecord,
  type ListAuditParams,
  type AuditTagType
} from "@/api/audit";
import { useIsAdmin } from "@/composables/useIsAdmin";
import { extractErrorMessage } from "@/utils/error";

defineOptions({ name: "AuditLog" });

const isAdmin = useIsAdmin();

// ==================== 状态 ====================
const loading = ref(false);
const reconciling = ref(false);
const list = ref<AuditRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);

const filters = reactive<{
  target_type: string;
  target_id: number | undefined;
  actor_id: number | undefined;
  action: string;
  dateRange: [string, string] | null;
}>({
  target_type: "",
  target_id: undefined,
  actor_id: undefined,
  action: "",
  dateRange: null
});

// 链验证状态
const chainStatus = ref<{
  ok: boolean | null;
  brokenAt?: number;
  error?: string;
  checkedAt?: number;
}>({ ok: null });

// ==================== 级联：action 下拉随 target_type 变化 ====================
const actionOptions = computed(() => {
  const head = [{ value: "", label: "全部" }];
  const t = filters.target_type;
  if (t && AUDIT_DICT[t]) {
    return [...head, ...AUDIT_DICT[t].actions];
  }
  // 未选类型：聚合所有 action（同名去重 + 来源标注）
  const all: { value: string; label: string }[] = [];
  for (const [tt, def] of Object.entries(AUDIT_DICT)) {
    for (const a of def.actions) {
      all.push({ value: a.value, label: `${a.label}  · ${def.label}` });
    }
  }
  const seen = new Set<string>();
  const uniq = all.filter(a => {
    if (seen.has(a.value)) return false;
    seen.add(a.value);
    return true;
  });
  return [...head, ...uniq];
});

// 切 target_type：如果已选 action 不在新类型的 action 列表里则清空，
// 并重置到第 1 页。刷新由后面的 combined watcher 统一触发（避免重复请求）。
watch(
  () => filters.target_type,
  (newType, oldType) => {
    if (newType === oldType) return;
    if (filters.action === "") {
      page.value = 1;
      return;
    }
    const def = newType ? AUDIT_DICT[newType] : null;
    if (def) {
      const exists = def.actions.some(a => a.value === filters.action);
      if (!exists) filters.action = "";
    }
    // 切回"全部类型"：保留 action（汇总下拉含所有 action）
    page.value = 1;
  }
);

// ==================== 统计卡（本页各类型数量） ====================
const byTypeCount = computed(() => {
  const counts: Record<string, number> = {};
  for (const key of Object.keys(AUDIT_DICT)) counts[key] = 0;
  for (const r of list.value) {
    if (counts[r.target_type] !== undefined) counts[r.target_type]++;
  }
  return counts;
});

// ==================== 链状态派生 ====================
const chainStatusText = computed(() => {
  if (chainStatus.value.ok === null) return "未验证";
  if (chainStatus.value.ok) return "链完整";
  return `链断裂 @ id=${chainStatus.value.brokenAt ?? "?"}`;
});

const chainStatusType = computed<AuditTagType>(() => {
  if (chainStatus.value.ok === null) return "info";
  return chainStatus.value.ok ? "success" : "danger";
});

// ==================== 方法 ====================
function buildParams(): ListAuditParams {
  const p: ListAuditParams = {
    page: page.value,
    page_size: pageSize.value
  };
  if (filters.target_type) p.target_type = filters.target_type;
  if (filters.target_id && filters.target_id > 0) p.target_id = filters.target_id;
  if (filters.actor_id && filters.actor_id > 0) p.actor_id = filters.actor_id;
  if (filters.action) p.action = filters.action;
  if (filters.dateRange && filters.dateRange.length === 2) {
    p.from = dayjs(filters.dateRange[0]).startOf("day").unix();
    p.to = dayjs(filters.dateRange[1]).endOf("day").unix();
  }
  return p;
}

async function fetchList() {
  if (!isAdmin.value) {
    ElMessage.error("当前账号非管理员，无法查看审计日志");
    return;
  }
  loading.value = true;
  try {
    const res = await listAudit(buildParams());
    if (res.success) {
      list.value = res.data?.list ?? [];
      total.value = res.data?.total ?? 0;
    } else {
      list.value = [];
      total.value = 0;
      ElMessage.error("加载审计日志失败: " + (res.message || "未知错误"));
    }
  } catch (e: any) {
    ElMessage.error(extractErrorMessage(e, "zh-CN"));
    console.error("[audit] API 调用失败", e);
  } finally {
    loading.value = false;
  }
}

function onSearch() {
  page.value = 1;
  fetchList();
}

function onReset() {
  filters.target_type = "";
  filters.target_id = undefined;
  filters.actor_id = undefined;
  filters.action = "";
  filters.dateRange = null;
  page.value = 1;
  fetchList();
}

/** 完整拉一遍（不分页）用于 CSV 导出。 */
async function fetchAllForExport(): Promise<AuditRecord[]> {
  const all: AuditRecord[] = [];
  const exportPageSize = 200;
  let curPage = 1;
  // 防御：最多 100 页（20000 条），防止误调用把内存打爆
  for (let i = 0; i < 100; i++) {
    const p = buildParams();
    p.page = curPage;
    p.page_size = exportPageSize;
    const res = await listAudit(p);
    if (!res.success) break;
    const items = res.data?.list ?? [];
    all.push(...items);
    if (items.length < exportPageSize) break;
    curPage += 1;
  }
  return all;
}

/**
 * RFC 4180 CSV 字段转义：
 * 含 " , \r \n 必须用双引号包裹，内部 " 替换为 "" 。
 */
function csvEscape(value: unknown): string {
  if (value === null || value === undefined) return "";
  const s = String(value);
  if (/[",\r\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

async function exportCsv() {
  try {
    const rows = await fetchAllForExport();
    if (rows.length === 0) {
      ElMessage.warning("当前筛选下无数据可导出");
      return;
    }
    const headers = [
      "id",
      "target_type",
      "target_type_label",
      "target_id",
      "action",
      "actor_id",
      "actor_ip",
      "user_agent",
      "detail",
      "hash_sm3",
      "prev_hash",
      "created_at"
    ];
    const csvLines = [headers.map(csvEscape).join(",")];
    for (const r of rows) {
      csvLines.push(
        [
          r.id,
          r.target_type,
          targetTypeLabel(r.target_type),
          r.target_id,
          r.action,
          r.actor_id,
          r.actor_ip ?? "",
          r.user_agent ?? "",
          r.detail ?? "",
          r.hash_sm3,
          r.prev_hash,
          r.created_at
        ]
          .map(csvEscape)
          .join(",")
      );
    }
    // BOM 让 Excel 正确识别 UTF-8
    const blob = new Blob(["\ufeff" + csvLines.join("\n")], {
      type: "text/csv;charset=utf-8"
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `audit_${dayjs().format("YYYYMMDD_HHmmss")}.csv`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    ElMessage.success(`已导出 ${rows.length} 条审计记录`);
  } catch (e: any) {
    ElMessage.error("导出失败: " + (e?.message || e));
  }
}

/** 触发哈希链验证（admin only）。 */
async function onReconcile() {
  try {
    await ElMessageBox.confirm(
      "将遍历全表逐行重算 hash，大数据量下耗时较长，确认执行？",
      "哈希链验证",
      {
        confirmButtonText: "开始验证",
        cancelButtonText: "取消",
        type: "warning"
      }
    );
  } catch {
    return;
  }
  reconciling.value = true;
  try {
    const res = await reconcileAuditChain();
    if (res.success && res.data?.ok) {
      chainStatus.value = { ok: true, checkedAt: Date.now() };
      ElMessage.success("审计链验证通过 ✓");
    } else {
      // 极少数 2xx 但 ok=false 的兜底（后端实际用 5xx + Header）
      chainStatus.value = {
        ok: false,
        brokenAt: (res.data as any)?.broken_at,
        error: (res.data as any)?.error,
        checkedAt: Date.now()
      };
      ElMessage.error(
        `审计链在 id=${(res.data as any)?.broken_at ?? "?"} 处断裂`
      );
    }
  } catch (e: any) {
    // Trail 后端：5xx + X-Audit-Broken-At header + ErrorWithDetail body
    const brokenAt =
      e?.response?.headers?.["x-audit-broken-at"] ||
      e?.response?.headers?.["X-Audit-Broken-At"];
    if (brokenAt) {
      chainStatus.value = {
        ok: false,
        brokenAt: Number(brokenAt),
        error: e?.response?.data?.error || e?.response?.data?.message,
        checkedAt: Date.now()
      };
      ElMessage.error(`审计链在 id=${brokenAt} 处断裂，请检查详情`);
    } else {
      ElMessage.error(extractErrorMessage(e, "zh-CN"));
    }
    console.error("[audit] reconcile failed", e);
  } finally {
    reconciling.value = false;
  }
}

// ==================== 展示工具函数 ====================
function shortHash(h: string) {
  if (!h) return "";
  return h.length > 12 ? h.slice(0, 12) + "…" : h;
}

function prettyDetail(d: string | undefined): string {
  if (!d) return "";
  try {
    // JSON 单行紧凑展示（不再换行；完整内容通过 tooltip 查看）
    return JSON.stringify(JSON.parse(d));
  } catch {
    return d;
  }
}

function fmtTime(t: number): string {
  if (!t) return "";
  return dayjs.unix(t).format("YYYY-MM-DD HH:mm:ss");
}

function fmtCheckedAt(t: number): string {
  if (!t) return "";
  return dayjs(t).format("YYYY-MM-DD HH:mm:ss");
}

function targetTypeTagType(t: string): AuditTagType {
  return AUDIT_DICT[t]?.tag ?? "info";
}

function targetTypeLabel(t: string): string {
  return AUDIT_DICT[t]?.label ?? t;
}

function actionTagType(action: string): AuditTagType {
  // 危险：删除类 / 校验失败
  if (
    action === "delete" ||
    action === "bulk-delete" ||
    action === "pdf_verify_failed" ||
    action === "restore.failed" ||
    action === "manual.failed" ||
    action === "run.failed" ||
    action === "run.timeout" ||
    action === "verify.corrupted" ||
    action === "login.failed" ||
    action === "refresh.failed" ||
    action === "password.change.failed"
  )
    return "danger";
  // 警告：恢复 / 拒绝 / 取消 / 摄像记录 / 校验缺失 / 跳过 / 签名锁定
  if (
    action === "restore" ||
    action === "reject" ||
    action === "cancel" ||
    action === "record" ||
    action === "verify.missing" ||
    action === "run.skipped" ||
    action === "signature.lock" ||
    action === "maintenance.enable" ||
    action === "maintenance.disable"
  )
    return "warning";
  // 信息：查看 / 订阅 / 取消订阅 / 扫描 / 阅读意愿书 / 超时跳过类
  if (
    action === "view" ||
    action === "subscribe" ||
    action === "unsubscribe" ||
    action === "scan"
  )
    return "info";
  // 主题：下载 / 绑定 / 解绑 / 角色分配类
  if (
    action === "download" ||
    action === "bind" ||
    action === "unbind" ||
    action === "assign_roles" ||
    action === "update_roles" ||
    action === "pdf_download" ||
    action === "evidence_export"
  )
    return "primary";
  // 成功：创建 / 上传 / 签 / 锁定 / 验证通过 / 状态变更 / 步骤提交 / 登录成功 等
  return "success";
}

const hasData = computed(() => list.value.length > 0);

// target_type 或 action 变化时回到第 1 页再拉数据。
// 注意：切 target_type 的 watcher 负责清 action + 设 page=1，
// 这里的 combined watcher 统一负责触发请求，避免同一变更触发 2-3 次请求。
watch(
  [() => filters.target_type, () => filters.action],
  () => {
    page.value = 1;
    fetchList();
  }
);

onMounted(fetchList);
</script>

<template>
  <div class="audit-container">
    <el-card shadow="never">
      <!-- 头部：标题 + 链状态 + 操作 -->
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><Histogram /></el-icon>
            <span>审计日志</span>
            <el-tag :type="chainStatusType" size="small" effect="dark">
              {{ chainStatusText }}
            </el-tag>
          </div>
          <div class="card-actions">
            <el-button
              type="warning"
              :icon="Check"
              :loading="reconciling"
              @click="onReconcile"
            >
              验证哈希链
            </el-button>
            <el-button
              :icon="Download"
              :disabled="!hasData"
              @click="exportCsv"
            >
              导出 CSV
            </el-button>
            <el-button :icon="Refresh" @click="fetchList">刷新</el-button>
          </div>
        </div>
      </template>

      <!-- 统计卡（12 种类型 + 本页总数 + 累计总数 = 14 张；紧凑布局） -->
      <div class="stats-row">
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">本页审计</div>
          <div class="stat-value">{{ list.length }}</div>
        </el-card>
        <el-card
          v-for="(def, key) in AUDIT_DICT"
          :key="key"
          class="stat-card"
          shadow="never"
        >
          <div class="stat-label">{{ def.label }} ({{ key }})</div>
          <div
            class="stat-value"
            :class="{
              'stat-primary': def.tag === 'primary',
              'stat-success': def.tag === 'success',
              'stat-warning': def.tag === 'warning',
              'stat-info': def.tag === 'info',
              'stat-danger': def.tag === 'danger'
            }"
          >
            {{ byTypeCount[key] ?? 0 }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">累计总数</div>
          <div class="stat-value">{{ total }}</div>
        </el-card>
      </div>

      <!-- 链验证结果 Alert -->
      <el-alert
        v-if="chainStatus.ok === false"
        type="error"
        :closable="false"
        show-icon
        style="margin: 12px 0"
      >
        <template #title>
          哈希链已断裂：id={{ chainStatus.brokenAt ?? "?" }}，原因：{{
            chainStatus.error ?? "未知"
          }}
        </template>
        建议：立即停服并联系技术负责人取证
      </el-alert>
      <el-alert
        v-else-if="chainStatus.ok === true && chainStatus.checkedAt"
        type="success"
        :closable="false"
        show-icon
        style="margin: 12px 0"
      >
        <template #title>
          哈希链完整 ✓（验证时间：{{ fmtCheckedAt(chainStatus.checkedAt) }}）
        </template>
        本次验证全表逐行重算 SM3 哈希通过
      </el-alert>

      <!-- 筛选区 -->
      <el-form :inline="true" :model="filters" class="audit-filter" @submit.prevent>
        <el-form-item label="目标类型">
          <el-select
            v-model="filters.target_type"
            placeholder="全部类型"
            clearable
            style="width: 200px"
          >
            <el-option
              v-for="opt in TARGET_TYPE_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="目标 ID">
          <el-input-number
            v-model="filters.target_id"
            :min="0"
            placeholder="0=全部"
            controls-position="right"
            style="width: 140px"
            @keyup.enter="onSearch"
          />
        </el-form-item>
        <el-form-item label="操作人 ID">
          <el-input-number
            v-model="filters.actor_id"
            :min="0"
            placeholder="0=全部"
            controls-position="right"
            style="width: 140px"
            @keyup.enter="onSearch"
          />
        </el-form-item>
        <el-form-item label="操作">
          <el-select
            v-model="filters.action"
            placeholder="全部"
            clearable
            filterable
            style="width: 240px"
          >
            <el-option
              v-for="opt in actionOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="时间区间">
          <el-date-picker
            v-model="filters.dateRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 260px"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" @click="onSearch">
            查询
          </el-button>
          <el-button @click="onReset">重置</el-button>
        </el-form-item>
      </el-form>

      <div class="audit-tip-bar">
        <span class="audit-tip">
          审计日志为 append-only（DB 触发器禁止 UPDATE/DELETE），仅管理员可查看
        </span>
      </div>

      <!-- 表格 -->
      <el-table
        v-loading="loading"
        :data="list"
        border
        stripe
        style="width: 100%; margin-top: 8px"
        empty-text="暂无审计记录"
      >
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="类型" width="130">
          <template #default="{ row }">
            <el-tag :type="targetTypeTagType(row.target_type)" size="small">
              {{ targetTypeLabel(row.target_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="target_id"
          label="目标 ID"
          width="90"
          align="right"
        />
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <el-tag :type="actionTagType(row.action)" size="small" effect="plain">
              {{ row.action }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="actor_id"
          label="操作人"
          width="80"
          align="right"
        />
        <el-table-column prop="actor_ip" label="IP" width="130" />
        <el-table-column label="时间" width="160">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="user_agent"
          label="UA"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column label="Hash (SM3)" width="140">
          <template #default="{ row }">
            <el-tooltip
              :content="row.hash_sm3"
              placement="top"
              :show-after="200"
            >
              <span class="hash-cell">{{ shortHash(row.hash_sm3) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="Prev Hash" width="140">
          <template #default="{ row }">
            <el-tooltip
              :content="row.prev_hash"
              placement="top"
              :show-after="200"
            >
              <span class="hash-cell">{{ shortHash(row.prev_hash) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="Detail" min-width="220">
          <template #default="{ row }">
            <el-tooltip
              :content="prettyDetail(row.detail)"
              placement="top"
              :show-after="300"
            >
              <pre class="detail-cell">{{ prettyDetail(row.detail) }}</pre>
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="audit-pagination">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100, 200]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @current-change="fetchList"
          @size-change="fetchList"
        />
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.audit-container {
  padding: 16px;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
}
.card-actions {
  display: flex;
  gap: 8px;
}

/* 统计卡：14 张（本页 + 12 类型 + 累计）紧凑布局
 * - minmax 108px（缩窄宽度），gap 8px
 * - el-card__body padding 10px 8px（缩内边距）
 * - stat-value 18px，label 11px
 * 整体行高 ~46px，与参考项目 backup.vue 风格保持一致
 */
.stats-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(108px, 1fr));
  gap: 8px;
}
.stat-card {
  text-align: center;
}
.stat-card :deep(.el-card__body) {
  padding: 10px 8px;
}
.stat-label {
  font-size: 11px;
  color: #6b7280;
  margin-bottom: 4px;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stat-value {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
  line-height: 1.2;
}
.stat-primary { color: #409eff; }
.stat-success { color: #67c23a; }
.stat-warning { color: #e6a23c; }
.stat-info    { color: #909399; }
.stat-danger  { color: #f56c6c; }

.audit-filter {
  margin-top: 14px;
  margin-bottom: 4px;
}
.audit-tip-bar {
  display: flex;
  align-items: center;
  margin-bottom: 4px;
}
.audit-tip {
  color: #909399;
  font-size: 12px;
}

.hash-cell {
  font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 12px;
  color: #606266;
  cursor: help;
}
.detail-cell {
  margin: 0;
  font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 12px;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  cursor: help;
  color: #606266;
  line-height: 1.4;
}
.audit-pagination {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

@media (max-width: 1200px) {
  .stats-row {
    grid-template-columns: repeat(4, 1fr);
  }
}
@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(3, 1fr);
  }
  .audit-filter :deep(.el-form-item) {
    width: 100%;
  }
}
</style>
