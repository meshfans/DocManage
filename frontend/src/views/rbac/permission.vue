<template>
  <div class="permission-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><Key /></el-icon>
            <span>权限管理</span>
            <el-tag size="small" type="info" effect="plain">
              {{ permList.length }} 项权限
            </el-tag>
          </div>
          <div class="header-actions">
            <el-select
              v-model="filterModule"
              placeholder="按模块筛选"
              clearable
              style="width: 180px"
            >
              <el-option
                v-for="m in moduleList"
                :key="m"
                :label="`${m} (${moduleStats[m] || 0})`"
                :value="m"
              />
            </el-select>
            <el-input
              v-model="filterText"
              placeholder="搜索 code/名称/路径"
              clearable
              style="width: 220px"
              @keyup.enter="handleSearch"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-button @click="handleSearch">搜索</el-button>
            <el-button @click="loadPermissions">
              <el-icon style="margin-right: 4px"><Refresh /></el-icon>
              刷新
            </el-button>
            <el-button v-perms="'rbac:permissions:upsert'" type="primary" @click="handleCreate">
              <el-icon style="margin-right: 4px"><Plus /></el-icon>
              新建权限
            </el-button>
          </div>
        </div>
      </template>

      <!-- 统计卡：按 HTTP 方法 + 系统预置 + 总数（与 system/audit.vue、system/backup.vue 保持一致）-->
      <div class="stats-row">
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">权限总数</div>
          <div class="stat-value">{{ permList.length }}</div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">模块数</div>
          <div class="stat-value stat-primary">
            {{ moduleList.length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">系统预置</div>
          <div class="stat-value stat-warning">
            {{ permList.filter(p => p.is_system).length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">自定义</div>
          <div class="stat-value stat-info">
            {{ permList.filter(p => !p.is_system).length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">已启用</div>
          <div class="stat-value stat-success">
            {{ permList.filter(p => p.status === 'active').length }}
          </div>
        </el-card>
      </div>

      <!-- 列表 -->
      <el-table
        v-loading="loading"
        :data="pagedPermList"
        stripe
        style="margin-top: 12px"
        empty-text="暂无权限，点击右上角「新建权限」开始"
      >
        <el-table-column prop="code" label="权限码" min-width="280" show-overflow-tooltip>
          <template #default="{ row }">
            <code style="font-size: 12px">{{ row.code }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column label="模块" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.module || "其他" }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="HTTP 方法" width="110">
          <template #default="{ row }">
            <el-tag
              :type="getMethodTagType(row.http_method)"
              size="small"
            >
              {{ row.http_method || "—" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="API 路径" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">
            <code v-if="row.api_path" style="font-size: 12px">{{ row.api_path }}</code>
            <span v-else style="color: #c0c4cc">—</span>
          </template>
        </el-table-column>
        <el-table-column label="系统预置" width="90" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.is_system" type="warning" size="small">是</el-tag>
            <el-tag v-else type="info" size="small">否</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-switch
              :model-value="row.status === 'active'"
              :disabled="row.is_system"
              @change="handleToggleStatus(row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <!-- 2026-06-25 A.3 修复：系统预置 permission 不可编辑（与 is_system=1 在后端对应） -->
            <el-button
              v-perms="'rbac:permissions:upsert'"
              link
              type="primary"
              size="small"
              :disabled="row.is_system"
              @click="handleEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              v-perms="'rbac:permissions:delete'"
              link
              type="danger"
              size="small"
              :disabled="row.is_system"
              @click="handleDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="filteredList.length"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      :close-on-click-modal="false"
      @close="handleDialogClose"
    >
      <el-form :model="formData" label-width="100px">
        <el-form-item label="权限码" required>
          <el-input
            v-model="formData.code"
            :disabled="dialogMode === 'edit'"
            placeholder="如 customer:create"
          />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="formData.name" placeholder="中文名" />
        </el-form-item>
        <el-form-item label="模块" required>
          <el-input v-model="formData.module" placeholder="如 customer" />
        </el-form-item>
        <el-form-item label="HTTP 方法">
          <el-select v-model="formData.http_method" clearable style="width: 100%">
            <el-option value="GET" label="GET" />
            <el-option value="POST" label="POST" />
            <el-option value="PUT" label="PUT" />
            <el-option value="DELETE" label="DELETE" />
            <el-option value="PATCH" label="PATCH" />
          </el-select>
          <span style="margin-left: 8px; color: #909399; font-size: 12px">
            空 = 仅按钮权限（无 API）
          </span>
        </el-form-item>
        <el-form-item label="API 路径">
          <el-input
            v-model="formData.api_path"
            placeholder="如 /api/customer/create，路径参数用 :id"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="formData.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="formData.status">
            <el-radio value="active">启用</el-radio>
            <el-radio value="disabled">禁用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, reactive, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Search, Refresh, Plus, Key } from "@element-plus/icons-vue";
import {
  listPermissions,
  upsertPermission,
  deletePermission
} from "@/api/rbac";
import type { Permission } from "@/api/rbac";

defineOptions({ name: "PermissionManagement" });

// ==================== 列表 + 分页 ====================
const loading = ref(false);
const submitting = ref(false);
const permList = ref<Permission[]>([]);

// 分页（前端分页，与 system/user.vue、system/backup.vue、rbac/role.vue 保持一致）
const currentPage = ref(1);
const pageSize = ref(20);

// 过滤
const filterText = ref("");
const filterModule = ref("");

// 编辑对话框
const dialogVisible = ref(false);
const dialogMode = ref<"create" | "edit">("create");
const dialogTitle = ref("");
const formData = reactive<Permission>({
  id: 0,
  code: "",
  name: "",
  module: "",
  api_path: "",
  http_method: "GET",
  description: "",
  is_system: false,
  status: "active"
});

async function loadPermissions() {
  loading.value = true;
  try {
    const res: any = await listPermissions();
    permList.value = res.data?.list || [];
    // 加载完成后重置到第一页
    currentPage.value = 1;
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载权限失败");
  } finally {
    loading.value = false;
  }
}

const moduleList = computed(() => {
  const set = new Set<string>();
  permList.value.forEach((p) => set.add(p.module || "其他"));
  return Array.from(set).sort();
});

// 按 module 分组的统计
const moduleStats = computed(() => {
  const stats: Record<string, number> = {};
  for (const p of permList.value) {
    const m = p.module || "其他";
    stats[m] = (stats[m] || 0) + 1;
  }
  return stats;
});

const filteredList = computed(() => {
  let list = permList.value;
  if (filterModule.value) {
    list = list.filter((p) => (p.module || "其他") === filterModule.value);
  }
  if (filterText.value) {
    const txt = filterText.value.toLowerCase();
    list = list.filter(
      (p) =>
        p.code.toLowerCase().includes(txt) ||
        p.name.toLowerCase().includes(txt) ||
        (p.api_path || "").toLowerCase().includes(txt)
    );
  }
  return list;
});

// 当前页数据
const pagedPermList = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value;
  return filteredList.value.slice(start, start + pageSize.value);
});

// 监听过滤条件变化，重置到第一页
watch([filterModule, filterText], () => {
  currentPage.value = 1;
});

function handleSearch() {
  currentPage.value = 1;
}

function handleSizeChange(val: number) {
  pageSize.value = val;
  currentPage.value = 1;
}

function handleCurrentChange(val: number) {
  currentPage.value = val;
}

function handleCreate() {
  dialogMode.value = "create";
  dialogTitle.value = "新建权限";
  Object.assign(formData, {
    id: 0,
    code: "",
    name: "",
    module: "",
    api_path: "",
    http_method: "GET",
    description: "",
    is_system: false,
    status: "active"
  });
  dialogVisible.value = true;
}

function handleEdit(row: Permission) {
  dialogMode.value = "edit";
  dialogTitle.value = "编辑权限";
  Object.assign(formData, JSON.parse(JSON.stringify(row)));
  dialogVisible.value = true;
}

async function handleDelete(row: Permission) {
  try {
    await ElMessageBox.confirm(
      `确定删除权限 "${row.code}" 吗？此操作不可恢复。`,
      "确认删除",
      {
        type: "warning",
        confirmButtonText: "确认删除",
        cancelButtonText: "取消"
      }
    );
    await deletePermission(row.code);
    ElMessage.success("删除成功");
    loadPermissions();
  } catch (e: any) {
    if (e !== "cancel" && e !== "close") {
      ElMessage.error(e?.response?.data?.message || e?.message || e || "删除失败");
    }
  }
}

async function handleSavePermission() {
  if (!formData.code || !formData.name) {
    ElMessage.error("权限码和名称不能为空");
    return;
  }
  if (!formData.module) {
    ElMessage.error("模块不能为空");
    return;
  }
  submitting.value = true;
  try {
    await upsertPermission(formData);
    ElMessage.success("保存成功");
    dialogVisible.value = false;
    loadPermissions();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "保存失败");
  } finally {
    submitting.value = false;
  }
}

async function handleToggleStatus(row: Permission) {
  const newStatus = row.status === "active" ? "disabled" : "active";
  try {
    await upsertPermission({ ...row, status: newStatus });
    ElMessage.success(newStatus === "active" ? "已启用" : "已禁用");
    loadPermissions();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "操作失败");
  }
}

type TagType = "success" | "primary" | "warning" | "danger" | "info";
function getMethodTagType(method: string): TagType {
  switch (method) {
    case "GET":
      return "success";
    case "POST":
      return "primary";
    case "PUT":
      return "warning";
    case "DELETE":
      return "danger";
    case "PATCH":
      return "info";
    default:
      return "info";
  }
}

function handleDialogClose() {
  // 重置表单
  Object.assign(formData, {
    id: 0,
    code: "",
    name: "",
    module: "",
    api_path: "",
    http_method: "GET",
    description: "",
    is_system: false,
    status: "active"
  });
}

onMounted(() => {
  loadPermissions();
});
</script>

<style scoped>
/* 与 system/user.vue、system/department.vue、system/backup.vue 保持一致的容器样式 */
.permission-container {
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

.header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

/* 统计卡：与 system/audit.vue、system/backup.vue、rbac/role.vue 保持一致 */
.stats-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.stat-card {
  text-align: center;
}

.stat-card :deep(.el-card__body) {
  padding: 14px 10px;
}

.stat-label {
  color: #909399;
  font-size: 12px;
  margin-bottom: 6px;
}

.stat-value {
  font-size: 22px;
  font-weight: 600;
  color: #303133;
}

.stat-primary {
  color: #409eff;
}

.stat-success {
  color: #67c23a;
}

.stat-warning {
  color: #e6a23c;
}

.stat-info {
  color: #909399;
}

.stat-danger {
  color: #f56c6c;
}

/* 分页：与 system/user.vue 保持一致 */
.pagination-container {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* 响应式 */
@media (max-width: 1200px) {
  .stats-row {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(2, 1fr);
  }

  .header-actions {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>