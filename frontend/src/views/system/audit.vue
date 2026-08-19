<script setup lang="ts">
/**
 * 审计日志查询 UI（admin only）
 *
 * 功能：
 *   - 按 target_type / actor_id / action / 时间区间筛选
 *   - 分页（默认 20 / 页，上限 200）
 *   - 导出 CSV（按当前 list 全量导出，不被分页限制）
 *   - 触发哈希链验证（POST /api/audit/reconcile），结果通过 X-Audit-Broken-At header 透传
 *
 * 设计要点：
 *   - CSV 导出走前端，不依赖后端，端点少一个
 *   - 时间筛选用 el-date-picker daterange，转成 unix seconds 提交
 *   - detail 字段是 JSON 字符串，前端做 pretty print
 *   - 哈希链字段展示前 12 字符（节省宽度），悬停 tooltip 显示完整
 */
import { ref, reactive, onMounted, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import dayjs from "dayjs";
import {
  listAudit,
  reconcileAuditChain,
  TARGET_TYPE_OPTIONS,
  type AuditRecord,
  type ListAuditParams
} from "@/api/audit";
import { useIsAdmin } from "@/composables/useIsAdmin";
import { extractErrorMessage } from "@/utils/error";

defineOptions({ name: "AuditLog" });

const isAdmin = useIsAdmin();

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

/** 当前筛选参数（fetch 时组装） */
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
    ElMessage.error(e?.response?.data?.message || e?.message || "请求失败");
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
 * 导出 CSV（前端实现，免后端开新端点）。
 *
 * ⚠️ 2026-08-19 M-F1 修复：所有字段统一切到 csvEscape() 转义。
 *   旧实现只对 `detail` 字段做 `"..."` 包裹，但 action / user_agent / actor_ip
 *   等可能含 `,` `"` `\n`（user agent 里常见换行 / 双引号），直接拼接会破坏 CSV。
 *   修复：用 csvEscape 统一处理，遇到 , " \r \n 自动加双引号包裹并转义 "。
 */
function csvEscape(value: unknown): string {
  if (value === null || value === undefined) return "";
  const s = String(value);
  // RFC 4180：含 " , \r \n 必须用双引号包裹，内部 " 替换为 ""
  if (/[",\r\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

/**
 * 导出 CSV（前端实现，免后端开新端点）。
 * 所有字段都走 csvEscape，避免 user_agent / actor_ip / detail 含换行或逗号破坏 CSV。
 */
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

/** 触发哈希链验证（admin only）。失败响应通过 X-Audit-Broken-At header 携带断点。 */
async function onReconcile() {
  try {
    await ElMessageBox.confirm(
      "将遍历全表逐行重算 hash，大数据量下耗时较长，确认执行？",
      "哈希链验证",
      { confirmButtonText: "开始验证", cancelButtonText: "取消", type: "warning" }
    );
  } catch {
    return;
  }
  reconciling.value = true;
  try {
    await reconcileAuditChain();
    ElMessage.success("审计链验证通过（OK）");
  } catch (e: any) {
    // ⚠️ 2026-08-19 M-F2 修复：5xx 错误优先用 extractErrorMessage 走 i18n 翻译，
    // 之前直接读 e.response.data.message 会显示英文代码。
    // 断裂点信息仍走 X-Audit-Broken-At header（后端 utils.ErrorWithDetail 透传）。
    const brokenAt =
      e?.response?.headers?.["x-audit-broken-at"] ||
      e?.response?.headers?.["X-Audit-Broken-At"];
    if (brokenAt) {
      // 断点 ≠ 业务错误码，单独拼接（i18n 找不到专属 code，因为断点 id 是动态值）。
      ElMessage.error(
        `审计链在 id=${brokenAt} 处断裂，请检查详情`
      );
    } else {
      // 通用错误：先按 code 翻译，失败 fallback 到 message
      ElMessage.error(extractErrorMessage(e, "zh-CN"));
    }
    console.error("[audit] reconcile failed", e);
  } finally {
    reconciling.value = false;
  }
}

/** 哈希字段截断展示（tooltip 显示完整） */
function shortHash(h: string) {
  if (!h) return "";
  return h.length > 12 ? h.slice(0, 12) + "…" : h;
}

/** detail 字段 JSON 美化（解析失败原样返回） */
function prettyDetail(d: string | undefined): string {
  if (!d) return "";
  try {
    return JSON.stringify(JSON.parse(d), null, 2);
  } catch {
    return d;
  }
}

/** 创建时间格式化 */
function fmtTime(t: number): string {
  if (!t) return "";
  return dayjs.unix(t).format("YYYY-MM-DD HH:mm:ss");
}

const hasData = computed(() => list.value.length > 0);

onMounted(fetchList);
</script>

<template>
  <div class="audit-page">
    <el-card shadow="never">
      <!-- 筛选区 -->
      <el-form :inline="true" :model="filters" class="audit-filter">
        <el-form-item label="对象类型">
          <el-select
            v-model="filters.target_type"
            placeholder="全部"
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
        <el-form-item label="对象 ID">
          <el-input-number
            v-model="filters.target_id"
            :min="0"
            placeholder="0=全部"
            controls-position="right"
            style="width: 140px"
          />
        </el-form-item>
        <el-form-item label="操作人 ID">
          <el-input-number
            v-model="filters.actor_id"
            :min="0"
            placeholder="0=全部"
            controls-position="right"
            style="width: 140px"
          />
        </el-form-item>
        <el-form-item label="动作">
          <el-input
            v-model="filters.action"
            placeholder="如 login.success"
            clearable
            style="width: 180px"
          />
        </el-form-item>
        <el-form-item label="时间区间">
          <el-date-picker
            v-model="filters.dateRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 240px"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" @click="onSearch">
            查询
          </el-button>
          <el-button @click="onReset">重置</el-button>
        </el-form-item>
      </el-form>

      <!-- 操作区 -->
      <div class="audit-toolbar">
        <el-button
          type="success"
          :disabled="!hasData"
          @click="exportCsv"
        >
          导出 CSV
        </el-button>
        <el-button
          type="warning"
          :loading="reconciling"
          @click="onReconcile"
        >
          验证哈希链
        </el-button>
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
        style="width: 100%"
        empty-text="暂无审计记录"
      >
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="target_type" label="对象类型" width="160" />
        <el-table-column prop="target_id" label="对象 ID" width="100" />
        <el-table-column prop="action" label="动作" width="160" />
        <el-table-column prop="actor_id" label="操作人" width="90" />
        <el-table-column prop="actor_ip" label="IP" width="140" />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
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
              :show-after="200"
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
.audit-page {
  padding: 16px;
}
.audit-filter {
  margin-bottom: 8px;
}
.audit-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.audit-tip {
  color: #909399;
  font-size: 12px;
  margin-left: auto;
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
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 80px;
  overflow: hidden;
  cursor: help;
  color: #606266;
}
.audit-pagination {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
