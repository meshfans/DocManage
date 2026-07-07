<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Histogram } from "@element-plus/icons-vue";
import {
  listBackups,
  getBackup,
  backupNow,
  restoreBackup,
  downloadBackup,
  deleteBackup,
  isAuditRecord,
  type BackupManifest,
  type ListBackupsParams
} from "@/api/backup";
import { useIsAdmin } from "@/composables/useIsAdmin";

defineOptions({ name: "BackupManagement" });

const isAdmin = useIsAdmin();

// ========== 状态 ==========
const loading = ref(false);
const backingUp = ref(false);
const restoring = ref(false);
const list = ref<BackupManifest[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const pageSizeBytes = ref(0);
const byStatus = ref<Record<string, number>>({});

const filters = reactive<ListBackupsParams>({
  type: undefined,
  status: undefined
});

// ========== 统计卡 ==========
const statsCards = computed(() => {
  const verified =
    (byStatus.value.verified ?? 0) + (byStatus.value.success ?? 0);
  const corrupted = byStatus.value.corrupted ?? 0;
  const missing = byStatus.value.missing ?? 0;
  const totalCount = Object.values(byStatus.value).reduce((a, b) => a + b, 0);
  return {
    totalCount,
    verified,
    corrupted,
    missing,
    totalBytes: pageSizeBytes.value
  };
});

// ========== 状态映射 ==========
const statusTypeMap: Record<
  string,
  "" | "success" | "warning" | "info" | "danger"
> = {
  verified: "success",
  success: "info",
  corrupted: "danger",
  missing: "danger",
  failed: "danger",
  pending: "warning",
  running: "warning"
};

const statusLabelMap: Record<string, string> = {
  verified: "已验证",
  success: "待验证",
  corrupted: "损坏",
  missing: "丢失",
  failed: "失败",
  pending: "排队中",
  running: "执行中"
};

// ========== 加载 ==========
async function loadList() {
  loading.value = true;
  try {
    const res = await listBackups({
      ...filters,
      page: page.value,
      page_size: pageSize.value
    });
    if (res.success && res.data) {
      list.value = res.data.list || [];
      total.value = res.data.total;
      pageSizeBytes.value = res.data.page_size_bytes;
      byStatus.value = res.data.by_status || {};
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  }
  loading.value = false;
}

watch(filters, () => {
  page.value = 1;
  loadList();
});

watch([page, pageSize], () => loadList());

// ========== 操作 ==========
async function handleBackupNow() {
  try {
    await ElMessageBox.confirm(
      "立即执行一次全量备份？\n\n• 备份目录：./backups\n• 永不清除（手动备份）\n• 完成后自动验证",
      "立即备份",
      { type: "info", confirmButtonText: "开始备份", cancelButtonText: "取消" }
    );
  } catch {
    return;
  }
  backingUp.value = true;
  try {
    const res = await backupNow();
    if (res.success) {
      ElMessage.success(
        `备份完成 manifest_id=${res.data?.manifest_id}（已自动验证）`
      );
      await loadList();
    } else {
      ElMessage.error(res.message || "备份失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "备份失败");
  }
  backingUp.value = false;
}

// ========== 恢复对话框 ==========
const restoreDialog = reactive({
  visible: false,
  backupId: 0,
  backupSnowid: "",
  confirmText: "",
  loading: false,
  // 2026-07-06 round6 精简：删除 encrypted 字段
  //   后端 manifest 已删 encrypted 列，备份永远明文
  row: null as BackupManifest | null
});

function openRestoreDialog(row: BackupManifest) {
  restoreDialog.backupId = row.id;
  restoreDialog.backupSnowid = row.snowid;
  restoreDialog.confirmText = "";
  restoreDialog.row = row;
  restoreDialog.visible = true;
}

// ========== 详情对话框 ==========
const detailDialog = reactive({
  visible: false,
  loading: false,
  data: null as BackupManifest | null
});

async function openDetailDialog(row: BackupManifest) {
  detailDialog.data = row; // 先用列表数据展示，立刻打开
  detailDialog.visible = true;
  detailDialog.loading = true;
  try {
    const res = await getBackup(row.id);
    if (res.success && res.data) {
      detailDialog.data = res.data;
    }
  } catch (e) {
    console.error("[BackupDetail] 加载详情失败:", e);
  }
  detailDialog.loading = false;
}

// 2026-06-27 bug #3 修复：通过 ID 直接打开详情（用于父/源备份跳转链接）
async function openDetailDialogById(id: number) {
  // 先清空旧数据，避免闪烁
  detailDialog.data = null;
  detailDialog.visible = true;
  detailDialog.loading = true;
  try {
    const res = await getBackup(id);
    if (res.success && res.data) {
      detailDialog.data = res.data;
    } else {
      ElMessage.error(res.message || "加载备份详情失败");
      detailDialog.visible = false;
    }
  } catch (e: any) {
    console.error("[BackupDetail] 按 ID 加载失败:", e);
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载备份详情失败");
    detailDialog.visible = false;
  }
  detailDialog.loading = false;
}

async function handleRestore() {
  if (restoreDialog.confirmText !== "恢复") {
    ElMessage.warning('请输入"恢复"以确认');
    return;
  }
  restoreDialog.loading = true;
  try {
    // 1) 先 dry_run 预演
    const dryRes = await restoreBackup({
      full_backup_id: restoreDialog.backupId,
      dry_run: true
    });
    if (!dryRes.success) {
      ElMessage.error(dryRes.message || "预演失败");
      restoreDialog.loading = false;
      return;
    }

    // 2) 实际恢复（同步触发维护模式）
    const realRes = await restoreBackup({
      full_backup_id: restoreDialog.backupId,
      dry_run: false
    });
    if (realRes.success) {
      ElMessage.success("恢复完成！请刷新页面或重新登录");
      restoreDialog.visible = false;
    } else {
      ElMessage.error(realRes.message || "恢复失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "恢复失败");
  }
  restoreDialog.loading = false;
}

// ========== 删除 ==========
async function handleDelete(row: BackupManifest) {
  try {
    await ElMessageBox.confirm(
      `确认删除备份？\n\nID: ${row.id}\nSnowID: ${row.snowid}\n类型: ${row.type}\n文件: ${row.file_path}\n\n此操作不可恢复！`,
      "删除备份",
      {
        type: "warning",
        confirmButtonText: "确认删除",
        cancelButtonText: "取消"
      }
    );
  } catch {
    return;
  }
  try {
    const res = await deleteBackup(row.id);
    if (res.success) {
      ElMessage.success("删除成功");
      await loadList();
    } else {
      ElMessage.error(res.message || "删除失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "删除失败");
  }
}

// ========== 工具函数 ==========
function formatSize(bytes: number): string {
  if (bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(2)} ${units[i]}`;
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "-";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatUnix(unix: number): string {
  if (!unix || unix <= 0) return "-";
  const d = new Date(unix * 1000);
  return d.toLocaleString("zh-CN", { hour12: false });
}

function truncateHash(hash: string, len = 12): string {
  if (!hash) return "-";
  if (hash.length <= len * 2) return hash;
  return hash.slice(0, len) + "..." + hash.slice(-len);
}

async function copyText(text: string) {
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    ElMessage.success("已复制到剪贴板");
  } catch (e) {
    // Fallback：旧浏览器无 Clipboard API
    const ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    document.body.removeChild(ta);
    ElMessage.success("已复制");
  }
}

// ========== 初始化 ==========
onMounted(() => {
  loadList();
});
</script>

<template>
  <div class="backup-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><Histogram /></el-icon>
            <span>备份管理</span>
          </div>
          <div>
            <el-select
              v-model="filters.type"
              placeholder="类型"
              clearable
              style="width: 110px; margin-right: 8px"
            >
              <el-option label="全量" value="full" />
              <el-option label="增量" value="incremental" />
            </el-select>
            <el-select
              v-model="filters.status"
              placeholder="状态"
              clearable
              style="width: 130px; margin-right: 8px"
            >
              <el-option label="排队中" value="pending" />
              <el-option label="执行中" value="running" />
              <el-option label="已验证" value="verified" />
              <el-option label="待验证" value="success" />
              <el-option label="损坏" value="corrupted" />
              <el-option label="丢失" value="missing" />
              <el-option label="失败" value="failed" />
            </el-select>
            <el-button @click="loadList">查询</el-button>
            <!-- 2026-06-25 P1-7.2 修复：原 v-if="isAdmin" 改为 v-perms，业务粒度更准 -->
            <el-button
              v-perms="'system:backup-now'"
              type="primary"
              :loading="backingUp"
              @click="handleBackupNow"
            >
              立即备份
            </el-button>
          </div>
        </div>
      </template>

      <!-- 统计卡 -->
      <div class="stats-row">
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">总备份数</div>
          <div class="stat-value">{{ statsCards.totalCount }}</div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">已验证 / 待验证</div>
          <div class="stat-value stat-success">{{ statsCards.verified }}</div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">损坏 / 丢失</div>
          <div
            class="stat-value"
            :class="{
              'stat-danger': statsCards.corrupted + statsCards.missing > 0
            }"
          >
            {{ statsCards.corrupted + statsCards.missing }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">本页总大小</div>
          <div class="stat-value">{{ formatSize(statsCards.totalBytes) }}</div>
        </el-card>
      </div>

      <el-alert
        v-if="statsCards.corrupted + statsCards.missing > 0"
        type="error"
        :closable="false"
        show-icon
        style="margin: 12px 0"
      >
        <template #title>
          检测到 {{ statsCards.corrupted }} 个损坏 /
          {{ statsCards.missing }} 个丢失备份
        </template>
        建议：立即检查磁盘与备份目录，必要时从其他介质恢复或重新备份
      </el-alert>

      <!-- 列表 -->
      <el-table
        v-loading="loading"
        :data="list"
        stripe
        empty-text="暂无备份"
        style="margin-top: 12px"
      >
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column label="SnowID" min-width="160">
          <template #default="{ row }">
            <code style="font-size: 12px">{{ row.snowid }}</code>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag
              v-if="isAuditRecord(row)"
              size="small"
              type="info"
              effect="plain"
            >
              审计
            </el-tag>
            <el-tag
              v-else
              size="small"
              :type="row.type === 'full' ? 'primary' : 'success'"
            >
              {{ row.type === "full" ? "全量" : "增量" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="110">
          <template #default="{ row }">
            {{ formatSize(row.file_size) }}
          </template>
        </el-table-column>
        <el-table-column label="文件数" width="80" align="right">
          <template #default="{ row }">
            <span style="font-size: 12px; color: #666">
              {{
                row.type === "incremental"
                  ? row.changed_files || 0
                  : row.total_files || 0
              }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTypeMap[row.status] || 'info'">
              {{ statusLabelMap[row.status] || row.status }}
            </el-tag>
            <!-- 2026-07-06 round6 精简：删除「🔒 加密」标记
                 后端 manifest 表已删除 encrypted 字段（备份永远明文） -->
            <div
              v-if="row.verified_at > 0"
              style="color: #999; font-size: 11px; margin-top: 2px"
            >
              {{ formatUnix(row.verified_at) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="SM3 哈希" min-width="140">
          <template #default="{ row }">
            <el-tooltip :content="row.file_hash_sm3 || '无'" placement="top">
              <code style="font-size: 11px">{{
                truncateHash(row.file_hash_sm3)
              }}</code>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="开始时间" min-width="160">
          <template #default="{ row }">
            <span style="font-size: 12px">{{
              formatUnix(row.started_at)
            }}</span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="80">
          <template #default="{ row }">
            <span style="font-size: 12px">{{
              formatDuration(row.duration_ms)
            }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="openDetailDialog(row)"
            >
              详情
            </el-button>
            <!-- 恢复/下载/删除 仅对真实备份显示（审计记录是 readonly 标记） -->
            <template v-if="!isAuditRecord(row)">
              <el-button
                v-if="row.status === 'verified' || row.status === 'success'"
                v-perms="'system:restore'"
                link
                type="primary"
                size="small"
                @click="openRestoreDialog(row)"
              >
                恢复
              </el-button>
              <el-button
                link
                type="primary"
                size="small"
                @click="downloadBackup(row.id)"
              >
                下载
              </el-button>
              <el-button
                v-perms="'backup:delete'"
                link
                type="danger"
                size="small"
                @click="handleDelete(row)"
              >
                删除
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        style="margin-top: 12px; justify-content: flex-end"
      />
    </el-card>

    <!-- 恢复二次确认对话框 -->
    <el-dialog
      v-model="restoreDialog.visible"
      title="恢复备份（危险操作）"
      width="500px"
      :close-on-click-modal="false"
    >
      <el-alert
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      >
        <template #title> 警告：恢复会覆盖当前数据库和上传文件！ </template>
        恢复前会自动快照到 ./backups/snapshot_*/ 目录。<br />
        恢复期间所有业务 API 会返回 503（维护模式自动开启）。
      </el-alert>

      <!-- 2026-07-06 round6 精简：删除「加密备份警告」alert + 恢复弹窗内「加密」descriptions-item
           后端不再支持加密备份（restoreDialog.encrypted 始终为 false） -->

      <el-descriptions :column="1" border size="small">
        <el-descriptions-item label="备份 ID">{{
          restoreDialog.backupId
        }}</el-descriptions-item>
        <el-descriptions-item label="SnowID">{{
          restoreDialog.backupSnowid
        }}</el-descriptions-item>
      </el-descriptions>

      <el-form style="margin-top: 16px">
        <el-form-item label='输入"恢复"以确认'>
          <el-input
            v-model="restoreDialog.confirmText"
            placeholder='输入"恢复"'
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="restoreDialog.visible = false">取消</el-button>
        <el-button
          type="danger"
          :loading="restoreDialog.loading"
          :disabled="restoreDialog.confirmText !== '恢复'"
          @click="handleRestore"
        >
          开始恢复
        </el-button>
      </template>
    </el-dialog>

    <!-- 备份详情对话框（展示元数据 + 三哈希） -->
    <el-dialog
      v-model="detailDialog.visible"
      title="备份详情"
      width="720px"
      :close-on-click-modal="false"
    >
      <div v-loading="detailDialog.loading">
        <template v-if="detailDialog.data">
          <!-- 基本信息 -->
          <div class="detail-section">
            <div class="section-title">基本信息</div>
            <el-descriptions :column="2" border size="small">
              <el-descriptions-item label="ID">{{
                detailDialog.data.id
              }}</el-descriptions-item>
              <el-descriptions-item label="SnowID">
                <code style="font-size: 12px">{{
                  detailDialog.data.snowid
                }}</code>
              </el-descriptions-item>
              <el-descriptions-item label="类型">
                <el-tag
                  v-if="isAuditRecord(detailDialog.data)"
                  size="small"
                  type="info"
                  effect="plain"
                >
                  审计记录（恢复操作）
                </el-tag>
                <el-tag
                  v-else
                  size="small"
                  :type="
                    detailDialog.data.type === 'full' ? 'primary' : 'success'
                  "
                >
                  {{ detailDialog.data.type === "full" ? "全量" : "增量" }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item
                v-if="detailDialog.data.type === 'incremental'"
                label="父备份 ID"
              >
                <el-link
                  v-if="detailDialog.data.parent_id"
                  type="primary"
                  :href="`#/system/backup`"
                  @click="openDetailDialogById(detailDialog.data.parent_id)"
                >
                  {{ detailDialog.data.parent_id }}
                </el-link>
                <span v-else>-</span>
              </el-descriptions-item>
              <!-- 2026-06-27 bug #3 修复：审计记录也展示"源备份 ID"（parent_id 关联） -->
              <el-descriptions-item
                v-if="isAuditRecord(detailDialog.data)"
                label="源备份 ID"
              >
                <el-link
                  v-if="detailDialog.data.parent_id"
                  type="primary"
                  :href="`#/system/backup`"
                  @click="openDetailDialogById(detailDialog.data.parent_id)"
                >
                  #{{ detailDialog.data.parent_id }}
                </el-link>
                <span v-else>-</span>
              </el-descriptions-item>
              <!-- 2026-07-06 round6 精简：删除「加密状态」+「密钥来源」descriptions-item
                   后端 manifest 已删除 encrypted / encryption_algo / encrypt_passphrase 字段
                   备份永远明文，无需展示加密信息 -->
              <el-descriptions-item label="状态">
                <el-tag
                  size="small"
                  :type="statusTypeMap[detailDialog.data.status] || 'info'"
                >
                  {{
                    statusLabelMap[detailDialog.data.status] ||
                    detailDialog.data.status
                  }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="文件大小">{{
                formatSize(detailDialog.data.file_size)
              }}</el-descriptions-item>
              <el-descriptions-item label="文件数">{{
                detailDialog.data.total_files ||
                detailDialog.data.changed_files ||
                0
              }}</el-descriptions-item>
              <el-descriptions-item label="耗时">{{
                formatDuration(detailDialog.data.duration_ms)
              }}</el-descriptions-item>
            </el-descriptions>
          </div>

          <!-- 时间 -->
          <div class="detail-section">
            <div class="section-title">时间</div>
            <el-descriptions :column="2" border size="small">
              <el-descriptions-item label="开始">{{
                formatUnix(detailDialog.data.started_at)
              }}</el-descriptions-item>
              <el-descriptions-item label="完成">{{
                formatUnix(detailDialog.data.finished_at)
              }}</el-descriptions-item>
              <el-descriptions-item label="验证">{{
                formatUnix(detailDialog.data.verified_at)
              }}</el-descriptions-item>
              <el-descriptions-item label="保留至">{{
                formatUnix(detailDialog.data.keep_until)
              }}</el-descriptions-item>
            </el-descriptions>
          </div>

          <!-- 三哈希（重点展示） -->
          <div class="detail-section">
            <div
              v-if="!detailDialog.data.file_hash_sm3"
              style="color: #999; padding: 8px 0"
            >
              无哈希记录（完整性无法校验）
            </div>
            <div v-else class="hash-block">
              <div class="hash-label">SM3（国密）</div>
              <div class="hash-value">
                <code>{{ detailDialog.data.file_hash_sm3 }}</code>
                <el-button
                  link
                  type="primary"
                  size="small"
                  @click="copyText(detailDialog.data.file_hash_sm3)"
                >
                  复制
                </el-button>
              </div>
            </div>
            <div v-if="detailDialog.data.file_hash_sha256" class="hash-block">
              <div class="hash-label">SHA-256（国际标准）</div>
              <div class="hash-value">
                <code>{{ detailDialog.data.file_hash_sha256 }}</code>
                <el-button
                  link
                  type="primary"
                  size="small"
                  @click="copyText(detailDialog.data.file_hash_sha256)"
                >
                  复制
                </el-button>
              </div>
            </div>
            <div v-if="detailDialog.data.file_hash_combined" class="hash-block">
              <div class="hash-label">
                Combined（交叉校验 = SHA256(SM3 + SHA256)）
              </div>
              <div class="hash-value">
                <code>{{ detailDialog.data.file_hash_combined }}</code>
                <el-button
                  link
                  type="primary"
                  size="small"
                  @click="copyText(detailDialog.data.file_hash_combined)"
                >
                  复制
                </el-button>
              </div>
            </div>
          </div>

          <!-- 文件路径 -->
          <div class="detail-section">
            <div class="section-title">文件路径</div>
            <div class="hash-block">
              <div class="hash-value">
                <code style="word-break: break-all">{{
                  detailDialog.data.file_path
                }}</code>
              </div>
            </div>
          </div>

          <!-- 错误信息（如有） -->
          <div v-if="detailDialog.data.error_msg" class="detail-section">
            <div class="section-title">错误信息</div>
            <el-alert type="error" :closable="false" show-icon>
              <template #title>{{ detailDialog.data.error_msg }}</template>
            </el-alert>
          </div>
        </template>
      </div>

      <template #footer>
        <el-button @click="detailDialog.visible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.backup-container {
  padding: 20px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.stats-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 12px;
}
.stat-card {
  text-align: center;
}
.stat-card :deep(.el-card__body) {
  padding: 16px;
}
.stat-label {
  color: #909399;
  font-size: 12px;
  margin-bottom: 6px;
}
.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
}
.stat-success {
  color: #67c23a;
}
.stat-danger {
  color: #f56c6c;
}
@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* 详情对话框 */
.detail-section {
  margin-bottom: 16px;
}
.detail-section:last-child {
  margin-bottom: 0;
}
.section-title {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 8px;
  padding-left: 8px;
  border-left: 3px solid #409eff;
}
.hash-block {
  margin-bottom: 10px;
}
.hash-block:last-child {
  margin-bottom: 0;
}
.hash-label {
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
  margin-bottom: 4px;
}
.hash-value {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  background: var(--el-fill-color-light, #f5f7fa);
  border-radius: 4px;
  font-family: monospace;
  font-size: 11px;
  word-break: break-all;
}
.hash-value code {
  flex: 1;
  color: var(--el-text-color-primary, #303133);
}
</style>
